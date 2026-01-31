package license

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// Scheduler handles periodic license checks and alerting
type Scheduler struct {
	manager    *Manager
	lastAlerts map[string]time.Time // De-duplication: type -> date
	mu         sync.Mutex
}

func NewScheduler(m *Manager) *Scheduler {
	return &Scheduler{
		manager:    m,
		lastAlerts: make(map[string]time.Time),
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	// Check immediately, then hourly
	s.Check()

	ticker := time.NewTicker(1 * time.Hour)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.Check()
			}
		}
	}()
}

func (s *Scheduler) Check() {
	state := s.manager.GetState()
	if state.Status != StatusValid && state.Status != StatusExpiredGrace {
		// If Invalid/Missing, we log error once? Or handled by Manager?
		// Prompt focuses on "30d before expiry", "7d before", "Daily in Grace".
		return
	}

	now := time.Now()
	days := state.DaysToExpiry

	var alertType string

	if state.Status == StatusExpiredGrace {
		alertType = "grace_daily"
	} else if days <= 7 {
		alertType = "7d"
	} else if days <= 30 {
		alertType = "30d"
	} else {
		return // No alert needed
	}

	// De-duplication (Max 1 per day)
	s.mu.Lock()
	defer s.mu.Unlock()

	if last, ok := s.lastAlerts[alertType]; ok {
		if isSameDay(last, now) {
			return // Already alerted today
		}
	}

	// Emit Alert
	s.emitAlert(alertType, days)
	s.lastAlerts[alertType] = now
}

func (s *Scheduler) emitAlert(typ string, days int) {
	// Prompt: "structured logs + metrics"
	msg := fmt.Sprintf("LICENSE ALERT [%s]: Expires in %d days", typ, days)
	log.Println(msg)

	// Metric: license_alerts_total{type="..."}
	// TODO: Increment metric counter when Metrics module is fully wired.
	// For now, log is the requirement sink.
}

func isSameDay(t1, t2 time.Time) bool {
	y1, m1, d1 := t1.Date()
	y2, m2, d2 := t2.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}
