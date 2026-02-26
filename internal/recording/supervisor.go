package recording

import (
	"context"
	"log"
	"sync"
	"time"
)

type Supervisor struct {
	config    *Config
	scheduler *ScheduleEngine
	license   *LicenseGate
	workers   map[string]*CameraWorker
	mu        sync.RWMutex
}

func NewSupervisor(cfg *Config, sched *ScheduleEngine, lic *LicenseGate) *Supervisor {
	return &Supervisor{
		config:    cfg,
		scheduler: sched,
		license:   lic,
		workers:   make(map[string]*CameraWorker),
	}
}

func (s *Supervisor) Run(ctx context.Context) {
	log.Println("[INFO] Supervisor loop started.")
	ticker := time.NewTicker(2 * time.Second) // Reconcile every 2s
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.StopAll()
			return
		case <-ticker.C:
			s.reconcile(ctx)
		}
	}
}

func (s *Supervisor) reconcile(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, cam := range s.config.Cameras {
		if !cam.Enabled {
			continue
		}

		shouldRecord := s.scheduler.ShouldRecord(cam.ID)
		worker, exists := s.workers[cam.ID]

		if shouldRecord && !exists {
			// Try to start
			if s.license.TryAcquire(cam.ID) {
				newWorker := &CameraWorker{CameraID: cam.ID, State: StateStopped}
				newWorker.Start(ctx)
				s.workers[cam.ID] = newWorker
			} else {
				// Stub a throttled worker entry for status visibility
				s.workers[cam.ID] = &CameraWorker{CameraID: cam.ID, State: StateThrottledByLicense}
			}
		} else if shouldRecord && exists && worker.State == StateThrottledByLicense {
			// Retry acquiring license if previously throttled
			if s.license.TryAcquire(cam.ID) {
				worker.Start(ctx)
			}
		} else if !shouldRecord && exists {
			// Stop active worker
			if worker.State == StateRecording {
				worker.Stop()
				s.license.Release()
				log.Printf("[EVENT] recording.schedule.transition | camera_id=%s state=STOPPING", cam.ID)
			}
			delete(s.workers, cam.ID)
		}
	}
}

func (s *Supervisor) StopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.workers {
		if w.State == StateRecording {
			w.Stop()
		}
	}
}

func (s *Supervisor) GetStatus() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	status := make(map[string]string)
	for id, w := range s.workers {
		status[id] = string(w.State)
	}
	return status
}
