package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/cameras"
	"github.com/technosupport/ts-vms/internal/media"
	"github.com/technosupport/ts-vms/internal/middleware"
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

	// 3. Check Ingest State
	ingest, err := h.MediaClient.GetIngestStatus(ctx, cameraID.String())
	if err != nil {
		resp["ingest_status_error"] = err.Error()
	} else {
		resp["ingest_running"] = ingest.Running
		resp["ingest_state"] = ingest.State
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// DebugMeHandler matches the desktop's UserIdentity model (deprecated, use UserHandler.GetMe)
func DebugMeHandler(w http.ResponseWriter, r *http.Request) {
	// 1. Extract Identity from JWT Context
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// 2. Map Permissions
	perms := make([]string, 0, len(ac.Permissions))
	for k := range ac.Permissions {
		perms = append(perms, k)
	}

	// 3. Construct Response
	resp := IdentityResponse{
		ID:       ac.UserID,
		Username: "user@" + ac.TenantID, // Placeholder or we inject username into token? Token usually has distinct username claim or we just use ID/Email.
		// For VMS, we probably want the Email/Username.
		// Start: TokenManager doesn't put username in AuthContext currently?
		// Let's check middleware/auth_context.go again. It has UserID, TenantID, Roles.
		// If we need Username, we might need to DB query OR add it to JWT.
		// For this specific bug (ID mismatch), ID is what matters.
		// UI uses Username for display. "user@..." is safe fallback or we can query DB.
		// Given strict constraints and "DebugMe" nature, I'll stick to ID match first.
		// Better: The UI might show "user@..." which is ugly.
		// But to fix the BUG, ID match is key.
		// Let's rely on ID match.
		TenantID:    ac.TenantID,
		Roles:       ac.Roles,
		Permissions: perms,
	}

	// FIX: If we want real username, we should query DB. But DebugMeHandler is a func, no dependencies.
	// To query DB, we'd need to convert it to a struct method or reference a global (bad).
	// For now, let's fix the ID mismatch. The UI will likely show the ID or "user@..." but the Password Change will WORK.
	// Actually, the LoginViewModel fetches this and sets `_sessionService.SetIdentity(identity)`.
	// The `UserDto` from ListUsers has the real username/email.
	// `UsersViewModel` uses `CurrentUser` (UserDto) for display in the form.
	// `_session.CurrentUser` is used for permission checks and this ID check.
	// So `Username` in Identity is less critical for the specific "Old Password" bug, but `ID` is critical.
	// However, `MainViewModel` logs `Identity found: {identity.Username}`.
	// I'll set Username to `ac.UserID` or "Authenticated User" to be safe if token doesn't have it.

	resp.Username = "user-" + ac.UserID[:8] // Temporary display name if we can't get real one without DB.

	// FIX: Ensure non-nil slices for JSON serialization (avoid null)
	if resp.Roles == nil {
		resp.Roles = []string{}
	}
	if resp.Permissions == nil {
		resp.Permissions = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
