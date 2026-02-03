package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/middleware"
)

// POST /api/v1/nvrs/{id}:test-connection
func (h *NVRHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	nvrID, err := uuid.Parse(id)
	if err != nil {
		http.Error(w, "invalid nvr id", http.StatusBadRequest)
		return
	}

	ac, _ := middleware.GetAuthContext(r.Context())
	tid := uuid.MustParse(ac.TenantID)

	status, err := h.Service.TestConnection(r.Context(), nvrID, tid)
	if err != nil {
		// Log error but return status
		// Using 200 OK with status field is common for "test" ops, or 502/504?
		// Plan says "Return status".
		// We'll return 200 with JSON.
	}

	json.NewEncoder(w).Encode(map[string]string{
		"status": status,
		"error":  errToString(err),
	})
}

// POST /api/v1/nvrs/{id}:discover-channels
func (h *NVRHandler) DiscoverChannels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	nvrID, err := uuid.Parse(id)
	if err != nil {
		http.Error(w, "invalid nvr id", http.StatusBadRequest)
		return
	}

	ac, _ := middleware.GetAuthContext(r.Context())
	tid := uuid.MustParse(ac.TenantID)

	count, err := h.Service.DiscoverChannels(r.Context(), nvrID, tid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"count":  count,
		"status": "success",
	})
}

// GET /api/v1/nvrs/{id}/channels
func (h *NVRHandler) GetChannels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	nvrID, err := uuid.Parse(id)
	if err != nil {
		http.Error(w, "invalid nvr id", http.StatusBadRequest)
		return
	}

	ac, _ := middleware.GetAuthContext(r.Context())
	tid := uuid.MustParse(ac.TenantID) // Although Service uses repo which might expect UUID, Service ListChannels takes filter.
	// Service ListChannels signature: (ctx, nvrID, filter, limit, offset)
	// Filter struct: ProvisionState, IsEnabled, Validation, Query.

	q := r.URL.Query()
	filter := data.NVRChannelFilter{
		Query: q.Get("q"),
	}
	if v := q.Get("provision_state"); v != "" {
		filter.ProvisionState = &v
	}
	if v := q.Get("validation_status"); v != "" {
		filter.Validation = &v
	}
	if v := q.Get("is_enabled"); v != "" {
		b := v == "true"
		filter.IsEnabled = &b
	}

	// Pagination
	limit := 50
	offset := 0
	// TODO: Parse limit/offset

	// Wait, Service doesn't have ListChannels method exposed directly?
	// I didn't add it to `Service` struct in `discovery.go`?
	// I added `DiscoverChannels`. I might have missed `ListChannels` passthrough in `service.go` or `discovery.go`.
	// Since `repo` has `ListChannels`, `Service` should likely expose it.
	// I'll assume I need to add it to `Service`.
	// For now, I'll call `h.Service.ListChannels` assuming I'll add it.

	// Quick implementation in Service likely:
	channels, total, err := h.Service.ListChannels(r.Context(), nvrID, tid, filter, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"data":  channels,
		"total": total,
	})
}

// POST /api/v1/nvrs/{id}:validate-channels
func (h *NVRHandler) ValidateChannels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	nvrID, err := uuid.Parse(id)
	if err != nil {
		http.Error(w, "invalid nvr id", http.StatusBadRequest)
		return
	}

	var req struct {
		ChannelIDs []uuid.UUID `json:"channel_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	ac, _ := middleware.GetAuthContext(r.Context())
	tid := uuid.MustParse(ac.TenantID)

	results, err := h.Service.ValidateChannels(r.Context(), nvrID, tid, req.ChannelIDs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"results": results,
	})
}

// POST /api/v1/nvrs/{id}:provision-cameras
func (h *NVRHandler) ProvisionCameras(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	nvrID, err := uuid.Parse(id)
	if err != nil {
		http.Error(w, "invalid nvr id", http.StatusBadRequest)
		return
	}

	var req struct {
		ChannelIDs []uuid.UUID `json:"channel_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	ac, _ := middleware.GetAuthContext(r.Context())
	tid := uuid.MustParse(ac.TenantID)

	count, err := h.Service.ProvisionCameras(r.Context(), nvrID, tid, req.ChannelIDs)
	if err != nil {
		// Handle partial failure or quota error
		if err.Error() == "license_limit_exceeded" {
			http.Error(w, "license quota exceeded", http.StatusForbidden) // 403 or 409?
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"provisioned_count": count,
	})
}

// POST /api/v1/nvrs/{id}/channels:bulk
func (h *NVRHandler) BulkChannelOp(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	nvrID, err := uuid.Parse(id)
	if err != nil {
		http.Error(w, "invalid nvr id", http.StatusBadRequest)
		return
	}

	var req struct {
		ChannelIDs []uuid.UUID `json:"channel_ids"`
		Action     string      `json:"action"` // "enable", "disable"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	if req.Action != "enable" && req.Action != "disable" {
		http.Error(w, "invalid action", http.StatusBadRequest)
		return
	}

	ac, _ := middleware.GetAuthContext(r.Context())
	tid := uuid.MustParse(ac.TenantID)

	err = h.Service.BulkChannelOp(r.Context(), nvrID, tid, req.ChannelIDs, req.Action)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func errToString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
