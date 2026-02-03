package nvr

import (
	"time"

	"github.com/google/uuid"
)

// VmsEvent is the normalized event envelope for Phase 2.10
type VmsEvent struct {
	EventID    uuid.UUID  `json:"event_id"`
	Source     string     `json:"source"` // "nvr"
	Vendor     string     `json:"vendor"` // "hikvision", "dahua", "onvif"
	TenantID   uuid.UUID  `json:"tenant_id"`
	SiteID     uuid.UUID  `json:"site_id"`
	NVRID      uuid.UUID  `json:"nvr_id"`
	CameraID   *uuid.UUID `json:"camera_id"`   // Nullable
	ChannelRef string     `json:"channel_ref"` // e.g. "1", "0", "Token-1"

	EventType string `json:"event_type"` // "motion", "tamper", "disk_full", "unknown"
	Severity  string `json:"severity"`   // "info", "warn", "critical"

	OccurredAt time.Time `json:"occurred_at"`
	ReceivedAt time.Time `json:"received_at"`

	DedupKey string `json:"dedup_key"`

	Snapshot VmsSnapshot `json:"snapshot"`

	Raw map[string]interface{} `json:"raw"` // Redacted
}

type VmsSnapshot struct {
	VendorRef string `json:"vendor_ref,omitempty"`
	Requested bool   `json:"requested,omitempty"` // If true, look for snapshots.request
}
