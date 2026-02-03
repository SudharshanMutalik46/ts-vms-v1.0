package api_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/api"
	"github.com/technosupport/ts-vms/internal/audit"
	"github.com/technosupport/ts-vms/internal/cameras"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/license"
	"github.com/technosupport/ts-vms/internal/middleware"
)

// Mock Auditor
type MockAuditor struct{}

func (m *MockAuditor) WriteEvent(ctx context.Context, evt audit.AuditEvent) error { return nil }

type MockLicense struct{}

func (m *MockLicense) GetLimits(tenantID uuid.UUID) license.LicenseLimits {
	return license.LicenseLimits{MaxCameras: 100}
}

// Mock Repo
type HMockRepo struct {
}

func (m *HMockRepo) Create(ctx context.Context, c *data.Camera) error { c.ID = uuid.New(); return nil }
func (m *HMockRepo) GetByID(ctx context.Context, id uuid.UUID) (*data.Camera, error) {
	return &data.Camera{ID: id, Name: "Handler Cam", IsEnabled: true}, nil
}
func (m *HMockRepo) Update(ctx context.Context, c *data.Camera) error             { return nil }
func (m *HMockRepo) SetStatus(ctx context.Context, id, t uuid.UUID, e bool) error { return nil }
func (m *HMockRepo) SoftDelete(ctx context.Context, id, t uuid.UUID) error        { return nil }
func (m *HMockRepo) CountAll(ctx context.Context, t uuid.UUID) (int, error)       { return 0, nil }
func (m *HMockRepo) BulkUpdateStatus(ctx context.Context, t uuid.UUID, ids []uuid.UUID, e bool) error {
	return nil
}
func (m *HMockRepo) BulkAddTags(ctx context.Context, t uuid.UUID, ids []uuid.UUID, tags []string) error {
	return nil
}
func (m *HMockRepo) BulkRemoveTags(ctx context.Context, t uuid.UUID, ids []uuid.UUID, tags []string) error {
	return nil
}
func (m *HMockRepo) List(ctx context.Context, t uuid.UUID, f data.CameraFilter, l, o int) ([]*data.Camera, int, error) {
	return []*data.Camera{{Name: "Listed Cam"}}, 1, nil
}
func (m *HMockRepo) CreateGroup(ctx context.Context, g *data.CameraGroup) error {
	g.ID = uuid.New()
	return nil
}
func (m *HMockRepo) ListGroups(ctx context.Context, t uuid.UUID) ([]*data.CameraGroup, error) {
	return []*data.CameraGroup{{Name: "Group1"}}, nil
}
func (m *HMockRepo) DeleteGroup(ctx context.Context, id, t uuid.UUID) error { return nil }
func (m *HMockRepo) SetGroupMembers(ctx context.Context, gid, t uuid.UUID, cids []uuid.UUID) error {
	return nil
}

func withAuth(req *http.Request) *http.Request {
	ac := &middleware.AuthContext{
		TenantID: uuid.New().String(),
		UserID:   uuid.New().String(),
	}
	ctx := middleware.WithAuthContext(req.Context(), ac)
	return req.WithContext(ctx)
}

func TestHandler_CreateCamera(t *testing.T) {
	svc := cameras.NewService(&HMockRepo{}, &MockLicense{}, &MockAuditor{})
	h := api.NewCameraHandler(svc)

	body := `{"name":"test-cam", "ip_address":"1.2.3.4", "port":554, "site_id":"` + uuid.New().String() + `"}`
	req := httptest.NewRequest("POST", "/api/v1/cameras", bytes.NewBufferString(body))
	req = withAuth(req)

	rr := httptest.NewRecorder()
	h.Create(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("Expected 201, got %d. Body: %s", rr.Code, rr.Body.String())
	}
}

func TestHandler_CreateCamera_BadJSON(t *testing.T) {
	svc := cameras.NewService(&HMockRepo{}, &MockLicense{}, &MockAuditor{})
	h := api.NewCameraHandler(svc)
	req := httptest.NewRequest("POST", "/api/v1/cameras", bytes.NewBufferString(`{invalid`))
	req = withAuth(req)
	rr := httptest.NewRecorder()
	h.Create(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", rr.Code)
	}
}

