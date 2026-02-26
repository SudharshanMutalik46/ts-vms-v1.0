package recording

import (
	"context"
	"log"
	"time"
)

type WorkerState string

const (
	StateStopped            WorkerState = "STOPPED"
	StateRecording          WorkerState = "RECORDING"
	StateError              WorkerState = "ERROR"
	StateThrottledByLicense WorkerState = "THROTTLED_BY_LICENSE"
)

type CameraWorker struct {
	CameraID string
	State    WorkerState
	cancel   context.CancelFunc
}

// Start spawns the background routine (Thread-per-camera)
func (w *CameraWorker) Start(ctx context.Context) {
	log.Printf("[EVENT] recording.worker.started | camera_id=%s", w.CameraID)
	w.State = StateRecording

	workerCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel

	go func() {
		defer log.Printf("[EVENT] recording.worker.stopped | camera_id=%s", w.CameraID)

		// Phase 4.3: This loop will execute GStreamer pipelines and file segmenting
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-workerCtx.Done():
				w.State = StateStopped
				return
			case <-ticker.C:
				// Stub: Simulate writing a frame
				_ = "writing segment..."
			}
		}
	}()
}

func (w *CameraWorker) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
}
