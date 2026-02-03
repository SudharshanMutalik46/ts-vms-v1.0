package health

import (
	"context"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/technosupport/ts-vms/internal/cameras"
	"github.com/technosupport/ts-vms/internal/data"
)

// MockHealthRepo
type MockHealthRepo struct {
	mock.Mock
}

func (m *MockHealthRepo) UpsertStatus(ctx context.Context, h *data.CameraHealthCurrent) error {
	args := m.Called(ctx, h)
	return args.Error(0)
}

func (m *MockHealthRepo) GetStatus(ctx context.Context, cameraID uuid.UUID) (*data.CameraHealthCurrent, error) {
	args := m.Called(ctx, cameraID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*data.CameraHealthCurrent), args.Error(1)
}

func (m *MockHealthRepo) AddHistory(ctx context.Context, h *data.CameraHealthHistory) error {
	args := m.Called(ctx, h)
	return args.Error(0)
}

func (m *MockHealthRepo) PruneHistory(ctx context.Context, cameraID uuid.UUID, maxRecords int) error {
	args := m.Called(ctx, cameraID, maxRecords)
	return args.Error(0)
}

func (m *MockHealthRepo) GetHistory(ctx context.Context, cameraID uuid.UUID, limit, offset int) ([]*data.CameraHealthHistory, error) {
	args := m.Called(ctx, cameraID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*data.CameraHealthHistory), args.Error(1)
}

func (m *MockHealthRepo) UpsertAlert(ctx context.Context, a *data.CameraAlert) error {
	args := m.Called(ctx, a)
	return args.Error(0)
}

func (m *MockHealthRepo) GetOpenAlert(ctx context.Context, cameraID uuid.UUID, alertType string) (*data.CameraAlert, error) {
	args := m.Called(ctx, cameraID, alertType)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*data.CameraAlert), args.Error(1)
}

func (m *MockHealthRepo) CloseAlert(ctx context.Context, alertID uuid.UUID) error {
	args := m.Called(ctx, alertID)
	return args.Error(0)
}

func (m *MockHealthRepo) ListAlerts(ctx context.Context, tenantID uuid.UUID, state string) ([]*data.CameraAlert, error) {
	args := m.Called(ctx, tenantID, state)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*data.CameraAlert), args.Error(1)
}

func (m *MockHealthRepo) ListStatuses(ctx context.Context, tenantID uuid.UUID) ([]*data.CameraHealthCurrent, error) {
	args := m.Called(ctx, tenantID)
	return args.Get(0).([]*data.CameraHealthCurrent), args.Error(1)
}

func (m *MockHealthRepo) ListTargets(ctx context.Context) ([]data.CameraHealthTarget, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]data.CameraHealthTarget), args.Error(1)
}

func (m *MockHealthRepo) GetTarget(ctx context.Context, cameraID uuid.UUID) (*data.CameraHealthTarget, error) {
	args := m.Called(ctx, cameraID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*data.CameraHealthTarget), args.Error(1)
}

// MockProber
type MockProber struct {
	mock.Mock
}

func (m *MockProber) Probe(ctx context.Context, tenantID, cameraID uuid.UUID, rtspURL string) (data.CameraHealthStatus, string, int) {
	args := m.Called(ctx, tenantID, cameraID, rtspURL)
	return args.Get(0).(data.CameraHealthStatus), args.String(1), args.Int(2)
}

// MockCreds (reuse cameras mock logic if needed, but redefining simple here)
type MockCreds struct {
	mock.Mock
}

func (m *MockCreds) GetCredentials(ctx context.Context, tenantID, cameraID uuid.UUID, reveal bool) (*cameras.CredentialOutput, bool, error) {
	args := m.Called(ctx, tenantID, cameraID, reveal)
	if args.Get(0) == nil {
		return nil, args.Bool(1), args.Error(2)
	}
	return args.Get(0).(*cameras.CredentialOutput), args.Bool(1), args.Error(2)
}

// Satisfy interface
func (m *MockCreds) SetCredentials(ctx context.Context, tenantID, cameraID uuid.UUID, user, pass string) error {
	return nil
}
func (m *MockCreds) DeleteCredentials(ctx context.Context, tenantID, cameraID uuid.UUID) error {
	return nil
}