func TestHandler_ListCameras(t *testing.T) {
	svc := cameras.NewService(&HMockRepo{}, &MockLicense{}, &MockAuditor{})
	h := api.NewCameraHandler(svc)
	req := httptest.NewRequest("GET", "/api/v1/cameras?limit=10", nil)
	req = withAuth(req)
	rr := httptest.NewRecorder()
	h.List(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
}

func TestHandler_EnableCamera(t *testing.T) {
	svc := cameras.NewService(&HMockRepo{}, &MockLicense{}, &MockAuditor{})
	h := api.NewCameraHandler(svc)
	req := httptest.NewRequest("POST", "/api/v1/cameras/uuid-here/enable", nil)
	req.SetPathValue("id", uuid.New().String()) // Go 1.22
	req = withAuth(req)
	rr := httptest.NewRecorder()
	h.Enable(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
}

func TestHandler_DisableCamera(t *testing.T) {
	svc := cameras.NewService(&HMockRepo{}, &MockLicense{}, &MockAuditor{})
	h := api.NewCameraHandler(svc)
	req := httptest.NewRequest("POST", "/api/v1/cameras/uuid-here/disable", nil)
	req.SetPathValue("id", uuid.New().String())
	req = withAuth(req)
	rr := httptest.NewRecorder()
	h.Disable(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
}

func TestHandler_BulkEnable(t *testing.T) {
	svc := cameras.NewService(&HMockRepo{}, &MockLicense{}, &MockAuditor{})
	h := api.NewCameraHandler(svc)
	body := `{"action":"enable", "camera_ids":["` + uuid.New().String() + `"]}`
	req := httptest.NewRequest("POST", "/api/v1/cameras/bulk", bytes.NewBufferString(body))
	req = withAuth(req)
	rr := httptest.NewRecorder()
	h.Bulk(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", rr.Code, rr.Body.String())
	}
}

func TestHandler_BulkDisable(t *testing.T) {
	svc := cameras.NewService(&HMockRepo{}, &MockLicense{}, &MockAuditor{})
	h := api.NewCameraHandler(svc)
	body := `{"action":"disable", "camera_ids":["` + uuid.New().String() + `"]}`
	req := httptest.NewRequest("POST", "/api/v1/cameras/bulk", bytes.NewBufferString(body))
	req = withAuth(req)
	rr := httptest.NewRecorder()
	h.Bulk(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", rr.Code, rr.Body.String())
	}
}

func TestHandler_CreateGroup(t *testing.T) {
	svc := cameras.NewService(&HMockRepo{}, &MockLicense{}, &MockAuditor{})
	h := api.NewCameraHandler(svc)
	body := `{"name":"Backend Group", "description":"Test Group"}`
	req := httptest.NewRequest("POST", "/api/v1/camera-groups", bytes.NewBufferString(body))
	req = withAuth(req)
	rr := httptest.NewRecorder()
	h.CreateGroup(rr, req)
	if rr.Code != http.StatusCreated {
		t.Errorf("Expected 201, got %d", rr.Code)
	}
}

func TestHandler_ListGroups(t *testing.T) {
	svc := cameras.NewService(&HMockRepo{}, &MockLicense{}, &MockAuditor{})
	h := api.NewCameraHandler(svc)
	req := httptest.NewRequest("GET", "/api/v1/camera-groups", nil)
	req = withAuth(req)
	rr := httptest.NewRecorder()
	h.ListGroups(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
}

func TestHandler_DeleteGroup(t *testing.T) {
	svc := cameras.NewService(&HMockRepo{}, &MockLicense{}, &MockAuditor{})
	h := api.NewCameraHandler(svc)
	req := httptest.NewRequest("DELETE", "/api/v1/camera-groups/"+uuid.New().String(), nil)
	req.SetPathValue("id", uuid.New().String())
	req = withAuth(req)
	rr := httptest.NewRecorder()
	h.DeleteGroup(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
}

func TestHandler_SetGroupMembers(t *testing.T) {
	svc := cameras.NewService(&HMockRepo{}, &MockLicense{}, &MockAuditor{})
	h := api.NewCameraHandler(svc)
	body := `{"camera_ids":["` + uuid.New().String() + `"]}`
	req := httptest.NewRequest("PUT", "/api/v1/camera-groups/members", bytes.NewBufferString(body))
	req.SetPathValue("id", uuid.New().String())
	req = withAuth(req)
	rr := httptest.NewRecorder()
	h.SetGroupMembers(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
}

func TestHandler_GroupMembers_BadID(t *testing.T) {
	svc := cameras.NewService(&HMockRepo{}, &MockLicense{}, &MockAuditor{})
	h := api.NewCameraHandler(svc)
	body := `{"camera_ids":["bad-uuid"]}`
	req := httptest.NewRequest("PUT", "/api/v1/camera-groups/members", bytes.NewBufferString(body))
	req.SetPathValue("id", uuid.New().String())
	req = withAuth(req)
	rr := httptest.NewRecorder()
	h.SetGroupMembers(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", rr.Code)
	}
}
