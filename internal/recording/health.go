package recording

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/technosupport/ts-vms/internal/middleware"
)

type HealthServer struct {
	config     *Config
	supervisor *RecordingArchiverService
	scheduler  *ScheduleEngine
	store      *PostgresStore
}

func NewHealthServer(cfg *Config, sup *RecordingArchiverService, sched *ScheduleEngine, store *PostgresStore) *HealthServer {
	return &HealthServer{config: cfg, supervisor: sup, scheduler: sched, store: store}
}

func (h *HealthServer) Start() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", h.handleHealth)
	mux.HandleFunc("/readyz", h.handleReady)
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(h.supervisor.GetStatus())
	})
	mux.Handle("/api/v1/recording/trigger", h.rbacMiddleware(http.HandlerFunc(h.handleTrigger)))
	go http.ListenAndServe(fmt.Sprintf(":%d", h.config.Global.HealthPort), mux)
}

func (h *HealthServer) handleTrigger(w http.ResponseWriter, r *http.Request) {
	camID := r.URL.Query().Get("camera_id")
	if camID == "" {
		http.Error(w, "missing camera_id", http.StatusBadRequest)
		return
	}
	h.scheduler.TriggerEvent(camID, h.config.Global.SegmentDurationSec)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (h *HealthServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := map[string]any{
		"db":      "unknown",
		"storage": "unknown",
		"state":   h.supervisor.GetStatus(),
	}
	code := http.StatusOK

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if h.store == nil || !h.store.Available() {
		status["db"] = "disabled"
	} else if h.store.PingContext(ctx) == nil {
		status["db"] = "ok"
	} else {
		status["db"] = "down"
		code = http.StatusServiceUnavailable
	}

	if _, err := os.Stat(h.config.Global.StorageRoot); err == nil {
		status["storage"] = "ok"
	} else {
		status["storage"] = err.Error()
		code = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(status)
}

func (h *HealthServer) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if h.config.FailoverRecovery.DBRequiredForReady && (h.store == nil || h.store.PingContext(ctx) != nil) {
		http.Error(w, "db not ready", http.StatusServiceUnavailable)
		return
	}
	if _, err := os.Stat(h.config.Global.StorageRoot); err != nil {
		http.Error(w, "storage not ready", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ready":true}`))
}

func (h *HealthServer) rbacMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.config.Global.DevRbacBypass {
			next.ServeHTTP(w, r)
			return
		}
		ac, ok := middleware.GetAuthContext(r.Context())
		if !ok || (!ac.HasPermission("recording.manage") && !ac.HasRole("admin")) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
