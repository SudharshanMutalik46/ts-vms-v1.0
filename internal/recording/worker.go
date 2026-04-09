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
	CameraID          string
	Camera            CameraConfig
	Config            *Config
	Store             ArchiveIndex
	State             WorkerState
	cancel            context.CancelFunc
	cmd               *exec.Cmd
	loopRunning       bool
	paused            bool
	stopping          bool
	running           bool
	runID             uint64
	licenseHeld       bool
	retries           int
	lastErr           string
	lastBeat          time.Time
	lastDataTime      time.Time
	knownFiles        map[string]struct{}
	currentDir        string // Path to the current run's storage directory
	lastSegmentEndTS  time.Time
	lastFinalizedScan time.Time
	recordingSourceIx int
	currentSourceIdx  int
	sourceBaseRTSP    string
	codecRetryUsed    bool
	startupAt         time.Time
	Keyring           *crypto.Keyring
	mu                sync.RWMutex
}

const finalizedBackfillInterval = 1 * time.Minute
const startupCodecRetryWindow = 20 * time.Second

func NewRecorderWorker(cfg *Config, cam CameraConfig, store ArchiveIndex, keyring *crypto.Keyring) *RecorderWorker {
	return &RecorderWorker{
		CameraID:       cam.ID,
		Camera:         cam,
		Config:         cfg,
		Store:          store,
		Keyring:        keyring,
		State:          StateStopped,
		knownFiles:     make(map[string]struct{}),
		lastDataTime:   time.Now(),
		sourceBaseRTSP: cam.RtspURL,
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
	w.codecRetryUsed = false
	w.currentSourceIdx = -1
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
		var ticker *time.Ticker
		var errCh chan error
		var cmd *exec.Cmd
		var syncInterval time.Duration
		if err := w.startPipeline(ctx, runID); err != nil {
			w.cleanupEmptyRunDir()
			if ctx.Err() != nil || w.isIntentionalStop(runID) {
				w.markStopped(runID)
				return
			}
			if w.retryStartupWithAlternateCodec(ctx, runID) {
				goto RESTART
			}
			w.markError(runID, err)
			w.advanceRecordingSource()
			if !w.waitBackoff(ctx, runID) {
				return
			}
			continue
		}
		w.markRecording(runID)

		errCh = make(chan error, 1)
		cmd = w.currentCmd()
		go func(cmd *exec.Cmd) {
			if cmd == nil {
				errCh <- fmt.Errorf("recording process missing")
				return
			}
			errCh <- cmd.Wait()
		}(cmd)

		syncInterval = time.Duration(w.Config.Global.SegmentDurationSec) * time.Second
		if syncInterval > 15*time.Second {
			syncInterval = 15 * time.Second
		}
		ticker = time.NewTicker(syncInterval)

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
				syncErr := w.syncSegments(syncCtx, false)
				cancel()

				if syncErr != nil {
					log.Printf("[WARNING] syncSegments failed for %s: %v", w.CameraID, syncErr)
					w.stopProcess() // force handle release, let errCh path restart cleanly
				}

				if w.checkWatchdog(runID) {
					log.Printf("[WARNING] watchdog triggered for %s: no data for too long. killing process.", w.CameraID)
					w.stopProcess()
				}
			case err := <-errCh:
				ticker.Stop()
				time.Sleep(2 * time.Second) // let GStreamer/taskkill release handles on Windows
				_ = w.syncSegments(ctx, true)
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
				startupAt := w.startupAt
				allowCodecRetry := !w.codecRetryUsed && time.Since(startupAt) <= startupCodecRetryWindow
				w.mu.Unlock()

				if allowCodecRetry && w.retryStartupWithAlternateCodec(ctx, runID) {
					goto RESTART
				}

				w.markError(runID, err)
				w.advanceRecordingSource()

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

func ffprobePath(ffmpegPath string) string {
	if ffmpegPath == "" {
		return ""
	}
	if strings.HasSuffix(strings.ToLower(ffmpegPath), "ffmpeg.exe") {
		return strings.TrimSuffix(ffmpegPath, "ffmpeg.exe") + "ffprobe.exe"
	}
	return ""
}

func gstDiscovererPath(gstLaunchPath string) string {
	if gstLaunchPath == "" {
		return ""
	}
	if strings.HasSuffix(strings.ToLower(gstLaunchPath), "gst-launch-1.0.exe") {
		return strings.TrimSuffix(gstLaunchPath, "gst-launch-1.0.exe") + "gst-discoverer-1.0.exe"
	}
	return ""
}

func injectRTSPCreds(rtspURL, user, pass string) string {
	if user == "" || rtspURL == "" {
		return rtspURL
	}
	lower := strings.ToLower(rtspURL)
	if !strings.HasPrefix(lower, "rtsp://") {
		return rtspURL
	}
	if strings.Contains(rtspURL, "@") {
		return rtspURL
	}
	auth := user
	if pass != "" {
		auth = auth + ":" + pass
	}
	return "rtsp://" + auth + "@" + rtspURL[len("rtsp://"):]
}

func (w *RecorderWorker) logDetectedCodec(ctx context.Context, source RecordingSource) {
	if w.Config == nil {
		return
	}
	url := injectRTSPCreds(source.RTSPURL, w.Camera.Username, w.Camera.Password)
	host := extractRTSPHost(source.RTSPURL)

	if probe := ffprobePath(w.Config.Global.FFmpegPath); probe != "" {
		cmd := exec.CommandContext(
			ctx,
			probe,
			"-v", "error",
			"-rtsp_transport", w.rtspTransport(),
			"-stimeout", "5000000",
			"-select_streams", "v:0",
			"-show_entries", "stream=codec_name",
			"-of", "default=nokey=1:noprint_wrappers=1",
			url,
		)

		if out, err := cmd.CombinedOutput(); err == nil {
			codec := strings.ToLower(strings.TrimSpace(string(out)))
			if codec != "" {
				log.Printf("[EVENT] recording.ffprobe.codec camera_id=%s host=%s codec=%s", w.CameraID, host, codec)
				return
			}
			log.Printf("[WARNING] recording.ffprobe.empty camera_id=%s host=%s", w.CameraID, host)
		} else {
			log.Printf("[WARNING] recording.ffprobe.failed camera_id=%s host=%s err=%v", w.CameraID, host, err)
		}
	}

	if discoverer := gstDiscovererPath(w.Config.Global.GstLaunchPath); discoverer != "" {
		cmd := exec.CommandContext(ctx, discoverer, "--timeout=10", url)
		out, err := cmd.CombinedOutput()
		txt := strings.ToLower(string(out))
		if codec := parseDiscovererCodec(txt); codec != "" {
			log.Printf("[EVENT] recording.gst_discoverer.codec camera_id=%s host=%s codec=%s", w.CameraID, host, codec)
			return
		}
		if err != nil {
			log.Printf("[WARNING] recording.gst_discoverer.failed camera_id=%s host=%s err=%v", w.CameraID, host, err)
			return
		}
		log.Printf("[WARNING] recording.gst_discoverer.empty camera_id=%s host=%s", w.CameraID, host)
	}
}

func (w *RecorderWorker) detectCodecFromRTSP(ctx context.Context, source RecordingSource) string {
	if w.Config == nil {
		return ""
	}
	discoverer := gstDiscovererPath(w.Config.Global.GstLaunchPath)
	if discoverer == "" {
		return ""
	}

	url := injectRTSPCreds(source.RTSPURL, w.Camera.Username, w.Camera.Password)
	probeCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, discoverer, "--timeout=10", url)
	out, err := cmd.CombinedOutput()
	txt := strings.ToLower(string(out))
	host := extractRTSPHost(source.RTSPURL)
	if codec := parseDiscovererCodec(txt); codec != "" {
		if err != nil {
			log.Printf("[WARNING] recording.gst_discoverer.nonfatal camera_id=%s host=%s err=%v", w.CameraID, host, err)
		}
		log.Printf("[EVENT] recording.gst_discoverer.codec camera_id=%s host=%s codec=%s", w.CameraID, host, codec)
		return codec
	}
	if err != nil {
		log.Printf("[WARNING] recording.gst_discoverer.failed camera_id=%s host=%s err=%v", w.CameraID, host, err)
		return ""
	}
	log.Printf("[WARNING] recording.gst_discoverer.empty camera_id=%s host=%s", w.CameraID, host)
	return ""
}

func parseDiscovererCodec(txt string) string {
	if txt == "" {
		return ""
	}
	if strings.Contains(txt, "video/x-h265") || strings.Contains(txt, "h.265") || strings.Contains(txt, "h265/90000") || strings.Contains(txt, "hevc") {
		return "h265"
	}
	if strings.Contains(txt, "video/x-h264") || strings.Contains(txt, "h.264") || strings.Contains(txt, "h264/90000") || strings.Contains(txt, "avc") {
		return "h264"
	}
	return ""
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
	w.lastFinalizedScan = time.Time{}
	w.startupAt = time.Now()
	w.mu.Unlock()

	if w.Store != nil {
		if lastEnd, err := w.Store.GetLatestSegmentEnd(ctx, w.CameraID); err == nil && !lastEnd.IsZero() {
			now := time.Now()
			if lastEnd.After(now) {
				lastEnd = now
			}
			w.mu.Lock()
			w.lastSegmentEndTS = lastEnd
			w.mu.Unlock()
			log.Printf("[EVENT] recording.worker.continuity_seeded camera_id=%s last_end=%s", w.CameraID, lastEnd.Format(time.RFC3339Nano))
		}
	}

	source, sourceIdx, err := w.selectRecordingSource(ctx)
	if err != nil {
		return err
	}

	w.mu.Lock()
	if w.currentSourceIdx != sourceIdx {
		w.codecRetryUsed = false
	}
	w.currentSourceIdx = sourceIdx
	w.Camera.RtspURL = source.RTSPURL
	w.mu.Unlock()

	if sourceIdx == 0 {
		log.Printf("[EVENT] recording.source.selected camera_id=%s source=main profile_token=%s host=%s codec=%s rtsp_url=%s", w.CameraID, source.ProfileToken, extractRTSPHost(source.RTSPURL), w.cameraCodec(), source.RTSPURL)
	} else {
		log.Printf("[WARNING] recording.source.fallback camera_id=%s source=sub profile_token=%s host=%s codec=%s rtsp_url=%s", w.CameraID, source.ProfileToken, extractRTSPHost(source.RTSPURL), w.cameraCodec(), source.RTSPURL)
	}

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

	// Detect codec from RTSP SDP/caps and set depay accordingly.
	if detected := w.detectCodecFromRTSP(ctx, source); detected != "" {
		w.Camera.Codec = detected
	} else if normalizeCodec(w.Camera.PreferredRecordingCodec) != "" && normalizeCodec(w.Camera.Codec) == "" {
		w.Camera.Codec = w.Camera.PreferredRecordingCodec
	} else if normalizeCodec(w.Camera.Codec) == "" {
		w.Camera.Codec = source.Codec
	}

	pattern := filepath.Join(runDir, "segment_%05d"+w.segmentExt()+".tmp")
	args := w.gstRecorderArgs(gstPath(pattern))
	log.Printf("[DEBUG] gst.launch camera_id=%s cmd=%s", w.CameraID, redactGstLaunchCmd(w.Config.Global.GstLaunchPath, args))
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

func (w *RecorderWorker) syncSegments(ctx context.Context, forceBackfill bool) error {
	w.mu.RLock()
	dir := w.currentDir
	w.mu.RUnlock()
	if dir == "" {
		return nil
	}

	segmentDuration := time.Duration(w.Config.Global.SegmentDurationSec) * time.Second
	settleDelay := 10 * time.Second
	activeTmpWindow := 90 * time.Second
	if d := 2 * segmentDuration; d > activeTmpWindow {
		activeTmpWindow = d
	}

	// Windows-friendly retry spread for rename/close races.
	retryDelays := []time.Duration{
		0,
		1 * time.Second,
		2 * time.Second,
		5 * time.Second,
		10 * time.Second,
	}

	upsertFinalized := func(finalPath string, info os.FileInfo, checksum string) error {
		if info == nil {
			var err error
			info, err = os.Stat(finalPath)
			if err != nil {
				return err
			}
		}
		if info.IsDir() || info.Size() == 0 {
			return nil
		}

		if _, ok := w.knownFiles[finalPath]; ok {
			return nil
		}

		end := info.ModTime()
		start := end.Add(-segmentDuration)

		w.mu.Lock()
		if !w.lastSegmentEndTS.IsZero() {
			// If drift is small (< 10s), snap to previous end to ensure perfect continuity
			if diff := start.Sub(w.lastSegmentEndTS); diff > -10*time.Second && diff < 10*time.Second {
				start = w.lastSegmentEndTS
				end = start.Add(segmentDuration)
			}
		}
		if end.After(w.lastSegmentEndTS) {
			w.lastSegmentEndTS = end
		}
		w.mu.Unlock()

		seg := &ArchiveSegment{
			TenantID:       w.Config.Global.DefaultTenantID,
			SiteID:         w.Config.Global.DefaultSiteID,
			CameraID:       w.CameraID,
			StartTS:        start,
			EndTS:          end,
			DurationMs:     end.Sub(start).Milliseconds(),
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
		w.maybePersistWorkingCodec(ctx)

		w.knownFiles[finalPath] = struct{}{}

		w.mu.Lock()
		w.lastDataTime = time.Now()
		w.mu.Unlock()

		return nil
	}

	// 1) Backfill finalized files on a cadence.
	// This keeps recovery coverage while avoiding a full directory walk every tick.
	if forceBackfill || w.shouldBackfillFinalized() {
		finalizedMatches, err := filepath.Glob(filepath.Join(dir, "*"+w.segmentExt()))
		if err != nil {
			return err
		}
		sort.Strings(finalizedMatches)

		for _, finalPath := range finalizedMatches {
			info, err := os.Stat(finalPath)
			if err != nil || info.IsDir() || info.Size() == 0 {
				continue
			}
			if _, ok := w.knownFiles[finalPath]; ok {
				continue
			}

			checksum, err := ComputeSHA256(finalPath)
			if err != nil {
				log.Printf("[RecorderWorker] checksum failed for finalized segment %s: %v", finalPath, err)
				continue
			}

			if err := upsertFinalized(finalPath, info, checksum); err != nil {
				return err
			}
		}

		w.markFinalizedScan()
	}

	// 2) Process tmp files in order.
	tmpMatches, err := filepath.Glob(filepath.Join(dir, "*"+w.segmentExt()+".tmp"))
	if err != nil {
		return err
	}
	sort.Strings(tmpMatches)

	var staleLockedTmp string
	var recentlyActiveTmp bool

	for _, p := range tmpMatches {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() || info.Size() == 0 {
			continue
		}

		age := time.Since(info.ModTime())
		if age < activeTmpWindow {
			recentlyActiveTmp = true
		}

		// Give GStreamer enough time to fully release the file on Windows.
		if age < settleDelay {
			continue
		}

		var finalPath, checksum string
		var finalizeErr error

		for i, delay := range retryDelays {
			if i > 0 {
				time.Sleep(delay)
			}
			finalPath, checksum, finalizeErr = FinalizeSegment(p)
			if finalizeErr == nil {
				break
			}
		}

		if finalizeErr != nil {
			if isSharingViolation(finalizeErr) && age >= activeTmpWindow {
				staleLockedTmp = p
				log.Printf("[RecorderWorker] stale locked tmp detected for %s (age=%s): %v", p, age, finalizeErr)
				break
			}

			log.Printf("[RecorderWorker] finalization failed after retries for %s: %v", p, finalizeErr)
			continue
		}

		info, err = os.Stat(finalPath)
		if err != nil {
			continue
		}

		if err := upsertFinalized(finalPath, info, checksum); err != nil {
			return err
		}
	}

	if staleLockedTmp != "" {
		return fmt.Errorf("stale tmp requires recorder restart: %s", staleLockedTmp)
	}

	// 3) Refresh watchdog only if there is evidence of recent ingest activity,
	// not merely because a stale tmp file exists.
	if recentlyActiveTmp {
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

func (w *RecorderWorker) shouldBackfillFinalized() bool {
	w.mu.RLock()
	lastScan := w.lastFinalizedScan
	w.mu.RUnlock()
	return lastScan.IsZero() || time.Since(lastScan) >= finalizedBackfillInterval
}

func (w *RecorderWorker) markFinalizedScan() {
	w.mu.Lock()
	w.lastFinalizedScan = time.Now()
	w.mu.Unlock()
}

func (w *RecorderWorker) cameraCodec() string {
	if c := normalizeCodec(w.Camera.Codec); c != "" {
		return c
	}
	if c := normalizeCodec(w.Camera.PreferredRecordingCodec); c != "" {
		return c
	}
	if c := inferCodecFromRTSPURL(w.Camera.RtspURL); c != "" {
		return c
	}
	return "h265"
}

func alternateCodec(codec string) string {
	switch normalizeCodec(codec) {
	case "h264":
		return "h265"
	case "h265":
		return "h264"
	default:
		return ""
	}
}

func (w *RecorderWorker) retryStartupWithAlternateCodec(ctx context.Context, runID uint64) bool {
	w.mu.Lock()
	startupAt := w.startupAt
	current := w.cameraCodec()
	if w.codecRetryUsed || startupAt.IsZero() || time.Since(startupAt) > startupCodecRetryWindow {
		w.mu.Unlock()
		return false
	}
	alt := alternateCodec(current)
	if alt == "" {
		w.mu.Unlock()
		return false
	}
	w.codecRetryUsed = true
	w.Camera.Codec = alt
	w.mu.Unlock()

	log.Printf("[WARNING] recording.codec.fallback camera_id=%s from=%s to=%s", w.CameraID, current, alt)
	w.stopProcess()
	w.clearProcessHandle(runID)
	w.cleanupEmptyRunDir()
	if !w.waitBackoff(ctx, runID) {
		return false
	}
	return true
}

func (w *RecorderWorker) persistWorkingCodec(ctx context.Context) {
	codec := normalizeCodec(w.cameraCodec())
	if codec == "" {
		return
	}
	if s, ok := w.Store.(*PostgresStore); ok && s != nil {
		if err := s.UpdatePreferredRecordingCodec(ctx, w.CameraID, codec); err != nil {
			log.Printf("[WARNING] failed to persist preferred codec for %s: %v", w.CameraID, err)
			return
		}
	}
	w.mu.Lock()
	w.Camera.PreferredRecordingCodec = codec
	w.mu.Unlock()
}

func (w *RecorderWorker) maybePersistWorkingCodec(ctx context.Context) {
	w.mu.RLock()
	codec := normalizeCodec(w.Camera.Codec)
	persisted := normalizeCodec(w.Camera.PreferredRecordingCodec)
	w.mu.RUnlock()
	if codec == "" || codec == persisted {
		return
	}
	w.persistWorkingCodec(ctx)
}

func (w *RecorderWorker) selectRecordingSource(ctx context.Context) (RecordingSource, int, error) {
	sources := w.loadRecordingSources(ctx)
	if len(sources) == 0 {
		return RecordingSource{}, 0, fmt.Errorf("no recording source available for %s", w.CameraID)
	}

	w.mu.RLock()
	idx := w.recordingSourceIx
	w.mu.RUnlock()
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sources) {
		idx = len(sources) - 1
	}
	return sources[idx], idx, nil
}

func (w *RecorderWorker) loadRecordingSources(ctx context.Context) []RecordingSource {
	if s, ok := w.Store.(*PostgresStore); ok && s != nil {
		if sources, err := s.LoadCameraRecordingSources(ctx, w.CameraID, w.Camera.RtspURL); err == nil && len(sources) > 0 {
			if w.Config != nil && w.Config.Recording.ForceMainStream {
				// Always prefer main stream; do not allow sub-stream fallback.
				return []RecordingSource{sources[0]}
			}
			return sources
		}
	}

	w.mu.RLock()
	rtspURL := strings.TrimSpace(w.sourceBaseRTSP)
	w.mu.RUnlock()
	if rtspURL == "" {
		return nil
	}
	if w.Config != nil && w.Config.Recording.ForceMainStream {
		return []RecordingSource{{RTSPURL: rtspURL, Codec: inferCodecFromRTSPURL(rtspURL)}}
	}
	return []RecordingSource{{RTSPURL: rtspURL, Codec: inferCodecFromRTSPURL(rtspURL)}}
}

func (w *RecorderWorker) advanceRecordingSource() {
	if w.Config != nil && w.Config.Recording.ForceMainStream {
		return
	}
	w.mu.Lock()
	w.recordingSourceIx++
	w.mu.Unlock()
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
	)

	switch w.cameraCodec() {
	case "h264":
		base = append(base,
			"!", "rtph264depay",
			"!", "h264parse", "config-interval=-1",
		)
	default:
		base = append(base,
			"!", "rtph265depay",
			"!", "h265parse", "config-interval=-1",
		)
	}

	switch w.cameraSegmentFormat() {
	case "mp4":
		base = append(base,
			"!", "splitmuxsink",
			"location="+gstPath(pattern),
			"max-size-time="+fmt.Sprintf("%d", segmentNs),
			"send-keyframe-requests=true",
			"muxer-factory=mp4mux",
			"muxer-properties=properties,streamable=true",
		)
	default:
		base = append(base,
			"!", "splitmuxsink",
			"location="+gstPath(pattern),
			"max-size-time="+fmt.Sprintf("%d", segmentNs),
			"send-keyframe-requests=true",
			"muxer-factory=matroskamux",
			"muxer-properties=properties,streamable=true",
		)
	}

	return base
}

func redactGstLaunchCmd(bin string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	if strings.TrimSpace(bin) != "" {
		parts = append(parts, bin)
	}
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "user-pw="):
			parts = append(parts, "user-pw=REDACTED")
		default:
			parts = append(parts, arg)
		}
	}
	return strings.Join(parts, " ")
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

func isSharingViolation(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "being used by another process") ||
		strings.Contains(s, "sharing violation") ||
		strings.Contains(s, "access is denied")
}
