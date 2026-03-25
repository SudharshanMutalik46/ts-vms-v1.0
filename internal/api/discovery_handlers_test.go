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
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/discovery"
	"github.com/technosupport/ts-vms/internal/middleware"
)

type discMockRepo struct {
	run  *data.DiscoveryRun
	dev  *data.DiscoveredDevice
	cred *data.OnvifCredential
}

func (m *discMockRepo) CreateRun(ctx context.Context, run *data.DiscoveryRun) error { return nil }
func (m *discMockRepo) UpdateRunStatus(ctx context.Context, id uuid.UUID, status string, finished bool, deviceCount, errorCount int) error {
	return nil
}
func (m *discMockRepo) GetRun(ctx context.Context, id uuid.UUID) (*data.DiscoveryRun, error) {
	if m.run == nil || m.run.ID != id {
		return nil, data.ErrRunNotFound
	}
	return m.run, nil
}
func (m *discMockRepo) UpsertDevice(ctx context.Context, d *data.DiscoveredDevice) error { return nil }
func (m *discMockRepo) UpdateDeviceProbe(ctx context.Context, d *data.DiscoveredDevice) error {
	return nil
}
func (m *discMockRepo) GetDevice(ctx context.Context, id uuid.UUID) (*data.DiscoveredDevice, error) {
	if m.dev == nil || m.dev.ID != id {
		return nil, data.ErrDeviceNotFound
	}
	return m.dev, nil
}
func (m *discMockRepo) ListDevices(ctx context.Context, runID uuid.UUID, limit, offset int) ([]*data.DiscoveredDevice, error) {
	if m.run == nil || m.run.ID != runID {
		return []*data.DiscoveredDevice{}, nil
	}
	if m.dev == nil {
		return []*data.DiscoveredDevice{}, nil
	}
	return []*data.DiscoveredDevice{m.dev}, nil
}
func (m *discMockRepo) StoreBootstrapCred(ctx context.Context, c *data.OnvifCredential) error { return nil }
func (m *discMockRepo) GetBootstrapCred(ctx context.Context, id uuid.UUID) (*data.OnvifCredential, error) {
	if m.cred == nil || m.cred.ID != id {
		return nil, data.ErrCredentialNotFound
	}
	return m.cred, nil
}

type discMockPerms struct {
	allow map[string]bool
}

func (m *discMockPerms) CheckPermission(ctx context.Context, permSlug, scopeType, scopeID string) (bool, error) {
	return m.allow[permSlug+"|"+scopeType+"|"+scopeID], nil
}

type discMockAuditor struct{}

func (m *discMockAuditor) WriteEvent(ctx context.Context, evt audit.AuditEvent) error { return nil }

func withDiscoveryAuth(req *http.Request, tenantID string) *http.Request {
	ac := &middleware.AuthContext{
		TenantID: tenantID,
		UserID:   uuid.New().String(),
	}
	return req.WithContext(middleware.WithAuthContext(req.Context(), ac))
}

func TestDiscoveryHandler_ListDevices_SiteScopedDenied(t *testing.T) {
	tenantID := uuid.New()
	siteID := uuid.New()
	runID := uuid.New()
	devID := uuid.New()

	repo := &discMockRepo{
		run: &data.DiscoveryRun{
			ID:       runID,
			TenantID: tenantID,
			SiteID:   &siteID,
		},
		dev: &data.DiscoveredDevice{
			ID:             devID,
			TenantID:       tenantID,
			DiscoveryRunID: runID,
		},
	}

	// NewService expect nvrRepo too now
	svc := discovery.NewService(repo, nil, nil, &discMockAuditor{})
	perms := &discMockPerms{
		allow: map[string]bool{
			// tenant-wide read not enough when run is site-scoped
			"onvif.discovery.read|tenant|" + tenantID.String(): true,
			"onvif.discovery.read|site|" + siteID.String():   false,
		},
	}

	h := api.NewDiscoveryHandler(svc, perms)

	req := httptest.NewRequest("GET", "/api/v1/onvif/discovered-devices?discovery_run_id="+runID.String(), nil)
	req = withDiscoveryAuth(req, tenantID.String())
	rr := httptest.NewRecorder()

	h.ListDevices(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDiscoveryHandler_ProbeDevice_SiteScopedDenied(t *testing.T) {
	tenantID := uuid.New()
	siteID := uuid.New()
	runID := uuid.New()
	devID := uuid.New()
	credID := uuid.New()

	repo := &discMockRepo{
		run: &data.DiscoveryRun{
			ID:       runID,
			TenantID: tenantID,
			SiteID:   &siteID,
		},
		dev: &data.DiscoveredDevice{
			ID:             devID,
			TenantID:       tenantID,
			DiscoveryRunID: runID,
			XAddrs:         []string{"http://192.168.1.10/onvif/device_service"},
		},
		cred: &data.OnvifCredential{
			ID:       credID,
			TenantID: tenantID,
		},
	}

	svc := discovery.NewService(repo, nil, nil, &discMockAuditor{})
	perms := &discMockPerms{
		allow: map[string]bool{
			"onvif.discovery.probe|tenant|" + tenantID.String(): true,
			"onvif.discovery.probe|site|" + siteID.String():   false,
		},
	}

	h := api.NewDiscoveryHandler(svc, perms)

	body := `{"credential_id":"` + credID.String() + `"}`
	req := httptest.NewRequest("POST", "/api/v1/onvif/discovered-devices/"+devID.String()+":probe", bytes.NewBufferString(body))
	req.SetPathValue("id", devID.String())
	req = withDiscoveryAuth(req, tenantID.String())
	rr := httptest.NewRecorder()

	h.ProbeDevice(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}
