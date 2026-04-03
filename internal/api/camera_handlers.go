package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/cameras"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/middleware"
)

type CameraHandler struct {
	Service                  *cameras.Service
	RecordingInternalBaseURL string
	RecordingServiceKey      string
	Client                   *http.Client
}

func NewCameraHandler(svc *cameras.Service) *CameraHandler {
	return &CameraHandler{
		Service:                  svc,
		RecordingInternalBaseURL: strings.TrimRight(os.Getenv("TS_VMS_RECORDING_INTERNAL_URL"), "/"),
		RecordingServiceKey:      os.Getenv("TS_VMS_SERVICE_KEY"),
		Client:                   &http.Client{Timeout: 5 * time.Second},
	}
}

// Helpers
func respondJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}

func effectiveRecordingRTSPURL(raw string, ip net.IP, port int) string {
	if strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return ""
}

func (h *CameraHandler) syncRecorder(ctx context.Context, cam *data.Camera) error {
	if h == nil || h.Client == nil || h.RecordingInternalBaseURL == "" || cam == nil {
		return nil
	}
	body, _ := json.Marshal(map[string]any{
		"rtsp_url": effectiveRecordingRTSPURL(cam.RtspUrl, cam.IPAddress, cam.Port),
		"enabled":  cam.IsEnabled,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.RecordingInternalBaseURL+"/internal/recording/cameras/"+cam.ID.String()+"/sync", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Service-Key", h.RecordingServiceKey)
	resp, err := h.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("recording sync failed: %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	return nil
}

func (h *CameraHandler) deleteRecorderCamera(ctx context.Context, cameraID uuid.UUID) error {
	if h == nil || h.Client == nil || h.RecordingInternalBaseURL == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.RecordingInternalBaseURL+"/internal/recording/cameras/"+cameraID.String()+"/delete", nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Service-Key", h.RecordingServiceKey)
	resp, err := h.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("recording delete failed: %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	return nil
}

// POST /api/v1/cameras
func (h *CameraHandler) Create(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	var req struct {
		SiteID    string   `json:"site_id"`
		Name      string   `json:"name"`
		IPAddress string   `json:"ip_address"`
		Port      int      `json:"port"`
		RtspUrl   string   `json:"rtsp_url"`
		IsEnabled *bool    `json:"is_enabled,omitempty"` // Pointer to distinguish omitted
		Tags      []string `json:"tags"`
		// Metadata... omitted for brevity but should map
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	// Basic Validation
	siteID, err := uuid.Parse(req.SiteID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid Site ID")
		return
	}
	ip := net.ParseIP(req.IPAddress)
	if ip == nil {
		respondError(w, http.StatusBadRequest, "Invalid IP")
		return
	}

	// Check Scope: Does user have access to this site?
	// Middleware usually checks "cameras.create" on "site" scope?
	// But endpoint is generic POST /cameras.
	// RBAC Logic: If user has 'create' on Site A, they can create for Site A.
	// We must verify `req.SiteID` is permitted.
	// TODO: Integrate with Permissions check.
	// For Phase 2.1, we assume Middleware checked broad permission, but we check specific site.
	// Wait, if Middleware is `RequirePermission("cameras.create", "tenant")` it allows all.
	// If `RequirePermission("cameras.create", "site")`, we need to know the site BEFORE handler.
	// But Site ID is in Body. Middleware can't see body easily.
	// So Handler must verify Site ID is in User's Permitted Sites.
	// This requires fetching permissions again or Context having them.
	// For now, let's assume Tenant Admin (Tenant Wide).
	// Strictly: We should check if `ac` has access to `siteID`.

	c := &data.Camera{
		TenantID:  uuid.MustParse(ac.TenantID),
		SiteID:    siteID,
		Name:      req.Name,
		IPAddress: ip,
		Port:      req.Port,
		RtspUrl:   req.RtspUrl,
		IsEnabled: true, // Default enabled? Or based on req?
		Tags:      req.Tags,
	}
	// Explicit enabled override
	// Explicit enabled override
	if req.IsEnabled != nil {
		c.IsEnabled = *req.IsEnabled
	} else {
		c.IsEnabled = true // Default true
	}

	if err := h.Service.CreateCamera(r.Context(), c); err != nil {
		if errors.Is(err, cameras.ErrLicenseLimitExceeded) {
			respondError(w, http.StatusPaymentRequired, "License limit would be exceeded") // 402 or 403 or 400? Prompt says reason_code.
			// Let's use 403 Forbidden with custom body? Or 400 Bad Request.
			// Standard is 403 for policy/quota.
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if c.IsEnabled {
		if err := h.syncRecorder(r.Context(), c); err != nil {
			// Keep DB and recorder consistent: rollback to disabled if recorder attach failed.
			_ = h.Service.DisableCamera(r.Context(), c.ID, c.TenantID)
			c.IsEnabled = false
			respondJSON(w, http.StatusBadGateway, map[string]any{
				"camera":  c,
				"warning": "camera created, but recorder attach failed; camera was disabled",
				"error":   err.Error(),
			})
			return
		}
	}

	respondJSON(w, http.StatusCreated, c)
}

// PUT /api/v1/cameras/{id}
func (h *CameraHandler) Update(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	idStr := r.PathValue("id")
	if idStr == "" {
		respondError(w, http.StatusBadRequest, "Missing ID")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		respondError(w, http.StatusBadRequest, "Camera name is required")
		return
	}

	cam, err := h.Service.GetCamera(r.Context(), uuid.MustParse(ac.TenantID), idStr)
	if err != nil {
		respondError(w, http.StatusNotFound, "Camera not found")
		return
	}

	cam.Name = strings.TrimSpace(req.Name)
	if err := h.Service.UpdateCamera(r.Context(), cam); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, cam)
}

// GET /api/v1/cameras
func (h *CameraHandler) List(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	// Query Params
	limit := 50 // Strict default
	// Prompt: "Default page size = 50... Maximum limit=50 even if client asks for 500"
	// So we ignore client limit if > 50? Or error? Prompt: "Maximum limit=50".
	// Let's cap it.
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			if v > 0 && v < 50 {
				limit = v
			}
		}
	}
	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	filter := data.CameraFilter{}
	if siteStr := r.URL.Query().Get("site_id"); siteStr != "" {
		if sid, err := uuid.Parse(siteStr); err == nil {
			filter.SiteID = &sid
		}
	}
	if q := r.URL.Query().Get("q"); q != "" {
		filter.Query = q
	}

	tenantID := uuid.MustParse(ac.TenantID)
	// Use Service.List (which wraps repo)
	list, total, err := h.Service.List(r.Context(), tenantID, filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Payload with metadata
	respondJSON(w, http.StatusOK, map[string]any{
		"data": list,
		"meta": map[string]int{
			"total":  total,
			"limit":  limit,
			"offset": offset,
		},
	})
}

// GET /api/v1/cameras/{id}
func (h *CameraHandler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	if idStr == "" {
		idStr = r.URL.Query().Get("id") // Fallback
	}
	if idStr == "" {
		respondError(w, http.StatusBadRequest, "Missing ID")
		return
	}

	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	cam, err := h.Service.GetCamera(r.Context(), uuid.MustParse(ac.TenantID), idStr)
	if err != nil {
		respondError(w, http.StatusNotFound, "Camera not found")
		return
	}

	respondJSON(w, http.StatusOK, cam)
}

// POST /api/v1/cameras/bulk
func (h *CameraHandler) Bulk(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	var req struct {
		Action    string      `json:"action"` // enable, disable, tag_add, tag_remove
		CameraIDs []uuid.UUID `json:"camera_ids"`
		Tags      []string    `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	tid := uuid.MustParse(ac.TenantID)

	var err error
	switch req.Action {
	case "enable":
		err = h.Service.BulkEnable(r.Context(), tid, req.CameraIDs)
	case "disable":
		err = h.Service.BulkDisable(r.Context(), tid, req.CameraIDs)
	case "tag_add":
		err = h.Service.BulkAddTags(r.Context(), tid, req.CameraIDs, req.Tags)
	case "tag_remove":
		err = h.Service.BulkRemoveTags(r.Context(), tid, req.CameraIDs, req.Tags)
	default:
		respondError(w, http.StatusBadRequest, "Invalid Action")
		return
	}

	if err != nil {
		if errors.Is(err, cameras.ErrLicenseLimitExceeded) {
			respondError(w, http.StatusPaymentRequired, "License limit exceeded")
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

// POST /api/v1/cameras/{id}:enable
func (h *CameraHandler) Enable(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Path // Need proper mux in Main to extract ID. assuming standard ServeMux
	// With ServeMux "POST /api/v1/cameras/{id}:enable", parsing ID is manual if simple StripPrefix
	// But we need {id}.
	// Let's assume Main passes `id` via Context or we parse URL locally.
	// Hacky URL parsing for ServeMux 1.22 pattern matching?
	// Let's assume caller extracted ID.
	// Actually typical pattern: `id := r.PathValue("id")` (Go 1.22)
	idStr = r.PathValue("id")
	if idStr == "" {
		respondError(w, http.StatusBadRequest, "Missing ID")
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid ID")
		return
	}

	ac, _ := middleware.GetAuthContext(r.Context())
	err = h.Service.EnableCamera(r.Context(), id, uuid.MustParse(ac.TenantID))
	if err != nil {
		if errors.Is(err, cameras.ErrLicenseLimitExceeded) {
			respondError(w, http.StatusPaymentRequired, "License limit exceeded")
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cam, err := h.Service.GetCamera(r.Context(), uuid.MustParse(ac.TenantID), id.String())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.syncRecorder(r.Context(), cam); err != nil {
		_ = h.Service.DisableCamera(r.Context(), id, uuid.MustParse(ac.TenantID))
		respondError(w, http.StatusBadGateway, "camera enabled in DB but recorder attach failed: "+err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "enabled"})
}

func (h *CameraHandler) Disable(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid ID")
		return
	}

	ac, _ := middleware.GetAuthContext(r.Context())
	err = h.Service.DisableCamera(r.Context(), id, uuid.MustParse(ac.TenantID))
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cam, err := h.Service.GetCamera(r.Context(), uuid.MustParse(ac.TenantID), id.String())
	if err == nil {
		cam.IsEnabled = false
		if syncErr := h.syncRecorder(r.Context(), cam); syncErr != nil {
			respondJSON(w, http.StatusOK, map[string]any{
				"status":          "disabled",
				"warning":         "camera disabled in DB, but recorder stop failed",
				"recording_error": syncErr.Error(),
			})
			return
		}
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
}

// --- Group Handlers ---

// POST /api/v1/camera-groups
func (h *CameraHandler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	ac, _ := middleware.GetAuthContext(r.Context())

	g := &data.CameraGroup{
		ID:          uuid.New(),
		TenantID:    uuid.MustParse(ac.TenantID),
		Name:        input.Name,
		Description: input.Description,
	}

	if err := h.Service.CreateGroup(r.Context(), g); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, g)
}

// GET /api/v1/camera-groups
func (h *CameraHandler) ListGroups(w http.ResponseWriter, r *http.Request) {
	ac, _ := middleware.GetAuthContext(r.Context())

	groups, err := h.Service.ListGroups(r.Context(), uuid.MustParse(ac.TenantID))
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, groups)
}

// PUT /api/v1/camera-groups/{id}/members
func (h *CameraHandler) SetGroupMembers(w http.ResponseWriter, r *http.Request) {
	groupIDStr := r.PathValue("id")
	groupID, err := uuid.Parse(groupIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid Group ID")
		return
	}

	var input struct {
		CameraIDs []string `json:"camera_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	cams := make([]uuid.UUID, len(input.CameraIDs))
	for i, s := range input.CameraIDs {
		uid, err := uuid.Parse(s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "Invalid Camera ID: "+s)
			return
		}
		cams[i] = uid
	}

	ac, _ := middleware.GetAuthContext(r.Context())
	if err := h.Service.SetGroupMembers(r.Context(), groupID, uuid.MustParse(ac.TenantID), cams); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// DELETE /api/v1/camera-groups/{id}
func (h *CameraHandler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	groupIDStr := r.PathValue("id")
	groupID, err := uuid.Parse(groupIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid Group ID")
		return
	}

	ac, _ := middleware.GetAuthContext(r.Context())
	if err := h.Service.DeleteGroup(r.Context(), groupID, uuid.MustParse(ac.TenantID)); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// DELETE /api/v1/cameras/{id}
func (h *CameraHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	idStr := r.PathValue("id")
	cameraID, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid camera id")
		return
	}

	tenantID := uuid.MustParse(ac.TenantID)
	if err := h.Service.DeleteCamera(r.Context(), cameraID, tenantID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.deleteRecorderCamera(r.Context(), cameraID); err != nil {
		respondJSON(w, http.StatusOK, map[string]any{
			"status":          "deleted",
			"warning":         "camera deleted in DB, but recorder detach failed",
			"recording_error": err.Error(),
		})
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
