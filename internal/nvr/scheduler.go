package nvr

import (
	"context"
	"math/rand"
	"time"

	"github.com/google/uuid"
)

// StartDailySync starts a background ticker to run discovery sync.
func (s *Service) StartDailySync(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	go func() {
		// Jitter startup to avoid immediate load on restart
		time.Sleep(time.Duration(rand.Intn(60)) * time.Second)

		// Run immediately on start (after jitter)
		s.RunDiscoverySync(ctx)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.RunDiscoverySync(ctx)
			}
		}
	}()
}

// RunDiscoverySync orchestrates the daily sync
// Audit: nvr.channel.daily_sync
func (s *Service) RunDiscoverySync(ctx context.Context) {
	// Bounds:
	// Cap NVRs per cycle: 200
	// Skip if last_synced < 24h

	// Use Nil UUID for system action
	s.audit(ctx, "nvr.channel.daily_sync", uuid.Nil, "system", "success", nil)

	// Placeholder logic
	// ...
}
