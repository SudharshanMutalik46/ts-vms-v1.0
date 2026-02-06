package health

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/technosupport/ts-vms/internal/data"
)

func TestService_PerformCheck_AlertLogic(t *testing.T) {
	mockRepo := new(MockHealthRepo)
	mockProber := new(MockProber)
	svc := NewService(mockRepo, &MockNVRRepo{}, mockProber)

	tid := uuid.New()
	cid := uuid.New()
	url := "rtsp://test"

	// Mock Prober Fail
	mockProber.On("Probe", mock.Anything, tid, cid, url).Return(data.HealthStatusOffline, "timeout", 0)

	// Mock Current Status (Consecutive = 4)
	mockRepo.On("GetStatus", mock.Anything, cid).Return(&data.CameraHealthCurrent{
		ConsecutiveFailures: 4,
		LastSuccessAt:       nil, // Never succeeded (or long ago)
	}, nil)

	// Expect UpsertStatus
	mockRepo.On("UpsertStatus", mock.Anything, mock.MatchedBy(func(h *data.CameraHealthCurrent) bool {
		return h.ConsecutiveFailures == 5 && h.Status == data.HealthStatusOffline
	})).Return(nil)

	// Expect AddHistory
	mockRepo.On("AddHistory", mock.Anything, mock.Anything).Return(nil)
	mockRepo.On("PruneHistory", mock.Anything, cid, 200).Return(nil)

	// Alert Logic:
	// Offline > 5m?
	// If failures=5 and lastSuccess=nil, implementation uses 6m -> Open Alert.
	mockRepo.On("GetOpenAlert", mock.Anything, cid, "offline_over_5m").Return(nil, nil)
	mockRepo.On("UpsertAlert", mock.Anything, mock.MatchedBy(func(a *data.CameraAlert) bool {
		return a.State == "open" && a.Type == "offline_over_5m"
	})).Return(nil)

	svc.PerformCheck(context.Background(), tid, cid, url)

	mockRepo.AssertExpectations(t)
	mockProber.AssertExpectations(t)
}

func TestService_PerformCheck_Recovery(t *testing.T) {
	mockRepo := new(MockHealthRepo)
	mockProber := new(MockProber)
	svc := NewService(mockRepo, &MockNVRRepo{}, mockProber)
	tid := uuid.New()
	cid := uuid.New()
	url := "rtsp://test"

	// Mock Prober Success
	mockProber.On("Probe", mock.Anything, tid, cid, url).Return(data.HealthStatusOnline, "ok", 15)

	// Mock Current Status (Was Offline)
	lastChecked := time.Now().Add(-1 * time.Minute)
	openAlert := &data.CameraAlert{ID: uuid.New(), State: "open"}

	mockRepo.On("GetStatus", mock.Anything, cid).Return(&data.CameraHealthCurrent{
		ConsecutiveFailures: 10,
		Status:              data.HealthStatusOffline,
		LastCheckedAt:       lastChecked,
	}, nil)

	// Expect UpsertStatus (Reset failures)
	mockRepo.On("UpsertStatus", mock.Anything, mock.MatchedBy(func(h *data.CameraHealthCurrent) bool {
		return h.ConsecutiveFailures == 0 && h.Status == data.HealthStatusOnline
	})).Return(nil)

	// Expect AddHistory
	mockRepo.On("AddHistory", mock.Anything, mock.Anything).Return(nil)
	mockRepo.On("PruneHistory", mock.Anything, cid, 200).Return(nil)

	// Alert Logic: Close Open Alert
	mockRepo.On("GetOpenAlert", mock.Anything, cid, "offline_over_5m").Return(openAlert, nil)
	mockRepo.On("CloseAlert", mock.Anything, openAlert.ID).Return(nil)

	svc.PerformCheck(context.Background(), tid, cid, url)

	mockRepo.AssertExpectations(t)
}
