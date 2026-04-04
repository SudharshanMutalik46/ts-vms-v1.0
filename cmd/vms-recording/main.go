package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"syscall"

	_ "github.com/lib/pq"
	"github.com/technosupport/ts-vms/internal/control"
	"github.com/technosupport/ts-vms/internal/crypto"
	"github.com/technosupport/ts-vms/internal/recording"
	"gopkg.in/yaml.v3"
)

func mergeCameraConfigs(base, extra []recording.CameraConfig) []recording.CameraConfig {
	m := make(map[string]recording.CameraConfig, len(base)+len(extra))
	for _, cam := range base {
		if cam.ID != "" {
			m[cam.ID] = cam
		}
	}
	for _, cam := range extra {
		if cam.ID != "" {
			if existing, ok := m[cam.ID]; ok {
				if cam.RtspURL != "" {
					existing.RtspURL = cam.RtspURL
				}
				if cam.Codec != "" {
					existing.Codec = cam.Codec
				}
				if cam.SegmentFormat != "" {
					existing.SegmentFormat = cam.SegmentFormat
				}
				if cam.RTSPTransport != "" {
					existing.RTSPTransport = cam.RTSPTransport
				}
				existing.Enabled = cam.Enabled
				m[cam.ID] = existing
				continue
			}
			m[cam.ID] = cam
		}
	}
	out := make([]recording.CameraConfig, 0, len(m))
	for _, cam := range m {
		out = append(out, cam)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func main() {
	cfgPath := "config/recording.yaml"
	if v := os.Getenv("TS_VMS_RECORDING_CONFIG"); v != "" {
		cfgPath = v
	}
	cfgBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		log.Fatalf("read config: %v", err)
	}

	var cfg recording.Config
	if err := yaml.Unmarshal(cfgBytes, &cfg); err != nil {
		log.Fatalf("parse config: %v", err)
	}
	cfg.ApplyDefaults()
	// DevRbacBypass is determined by config file or defaults

	var db *sql.DB
	dsn := os.Getenv("TS_VMS_DSN")
	if dsn == "" {
		dsn = os.Getenv("TS_VMS_PG_DSN")
	}
	if dsn != "" {
		db, err = sql.Open("postgres", dsn)
		if err != nil {
			log.Fatalf("open db: %v", err)
		}
	}
	store := recording.NewPostgresStore(db)
	if store.Available() {
		if dbCams, err := store.LoadEnabledCameras(context.Background()); err == nil && len(dbCams) > 0 {
			cfg.Cameras = mergeCameraConfigs(cfg.Cameras, dbCams)
		} else if err != nil && err != recording.ErrDBUnavailable {
			log.Printf("recording bootstrap camera load skipped: %v", err)
		}
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid config: %v", err)
	}
	if cfg.FailoverRecovery.DBRequiredForReady && store.PingContext(context.Background()) != nil {
		log.Fatal("db is required for readiness but unavailable")
	}

	schedules := cfg.Schedules
	if db != nil {
		if dbSchedules, err := store.LoadSchedules(context.Background()); err == nil && len(dbSchedules) > 0 {
			schedules = dbSchedules
		}
	}
	keyring := crypto.NewKeyring()
	if err := keyring.LoadFromEnv(); err != nil {
		log.Printf("[WARNING] failed to load master keys from environment: %v. RTSP authentication may fail.", err)
	}

	scheduler := recording.NewScheduleEngine(schedules)
	license := recording.NewLicenseGate(cfg.Limits.MaxRecordingCameras)
	supervisor := recording.NewRecordingArchiverService(&cfg, scheduler, license, store, keyring)
	health := recording.NewHealthServer(&cfg, supervisor, scheduler, store)
	exporter := recording.NewExportService(&cfg, store)
	reconciler := recording.NewReconciler(&cfg, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	recMgr := &recording.RecoveryManager{
		PidPath: "vms-recording.pid",
		Run: func(runCtx context.Context) error {
			return reconciler.Run(runCtx)
		},
	}
	if err := recMgr.CheckAndProtect(ctx); err != nil {
		log.Fatalf("startup recovery failed: %v", err)
	}
	defer recMgr.CleanExit()

	go supervisor.Run(ctx)
	health.Start()

	internalAPI := &recording.InternalAPI{
		ServiceKey:        os.Getenv("TS_VMS_SERVICE_KEY"),
		RecordingArchiver: supervisor,
		ScheduleStore:     store,
	}
	go http.ListenAndServe(":8087", internalAPI.ServeMux())

	publicAPI := &control.PublicRecordingAPI{
		InternalBaseURL: "http://127.0.0.1:8087",
		ServiceKey:      os.Getenv("TS_VMS_SERVICE_KEY"),
		DB:              store,
		Exporter:        exporter,
		Config:          &cfg,
		DefaultTenantID: cfg.Global.DefaultTenantID,
		DefaultSiteID:   cfg.Global.DefaultSiteID,
	}
	go http.ListenAndServe(":8088", publicAPI.ServeMux())

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	cancel()
}
