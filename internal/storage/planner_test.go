package storage

import (
	"path/filepath"
	"testing"
	"time"
)

// ---------- Mock implementations for Storage tests ---------- //

type mockStats map[string]VolumeStats

func (m mockStats) GetStats(path string) (VolumeStats, error) { // Note value receiver handles map simply here
	// Ensure paths match OS natively during lookup since config paths can have varied slashes
	cleaned := filepath.Clean(path)
	for k, v := range m {
		if filepath.Clean(k) == cleaned {
			return v, nil
		}
	}
	// default empty
	return VolumeStats{}, nil
}

type mockQuota struct {
	tenant map[string]float64
	site   map[string]float64
}

func (m *mockQuota) GetTenantUsage(tenantID string) float64 { return m.tenant[tenantID] }
func (m *mockQuota) GetSiteUsage(siteID string) float64     { return m.site[siteID] }

// ---------- Test Cases ---------- //

func defaultSetup() *StorageConfig {
	return &StorageConfig{
		Volumes: []VolumeConfig{
			{ID: "v1", Path: "C:\\ts_vms_storage\\hot1", Tier: "hot", Priority: 1, MaxUsagePercent: 80.0, ReservedFreeGB: 50.0, Enabled: true},
			{ID: "v2", Path: "D:\\ts_vms_storage\\hot2", Tier: "hot", Priority: 2, MaxUsagePercent: 80.0, ReservedFreeGB: 50.0, Enabled: true},
			{ID: "v3", Path: "E:\\ts_vms_storage\\warm1", Tier: "warm", Priority: 1, MaxUsagePercent: 90.0, ReservedFreeGB: 100.0, Enabled: true},
		},
		Quotas: QuotasConfig{
			TenantLimits: map[string]float64{"tenantA": 500},
			SiteLimits:   map[string]float64{"siteB": 250},
		},
	}
}

func TestResolvePath(t *testing.T) {
	cfg := defaultSetup()
	p := NewPlanner(cfg, nil, nil)
	ts := time.Date(2026, 2, 26, 14, 30, 0, 0, time.UTC)

	actual := p.ResolvePath("C:\\hot1", "tA", "sA", "cam1", ts)
	expected := filepath.Join("C:\\hot1", "tA", "sA", "cam1", "2026-02-26", "14")
	if actual != expected {
		t.Fatalf("ResolvePath failed.\nGot: %s\nExp: %s", actual, expected)
	}
}

func TestSegmentFileName(t *testing.T) {
	p := NewPlanner(defaultSetup(), nil, nil)
	ts := time.UnixMilli(1708945200000)

	actual := p.SegmentFileName("camX", ts, 60, 42)
	expected := "camX_1708945200000_60_00042.mkv"
	if actual != expected {
		t.Fatalf("SegmentFileName failed.\nGot: %s\nExp: %s", actual, expected)
	}
}

func TestChooseVolume_Prioritization(t *testing.T) {
	cfg := defaultSetup()
	ms := mockStats{
		"C:\\ts_vms_storage\\hot1": {TotalGB: 1000, UsedGB: 100, FreeGB: 900}, // 10%
		"D:\\ts_vms_storage\\hot2": {TotalGB: 1000, UsedGB: 0, FreeGB: 1000},  // 0%
	}
	mq := &mockQuota{tenant: make(map[string]float64), site: make(map[string]float64)}

	p := NewPlanner(cfg, ms, mq)
	vol, err := p.ChooseVolume("hot", "tA", "sA")
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if vol.ID != "v1" { // Priority 1 over 2
		t.Fatalf("Expected v1 to be selected by Priority initially, got %s", vol.ID)
	}
}

func TestChooseVolume_Spillover(t *testing.T) {
	cfg := defaultSetup()
	ms := mockStats{
		"C:\\ts_vms_storage\\hot1": {TotalGB: 1000, UsedGB: 850, FreeGB: 150}, // 85% - Over max of 80% !
		"D:\\ts_vms_storage\\hot2": {TotalGB: 1000, UsedGB: 100, FreeGB: 900}, // 10%
	}
	mq := &mockQuota{tenant: make(map[string]float64), site: make(map[string]float64)}

	p := NewPlanner(cfg, ms, mq)
	vol, err := p.ChooseVolume("hot", "tA", "sA")
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if vol.ID != "v2" { // Priority 1 breached, Spillover to Priority 2
		t.Fatalf("Expected spillover to v2, got %s", vol.ID)
	}
}

func TestChooseVolume_QuotaHit(t *testing.T) {
	cfg := defaultSetup()
	ms := mockStats{
		"C:\\ts_vms_storage\\hot1": {TotalGB: 1000, UsedGB: 10, FreeGB: 990},
	}
	mq := &mockQuota{tenant: map[string]float64{"tenantA": 501}, site: make(map[string]float64)}

	p := NewPlanner(cfg, ms, mq)
	_, err := p.ChooseVolume("hot", "tenantA", "sA")
	if err != ErrQuotaExceeded {
		t.Fatalf("Expected Quota Exceeded error, got %v", err)
	}
}
