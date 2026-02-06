package health

import (
	"context"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/data"
)

type MockNVRRepo struct{}

// This line forces compile-time check:
var _ data.NVRRepository = (*MockNVRRepo)(nil)

func (m *MockNVRRepo) Create(ctx context.Context, nvr *data.NVR) error              { return nil }
func (m *MockNVRRepo) GetByID(ctx context.Context, id uuid.UUID) (*data.NVR, error) { return nil, nil }
func (m *MockNVRRepo) List(ctx context.Context, tenantID uuid.UUID, filter data.NVRFilter, limit, offset int) ([]*data.NVR, int, error) {
	return nil, 0, nil
}
func (m *MockNVRRepo) ListAllNVRs(ctx context.Context) ([]*data.NVR, error) { return nil, nil }
func (m *MockNVRRepo) Update(ctx context.Context, nvr *data.NVR) error      { return nil }
func (m *MockNVRRepo) Delete(ctx context.Context, id uuid.UUID) error       { return nil }

func (m *MockNVRRepo) UpsertChannel(ctx context.Context, ch *data.NVRChannel) error { return nil }
func (m *MockNVRRepo) ListChannels(ctx context.Context, nvrID uuid.UUID, filter data.NVRChannelFilter, limit, offset int) ([]*data.NVRChannel, int, error) {
	return nil, 0, nil
}
func (m *MockNVRRepo) GetChannel(ctx context.Context, id uuid.UUID) (*data.NVRChannel, error) {
	return nil, nil
}
func (m *MockNVRRepo) GetChannelByRef(ctx context.Context, nvrID uuid.UUID, ref string) (*data.NVRChannel, error) {
	return nil, nil
}
func (m *MockNVRRepo) UpdateChannelStatus(ctx context.Context, id uuid.UUID, validationStatus string, errCode *string) error {
	return nil
}
func (m *MockNVRRepo) UpdateChannelProvisionState(ctx context.Context, id uuid.UUID, state string) error {
	return nil
}
func (m *MockNVRRepo) BulkEnableChannels(ctx context.Context, ids []uuid.UUID, enable bool) error {
	return nil
}

func (m *MockNVRRepo) UpsertLink(ctx context.Context, link *data.NVRLink) error { return nil }
func (m *MockNVRRepo) GetLinkByCameraID(ctx context.Context, cameraID uuid.UUID) (*data.NVRLink, error) {
	return nil, nil
}
func (m *MockNVRRepo) ListLinks(ctx context.Context, nvrID uuid.UUID, limit, offset int) ([]*data.NVRLink, error) {
	return nil, nil
}
func (m *MockNVRRepo) UnlinkCamera(ctx context.Context, cameraID uuid.UUID) error { return nil }

func (m *MockNVRRepo) UpsertCredential(ctx context.Context, cred *data.NVRCredential) error {
	return nil
}
func (m *MockNVRRepo) GetCredential(ctx context.Context, nvrID uuid.UUID) (*data.NVRCredential, error) {
	return nil, nil
}
func (m *MockNVRRepo) DeleteCredential(ctx context.Context, nvrID uuid.UUID) error { return nil }

func (m *MockNVRRepo) UpsertNVRHealth(ctx context.Context, h *data.NVRHealth) error { return nil }
func (m *MockNVRRepo) UpsertChannelHealth(ctx context.Context, h *data.NVRChannelHealth) error {
	return nil
}
func (m *MockNVRRepo) GetNVRHealthSummary(ctx context.Context, tenantID uuid.UUID, siteIDs []uuid.UUID) (*data.NVRHealthSummary, error) {
	return nil, nil
}
func (m *MockNVRRepo) ListChannelHealth(ctx context.Context, nvrID uuid.UUID, limit, offset int) ([]*data.NVRChannelHealth, error) {
	return nil, nil
}

func (m *MockNVRRepo) UpsertEventPollState(ctx context.Context, state *data.NVREventPollState) error {
	return nil
}
func (m *MockNVRRepo) GetEventPollState(ctx context.Context, nvrID uuid.UUID) (*data.NVREventPollState, error) {
	return nil, nil
}
