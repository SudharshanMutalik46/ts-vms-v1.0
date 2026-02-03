package data

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type NVR struct {
	ID           uuid.UUID  `json:"id"`
	TenantID     uuid.UUID  `json:"tenant_id"`
	SiteID       uuid.UUID  `json:"site_id"`
	Name         string     `json:"name"`
	Vendor       string     `json:"vendor"`
	IPAddress    string     `json:"ip_address"` // Stored as INET in DB, string here
	Port         int        `json:"port"`
	IsEnabled    bool       `json:"is_enabled"`
	Status       string     `json:"status"` // unknown, online, offline, auth_failed, error
	LastStatusAt *time.Time `json:"last_status_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type NVREventPollState struct {
	TenantID            uuid.UUID  `json:"tenant_id"`
	NVRID               uuid.UUID  `json:"nvr_id"`
	LastSuccessAt       *time.Time `json:"last_success_at"`
	Cursor              *string    `json:"cursor"`
	SinceTS             *time.Time `json:"since_ts"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	LastErrorCode       *string    `json:"last_error_code"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type NVRLink struct {
	ID            uuid.UUID `json:"id"`
	TenantID      uuid.UUID `json:"tenant_id"`
	CameraID      uuid.UUID `json:"camera_id"`
	NVRID         uuid.UUID `json:"nvr_id"`
	NVRChannelRef *string   `json:"nvr_channel_ref,omitempty"`
	RecordingMode string    `json:"recording_mode"` // vms, nvr
	IsEnabled     bool      `json:"is_enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type NVRCredential struct {
	ID             uuid.UUID `json:"id"`
	TenantID       uuid.UUID `json:"tenant_id"`
	NVRID          uuid.UUID `json:"nvr_id"`
	MasterKID      string    `json:"-"`
	DekNonce       []byte    `json:"-"`
	DekCiphertext  []byte    `json:"-"`
	DekTag         []byte    `json:"-"`
	DataNonce      []byte    `json:"-"`
	DataCiphertext []byte    `json:"-"`
	DataTag        []byte    `json:"-"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type NVRChannel struct {
	ID                uuid.UUID      `json:"id"`
	TenantID          uuid.UUID      `json:"tenant_id"`
	SiteID            uuid.UUID      `json:"site_id"`
	NVRID             uuid.UUID      `json:"nvr_id"`
	ChannelRef        string         `json:"channel_ref"`
	Name              string         `json:"name"`
	IsEnabled         bool           `json:"is_enabled"`
	SupportsSubstream *bool          `json:"supports_substream,omitempty"`
	RTSPMain          string         `json:"rtsp_main_url_sanitized"`
	RTSPSub           string         `json:"rtsp_sub_url_sanitized,omitempty"`
	DiscoveredAt      time.Time      `json:"discovered_at"`
	LastSyncedAt      time.Time      `json:"last_synced_at"`
	ValidationStatus  string         `json:"validation_status"`
	LastValidationAt  *time.Time     `json:"last_validation_at,omitempty"`
	LastErrorCode     *string        `json:"last_error_code,omitempty"`
	ProvisionState    string         `json:"provision_state"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

type NVRFilter struct {
	SiteID    *uuid.UUID
	Vendor    *string
	Status    *string
	IsEnabled *bool
	Query     string // Name or IP
}

type NVRChannelFilter struct {
	ProvisionState *string
	IsEnabled      *bool
	Validation     *string
	Query          string
}

type NVRRepository interface {
	// NVR CRUD
	Create(ctx context.Context, nvr *NVR) error
	GetByID(ctx context.Context, id uuid.UUID) (*NVR, error)
	List(ctx context.Context, tenantID uuid.UUID, filter NVRFilter, limit, offset int) ([]*NVR, int, error)
	// System Helper for Monitor
	ListAllNVRs(ctx context.Context) ([]*NVR, error)
	Update(ctx context.Context, nvr *NVR) error
	Delete(ctx context.Context, id uuid.UUID) error

	// Discovery (Phase 2.8)
	UpsertChannel(ctx context.Context, ch *NVRChannel) error
	ListChannels(ctx context.Context, nvrID uuid.UUID, filter NVRChannelFilter, limit, offset int) ([]*NVRChannel, int, error)
	GetChannel(ctx context.Context, id uuid.UUID) (*NVRChannel, error)
	GetChannelByRef(ctx context.Context, nvrID uuid.UUID, ref string) (*NVRChannel, error)
	UpdateChannelStatus(ctx context.Context, id uuid.UUID, validationStatus string, errCode *string) error
	UpdateChannelProvisionState(ctx context.Context, id uuid.UUID, state string) error
	BulkEnableChannels(ctx context.Context, ids []uuid.UUID, enable bool) error

	// Linking
	UpsertLink(ctx context.Context, link *NVRLink) error
	GetLinkByCameraID(ctx context.Context, cameraID uuid.UUID) (*NVRLink, error)
	ListLinks(ctx context.Context, nvrID uuid.UUID, limit, offset int) ([]*NVRLink, error)
	UnlinkCamera(ctx context.Context, cameraID uuid.UUID) error

	// Credentials
	UpsertCredential(ctx context.Context, cred *NVRCredential) error
	GetCredential(ctx context.Context, nvrID uuid.UUID) (*NVRCredential, error)
	DeleteCredential(ctx context.Context, nvrID uuid.UUID) error

	// Health (Phase 2.9)
	UpsertNVRHealth(ctx context.Context, h *NVRHealth) error
	UpsertChannelHealth(ctx context.Context, h *NVRChannelHealth) error
	// GetNVRHealthSummary respects RBAC site scope
	GetNVRHealthSummary(ctx context.Context, tenantID uuid.UUID, siteIDs []uuid.UUID) (*NVRHealthSummary, error)
	ListChannelHealth(ctx context.Context, nvrID uuid.UUID, limit, offset int) ([]*NVRChannelHealth, error)

	// Event Polling
	UpsertEventPollState(ctx context.Context, state *NVREventPollState) error
	GetEventPollState(ctx context.Context, nvrID uuid.UUID) (*NVREventPollState, error)
}

// Health Models
type NVRHealth struct {
	TenantID            uuid.UUID  `json:"tenant_id"`
	NVRID               uuid.UUID  `json:"nvr_id"`
	Status              string     `json:"status"` // online, offline, auth_failed, error
	LastCheckedAt       time.Time  `json:"last_checked_at"`
	LastSuccessAt       *time.Time `json:"last_success_at,omitempty"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	LastErrorCode       *string    `json:"last_error_code,omitempty"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type NVRChannelHealth struct {
	TenantID            uuid.UUID  `json:"tenant_id"`
	NVRID               uuid.UUID  `json:"nvr_id"`
	ChannelID           uuid.UUID  `json:"channel_id"`
	Status              string     `json:"status"` // online, offline, auth_failed, stream_error
	LastCheckedAt       time.Time  `json:"last_checked_at"`
	LastSuccessAt       *time.Time `json:"last_success_at,omitempty"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	LastErrorCode       *string    `json:"last_error_code,omitempty"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type NVRHealthSummary struct {
	TotalNVRs            int `json:"total_nvrs"`
	NVRsOnline           int `json:"nvrs_online"`
	NVRsOffline          int `json:"nvrs_offline"`
	NVRsAuthFailed       int `json:"nvrs_auth_failed"`
	NVRsError            int `json:"nvrs_error"`
	TotalChannelsEnabled int `json:"total_channels_enabled"`
	ChannelsOnline       int `json:"channels_online"`
	ChannelsOffline      int `json:"channels_offline"`
	ChannelsAuthFailed   int `json:"channels_auth_failed"`
	ChannelsStreamError  int `json:"channels_stream_error"`
	// Dynamic/Cascaded
	ChannelsUnreachableDueToNVR int `json:"channels_unreachable_due_to_nvr"`
}
