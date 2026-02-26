package retention

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type EngineStatus struct {
	LastRun          time.Time
	DeletedCount     int
	SkippedProtected int
	Errors           int
}

type RetentionEngine struct {
	cfg        Config
	resolver   *PolicyResolver
	protector  IProtector
	audit      IAuditWriter
	verifier   ISpaceVerifier
	enumerator ISegmentEnumerator

	mu     sync.Mutex
	status EngineStatus
}

func NewRetentionEngine(cfg Config, prot IProtector, enum ISegmentEnumerator, ver ISpaceVerifier, aud IAuditWriter) *RetentionEngine {
	return &RetentionEngine{
		cfg:        cfg,
		resolver:   NewPolicyResolver(cfg),
		protector:  prot,
		enumerator: enum,
		verifier:   ver,
		audit:      aud,
	}
}

func (e *RetentionEngine) StartDaemon(ctx context.Context) {
	interval := time.Duration(e.cfg.Defaults.CleanupIntervalMinutes) * time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.RunOnce(time.Now())
		}
	}
}

func (e *RetentionEngine) RunOnce(now time.Time) EngineStatus {
	e.mu.Lock()
	e.status.LastRun = now
	e.status.DeletedCount = 0
	e.status.SkippedProtected = 0
	e.status.Errors = 0
	e.mu.Unlock()

	// 4.4 tests use fake_vols
	vol := "."

	segments, err := e.enumerator.Enumerate(vol)
	if err != nil {
		e.mu.Lock()
		e.status.Errors++
		e.mu.Unlock()
		return e.GetStatus()
	}
	e.processVolume(vol, segments, now)

	return e.GetStatus()
}

func (e *RetentionEngine) processVolume(vol string, segments []SegmentMeta, now time.Time) {
	tenantUsage := make(map[string]int64)
	siteUsage := make(map[string]int64)
	camUsage := make(map[string]int64)

	var toDelete []SegmentMeta
	deletionReasons := make(map[string]string)

	for _, seg := range segments {
		if e.cfg.Protection.ProtectIfEventLinked && e.protector.IsProtected(seg.CameraID, filepath.Base(seg.Path)) {
			e.audit.Write(AuditRecord{
				EventType:   "retention.skip_protected",
				TenantID:    seg.TenantID,
				SiteID:      seg.SiteID,
				CameraID:    seg.CameraID,
				SegmentPath: seg.Path,
				Reason:      "event_linked",
			})
			e.mu.Lock()
			e.status.SkippedProtected++
			e.mu.Unlock()
			continue
		}

		if now.Sub(seg.StartTime).Minutes() < float64(e.cfg.Safety.NeverDeleteNewerThanMinutes) {
			continue // Skip active/recent files
		}

		daysToKeep, _, _ := e.resolver.Resolve(seg.TenantID, seg.SiteID, seg.CameraID)
		ageDays := now.Sub(seg.StartTime).Hours() / 24.0

		if daysToKeep > 0 && ageDays > float64(daysToKeep) {
			toDelete = append(toDelete, seg)
			deletionReasons[seg.Path] = "days_policy"
		} else {
			tenantUsage[seg.TenantID] += seg.SizeBytes
			siteUsage[seg.SiteID] += seg.SizeBytes
			camUsage[seg.CameraID] += seg.SizeBytes
		}
	}

	for _, seg := range segments {
		if deletionReasons[seg.Path] != "" || e.protector.IsProtected(seg.CameraID, filepath.Base(seg.Path)) {
			continue
		}

		_, maxGB, _ := e.resolver.Resolve(seg.TenantID, seg.SiteID, seg.CameraID)
		if maxGB <= 0 {
			continue
		}

		limitBytes := int64(maxGB) * 1024 * 1024 * 1024

		if tenantUsage[seg.TenantID] > limitBytes || siteUsage[seg.SiteID] > limitBytes || camUsage[seg.CameraID] > limitBytes {
			toDelete = append(toDelete, seg)
			deletionReasons[seg.Path] = "size_policy"

			tenantUsage[seg.TenantID] -= seg.SizeBytes
			siteUsage[seg.SiteID] -= seg.SizeBytes
			camUsage[seg.CameraID] -= seg.SizeBytes
		}
	}

	e.executeDeletions(vol, toDelete, deletionReasons)
}

func (e *RetentionEngine) executeDeletions(vol string, toDelete []SegmentMeta, reasons map[string]string) {
	if len(toDelete) == 0 {
		return
	}

	var expectedFreed int64 = 0
	var batchDeleted int = 0

	for _, seg := range toDelete {
		if e.cfg.Safety.SkipIfLocked {
			f, err := os.OpenFile(seg.Path, os.O_RDWR, 0)
			if err != nil {
				e.audit.Write(AuditRecord{
					EventType: "retention.skip_locked",
					TenantID:  seg.TenantID, SiteID: seg.SiteID, CameraID: seg.CameraID, SegmentPath: seg.Path, Reason: "file_in_use",
				})
				continue
			}
			f.Close()
		}

		if !e.cfg.Defaults.DryRun {
			os.Remove(seg.Path)
			if e.cfg.Safety.IncludeSidecars {
				os.Remove(seg.Path + ".sha256")
			}
		}
		expectedFreed += seg.SizeBytes
		batchDeleted++

		days, gb, scope := e.resolver.Resolve(seg.TenantID, seg.SiteID, seg.CameraID)
		e.audit.Write(AuditRecord{
			EventType:          "retention.delete",
			TenantID:           seg.TenantID,
			SiteID:             seg.SiteID,
			CameraID:           seg.CameraID,
			SegmentPath:        seg.Path,
			Reason:             reasons[seg.Path],
			BytesFreedExpected: seg.SizeBytes,
			PolicySnapshot: map[string]any{
				"days_to_keep":   days,
				"max_storage_gb": gb,
				"scope_applied":  scope,
			},
		})
	}

	e.mu.Lock()
	e.status.DeletedCount += batchDeleted
	e.mu.Unlock()
}

func (e *RetentionEngine) GetStatus() EngineStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status
}
