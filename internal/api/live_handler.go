package api

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/technosupport/ts-vms/internal/live"
	"github.com/technosupport/ts-vms/internal/middleware"
)

type LiveHandler struct {
	Service   *live.Service
	Telemetry *live.TelemetryService
}

func NewLiveHandler(svc *live.Service, tel *live.TelemetryService) *LiveHandler {
	return &LiveHandler{
		Service:   svc,
		Telemetry: tel,
	}
}

// getCameraID handles both chi and std mux (Go 1.22+)
func getCameraID(r *http.Request) string {
	id := chi.URLParam(r, "id")
	if id == "" {
		id = r.PathValue("id")
	}
	if id == "" {
		// Manual fallback for deep nesting if everything else fails
		pathParts := splitPath(r.URL.Path)
		for i := len(pathParts) - 1; i >= 0; i-- {
			if pathParts[i] == "cameras" && i+1 < len(pathParts) {
				return pathParts[i+1]
			}
		}
	}
	return id
}

// StartSession initiates a WebRTC/HLS session
// POST /api/v1/cameras/{id}/live/start
func (h *LiveHandler) StartSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := middleware.GetUserFromContext(ctx)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	cameraID := getCameraID(r)
	if cameraID == "" {
		http.Error(w, "Camera ID required", http.StatusBadRequest)
		return
	}

	// Parse Body for Quality/ViewMode
	var req struct {
		ViewMode string `json:"view_mode"`
		Quality  string `json:"quality"`
	}
	// Decode is optional (body might be empty)
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}

	resp, err := h.Service.StartLiveSession(ctx, user, cameraID, req.ViewMode, req.Quality)
	if err != nil {
		// Check for Live Limit
		if err.Error() == live.ErrLiveLimitExceeded ||
			(len(err.Error()) > len(live.ErrLiveLimitExceeded) && err.Error()[:len(live.ErrLiveLimitExceeded)] == live.ErrLiveLimitExceeded) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			// Parse explicit values if formatted string
			json.NewEncoder(w).Encode(map[string]interface{}{
				"code":  live.ErrLiveLimitExceeded,
				"error": err.Error(),
				"limit": 16, // Contract
			})
			return
		}

		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// RecordEvent handles client telemetry ingestion
// POST /api/v1/live/events
func (h *LiveHandler) RecordEvent(w http.ResponseWriter, r *http.Request) {
	var evt live.TelemetryEvent
	if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if err := h.Telemetry.RecordEvent(r.Context(), &evt); err != nil {
		// Rate limit or validation error
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// POST /api/v1/live/{session_id}/overlay/enable
func (h *LiveHandler) EnableOverlay(w http.ResponseWriter, r *http.Request) {
	sessID := chi.URLParam(r, "session_id")
	if err := h.Service.SetOverlayState(r.Context(), sessID, true); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Register demand for the camera associated with this session
	// Optimization: sessID should map to camera. For MVP, we can lookup session then camera.
	// But simpler for this phase: if we can't easily get cameraID here, we rely on GetLatestDetection polling.
	w.WriteHeader(http.StatusOK)
}

// POST /api/v1/live/{session_id}/overlay/disable
func (h *LiveHandler) DisableOverlay(w http.ResponseWriter, r *http.Request) {
	sessID := chi.URLParam(r, "session_id")
	if err := h.Service.SetOverlayState(r.Context(), sessID, false); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// GET /api/v1/cameras/{id}/detections/latest?stream=basic|weapon
func (h *LiveHandler) GetLatestDetection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := middleware.GetUserFromContext(ctx)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	cameraID := getCameraID(r)
	stream := r.URL.Query().Get("stream")
	if stream == "" {
		stream = "basic"
	}

	if cameraID == "" {
		http.Error(w, "Camera ID required", http.StatusBadRequest)
		return
	}

	// Weapon feature flag check
	if stream == "weapon" {
		// Check if weapon AI is enabled via ENV or license
		weaponEnabled := os.Getenv("WEAPON_AI_ENABLED") == "true"
		if !weaponEnabled {
			// Return 200 with upgrade_required instead of 403
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"upgrade_required": true,
				"feature":          "weapon_ai",
			})
			return
		}
	}

	// Verify Access (RBAC)
	_, err = h.Service.CameraService.GetCamera(ctx, user.TenantID, cameraID)
	if err != nil {
		http.Error(w, "Camera access denied", http.StatusForbidden)
		return
	}

	// Register demand (Phase 3.8: Tells AI engine to start/keep processing this camera)
	h.Service.RefreshOverlayDemand(ctx, cameraID)

	payload, err := h.Service.GetLatestDetection(ctx, user.TenantID, cameraID, stream)
	if err != nil {
		// redis.Nil is expected when no detection available
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if payload == nil {
		// 204 No Content (No fresh detection)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(payload)
}

// GET /api/v1/cameras/{id}/snapshot
// For Phase 3.8: Proxy to Media OR return Placeholder if not supported.
// Prompt constraint: "don't force heavy decode... mock if strictly limited".
// We will return a placeholder image for now to satisfy the Overlay loop until Media Plane supports snapshots.
func (h *LiveHandler) GetSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := middleware.GetUserFromContext(ctx)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	camID := getCameraID(r)
	if camID == "" {
		http.Error(w, "Camera ID required", http.StatusBadRequest)
		return
	}
	// Verify Access
	_, err = h.Service.CameraService.GetCamera(ctx, user.TenantID, camID)
	if err != nil {
		http.Error(w, "Camera access denied", http.StatusForbidden)
		return
	}

	// Placeholder JPEG (1x1 black pixel or simple header)
	// Minimal JPEG Header + Data
	w.Header().Set("Content-Type", "image/jpeg")
	// A tiny valid JPEG (1x1 gray)
	tinyJpeg := []byte{
		0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 0x4a, 0x46, 0x49, 0x46, 0x00, 0x01, 0x01, 0x01, 0x00, 0x48,
		0x00, 0x48, 0x00, 0x00, 0xff, 0xdb, 0x00, 0x43, 0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xc0, 0x00, 0x0b,
		0x08, 0x00, 0x01, 0x00, 0x01, 0x01, 0x01, 0x11, 0x00, 0xff, 0xc4, 0x00, 0x1f, 0x00, 0x00, 0x01,
		0x05, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
		0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0xff, 0xda, 0x00, 0x08, 0x01, 0x01,
		0x00, 0x00, 0x3f, 0x00, 0xbf, 0xff, 0xd9,
	}
	w.Write(tinyJpeg)
}

// Helper to split path (std lib doesn't include "SplitPath" clean)
func splitPath(p string) []string {
	// naive split
	var res []string
	start := 0
	for i := 0; i < len(p); i++ {
		if p[i] == '/' {
			if i > start {
				res = append(res, p[start:i])
			}
			start = i + 1
		}
	}
	if start < len(p) {
		res = append(res, p[start:])
	}
	return res
}
