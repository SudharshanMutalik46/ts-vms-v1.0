package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/audit"
	"github.com/technosupport/ts-vms/internal/cameras"
	"github.com/technosupport/ts-vms/internal/crypto"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/middleware"
)

// Mocks
type MockPermChecker struct {
	Result bool
}

func (m *MockPermChecker) CheckPermission(ctx context.Context, permSlug, scopeType, scopeID string) (bool, error) {
	return m.Result, nil
}

type MockCamProvider struct {
	Camera *data.Camera
	Err    error
}

func (m *MockCamProvider) GetByID(ctx context.Context, id, tenantID uuid.UUID) (*data.Camera, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	// Emulate tenant check if needed, but mock usually assumes success context
	if m.Camera == nil {
		return nil, data.ErrCredentialNotFound // fallback error
	}
	return m.Camera, nil
}

type MockCredUpdater struct {
	Store map[string]*data.CameraCredential
}

func (m *MockCredUpdater) Upsert(ctx context.Context, c *data.CameraCredential) error {
	m.Store[c.CameraID.String()] = c
	return nil
}
func (m *MockCredUpdater) Get(ctx context.Context, id uuid.UUID) (*data.CameraCredential, error) {
	if c, ok := m.Store[id.String()]; ok {
		return c, nil
	}
	return nil, data.ErrCredentialNotFound
}
func (m *MockCredUpdater) Delete(ctx context.Context, id uuid.UUID) error {
	delete(m.Store, id.String())
	return nil
}

// Local Definition of MockAuditor for Handler pkg
type MockAuditor struct{}

func (m *MockAuditor) WriteEvent(ctx context.Context, evt audit.AuditEvent) error { return nil }

func TestCredentialHandler(t *testing.T) {
	// Setup Core Services
	repo := &MockCredUpdater{Store: make(map[string]*data.CameraCredential)}
	aud := &MockAuditor{} // Using the one defined in this file

	key, _ := crypto.GenerateDEK()
	keyStr := base64.StdEncoding.EncodeToString(key)
	t.Setenv("MASTER_KEYS", `[{"kid":"test","material":"`+keyStr+`"}]`)
	t.Setenv("ACTIVE_MASTER_KID", "test")
	kr := crypto.NewKeyring()
	kr.LoadFromEnv()
	credSvc := cameras.NewCredentialService(repo, kr, aud)

	tenantID := uuid.New()
	camID := uuid.New()
	siteID := uuid.New()

	camProvider := &MockCamProvider{
		Camera: &data.Camera{
			ID:       camID,
			TenantID: tenantID,
			SiteID:   siteID,
		},
	}

	// 1. Test PUT (Success)
	h := NewCredentialHandler(credSvc, camProvider, &MockPermChecker{Result: true})

	body := `{"username":"admin", "password":"password"}`
	req := httptest.NewRequest("PUT", "/api/v1/cameras/"+camID.String()+"/credentials", bytes.NewBufferString(body))
	req.SetPathValue("id", camID.String())

	// Inject Auth Context
	ctx := middleware.WithAuthContext(req.Context(), &middleware.AuthContext{
		TenantID: tenantID.String(),
		UserID:   uuid.New().String(),
	})
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.Update(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("PUT Expected 200, got %d. Body: %s", rr.Code, rr.Body.String())
	}

	// 2. Test GET (Reveal)
	req2 := httptest.NewRequest("GET", "/api/v1/cameras/"+camID.String()+"/credentials?reveal=true", nil)
	req2.SetPathValue("id", camID.String())
	req2 = req2.WithContext(ctx)
	rr2 := httptest.NewRecorder()

	h.Get(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Errorf("GET Expected 200, got %d", rr2.Code)
	}
	var out cameras.CredentialOutput
	json.NewDecoder(rr2.Body).Decode(&out)
	if out.Data == nil || out.Data.Username != "admin" {
		t.Error("GET Reveal failed")
	}

	// 3. Test Unauthorized (Site Scope Fail) -> 404
	hDeny := NewCredentialHandler(credSvc, camProvider, &MockPermChecker{Result: false})
	req3 := httptest.NewRequest("GET", "/api/v1/cameras/"+camID.String()+"/credentials", nil)
	req3.SetPathValue("id", camID.String())
	req3 = req3.WithContext(ctx)
	rr3 := httptest.NewRecorder()

	hDeny.Get(rr3, req3)
	if rr3.Code != http.StatusNotFound {
		t.Errorf("GET Unauthorized Expected 404 (Non-enumeration), got %d", rr3.Code)
	}

	// 4. Test DELETE
	req4 := httptest.NewRequest("DELETE", "/api/v1/cameras/"+camID.String()+"/credentials", nil)
	req4.SetPathValue("id", camID.String())
	req4 = req4.WithContext(ctx)
	rr4 := httptest.NewRecorder()

	h.Delete(rr4, req4)
	if rr4.Code != http.StatusOK {
		t.Errorf("DELETE Expected 200, got %d", rr4.Code)
	}

	// Verify Deletion
	stored, _ := repo.Get(context.Background(), camID)
	if stored != nil {
		t.Error("Failed to delete from repo")
	}
}
