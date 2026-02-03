package cameras_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/audit"
	"github.com/technosupport/ts-vms/internal/cameras"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/license"
)

// MockRepository
type MockRepo struct {
	Count int
	Calls map[string]int
	Err   error
}

func (m *MockRepo) Create(ctx context.Context, c *data.Camera) error {
	m.Calls["Create"]++
	return m.Err
}
func (m *MockRepo) GetByID(ctx context.Context, id uuid.UUID) (*data.Camera, error) {
	return &data.Camera{ID: id, IsEnabled: false}, m.Err
}
func (m *MockRepo) Update(ctx context.Context, c *data.Camera) error { return m.Err }
func (m *MockRepo) SetStatus(ctx context.Context, id, tenantID uuid.UUID, enabled bool) error {
	m.Calls["SetStatus"]++
	return m.Err
}
func (m *MockRepo) SoftDelete(ctx context.Context, id, tenantID uuid.UUID) error { return m.Err }
func (m *MockRepo) CountAll(ctx context.Context, tenantID uuid.UUID) (int, error) {
	return m.Count, m.Err
}
func (m *MockRepo) BulkUpdateStatus(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID, enabled bool) error {
	m.Calls["BulkUpdateStatus"]++
	return m.Err
}
func (m *MockRepo) BulkAddTags(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID, tags []string) error {
	return m.Err
}
func (m *MockRepo) BulkRemoveTags(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID, tags []string) error {
	return m.Err
}
func (m *MockRepo) List(ctx context.Context, tenantID uuid.UUID, filter data.CameraFilter, limit, offset int) ([]*data.Camera, int, error) {
	return nil, 0, m.Err
}
func (m *MockRepo) CreateGroup(ctx context.Context, g *data.CameraGroup) error { return m.Err }
func (m *MockRepo) ListGroups(ctx context.Context, tenantID uuid.UUID) ([]*data.CameraGroup, error) {
	return nil, m.Err
}
func (m *MockRepo) DeleteGroup(ctx context.Context, id, tenantID uuid.UUID) error { return m.Err }
func (m *MockRepo) SetGroupMembers(ctx context.Context, groupID, tenantID uuid.UUID, cameraIDs []uuid.UUID) error {
	return m.Err
}

type MockAuditor struct {
	LastEvent *audit.AuditEvent
}

func (m *MockAuditor) WriteEvent(ctx context.Context, evt audit.AuditEvent) error {
	m.LastEvent = &evt
	return nil
}

type MockLicense struct {
	Limits license.LicenseLimits
}

func (m *MockLicense) GetLimits(tenantID uuid.UUID) license.LicenseLimits {
	return m.Limits
}

func TestCreateCamera_Success(t *testing.T) {
	repo := &MockRepo{Calls: make(map[string]int), Count: 5}
	lic := &MockLicense{Limits: license.LicenseLimits{MaxCameras: 10}}
	aud := &MockAuditor{}
	svc := cameras.NewService(repo, lic, aud)

	cam := &data.Camera{
		TenantID:  uuid.New(),
		Name:      "Test Cam",
		IPAddress: testIP(), // we need a helper or just net.ParseIP("1.2.3.4")
	}

	err := svc.CreateCamera(context.Background(), cam)
	if err != nil {
		t.Fatalf("Expected success, got %v", err)
	}

	if repo.Calls["Create"] != 1 {
		t.Errorf("Expected Create call, got %d", repo.Calls["Create"])
	}
	if aud.LastEvent == nil || aud.LastEvent.Action != "camera.create" {
		t.Error("Audit event missing or incorrect")
	}
}

func TestCreateCamera_LicenseExceeded(t *testing.T) {
	repo := &MockRepo{Calls: make(map[string]int), Count: 10}
	lic := &MockLicense{Limits: license.LicenseLimits{MaxCameras: 10}} // Full
	aud := &MockAuditor{}
	svc := cameras.NewService(repo, lic, aud)

	cam := &data.Camera{TenantID: uuid.New(), Name: "Test Cam", IPAddress: testIP()}

	err := svc.CreateCamera(context.Background(), cam)
	if !errors.Is(err, cameras.ErrLicenseLimitExceeded) {
		t.Errorf("Expected ErrLicenseLimitExceeded, got %v", err)
	}
}

func TestEnableCamera_QuotaExceeded(t *testing.T) {
	repo := &MockRepo{Calls: make(map[string]int), Count: 11} // Over limit
	lic := &MockLicense{Limits: license.LicenseLimits{MaxCameras: 10}}
	aud := &MockAuditor{}
	svc := cameras.NewService(repo, lic, aud)

	err := svc.EnableCamera(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, cameras.ErrLicenseLimitExceeded) {
		t.Errorf("Expected License Limit Error, got %v", err)
	}
}

func TestBulkEnable_Success(t *testing.T) {
	repo := &MockRepo{Calls: make(map[string]int), Count: 5}
	lic := &MockLicense{Limits: license.LicenseLimits{MaxCameras: 10}}
	aud := &MockAuditor{}
	svc := cameras.NewService(repo, lic, aud)

	ids := []uuid.UUID{uuid.New(), uuid.New()}
	err := svc.BulkEnable(context.Background(), uuid.New(), ids)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if repo.Calls["BulkUpdateStatus"] != 1 {
		t.Error("Expected BulkUpdateStatus call")
	}
	if aud.LastEvent.Action != "camera.bulk.enable" {
		t.Error("Audit mismatch")
	}
}

