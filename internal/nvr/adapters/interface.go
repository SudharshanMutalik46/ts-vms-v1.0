package adapters

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Target info required to connect to an NVR
type NvrTarget struct {
	TenantID uuid.UUID
	NVRID    uuid.UUID
	SiteID   uuid.UUID
	IP       string
	Port     int
	Vendor   string
}

// Credential for NVR (in-memory only)
type NvrCredential struct {
	Username string
	Password string
	AuthType string // "digest", "basic", "onvif"
}

// Simplified Device Info
type NvrDeviceInfo struct {
	Manufacturer        string `json:"manufacturer"`
	Model               string `json:"model"`
	FirmwareVersion     string `json:"firmware_version"`
	SerialNumber        string `json:"serial_number"`
	CapabilitiesSummary string `json:"capabilities_summary"`
}

// Channel metadata
type NvrChannel struct {
	ChannelRef        string            `json:"channel_ref"`
	Name              string            `json:"name"`
	Enabled           bool              `json:"enabled"`
	SupportsSubStream bool              `json:"supports_sub_stream"` // true/false or assume false
	RTSPMain          string            `json:"rtsp_main"`           // Sanitized
	RTSPSub           string            `json:"rtsp_sub"`            // Sanitized
	Metadata          map[string]string `json:"metadata,omitempty"`
}

// Common Event Model
type NvrEvent struct {
	EventType     string                 `json:"event_type"` // motion, tamper, line_crossing, unknown
	Severity      string                 `json:"severity"`   // info, warn, critical
	ChannelRef    string                 `json:"channel_ref,omitempty"`
	OccurredAt    time.Time              `json:"occurred_at"`
	RawVendorType string                 `json:"raw_vendor_type"`
	RawPayload    map[string]interface{} `json:"raw_payload,omitempty"` // redacts secrets
}

// The core Adapter Interface
type Adapter interface {
	// Get version/capabilities of device
	GetDeviceInfo(ctx context.Context, target NvrTarget, cred NvrCredential) (NvrDeviceInfo, error)

	// List channels available on NVR
	ListChannels(ctx context.Context, target NvrTarget, cred NvrCredential) ([]NvrChannel, error)

	// Fetch recent events (bounded)
	FetchEvents(ctx context.Context, target NvrTarget, cred NvrCredential, since time.Time, limit int) ([]NvrEvent, int, error)

	// Helper to extract RTSP URLs for a specific channel
	GetRtspUrls(ctx context.Context, target NvrTarget, cred NvrCredential, channelRef string) (string, string, error)

	// Kind string (hikvision, dahua, onvif, rtsp)
	Kind() string
}

// Factory Helper
type Factory func(target NvrTarget, cred NvrCredential) (Adapter, error)
