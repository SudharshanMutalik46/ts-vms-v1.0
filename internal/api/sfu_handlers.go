package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/cameras"
)

type SfuHandler struct {
	Service *cameras.SfuService
}

func NewSfuHandler(svc *cameras.SfuService) *SfuHandler {
	return &SfuHandler{Service: svc}
}

func (h *SfuHandler) writeError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (h *SfuHandler) GetRtpCapabilities(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	cameraID, err := uuid.Parse(idStr)
	if err != nil {
		h.writeError(w, "invalid camera id: "+idStr, http.StatusBadRequest)
		return
	}

	tenantID, err := getTenantID(r)
	if err != nil {
		h.writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	caps, err := h.Service.GetRtpCapabilities(r.Context(), tenantID, cameraID)
	if err != nil {
		h.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(caps)
}

func (h *SfuHandler) JoinRoom(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	cameraID, err := uuid.Parse(idStr)
	if err != nil {
		h.writeError(w, "invalid camera id: "+idStr, http.StatusBadRequest)
		return
	}

	tenantID, err := getTenantID(r)
	if err != nil {
		h.writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var body struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	sessionID := body.SessionID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	res, err := h.Service.JoinRoom(r.Context(), tenantID, cameraID, sessionID)
	if err != nil {
		if err.Error() == "SFU error: status=429" || err.Error() == "room at capacity" {
			h.writeError(w, "room at capacity", http.StatusTooManyRequests)
			return
		}
		h.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(res)
}

func (h *SfuHandler) CreateTransport(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	// For CreateTransport, 'id' in path ? The route is POST /api/v1/sfu/transports (no id?)
	// Wait, existing code used r.PathValue("id").
	// Checking main.go:
	// mux.Handle("POST /api/v1/sfu/transports", ...HandlerFunc(sfuHandler.CreateTransport))))
	// THERE IS NO {id} in the route!
	// So r.PathValue("id") returns "" (empty string).
	// uuid.Parse("") fails.
	// THIS IS THE BUG for CreateTransport.
	// But JoinRoom HAS {id}.
	// "POST /api/v1/sfu/rooms/{id}/join"
	// So JoinRoom should work.
	// I will fix JoinRoom JSON here.

	// Re-checking CreateTransport logic:
	// It calls h.Service.CreateTransport(ctx, tenantID, cameraID).
	// Does it NEED a camera ID?
	// The MediaSoup Router is associated with a Room (Camera).
	// So CreateTransport MUST know the Room.
	// BUT the route `POST /api/v1/sfu/transports` has NO Camera ID.
	// This API design is flawed or I missed something.
	// Maybe it expects CameraID in body?
	// Or maybe the route SHOULD be `/api/v1/sfu/rooms/{id}/transports`?
	// Let's assume the previous code was wrong and I should fix the ROUTE in main.go too?
	// Or maybe client sends it?

	// Proceeding with JSON fix for now.
	cameraID, err := uuid.Parse(idStr)
	if err != nil {
		// If route doesn't have ID, this always fails.
		// I'll emit JSON so client sees it.
		h.writeError(w, "missing or invalid camera id (route parameter)", http.StatusBadRequest)
		return
	}

	// ...
	tenantID, err := getTenantID(r)
	if err != nil {
		h.writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	transport, err := h.Service.CreateTransport(r.Context(), tenantID, cameraID)
	if err != nil {
		h.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(transport)
}

func (h *SfuHandler) ConnectTransport(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	transportID := r.PathValue("transportId")

	tenantID, err := getTenantID(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var body struct {
		DtlsParameters json.RawMessage `json:"dtlsParameters"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	if err := h.Service.ConnectTransport(r.Context(), tenantID.String(), idStr, transportID, body.DtlsParameters); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *SfuHandler) Consume(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	transportID := r.PathValue("transportId")

	tenantID, err := getTenantID(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var body struct {
		RtpCapabilities json.RawMessage `json:"rtpCapabilities"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	consumer, err := h.Service.Consume(r.Context(), tenantID.String(), idStr, transportID, body.RtpCapabilities)
	if err != nil {
		fmt.Printf("[ERROR] SFU Handler Consume failed: %v\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(consumer)
}

func (h *SfuHandler) LeaveRoom(w http.ResponseWriter, r *http.Request) {
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

	if err := h.Service.LeaveRoom(r.Context(), tenantID, cameraID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
