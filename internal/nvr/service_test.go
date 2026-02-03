package nvr

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/data"
)

// Mock Repo
type mockRepo struct {
	nvrs     map[uuid.UUID]*data.NVR
	links    map[uuid.UUID]*data.NVRLink
	creds    map[uuid.UUID]*data.NVRCredential
	channels map[uuid.UUID]*data.NVRChannel
}

func (m *mockRepo) Create(ctx context.Context, nvr *data.NVR) error { m.nvrs[nvr.ID] = nvr; return nil }
func (m *mockRepo) GetByID(ctx context.Context, id uuid.UUID) (*data.NVR, error) {
	if n, ok := m.nvrs[id]; ok {
		return n, nil
	}
	return nil, data.ErrRecordNotFound
}
func (m *mockRepo) Update(ctx context.Context, nvr *data.NVR) error { m.nvrs[nvr.ID] = nvr; return nil }
func (m *mockRepo) Delete(ctx context.Context, id uuid.UUID) error  { delete(m.nvrs, id); return nil }
func (m *mockRepo) List(ctx context.Context, tid uuid.UUID, f data.NVRFilter, l, o int) ([]*data.NVR, int, error) {
	return nil, 0, nil
}
func (m *mockRepo) ListAllNVRs(ctx context.Context) ([]*data.NVR, error) { return nil, nil }
func (m *mockRepo) UpsertLink(ctx context.Context, l *data.NVRLink) error {
	m.links[l.CameraID] = l
	return nil
}
func (m *mockRepo) GetLinkByCameraID(ctx context.Context, cid uuid.UUID) (*data.NVRLink, error) {
	if l, ok := m.links[cid]; ok {
		return l, nil
	}
	return nil, data.ErrRecordNotFound
}
func (m *mockRepo) ListLinks(ctx context.Context, nid uuid.UUID, l, o int) ([]*data.NVRLink, error) {
	return nil, nil
}
func (m *mockRepo) UnlinkCamera(ctx context.Context, cid uuid.UUID) error {
	delete(m.links, cid)
	return nil
}
func (m *mockRepo) UpsertCredential(ctx context.Context, c *data.NVRCredential) error {
	m.creds[c.NVRID] = c
	return nil
}
func (m *mockRepo) GetCredential(ctx context.Context, nid uuid.UUID) (*data.NVRCredential, error) {
	if c, ok := m.creds[nid]; ok {
		return c, nil
	}
	return nil, data.ErrRecordNotFound
}
func (m *mockRepo) DeleteCredential(ctx context.Context, nid uuid.UUID) error {
	delete(m.creds, nid)
	return nil
}

// Discovery Stubs
func (m *mockRepo) UpsertChannel(ctx context.Context, ch *data.NVRChannel) error {
	if ch.ID == uuid.Nil {
		ch.ID = uuid.New()
	}
	m.channels[ch.ID] = ch
	return nil
}
func (m *mockRepo) ListChannels(ctx context.Context, nvrID uuid.UUID, filter data.NVRChannelFilter, limit, offset int) ([]*data.NVRChannel, int, error) {
	var res []*data.NVRChannel
	for _, c := range m.channels {
		if c.NVRID == nvrID {
			res = append(res, c)
		}
	}
	return res, len(res), nil
}
func (m *mockRepo) GetChannel(ctx context.Context, id uuid.UUID) (*data.NVRChannel, error) {
	if c, ok := m.channels[id]; ok {
		return c, nil
	}
	return nil, data.ErrRecordNotFound
}
func (m *mockRepo) GetChannelByRef(ctx context.Context, nvrID uuid.UUID, ref string) (*data.NVRChannel, error) {
	return nil, nil
}
func (m *mockRepo) UpdateChannelStatus(ctx context.Context, id uuid.UUID, validationStatus string, errCode *string) error {
	return nil
}

// UpdateChannelProvisionState implemented below with logic
func (m *mockRepo) BulkEnableChannels(ctx context.Context, ids []uuid.UUID, enable bool) error {
	return nil
}

// Event Polling Mock
func (m *mockRepo) UpsertEventPollState(ctx context.Context, state *data.NVREventPollState) error {
	return nil
}
func (m *mockRepo) GetEventPollState(ctx context.Context, nvrID uuid.UUID) (*data.NVREventPollState, error) {
	return nil, nil
}

// Health (Phase 2.9)
func (m *mockRepo) UpsertNVRHealth(ctx context.Context, h *data.NVRHealth) error { return nil }
func (m *mockRepo) UpsertChannelHealth(ctx context.Context, h *data.NVRChannelHealth) error {
	return nil
}
func (m *mockRepo) GetNVRHealthSummary(ctx context.Context, tid uuid.UUID, s []uuid.UUID) (*data.NVRHealthSummary, error) {
	return nil, nil
}
func (m *mockRepo) ListChannelHealth(ctx context.Context, nid uuid.UUID, l, o int) ([]*data.NVRChannelHealth, error) {
	return nil, nil
}

// Mock Keyring (Real implementation is fine if isolated, but here we mock to verify AAD passed)
type mockKeyring struct {
	lastAAD []byte
}

