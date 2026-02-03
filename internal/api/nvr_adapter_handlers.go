package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/middleware"
)

func (h *NVRHandler) GetAdapterDeviceInfo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	nvrID, err := uuid.Parse(id)
	if err != nil {
		http.Error(w, "invalid nvr id", http.StatusBadRequest)
		return
	}

	ac, _ := middleware.GetAuthContext(r.Context())
	tid := uuid.MustParse(ac.TenantID)

	info, err := h.Service.GetAdapterDeviceInfo(r.Context(), nvrID, tid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(info)
}

func (h *NVRHandler) GetAdapterChannels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	nvrID, err := uuid.Parse(id)
	if err != nil {
		http.Error(w, "invalid nvr id", http.StatusBadRequest)
		return
	}

	ac, _ := middleware.GetAuthContext(r.Context())
	tid := uuid.MustParse(ac.TenantID)

	channels, err := h.Service.GetAdapterChannels(r.Context(), nvrID, tid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"data":  channels,
		"total": len(channels),
	})
}

func (h *NVRHandler) GetAdapterEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	nvrID, err := uuid.Parse(id)
	if err != nil {
		http.Error(w, "invalid nvr id", http.StatusBadRequest)
		return
	}

	ac, _ := middleware.GetAuthContext(r.Context())
	tid := uuid.MustParse(ac.TenantID)

	// Parse query params
	sinceStr := r.URL.Query().Get("since")
	since := time.Now().Add(-1 * time.Hour) // Default last hour
	if sinceStr != "" {
		if t, err := time.Parse(time.RFC3339, sinceStr); err == nil {
			since = t
		}
	}

	// Limit
	limit := 50
	// ... could parse limit if needed

	events, next, err := h.Service.GetAdapterEvents(r.Context(), nvrID, tid, since, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"data":        events,
		"next_cursor": next,
	})
}
