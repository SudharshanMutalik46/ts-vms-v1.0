package recording

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/technosupport/ts-vms/internal/platform/paths"
	rec "github.com/technosupport/ts-vms/internal/recording/recovery"
)

type Reconciler struct {
	Config  *Config
	Store   ArchiveIndex
	Scanner *rec.Scanner
}

func NewReconciler(cfg *Config, store ArchiveIndex) *Reconciler {
	rcfg := rec.Config{
		Enabled:             cfg.FailoverRecovery.Enabled,
		RestartBackoffSec:   cfg.FailoverRecovery.RestartBackoffSec,
		DBRequiredForReady:  cfg.FailoverRecovery.DBRequiredForReady,
		OrphanReconcileMode: cfg.FailoverRecovery.OrphanReconcileMode,
	}
	return &Reconciler{Config: cfg, Store: store, Scanner: rec.NewScanner(rcfg)}
}

func (r *Reconciler) Run(ctx context.Context) error {
	return r.runVolumes(ctx, []string{r.Config.Global.StorageRoot}, true, 0)
}

// SweepOrphanTmpFiles finalizes stale .tmp files left behind by disconnected
// cameras or abnormal recorder exits. The age threshold is intentionally high
// so active live segments are not touched.
func (r *Reconciler) SweepOrphanTmpFiles(ctx context.Context, minAge time.Duration) error {
	return r.sweepTmpVolumes(ctx, []string{r.Config.Global.StorageRoot}, minAge)
}

// ReconcileCamera rescans only the storage folder for a single camera and
// upserts any finalized video files that exist on disk but are missing from DB.
func (r *Reconciler) ReconcileCamera(ctx context.Context, cameraID string) error {
	cameraID = strings.TrimSpace(cameraID)
	if cameraID == "" {
		return nil
	}

	cameraRoot, err := paths.SafeJoin(
		r.Config.Global.StorageRoot,
		r.Config.Global.DefaultTenantID,
		r.Config.Global.DefaultSiteID,
		cameraID,
	)
	if err != nil {
		return err
	}

	if _, err := os.Stat(cameraRoot); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	return r.runVolumes(ctx, []string{cameraRoot}, false, 10*time.Second)
}

// LoadCameraSegments scans a single camera folder and returns finalized video
// segments for playback, even if the archive index is temporarily stale.
func (r *Reconciler) LoadCameraSegments(ctx context.Context, cameraID string, from, to time.Time) ([]ArchiveSegment, error) {
	cameraID = strings.TrimSpace(cameraID)
	if cameraID == "" {
		return nil, nil
	}

	cameraRoot, err := paths.SafeJoin(
		r.Config.Global.StorageRoot,
		r.Config.Global.DefaultTenantID,
		r.Config.Global.DefaultSiteID,
		cameraID,
	)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(cameraRoot); err != nil {
		if os.IsNotExist(err) {
			return r.loadCameraSegmentsFallback(ctx, from, to)
		}
		return nil, err
	}

	segments, err := r.collectSegments([]string{cameraRoot}, from, to)
	if err != nil {
		return nil, err
	}
	if len(segments) > 0 {
		return segments, nil
	}

	return r.loadCameraSegmentsFallback(ctx, from, to)
}

func (r *Reconciler) loadCameraSegmentsFallback(ctx context.Context, from, to time.Time) ([]ArchiveSegment, error) {
	siteRoot, err := paths.SafeJoin(
		r.Config.Global.StorageRoot,
		r.Config.Global.DefaultTenantID,
		r.Config.Global.DefaultSiteID,
	)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(siteRoot); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	segments, err := r.collectSegments([]string{siteRoot}, from, to)
	if err != nil {
		return nil, err
	}
	return chooseBestCameraSegments(segments), nil
}

