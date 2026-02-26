package retention

import (
	"strings"
)

type ScopeConfig struct {
	DaysToKeep   int `yaml:"days_to_keep"`
	MaxStorageGB int `yaml:"max_storage_gb"`
}

type TenantConfig struct {
	TenantID    string `yaml:"tenant_id"`
	ScopeConfig `yaml:",inline"`
}

type SiteConfig struct {
	TenantID    string `yaml:"tenant_id"`
	SiteID      string `yaml:"site_id"`
	ScopeConfig `yaml:",inline"`
}

type CameraConfig struct {
	CameraID    string `yaml:"camera_id"`
	ScopeConfig `yaml:",inline"`
}

type Config struct {
	Defaults struct {
		DaysToKeep             int    `yaml:"days_to_keep"`
		MaxStorageGB           int    `yaml:"max_storage_gb"`
		CleanupIntervalMinutes int    `yaml:"cleanup_interval_minutes"`
		DryRun                 bool   `yaml:"dry_run"`
		DeleteMode             string `yaml:"delete_mode"`
	} `yaml:"defaults"`

	Scopes struct {
		Tenants []TenantConfig `yaml:"tenants"`
		Sites   []SiteConfig   `yaml:"sites"`
		Cameras []CameraConfig `yaml:"cameras"`
	} `yaml:"scopes"`

	Protection struct {
		ProtectIfEventLinked bool   `yaml:"protect_if_event_linked"`
		ProtectionSource     string `yaml:"protection_source"`
	} `yaml:"protection"`

	Safety struct {
		NeverDeleteNewerThanMinutes int  `yaml:"never_delete_newer_than_minutes"`
		SkipIfLocked                bool `yaml:"skip_if_locked"`
		IncludeSidecars             bool `yaml:"include_sidecars"`
	} `yaml:"safety"`
}

type PolicyResolver struct {
	config Config
}

func NewPolicyResolver(cfg Config) *PolicyResolver {
	return &PolicyResolver{config: cfg}
}

// Resolve returns the evaluated days and size for a given hierarchy
// Precedence: Camera > Site > Tenant > Global Defaults
func (p *PolicyResolver) Resolve(tenantID, siteID, cameraID string) (daysToKeep int, maxStorageGB int, appliedScope string) {
	daysToKeep = p.config.Defaults.DaysToKeep
	maxStorageGB = p.config.Defaults.MaxStorageGB
	appliedScope = "global"

	// Tenant
	for _, t := range p.config.Scopes.Tenants {
		if strings.EqualFold(t.TenantID, tenantID) {
			daysToKeep = t.DaysToKeep
			maxStorageGB = t.MaxStorageGB
			appliedScope = "tenant"
			break
		}
	}

	// Site
	for _, s := range p.config.Scopes.Sites {
		if strings.EqualFold(s.TenantID, tenantID) && strings.EqualFold(s.SiteID, siteID) {
			daysToKeep = s.DaysToKeep
			maxStorageGB = s.MaxStorageGB
			appliedScope = "site"
			break
		}
	}

	// Camera
	for _, c := range p.config.Scopes.Cameras {
		if strings.EqualFold(c.CameraID, cameraID) {
			daysToKeep = c.DaysToKeep
			maxStorageGB = c.MaxStorageGB
			appliedScope = "camera"
			break
		}
	}

	return daysToKeep, maxStorageGB, appliedScope
}
