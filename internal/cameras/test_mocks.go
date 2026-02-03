package cameras

import (
	"context"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/audit"
	"github.com/technosupport/ts-vms/internal/data"
)

// MockAuditor
type MockAuditor struct {
	Events []audit.AuditEvent
}

func (m *MockAuditor) WriteEvent(ctx context.Context, evt audit.AuditEvent) error {
	m.Events = append(m.Events, evt)
	return nil
}

// MockCredentialProvider
type MockCredentialProvider struct {
	GetFunc func(ctx context.Context, tenantID, cameraID uuid.UUID, reveal bool) (*CredentialOutput, bool, error)
}

func (m *MockCredentialProvider) GetCredentials(ctx context.Context, tenantID, cameraID uuid.UUID, reveal bool) (*CredentialOutput, bool, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, tenantID, cameraID, reveal)
	}
	return nil, false, nil
}

// MockMediaRepo (Partial)
type MockMediaRepo struct {
	UpsertProfileFunc          func(ctx context.Context, p *data.CameraMediaProfile) error
	UpsertSelectionFunc        func(ctx context.Context, s *data.CameraStreamSelection) error
	GetSelectionFunc           func(ctx context.Context, cameraID uuid.UUID) (*data.CameraStreamSelection, error)
	GetValidationResultsFunc   func(ctx context.Context, cameraID uuid.UUID) ([]*data.RTSPValidationResult, error)
	UpsertValidationResultFunc func(ctx context.Context, res *data.RTSPValidationResult) error
	ListProfilesFunc           func(ctx context.Context, cameraID uuid.UUID) ([]*data.CameraMediaProfile, error)
}

func (m *MockMediaRepo) UpsertProfile(ctx context.Context, p *data.CameraMediaProfile) error {
	if m.UpsertProfileFunc != nil {
		return m.UpsertProfileFunc(ctx, p)
	}
	return nil
}
func (m *MockMediaRepo) UpsertSelection(ctx context.Context, s *data.CameraStreamSelection) error {
	if m.UpsertSelectionFunc != nil {
		return m.UpsertSelectionFunc(ctx, s)
	}
	return nil
}
func (m *MockMediaRepo) GetSelection(ctx context.Context, cameraID uuid.UUID) (*data.CameraStreamSelection, error) {
	if m.GetSelectionFunc != nil {
		return m.GetSelectionFunc(ctx, cameraID)
	}
	return nil, nil // Not found
}
func (m *MockMediaRepo) GetValidationResults(ctx context.Context, cameraID uuid.UUID) ([]*data.RTSPValidationResult, error) {
	if m.GetValidationResultsFunc != nil {
		return m.GetValidationResultsFunc(ctx, cameraID)
	}
	return nil, nil
}
func (m *MockMediaRepo) UpsertValidationResult(ctx context.Context, res *data.RTSPValidationResult) error {
	if m.UpsertValidationResultFunc != nil {
		return m.UpsertValidationResultFunc(ctx, res)
	}
	return nil
}
func (m *MockMediaRepo) ListProfiles(ctx context.Context, cameraID uuid.UUID) ([]*data.CameraMediaProfile, error) {
	if m.ListProfilesFunc != nil {
		return m.ListProfilesFunc(ctx, cameraID)
	}
	return nil, nil
}

// MockCameraRepo
type MockCameraRepo struct {
	GetByIDFunc func(ctx context.Context, id uuid.UUID) (*data.Camera, error)
	// Add other methods of Repository interface as stubs
}

func (m *MockCameraRepo) Create(ctx context.Context, c *data.Camera) error { return nil }
func (m *MockCameraRepo) GetByID(ctx context.Context, id uuid.UUID) (*data.Camera, error) {
	if m.GetByIDFunc != nil {
		return m.GetByIDFunc(ctx, id)
	}
	return nil, nil
}
func (m *MockCameraRepo) Update(ctx context.Context, c *data.Camera) error { return nil }
func (m *MockCameraRepo) SetStatus(ctx context.Context, id, tenantID uuid.UUID, enabled bool) error {
	return nil
}
func (m *MockCameraRepo) SoftDelete(ctx context.Context, id, tenantID uuid.UUID) error { return nil }
func (m *MockCameraRepo) CountAll(ctx context.Context, tenantID uuid.UUID) (int, error) {
	return 0, nil
}
func (m *MockCameraRepo) BulkUpdateStatus(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID, enabled bool) error {
	return nil
}
func (m *MockCameraRepo) BulkAddTags(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID, tags []string) error {
	return nil
}
func (m *MockCameraRepo) BulkRemoveTags(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID, tags []string) error {
	return nil
}
func (m *MockCameraRepo) List(ctx context.Context, tenantID uuid.UUID, filter data.CameraFilter, limit, offset int) ([]*data.Camera, int, error) {
	return nil, 0, nil
}
func (m *MockCameraRepo) CreateGroup(ctx context.Context, g *data.CameraGroup) error { return nil }
func (m *MockCameraRepo) ListGroups(ctx context.Context, tenantID uuid.UUID) ([]*data.CameraGroup, error) {
	return nil, nil
}
func (m *MockCameraRepo) DeleteGroup(ctx context.Context, id, tenantID uuid.UUID) error { return nil }
func (m *MockCameraRepo) SetGroupMembers(ctx context.Context, groupID, tenantID uuid.UUID, cameraIDs []uuid.UUID) error {
	return nil
}
