package recording

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/technosupport/ts-vms/internal/crypto"
)

type WorkerState string

const (
	StateStopped            WorkerState = "STOPPED"
	StateStarting           WorkerState = "STARTING"
	StateRecording          WorkerState = "RECORDING"
	StatePaused             WorkerState = "PAUSED"
	StateError              WorkerState = "ERROR"
	StateThrottledByLicense WorkerState = "THROTTLED_BY_LICENSE"
)

type RecorderWorker struct {
	CameraID         string
	Camera           CameraConfig
	Config           *Config
	Store            ArchiveIndex
	State            WorkerState
	cancel           context.CancelFunc
	cmd              *exec.Cmd
	loopRunning      bool
	paused           bool
	stopping         bool
	running          bool
	runID            uint64
	licenseHeld      bool
	retries          int
	lastErr          string
	lastBeat         time.Time
	lastDataTime     time.Time
	knownFiles       map[string]struct{}
	currentDir       string // Path to the current run's storage directory
	lastSegmentEndTS time.Time
	Keyring          *crypto.Keyring
	mu               sync.RWMutex
}

func NewRecorderWorker(cfg *Config, cam CameraConfig, store ArchiveIndex, keyring *crypto.Keyring) *RecorderWorker {
	return &RecorderWorker{
		CameraID:     cam.ID,
		Camera:       cam,
		Config:       cfg,
		Store:        store,
		Keyring:      keyring,
		State:        StateStopped,
		knownFiles:   make(map[string]struct{}),
		lastDataTime: time.Now(),
	}
}

func (w *RecorderWorker) IsRunning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.running
}

func (w *RecorderWorker) Start(ctx context.Context) {
	w.mu.Lock()
	if w.loopRunning {
		w.mu.Unlock()
		return
	}
	workerCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.loopRunning = true
	w.paused = false
	w.stopping = false
	w.running = true
	w.State = StateStarting
	w.lastErr = ""
	w.lastDataTime = time.Now()
	w.runID++
	runID := w.runID
	w.mu.Unlock()

	go w.loop(workerCtx, runID)
}

func (w *RecorderWorker) loop(ctx context.Context, runID uint64) {
	defer func() {
		w.finishRun(runID)
		w.mu.Lock()
		w.loopRunning = false
		if w.cancel != nil {
			w.cancel = nil
		}
		w.mu.Unlock()
	}()

	for {
		if err := w.startPipeline(ctx, runID); err != nil {
			w.cleanupEmptyRunDir()
			if ctx.Err() != nil || w.isIntentionalStop(runID) {
				w.markStopped(runID)
				return
			}
			w.markError(runID, err)
			if !w.waitBackoff(ctx, runID) {
				return
			}
			continue
		}
		w.markRecording(runID)

		errCh := make(chan error, 1)
		cmd := w.currentCmd()
		go func(cmd *exec.Cmd) {
			if cmd == nil {
				errCh <- fmt.Errorf("recording process missing")
				return
			}
			errCh <- cmd.Wait()
		}(cmd)

		ticker := time.NewTicker(time.Duration(w.Config.Global.SegmentDurationSec) * time.Second)

		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				w.stopProcess()
				w.cleanupEmptyRunDir()
				w.markStopped(runID)
				return
			case <-ticker.C:
				w.touchHeartbeat(runID)

				// Use a shorter timeout for segment syncing to prevent hanging the whole worker
				syncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				_ = w.syncSegments(syncCtx)
				cancel()

				if w.checkWatchdog(runID) {
					log.Printf("[WARNING] watchdog triggered for %s: no data for too long. killing process.", w.CameraID)
					w.stopProcess()
				}
			case err := <-errCh:
				ticker.Stop()
				_ = w.syncSegments(ctx)
				w.cleanupEmptyRunDir()
				if ctx.Err() != nil || w.isIntentionalStop(runID) {
					w.markStopped(runID)
					return
				}
				w.clearProcessHandle(runID)

				w.mu.Lock()
				w.cmd = nil
				if err == nil {
					err = fmt.Errorf("recording pipeline exited unexpectedly")
				}
				w.mu.Unlock()
				w.markError(runID, err)

				if !w.waitBackoff(ctx, runID) {
					return
				}
				goto RESTART
			}
		}
	RESTART:
	}
}