func (r *Reconciler) collectSegments(roots []string, from, to time.Time) ([]ArchiveSegment, error) {
	findings, _, err := r.Scanner.Scan(roots)
	if err != nil {
		return nil, err
	}

	segments := make([]ArchiveSegment, 0, len(findings))
	for _, f := range findings {
		if f.Kind != "video" {
			continue
		}

		container := "mkv"
		if strings.HasSuffix(strings.ToLower(f.Path), ".mp4") {
			container = "mp4"
		}

		end := f.ModTime
		start := end.Add(-time.Duration(r.Config.Global.SegmentDurationSec) * time.Second)
		seg := ArchiveSegment{
			TenantID:   r.Config.Global.DefaultTenantID,
			SiteID:     r.Config.Global.DefaultSiteID,
			CameraID:   inferCameraIDFromPath(f.Path),
			StartTS:    start,
			EndTS:      end,
			DurationMs: int64(end.Sub(start) / time.Millisecond),
			Path:       f.Path,
			FilePath:   f.Path,
			SizeBytes:  f.SizeBytes,
			FileSize:   f.SizeBytes,
			Container:  container,
			Finalized:  true,
		}

		if !to.IsZero() && !seg.StartTS.Before(to) {
			continue
		}
		if !from.IsZero() && !seg.EndTS.After(from) {
			continue
		}
		segments = append(segments, seg)
	}

	sort.Slice(segments, func(i, j int) bool {
		if segments[i].CameraID == segments[j].CameraID {
			return segments[i].StartTS.Before(segments[j].StartTS)
		}
		return segments[i].CameraID < segments[j].CameraID
	})

	return stitchSegmentContinuity(segments, time.Second), nil
}

func stitchSegmentContinuity(segments []ArchiveSegment, maxGap time.Duration) []ArchiveSegment {
	if len(segments) < 2 {
		return segments
	}

	out := make([]ArchiveSegment, 0, len(segments))
	var prev ArchiveSegment
	havePrev := false

	for _, seg := range segments {
		if !havePrev || seg.CameraID != prev.CameraID {
			out = append(out, seg)
			prev = seg
			havePrev = true
			continue
		}

		if !seg.StartTS.After(prev.EndTS) {
			out = append(out, seg)
			prev = seg
			continue
		}

		if seg.StartTS.Sub(prev.EndTS) <= maxGap {
			seg.StartTS = prev.EndTS
			if seg.EndTS.Before(seg.StartTS) {
				seg.EndTS = seg.StartTS
			}
			if seg.DurationMs <= 0 {
				seg.DurationMs = int64(seg.EndTS.Sub(seg.StartTS) / time.Millisecond)
			}
		}

		out = append(out, seg)
		prev = seg
	}

	return out
}

func chooseBestCameraSegments(segments []ArchiveSegment) []ArchiveSegment {
	if len(segments) == 0 {
		return nil
	}

	type cameraStats struct {
		count    int
		duration time.Duration
	}

	stats := make(map[string]cameraStats)
	for _, seg := range segments {
		st := stats[seg.CameraID]
		st.count++
		st.duration += seg.EndTS.Sub(seg.StartTS)
		stats[seg.CameraID] = st
	}

	bestCameraID := ""
	best := cameraStats{}
	for cameraID, st := range stats {
		if bestCameraID == "" || st.count > best.count || (st.count == best.count && st.duration > best.duration) {
			bestCameraID = cameraID
			best = st
		}
	}

	filtered := make([]ArchiveSegment, 0, best.count)
	for _, seg := range segments {
		if seg.CameraID == bestCameraID {
			filtered = append(filtered, seg)
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].StartTS.Before(filtered[j].StartTS)
	})

	return filtered
}

