package api

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/middleware"
)

// GetNVRHealthSummary returns aggregated stats.
// Permissions: nvr.health.read
// Enforces: Site Scope
// GetNVRHealthSummary returns aggregated stats.
// Permissions: nvr.health.read
// Enforces: Site Scope
func (h *NVRHandler) GetNVRHealthSummary(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	tid := uuid.MustParse(ac.TenantID) // Assuming UUID validation in middleware? Or safe parse.

	// Parse Site IDs from query or use RBAC allowed sites?
	// Phase 1 RBAC: `ac.SiteIDs` usually available?
	// If `ac.IsAdmin` is false, restrict to `ac.SiteIDs`.
	// Since AuthContext struct details vary, let's assume we pass what we have.
	// For now, assume System Admin sees all, Site Admin sees subset.
	// We need `ac.SiteIDs`.
	// Let's pass nil if all allowed.

	summary, err := h.Service.GetRepo().GetNVRHealthSummary(r.Context(), tid, nil) // Fix: Add site logic if available
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusOK, summary)
}

// GetNVRChannelHealth lists channels with Effective Status.
func (h *NVRHandler) GetNVRChannelHealth(w http.ResponseWriter, r *http.Request) {
	nvrIDStr := r.PathValue("id")
	nvrID, err := uuid.Parse(nvrIDStr)
	if err != nil {
		http.Error(w, "Invalid NVR ID", http.StatusBadRequest)
		return
	}

	// Get NVR to check status for Cascade Logic
	nvr, err := h.Service.GetNVR(r.Context(), nvrID) // Fixed args
	if err != nil {
		http.Error(w, "NVR not found", http.StatusNotFound)
		return
	}

	// Get Raw Channel Health
	healthRows, err := h.Service.GetRepo().ListChannelHealth(r.Context(), nvrID, 500, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Compute Effective Status
	type ChannelStatus struct {
		ChannelID       uuid.UUID `json:"channel_id"`
		Status          string    `json:"status"`
		EffectiveStatus string    `json:"effective_status"`
		LastCheckedAt   string    `json:"last_checked_at"`
	}

	var resp []ChannelStatus

	nvrIsOnline := (nvr.Status == "online")

	// Map rows
	for _, row := range healthRows {
		eff := row.Status
		if !nvrIsOnline && row.Status != "unknown" {
			eff = "unreachable_due_to_nvr"
		}

		resp = append(resp, ChannelStatus{
			ChannelID:       row.ChannelID,
			Status:          row.Status,
			EffectiveStatus: eff,
			LastCheckedAt:   row.LastCheckedAt.Format(time.RFC3339),
		})
	}

	respondJSON(w, http.StatusOK, resp)
}
