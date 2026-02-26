package retention

import (
	"testing"
	"time"
)

type MockProtector struct {
	protected map[string]bool
}

func (m *MockProtector) IsProtected(cameraID string, filename string) bool {
	return m.protected[filename]
}

type MockAudit struct {
	records []AuditRecord
}

func (m *MockAudit) Write(r AuditRecord) {
	m.records = append(m.records, r)
}

type MockVerifier struct {
	reclaimed int64
}

func (m *MockVerifier) GetFreeSpace(path string) (uint64, error) { return 1000, nil }
func (m *MockVerifier) VerifyReclamation(path string, exp int64, b uint64) {
	m.reclaimed = exp
}

type MockEnumerator struct {
	segments []SegmentMeta
}

func (m *MockEnumerator) Enumerate(vol string) ([]SegmentMeta, error) {
	return m.segments, nil
}

func TestPolicyResolverPrecedence(t *testing.T) {
	cfg := Config{}
	cfg.Defaults.DaysToKeep = 7
	cfg.Scopes.Tenants = []TenantConfig{{TenantID: "t1", ScopeConfig: ScopeConfig{DaysToKeep: 14}}}
	cfg.Scopes.Sites = []SiteConfig{{TenantID: "t1", SiteID: "s1", ScopeConfig: ScopeConfig{DaysToKeep: 30}}}
	cfg.Scopes.Cameras = []CameraConfig{{CameraID: "c1", ScopeConfig: ScopeConfig{DaysToKeep: 60}}}

	pr := NewPolicyResolver(cfg)

	// Global fallback
	d, _, s := pr.Resolve("t2", "s2", "c2")
	if d != 7 || s != "global" {
		t.Errorf("Expected global 7, got %d %s", d, s)
	}

	// Tenant match
	d, _, s = pr.Resolve("t1", "s2", "c2")
	if d != 14 || s != "tenant" {
		t.Errorf("Expected tenant 14, got %d %s", d, s)
	}

	// Site match
	d, _, s = pr.Resolve("t1", "s1", "c2")
	if d != 30 || s != "site" {
		t.Errorf("Expected site 30, got %d %s", d, s)
	}

	// Camera match
	d, _, s = pr.Resolve("t1", "s1", "c1")
	if d != 60 || s != "camera" {
		t.Errorf("Expected camera 60, got %d %s", d, s)
	}
}

func TestEngineLogic(t *testing.T) {
	cfg := Config{}
	cfg.Defaults.DaysToKeep = 7
	cfg.Defaults.MaxStorageGB = 1 // extremely small cap to force size deletion testing
	cfg.Defaults.DryRun = true
	cfg.Safety.NeverDeleteNewerThanMinutes = 15

	now := time.Now()

	segments := []SegmentMeta{
		{TenantID: "t1", SiteID: "s1", CameraID: "c1", Filename: "old.mp4", StartTime: now.AddDate(0, 0, -10), SizeBytes: 100},              // should delete (days)
		{TenantID: "t1", SiteID: "s1", CameraID: "c1", Filename: "prot.mp4", StartTime: now.AddDate(0, 0, -10), SizeBytes: 100},             // should keep (protected)
		{TenantID: "t1", SiteID: "s1", CameraID: "c1", Filename: "recent.mp4", StartTime: now.AddDate(0, 0, -1), SizeBytes: 2147483648},     // 2GB, will trigger size deletion
		{TenantID: "t1", SiteID: "s1", CameraID: "c1", Filename: "newest.mp4", StartTime: now.Add(-5 * time.Minute), SizeBytes: 2147483648}, // 2GB but within 15 mins
	}

	prot := &MockProtector{protected: map[string]bool{"prot.mp4": true}}
	enum := &MockEnumerator{segments: segments}
	audit := &MockAudit{}
	verif := &MockVerifier{}

	engine := NewRetentionEngine(cfg, prot, enum, verif, audit)
	engine.RunOnce(now)

	// old.mp4 deleted due to days.
	// recent.mp4 deleted due to size (cap is 1GB, we have >4GB).
	// newest.mp4 kept due to safety (newer than 15 mins).
	// prot.mp4 kept due to protection.

	var delDays, delSize, skipProt int
	for _, r := range audit.records {
		if r.EventType == "retention.delete" {
			if r.Reason == "days_policy" && r.SegmentPath == segments[0].Path {
				delDays++
			}
			if r.Reason == "size_policy" && r.SegmentPath == segments[2].Path {
				delSize++
			}
		} else if r.EventType == "retention.skip_protected" {
			skipProt++
		}
	}

	status := engine.GetStatus()

	if delDays != 1 {
		t.Errorf("Expected 1 days_policy delete, got %d", delDays)
	}
	if delSize != 1 {
		t.Errorf("Expected 1 size_policy delete, got %d", delSize)
	}
	if status.SkippedProtected != 1 {
		t.Errorf("Expected 1 protected skip, got %d", status.SkippedProtected)
	}
	if status.DeletedCount != 2 {
		t.Errorf("Expected 2 total deletions, got %d", status.DeletedCount)
	}
}
