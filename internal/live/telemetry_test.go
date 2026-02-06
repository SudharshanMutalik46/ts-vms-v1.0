package live_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/technosupport/ts-vms/internal/live"
)

func TestTelemetryService_RateLimit(t *testing.T) {
	_, rdb := setupTestRedis(t)
	svc := live.NewTelemetryService(rdb)
	ctx := context.Background()

	sessID := "sess_123"
	// Create session first (validation requirement)
	rdb.Set(ctx, "live:sess:"+sessID, "{}", time.Minute)

	evt := &live.TelemetryEvent{
		ViewerSessionID: sessID,
		EventType:       "webrtc_attempt",
		ReasonCode:      "",
	}

	// 1. Should succeed 40 times
	for i := 0; i < 40; i++ {
		err := svc.RecordEvent(ctx, evt)
		assert.NoError(t, err, "Event %d should pass", i)
	}

	// 2. Should fail 31st time
	err := svc.RecordEvent(ctx, evt)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit")
}

func TestTelemetryService_Validation(t *testing.T) {
	_, rdb := setupTestRedis(t)
	svc := live.NewTelemetryService(rdb)
	ctx := context.Background()

	sessID := "sess_valid"
	rdb.Set(ctx, "live:sess:"+sessID, "{}", time.Minute)

	tests := []struct {
		name      string
		event     *live.TelemetryEvent
		wantErr   bool
		errString string
	}{
		{
			name: "Valid Event",
			event: &live.TelemetryEvent{
				ViewerSessionID: sessID,
				EventType:       "webrtc_attempt",
			},
			wantErr: false,
		},
		{
			name: "Invalid Event Type",
			event: &live.TelemetryEvent{
				ViewerSessionID: sessID,
				EventType:       "hack_attempt",
			},
			wantErr:   true,
			errString: "invalid event type",
		},
		{
			name: "Invalid Reason Code",
			event: &live.TelemetryEvent{
				ViewerSessionID: sessID,
				EventType:       "fallback_to_hls",
				ReasonCode:      "BAD_CODE",
			},
			wantErr:   true,
			errString: "invalid reason code",
		},
		{
			name: "Session Not Found",
			event: &live.TelemetryEvent{
				ViewerSessionID: "sess_missing",
				EventType:       "webrtc_attempt",
			},
			wantErr:   true,
			errString: "session not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.RecordEvent(ctx, tt.event)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errString)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Helpers

func setupTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	t.Cleanup(s.Close)

	rdb := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})
	return s, rdb
}
