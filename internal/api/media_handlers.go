package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/cameras"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/middleware"
)

type MediaHandler struct {
	Service *cameras.MediaService
}

func NewMediaHandler(svc *cameras.MediaService) *MediaHandler {
	return &MediaHandler{Service: svc}
}

func getTenantID(r *http.Request) (uuid.UUID, error) {
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		return uuid.Nil, fmt.Errorf("no auth context")
	}
	// ac.TenantID is likely string if lint complained.
	return uuid.Parse(ac.TenantID)
}

// GET /api/v1/cameras/{id}/media-profiles
func (h *MediaHandler) ListProfiles(w http.ResponseWriter, r *http.Request) {
	// RBAC: camera.media.read
	// (Middleware handles token, we assume context has Claims)
	// Just need to ensure permission.
	// Actually, we should check `RequirePermission("camera.media.read")` middleware in Routes.

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

	profiles, err := h.Service.GetProfiles(r.Context(), tenantID, cameraID)
	if err != nil {
		http.Error(w, "failed to get profiles", http.StatusInternalServerError)
		return
	}
	// Ensure non-nil slice so JSON is [] not null
	if profiles == nil {
		profiles = []*data.CameraMediaProfile{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profiles)
}

// POST /api/v1/cameras/{id}:select-media-profiles
func (h *MediaHandler) SelectProfiles(w http.ResponseWriter, r *http.Request) {
	// RBAC: camera.media.select

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

	// Body optional (policy override), ignored for now as per plan

	selection, err := h.Service.SelectMediaProfiles(r.Context(), tenantID, cameraID)
	if err != nil {
		http.Error(w, "selection failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(selection)
}

// PUT /api/v1/cameras/{id}/media-selection
func (h *MediaHandler) UpdateSelectionUrls(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		MainRTSP string `json:"main_rtsp_url_sanitized"`
		SubRTSP  string `json:"sub_rtsp_url_sanitized"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.MainRTSP) == "" && strings.TrimSpace(req.SubRTSP) == "" {
		http.Error(w, "main or sub rtsp url is required", http.StatusBadRequest)
		return
	}

	selection, err := h.Service.UpdateManualStreamUrls(r.Context(), tenantID, cameraID, req.MainRTSP, req.SubRTSP)
	if err != nil {
		http.Error(w, "update failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(selection)
}

// GET /api/v1/cameras/{id}/media-selection
func (h *MediaHandler) GetSelection(w http.ResponseWriter, r *http.Request) {
	// RBAC: camera.media.read

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

	sel, val, err := h.Service.GetSelection(r.Context(), tenantID, cameraID)
	if err != nil {
		http.Error(w, "failed to get selection", http.StatusInternalServerError)
		return
	}
	if sel == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	resp := map[string]interface{}{
		"selection":  sel,
		"validation": val,
	}

	if profiles, err := h.Service.GetProfiles(r.Context(), tenantID, cameraID); err == nil {
		codecByToken := make(map[string]string, len(profiles))
		for _, profile := range profiles {
			if profile == nil || profile.ProfileToken == "" {
				continue
			}
			codecByToken[profile.ProfileToken] = profile.VideoCodec
		}

		if selection, ok := resp["selection"].(*data.CameraStreamSelection); ok && selection != nil {
			resp["selection"] = map[string]interface{}{
				"main_profile_token":      selection.MainProfileToken,
				"main_rtsp_url_sanitized": selection.MainRTSP,
				"main_supported":          selection.MainSupported,
				"main_codec":              codecByToken[selection.MainProfileToken],
				"sub_profile_token":       selection.SubProfileToken,
				"sub_rtsp_url_sanitized":  selection.SubRTSP,
				"sub_supported":           selection.SubSupported,
				"sub_codec":               codecByToken[selection.SubProfileToken],
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// POST /api/v1/cameras/{id}:validate-rtsp
func (h *MediaHandler) ValidateRTSP(w http.ResponseWriter, r *http.Request) {
	// RBAC: camera.media.validate

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

	if err := h.Service.ValidateRTSP(r.Context(), tenantID, cameraID); err != nil {
		http.Error(w, "validation trigger failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"status":"queued"}`))
}
