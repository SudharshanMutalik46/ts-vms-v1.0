package api

import (
	"encoding/json"
	"fmt"
	"log"
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

// writeStructuredError writes a standardized JSON error response.
func (h *SfuHandler) writeStructuredError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("[ERROR] SFU Handler error: %v (REQ:%s)", err, r.Header.Get("X-Request-ID"))
	// 1. Get X-Request-ID
	reqID := w.Header().Get("X-Request-ID")
	if reqID == "" {
		// Fallback check request header if response header not set yet?
		reqID = r.Header.Get("X-Request-ID")
	}
	if reqID == "" {
		reqID = "unknown"
	}

	resp := map[string]interface{}{
		"req_id": reqID,
	}

	statusCode := http.StatusInternalServerError

	// 2. Unpack Error
	if sfuErr, ok := err.(*cameras.SfuStepError); ok {
		resp["step"] = sfuErr.Step
		resp["error_code"] = sfuErr.ErrorCode
		resp["safe_message"] = sfuErr.SafeMessage
		if sfuErr.RequiredAction != "" {
			resp["required_action"] = sfuErr.RequiredAction
		}

		if sfuErr.FallbackHint {
			resp["fallback_hint"] = true
			if sfuErr.FallbackURL != "" {
				resp["fallback_url"] = sfuErr.FallbackURL
			}
		} else {
			resp["fallback_hint"] = nil
		}

		// Map codes
		switch sfuErr.ErrorCode {
		case "ERR_AUTH_MISSING", "ERR_AUTH_INVALID":
			statusCode = http.StatusUnauthorized
		case "ERR_RBAC_DENIED", "ERR_FORBIDDEN":
			statusCode = http.StatusForbidden
		case "ERR_ROOM_FULL":
			statusCode = http.StatusTooManyRequests
		case "ERR_CAMERA_NOT_FOUND":
			statusCode = http.StatusNotFound
		case "ERR_SFU_DOWN":
			statusCode = http.StatusServiceUnavailable
		case "ERR_BAD_REQUEST":
			statusCode = http.StatusBadRequest
		default:
			statusCode = http.StatusInternalServerError
		}
	} else {
		// Generic
		resp["step"] = "handler"
		resp["error_code"] = "ERR_INTERNAL"
		resp["safe_message"] = err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(resp)
}

func (h *SfuHandler) GetRtpCapabilities(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	roomID, err := uuid.Parse(idStr)
	if err != nil {
		h.writeStructuredError(w, r, cameras.NewSfuError("parse_params", "ERR_BAD_REQUEST", "invalid room id", err))
		return
	}



	tenantID, err := getTenantID(r)
	if err != nil {
		h.writeStructuredError(w, r, cameras.NewSfuError("auth", "ERR_AUTH_INVALID", "unauthorized", err))
		return
	}

	caps, err := h.Service.GetRtpCapabilities(r.Context(), tenantID, roomID)
	if err != nil {
		h.writeStructuredError(w, r, err) // Service returns SfuStepError hopefully, if not generic wraps it handling
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(caps)
}

func (h *SfuHandler) JoinRoom(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	roomID, err := uuid.Parse(idStr)
	if err != nil {
		h.writeStructuredError(w, r, cameras.NewSfuError("parse_params", "ERR_BAD_REQUEST", "invalid room id", err))
		return
	}

	tenantID, err := getTenantID(r)
	if err != nil {
		h.writeStructuredError(w, r, cameras.NewSfuError("auth", "ERR_AUTH_INVALID", "unauthorized", err))
		return
	}

	var body struct {
		SessionID        string   `json:"sessionId"`
		CodecPreferences []string `json:"codecPreferences"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	sessionID := body.SessionID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	res, err := h.Service.JoinRoom(r.Context(), tenantID, roomID, sessionID, body.CodecPreferences)
	if err != nil {
		h.writeStructuredError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(res)
}

func (h *SfuHandler) CreateTransport(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	roomID, err := uuid.Parse(idStr)
	if err != nil {
		h.writeStructuredError(w, r, cameras.NewSfuError("parse_params", "ERR_BAD_REQUEST", "invalid room id", err))
		return
	}

	tenantID, err := getTenantID(r)
	if err != nil {
		h.writeStructuredError(w, r, cameras.NewSfuError("auth", "ERR_AUTH_INVALID", "unauthorized", err))
		return
	}

	transport, err := h.Service.CreateTransport(r.Context(), tenantID, roomID)
	if err != nil {
		h.writeStructuredError(w, r, err)
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
		h.writeStructuredError(w, r, cameras.NewSfuError("auth", "ERR_AUTH_INVALID", "unauthorized", err))
		return
	}

	var body struct {
		DtlsParameters json.RawMessage `json:"dtlsParameters"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeStructuredError(w, r, cameras.NewSfuError("parse_body", "ERR_BAD_REQUEST", "invalid body", err))
		return
	}

	if err := h.Service.ConnectTransport(r.Context(), tenantID.String(), idStr, transportID, body.DtlsParameters); err != nil {
		h.writeStructuredError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *SfuHandler) ResumeConsumer(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	transportID := r.PathValue("transportId")
	consumerID := r.PathValue("consumerId")

	tenantID, err := getTenantID(r)
	if err != nil {
		h.writeStructuredError(w, r, cameras.NewSfuError("auth", "ERR_AUTH_INVALID", "unauthorized", err))
		return
	}

	if idStr == "" || transportID == "" || consumerID == "" {
		h.writeStructuredError(w, r, cameras.NewSfuError("parse_params", "ERR_BAD_REQUEST", "missing route parameters", nil))
		return
	}

	if err := h.Service.ResumeConsumer(r.Context(), tenantID.String(), idStr, transportID, consumerID); err != nil {
		h.writeStructuredError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *SfuHandler) Consume(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	transportID := r.PathValue("transportId")

	tenantID, err := getTenantID(r)
	if err != nil {
		h.writeStructuredError(w, r, cameras.NewSfuError("auth", "ERR_AUTH_INVALID", "unauthorized", err))
		return
	}

	var body struct {
		RtpCapabilities json.RawMessage `json:"rtpCapabilities"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeStructuredError(w, r, cameras.NewSfuError("parse_body", "ERR_BAD_REQUEST", "invalid body", err))
		return
	}

	consumer, err := h.Service.Consume(r.Context(), tenantID.String(), idStr, transportID, body.RtpCapabilities)
	if err != nil {
		fmt.Printf("[ERROR] SFU Handler Consume failed: %v\n", err)
		h.writeStructuredError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(consumer)
}

func (h *SfuHandler) LeaveRoom(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	roomID, err := uuid.Parse(idStr)
	if err != nil {
		h.writeStructuredError(w, r, cameras.NewSfuError("parse_params", "ERR_BAD_REQUEST", "invalid room id", err))
		return
	}

	tenantID, err := getTenantID(r)
	if err != nil {
		h.writeStructuredError(w, r, cameras.NewSfuError("auth", "ERR_AUTH_INVALID", "unauthorized", err))
		return
	}

	if err := h.Service.LeaveRoom(r.Context(), tenantID, roomID); err != nil {
		h.writeStructuredError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}
