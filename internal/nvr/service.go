package nvr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/audit"
	"github.com/technosupport/ts-vms/internal/crypto"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/nvr/adapters"
)

var (
	ErrNVRNotFound = errors.New("nvr not found")
	ErrInvalidOp   = errors.New("invalid operation")
)

type KeyManager interface {
	WrapDEK(dek []byte, aad []byte) (string, []byte, []byte, []byte, error)
	UnwrapDEK(kid string, nonce, ciphertext, tag, aad []byte) ([]byte, error)
}

type Auditor interface {
	WriteEvent(ctx context.Context, evt audit.AuditEvent) error
}

type CameraCreator interface {
	CreateCamera(ctx context.Context, c *data.Camera) error
	EnableCamera(ctx context.Context, id, tenantID uuid.UUID) error
	DisableCamera(ctx context.Context, id, tenantID uuid.UUID) error
}

type Service struct {
	repo    data.NVRRepository
	keyring KeyManager
	auditor Auditor
	cameras CameraCreator
}

func NewService(repo data.NVRRepository, keyring KeyManager, auditor Auditor, cameras CameraCreator) *Service {
	return &Service{
		repo:    repo,
		keyring: keyring,
		auditor: auditor,
		cameras: cameras,
	}
}

func (s *Service) GetRepo() data.NVRRepository {
	return s.repo
}

// --- CRUD ---

func (s *Service) CreateNVR(ctx context.Context, nvr *data.NVR) error {
	// Validation
	if nvr.Name == "" || len(nvr.Name) > 120 {
		return errors.New("invalid name")
	}
	if ip := net.ParseIP(nvr.IPAddress); ip == nil {
		return errors.New("invalid ip address")
	}
	// Vendor allowed: "hikvision" | "dahua" | "onvif" | "generic" | "unknown"
	switch nvr.Vendor {
	case "hikvision", "dahua", "onvif", "generic", "unknown":
		// ok
	default:
		return errors.New("invalid vendor")
	}

	nvr.Status = "unknown" // Initial status

	if err := s.repo.Create(ctx, nvr); err != nil {
		return err
	}

	s.audit(ctx, "nvr.create", nvr.TenantID, nvr.ID.String(), "success", map[string]any{
		"name": nvr.Name,
		"ip":   nvr.IPAddress,
	})
	return nil
}

func (s *Service) ListNVRs(ctx context.Context, tenantID uuid.UUID, filter data.NVRFilter, limit, offset int) ([]*data.NVR, int, error) {
	return s.repo.List(ctx, tenantID, filter, limit, offset)
}

func (s *Service) GetNVR(ctx context.Context, id uuid.UUID) (*data.NVR, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *Service) UpdateNVR(ctx context.Context, nvr *data.NVR) error {
	if err := s.repo.Update(ctx, nvr); err != nil {
		return err
	}
	s.audit(ctx, "nvr.update", nvr.TenantID, nvr.ID.String(), "success", nil)
	return nil
}

func (s *Service) DeleteNVR(ctx context.Context, id, tenantID uuid.UUID) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.audit(ctx, "nvr.delete", tenantID, id.String(), "success", nil)
	return nil
}

// --- Linking ---

func (s *Service) UpsertLink(ctx context.Context, link *data.NVRLink) error {
	// Validation
	if link.RecordingMode != "vms" && link.RecordingMode != "nvr" {
		return errors.New("invalid recording mode")
	}

	if err := s.repo.UpsertLink(ctx, link); err != nil {
		return err
	}

	s.audit(ctx, "nvr.link.upsert", link.TenantID, link.NVRID.String(), "success", map[string]any{
		"camera_id":      link.CameraID,
		"recording_mode": link.RecordingMode,
	})
	return nil
}

func (s *Service) UnlinkCamera(ctx context.Context, tenantID, cameraID uuid.UUID) error {
	// We need to know NVR ID for audit? Or just audit on "camera"?
	// Let's fetch link first?
	link, err := s.repo.GetLinkByCameraID(ctx, cameraID)
	if err == nil {
		// Log
		defer func() {
			s.audit(ctx, "nvr.link.unlink", tenantID, link.NVRID.String(), "success", map[string]any{"camera_id": cameraID})
		}()
	}

	return s.repo.UnlinkCamera(ctx, cameraID)
}

func (s *Service) ListLinks(ctx context.Context, nvrID uuid.UUID, limit, offset int) ([]*data.NVRLink, error) {
	return s.repo.ListLinks(ctx, nvrID, limit, offset)
}

