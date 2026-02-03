package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/cameras"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/middleware"
)

type PermissionChecker interface {
	CheckPermission(ctx context.Context, permSlug, scopeType, scopeID string) (bool, error)
}

type CameraProvider interface {
	GetByID(ctx context.Context, id, tenantID uuid.UUID) (*data.Camera, error)
}

type CredentialHandler struct {
	CredService   *cameras.CredentialService
	CameraService CameraProvider
	Perms         PermissionChecker
}

func NewCredentialHandler(credSvc *cameras.CredentialService, camSvc CameraProvider, perms PermissionChecker) *CredentialHandler {
	return &CredentialHandler{CredService: credSvc, CameraService: camSvc, Perms: perms}
}

// Helper to check scope and return tenantID, cameraID
// Returns 404 if not found OR not authorized (Non-enumeration)
func (h *CredentialHandler) checkAccess(w http.ResponseWriter, r *http.Request, permission string) (uuid.UUID, uuid.UUID, bool) {
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		respondError(w, http.StatusForbidden, "Forbidden")
		return uuid.Nil, uuid.Nil, false
	}
	tenantID := uuid.MustParse(ac.TenantID)

	idStr := r.PathValue("id")
	if idStr == "" {
		respondError(w, http.StatusBadRequest, "Missing Camera ID")
		return uuid.Nil, uuid.Nil, false
	}
	cameraID, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid Camera ID")
		return uuid.Nil, uuid.Nil, false
	}

	// 1. Resolve Camera to Site (and ensure it exists)
	cam, err := h.CameraService.GetByID(r.Context(), cameraID, tenantID) // This checks tenant isolation
	if err != nil {
		// Log internal error?
		// Return 404 to hide existence
		respondError(w, http.StatusNotFound, "Camera not found")
		return uuid.Nil, uuid.Nil, false
	}

	// 2. Check Site Scope
	scopeType := "tenant"
	scopeID := ac.TenantID
	if cam.SiteID != uuid.Nil {
		scopeType = "site"
		scopeID = cam.SiteID.String()
	}

	allowed, err := h.Perms.CheckPermission(r.Context(), permission, scopeType, scopeID)
	if err != nil || !allowed {
		respondError(w, http.StatusNotFound, "Camera not found") // 404 per Requirement
		return uuid.Nil, uuid.Nil, false
	}

	return tenantID, cameraID, true
}

func (h *CredentialHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, cameraID, ok := h.checkAccess(w, r, "camera.credential.write")
	if !ok {
		return
	}

	var input cameras.CredentialInput
	// Cap Payload Size (Service does strict validation, but Handler limit is good too)
	r.Body = http.MaxBytesReader(w, r.Body, 8192) // 8KB safety
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	// Validation
	if input.Username == "" {
		respondError(w, http.StatusBadRequest, "Username required")
		return
	}
	if len(input.Username) > 128 || len(input.Password) > 128 {
		respondError(w, http.StatusBadRequest, "Credentials too long")
		return
	}

	if err := h.CredService.SetCredentials(r.Context(), tenantID, cameraID, input); err != nil {
		if errors.Is(err, cameras.ErrCredentialTooLarge) {
			respondError(w, http.StatusBadRequest, "Payload too large")
			return
		}
		respondError(w, http.StatusInternalServerError, "Internal Error")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *CredentialHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, cameraID, ok := h.checkAccess(w, r, "camera.credential.read")
	if !ok {
		return
	}

	reveal := r.URL.Query().Get("reveal") == "true"

	out, found, err := h.CredService.GetCredentials(r.Context(), tenantID, cameraID, reveal)
	if err != nil {
		// Crypto error or internal
		respondError(w, http.StatusInternalServerError, "Internal Check Failed")
		return
	}

	if !found {
		// Explicit 404 if authenticated but no record
		respondError(w, http.StatusNotFound, "Credentials not found")
		return
	}

	respondJSON(w, http.StatusOK, out)
}

func (h *CredentialHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, cameraID, ok := h.checkAccess(w, r, "camera.credential.delete")
	if !ok {
		return
	}

	if err := h.CredService.DeleteCredentials(r.Context(), tenantID, cameraID); err != nil {
		respondError(w, http.StatusInternalServerError, "Delete Failed")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
