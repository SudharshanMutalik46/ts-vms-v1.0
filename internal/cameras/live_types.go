package cameras

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Standardized Reason Codes for Phase 3.6
const (
	ReasonSfuSignalingFailed = "SFU_SIGNALING_FAILED"
	ReasonIceFailed          = "ICE_FAILED"
	ReasonDtlsFailed         = "DTLS_FAILED"
	ReasonTrackTimeout       = "TRACK_TIMEOUT"
	ReasonRTPTimeout         = "RTP_TIMEOUT"
	ReasonSfuBusy            = "SFU_BUSY"
	ReasonBrowserUnsupported = "BROWSER_NOT_SUPPORTED"
	ReasonPermissionDenied   = "PERMISSION_DENIED"
	ReasonUnknown            = "UNKNOWN"
)

// LiveStartResponse is the unified response for starting a live view session.
type LiveStartResponse struct {
	ViewerSessionID string         `json:"viewer_session_id"`
	ExpiresAt       int64          `json:"expires_at"`
	Primary         string         `json:"primary"`
	Fallback        string         `json:"fallback"`
	WebRTC          WebRTCConfig   `json:"webrtc"`
	HLS             HLSConfig      `json:"hls"`
	FallbackPolicy  FallbackPolicy `json:"fallback_policy"`
	TelemetryPolicy TelemetryPolicy `json:"telemetry_policy"`
}

type WebRTCConfig struct {
	SfuURL           string          `json:"sfu_url"`
	RoomID           string          `json:"room_id"`
	RtpCapabilities  json.RawMessage `json:"rtp_caps"`
	ConnectTimeoutMs int             `json:"connect_timeout_ms"`
}

type HLSConfig struct {
	PlaylistURL     string `json:"playlist_url"`
	TargetLatencyMs int    `json:"target_latency_ms"`
}

type FallbackPolicy struct {
	WebRTCConnectTimeoutMs int   `json:"webrtc_connect_timeout_ms"`
	WebRTCTrackTimeoutMs   int   `json:"webrtc_track_timeout_ms"`
	RetryBackoffMs        []int `json:"retry_backoff_ms"`
	MaxAutoRetries        int   `json:"max_auto_retries"`
}

type TelemetryPolicy struct {
	ClientEventEndpoint string  `json:"client_event_endpoint"`
	Sampling           float64 `json:"sampling"`
}

// ViewerSession represents the server-side state of a live view session.
type ViewerSession struct {
	ID            string    `json:"viewer_session_id"`
	TenantID      uuid.UUID `json:"tenant_id"`
	UserID        uuid.UUID `json:"user_id"`
	CameraID      uuid.UUID `json:"camera_id"`
	Mode          string    `json:"mode"` // webrtc | hls
	CreatedAt     time.Time `json:"created_at"`
	LastSeenAt    time.Time `json:"last_seen_at"`
	FallbackCount int       `json:"fallback_count"`
	LastError     string    `json:"last_error"`
	TTFFMs        int64     `json:"ttff_ms"`
}
