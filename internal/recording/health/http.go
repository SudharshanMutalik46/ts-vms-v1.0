package health

import (
	"encoding/json"
	"net/http"
)

// StatusHandler exposes the GlobalStatus over HTTP
func StatusHandler(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := m.GetGlobalStatus()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	}
}

// LivenessHandler always returns 200 OK if the HTTP server can accept requests
func LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}
}

// ReadinessState provides dynamic variables for readiness evaluation
type ReadinessState struct {
	IsDBConnected      func() bool
	IsBreakerEngaged   func() bool
	DBRequiredForReady bool
}

// ReadinessHandler evaluates dependencies before signaling readiness
func ReadinessHandler(state ReadinessState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if state.IsBreakerEngaged != nil && state.IsBreakerEngaged() {
			http.Error(w, "Not Ready: Storage Circuit Breaker Engaged", http.StatusServiceUnavailable)
			return
		}

		if state.DBRequiredForReady && state.IsDBConnected != nil && !state.IsDBConnected() {
			http.Error(w, "Not Ready: Database Unavailable", http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("READY"))
	}
}
