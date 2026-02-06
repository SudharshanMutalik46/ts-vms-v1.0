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
	json.NewEncoder(w).Encode(resp)
}
