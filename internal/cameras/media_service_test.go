package cameras

import (
	"context"
	"net"
	"testing"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/onvif"
)

// MockOnvifClient
type MockOnvifClient struct {
	Profiles  []onvif.MediaProfile
	StreamURI string
}

func (m *MockOnvifClient) GetCapabilities(ctx context.Context) (map[string]bool, string, string, string, error) {
	return map[string]bool{"Media": true}, "http://mock/media", "http://mock/events", "", nil
}
func (m *MockOnvifClient) GetProfiles(ctx context.Context, mediaURI string) ([]onvif.MediaProfile, error) {
	return m.Profiles, nil
}
func (m *MockOnvifClient) GetStreamUri(ctx context.Context, mediaURI, token string, useMedia2 bool) (string, error) {
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
			Profiles: []onvif.MediaProfile{
				{Token: "t1", Name: "Main", VideoEncoderConfiguration: &onvif.VideoEncoderConfiguration{
					Encoding: "H264",
					Resolution: onvif.Resolution{
						Width:  1920,
						Height: 1080,
					},
					RateControl: onvif.RateControl{
						FrameRateLimit: 30,
						BitrateLimit:   4096,
					}}},
				{Token: "t2", Name: "Sub", VideoEncoderConfiguration: &onvif.VideoEncoderConfiguration{
					Encoding: "H264",
					Resolution: onvif.Resolution{
						Width:  640,
						Height: 360,
					},
					RateControl: onvif.RateControl{
						FrameRateLimit: 15,
						BitrateLimit:   1024,
					}}},
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

func TestUpdateManualStreamUrls(t *testing.T) {
	mockCamRepo := &MockCameraRepo{}
	mockMediaRepo := &MockMediaRepo{}
	mockCreds := &MockCredentialProvider{}
	mockAuditor := &MockAuditor{}

	svc := NewMediaService(mockMediaRepo, mockCamRepo, mockCreds, mockAuditor)

	tenantID := uuid.New()
	cameraID := uuid.New()
	mockCamRepo.GetByIDFunc = func(ctx context.Context, id uuid.UUID) (*data.Camera, error) {
		return &data.Camera{ID: cameraID, TenantID: tenantID}, nil
	}

	var saved *data.CameraStreamSelection
	mockMediaRepo.GetSelectionFunc = func(ctx context.Context, t, c uuid.UUID) (*data.CameraStreamSelection, error) {
		return &data.CameraStreamSelection{
			ID:               uuid.New(),
			TenantID:         tenantID,
			CameraID:         cameraID,
			MainProfileToken: "main-token",
			SubProfileToken:  "sub-token",
		}, nil
	}
	mockMediaRepo.UpsertSelectionFunc = func(ctx context.Context, s *data.CameraStreamSelection) error {
		copySel := *s
		saved = &copySel
		return nil
	}

	sel, err := svc.UpdateManualStreamUrls(context.Background(), tenantID, cameraID, "rtsp://10.0.0.1/main", "rtsp://10.0.0.1/sub")
	if err != nil {
		t.Fatalf("UpdateManualStreamUrls failed: %v", err)
	}

	if saved == nil {
		t.Fatal("expected UpsertSelection to be called")
	}
	if saved.MainRTSP != "rtsp://10.0.0.1/main" {
		t.Fatalf("unexpected main rtsp: %s", saved.MainRTSP)
	}
	if saved.SubRTSP != "rtsp://10.0.0.1/sub" {
		t.Fatalf("unexpected sub rtsp: %s", saved.SubRTSP)
	}
	if sel.MainProfileToken != "" || sel.SubProfileToken != "" {
		t.Fatalf("expected manual update to clear profile tokens")
	}
}
