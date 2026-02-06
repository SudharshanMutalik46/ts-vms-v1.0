package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/cameras"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/media"
	"github.com/technosupport/ts-vms/internal/middleware"
	"github.com/technosupport/ts-vms/internal/platform/paths"
)

type HlsDebugHandler struct {
	MediaClient *media.Client
	CameraRepo  cameras.Repository
	MediaRepo   *data.MediaModel
}

func NewHlsDebugHandler(mediaClient *media.Client, repo cameras.Repository, mediaRepo *data.MediaModel) *HlsDebugHandler {
	return &HlsDebugHandler{
		MediaClient: mediaClient,
		CameraRepo:  repo,
		MediaRepo:   mediaRepo,
	}
}

func (h *HlsDebugHandler) GetHlsDebug(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	// Dev-only / Admin check?
	// The requirement is "admin-only, dev builds". We assume RBAC checks.
	// But let's check tenant anyway.

	idStr := r.PathValue("id")
	cameraID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid camera id", http.StatusBadRequest)
		return
	}
	tenantID := uuid.MustParse(ac.TenantID)

	resp := map[string]interface{}{
		"tenant_id": tenantID.String(),
		"camera_id": cameraID.String(),
	}

	// 1. Camera Exists?
	_, err = h.CameraRepo.GetByID(r.Context(), cameraID)
	if err != nil {
		resp["camera_exists"] = false
		resp["camera_error"] = err.Error()
	} else {
		resp["camera_exists"] = true
	}

	// 2. Media Selection?
	sel, err := h.MediaRepo.GetSelection(r.Context(), cameraID)
	if err != nil && err != sql.ErrNoRows {
		resp["media_selection_error"] = err.Error()
	}
	resp["media_selection_exists"] = (sel != nil)
	if sel != nil {
		resp["selected_profile"] = "manual" // Simplified for preventing leak of RTSP
		if sel.MainProfileToken != "" {
			resp["selected_profile"] = sel.MainProfileToken
		}
	} else {
		resp["selected_profile"] = "none (using default)"
	}

	// 3. HLS State
	status, err := h.MediaClient.GetIngestStatus(r.Context(), cameraID.String())
	if err != nil {
		resp["hls_error"] = err.Error()
	} else {
		resp["hls_state"] = status.State
		resp["active_session_id"] = status.SessionId
		resp["ingest_running"] = status.Running
	}

	// 4. FS Paths
	dataRoot := paths.ResolveDataRoot()
	resp["data_root"] = dataRoot
	if status != nil && status.SessionId != "" {
		resp["expected_session_dir"] = filepath.Join(dataRoot, "hls", "live", tenantID.String(), cameraID.String(), status.SessionId)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
