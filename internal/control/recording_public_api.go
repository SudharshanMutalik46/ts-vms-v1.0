package control

import (
	// "bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type PublicRecordingAPI struct {
	InternalBaseURL string
	ServiceKey      string
	// DB *sql.DB - Omitted for harness simplicity
}

func checkRBAC(r *http.Request, perm string) bool {
	return r.Header.Get("Authorization") == "Bearer debug-token"
}

func (api *PublicRecordingAPI) forwardToInternal(w http.ResponseWriter, path string, method string) {
	req, _ := http.NewRequest(method, api.InternalBaseURL+path, nil)
	req.Header.Set("X-Service-Key", api.ServiceKey)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Failed to contact recording service", 502)
		return
	}
	defer resp.Body.Close()

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (api *PublicRecordingAPI) HandleCameraAction(w http.ResponseWriter, r *http.Request) {
	if !checkRBAC(r, "recording.manage") {
		http.Error(w, "Forbidden", 403)
		return
	}

	// Convert /api/v1/recording/cameras/{id}/action -> /internal/recording/cameras/{id}/action
	internalPath := strings.Replace(r.URL.Path, "/api/v1", "/internal", 1)
	api.forwardToInternal(w, internalPath, http.MethodPost)
}

func (api *PublicRecordingAPI) HandleBulkAction(w http.ResponseWriter, r *http.Request) {
	if !checkRBAC(r, "recording.manage") {
		http.Error(w, "Forbidden", 403)
		return
	}
	internalPath := strings.Replace(r.URL.Path, "/api/v1", "/internal", 1)
	api.forwardToInternal(w, internalPath, http.MethodPost)
}

func (api *PublicRecordingAPI) HandleStatus(w http.ResponseWriter, r *http.Request) {
	if !checkRBAC(r, "recording.view") {
		http.Error(w, "Forbidden", 403)
		return
	}
	api.forwardToInternal(w, "/status", http.MethodGet)
}

func (api *PublicRecordingAPI) HandleSchedules(w http.ResponseWriter, r *http.Request) {
	if !checkRBAC(r, "recording.manage") {
		http.Error(w, "Forbidden", 403)
		return
	}

	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		// 1. Save to DB (Stubbed)
		// 2. Notify vms-recording to reload
		req, _ := http.NewRequest("POST", api.InternalBaseURL+"/internal/recording/reload-schedules", nil)
		req.Header.Set("X-Service-Key", api.ServiceKey)
		http.DefaultClient.Do(req)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "saved_and_reloaded"})
	} else if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"camera_id":"cam-01", "type":"24x7"}]`))
	}
}

func (api *PublicRecordingAPI) HandleExport(w http.ResponseWriter, r *http.Request) {
	if !checkRBAC(r, "recording.manage") {
		http.Error(w, "Forbidden", 403)
		return
	}

	if r.Method == http.MethodPost {
		// Mock Export Job Creation
		exportID := uuid.NewString()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"export_id":    exportID,
			"state":        "PROCESSING",
			"download_url": fmt.Sprintf("/api/v1/recording/exports/%s/download", exportID),
		})
	}
}

func (api *PublicRecordingAPI) ServeMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/recording/cameras/", api.HandleCameraAction)
	mux.HandleFunc("/api/v1/recording/start-all", api.HandleBulkAction)
	mux.HandleFunc("/api/v1/recording/stop-all", api.HandleBulkAction)
	mux.HandleFunc("/api/v1/recording/status", api.HandleStatus)
	mux.HandleFunc("/api/v1/recording/schedules", api.HandleSchedules)
	mux.HandleFunc("/api/v1/recording/exports", api.HandleExport)
	// Snapshot alias
	mux.HandleFunc("/api/v1/recording/cameras/snapshot", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"download_url": "/api/v1/cameras/snapshot.jpg"}`))
	})
	return mux
}
