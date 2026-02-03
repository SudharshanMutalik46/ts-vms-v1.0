package data

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type CameraHealthStatus string

const (
	HealthStatusOnline      CameraHealthStatus = "ONLINE"
	HealthStatusOffline     CameraHealthStatus = "OFFLINE"
	HealthStatusAuthFailed  CameraHealthStatus = "AUTH_FAILED"
	HealthStatusStreamError CameraHealthStatus = "STREAM_ERROR"
)

type CameraHealthCurrent struct {
	TenantID            uuid.UUID          `json:"tenant_id"`
	CameraID            uuid.UUID          `json:"camera_id"`
	Status              CameraHealthStatus `json:"status"`
	LastCheckedAt       time.Time          `json:"last_checked_at"`
	LastSuccessAt       *time.Time         `json:"last_success_at,omitempty"`
	ConsecutiveFailures int                `json:"consecutive_failures"`
	LastErrorCode       string             `json:"last_error_code,omitempty"`
	UpdatedAt           time.Time          `json:"updated_at"`

	// Enriched Fields (Phase 2.6 NVR Context)
	NVRLinked       bool               `json:"nvr_linked"`
	NVRID           *uuid.UUID         `json:"nvr_id,omitempty"`
	NVRStatus       string             `json:"nvr_status,omitempty"`
	EffectiveStatus CameraHealthStatus `json:"effective_status,omitempty"`
	EffectiveReason string             `json:"effective_reason,omitempty"`
}

type CameraHealthHistory struct {
	ID         uuid.UUID          `json:"id"`
	TenantID   uuid.UUID          `json:"tenant_id"`
	CameraID   uuid.UUID          `json:"camera_id"`
	OccurredAt time.Time          `json:"occurred_at"`
	Status     CameraHealthStatus `json:"status"`
	ReasonCode string             `json:"reason_code,omitempty"`
	RTTMS      int                `json:"rtt_ms,omitempty"`
}

type CameraAlert struct {
	ID             uuid.UUID  `json:"id"`
	TenantID       uuid.UUID  `json:"tenant_id"`
	CameraID       uuid.UUID  `json:"camera_id"`
	Type           string     `json:"type"`
	State          string     `json:"state"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	LastNotifiedAt *time.Time `json:"last_notified_at,omitempty"`
}

// CameraHealthTarget is a minimal struct for the scheduler
type CameraHealthTarget struct {
	TenantID            uuid.UUID
	CameraID            uuid.UUID
	RTSPURL             string
	Status              CameraHealthStatus
	LastCheckedAt       time.Time
	ConsecutiveFailures int
}

// HealthRepository defines operations for health monitoring
type HealthRepository interface {
	UpsertStatus(ctx context.Context, h *CameraHealthCurrent) error
	GetStatus(ctx context.Context, cameraID uuid.UUID) (*CameraHealthCurrent, error)
	AddHistory(ctx context.Context, h *CameraHealthHistory) error
	PruneHistory(ctx context.Context, cameraID uuid.UUID, maxRecords int) error
	GetHistory(ctx context.Context, cameraID uuid.UUID, limit, offset int) ([]*CameraHealthHistory, error)

	UpsertAlert(ctx context.Context, a *CameraAlert) error
	GetOpenAlert(ctx context.Context, cameraID uuid.UUID, alertType string) (*CameraAlert, error)
	CloseAlert(ctx context.Context, alertID uuid.UUID) error
	ListAlerts(ctx context.Context, tenantID uuid.UUID, state string) ([]*CameraAlert, error)

	ListStatuses(ctx context.Context, tenantID uuid.UUID) ([]*CameraHealthCurrent, error)

	ListTargets(ctx context.Context) ([]CameraHealthTarget, error)
	GetTarget(ctx context.Context, cameraID uuid.UUID) (*CameraHealthTarget, error)
}