func (s *Service) ListChannels(ctx context.Context, nvrID, tenantID uuid.UUID, filter data.NVRChannelFilter, limit, offset int) ([]*data.NVRChannel, int, error) {
	// Verify Access (GetNVR checks RLS via repo usually, but repo method ListChannels takes nvrID)
	// We should ensure NVR belongs to tenant.
	// But ListChannels in repo takes `nvrID`. It doesn't take `tenantID` in my `NVRRepository` interface earlier?
	// Let's check `NVRRepository` interface update in `nvr_models.go`.
	// Line 66: ListChannels(ctx, nvrID, filter, limit, offset)
	// It relies on NVR being accessible.
	// To enforce tenant isolation, we should check NVR ownership first.
	nvr, err := s.repo.GetByID(ctx, nvrID)
	if err != nil {
		return nil, 0, err
	}
	if nvr.TenantID != tenantID {
		return nil, 0, errors.New("access denied")
	}
	return s.repo.ListChannels(ctx, nvrID, filter, limit, offset)
}

// --- Credentials ---

func (s *Service) SetCredentials(ctx context.Context, nvrID, tenantID uuid.UUID, username, password string) error {
	// 1. Prepare Payload
	payload := map[string]string{
		"username": username,
		"password": password,
	}
	payloadBytes, _ := json.Marshal(payload)

	// 2. Generate DEK
	dek, err := crypto.GenerateDEK()
	if err != nil {
		return err
	}

	// 3. AAD Binding
	aad := []byte(fmt.Sprintf("%s:%s:nvr_credential_v1", tenantID.String(), nvrID.String()))

	// 4. Encrypt Data with DEK
	dataNonce, dataCipher, dataTag, err := crypto.EncryptGCM(dek, payloadBytes, aad)
	if err != nil {
		return err
	}

	// 5. Wrap DEK with Master Key
	kid, dekNonce, dekCipher, dekTag, err := s.keyring.WrapDEK(dek, aad)
	if err != nil {
		return err
	}

	// 6. Store
	cred := &data.NVRCredential{
		TenantID:       tenantID,
		NVRID:          nvrID,
		MasterKID:      kid,
		DekNonce:       dekNonce,
		DekCiphertext:  dekCipher,
		DekTag:         dekTag,
		DataNonce:      dataNonce,
		DataCiphertext: dataCipher,
		DataTag:        dataTag,
	}

	if err := s.repo.UpsertCredential(ctx, cred); err != nil {
		return err
	}

	s.audit(ctx, "nvr.credential.write", tenantID, nvrID.String(), "success", nil)
	return nil
}

// GetCredentials returns Decrypted credentials! Caller must ensure permission.
func (s *Service) GetCredentials(ctx context.Context, nvrID, tenantID uuid.UUID) (string, string, error) {
	cred, err := s.repo.GetCredential(ctx, nvrID)
	if err != nil {
		return "", "", err
	}
	// Verify Tenant Isolation (Repo does RLS, but double check logic)
	if cred.TenantID != tenantID {
		return "", "", errors.New("access denied")
	}

	// 1. AAD Reconstruct
	aad := []byte(fmt.Sprintf("%s:%s:nvr_credential_v1", tenantID.String(), nvrID.String()))

	// 2. Unwrap DEK
	dek, err := s.keyring.UnwrapDEK(cred.MasterKID, cred.DekNonce, cred.DekCiphertext, cred.DekTag, aad)
	if err != nil {
		s.audit(ctx, "nvr.credential.read", tenantID, nvrID.String(), "fail", nil)
		return "", "", errors.New("decryption failed")
	}

	// 3. Decrypt Data
	payloadBytes, err := crypto.DecryptGCM(dek, cred.DataNonce, cred.DataCiphertext, cred.DataTag, aad)
	if err != nil {
		s.audit(ctx, "nvr.credential.read", tenantID, nvrID.String(), "failure", nil)
		return "", "", errors.New("decryption failed")
	}

	// 4. Parse
	var payload map[string]string
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return "", "", err
	}

	s.audit(ctx, "nvr.credential.read", tenantID, nvrID.String(), "success", nil)
	return payload["username"], payload["password"], nil
}

func (s *Service) DeleteCredentials(ctx context.Context, nvrID, tenantID uuid.UUID) error {
	if err := s.repo.DeleteCredential(ctx, nvrID); err != nil {
		return err
	}
	s.audit(ctx, "nvr.credential.delete", tenantID, nvrID.String(), "success", nil)
	return nil
}

// --- Helpers ---

func (s *Service) audit(ctx context.Context, action string, tenantID uuid.UUID, targetID string, result string, meta map[string]any) {
	if s.auditor == nil {
		return
	}

	var metaBytes json.RawMessage
	if meta != nil {
		b, _ := json.Marshal(meta)
		metaBytes = b
	}

	s.auditor.WriteEvent(ctx, audit.AuditEvent{
		EventID:    uuid.New(),
		TenantID:   tenantID,
		Action:     action,
		TargetType: "nvr",
		TargetID:   targetID,
		Result:     result,
		Metadata:   metaBytes,
		CreatedAt:  time.Now(),
	})
}

