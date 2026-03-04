package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

var (
	ErrNoStorageAvailable = errors.New("NO_STORAGE_AVAILABLE")
	ErrQuotaExceeded      = errors.New("QUOTA_EXCEEDED")
)

// Config models matching storage.yaml
type VolumeConfig struct {
	ID              string  `yaml:"id"`
	Path            string  `yaml:"path"`
	Tier            string  `yaml:"tier"`
	Priority        int     `yaml:"priority"`
	MaxUsagePercent float64 `yaml:"max_usage_percent"`
	ReservedFreeGB  float64 `yaml:"reserved_free_gb"`
	Enabled         bool    `yaml:"enabled"`
}

type QuotasConfig struct {
	TenantLimits map[string]float64 `yaml:"tenant_limits"` // in GB
	SiteLimits   map[string]float64 `yaml:"site_limits"`   // in GB
}

type AlertsConfig struct {
	WarnAtPercent        float64 `yaml:"warn_at_percent"`
	CritAtPercent        float64 `yaml:"crit_at_percent"`
	CheckIntervalSeconds int     `yaml:"check_interval_seconds"`
}

type StorageConfig struct {
	Volumes []VolumeConfig `yaml:"volumes"`
	Quotas  QuotasConfig   `yaml:"quotas"`
	Alerts  AlertsConfig   `yaml:"alerts"`
}

type VolumeStats struct {
	TotalGB float64
	UsedGB  float64
	FreeGB  float64
}

// StatsProvider interface allows mocking disk usage during testing
type StatsProvider interface {
	GetStats(path string) (VolumeStats, error)
}

// QuotaProvider interface abstracts away DB lookups for per-tenant/site usage
type QuotaProvider interface {
	GetTenantUsage(tenantID string) float64
	GetSiteUsage(siteID string) float64
}

// Planner resolves paths, evaluates storage health, and selects volumes dynamically
type Planner struct {
	mu     sync.RWMutex
	config *StorageConfig
	stats  StatsProvider
	quotas QuotaProvider
}

// NewPlanner initializes a Thread-safe Planner
func NewPlanner(cfg *StorageConfig, sp StatsProvider, qp QuotaProvider) *Planner {
	return &Planner{
		config: cfg,
		stats:  sp,
		quotas: qp,
	}
}

// ResolvePath builds the hierarchical storage directory: <vol>/<tenant>/<site>/<camera>/YYYY-MM-DD/HH/
func (p *Planner) ResolvePath(volPath, tenantID, siteID, cameraID string, ts time.Time) string {
	dateStr := ts.Format("2006-01-02")
	hourStr := ts.Format("15")
	return filepath.Join(volPath, tenantID, siteID, cameraID, dateStr, hourStr)
}

// EnsureDir safely creates the physical hierarchical directories, maintaining 0755
func (p *Planner) EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// SegmentFileName computes stable, sortable file names for recorded segments
func (p *Planner) SegmentFileName(cameraID string, startTs time.Time, durationSec int, seq int) string {
	// format: <cam>_<unix_ms>_<duration>_<seq>.mkv
	return fmt.Sprintf("%s_%d_%d_%05d.mkv", cameraID, startTs.UnixMilli(), durationSec, seq)
}

// ChooseVolume finds the best contiguous volume on disk based on Tier, Priority, and active quotas.
// Handled automatically: Spillover logic to secondary volumes if usage thresholds are breached.
func (p *Planner) ChooseVolume(tierPref, tenantID, siteID string) (*VolumeConfig, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// 1. Quota Verification (if enabled)
	if p.quotas != nil {
		if limit, exists := p.config.Quotas.TenantLimits[tenantID]; exists {
			if p.quotas.GetTenantUsage(tenantID) >= limit {
				return nil, ErrQuotaExceeded
			}
		}
		if limit, exists := p.config.Quotas.SiteLimits[siteID]; exists {
			if p.quotas.GetSiteUsage(siteID) >= limit {
				return nil, ErrQuotaExceeded
			}
		}
	}

	// 2. Filter available & enabled volumes matching Tier Preference
	var eligible []VolumeConfig
	for _, v := range p.config.Volumes {
		if !v.Enabled || v.Tier != tierPref {
			continue
		}

		stats, err := p.stats.GetStats(v.Path)
		if err != nil {
			// Skip volume if offline or permission denied
			continue
		}

		// Compute Usage %
		var usagePercent float64 = 0
		if stats.TotalGB > 0 {
			usagePercent = (stats.UsedGB / stats.TotalGB) * 100.0
		}

		// Spillover Condition Check
		if usagePercent < v.MaxUsagePercent && stats.FreeGB > v.ReservedFreeGB {
			eligible = append(eligible, v)
		}
	}

	if len(eligible) == 0 {
		return nil, ErrNoStorageAvailable
	}

	// 3. Sort by priority tier configurations (Low Int = High Priority)
	sort.Slice(eligible, func(i, j int) bool {
		return eligible[i].Priority < eligible[j].Priority
	})

	return &eligible[0], nil
}
