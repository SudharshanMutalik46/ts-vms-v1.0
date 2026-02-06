package live

import (
	"time"

	"github.com/google/uuid"
)

// ReasonCode enum for telemetry and logs
type ReasonCode string

const (
	ReasonConnectTimeout      ReasonCode = "CONNECT_TIMEOUT"      // 5s deadline
	ReasonTrackTimeout        ReasonCode = "TRACK_TIMEOUT"        // 2s deadline after connect
	ReasonICEFailed           ReasonCode = "ICE_FAILED"           // ICE state failed
	ReasonDTLSFailed          ReasonCode = "DTLS_FAILED"          // DTLS handshake failed
	ReasonSFUSignalingFailed  ReasonCode = "SFU_SIGNALING_FAILED" // API/WS error
	ReasonSFUBusy             ReasonCode = "SFU_BUSY"             // Capacity limit
	ReasonPermissionDenied    ReasonCode = "PERMISSION_DENIED"    // RBAC/Key issue
	ReasonBrowserNotSupported ReasonCode = "BROWSER_NOT_SUPPORTED"
	ReasonUnknown             ReasonCode = "UNKNOWN"
)

const (
	ErrLiveLimitExceeded = "LIVE_LIMIT_EXCEEDED"
)

// LiveSessionResponse defines the dual-path contract
type LiveSessionResponse struct {
	ViewerSessionID string           `json:"viewer_session_id"`
	ExpiresAt       int64            `json:"expires_at"`       // Unix MS
	Primary         string           `json:"primary"`          // "webrtc"
	Fallback        string           `json:"fallback"`         // "hls"
	SelectedQuality string           `json:"selected_quality"` // "sub" or "main"
	WebRTC          *WebRTCBlock     `json:"webrtc"`
	HLS             *HLSBlock        `json:"hls"`
	FallbackPolicy  *FallbackPolicy  `json:"fallback_policy"`
	TelemetryPolicy *TelemetryPolicy `json:"telemetry_policy"`
}

type WebRTCBlock struct {
	SFUURL           string `json:"sfu_url"`
	RoomID           string `json:"room_id"`
	ConnectTimeoutMs int    `json:"connect_timeout_ms"`
}

type HLSBlock struct {
	PlaylistURL     string `json:"playlist_url"`
	TargetLatencyMs int    `json:"target_latency_ms"`
}

type FallbackPolicy struct {
	WebRTCConnectTimeoutMs int   `json:"webrtc_connect_timeout_ms"`
	WebRTCTrackTimeoutMs   int   `json:"webrtc_track_timeout_ms"`
	MaxAutoRetries         int   `json:"max_auto_retries"`
	RetryBackoffMs         []int `json:"retry_backoff_ms"`
}

type TelemetryPolicy struct {
	Endpoint string `json:"client_event_endpoint"`
}

// TelemetryEvent ingestion struct
type TelemetryEvent struct {
	ViewerSessionID string     `json:"viewer_session_id"`
	CameraID        string     `json:"camera_id"`
	EventType       string     `json:"event_type"` // e.g., "fallback_to_hls"
	ReasonCode      ReasonCode `json:"reason_code"`
	Mode            string     `json:"mode"`
	TTFFMs          int        `json:"ttff_ms,omitempty"`
	TimestampMs     int64      `json:"ts_unix_ms"`
}

// ViewerSession stored in Redis
type ViewerSession struct {
	ID            string    `json:"id"`
	TenantID      uuid.UUID `json:"tenant_id"`
	UserID        uuid.UUID `json:"user_id"`
	CameraID      string    `json:"camera_id"`
	Mode          string    `json:"mode"`
	CreatedAt     time.Time `json:"created_at"`
	LastSeenAt    time.Time `json:"last_seen_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	FallbackCount int       `json:"fallback_count"`
	LastError     string    `json:"last_error"`
}
