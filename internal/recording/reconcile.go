package recording

import (
	"context"
	"os"
	"path/filepath"
	"time"

	rec "github.com/technosupport/ts-vms/internal/recording/recovery"
)

type Reconciler struct {
	Config  *Config
	Store   *PostgresStore
	Scanner *rec.Scanner
}

func NewReconciler(cfg *Config, store *PostgresStore) *Reconciler {
	rcfg := rec.Config{
		Enabled:             cfg.FailoverRecovery.Enabled,
		RestartBackoffSec:   cfg.FailoverRecovery.RestartBackoffSec,
		DBRequiredForReady:  cfg.FailoverRecovery.DBRequiredForReady,
		OrphanReconcileMode: cfg.FailoverRecovery.OrphanReconcileMode,
	}
	return &Reconciler{Config: cfg, Store: store, Scanner: rec.NewScanner(rcfg)}
}

func (r *Reconciler) Run(ctx context.Context) error {
	findings, _, err := r.Scanner.Scan([]string{r.Config.Global.StorageRoot})
	if err != nil {
		return err
	}
	dbEnabled := r.Store != nil && r.Store.Available()
	seen := make(map[string]struct{}, len(findings))
	for _, f := range findings {
		seen[f.Path] = struct{}{}
		switch f.Kind {
		case "tmp":
			_ = os.Remove(f.Path)
			if dbEnabled {
				_ = r.Store.AuditRecoveryEvent(ctx, f.Path, "tmp_deleted", "")
			}
		case "video":
			if !dbEnabled {
				continue
			}
			seg := &Segment{
				TenantID:   r.Config.Global.DefaultTenantID,
				SiteID:     r.Config.Global.DefaultSiteID,
				CameraID:   inferCameraIDFromPath(f.Path),
				StartTS:    f.ModTime.Add(-time.Duration(r.Config.Global.SegmentDurationSec) * time.Second),
				EndTS:      f.ModTime,
				DurationMs: int64(r.Config.Global.SegmentDurationSec * 1000),
				Path:       f.Path,
				SizeBytes:  f.SizeBytes,
			}
			if err := r.Store.UpsertSegmentFromDisk(ctx, seg); err != nil {
				return err
			}
		case "corrupt":
			qPath := f.Path + ".quarantine"
			_ = os.Rename(f.Path, qPath)
			if dbEnabled {
				_ = r.Store.MarkCorrupt(ctx, f.Path, qPath)
			}
		}
	}
	if !dbEnabled {
		return nil
	}
	paths, err := r.Store.ExpectedPathsSince(ctx, time.Now().Add(-72*time.Hour))
	if err != nil {
		return err
	}
	for _, p := range paths {
		if _, ok := seen[p]; !ok {
			if _, err := os.Stat(p); os.IsNotExist(err) {
				_ = r.Store.MarkMissing(ctx, p)
			}
		}
	}
	return nil
}

func inferCameraIDFromPath(path string) string {
	// Expected structure: .../camera_uuid/yyyy-mm-dd/hh/segment.mp4
	dir := filepath.Dir(path)      // hh
	dir = filepath.Dir(dir)       // yyyy-mm-dd
	cameraDir := filepath.Base(filepath.Dir(dir)) // camera_uuid

	if cameraDir != "" && cameraDir != "." && cameraDir != string(filepath.Separator) && cameraDir != "\\" {
		return cameraDir
	}
	return "unknown"
}
