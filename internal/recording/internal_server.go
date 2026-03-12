package recording

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type InternalAPI struct {
	ServiceKey        string
	RecordingArchiver *RecordingArchiverService
	ScheduleStore     *PostgresStore
}

func (api *InternalAPI) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Service-Key") != api.ServiceKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (api *InternalAPI) actionHandler(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) < 5 {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		camID := parts[4]
		_ = api.RecordingArchiver.ApplyManualState(camID, action)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "camera_id": camID, "action": action})
	}
}

func (api *InternalAPI) syncCameraHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	camID := parts[4]

	var req struct {
		RtspURL string `json:"rtsp_url"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Enabled && strings.TrimSpace(req.RtspURL) == "" {
		http.Error(w, "rtsp_url required for enabled camera", http.StatusBadRequest)
		return
	}

	api.RecordingArchiver.UpsertCamera(CameraConfig{
		ID:      camID,
		RtspURL: strings.TrimSpace(req.RtspURL),
		Enabled: req.Enabled,
	})
	action := "STOP"
	if req.Enabled {
		action = "START"
	}
	if err := api.RecordingArchiver.ApplyManualState(camID, action); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":    "ok",
		"camera_id": camID,
		"enabled":   req.Enabled,
		"action":    action,
	})
}

func (api *InternalAPI) deleteCameraHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	camID := parts[4]
	api.RecordingArchiver.RemoveCamera(camID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "camera_id": camID, "action": "DELETE"})
}

func (api *InternalAPI) bulkHandler(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		api.RecordingArchiver.BulkAction(action)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "bulk_action": action})
	}
}

func (api *InternalAPI) attachCameraHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var cam CameraConfig
	if err := json.NewDecoder(r.Body).Decode(&cam); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := api.RecordingArchiver.AttachCamera(cam); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":              "ok",
		"camera_id":           cam.ID,
		"enabled":             cam.Enabled,
		"auto_record_started": cam.Enabled,
	})
}

func (api *InternalAPI) ServeMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/recording/cameras/attach", api.authMiddleware(api.attachCameraHandler))
	mux.HandleFunc("/internal/recording/cameras/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/sync"):
			api.authMiddleware(api.syncCameraHandler)(w, r)
		case strings.HasSuffix(r.URL.Path, "/delete"):
			api.authMiddleware(api.deleteCameraHandler)(w, r)
		case strings.HasSuffix(r.URL.Path, "/start"):
			api.authMiddleware(api.actionHandler("START"))(w, r)
		case strings.HasSuffix(r.URL.Path, "/stop"):
			api.authMiddleware(api.actionHandler("STOP"))(w, r)
		case strings.HasSuffix(r.URL.Path, "/pause"):
			api.authMiddleware(api.actionHandler("PAUSE"))(w, r)
		case strings.HasSuffix(r.URL.Path, "/resume"):
			api.authMiddleware(api.actionHandler("RESUME"))(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/internal/recording/start-all", api.authMiddleware(api.bulkHandler("START")))
	mux.HandleFunc("/internal/recording/stop-all", api.authMiddleware(api.bulkHandler("STOP")))

	mux.HandleFunc("/internal/recording/reload-schedules", api.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if api.ScheduleStore == nil || !api.ScheduleStore.Available() {
			http.Error(w, "recording database unavailable", http.StatusServiceUnavailable)
			return
		}
		cfgs, err := api.ScheduleStore.LoadSchedules(context.Background())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		api.RecordingArchiver.ReloadSchedules(cfgs)
		w.WriteHeader(http.StatusOK)
	}))

	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(api.RecordingArchiver.GetStatus())
	})
	return mux
}
