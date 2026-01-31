package license

import (
	"context"

	"github.com/google/uuid"
)

// UsageProvider interface allows swapping in the real DB counter later
type UsageProvider interface {
	CurrentUsage(ctx context.Context, tenantID uuid.UUID) (UsageStats, error)
}

type UsageStats struct {
	Cameras int
	NVRs    int
	// Add feature specific usage here if needed
}

// StubUsageProvider implementation for Phase 1.6
// Real implementation waits for Phase 2 Database Tables.
type StubUsageProvider struct {
	// In future, inject DB connection here
}

func (s *StubUsageProvider) CurrentUsage(ctx context.Context, tenantID uuid.UUID) (UsageStats, error) {
	// TODO: Wire to real DB counts in Phase 2
	return UsageStats{
		Cameras: 0,
		NVRs:    0,
	}, nil
}
