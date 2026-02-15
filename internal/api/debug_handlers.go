package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/cameras"
	"github.com/technosupport/ts-vms/internal/media"
)

type DebugHandler struct {
	SfuService  *cameras.SfuService
	MediaClient *media.Client
}

func NewDebugHandler(sfuSvc *cameras.SfuService, mediaClient *media.Client) *DebugHandler {
	return &DebugHandler{
		SfuService:  sfuSvc,
		MediaClient: mediaClient,
	}
}

func (h *DebugHandler) GetLiveDebug(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	cameraID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid camera id", http.StatusBadRequest)
		return
	}

	tenantID, err := getTenantID(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	resp := map[string]interface{}{
		"camera_id": cameraID.String(),
		"tenant_id": tenantID.String(),
	}

	// 1. Check SFU Reachability (Proxy via GetRtpCapabilities)
	// We don't care if room exists or not, just if we can talk to SFU.
	// This will try to get router caps for the camera room.
	_, err = h.SfuService.GetRtpCapabilities(ctx, tenantID, cameraID)
	if err != nil {
		resp["sfu_reachable"] = false
		resp["sfu_error"] = err.Error()
	} else {
		resp["sfu_reachable"] = true
	}

	// 2. Check Media Plane Reachability
	ok, statusMsg, err := h.MediaClient.Health(ctx)
	if err != nil {
		resp["media_reachable"] = false
		resp["media_error"] = err.Error()
	} else {
		resp["media_reachable"] = ok
		resp["media_status"] = statusMsg
	}

	// 3. Check Ingest / HLS State
	ingest, err := h.MediaClient.GetIngestStatus(ctx, cameraID.String())
	if err != nil {
		resp["ingest_status_error"] = err.Error()
	} else {
		resp["ingest_running"] = ingest.Running
		resp["ingest_state"] = ingest.State
		resp["hls_state"] = ingest.HlsState
		resp["hls_session_id"] = ingest.SessionId
		if ingest.SessionId != "" {
			resp["hls_playlist_url"] = "/hls/live/" + tenantID.String() + "/" + cameraID.String() + "/" + ingest.SessionId + "/playlist.m3u8"
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// IdentityResponse matches the desktop's UserIdentity model
type IdentityResponse struct {
	ID          string   `json:"id"`
	Username    string   `json:"username"`
	TenantID    string   `json:"tenant_id"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
}

func DebugMeHandler(w http.ResponseWriter, r *http.Request) {
	// 1. In production, extract these from the JWT context middleware
	// For now, using the IDs from your previous raw response
	userID := "00000000-0000-0000-0000-000000000002"
	tenantID := "00000000-0000-0000-0000-000000000001"

	// 2. Define the response with explicit permissions required for Phase 1.5
	resp := IdentityResponse{
		ID:       userID,
		Username: "admin@technosupport.com",
		TenantID: tenantID,
		Roles:    []string{"admin"},
		Permissions: []string{
			"audit.read",   // Enables Audit Logs Button
			"audit.export", // Enables Export functionality
			"user.read",    // Enables User Management view
			"camera.view",  // Enables Live Dashboard
			"license.read", // Enables License view
			"cameras.list", // Fix: Needed for loading cameras
			"cameras.manage",
			"cameras.create",
			"nvr.read",
			"nvr.write",
			"admin.access",
		},
	}

	// 3. Set production headers
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// 4. Use Encoder for efficiency
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
