package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/lib/pq"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"gopkg.in/yaml.v3"

	"github.com/technosupport/ts-vms/internal/api"
	"github.com/technosupport/ts-vms/internal/audit"
	"github.com/technosupport/ts-vms/internal/auth"
	"github.com/technosupport/ts-vms/internal/cameras"
	"github.com/technosupport/ts-vms/internal/crypto"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/discovery"
	"github.com/technosupport/ts-vms/internal/health"
	"github.com/technosupport/ts-vms/internal/license"
	"github.com/technosupport/ts-vms/internal/middleware"
	"github.com/technosupport/ts-vms/internal/nvr"
	"github.com/technosupport/ts-vms/internal/platform/paths"
	"github.com/technosupport/ts-vms/internal/platform/windows"
	"github.com/technosupport/ts-vms/internal/ratelimit"
	"github.com/technosupport/ts-vms/internal/session"
	"github.com/technosupport/ts-vms/internal/tokens"
	"github.com/technosupport/ts-vms/internal/users"

	_ "github.com/technosupport/ts-vms/internal/nvr/adapters/dahua"
	_ "github.com/technosupport/ts-vms/internal/nvr/adapters/hikvision"
	_ "github.com/technosupport/ts-vms/internal/nvr/adapters/onvif"
	_ "github.com/technosupport/ts-vms/internal/nvr/adapters/rtsp"
)

const (
	serviceName  = "TS-VMS-Control"
	eventIDStart = 100
	eventIDStop  = 101
	eventIDError = 102
)