func TestBulkEnable_QuotaExceeded(t *testing.T) {
	repo := &MockRepo{Calls: make(map[string]int), Count: 11} // Over limit
	lic := &MockLicense{Limits: license.LicenseLimits{MaxCameras: 10}}
	aud := &MockAuditor{}
	svc := cameras.NewService(repo, lic, aud)

	err := svc.BulkEnable(context.Background(), uuid.New(), []uuid.UUID{uuid.New()})
	if !errors.Is(err, cameras.ErrLicenseLimitExceeded) {
		t.Errorf("Expected limit exceeded, got %v", err)
	}
}

func TestBulkDisable_Success(t *testing.T) {
	repo := &MockRepo{Calls: make(map[string]int)}
	lic := &MockLicense{}
	aud := &MockAuditor{}
	svc := cameras.NewService(repo, lic, aud)

	err := svc.BulkDisable(context.Background(), uuid.New(), []uuid.UUID{uuid.New()})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if aud.LastEvent.Action != "camera.bulk.disable" {
		t.Error("Audit mismatch")
	}
}

func TestBulkAddTags(t *testing.T) {
	repo := &MockRepo{Calls: make(map[string]int)}
	aud := &MockAuditor{}
	svc := cameras.NewService(repo, &MockLicense{}, aud)
	err := svc.BulkAddTags(context.Background(), uuid.New(), []uuid.UUID{uuid.New()}, []string{"tag1"})
	if err != nil {
		t.Errorf("Error: %v", err)
	}
	if aud.LastEvent.Action != "camera.bulk.tag_add" {
		t.Error("Audit mismatch")
	}
}

func TestBulkRemoveTags(t *testing.T) {
	repo := &MockRepo{Calls: make(map[string]int)}
	aud := &MockAuditor{}
	svc := cameras.NewService(repo, &MockLicense{}, aud)
	err := svc.BulkRemoveTags(context.Background(), uuid.New(), []uuid.UUID{uuid.New()}, []string{"tag1"})
	if err != nil {
		t.Errorf("Error: %v", err)
	}
	if aud.LastEvent.Action != "camera.bulk.tag_remove" {
		t.Error("Audit mismatch")
	}
}

func TestCreateGroup(t *testing.T) {
	repo := &MockRepo{Calls: make(map[string]int)}
	svc := cameras.NewService(repo, &MockLicense{}, &MockAuditor{})
	err := svc.CreateGroup(context.Background(), &data.CameraGroup{})
	if err != nil {
		t.Error(err)
	}
	// Verify mock calls if needed (CreateGroup doesn't set Calls in mock yet, usually MockRepo needs generic handling or dedicated field)
}

func TestDeleteGroup(t *testing.T) {
	repo := &MockRepo{Calls: make(map[string]int)}
	svc := cameras.NewService(repo, &MockLicense{}, &MockAuditor{})
	err := svc.DeleteGroup(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Error(err)
	}
}

func TestSetGroupMembers(t *testing.T) {
	repo := &MockRepo{Calls: make(map[string]int)}
	svc := cameras.NewService(repo, &MockLicense{}, &MockAuditor{})
	err := svc.SetGroupMembers(context.Background(), uuid.New(), uuid.New(), []uuid.UUID{uuid.New()})
	if err != nil {
		t.Error(err)
	}
}

func TestCreateCamera_NameLimit(t *testing.T) {
	svc := cameras.NewService(&MockRepo{}, &MockLicense{}, &MockAuditor{})
	longName := make([]byte, 121)
	cam := &data.Camera{Name: string(longName)}
	err := svc.CreateCamera(context.Background(), cam)
	if !errors.Is(err, cameras.ErrNameTooLong) {
		t.Error("Expected NameTooLong")
	}
}

func TestCreateCamera_InvalidIP(t *testing.T) {
	svc := cameras.NewService(&MockRepo{}, &MockLicense{}, &MockAuditor{})
	cam := &data.Camera{Name: "Valid", IPAddress: nil}
	err := svc.CreateCamera(context.Background(), cam)
	if !errors.Is(err, cameras.ErrInvalidIP) {
		t.Error("Expected InvalidIP")
	}
}

func TestUpdateCamera(t *testing.T) {
	aud := &MockAuditor{}
	svc := cameras.NewService(&MockRepo{}, &MockLicense{}, aud)
	err := svc.UpdateCamera(context.Background(), &data.Camera{ID: uuid.New()})
	if err != nil {
		t.Error(err)
	}
	if aud.LastEvent.Action != "camera.update" {
		t.Error("Audit mismatch")
	}
}

func TestDeleteCamera(t *testing.T) {
	aud := &MockAuditor{}
	svc := cameras.NewService(&MockRepo{}, &MockLicense{}, aud)
	err := svc.DeleteCamera(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Error(err)
	}
	if aud.LastEvent.Action != "camera.delete" {
		t.Error("Audit mismatch")
	}
}

func testIP() net.IP { return net.ParseIP("192.168.1.1") }
