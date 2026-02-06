package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/live"
)

type InternalHandler struct {
	Service *live.Service
}

func NewInternalHandler(svc *live.Service) *InternalHandler {
	return &InternalHandler{Service: svc}
}

// Service Token Middleware (Supports both Bearer and X-AI-Service-Token)
func (h *InternalHandler) ServiceAuthMiddleware(next http.Handler) http.Handler {
	token := os.Getenv("AI_SERVICE_TOKEN")
	if token == "" {
		token = "dev_ai_secret" // Fallback for dev
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		customHeader := r.Header.Get("X-AI-Service-Token")

		valid := false
		if authHeader != "" {
			parts := strings.Split(authHeader, " ")
			if len(parts) == 2 && parts[0] == "Bearer" && parts[1] == token {
				valid = true
			}
		}
		if !valid && customHeader != "" && customHeader == token {
			valid = true
		}

		if !valid {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// HandleIngestDetection accepts detections from AI Service
// POST /api/v1/internal/detections
// NOTE: This endpoint is dev-only when ENABLE_HTTP_INGEST=true
func (h *InternalHandler) IngestDetection(w http.ResponseWriter, r *http.Request) {
	// Check if HTTP ingest is enabled (dev-only)
	if os.Getenv("ENABLE_HTTP_INGEST") != "true" {
		http.Error(w, "HTTP ingest disabled", http.StatusForbidden)
		return
	}

	// Limit Body Size (8KB)
	r.Body = http.MaxBytesReader(w, r.Body, 8192)

	var payload live.DetectionPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid JSON or Body Too Large", http.StatusBadRequest)
		return
	}

	// Validation using service layer
	if err := live.ValidateDetection(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Tenant?
	// The AI Service likely knows the TenantID if it pulled list from us.
	// But payload matches contract: { "camera_id": ... }
	// We need to know which Tenant owns this camera to store it correctly "det:latest:{tenant}:{cam}"
	// Ideally detection payload includes TenantID.
	// OR we look it up. Lookup is expensive (DB).
	// Solution: Active Camera List provided to AI contained TenantID?
	// The "Active List" return `[]string` (camera IDs).
	// Wait, `det:latest:{tenant}:{camera}` requires TenantID.
	// We should probably rely on the AI service to pass back the TenantID it got.
	// Let's assume payload HAS TenantID or we fix Active List to return it.
	// User Requirement: "Key must include tenant".
	// Let's Lookup: live:active_overlay ZSET stores CameraID.
	// We assume we can get TenantID from CameraID? -> `s.GetCamera` checks it.
	// But `GetCamera` requires TenantID input to verify.
	// We need a Reverse Lookup or just trust AI service?
	// TRUST AI SERVICE: It calls `GetActive` -> We return struct {ID, TenantID}.
	// Update Internal Handler `GetActiveCameras` to return struct.
	// Update Ingest to accept TenantID.

	// Assuming Payload struct in service.go needs TenantID field?
	// "Contract" in prompt: { "camera_id": ..., "objects": ... } NO TENANT.
	// So we must lookup.
	// "Option A: Storage Key det:latest:{tenant_id}:{camera_id}"
	// How do we get TenantID?
	// Does CameraID imply Tenant? Yes, globally unique UUID.
	// Can we look it up? `repo.ResolveSiteID` exists. Maybe `repo.GetTenantID(camID)`?
	// Or cache it.
	// Simpler: The ZSET `live:overlay_demand` stores CameraID.
	// When we added to ZSET (IncrementOverlayDemand), we knew the TenantID?
	// Yes, `StartLiveSession` context has User -> Tenant.
	// Store `tenant_id:camera_id` in ZSET?
	// "Member" = "tenant_uuid:camera_uuid".
	// This solves it.

	// Re-reading service.go: `IncrementOverlayDemand(ctx, cameraID)`.
	// I should change that to `IncrementOverlayDemand(ctx, tenantID, cameraID)`.
	// And store combined string.
	// Then `GetActiveCameras` returns combined strings.
	// AI Service parses them.
	// AI Service sends back detection with just CameraID? Or both?
	// Ingest needs to know.
	// If AI Service sends `camera_id` and we stored `tenant:camera` in ZSET... we can't easily map back without lookup.
	// Unless AI Service sends TenantID in payload (easier).
	// Let's add TenantID to DetectionPayload.

	// NOTE: I cannot change `live.DetectionPayload` easily if I just wrote it in `service.go`.
	// I will update `service.go` next step to include TenantID in payload if needed,
	// OR update `IngestDetection` to assume `camera_id` is sufficient IF we relax the key requirement?
	// No, multi-session safety.
	// I will Parse TenantID from the Request? Or require it in Payload.
	// Prompt Contract:
	// { "camera_id": "...", "ts_unix_ms": ... "objects": ... }
	// NO TenantID.
	// OK, I must lookup.
	// `h.Service.ResolveCameraTenant(ctx, camID)` -> (tenantID, error).
	// I will add this helper to Service.

	// For now, let's assume I can resolve it.
	tenantID, err := h.Service.ResolveCameraTenant(r.Context(), payload.CameraID)
	// Make sure ResolveCameraTenant uses a fast lookup (e.g. Redis cache or DB).
	if err != nil {
		http.Error(w, "Camera not found", http.StatusNotFound)
		return
	}

	if err := h.Service.SaveDetection(r.Context(), tenantID, &payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// GET /api/v1/internal/cameras/active
// Returns cameras with overlay demand seen within 20s
func (h *InternalHandler) GetActiveCameras(w http.ResponseWriter, r *http.Request) {
	list, err := h.Service.GetActiveCamerasForAI(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// GET /api/v1/internal/cameras/{id}/snapshot
// Auth: Service Token
func (h *InternalHandler) GetInternalSnapshot(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	if idStr == "" {
		// Chi fallback
		pathParts := strings.Split(r.URL.Path, "/")
		if len(pathParts) >= 2 {
			idStr = pathParts[len(pathParts)-2]
		}
	}

	camID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid Camera ID", http.StatusBadRequest)
		return
	}

	// Internal unsafe retrieval to get IP
	cam, err := h.Service.CameraService.GetByIDUnsafe(r.Context(), camID)
	if err != nil {
		http.Error(w, "Camera not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")

	// Construct RTSP URL (Missing in DB, so we construct from IP)
	// Default pattern: rtsp://<ip>/live/0/SUB (Matches user's verification cam)
	// TODO: Use CameraCredential or specific profile table in future.
	rtspURL := fmt.Sprintf("rtsp://%s/live/0/SUB", cam.IPAddress.String())

	// FFmpeg capture to pipe
	// -rtsp_transport tcp: Force TCP for reliability
	// -i ...: Input
	// -vframes 1: Single frame
	// -f image2: Format
	// -: Output to stdout
	args := []string{
		"-y",
		"-rtsp_transport", "tcp",
		"-i", rtspURL,
		"-vframes", "1",
		"-f", "image2",
		"-update", "1",
		"-",
	}

	cmd := exec.Command("ffmpeg", args...)
	cmd.Stdout = w
	cmd.Stderr = os.Stderr // Optional debug

	if err := cmd.Run(); err != nil {
		// Log error to stderr (captured by Control Plane logs)
		fmt.Fprintf(os.Stderr, "Snapshot failed for %s: %v\n", camID, err)

		// FALLBACK: Serve static image if available (for testing offline cams)
		fallbackData, err2 := os.ReadFile("fallback.jpg")
		if err2 == nil {
			fmt.Fprintf(os.Stderr, "Serving fallback.jpg for %s\n", camID)
			w.Write(fallbackData)
			return
		}

		// Since we might have written partial response, we can't reliably send HTTP error now if headers sent.
		// But usually ffmpeg fails fast.
		return
	}
}
