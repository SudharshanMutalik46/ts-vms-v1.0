package circuit_breaker

import (
	"log/slog"
	"sync"
	"time"
)

type Config struct {
	Enabled          bool
	WarnFreeGB       float64
	CritFreeGB       float64
	WarnUsagePercent float64
	CritUsagePercent float64
	CheckIntervalSec int
	CooldownSec      int
}

// VolumeStats represents the health of a single storage volume
type VolumeStats struct {
	Path         string
	TotalBytes   uint64
	FreeBytes    uint64
	UsagePercent float64
}

// DiskProvider provides a way to mock disk stats for testing
type DiskProvider interface {
	GetStats(path string) (VolumeStats, error)
}

// Manager evaluates active recording volumes against free space thresholds
type Manager struct {
	cfg      Config
	provider DiskProvider
	volumes  []string

	mu             sync.RWMutex
	engaged        bool
	lastTransition time.Time

	stopCh chan struct{}
}

func NewManager(cfg Config, provider DiskProvider, volumes []string) *Manager {
	return &Manager{
		cfg:      cfg,
		provider: provider,
		volumes:  volumes,
		stopCh:   make(chan struct{}),
	}
}

func (m *Manager) Start() {
	if !m.cfg.Enabled {
		return
	}
	go m.loop()
}

func (m *Manager) Stop() {
	if m.cfg.Enabled {
		close(m.stopCh)
	}
}

func (m *Manager) loop() {
	ticker := time.NewTicker(time.Duration(m.cfg.CheckIntervalSec) * time.Second)
	defer ticker.Stop()

	// initial check
	m.evaluate()

	for {
		select {
		case <-ticker.C:
			m.evaluate()
		case <-m.stopCh:
			return
		}
	}
}

func (m *Manager) evaluate() {
	if len(m.volumes) == 0 {
		return
	}

	criticalVolumes := 0
	warningVolumes := 0

	for _, vol := range m.volumes {
		stats, err := m.provider.GetStats(vol)
		if err != nil {
			slog.Error("circuit_breaker.disk_stat_error", "volume", vol, "error", err)
			continue
		}

		freeGB := float64(stats.FreeBytes) / (1024 * 1024 * 1024)

		isCrit := freeGB < m.cfg.CritFreeGB || stats.UsagePercent > m.cfg.CritUsagePercent
		isWarn := freeGB < m.cfg.WarnFreeGB || stats.UsagePercent > m.cfg.WarnUsagePercent

		if isCrit {
			criticalVolumes++
			slog.Error("circuit_breaker.volume_critical", "volume", vol, "free_gb", freeGB, "usage_pct", stats.UsagePercent)
		} else if isWarn {
			warningVolumes++
			slog.Warn("circuit_breaker.volume_warning", "volume", vol, "free_gb", freeGB, "usage_pct", stats.UsagePercent)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	wasEngaged := m.engaged

	if criticalVolumes > 0 {
		m.engaged = true
	} else if warningVolumes == 0 {
		// Only recover if we are above warning thresholds (creates a buffer so we don't flap)
		m.engaged = false
	}

	if wasEngaged != m.engaged {
		if now.Sub(m.lastTransition).Seconds() >= float64(m.cfg.CooldownSec) {
			m.lastTransition = now
			if m.engaged {
				slog.Error("recording.circuit_breaker.engaged", "critical_volumes", criticalVolumes)
			} else {
				slog.Info("recording.circuit_breaker.released", "critical_volumes", criticalVolumes)
			}
		} else {
			// Revert state if we haven't passed cooldown
			m.engaged = wasEngaged
		}
	}
}

// IsEngaged returns true if the circuit breaker is currently preventing recording
func (m *Manager) IsEngaged() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.engaged
}
