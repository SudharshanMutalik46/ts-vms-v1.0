package storage

import (
	"context"
	"log"
	"time"
)

// Monitor acts as a background runner auditing the actual free space on volumes over time.
// It watches volumes bound to the Planner and fires warnings to logs or telemetry sinks.
type Monitor struct {
	planner *Planner
}

func NewMonitor(p *Planner) *Monitor {
	return &Monitor{planner: p}
}

// Run begins the infinite monitor loop utilizing the config's standard check interval
func (m *Monitor) Run(ctx context.Context) {
	interval := time.Duration(m.planner.config.Alerts.CheckIntervalSeconds) * time.Second
	if interval == 0 {
		interval = 60 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkVolumes()
		}
	}
}

func (m *Monitor) checkVolumes() {
	m.planner.mu.RLock()
	defer m.planner.mu.RUnlock()

	for _, v := range m.planner.config.Volumes {
		if !v.Enabled {
			continue
		}

		stats, err := m.planner.stats.GetStats(v.Path)
		if err != nil {
			log.Printf("ERROR storage.volume_read_fail vol=%s path=%s err=%v", v.ID, v.Path, err)
			continue
		}

		usagePercent := 0.0
		if stats.TotalGB > 0 {
			usagePercent = (stats.UsedGB / stats.TotalGB) * 100.0
		}

		// Emit Status Event Baseline
		log.Printf("EVENT storage.volume.status vol=%s tier=%s total=%.2fGB used=%.2fGB free=%.2fGB usage=%.2f%%",
			v.ID, v.Tier, stats.TotalGB, stats.UsedGB, stats.FreeGB, usagePercent)

		warnAt := m.planner.config.Alerts.WarnAtPercent
		critAt := m.planner.config.Alerts.CritAtPercent

		// Storage Threshold Alert Handlers
		if usagePercent >= critAt {
			log.Printf("CRITICAL storage.volume.low_space_critical vol=%s usage=%.2f%% limit=%.2f%%", v.ID, usagePercent, critAt)
		} else if usagePercent >= warnAt {
			log.Printf("WARNING storage.volume.low_space_warning vol=%s usage=%.2f%% limit=%.2f%%", v.ID, usagePercent, warnAt)
		}
	}
}
