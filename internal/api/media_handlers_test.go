package api_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/api"
	"github.com/technosupport/ts-vms/internal/cameras"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/middleware"
)

func TestHandler_UpdateSelectionUrls(t *testing.T) {
	mockCamRepo := &cameras.MockCameraRepo{}
	mockMediaRepo := &cameras.MockMediaRepo{}
	mockCreds := &cameras.MockCredentialProvider{}
	mockAuditor := &cameras.MockAuditor{}

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

	svc := cameras.NewMediaService(mockMediaRepo, mockCamRepo, mockCreds, mockAuditor)
	h := api.NewMediaHandler(svc)

	body := `{"main_rtsp_url_sanitized":"rtsp://10.0.0.1/main","sub_rtsp_url_sanitized":"rtsp://10.0.0.1/sub"}`
	req := httptest.NewRequest("PUT", "/api/v1/cameras/"+cameraID.String()+"/media-selection", bytes.NewBufferString(body))
	req.SetPathValue("id", cameraID.String())
	req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{
		TenantID: tenantID.String(),
		UserID:   uuid.New().String(),
	}))

	rr := httptest.NewRecorder()
	h.UpdateSelectionUrls(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d. Body: %s", rr.Code, rr.Body.String())
	}
	if saved == nil {
		t.Fatal("expected selection to be saved")
	}
	if saved.MainRTSP != "rtsp://10.0.0.1/main" {
		t.Fatalf("unexpected main rtsp: %s", saved.MainRTSP)
	}
	if saved.SubRTSP != "rtsp://10.0.0.1/sub" {
		t.Fatalf("unexpected sub rtsp: %s", saved.SubRTSP)
	}
	if saved.MainProfileToken != "" || saved.SubProfileToken != "" {
		t.Fatalf("expected manual update to clear profile tokens")
	}
}