func gstPath(v string) string {
	return filepath.ToSlash(filepath.Clean(v))
}

func (w *RecorderWorker) startPipeline(ctx context.Context, runID uint64) error {
	// Root storage for this camera
	baseDir := filepath.Join(w.Config.Global.StorageRoot, w.Config.Global.DefaultTenantID, w.Config.Global.DefaultSiteID, w.CameraID)
	// Unique subdirectory for this run to avoid filename collisions and data loss
	runDir := filepath.Join(baseDir, time.Now().Format("20060102_150405"))

	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}

	w.mu.Lock()
	w.currentDir = runDir
	w.knownFiles = make(map[string]struct{})
	w.lastSegmentEndTS = time.Time{} // Reset on new run
	w.mu.Unlock()

	// Fetch Credentials if available
	if w.Store != nil && w.Keyring != nil {
		cred, err := w.Store.GetCredentials(ctx, w.CameraID)
		if err == nil && cred != nil {
			user, pass, err := w.Store.DecryptCredentials(cred, w.Keyring)
			if err == nil {
				w.Camera.Username = user
				w.Camera.Password = pass
			} else {
				log.Printf("[WARNING] failed to decrypt credentials for %s: %v", w.CameraID, err)
			}
		}
	}

	pattern := filepath.Join(runDir, "segment_%05d"+w.segmentExt()+".tmp")
	args := w.gstRecorderArgs(gstPath(pattern))
	cmd := exec.CommandContext(ctx, w.Config.Global.GstLaunchPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("gstreamer start failed for %s: %w", w.CameraID, err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.runID != runID || w.stopping {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return context.Canceled
	}
	w.cmd = cmd
	log.Printf("[EVENT] recording.worker.started camera_id=%s backend=gstreamer pid=%d codec=%s format=%s", w.CameraID, cmd.Process.Pid, w.cameraCodec(), w.cameraSegmentFormat())
	return nil
}

func (w *RecorderWorker) syncSegments(ctx context.Context) error {
	w.mu.RLock()
	dir := w.currentDir
	w.mu.RUnlock()

	if dir == "" {
		return nil
	}

	matches, err := filepath.Glob(filepath.Join(dir, "*"+w.segmentExt()+".tmp"))
	if err != nil {
		return err
	}
	// Important: process in order
	sort.Strings(matches)

	for _, p := range matches {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() || info.Size() == 0 {
			continue
		}
		// Wait a bit to ensure file is closed by GStreamer
		if time.Since(info.ModTime()) < 5*time.Second {
			continue
		}

		// Use the strict finalize pipeline: Flush -> Rename -> Checksum
		finalPath, checksum, err := FinalizeSegment(p)
		if err != nil {
			log.Printf("[RecorderWorker] finalization failed for %s: %v", p, err)
			continue
		}

		info, err = os.Stat(finalPath)
		if err != nil {
			continue
		}
		end := info.ModTime()
		start := end.Add(-time.Duration(w.Config.Global.SegmentDurationSec) * time.Second)

		w.mu.Lock()
		if !w.lastSegmentEndTS.IsZero() {
			// If drift is small (< 10s), snap to previous end to ensure perfect continuity
			if diff := start.Sub(w.lastSegmentEndTS); diff > -10*time.Second && diff < 10*time.Second {
				start = w.lastSegmentEndTS
				end = start.Add(time.Duration(w.Config.Global.SegmentDurationSec) * time.Second)
			}
		}
		w.lastSegmentEndTS = end
		w.mu.Unlock()

		seg := &ArchiveSegment{
			TenantID:   w.Config.Global.DefaultTenantID,
			SiteID:     w.Config.Global.DefaultSiteID,
			CameraID:   w.CameraID,
			StartTS:    start,
			EndTS:      end,
			DurationMs: end.Sub(start).Milliseconds(),
			Path:           finalPath,
			FilePath:       finalPath,
			SizeBytes:      info.Size(),
			FileSize:       info.Size(),
			Container:      "mkv",
			ChecksumSHA256: checksum,
			Finalized:      true,
		}
		if w.Store != nil {
			if err := w.Store.UpsertFinalizedSegment(ctx, seg); err != nil {
				return err
			}
		}
		w.knownFiles[finalPath] = struct{}{}
		w.mu.Lock()
		w.lastDataTime = time.Now()
		w.mu.Unlock()
	}
	return nil
}

func (w *RecorderWorker) checkWatchdog(runID uint64) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.runID != runID || !w.running || w.paused || w.stopping {
		return false
	}
	// If no data for more than 2x segment duration + 30s buffer
	threshold := time.Duration(w.Config.Global.SegmentDurationSec*2+30) * time.Second
	return time.Since(w.lastDataTime) > threshold
}

