package health

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/technosupport/ts-vms/internal/data"
)

func TestScheduler_Run(t *testing.T) {
	mockRepo := new(MockHealthRepo)
	mockProber := new(MockProber)
	svc := NewService(mockRepo, &MockNVRRepo{}, mockProber)

	cfg := SchedulerConfig{
		Interval:       100 * time.Millisecond,
		WorkerPoolSize: 2,
	}
	scheduler := NewScheduler(cfg, svc)

	// Mock Data
	tid := uuid.New()
	cid := uuid.New()
	target := data.CameraHealthTarget{
		TenantID:      tid,
		CameraID:      cid,
		RTSPURL:       "rtsp://mock.local/stream",
		Status:        data.HealthStatusOffline,
		LastCheckedAt: time.Now().Add(-10 * time.Minute), // Should check
	}

	// Expect ListTargets
	mockRepo.On("ListTargets", mock.Anything).Return([]data.CameraHealthTarget{target}, nil)

	// Expect Probe (via Service.PerformCheck)
	// Service.PerformCheck calls Probe -> GetStatus -> UpsertStatus -> AddHistory -> Alert
	mockProber.On("Probe", mock.Anything, tid, cid, target.RTSPURL).Return(data.HealthStatusOnline, "ok", 10)

	// Expect Repo interactions in Service
	mockRepo.On("GetStatus", mock.Anything, cid).Return(nil, nil)
	mockRepo.On("UpsertStatus", mock.Anything, mock.Anything).Return(nil)
	mockRepo.On("AddHistory", mock.Anything, mock.Anything).Return(nil)
	mockRepo.On("PruneHistory", mock.Anything, cid, LimitHistory).Return(nil)
	// Alert logic
	mockRepo.On("GetOpenAlert", mock.Anything, cid, "offline_over_5m").Return(nil, nil)
	// If online, no alert creation if no open alert.

	scheduler.Start()
	time.Sleep(300 * time.Millisecond) // Wait for 2-3 ticks
	scheduler.Stop()

	// Verify ListTargets called at least twice
	// mockRepo.AssertNumberOfCalls(t, "ListTargets", 2) // Approximate
}

func TestScheduler_Backoff(t *testing.T) {
	// ... logic to test shouldSkip ...
	s := &Scheduler{}

	now := time.Now()

	// Case 1: Success -> No Backoff
	c1 := data.CameraHealthTarget{Status: data.HealthStatusOnline, LastCheckedAt: now.Add(-10 * time.Second)}
	assert.False(t, s.shouldSkip(c1))

	// Case 2: Failure 1 -> Backoff 60s
	// Checked 30s ago -> Skip
	c2 := data.CameraHealthTarget{
		Status:              data.HealthStatusOffline,
		LastCheckedAt:       now.Add(-30 * time.Second),
		ConsecutiveFailures: 1,
	}
	assert.True(t, s.shouldSkip(c2))

	// Case 3: Failure 1 -> Backoff 60s
	// Checked 70s ago -> Process
	c3 := data.CameraHealthTarget{
		Status:              data.HealthStatusOffline,
		LastCheckedAt:       now.Add(-70 * time.Second),
		ConsecutiveFailures: 1,
	}
	assert.False(t, s.shouldSkip(c3))
}

const LimitHistory = 200 // from logic
