package recording

import (
	"encoding/json"
	"net/http"
	"strings"
)

type InternalAPI struct {
	ServiceKey string
	Supervisor *SupervisorExt
}

func (api *InternalAPI) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Service-Key") != api.ServiceKey {
			http.Error(w, "Unauthorized Internal API", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (api *InternalAPI) actionHandler(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) < 5 {
			http.Error(w, "Bad path", 400)
			return
		}
		camID := parts[4]

		api.Supervisor.ApplyManualState(camID, action)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "camera_id": camID, "action": action})
	}
}

func (api *InternalAPI) bulkHandler(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		api.Supervisor.BulkAction(action)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "bulk_action": action})
	}
}

func (api *InternalAPI) ServeMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/recording/cameras/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/start") {
			api.authMiddleware(api.actionHandler("START"))(w, r)
		} else if strings.HasSuffix(r.URL.Path, "/stop") {
			api.authMiddleware(api.actionHandler("STOP"))(w, r)
		} else if strings.HasSuffix(r.URL.Path, "/pause") {
			api.authMiddleware(api.actionHandler("PAUSE"))(w, r)
		} else if strings.HasSuffix(r.URL.Path, "/resume") {
			api.authMiddleware(api.actionHandler("RESUME"))(w, r)
		}
	})
	mux.HandleFunc("/internal/recording/start-all", api.authMiddleware(api.bulkHandler("START")))
	mux.HandleFunc("/internal/recording/stop-all", api.authMiddleware(api.bulkHandler("STOP")))
	mux.HandleFunc("/internal/recording/reload-schedules", api.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		api.Supervisor.ReloadSchedules(nil)
		w.WriteHeader(200)
	}))
	// Map public status internally as well
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.Supervisor.GetStatus())
	})
	return mux
}