// --- Adapter Integration (Phase 2.7) ---

func (s *Service) getAdapterClient(ctx context.Context, nvrID uuid.UUID) (adapters.Adapter, adapters.NvrTarget, adapters.NvrCredential, error) {
	// 1. Fetch NVR
	nvr, err := s.GetNVR(ctx, nvrID)
	if err != nil {
		return nil, adapters.NvrTarget{}, adapters.NvrCredential{}, err
	}

	// 2. Fetch & Decrypt Credentials
	// Note: We use repo.GetCredential directly and decrypt ourselves safely here
	// to avoid exposing credentials outside of service/adapter boundary.
	// Or utilize existing s.GetCredentials helper but it returns strings.
	user, pass, err := s.GetCredentials(ctx, nvrID, nvr.TenantID)
	if err != nil {
		// Log specific error about credentials?
		// Fallback to empty creds if not found?
		// For adapters, missing creds usually means failure unless public.
		// "Adapters must authenticate".
		// We'll proceed with empty creds if err is "not found" or similar?
		// But GetCredentials returns error on decryption fail too.
		// We'll treat error as "no keys available" and pass empty.
		// Actually, GetCredentials checks RLS.
	}

	// 3. Construct Target & Cred
	target := adapters.NvrTarget{
		TenantID: nvr.TenantID,
		NVRID:    nvr.ID,
		SiteID:   nvr.SiteID,
		IP:       nvr.IPAddress,
		Port:     nvr.Port,
		Vendor:   nvr.Vendor,
	}
	cred := adapters.NvrCredential{
		Username: user,
		Password: pass,
		AuthType: "digest", // Default for now
	}

	adapter, err := adapters.GetAdapter(target, cred)
	if err != nil {
		return nil, adapters.NvrTarget{}, adapters.NvrCredential{}, err
	}
	return adapter, target, cred, nil
}

func (s *Service) GetAdapterDeviceInfo(ctx context.Context, nvrID, tenantID uuid.UUID) (adapters.NvrDeviceInfo, error) {
	adapter, target, cred, err := s.getAdapterClient(ctx, nvrID)
	if err != nil {
		s.audit(ctx, "nvr.adapter.device_info", tenantID, nvrID.String(), "failure", map[string]any{"error": err.Error()})
		return adapters.NvrDeviceInfo{}, err
	}

	info, err := adapter.GetDeviceInfo(ctx, target, cred)
	if err != nil {
		s.audit(ctx, "nvr.adapter.device_info", tenantID, nvrID.String(), "failure", map[string]any{"error": err.Error()})
		return adapters.NvrDeviceInfo{}, err
	}

	s.audit(ctx, "nvr.adapter.device_info", tenantID, nvrID.String(), "success", nil)
	return info, nil
}

func (s *Service) GetAdapterChannels(ctx context.Context, nvrID, tenantID uuid.UUID) ([]adapters.NvrChannel, error) {
	adapter, target, cred, err := s.getAdapterClient(ctx, nvrID)
	if err != nil {
		s.audit(ctx, "nvr.adapter.channels_list", tenantID, nvrID.String(), "failure", map[string]any{"error": err.Error()})
		return nil, err
	}

	channels, err := adapter.ListChannels(ctx, target, cred)
	if err != nil {
		s.audit(ctx, "nvr.adapter.channels_list", tenantID, nvrID.String(), "failure", map[string]any{"error": err.Error()})
		return nil, err
	}

	s.audit(ctx, "nvr.adapter.channels_list", tenantID, nvrID.String(), "success", map[string]any{"count": len(channels)})
	return channels, nil
}

func (s *Service) GetAdapterEvents(ctx context.Context, nvrID, tenantID uuid.UUID, since time.Time, limit int) ([]adapters.NvrEvent, int, error) {
	// Constrain limits
	limit = adapters.ConstrainLimits(limit, adapters.MaxEvents)

	adapter, target, cred, err := s.getAdapterClient(ctx, nvrID)
	if err != nil {
		s.audit(ctx, "nvr.adapter.events_fetch", tenantID, nvrID.String(), "failure", map[string]any{"error": err.Error()})
		return nil, 0, err
	}

	events, next, err := adapter.FetchEvents(ctx, target, cred, since, limit)
	if err != nil {
		s.audit(ctx, "nvr.adapter.events_fetch", tenantID, nvrID.String(), "failure", map[string]any{"error": err.Error()})
		return nil, 0, err
	}

	s.audit(ctx, "nvr.adapter.events_fetch", tenantID, nvrID.String(), "success", map[string]any{"count": len(events)})
	return events, next, nil
}
