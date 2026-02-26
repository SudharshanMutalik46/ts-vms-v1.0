package health

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// cameraInternal holds the fast-path atomic counters and sliding window history
type cameraInternal struct {
	id    string
	state State
	mu    sync.RWMutex

	SinceTS                time.Time
	lastError              string
	restartCount           int
	lastSegmentFinalizedTS time.Time
	lastFramePTSTS         time.Time

	// Fast-path atomic counters
	atomicBytesWritten  uint64
	atomicValidFrames   uint64
	atomicDroppedFrames uint64

	// Sliding Windows
	byteHistory  []windowSample
	frameHistory []windowSample

	// Pre-calculated stats
	cachedStatus CameraStatus

	// Alert tracking
	sustainedDiskCrit int
	sustainedDropCrit int
	lastDiskAlert     time.Time
	lastDropAlert     time.Time
}

type windowSample struct {
	ts      time.Time
	bytes   uint64
	valid   uint64
	dropped uint64
}

type Manager struct {
	cfg     Config
	cameras map[string]*cameraInternal
	mu      sync.RWMutex
	stopCh  chan struct{}
}

func NewManager(cfg Config) *Manager {
	return &Manager{
		cfg:     cfg,
		cameras: make(map[string]*cameraInternal),
		stopCh:  make(chan struct{}),
	}
}

func (m *Manager) getOrCreate(camID string) *cameraInternal {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cameras[camID]
	if !ok {
		c = &cameraInternal{
			id:      camID,
			state:   StateStopped,
			SinceTS: time.Now(),
		}
		m.cameras[camID] = c
	}
	return c
}

// SetState updates the state machine
func (m *Manager) SetState(camID string, state State, err error) {
	c := m.getOrCreate(camID)
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state != state {
		c.state = state
		c.SinceTS = time.Now()
		if state == StateFailed {
			slog.Warn("recording.camera.failed", "camera_id", camID, "error", err)
		}
		if state == StateRecovering {
			c.restartCount++
		}
	}
	if err != nil {
		c.lastError = err.Error()
	} else {
		c.lastError = ""
	}
}

// AddTelemetry is the fast-path called by SegmentWriter / Pipeline
func (m *Manager) AddTelemetry(camID string, bytes uint64, validFrames uint64, droppedFrames uint64, pts time.Time) {
	c := m.getOrCreate(camID)
	atomic.AddUint64(&c.atomicBytesWritten, bytes)
	atomic.AddUint64(&c.atomicValidFrames, validFrames)
	atomic.AddUint64(&c.atomicDroppedFrames, droppedFrames)

	c.mu.Lock()
	c.lastFramePTSTS = pts
	c.mu.Unlock()
}

func (m *Manager) Start() {
	go m.evalLoop()
}

func (m *Manager) Stop() {
	close(m.stopCh)
}

func (m *Manager) evalLoop() {
	ticker := time.NewTicker(time.Duration(m.cfg.SampleIntervalSec) * time.Second)
	defer ticker.Stop()

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
	now := time.Now()
	diskLimit := now.Add(-time.Duration(m.cfg.DiskWriteRate.WindowSec) * time.Second)
	dropLimit := now.Add(-time.Duration(m.cfg.FrameDrop.WindowSec) * time.Second)

	m.mu.RLock()
	cameras := make([]*cameraInternal, 0, len(m.cameras))
	for _, c := range m.cameras {
		cameras = append(cameras, c)
	}
	m.mu.RUnlock()

	for _, c := range cameras {
		// 1. Snapshot atomics
		snapBytes := atomic.LoadUint64(&c.atomicBytesWritten)
		snapValid := atomic.LoadUint64(&c.atomicValidFrames)
		snapDrop := atomic.LoadUint64(&c.atomicDroppedFrames)

		c.mu.Lock()

		// 2. Append to history
		c.byteHistory = append(c.byteHistory, windowSample{ts: now, bytes: snapBytes})
		c.frameHistory = append(c.frameHistory, windowSample{ts: now, valid: snapValid, dropped: snapDrop})

		// 3. Prune old history
		c.byteHistory = pruneHistory(c.byteHistory, diskLimit)
		c.frameHistory = pruneHistory(c.frameHistory, dropLimit)

		// 4. Calculate Disk MB/s
		var mbps float64
		if len(c.byteHistory) > 1 {
			first := c.byteHistory[0]
			last := c.byteHistory[len(c.byteHistory)-1]
			bytesDiff := last.bytes - first.bytes
			timeDiff := last.ts.Sub(first.ts).Seconds()
			if timeDiff > 0 {
				mbps = (float64(bytesDiff) / 1048576.0) / timeDiff
			}
		}

		// 5. Calculate Frame Drop Pct
		var dropPct float64
		var dropsInWindow uint64
		if len(c.frameHistory) > 1 {
			first := c.frameHistory[0]
			last := c.frameHistory[len(c.frameHistory)-1]
			validDiff := last.valid - first.valid
			dropDiff := last.dropped - first.dropped
			dropsInWindow = dropDiff
			totalFrames := validDiff + dropDiff
			if totalFrames > 0 {
				dropPct = (float64(dropDiff) / float64(totalFrames)) * 100.0
			}
		}

		// 6. Update Cache
		c.cachedStatus = CameraStatus{
			CameraID:               c.id,
			State:                  c.state,
			SinceTS:                c.SinceTS,
			LastError:              c.lastError,
			RestartCount:           c.restartCount,
			LastSegmentFinalizedTS: c.lastSegmentFinalizedTS,
			LastWriteBytes:         snapBytes,
			WriteMBpsAvg:           mbps,
			FrameDropCountWindow:   dropsInWindow,
			FrameDropRatePctWindow: dropPct,
			LastFramePTSTS:         c.lastFramePTSTS,
		}

		if mbps > c.cachedStatus.PeakMBps {
			c.cachedStatus.PeakMBps = mbps
		}

		// 7. Evaluate Alerts (Only if ACTIVE)
		if c.state == StateActive {
			m.evaluateAlerts(c, mbps, dropPct, now)
		}

		c.mu.Unlock()
	}
}

