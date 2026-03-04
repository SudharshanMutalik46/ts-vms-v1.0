package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/technosupport/ts-vms/internal/middleware"
	"github.com/technosupport/ts-vms/internal/recording"
)

type PublicRecordingAPI struct {
	InternalBaseURL string
	ServiceKey      string
	DB              *recording.PostgresStore
	Exporter        *recording.ExportService
	DefaultTenantID string
	DefaultSiteID   string
}

func requirePermission(r *http.Request, perm string) bool {
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		return false
	}
	return ac.HasPermission(perm) || ac.HasRole("admin")
}

func userIDFromContext(ctx context.Context) string {
	ac, ok := middleware.GetAuthContext(ctx)
	if !ok {
		return "system"
	}
	return ac.UserID
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, recording.ErrDBUnavailable) {
		http.Error(w, "recording database unavailable", http.StatusServiceUnavailable)
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func (api *PublicRecordingAPI) forwardToInternal(w http.ResponseWriter, path string, method string) {
	req, _ := http.NewRequest(method, api.InternalBaseURL+path, nil)
	req.Header.Set("X-Service-Key", api.ServiceKey)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "failed to contact recording service", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (api *PublicRecordingAPI) forwardToInternalWithBody(w http.ResponseWriter, r *http.Request, path string, method string) {
	req, _ := http.NewRequest(method, api.InternalBaseURL+path, r.Body)
	req.Header.Set("X-Service-Key", api.ServiceKey)
	req.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "failed to contact recording service", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (api *PublicRecordingAPI) HandleAttachCamera(w http.ResponseWriter, r *http.Request) {
	if !requirePermission(r, "recording.manage") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	api.forwardToInternalWithBody(w, r, "/internal/recording/cameras/attach", http.MethodPost)
}

func (api *PublicRecordingAPI) HandleCameraAction(w http.ResponseWriter, r *http.Request) {
	if !requirePermission(r, "recording.manage") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	internalPath := strings.Replace(r.URL.Path, "/api/v1", "/internal", 1)
	api.forwardToInternal(w, internalPath, http.MethodPost)
}

func (api *PublicRecordingAPI) HandleBulkAction(w http.ResponseWriter, r *http.Request) {
	if !requirePermission(r, "recording.manage") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	internalPath := strings.Replace(r.URL.Path, "/api/v1", "/internal", 1)
	api.forwardToInternal(w, internalPath, http.MethodPost)
}

func (api *PublicRecordingAPI) HandleStatus(w http.ResponseWriter, r *http.Request) {
	if !requirePermission(r, "recording.view") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	api.forwardToInternal(w, "/status", http.MethodGet)
}

func (api *PublicRecordingAPI) HandleSchedules(w http.ResponseWriter, r *http.Request) {
	if !requirePermission(r, "recording.manage") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		cfgs, err := api.DB.LoadSchedules(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cfgs)
	case http.MethodPost, http.MethodPut:
		var cfg recording.ScheduleConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := api.DB.SaveSchedule(r.Context(), api.DefaultTenantID, api.DefaultSiteID, cfg); err != nil {
			writeStoreError(w, err)
			return
		}
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, api.InternalBaseURL+"/internal/recording/reload-schedules", nil)
		req.Header.Set("X-Service-Key", api.ServiceKey)
		_, _ = http.DefaultClient.Do(req)
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved_and_reloaded"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *PublicRecordingAPI) HandleExport(w http.ResponseWriter, r *http.Request) {
	if !requirePermission(r, "export.manage") && !requirePermission(r, "recording.manage") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodPost:
		var req recording.ExportRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req.UserID = userIDFromContext(r.Context())
		job, err := api.Exporter.QueueExport(r.Context(), req)
		if err != nil {
			if errors.Is(err, recording.ErrDBUnavailable) {
				http.Error(w, "recording database unavailable", http.StatusServiceUnavailable)
			} else {
				http.Error(w, err.Error(), http.StatusBadRequest)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"export_id":    job.ID,
			"state":        job.State,
			"download_url": "/api/v1/recording/exports/" + job.ID + "/download",
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *PublicRecordingAPI) HandleExportDownload(w http.ResponseWriter, r *http.Request) {
	if !requirePermission(r, "export.download") && !requirePermission(r, "export.manage") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	exportID := strings.TrimPrefix(r.URL.Path, "/api/v1/recording/exports/")
	exportID = strings.TrimSuffix(exportID, "/download")
	f, job, err := api.DB.OpenExportArtifact(r.Context(), exportID)
	if err != nil {
		if errors.Is(err, recording.ErrDBUnavailable) {
			http.Error(w, "recording database unavailable", http.StatusServiceUnavailable)
		} else {
			http.Error(w, "not found", http.StatusNotFound)
		}
		return
	}
	if job == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	ext := strings.ToLower(filepath.Ext(job.OutputPath))
	if ext == "" {
		ext = ".mkv"
	}
	contentType := "application/octet-stream"
	switch ext {
	case ".mp4":
		contentType = "video/mp4"
	case ".mkv":
		contentType = "video/x-matroska"
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\""+job.ID+ext+"\"")
	w.Header().Set("Content-Type", contentType)
	_, _ = io.Copy(w, f)
}

func (api *PublicRecordingAPI) ServeMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/recording/cameras/attach", api.HandleAttachCamera)
	mux.HandleFunc("/api/v1/recording/cameras/", api.HandleCameraAction)
	mux.HandleFunc("/api/v1/recording/start-all", api.HandleBulkAction)
	mux.HandleFunc("/api/v1/recording/stop-all", api.HandleBulkAction)
	mux.HandleFunc("/api/v1/recording/status", api.HandleStatus)
	mux.HandleFunc("/api/v1/recording/schedules", api.HandleSchedules)
	mux.HandleFunc("/api/v1/recording/exports", api.HandleExport)
	mux.HandleFunc("/api/v1/recording/exports/", api.HandleExportDownload)
	return mux
}
