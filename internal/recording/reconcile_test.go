package recording

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadCameraSegmentsFallsBackToSiteRoot(t *testing.T) {
	root := t.TempDir()

	cfg := &Config{}
	cfg.Global.StorageRoot = root
	cfg.Global.DefaultTenantID = "tenant_sys"
	cfg.Global.DefaultSiteID = "site_hq"
	cfg.Global.SegmentDurationSec = 60

	r := NewReconciler(cfg, nil)

	base := time.Date(2026, 4, 4, 22, 10, 0, 0, time.Local)
	from := base.Add(-2 * time.Hour)
	to := base.Add(1 * time.Hour)

	writeVideo := func(cameraID, runDir, name string, modTime time.Time) string {
		path := filepath.Join(root, cfg.Global.DefaultTenantID, cfg.Global.DefaultSiteID, cameraID, runDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte("test video"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
		return path
	}

	writeVideo("cam-a", "20260404_220455", "segment_00000.mkv", base.Add(-30*time.Minute))
	writeVideo("cam-b", "20260404_220503", "segment_00000.mkv", base.Add(-29*time.Minute))
	writeVideo("cam-b", "20260404_220503", "segment_00001.mkv", base.Add(-28*time.Minute))

	segments, err := r.LoadCameraSegments(context.Background(), "room-1", from, to)
	if err != nil {
		t.Fatalf("LoadCameraSegments: %v", err)
	}
	if len(segments) != 2 {
		t.Fatalf("expected 2 fallback segments, got %d", len(segments))
	}
	for _, seg := range segments {
		if seg.CameraID != "cam-b" {
			t.Fatalf("expected fallback to prefer cam-b, got camera %q", seg.CameraID)
		}
	}
}
