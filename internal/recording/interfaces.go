package recording

import (
	"context"
	"time"

	"github.com/technosupport/ts-vms/internal/crypto"
	"github.com/technosupport/ts-vms/internal/data"
)

// ArchiveIndex is the shared truth used by recorder, playback lookup, retention and evidence workflows.
// Recording writes finalized segments into it. Playback reads from it. UI never bypasses it.
type ArchiveIndex interface {
	Available() bool
	GetSegments(ctx context.Context, cameraID string, from, to time.Time) ([]ArchiveSegment, error)
	GetLatestSegmentEnd(ctx context.Context, cameraID string) (time.Time, error)
	UpsertFinalizedSegment(ctx context.Context, seg *ArchiveSegment) error
	MarkMissing(ctx context.Context, path string) error
	MarkCorrupt(ctx context.Context, path, quarantinePath string) error
	CreateEvent(ctx context.Context, ev *Event) error
	LinkSegmentToEvent(ctx context.Context, eventID, segmentID string) error
	GetCredentials(ctx context.Context, cameraID string) (*data.CameraCredential, error)
	DecryptCredentials(cred *data.CameraCredential, keyring *crypto.Keyring) (string, string, error)
	AuditRecoveryEvent(ctx context.Context, path, state, detail string) error
	ExpectedPathsSince(ctx context.Context, since time.Time) ([]string, error)
	CreateExportJob(ctx context.Context, job *ExportJob, requestedBy string) error
	GetExportJob(ctx context.Context, id string) (*ExportJob, error)
	UpdateExportJob(ctx context.Context, job *ExportJob) error
}

// Keep legacy name for existing handlers/services while redirecting behavior to ArchiveIndex.
type IMetadataDB = ArchiveIndex

type Event struct {
	ID       string    `json:"id"`
	CameraID string    `json:"camera_id"`
	EventTS  time.Time `json:"event_ts"`
	Type     string    `json:"type"`
	Data     string    `json:"data"`
}

type ArchiveSegment struct {
	ID             string    `json:"id"`
	SegmentID      string    `json:"segment_id"`
	TenantID       string    `json:"tenant_id"`
	SiteID         string    `json:"site_id"`
	CameraID       string    `json:"camera_id"`
	StartTS        time.Time `json:"start_ts"`
	EndTS          time.Time `json:"end_ts"`
	DurationMs     int64     `json:"duration_ms"`
	FilePath       string    `json:"file_path"`
	Path           string    `json:"path"`
	FileSize       int64     `json:"file_size"`
	SizeBytes      int64     `json:"size_bytes"`
	Container      string    `json:"container"`
	VideoCodec     string    `json:"video_codec"`
	ChecksumSHA256 string    `json:"checksum_sha256"`
	HealthState    string    `json:"health_state"`
	IsMissing      bool      `json:"is_missing"`
	IsCorrupt      bool      `json:"is_corrupt"`
	IsProtected    bool      `json:"is_protected"`
	EventID        *string   `json:"event_id,omitempty"`
	Finalized      bool      `json:"is_finalized"`
}

type ExportJob struct {
	ID         string    `json:"id"`
	CameraID   string    `json:"camera_id"`
	FromTS     time.Time `json:"from_ts"`
	ToTS       time.Time `json:"to_ts"`
	State      string    `json:"state"`
	OutputPath string    `json:"output_path"`
	Error      string    `json:"error,omitempty"`
}

type Segment = ArchiveSegment