func main() {
	// 1. Windows Service Check
	isService := windows.IsWindowsService()
	elog := windows.NewEventLogger(serviceName)
	defer elog.Close()

	if isService {
		elog.Info(eventIDStart, "Starting as Windows Service")
	}

	stopChan := make(chan struct{})
	if isService {
		go func() {
			if err := windows.RunAsService(serviceName, stopChan); err != nil {
				elog.Error(eventIDError, fmt.Sprintf("Service run error: %v", err))
				os.Exit(1)
			}
		}()
	}

	// 2. Platform Paths
	if err := paths.EnsureDirs(); err != nil {
		elog.Error(eventIDError, fmt.Sprintf("Platform init error: %v", err))
		log.Fatalf("Platform init error: %v", err)
	}

	// 3. Config
	dbHost := os.Getenv("DB_HOST")
	dbUser := os.Getenv("DB_USER")
	dbPass := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")
	redisAddr := os.Getenv("REDIS_ADDR")
	jwtKey := os.Getenv("JWT_SIGNING_KEY")

	if jwtKey == "" {
		jwtKey = "dev-secret-do-not-use-in-prod"
	}
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	connStr := fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable", dbUser, dbPass, dbHost, dbName)

	// 2. DB Init
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("DB open error: %v", err)
	}
	if err := db.Ping(); err != nil {
		log.Fatalf("DB ping error: %v", err)
	}

	// 3. Components
	// Shared Redis Client
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})

	// Managers
	sessionMgr := session.NewManager(redisAddr, "") // TODO: Update SessionMgr to use shared client in future refactor
	tokenMgr := tokens.NewManager(jwtKey)

	// Audit Service (Phase 1.5)
	auditService := audit.NewService(db)

	// Config Spooler (Using default from task or env helper later)
	// For now using hardcoded default from prompt requirements via ConfigureFailover
	audit.ConfigureFailover("C:\\ProgramData\\TechnoSupport\\VMS\\audit_spool", 1024)
	auditService.StartReplayer(context.Background()) // Background context for long running

	// License Manager (Phase 1.6)
	// Config Loading - Quick inline for Phase 1.6
	var licCfg struct {
		License struct {
			Path          string `yaml:"path"`
			PublicKeyPath string `yaml:"public_key_path"`
		} `yaml:"license"`
	}
	// Re-read config (inefficient but safe for this phase wiring)
	licCfgData, _ := os.ReadFile("config/default.yaml")
	_ = yaml.Unmarshal(licCfgData, &licCfg)

	// 1. Create Parser
	licenseParser, err := license.NewParser(licCfg.License.PublicKeyPath)
	if err != nil {
		log.Printf("Warning: Failed to load License Public Key: %v. License verification will fail.", err)
	}

	// 2. Create Manager
	usageStub := &license.StubUsageProvider{}
	licenseManager := license.NewManager(licCfg.License.Path, licenseParser, usageStub, auditService)

	// 3. Start Watcher & Scheduler
	licenseManager.StartWatcher(context.Background())
	licenseScheduler := license.NewScheduler(licenseManager)
	licenseScheduler.Start(context.Background())

	// 3.1 Camera Components (Phase 2.1)
	camRepo := data.CameraModel{DB: db}
	camService := cameras.NewService(camRepo, licenseManager, auditService)
	camHandler := api.NewCameraHandler(camService)

	// Crypto Components (Phase 2.2)
	keyring := crypto.NewKeyring()
	if err := keyring.LoadFromEnv(); err != nil {
		log.Fatalf("Failed to initialize Keyring: %v", err)
	}
	credRepo := data.CredentialModel{DB: db}
	credService := cameras.NewCredentialService(credRepo, keyring, auditService)

	// Discovery Components (Phase 2.3)
	discRepo := &data.DiscoveryModel{DB: db}
	discService := discovery.NewService(discRepo, keyring, auditService)

	// Media Components (Phase 2.4)
	mediaRepo := &data.MediaModel{DB: db}
	// Note: CredService and OnvifClient used internally
	mediaService := cameras.NewMediaService(mediaRepo, &camRepo, credService, auditService)
	mediaHandler := api.NewMediaHandler(mediaService)

	// NVR Components (Phase 2.6)
	nvrRepo := data.NVRModel{DB: db}
	nvrService := nvr.NewService(&nvrRepo, keyring, auditService, camService)
	nvrHandler := api.NewNVRHandler(nvrService)

	// Health Components (Phase 2.5)
	healthRepo := &data.HealthModel{DB: db}
	healthProber := health.NewRTSPProber(credService)
	healthService := health.NewService(healthRepo, &nvrRepo, healthProber)
	healthHandler := api.NewHealthHandler(healthService)

	healthScheduler := health.NewScheduler(health.SchedulerConfig{}, healthService)
	healthScheduler.Start() // Starts background goroutine

	// RBAC Components
	blacklist := auth.NewRedisBlacklist(rdb)
	permModel := data.PermissionModel{DB: db}

	// Helper to load Config (Quick inline for phase 1.4)
	var rootCfg struct {
		RateLimit middleware.Config `yaml:"rate_limit"`
		Events    struct {
			Nvr nvr.PollerConfig `yaml:"nvr"`
		} `yaml:"events"`
	}
	cfgData, _ := os.ReadFile("config/default.yaml")
	_ = yaml.Unmarshal(cfgData, &rootCfg) // Error handling ignored for brevity in main

	limiter := ratelimit.NewLimiter(rdb, "stable-salt-val") // In prod use Env Var

	// Use Real Camera Resolver (camRepo implements it)
	permsMiddleware := middleware.NewPermissionMiddleware(permModel, camRepo)

	// --- Phase 2.10 NVR Events ---
	var nvrPoller *nvr.NVRPoller
	if rootCfg.Events.Nvr.Enabled {
		// Re-define a local config struct that matches yaml
		type NvrEventConfig struct {
			Enabled          bool   `yaml:"enabled"`
			PollIntervalMs   int    `yaml:"poll_interval_ms"`
			MaxInflight      int    `yaml:"max_inflight_nvrs"`
			MaxEventsPerPoll int    `yaml:"max_events_per_poll"`
			TimeBudgetMs     int    `yaml:"time_budget_ms"`
			BackoffMs        int    `yaml:"backoff_ms"`
			PublishRetryMax  int    `yaml:"publish_retry_max"`
			DedupTTLSeconds  int    `yaml:"dedup_ttl_seconds"`
			DedupMaxKeys     int    `yaml:"dedup_max_keys"`
			NatsSubject      string `yaml:"nats_subject"`
			SnapshotMode     string `yaml:"snapshot_mode"`
		}
		var rawEvtCfg struct {
			Events struct {
				Nvr NvrEventConfig `yaml:"nvr"`
			} `yaml:"events"`
		}
		_ = yaml.Unmarshal(cfgData, &rawEvtCfg) // Re-parse for safety config match
		c := rawEvtCfg.Events.Nvr

		// NATS Connection
		// Default to localhost:4222 for now or env
		natsURL := os.Getenv("NATS_URL")
		if natsURL == "" {
			natsURL = nats.DefaultURL
		}
		nc, err := nats.Connect(natsURL, nats.Name(serviceName))
		if err != nil {
			elog.Error(eventIDError, fmt.Sprintf("NATS Connect Failed: %v", err))
			log.Printf("Warning: NATS Connect Failed: %v. Event polling disabled.", err)
		} else {
			elog.Info(eventIDStart, "Connected to NATS")

			// Components
			pub := nvr.NewNATSPublisher(nc, c.NatsSubject, c.PublishRetryMax)
			enricher := nvr.NewEventEnricher(&nvrRepo)
			dedup := nvr.NewEventDedup(c.DedupMaxKeys, c.DedupTTLSeconds)

			// Poller
			pCfg := nvr.PollerConfig{
				Enabled:          c.Enabled,
				PollInterval:     time.Duration(c.PollIntervalMs) * time.Millisecond,
				MaxInflight:      c.MaxInflight,
				MaxEventsPerPoll: c.MaxEventsPerPoll,
				TimeBudget:       time.Duration(c.TimeBudgetMs) * time.Millisecond,
				Backoff:          time.Duration(c.BackoffMs) * time.Millisecond,
			}
			if pCfg.PollInterval == 0 {
				pCfg.PollInterval = 5 * time.Second
			}

			nvrPoller = nvr.NewNVRPoller(nvrService, pub, enricher, dedup, pCfg)
			nvrPoller.Start()
			elog.Info(eventIDStart, "NVR Event Poller Started")

			defer nc.Close()
		}
	}

	// Credential Handler (Phase 2.2)
	credHandler := api.NewCredentialHandler(credService, camService, permsMiddleware)

	// Discovery Handler (Phase 2.3)
	discHandler := api.NewDiscoveryHandler(discService, permsMiddleware)

	jwtMiddleware := middleware.NewJWTAuth(tokenMgr, blacklist)

	// Rate Limit Middleware
	rlMiddleware := middleware.NewRateLimitMiddleware(limiter, tokenMgr, rootCfg.RateLimit, rootCfg.RateLimit.Endpoints)

	// Audit Middleware
	auditMiddleware := middleware.NewAuditMiddleware(auditService)

	// Authorization
	authHandler := &api.AuthHandler{
		DB:      db,
		Tokens:  tokenMgr,
		Session: sessionMgr,
		Hasher:  auth.DefaultParams,
	}

	// Audit API Handler
	auditHandler := &api.AuditHandler{
		Service: auditService,
		Perms:   permsMiddleware,
	}

	// User Service (Phase 1.7)
	userRepo := data.UserModel{DB: db}
	userService := users.NewService(&userRepo, auditService, sessionMgr, tokenMgr)

	userHandler := &api.UserHandler{
		Service: userService,
	}

	winHandler := api.NewWindowsHandler()

	// License API Handler
	licenseHandler := &api.LicenseHandler{
		Manager: licenseManager,
	}

	// 4. Routes
	mux := http.NewServeMux()

	// Public Routes
	mux.HandleFunc("/api/v1/auth/login", authHandler.Login)
	mux.HandleFunc("/api/v1/auth/refresh", authHandler.Refresh)
	mux.HandleFunc("/api/v1/auth/logout", authHandler.Logout)
	mux.HandleFunc("/api/v1/auth/complete-reset", userHandler.CompleteReset)

	// Protected Routes Mux
	protectedMux := http.NewServeMux()

	// --- Phase 2.1 Camera Routes ---
	// CRUD
	// POST /cameras -> cameras.create (Site Scope needed in Body? Or Tenant Wide?)
	// Let's require "cameras.create" (tenant scope or site scope if supported later).
	// For now "tenant".
	protectedMux.Handle("POST /api/v1/cameras",
		permsMiddleware.RequirePermission("cameras.create", "tenant")(http.HandlerFunc(camHandler.Create)))

	protectedMux.Handle("GET /api/v1/cameras",
		permsMiddleware.RequirePermission("cameras.list", "tenant")(http.HandlerFunc(camHandler.List)))

	protectedMux.Handle("POST /api/v1/cameras/bulk",
		permsMiddleware.RequirePermission("cameras.manage", "tenant")(http.HandlerFunc(camHandler.Bulk)))

	// Enable/Disable
	protectedMux.Handle("POST /api/v1/cameras/{id}/enable",
		permsMiddleware.RequirePermission("cameras.manage", "tenant")(http.HandlerFunc(camHandler.Enable)))
	protectedMux.Handle("POST /api/v1/cameras/{id}/disable",
		permsMiddleware.RequirePermission("cameras.manage", "tenant")(http.HandlerFunc(camHandler.Disable)))

	// --- Existing Routes ---

	// Debug
	debugHandler := permsMiddleware.RequirePermission("debug.view", "tenant")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac, _ := middleware.GetAuthContext(r.Context())
		fmt.Fprintf(w, "Hello Tenant:%s User:%s", ac.TenantID, ac.UserID)
	}))
	protectedMux.Handle("GET /api/v1/debug/me", debugHandler)

	// Audit
	protectedMux.Handle("GET /api/v1/audit/events",
		permsMiddleware.RequirePermission("audit.read", "tenant")(http.HandlerFunc(auditHandler.GetEvents)))
	protectedMux.Handle("POST /api/v1/audit/exports",
		permsMiddleware.RequirePermission("audit.export", "tenant")(http.HandlerFunc(auditHandler.ExportEvents)))

	// License
	protectedMux.Handle("GET /api/v1/license/status",
		permsMiddleware.RequirePermission("license.read", "tenant")(http.HandlerFunc(licenseHandler.GetStatus)))
	protectedMux.Handle("POST /api/v1/license/reload",
		permsMiddleware.RequirePermission("license.manage", "tenant")(http.HandlerFunc(licenseHandler.Reload)))

	// Users
	protectedMux.Handle("GET /api/v1/users/{id}",
		permsMiddleware.RequirePermission("user.read", "tenant")(http.HandlerFunc(userHandler.GetUser)))
	protectedMux.Handle("POST /api/v1/users",
		permsMiddleware.RequirePermission("user.create", "tenant")(http.HandlerFunc(userHandler.CreateUser)))
	protectedMux.Handle("POST /api/v1/users/{id}/disable",
		permsMiddleware.RequirePermission("user.disable", "tenant")(http.HandlerFunc(userHandler.DisableUser)))
	protectedMux.Handle("POST /api/v1/users/{id}/reset-password",
		permsMiddleware.RequirePermission("user.password.reset", "tenant")(http.HandlerFunc(userHandler.ResetPassword)))
	protectedMux.Handle("PUT /api/v1/users/{id}/roles",
		permsMiddleware.RequirePermission("user.role.assign", "tenant")(http.HandlerFunc(userHandler.AssignRole)))

	// Mount Protected
	mux.Handle("/api/v1/", jwtMiddleware.Middleware(protectedMux)) // Mounts all protected at /api/v1/ (path matching handles specific)
	// Note: Standard Mux prefix matching.
	// If we mount /api/v1/protected/..., we need strip prefix.
	// But our routes above are full paths `/api/v1/...`.
	// So we mount "/" ?? No, that would intercept public.
	// We can't easily mix public/private under same /api/v1 prefix with standard mux if we want ONE middleware wrapping some.
	// We have to explicitly wrap handlers or use subroutines.
	// The pattern used previously was `mux.Handle("/api/v1/protected/", ...)`
	// But our routes are `/api/v1/cameras`.
	// Let's wrap the Mux itself?
	// `jwtMiddleware.Middleware(protectedMux)` returns a Handler.
	// But `protectedMux` has routes `/api/v1/cameras`.
	// If we mount that at `/`, it works.
	// EXCEPT public routes are also on `mux`.
	// `mux` is the root.
	// `mux.Handle("/api/v1/cameras", jwtMiddleware.Middleware(cameraHandler))`?
	// Too tedious.
	// Let's make `HandlerFromMux` approach.
	// We'll trust the previous pattern or fix it.
	// Previous: `mux.Handle("/api/v1/protected/", ...)`
	// This implies an explicit URL structure.
	// But requirements usually say `/api/v1/cameras`.
	// So we should wrap INDIVIDUAL routes or Groups.

	// FIX: We can't mount specific paths easily behind middleware in one block in StdLib without sub-tree.
	// Sub-tree `/api/v1/` would cover everything including Auth.
	// So we manually wrap the Protected Handlers?
	// Or we use a helper `Protect(h)`.
	// Let's use a helper for cleaner Main.
	Protect := func(h http.Handler) http.Handler { return jwtMiddleware.Middleware(h) }

	mux.Handle("POST /api/v1/cameras", Protect(permsMiddleware.RequirePermission("cameras.create", "tenant")(http.HandlerFunc(camHandler.Create))))
	mux.Handle("GET /api/v1/cameras", Protect(permsMiddleware.RequirePermission("cameras.list", "tenant")(http.HandlerFunc(camHandler.List))))
	mux.Handle("POST /api/v1/cameras/bulk", Protect(permsMiddleware.RequirePermission("cameras.manage", "tenant")(http.HandlerFunc(camHandler.Bulk))))
	mux.Handle("POST /api/v1/cameras/{id}/enable", Protect(permsMiddleware.RequirePermission("cameras.manage", "tenant")(http.HandlerFunc(camHandler.Enable))))
	mux.Handle("POST /api/v1/cameras/{id}/disable", Protect(permsMiddleware.RequirePermission("cameras.manage", "tenant")(http.HandlerFunc(camHandler.Disable))))

	// Credentials (Phase 2.2)
	mux.Handle("PUT /api/v1/cameras/{id}/credentials", Protect(http.HandlerFunc(credHandler.Update)))
	mux.Handle("GET /api/v1/cameras/{id}/credentials", Protect(http.HandlerFunc(credHandler.Get)))
	mux.Handle("DELETE /api/v1/cameras/{id}/credentials", Protect(http.HandlerFunc(credHandler.Delete)))

	// Discovery (Phase 2.3)
	mux.Handle("POST /api/v1/onvif/credentials", Protect(http.HandlerFunc(discHandler.CreateCredential)))
	mux.Handle("POST /api/v1/onvif/discovery-runs", Protect(http.HandlerFunc(discHandler.StartRun)))
	mux.Handle("GET /api/v1/onvif/discovery-runs/{id}", Protect(http.HandlerFunc(discHandler.GetRun)))
	mux.Handle("GET /api/v1/onvif/discovered-devices", Protect(http.HandlerFunc(discHandler.ListDevices)))
	mux.Handle("POST /api/v1/onvif/discovered-devices/{id}/probe", Protect(http.HandlerFunc(discHandler.ProbeDevice)))

	// Media (Phase 2.4)
	mux.Handle("GET /api/v1/cameras/{id}/media-profiles", Protect(permsMiddleware.RequirePermission("camera.media.read", "tenant")(http.HandlerFunc(mediaHandler.ListProfiles))))
	mux.Handle("POST /api/v1/cameras/{id}/select-media-profiles", Protect(permsMiddleware.RequirePermission("camera.media.select", "tenant")(http.HandlerFunc(mediaHandler.SelectProfiles))))
	mux.Handle("GET /api/v1/cameras/{id}/media-selection", Protect(permsMiddleware.RequirePermission("camera.media.read", "tenant")(http.HandlerFunc(mediaHandler.GetSelection))))
	mux.Handle("POST /api/v1/cameras/{id}/validate-rtsp", Protect(permsMiddleware.RequirePermission("camera.media.validate", "tenant")(http.HandlerFunc(mediaHandler.ValidateRTSP))))

	// Health (Phase 2.5)
	// Permissions:
	// camera.health.read (List, Get, History)
	// alerts.read (List Alerts)
	// camera.health.recheck (Manual)

	mux.Handle("GET /api/v1/cameras/health", Protect(permsMiddleware.RequirePermission("camera.health.read", "tenant")(http.HandlerFunc(healthHandler.GetHealth))))
	mux.Handle("GET /api/v1/cameras/{id}/health", Protect(permsMiddleware.RequirePermission("camera.health.read", "tenant")(http.HandlerFunc(healthHandler.GetCameraHealth))))
	mux.Handle("GET /api/v1/cameras/{id}/health/history", Protect(permsMiddleware.RequirePermission("camera.health.read", "tenant")(http.HandlerFunc(healthHandler.GetHistory))))
	mux.Handle("GET /api/v1/alerts/cameras", Protect(permsMiddleware.RequirePermission("alerts.read", "tenant")(http.HandlerFunc(healthHandler.ListAlerts))))
	mux.Handle("POST /api/v1/cameras/{id}/health-recheck", Protect(permsMiddleware.RequirePermission("camera.health.recheck", "tenant")(http.HandlerFunc(healthHandler.ManualRecheck))))

	// NVR Routes (Phase 2.6)
	// CRUD
	mux.Handle("POST /api/v1/nvrs", Protect(permsMiddleware.RequirePermission("nvr.write", "tenant")(http.HandlerFunc(nvrHandler.Create))))
	mux.Handle("GET /api/v1/nvrs", Protect(permsMiddleware.RequirePermission("nvr.read", "tenant")(http.HandlerFunc(nvrHandler.List))))
	mux.Handle("GET /api/v1/nvrs/{id}", Protect(permsMiddleware.RequirePermission("nvr.read", "tenant")(http.HandlerFunc(nvrHandler.Get))))
	mux.Handle("PUT /api/v1/nvrs/{id}", Protect(permsMiddleware.RequirePermission("nvr.write", "tenant")(http.HandlerFunc(nvrHandler.Update))))
	mux.Handle("DELETE /api/v1/nvrs/{id}", Protect(permsMiddleware.RequirePermission("nvr.delete", "tenant")(http.HandlerFunc(nvrHandler.Delete))))

	// Linking
	mux.Handle("PUT /api/v1/nvrs/{id}/cameras", Protect(permsMiddleware.RequirePermission("nvr.link.write", "tenant")(http.HandlerFunc(nvrHandler.UpsertLinks))))
	mux.Handle("GET /api/v1/nvrs/{id}/cameras", Protect(permsMiddleware.RequirePermission("nvr.link.read", "tenant")(http.HandlerFunc(nvrHandler.ListLinks))))
	mux.Handle("DELETE /api/v1/nvrs/{id}/cameras", Protect(permsMiddleware.RequirePermission("nvr.link.write", "tenant")(http.HandlerFunc(nvrHandler.Unlink))))

	// NVR Credentials
	mux.Handle("PUT /api/v1/nvrs/{id}/credentials", Protect(permsMiddleware.RequirePermission("nvr.credential.write", "tenant")(http.HandlerFunc(nvrHandler.SetCredentials))))
	mux.Handle("GET /api/v1/nvrs/{id}/credentials", Protect(permsMiddleware.RequirePermission("nvr.credential.read", "tenant")(http.HandlerFunc(nvrHandler.GetCredentials))))
	mux.Handle("DELETE /api/v1/nvrs/{id}/credentials", Protect(permsMiddleware.RequirePermission("nvr.credential.delete", "tenant")(http.HandlerFunc(nvrHandler.DeleteCredentials))))

	// NVR Adapter Routes (Phase 2.7)
	mux.Handle("GET /api/v1/nvrs/{id}/adapter/device-info", Protect(permsMiddleware.RequirePermission("nvr.adapter.read", "tenant")(http.HandlerFunc(nvrHandler.GetAdapterDeviceInfo))))
	mux.Handle("GET /api/v1/nvrs/{id}/adapter/channels", Protect(permsMiddleware.RequirePermission("nvr.adapter.read", "tenant")(http.HandlerFunc(nvrHandler.GetAdapterChannels))))
	mux.Handle("GET /api/v1/nvrs/{id}/adapter/events", Protect(permsMiddleware.RequirePermission("nvr.adapter.read", "tenant")(http.HandlerFunc(nvrHandler.GetAdapterEvents))))

	// NVR Discovery (Phase 2.8)
	// NVR Discovery (Phase 2.8)
	mux.Handle("POST /api/v1/nvrs/{id}/test-connection", Protect(permsMiddleware.RequirePermission("nvr.adapter.probe", "tenant")(http.HandlerFunc(nvrHandler.TestConnection))))
	mux.Handle("POST /api/v1/nvrs/{id}/discover-channels", Protect(permsMiddleware.RequirePermission("nvr.discovery.run", "tenant")(http.HandlerFunc(nvrHandler.DiscoverChannels))))
	mux.Handle("GET /api/v1/nvrs/{id}/channels", Protect(permsMiddleware.RequirePermission("nvr.discovery.read", "tenant")(http.HandlerFunc(nvrHandler.GetChannels))))
	mux.Handle("POST /api/v1/nvrs/{id}/validate-channels", Protect(permsMiddleware.RequirePermission("nvr.discovery.validate", "tenant")(http.HandlerFunc(nvrHandler.ValidateChannels))))
	mux.Handle("POST /api/v1/nvrs/{id}/provision-cameras", Protect(permsMiddleware.RequirePermission("nvr.link.write", "tenant")(http.HandlerFunc(nvrHandler.ProvisionCameras))))
	mux.Handle("POST /api/v1/nvrs/{id}/channels/bulk", Protect(permsMiddleware.RequirePermission("nvr.channel.write", "tenant")(http.HandlerFunc(nvrHandler.BulkChannelOp))))

	// Start NVR Scheduler
	nvrService.StartDailySync(context.Background())

	// NVR Monitor (Phase 2.9)
	nvrMonitor := nvr.NewMonitor(nvrService, &nvrRepo)
	nvrMonitor.Start(context.Background())

	// NVR Health API
	mux.Handle("GET /api/v1/health/nvrs/summary", Protect(permsMiddleware.RequirePermission("nvr.health.read", "tenant")(http.HandlerFunc(nvrHandler.GetNVRHealthSummary))))
	mux.Handle("GET /api/v1/health/nvrs/{id}/channels", Protect(permsMiddleware.RequirePermission("nvr.health.read", "tenant")(http.HandlerFunc(nvrHandler.GetNVRChannelHealth))))

	// Groups
	mux.Handle("POST /api/v1/camera-groups", Protect(permsMiddleware.RequirePermission("cameras.manage", "tenant")(http.HandlerFunc(camHandler.CreateGroup))))
	mux.Handle("GET /api/v1/camera-groups", Protect(permsMiddleware.RequirePermission("cameras.list", "tenant")(http.HandlerFunc(camHandler.ListGroups))))
	mux.Handle("DELETE /api/v1/camera-groups/{id}", Protect(permsMiddleware.RequirePermission("cameras.manage", "tenant")(http.HandlerFunc(camHandler.DeleteGroup))))
	mux.Handle("PUT /api/v1/camera-groups/{id}/members", Protect(permsMiddleware.RequirePermission("cameras.manage", "tenant")(http.HandlerFunc(camHandler.SetGroupMembers))))

	// Re-map existing protected routes manually too to ensure they are at /api/v1/...
	mux.Handle("GET /api/v1/debug/me", Protect(debugHandler))

	mux.Handle("GET /api/v1/audit/events", Protect(permsMiddleware.RequirePermission("audit.read", "tenant")(http.HandlerFunc(auditHandler.GetEvents))))
	mux.Handle("POST /api/v1/audit/exports", Protect(permsMiddleware.RequirePermission("audit.export", "tenant")(http.HandlerFunc(auditHandler.ExportEvents))))

	mux.Handle("GET /api/v1/license/status", Protect(permsMiddleware.RequirePermission("license.read", "tenant")(http.HandlerFunc(licenseHandler.GetStatus))))
	mux.Handle("POST /api/v1/license/reload", Protect(permsMiddleware.RequirePermission("license.manage", "tenant")(http.HandlerFunc(licenseHandler.Reload))))

	mux.Handle("GET /api/v1/users/{id}", Protect(permsMiddleware.RequirePermission("user.read", "tenant")(http.HandlerFunc(userHandler.GetUser))))
	mux.Handle("POST /api/v1/users", Protect(permsMiddleware.RequirePermission("user.create", "tenant")(http.HandlerFunc(userHandler.CreateUser))))
	mux.Handle("POST /api/v1/users/{id}/disable", Protect(permsMiddleware.RequirePermission("user.disable", "tenant")(http.HandlerFunc(userHandler.DisableUser))))
	mux.Handle("POST /api/v1/users/{id}/reset-password", Protect(permsMiddleware.RequirePermission("user.password.reset", "tenant")(http.HandlerFunc(userHandler.ResetPassword))))
	mux.Handle("PUT /api/v1/users/{id}/roles", Protect(permsMiddleware.RequirePermission("user.role.assign", "tenant")(http.HandlerFunc(userHandler.AssignRole))))

	// Windows-Specific (Phase 2.11)
	mux.Handle("POST /api/v1/windows/discovery:scan", Protect(permsMiddleware.RequirePermission("admin.discovery.run", "tenant")(http.HandlerFunc(winHandler.WindowsDiscoveryHandler))))

	// Wrap TOP Level Mux with Global Rate Limiter -> Audit Logger
	// Order: RateLimit -> Audit (Log accepted requests) -> Mux
	// If RateLimit blocks, Audit middleware won't run (or should it? Usually no, unless we audit blocks too.
	// Prompt says "Log all mutating requests". If blocked 429, it never reached mux.
	// But `finalHandler` wraps mux.
	// Correct chain: RateLimit (outer) -> Audit (inner) -> Mux.
	// If RateLimit blocks, it returns early. Audit assumes request reached app logic.
	// If user wants to audit ratelimits, audit should be OUTER.
	// Prompt D: "Automatic HTTP Request Logging... Log all mutating...".
	// Usually audit successful or app-level failures.
	// Let's put Audit right before Mux (after Rate Limit).

	// RateLimit -> Audit -> Mux
	auditWrappedMux := auditMiddleware.LogRequest(mux)
	finalHandler := rlMiddleware.GlobalLimiter(auditWrappedMux)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting server on :%s", port)
	server := &http.Server{
		Addr:    ":" + port,
		Handler: finalHandler,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			elog.Error(eventIDError, fmt.Sprintf("HTTP server error: %v", err))
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Wait for stop signal (Service Stop or Interrupt)
	// For simplicity in this phase, we wait on stopChan if in service mode,
	// or block indefinitely (or handle signals) in console mode.
	if isService {
		<-stopChan
		elog.Info(eventIDStop, "Service stop requested")
	} else {
		// In console mode, we could handle Ctrl+C here if desired,
		// but standard log.Fatal on server start is fine for now.
		select {}
	}

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	healthScheduler.Stop()
	if nvrPoller != nil {
		nvrPoller.Stop()
	}

	if err := server.Shutdown(ctx); err != nil {
		elog.Error(eventIDError, fmt.Sprintf("Graceful shutdown error: %v", err))
	}
	elog.Info(eventIDStop, "Server stopped gracefully")
}
