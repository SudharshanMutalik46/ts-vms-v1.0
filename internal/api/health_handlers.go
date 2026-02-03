package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/health"
	"github.com/technosupport/ts-vms/internal/middleware"
)

type HealthHandler struct {
	Service *health.Service
}

func NewHealthHandler(svc *health.Service) *HealthHandler {
	return &HealthHandler{Service: svc}
}

func (h *HealthHandler) GetHealth(w http.ResponseWriter, r *http.Request) {
	// 1. Tenant ID
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		http.Error(w, "no auth context", http.StatusUnauthorized)
		return
	}
	tenantID, _ := uuid.Parse(ac.TenantID)

	// 2. Call Service (Repo)
	// We need to add ListHealth to Service or call Repo via accessor?
	// Service wrapper is better.
	statuses, err := h.Service.Repo.ListStatuses(r.Context(), tenantID)
	if err != nil {
		http.Error(w, "failed to list health", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(statuses)
}

func (h *HealthHandler) GetCameraHealth(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	cameraID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid camera id", http.StatusBadRequest)
		return
	}

	status, err := h.Service.GetStatus(r.Context(), cameraID)
	if err != nil {
		http.Error(w, "failed to get status", http.StatusInternalServerError)
		return
	}
	if status == nil {
		http.Error(w, "status not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (h *HealthHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	cameraID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid camera id", http.StatusBadRequest)
		return
	}

	limit := 50
	offset := 0

	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			limit = v
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil {
			offset = v
		}
	}

	history, err := h.Service.GetHistory(r.Context(), cameraID, limit, offset)
	if err != nil {
		http.Error(w, "failed to get history", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

func (h *HealthHandler) ListAlerts(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		http.Error(w, "no auth context", http.StatusUnauthorized)
		return
	}
	tenantID, _ := uuid.Parse(ac.TenantID)

	state := r.URL.Query().Get("state") // e.g., "open"

	alerts, err := h.Service.ListAlerts(r.Context(), tenantID, state)
	if err != nil {
		http.Error(w, "failed to list alerts", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(alerts)
}

func (h *HealthHandler) ManualRecheck(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		http.Error(w, "no auth context", http.StatusUnauthorized)
		return
	}
	tenantID, _ := uuid.Parse(ac.TenantID)

	idStr := r.PathValue("id")
	cameraID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid camera id", http.StatusBadRequest)
		return
	}

	// Permission Check? (RBAC middleware handles route access, but we might want "camera.health.recheck" specific check here if not generic)
	// Assuming generic "manage" or specific permission middleware on route.

	if err := h.Service.ManualCheck(r.Context(), tenantID, cameraID); err != nil {
		http.Error(w, "failed to trigger check", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"status":"recheck_triggered"}`))
}
