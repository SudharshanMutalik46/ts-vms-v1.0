package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/cameras"
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

	idStr := chi.URLParam(r, "id")
	cameraID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid camera id", http.StatusBadRequest)
		return
	}

	profiles, err := h.Service.GetProfiles(r.Context(), cameraID)
	if err != nil {
		http.Error(w, "failed to get profiles", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profiles)
}

// POST /api/v1/cameras/{id}:select-media-profiles
func (h *MediaHandler) SelectProfiles(w http.ResponseWriter, r *http.Request) {
	// RBAC: camera.media.select

	idStr := chi.URLParam(r, "id")
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

// GET /api/v1/cameras/{id}/media-selection
func (h *MediaHandler) GetSelection(w http.ResponseWriter, r *http.Request) {
	// RBAC: camera.media.read

	idStr := chi.URLParam(r, "id")
	cameraID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid camera id", http.StatusBadRequest)
		return
	}

	sel, val, err := h.Service.GetSelection(r.Context(), cameraID)
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// POST /api/v1/cameras/{id}:validate-rtsp
func (h *MediaHandler) ValidateRTSP(w http.ResponseWriter, r *http.Request) {
	// RBAC: camera.media.validate

	idStr := chi.URLParam(r, "id")
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
