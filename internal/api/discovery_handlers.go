package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/discovery"
	"github.com/technosupport/ts-vms/internal/middleware"
)

type DiscoveryHandler struct {
	Service *discovery.Service
	Perms   PermissionChecker
}

func NewDiscoveryHandler(svc *discovery.Service, perms PermissionChecker) *DiscoveryHandler {
	return &DiscoveryHandler{Service: svc, Perms: perms}
}

// POST /api/v1/onvif/credentials
func (h *DiscoveryHandler) CreateCredential(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if allowed, _ := h.Perms.CheckPermission(r.Context(), "onvif.discovery.run", "tenant", ac.TenantID); !allowed {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	id, err := h.Service.CreateBootstrapCredential(r.Context(), uuid.MustParse(ac.TenantID), req.Username, req.Password)
	if err != nil {
		http.Error(w, "Failed to create credential", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"id": id.String()})
}

// POST /api/v1/onvif/discovery-runs
func (h *DiscoveryHandler) StartRun(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		SiteID string `json:"site_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // optional

	if req.SiteID != "" {
		if allowed, _ := h.Perms.CheckPermission(r.Context(), "onvif.discovery.run", "site", req.SiteID); !allowed {
			http.Error(w, "Forbidden (Site)", http.StatusForbidden)
			return
		}
	} else {
		if allowed, _ := h.Perms.CheckPermission(r.Context(), "onvif.discovery.run", "tenant", ac.TenantID); !allowed {
			http.Error(w, "Forbidden (Tenant)", http.StatusForbidden)
			return
		}
	}

	var siteUUID *uuid.UUID
	if req.SiteID != "" {
		id := uuid.MustParse(req.SiteID)
		siteUUID = &id
	}

	id, err := h.Service.StartDiscovery(r.Context(), uuid.MustParse(ac.TenantID), siteUUID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"id": id.String()})
}

// GET /api/v1/onvif/discovery-runs/{id}
func (h *DiscoveryHandler) GetRun(w http.ResponseWriter, r *http.Request) {
	runID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	run, ok := h.checkRunAccess(w, r, runID, "onvif.discovery.read")
	if !ok {
		return
	}

	json.NewEncoder(w).Encode(run)
}

// GET /api/v1/onvif/discovered-devices?discovery_run_id=...
func (h *DiscoveryHandler) ListDevices(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	runIDStr := r.URL.Query().Get("discovery_run_id")
	if runIDStr == "" {
		http.Error(w, "discovery_run_id required", http.StatusBadRequest)
		return
	}

	runID, err := uuid.Parse(runIDStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	_, ok = h.checkRunAccess(w, r, runID, "onvif.discovery.read")
	if !ok {
		return
	}

	list, err := h.Service.ListDevices(r.Context(), runID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Defensive tenant filter in case repo behavior changes later.
	tenantID := uuid.MustParse(ac.TenantID)
	out := make([]*data.DiscoveredDevice, 0, len(list))
	for _, d := range list {
		if d != nil && d.TenantID == tenantID {
			out = append(out, d)
		}
	}

	json.NewEncoder(w).Encode(out)
}

// POST /api/v1/onvif/discovered-devices/{id}:probe
func (h *DiscoveryHandler) ProbeDevice(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	devID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var req struct {
		CredentialID string `json:"credential_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	credID, err := uuid.Parse(req.CredentialID)
	if err != nil {
		http.Error(w, "Invalid Credential ID", http.StatusBadRequest)
		return
	}

	_, run, ok := h.checkDeviceAccess(w, r, devID, "onvif.discovery.probe")
	if !ok {
		return
	}

	// Safety: if the run is site-scoped, probe permission must already have been
	// checked against that site in checkDeviceAccess().
	_ = run

	err = h.Service.ProbeDevice(r.Context(), devID, credID, uuid.MustParse(ac.TenantID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *DiscoveryHandler) checkRunAccess(
	w http.ResponseWriter,
	r *http.Request,
	runID uuid.UUID,
	permission string,
) (*data.DiscoveryRun, bool) {
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return nil, false
	}

	run, err := h.Service.GetRun(r.Context(), runID)
	if err != nil || run == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return nil, false
	}

	// Non-enumeration: tenant mismatch also returns 404.
	if run.TenantID.String() != ac.TenantID {
		http.Error(w, "Not Found", http.StatusNotFound)
		return nil, false
	}

	scopeType := "tenant"
	scopeID := ac.TenantID
	if run.SiteID != nil && *run.SiteID != uuid.Nil {
		scopeType = "site"
		scopeID = run.SiteID.String()
	}

	allowed, err := h.Perms.CheckPermission(r.Context(), permission, scopeType, scopeID)
	if err != nil || !allowed {
		http.Error(w, "Not Found", http.StatusNotFound)
		return nil, false
	}

	return run, true
}

func (h *DiscoveryHandler) checkDeviceAccess(
	w http.ResponseWriter,
	r *http.Request,
	deviceID uuid.UUID,
	permission string,
) (*data.DiscoveredDevice, *data.DiscoveryRun, bool) {
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return nil, nil, false
	}

	dev, err := h.Service.GetDevice(r.Context(), deviceID)
	if err != nil || dev == nil {
		http.Error(w, "Device Not Found", http.StatusNotFound)
		return nil, nil, false
	}

	if dev.TenantID.String() != ac.TenantID {
		http.Error(w, "Device Not Found", http.StatusNotFound)
		return nil, nil, false
	}

	run, err := h.Service.GetRun(r.Context(), dev.DiscoveryRunID)
	if err != nil || run == nil {
		http.Error(w, "Device Not Found", http.StatusNotFound)
		return nil, nil, false
	}

	if run.TenantID.String() != ac.TenantID {
		http.Error(w, "Device Not Found", http.StatusNotFound)
		return nil, nil, false
	}

	scopeType := "tenant"
	scopeID := ac.TenantID
	if run.SiteID != nil && *run.SiteID != uuid.Nil {
		scopeType = "site"
		scopeID = run.SiteID.String()
	}

	allowed, err := h.Perms.CheckPermission(r.Context(), permission, scopeType, scopeID)
	if err != nil || !allowed {
		http.Error(w, "Device Not Found", http.StatusNotFound)
		return nil, nil, false
	}

	return dev, run, true
}
