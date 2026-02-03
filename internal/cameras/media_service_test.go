package cameras

import (
	"context"
	"net"
	"testing"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/discovery"
)

// MockOnvifClient
type MockOnvifClient struct {
	Profiles  []discovery.MediaProfile
	StreamURI string
}

func (m *MockOnvifClient) GetCapabilities(ctx context.Context) (map[string]bool, string, error) {
	return map[string]bool{"Media": true}, "http://mock/media", nil
}
func (m *MockOnvifClient) GetProfiles(ctx context.Context, mediaURI string) ([]discovery.MediaProfile, error) {
	return m.Profiles, nil
}
func (m *MockOnvifClient) GetStreamUri(ctx context.Context, mediaURI, token string) (string, error) {
	return m.StreamURI + "/" + token, nil
}

func TestSelectMediaProfiles_Orchestration(t *testing.T) {
	// Setup Mocks
	mockCamRepo := &MockCameraRepo{}
	mockMediaRepo := &MockMediaRepo{}
	mockCreds := &MockCredentialProvider{}
	mockAuditor := &MockAuditor{}

	// SUT
	svc := NewMediaService(mockMediaRepo, mockCamRepo, mockCreds, mockAuditor)

	// Inject Mock Factory
	svc.ClientFactory = func(x, u, p string) (OnvifClient, error) {
		return &MockOnvifClient{
			Profiles: []discovery.MediaProfile{
				{Token: "t1", Name: "Main", VideoEncoderConfiguration: struct {
					Encoding   string
					Resolution struct {
						Width  int
						Height int
					}
				}{Encoding: "H264", Resolution: struct {
					Width  int
					Height int
				}{1920, 1080}}},
				{Token: "t2", Name: "Sub", VideoEncoderConfiguration: struct {
					Encoding   string
					Resolution struct {
						Width  int
						Height int
					}
				}{Encoding: "H264", Resolution: struct {
					Width  int
					Height int
				}{640, 360}}},
			},
			StreamURI: "rtsp://camera",
		}, nil
	}

	// Context
	ctx := context.Background()
	tenantID := uuid.New()
	cameraID := uuid.New()

	// 1. Mock Camera
	mockCamRepo.GetByIDFunc = func(ctx context.Context, id uuid.UUID) (*data.Camera, error) {
		return &data.Camera{
			ID:        cameraID,
			TenantID:  tenantID,
			IPAddress: net.ParseIP("192.168.1.100"),
		}, nil
	}

	// 2. Mock Credentials
	mockCreds.GetFunc = func(ctx context.Context, t, c uuid.UUID, r bool) (*CredentialOutput, bool, error) {
		return &CredentialOutput{
			Exists: true,
			Data:   &CredentialInput{Username: "admin", Password: "password"},
		}, true, nil
	}

	// 3. Mock Persistence
	mockMediaRepo.UpsertProfileFunc = func(ctx context.Context, p *data.CameraMediaProfile) error {
		return nil
	}
	mockMediaRepo.UpsertSelectionFunc = func(ctx context.Context, s *data.CameraStreamSelection) error {
		if s.MainProfileToken != "t1" {
			t.Errorf("Expected Main Token t1, got %s", s.MainProfileToken)
		}
		if s.SubProfileToken != "t2" {
			t.Errorf("Expected Sub Token t2, got %s", s.SubProfileToken)
		}
		return nil
	}

	// EXECUTE
	sel, err := svc.SelectMediaProfiles(ctx, tenantID, cameraID)
	if err != nil {
		t.Fatalf("SelectMediaProfiles failed: %v", err)
	}

	if sel.MainProfileToken != "t1" {
		t.Errorf("Result MainProfileToken = %s; want t1", sel.MainProfileToken)
	}

	// Verify Audit Event?
	if len(mockAuditor.Events) == 0 {
		t.Error("Expected audit event")
	}
}