func (r *Reconciler) runVolumes(ctx context.Context, volumes []string, markMissing bool, settleDelay time.Duration) error {
	findings, _, err := r.Scanner.Scan(volumes)
	if err != nil {
		if len(volumes) == 1 && os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if settleDelay < 0 {
		settleDelay = 0
	}

	dbEnabled := r.Store != nil && r.Store.Available()
	seen := make(map[string]struct{}, len(findings))
	for _, f := range findings {
		seen[f.Path] = struct{}{}
		switch f.Kind {
		case "tmp":
			if time.Since(f.ModTime) < settleDelay {
				continue
			}
			if err := finalizeRecoveredTmp(ctx, r.Store, f.Path, f.ModTime, r.Config); err != nil {
				log.Printf("[recording.recovery] tmp_finalize_failed path=%s err=%v", f.Path, err)
			}
		case "video":
			if !dbEnabled {
				continue
			}
			container := "mkv"
			if strings.HasSuffix(strings.ToLower(f.Path), ".mp4") {
				container = "mp4"
			}
			checksum, _ := ComputeSHA256(f.Path)
			seg := &ArchiveSegment{
				TenantID:       r.Config.Global.DefaultTenantID,
				SiteID:         r.Config.Global.DefaultSiteID,
				CameraID:       inferCameraIDFromPath(f.Path),
				StartTS:        f.ModTime.Add(-time.Duration(r.Config.Global.SegmentDurationSec) * time.Second),
				EndTS:          f.ModTime,
				DurationMs:     int64(r.Config.Global.SegmentDurationSec * 1000),
				Path:           f.Path,
				FilePath:       f.Path,
				SizeBytes:      f.SizeBytes,
				FileSize:       f.SizeBytes,
				Container:      container,
				ChecksumSHA256: checksum,
				Finalized:      true,
			}
			if err := r.Store.UpsertFinalizedSegment(ctx, seg); err != nil {
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
	if !dbEnabled || !markMissing {
		return nil
	}

	expectedPaths, err := r.Store.ExpectedPathsSince(ctx, time.Now().Add(-72*time.Hour))
	if err != nil {
		return err
	}
	for _, p := range expectedPaths {
		if _, ok := seen[p]; ok {
			continue
		}
		if _, err := os.Stat(p); os.IsNotExist(err) {
			_ = r.Store.MarkMissing(ctx, p)
		}
	}
	return nil
}

func (r *Reconciler) sweepTmpVolumes(ctx context.Context, volumes []string, minAge time.Duration) error {
	findings, _, err := r.Scanner.Scan(volumes)
	if err != nil {
		if len(volumes) == 1 && os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if minAge < 0 {
		minAge = 0
	}

	for _, f := range findings {
		if f.Kind != "tmp" {
			continue
		}
		if time.Since(f.ModTime) < minAge {
			continue
		}
		if err := finalizeRecoveredTmp(ctx, r.Store, f.Path, f.ModTime, r.Config); err != nil {
			log.Printf("[recording.recovery] orphan_tmp_finalize_failed path=%s err=%v", f.Path, err)
		}
	}
	return nil
}

func finalizeRecoveredTmp(ctx context.Context, store ArchiveIndex, tmpPath string, modTime time.Time, cfg *Config) error {
	if tmpPath == "" {
		return nil
	}

	var (
		finalPath string
		checksum  string
		err       error
	)
	for i, delay := range []time.Duration{0, 1 * time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second} {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
		finalPath, checksum, err = FinalizeSegment(tmpPath)
		if err == nil {
			break
		}
		if os.IsNotExist(err) {
			return nil
		}
	}
	if err != nil {
		return err
	}

	info, statErr := os.Stat(finalPath)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return nil
		}
		return statErr
	}

	end := modTime
	if end.IsZero() {
		end = info.ModTime()
	}
	start := end.Add(-time.Duration(cfg.Global.SegmentDurationSec) * time.Second)
	seg := &ArchiveSegment{
		TenantID:       cfg.Global.DefaultTenantID,
		SiteID:         cfg.Global.DefaultSiteID,
		CameraID:       inferCameraIDFromPath(finalPath),
		StartTS:        start,
		EndTS:          end,
		DurationMs:     int64(end.Sub(start) / time.Millisecond),
		Path:           finalPath,
		FilePath:       finalPath,
		SizeBytes:      info.Size(),
		FileSize:       info.Size(),
		Container:      "mkv",
		ChecksumSHA256: checksum,
		Finalized:      true,
	}
	if store != nil && store.Available() {
		return store.UpsertFinalizedSegment(ctx, seg)
	}
	return nil
}

func inferCameraIDFromPath(path string) string {
	// Root storage for this camera: root/tenant/site/camera_uuid/run_dir/segment.[mp4|mkv]
	dir := filepath.Dir(path)                     // run_dir
	cameraDir := filepath.Base(filepath.Dir(dir)) // camera_uuid

	if cameraDir != "" && cameraDir != "." && cameraDir != string(filepath.Separator) && cameraDir != "\\" {
		return cameraDir
	}
	return "unknown"
}
