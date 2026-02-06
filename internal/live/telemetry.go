package live

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redis/go-redis/v9"
)

var (
	// Metrics - Low Cardinality Only
	metricSessionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "live_view_sessions_active",
		Help: "Current number of active viewer sessions",
	})

	metricStartTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "live_view_start_total",
		Help: "Total livestreams started by primary mode",
	}, []string{"primary"})

	metricFallbackTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "live_view_fallback_total",
		Help: "Total fallbacks to HLS by reason code",
	}, []string{"reason"})

	metricEventsDropped = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "live_view_client_events_dropped_total",
		Help: "Telemetry events rejected",
	}, []string{"reason"})

	metricTTFF = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "live_view_ttff_ms",
		Help:    "Time to first frame in milliseconds (aggregated from client)",
		Buckets: []float64{100, 250, 500, 1000, 2000, 3000, 5000, 10000},
	})
	metricGridTilesActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "grid_tiles_active",
		Help: "Current active tiles in grid mode",
	})

	metricGridStartTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "grid_tile_start_total",
		Help: "Total livestreams started in grid mode",
	})

	metricLiveLimitExceeded = promauto.NewCounter(prometheus.CounterOpts{
		Name: "live_limit_exceeded_total",
		Help: "Total rate limit errors returned",
	})
)

type TelemetryService struct {
	Redis *redis.Client
}

func NewTelemetryService(r *redis.Client) *TelemetryService {
	return &TelemetryService{Redis: r}
}

// Allowed Event Types
var allowedEvents = map[string]bool{
	"webrtc_attempt":       true,
	"webrtc_connected":     true,
	"webrtc_first_frame":   true,
	"webrtc_failed":        true,
	"fallback_to_hls":      true,
	"hls_playing":          true,
	"retry_webrtc_clicked": true,
	"session_end":          true,
	"grid_open":            true,
	"tile_start":           true,
	"tile_stop":            true,
	"tile_fullscreen":      true,
}

// Allowed Reason Codes
var allowedReasonCodes = map[ReasonCode]bool{
	ReasonConnectTimeout:      true,
	ReasonTrackTimeout:        true,
	ReasonICEFailed:           true,
	ReasonDTLSFailed:          true,
	ReasonSFUSignalingFailed:  true,
	ReasonSFUBusy:             true,
	ReasonPermissionDenied:    true,
	ReasonBrowserNotSupported: true,
	ReasonUnknown:             true,
}

func (s *TelemetryService) RecordEvent(ctx context.Context, evt *TelemetryEvent) error {
	// 1. Validate Payload
	if !allowedEvents[evt.EventType] {
		metricEventsDropped.WithLabelValues("invalid_type").Inc()
		return fmt.Errorf("invalid event type: %s", evt.EventType)
	}

	if evt.ReasonCode != "" && !allowedReasonCodes[evt.ReasonCode] {
		metricEventsDropped.WithLabelValues("invalid_reason").Inc()
		return fmt.Errorf("invalid reason code: %s", evt.ReasonCode)
	}

	// 2. Validate Session Existence
	// Only if not session_end? No, session_end should also be for valid session.
	// But duplicate session_ends might happen.
	sessKey := fmt.Sprintf("live:sess:%s", evt.ViewerSessionID)
	// We need Session Data to verify tenant/user for cleanup
	sessData, err := s.Redis.Get(ctx, sessKey).Result()
	if err != nil {
		// Session might be gone (expired/scrubbed), but we process session_end leniently?
		// But RateLimit needs session access usually.
		if evt.EventType == "session_end" {
			// If session payload gone, we can't look up TenantID to cleanup set...
			// This is a common race condition.
			// Client sends ID... we can't clean SET without Tenant/User ID.
			// Unless we stored reverse mapping or client sends TenantID.
			// Client payload doesn't have TenantID.
			// So if we can't find session, we can't cleanup SET.
			// But that's fine, SCRUBBER handles it next time.
			return nil
		}
		metricEventsDropped.WithLabelValues("unknown_session").Inc()
		return fmt.Errorf("session not found: %s", evt.ViewerSessionID)
	}

	// 3. Rate Limit
	limitKey := fmt.Sprintf("live:limit:%s", evt.ViewerSessionID)
	count, err := s.Redis.Incr(ctx, limitKey).Result()
	if err == nil {
		if count == 1 {
			s.Redis.Expire(ctx, limitKey, 10*time.Second)
		}
		if count > 40 { // Bumped slightly for Grid usage
			metricEventsDropped.WithLabelValues("rate_limit").Inc()
			return fmt.Errorf("rate limit exceeded")
		}
	}

	// 4. Update Session / Metrics / Cleanup

	// Parse session to get IDs for Key construction
	// We do minimal parse for performance if needed, but JSON unmarshal is safest.
	// We only need TenantID and UserID for cleanup on session_end

	if evt.EventType == "session_end" {
		// Cleanup Active Set
		// We need User/Tenant.
		// NOTE: TelemetryEvent doesn't carry Tenant/User (security).
		// We rely on Redis session data.
		// (Assuming sessData was found above).
		// Wait, 'sessData' is string. We scan or assume struct matches.
		// Let's rely on Scrubbing mostly, but try best effort here.
		// Or: We add TenantID to live:sess:{id} key? No, key usage is fixed.
		// We can parse the JSON string lightly or regex, but unmarshal is robust.
		// Since session_end is rare event (once per session), parsing is fine.
		// But session payload *in Redis* depends on storage format from service.go. (ViewerSession)
		// Let's assume ViewerSession struct.
		// Wait, I cannot import internal/live/service (cycle). ViewerSession is in models.go? Yes.
		var vs ViewerSession
		if jsonErr := json.Unmarshal([]byte(sessData), &vs); jsonErr == nil {
			activeKey := fmt.Sprintf("live:active:%s:%s", vs.TenantID, vs.UserID)
			s.Redis.SRem(ctx, activeKey, evt.ViewerSessionID)
			metricSessionsActive.Dec()
		}

		// Also remove signal key and limits?
		// We leave till TTL usually.
		return nil
	}

	// Heartbeat
	s.Redis.Expire(ctx, sessKey, SessionTTL)
	s.Redis.HSet(ctx, sessKey, "last_seen_at", time.Now().Format(time.RFC3339))

	// Metrics
	if evt.EventType == "fallback_to_hls" {
		metricFallbackTotal.WithLabelValues(string(evt.ReasonCode)).Inc()
		// Also could update session "mode" in Redis if needed
	}
	if evt.EventType == "tile_start" && evt.Mode == "grid" {
		metricGridStartTotal.Inc()
		metricGridTilesActive.Inc()
	}
	if evt.EventType == "tile_stop" {
		metricGridTilesActive.Dec()
	}
	if evt.EventType == "webrtc_connected" {
		metricStartTotal.WithLabelValues("webrtc").Inc()
	}
	if evt.TTFFMs > 0 {
		metricTTFF.Observe(float64(evt.TTFFMs))
	}
	if evt.EventType == "webrtc_attempt" {
		metricSessionsActive.Inc()
	}

	// Update Session Mode?
	// Not strictly required for Phase 3.7 unless state tracking needed.

	return nil
}