func (w *RecorderWorker) Pause() {
	w.mu.Lock()
	if !w.running {
		w.State = StatePaused
		w.paused = true
		w.mu.Unlock()
		return
	}
	w.paused = true
	w.State = StatePaused
	w.stopping = true
	cancel := w.cancel
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	w.stopProcess()
}

func (w *RecorderWorker) Resume(ctx context.Context) {
	w.mu.Lock()
	w.paused = false
	running := w.running
	stopping := w.stopping
	w.mu.Unlock()

	if running && !stopping {
		return
	}
	go func() {
		for i := 0; i < 20; i++ {
			if !w.IsRunning() {
				w.Start(ctx)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		w.Start(ctx)
	}()
}

func (w *RecorderWorker) Stop() {
	w.mu.Lock()
	cancel := w.cancel
	running := w.loopRunning
	w.paused = false
	w.stopping = true
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	w.stopProcess()
	if !running {
		w.markStopped(w.runID)
	}
}

func (w *RecorderWorker) stopProcess() {
	w.mu.RLock()
	cmd := w.cmd
	w.mu.RUnlock()
	if cmd != nil && cmd.Process != nil {
		log.Printf("[DEBUG] killing process %d for %s", cmd.Process.Pid, w.CameraID)
		// On Windows, Kill() is sometimes unreliable for GStreamer.
		// Force kill with taskkill to ensure the process is gone.
		_ = exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", cmd.Process.Pid)).Run()
		_ = cmd.Process.Kill()
	}
}

func (w *RecorderWorker) cleanupEmptyRunDir() {
	w.mu.RLock()
	dir := w.currentDir
	w.mu.RUnlock()
	if dir != "" {
		_ = os.Remove(dir)
	}
}

func (w *RecorderWorker) HasLicense() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.licenseHeld
}

func (w *RecorderWorker) SetLicenseHeld(v bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.licenseHeld = v
}

func (w *RecorderWorker) SetThrottled() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.running {
		w.State = StateThrottledByLicense
		w.lastErr = "recording license quota exceeded"
	}
}

func (w *RecorderWorker) markRecording(runID uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.runID != runID {
		return
	}
	w.State = StateRecording
	w.retries = 0
	w.lastErr = ""
	w.lastBeat = time.Now()
}

func (w *RecorderWorker) markError(runID uint64, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.runID != runID {
		return
	}
	w.State = StateError
	w.retries++
	if err != nil {
		w.lastErr = err.Error()
	}
}

func (w *RecorderWorker) setStopped() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.running = false
	w.loopRunning = false
	w.State = StateStopped
}

func (w *RecorderWorker) markStopped(runID uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.runID != runID {
		return
	}
	if w.paused {
		w.State = StatePaused
	} else {
		w.State = StateStopped
	}
	w.cmd = nil
}

func (w *RecorderWorker) finishRun(runID uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.runID != runID {
		return
	}
	w.cmd = nil
	w.cancel = nil
	w.running = false
	w.stopping = false
}

func (w *RecorderWorker) clearProcessHandle(runID uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.runID != runID {
		return
	}
	w.cmd = nil
}

func (w *RecorderWorker) touchHeartbeat(runID uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.runID != runID {
		return
	}
	w.lastBeat = time.Now()
}

