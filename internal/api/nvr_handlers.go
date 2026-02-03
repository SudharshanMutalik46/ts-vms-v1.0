package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/middleware"
	"github.com/technosupport/ts-vms/internal/nvr"
)

type NVRHandler struct {
	Service *nvr.Service
}

func NewNVRHandler(service *nvr.Service) *NVRHandler {
	return &NVRHandler{Service: service}
}

// --- Requests ---
type CreateNVRRequest struct {
	SiteID    string `json:"site_id"`
	Name      string `json:"name"`
	Vendor    string `json:"vendor"`
	IPAddress string `json:"ip_address"`
	Port      int    `json:"port"`
	IsEnabled bool   `json:"is_enabled,omitempty"`
}

type UpdateNVRRequest struct {
	Name      string `json:"name,omitempty"`
	Vendor    string `json:"vendor,omitempty"`
	IPAddress string `json:"ip_address,omitempty"`
	Port      int    `json:"port,omitempty"`
	IsEnabled *bool  `json:"is_enabled,omitempty"`
	Status    string `json:"status,omitempty"` // Manual override
}

type UpsertLinkRequest struct {
	Links []struct {
		CameraID      string `json:"camera_id"`
		NVRChannelRef string `json:"nvr_channel_ref,omitempty"`
		RecordingMode string `json:"recording_mode"`
		IsEnabled     bool   `json:"is_enabled,omitempty"` // default true
	} `json:"links"`
}

type SetCredentialsRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type UnlinkRequest struct {
	CameraIDs []string `json:"camera_ids"`
}

// --- Handlers ---

func (h *NVRHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateNVRRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	ac, _ := middleware.GetAuthContext(r.Context())
	tid := ac.TenantID
	siteID, _ := uuid.Parse(req.SiteID)

	n := &data.NVR{
		TenantID:  uuid.MustParse(tid),
		SiteID:    siteID,
		Name:      req.Name,
		Vendor:    req.Vendor,
		IPAddress: req.IPAddress,
		Port:      req.Port,
		IsEnabled: true, // default
	}
	if req.Port == 0 {
		n.Port = 80
	}

	if err := h.Service.CreateNVR(r.Context(), n); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(n)
}

func (h *NVRHandler) List(w http.ResponseWriter, r *http.Request) {
	tid := r.Context().Value("tenant_id").(string)

	filter := data.NVRFilter{
		Query: r.URL.Query().Get("q"),
	}
	if s := r.URL.Query().Get("site_id"); s != "" {
		uid := uuid.MustParse(s)
		filter.SiteID = &uid
	}
	if s := r.URL.Query().Get("vendor"); s != "" {
		filter.Vendor = &s
	}
	if s := r.URL.Query().Get("status"); s != "" {
		filter.Status = &s
	}

	nvrs, total, err := h.Service.ListNVRs(r.Context(), uuid.MustParse(tid), filter, 50, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{"data": nvrs, "total": total})
}

func (h *NVRHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	nvr, err := h.Service.GetNVR(r.Context(), uuid.MustParse(id))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(nvr)
}

func (h *NVRHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	nvr, err := h.Service.GetNVR(r.Context(), uuid.MustParse(id))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	var req UpdateNVRRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	if req.Name != "" {
		nvr.Name = req.Name
	}
	if req.Vendor != "" {
		nvr.Vendor = req.Vendor
	}
	if req.IPAddress != "" {
		nvr.IPAddress = req.IPAddress
	}
	if req.Port != 0 {
		nvr.Port = req.Port
	}
	if req.IsEnabled != nil {
		nvr.IsEnabled = *req.IsEnabled
	}
	if req.Status != "" {
		nvr.Status = req.Status
	}

	if err := h.Service.UpdateNVR(r.Context(), nvr); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(nvr)
}

func (h *NVRHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ac, _ := middleware.GetAuthContext(r.Context())
	tid := ac.TenantID

	if err := h.Service.DeleteNVR(r.Context(), uuid.MustParse(id), uuid.MustParse(tid)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Link Handlers ---

func (h *NVRHandler) UpsertLinks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ac, _ := middleware.GetAuthContext(r.Context())
	tid := ac.TenantID
	nvrID := uuid.MustParse(id)

	var req UpsertLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	if len(req.Links) > 200 {
		http.Error(w, "too many items", http.StatusRequestEntityTooLarge)
		return
	}

	for _, l := range req.Links {
		link := &data.NVRLink{
			TenantID:      uuid.MustParse(tid),
			NVRID:         nvrID,
			CameraID:      uuid.MustParse(l.CameraID),
			RecordingMode: l.RecordingMode,
			IsEnabled:     true,
		}
		if l.NVRChannelRef != "" {
			link.NVRChannelRef = &l.NVRChannelRef
		}

		if err := h.Service.UpsertLink(r.Context(), link); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return // Partial failure stops? Or should we try all?
			// Ideally bulk operation should be all or nothing or return errors.
			// Currently returning 500 on first error.
		}
	}
	w.WriteHeader(http.StatusOK)
}

func (h *NVRHandler) ListLinks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	links, err := h.Service.ListLinks(r.Context(), uuid.MustParse(id), 50, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(links)
}

func (h *NVRHandler) Unlink(w http.ResponseWriter, r *http.Request) {
	// Body: camera_ids
	var req UnlinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	ac, _ := middleware.GetAuthContext(r.Context())
	tid := ac.TenantID

	for _, camID := range req.CameraIDs {
		if err := h.Service.UnlinkCamera(r.Context(), uuid.MustParse(tid), uuid.MustParse(camID)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

// --- Credential Handlers ---

func (h *NVRHandler) SetCredentials(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ac, _ := middleware.GetAuthContext(r.Context())
	tid := ac.TenantID

	var req SetCredentialsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	err := h.Service.SetCredentials(r.Context(), uuid.MustParse(id), uuid.MustParse(tid), req.Username, req.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *NVRHandler) GetCredentials(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ac, _ := middleware.GetAuthContext(r.Context())
	tid := ac.TenantID

	u, p, err := h.Service.GetCredentials(r.Context(), uuid.MustParse(id), uuid.MustParse(tid))
	if err != nil {
		// Logged in service audit as fail
		http.Error(w, "failed to get credentials", http.StatusForbidden)
		return
	}

	// Return decrypted?
	// The prompt allowed returning decrypted if permissioned.
	// RBAC middleware ensures "nvr.credential.read".
	json.NewEncoder(w).Encode(map[string]string{"username": u, "password": p})
}

func (h *NVRHandler) DeleteCredentials(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ac, _ := middleware.GetAuthContext(r.Context())
	tid := ac.TenantID

	if err := h.Service.DeleteCredentials(r.Context(), uuid.MustParse(id), uuid.MustParse(tid)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
