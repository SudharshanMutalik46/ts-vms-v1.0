package recording

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type HealthServer struct {
	config     *Config
	supervisor *Supervisor
	scheduler  *ScheduleEngine
}

func NewHealthServer(cfg *Config, sup *Supervisor, sched *ScheduleEngine) *HealthServer {
	return &HealthServer{config: cfg, supervisor: sup, scheduler: sched}
}

func (h *HealthServer) Start() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })

	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(h.supervisor.GetStatus())
	})

	// RBAC Protected Trigger Endpoint
	mux.Handle("/api/v1/recording/trigger", h.rbacMiddleware(http.HandlerFunc(h.handleTrigger)))

	addr := fmt.Sprintf(":%d", h.config.Global.HealthPort)
	go http.ListenAndServe(addr, mux)
}

func (h *HealthServer) handleTrigger(w http.ResponseWriter, r *http.Request) {
	camID := r.URL.Query().Get("camera_id")
	if camID == "" {
		http.Error(w, "missing camera_id", 400)
		return
	}
	h.scheduler.TriggerEvent(camID, h.config.Global.SegmentDurationSec)
	w.WriteHeader(200)
	w.Write([]byte("Event triggered successfully"))
}

// rbacMiddleware enforces the recording.manage claim
func (h *HealthServer) rbacMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.config.Global.DevRbacBypass {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		// Stub check: In production, parse JWT and check claims map for `recording.manage`
		if !strings.Contains(authHeader, "recording.manage") {
			http.Error(w, "Forbidden: Missing recording.manage permission", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