func (w *RecorderWorker) isIntentionalStop(runID uint64) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.runID == runID && (w.stopping || w.paused)
}

func (w *RecorderWorker) waitBackoff(ctx context.Context, runID uint64) bool {
	w.mu.RLock()
	retries := w.retries
	validRun := w.runID == runID
	backoffBase := w.Config.FailoverRecovery.RestartBackoffSec
	w.mu.RUnlock()
	if !validRun {
		return false
	}
	if backoffBase <= 0 {
		backoffBase = 5
	}
	backoff := time.Duration(max(2, backoffBase*max(1, retries))) * time.Second
	select {
	case <-ctx.Done():
		return false
	case <-time.After(backoff):
		return true
	}
}

func (w *RecorderWorker) currentCmd() *exec.Cmd {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.cmd
}

func (w *RecorderWorker) cameraCodec() string {
	c := strings.ToLower(strings.TrimSpace(w.Camera.Codec))
	if c == "" {
		return "h265"
	}
	return c
}

func (w *RecorderWorker) cameraSegmentFormat() string {
	f := strings.ToLower(strings.TrimSpace(w.Camera.SegmentFormat))
	if f == "" {
		return "mkv"
	}
	return f
}

func (w *RecorderWorker) segmentExt() string {
	switch w.cameraSegmentFormat() {
	case "mp4":
		return ".mp4"
	default:
		return ".mkv"
	}
}

func (w *RecorderWorker) rtspTransport() string {
	t := strings.ToLower(strings.TrimSpace(w.Camera.RTSPTransport))
	if t == "udp" {
		return "udp"
	}
	return "tcp"
}

func (w *RecorderWorker) gstRecorderArgs(pattern string) []string {
	latency := 200
	if w.Config != nil && w.Config.Performance.Pipeline.RTSPSrcLatencyMs > 0 {
		latency = w.Config.Performance.Pipeline.RTSPSrcLatencyMs
	}
	segmentNs := int64(w.Config.Global.SegmentDurationSec) * int64(time.Second)
	if segmentNs <= 0 {
		segmentNs = int64(60 * time.Second)
	}

	base := []string{
		"-e",
		"rtspsrc",
		"location=" + w.Camera.RtspURL,
	}

	if w.Camera.Username != "" {
		base = append(base, "user-id="+w.Camera.Username)
	}
	if w.Camera.Password != "" {
		base = append(base, "user-pw="+w.Camera.Password)
	}

	base = append(base,
		"protocols="+w.rtspTransport(),
		"latency="+fmt.Sprintf("%d", latency),
		"timeout=10000000",     // 10s timeout for generic RTSP operations
		"tcp-timeout=10000000", // 10s timeout for TCP connections
		"!",
	)

	switch w.cameraCodec() {
	case "h264":
		base = append(base,
			"rtph264depay",
			"!", "h264parse", "config-interval=-1",
		)
	default:
		base = append(base,
			"rtph265depay",
			"!", "h265parse", "config-interval=-1",
		)
	}

	switch w.cameraSegmentFormat() {
	case "mp4":
		base = append(base,
			"!", "splitmuxsink",
			"location="+gstPath(pattern),
			"max-size-time="+fmt.Sprintf("%d", segmentNs),
			"async-finalize=true",
			"muxer-factory=mp4mux",
			"muxer-properties=properties,streamable=true",
		)
	default:
		base = append(base,
			"!", "splitmuxsink",
			"location="+gstPath(pattern),
			"max-size-time="+fmt.Sprintf("%d", segmentNs),
			"async-finalize=true",
			"muxer-factory=matroskamux",
			"muxer-properties=properties,streamable=true",
		)
	}

	return base
}

func (w *RecorderWorker) Status() map[string]any {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return map[string]any{
		"camera_id":      w.CameraID,
		"state":          w.State,
		"last_error":     w.lastErr,
		"last_heartbeat": w.lastBeat,
		"retries":        w.retries,
		"running":        w.running,
		"paused":         w.paused,
		"license_held":   w.licenseHeld,
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