func (m *Manager) evaluateAlerts(c *cameraInternal, mbps, dropPct float64, now time.Time) {
	// Disk Alerts
	if m.cfg.DiskWriteRate.Enabled {
		if mbps < m.cfg.DiskWriteRate.CritMinMBps {
			c.sustainedDiskCrit++
			if c.sustainedDiskCrit >= m.cfg.Alerts.SustainedWindowsForCritical && now.Sub(c.lastDiskAlert).Seconds() > float64(m.cfg.Alerts.CooldownSec) {
				slog.Error("recording.disk.low_write_rate.crit", "camera_id", c.id, "mbps", mbps)
				c.lastDiskAlert = now
			}
		} else if mbps < m.cfg.DiskWriteRate.WarnMinMBps {
			c.sustainedDiskCrit = 0
			if now.Sub(c.lastDiskAlert).Seconds() > float64(m.cfg.Alerts.CooldownSec) {
				slog.Warn("recording.disk.low_write_rate.warn", "camera_id", c.id, "mbps", mbps)
				c.lastDiskAlert = now
			}
		} else {
			c.sustainedDiskCrit = 0
		}
	}

	// Frame Drop Alerts
	if m.cfg.FrameDrop.Enabled {
		if dropPct > m.cfg.FrameDrop.CritDropRatePct {
			c.sustainedDropCrit++
			if c.sustainedDropCrit >= m.cfg.Alerts.SustainedWindowsForCritical && now.Sub(c.lastDropAlert).Seconds() > float64(m.cfg.Alerts.CooldownSec) {
				slog.Error("recording.frame_drop.crit", "camera_id", c.id, "drop_pct", dropPct)
				c.lastDropAlert = now
			}
		} else if dropPct > m.cfg.FrameDrop.WarnDropRatePct {
			c.sustainedDropCrit = 0
			if now.Sub(c.lastDropAlert).Seconds() > float64(m.cfg.Alerts.CooldownSec) {
				slog.Warn("recording.frame_drop.warn", "camera_id", c.id, "drop_pct", dropPct)
				c.lastDropAlert = now
			}
		} else {
			c.sustainedDropCrit = 0
		}
	}
}

func pruneHistory(h []windowSample, limit time.Time) []windowSample {
	keepIdx := 0
	for i, s := range h {
		if s.ts.After(limit) {
			keepIdx = i
			break
		}
	}
	if keepIdx > 0 {
		return h[keepIdx:]
	}
	return h
}

func (m *Manager) GetGlobalStatus() GlobalStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res := GlobalStatus{
		Cameras: make(map[string]*CameraStatus),
	}

	for id, c := range m.cameras {
		c.mu.RLock()
		status := c.cachedStatus
		c.mu.RUnlock()

		res.Cameras[id] = &status
		res.TotalWriteMBps += status.WriteMBpsAvg

		if status.State == StateActive {
			res.ActiveCameras++
		} else if status.State == StateFailed {
			res.FailedCameras++
		}
	}
	return res
}
