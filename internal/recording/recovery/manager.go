package recovery

import (
	"log/slog"
	"time"
)

type Config struct {
	Enabled             bool
	RestartBackoffSec   int
	DBRequiredForReady  bool
	OrphanReconcileMode string
}

// MockIndex represents the Phase 4.5 database queries
type MockIndex struct {
	Data map[string]SegmentMeta
}

type SegmentMeta struct {
	Path      string
	EndTS     time.Time
	IsCorrupt bool
}

func (m *MockIndex) GetLastGoodSegment(cameraID string) (SegmentMeta, bool) {
	s, ok := m.Data[cameraID]
	return s, ok
}

// Manager orchestrates startup state recovery
type Manager struct {
	cfg   Config
	index *MockIndex
}

func NewManager(cfg Config, index *MockIndex) *Manager {
	return &Manager{
		cfg:   cfg,
		index: index,
	}
}

// ResumePlan describes how a camera should be restarted
type ResumePlan struct {
	CameraID   string
	BaselineTS time.Time
	LastPath   string
	StartFresh bool
}

func (m *Manager) EvaluateResume(cameraID string) ResumePlan {
	if !m.cfg.Enabled {
		return ResumePlan{CameraID: cameraID, StartFresh: true, BaselineTS: time.Now()}
	}

	meta, found := m.index.GetLastGoodSegment(cameraID)
	if !found {
		slog.Info("recovery.no_history", "camera_id", cameraID)
		return ResumePlan{CameraID: cameraID, StartFresh: true, BaselineTS: time.Now()}
	}

	if meta.IsCorrupt {
		slog.Warn("recovery.last_segment_corrupt", "camera_id", cameraID, "path", meta.Path)
		// Fallback to time.Now() if corrupt, or we could seek further back in a real DB
		return ResumePlan{CameraID: cameraID, StartFresh: true, BaselineTS: time.Now()}
	}

	baseline := meta.EndTS
	now := time.Now()

	// If the DB says the last segment ended in the future (clock drift), we trust time.Now() to be safe
	if baseline.After(now) {
		baseline = now
	}

	slog.Info("recovery.plan_created", "camera_id", cameraID, "baseline_ts", baseline, "last_path", meta.Path)

	return ResumePlan{
		CameraID:   cameraID,
		BaselineTS: baseline,
		LastPath:   meta.Path,
		StartFresh: false,
	}
}
