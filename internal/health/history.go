package health

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/data"
)

const MaxHistoryPerCamera = 200

type HistoryManager struct {
	repo data.HealthRepository
}

func NewHistoryManager(repo data.HealthRepository) *HistoryManager {
	return &HistoryManager{repo: repo}
}

// AddEntry adds a new history record and enforces boundedness
func (h *HistoryManager) AddEntry(ctx context.Context, tenantID, cameraID uuid.UUID, status data.CameraHealthStatus, reason string, rtt int) error {
	entry := &data.CameraHealthHistory{
		TenantID:   tenantID,
		CameraID:   cameraID,
		OccurredAt: time.Now(),
		Status:     status,
		ReasonCode: reason,
		RTTMS:      rtt,
	}

	// 1. Add new entry
	if err := h.repo.AddHistory(ctx, entry); err != nil {
		return err
	}

	// 2. Prune old entries (Boundedness)
	// This ensures we never grow indefinitely.
	// Pruning could be async or probabilistic if load is high, but strict N requires sync or frequent cleanup.
	// We'll call Prune with the limit.
	return h.repo.PruneHistory(ctx, cameraID, MaxHistoryPerCamera)
}