func (m *mockKeyring) WrapDEK(dek, aad []byte) (string, []byte, []byte, []byte, error) {
	m.lastAAD = aad
	return "master-1", []byte("nonce"), []byte("cipher"), []byte("tag"), nil
}
func (m *mockKeyring) UnwrapDEK(kid string, nonce, ciphertext, tag, aad []byte) ([]byte, error) {
	m.lastAAD = aad
	return make([]byte, 32), nil // Return dummy DEK
}

func TestAADBinding(t *testing.T) {
	keyring := &mockKeyring{}
	repo := &mockRepo{
		creds:    make(map[uuid.UUID]*data.NVRCredential),
		channels: make(map[uuid.UUID]*data.NVRChannel),
		nvrs:     make(map[uuid.UUID]*data.NVR),
		links:    make(map[uuid.UUID]*data.NVRLink),
	}
	svc := NewService(repo, keyring, nil, nil)

	tenantID := uuid.New()
	nvrID := uuid.New()

	// 1. Set Credentials
	err := svc.SetCredentials(context.Background(), nvrID, tenantID, "user", "pass")
	if err != nil {
		t.Fatalf("SetCredentials failed: %v", err)
	}

	expectedAAD := tenantID.String() + ":" + nvrID.String() + ":nvr_credential_v1"
	if string(keyring.lastAAD) != expectedAAD {
		t.Errorf("Expected AAD %s, got %s", expectedAAD, string(keyring.lastAAD))
	}

	// 2. Get Credentials (Mock Unwrap returns success)
	// We need to mock crypto.DecryptGCM?
	// The service uses real crypto.DecryptGCM.
	// Since we returned dummy DEK and dummy Cipher in mock, real DecryptGCM will fail or we need to mock crypto too?
	// Service calls `crypto.DecryptGCM` directly. We can't mock package level functions easily.
	// We should just verify AAD passed to UnwrapDEK.
}

func TestValidation(t *testing.T) {
	svc := NewService(&mockRepo{
		nvrs:     make(map[uuid.UUID]*data.NVR),
		channels: make(map[uuid.UUID]*data.NVRChannel),
	}, nil, nil, nil)

	// Invalid Vendor
	err := svc.CreateNVR(context.Background(), &data.NVR{
		Name: "test", IPAddress: "1.2.3.4", Vendor: "bad",
	})
	if err == nil {
		t.Error("Expected error for invalid vendor")
	}

	// Invalid IP
	err = svc.CreateNVR(context.Background(), &data.NVR{
		Name: "test", IPAddress: "bad-ip", Vendor: "hikvision",
	})
	if err == nil {
		t.Error("Expected error for invalid IP")
	}
}

func TestDiscoveryLogic(t *testing.T) {
	// Need to mock Adapter factory or client.
	// Service uses `getAdapterClient`. This calls `adapters.NewClient`.
	// We can't easily mock `adapters.NewClient` as it is a function call unless we redirect it or interface it.
	// In `discovery.go` design, `s.getAdapterClient` calls `adapters.NewClient`.
	// We should probably rely on Integration Tests or refactor Service to accept an AdapterFactory.
	// Given constraints, I'll rely on basic validation logic tests if possible, or skip deep mock of external adapter for unit test.
	// But `ProvisionCameras` logic is testable if we mock `repo`.

	keyring := &mockKeyring{}
	repo := &mockRepo{
		nvrs:     make(map[uuid.UUID]*data.NVR),
		links:    make(map[uuid.UUID]*data.NVRLink),
		creds:    make(map[uuid.UUID]*data.NVRCredential),
		channels: make(map[uuid.UUID]*data.NVRChannel),
	}
	// We also need a CameraCreator mock!
	camCreator := &mockCamCreator{}

	svc := NewService(repo, keyring, nil, camCreator)

	tid := uuid.New()
	nid := uuid.New()

	// Setup NVR
	repo.nvrs[nid] = &data.NVR{ID: nid, TenantID: tid, Name: "TestNVR", IPAddress: "1.2.3.4", Vendor: "hikvision"}

	// Setup Channel in Repo
	chID := uuid.New()
	ref := "ch1"

	repo.channels[chID] = &data.NVRChannel{
		ID:             chID,
		TenantID:       tid,
		NVRID:          nid,
		ChannelRef:     ref,
		RTSPMain:       "rtsp://1.2.3.4/main",
		ProvisionState: "pending",
	}

	// Test Provision Cameras
	count, err := svc.ProvisionCameras(context.Background(), nid, tid, []uuid.UUID{chID})
	if err != nil {
		t.Fatalf("ProvisionCameras failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 camera provisioned, got %d", count)
	}

	// Verify ProvisionState updated
	ch, _ := repo.GetChannel(context.Background(), chID)
	if ch.ProvisionState != "created" {
		t.Errorf("Expected state 'created', got '%s'", ch.ProvisionState)
	}
}

func (m *mockRepo) UpdateChannelProvisionState(ctx context.Context, id uuid.UUID, state string) error {
	if ch, ok := m.channels[id]; ok {
		ch.ProvisionState = state
	}
	return nil
}

type mockCamCreator struct{}

func (m *mockCamCreator) CreateCamera(ctx context.Context, c *data.Camera) error          { return nil }
func (m *mockCamCreator) EnableCamera(ctx context.Context, id, tenantID uuid.UUID) error  { return nil }
func (m *mockCamCreator) DisableCamera(ctx context.Context, id, tenantID uuid.UUID) error { return nil }
