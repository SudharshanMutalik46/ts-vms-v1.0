package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/technosupport/ts-vms/internal/license"
)

type LicenseHandler struct {
	Manager *license.Manager
}

// Status Response (Redacted Safe Summary)
type LicenseStatusResponse struct {
	Status       string                `json:"status"`
	LicenseID    string                `json:"license_id,omitempty"`
	IssuedAt     *time.Time            `json:"issued_at,omitempty"`
	ValidUntil   *time.Time            `json:"valid_until,omitempty"`
	DaysToExpiry int                   `json:"days_to_expiry"`
	Limits       license.LicenseLimits `json:"limits"`
	Features     []string              `json:"features"` // Names only
	LastReload   time.Time             `json:"last_reload"`
	ReasonCode   string                `json:"reason_code,omitempty"`
}

func (h *LicenseHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	// RBAC: license.read (Handled by middleware wrapper)
	state := h.Manager.GetState()

	resp := LicenseStatusResponse{
		Status:       string(state.Status),
		ReasonCode:   state.ReasonCode,
		LastReload:   state.LastReload,
		DaysToExpiry: state.DaysToExpiry,
	}

	if state.Payload != nil {
		resp.LicenseID = state.Payload.LicenseID.String()
		resp.IssuedAt = &state.Payload.IssuedAt
		resp.ValidUntil = &state.Payload.ValidUntil
		resp.Limits = state.Payload.Limits

		// Map features to list
		var features []string
		for k, v := range state.Payload.Features {
			if v {
				features = append(features, k)
			}
		}
		resp.Features = features
	}

	json.NewEncoder(w).Encode(resp)
}

func (h *LicenseHandler) Reload(w http.ResponseWriter, r *http.Request) {
	// RBAC: license.manage (Handled by wrapper)
	// Trigger Reload
	h.Manager.Reload()

	// Return new status immediately
	h.GetStatus(w, r)
}
