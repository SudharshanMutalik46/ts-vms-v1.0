package recording

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/technosupport/ts-vms/internal/crypto"
)

type RecordingArchiverService struct {
	config        *Config
	scheduler     *ScheduleEngine
	license       *LicenseGate
	store         *PostgresStore
	workers       map[string]*RecorderWorker
	cameraConfigs map[string]CameraConfig
	ctx           context.Context
	keyring       *crypto.Keyring
	mu            sync.RWMutex
}

func NewRecordingArchiverService(cfg *Config, sched *ScheduleEngine, lic *LicenseGate, store *PostgresStore, keyring *crypto.Keyring) *RecordingArchiverService {
	camMap := make(map[string]CameraConfig, len(cfg.Cameras))
	for _, cam := range cfg.Cameras {
		camMap[cam.ID] = cam
	}
	return &RecordingArchiverService{
		config:        cfg,
		scheduler:     sched,
		license:       lic,
		store:         store,
		workers:       make(map[string]*RecorderWorker),
		cameraConfigs: camMap,
		keyring:       keyring,
	}
}

func (s *RecordingArchiverService) Run(ctx context.Context) {
	s.ctx = ctx
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.StopAll()
			return
		case <-ticker.C:
			s.reconcile()
		}
	}
}

func (s *RecordingArchiverService) reconcile() {
	s.mu.Lock()
	defer s.mu.Unlock()

	cameras := make([]CameraConfig, 0, len(s.cameraConfigs))
	for _, cam := range s.cameraConfigs {
		cameras = append(cameras, cam)
	}

	for _, cam := range cameras {
		should := s.scheduler.ShouldRecord(cam.ID)
		worker, exists := s.workers[cam.ID]
		if !cam.Enabled {
			if exists {
				worker.Stop()
				delete(s.workers, cam.ID)
				s.license.Release()
			}
			continue
		}
		if should {
			if !exists {
				worker = s.ensureWorkerLocked(cam)
				if !s.acquireLicenseLocked(worker, cam.ID) {
					continue
				}
			}
			if !worker.IsRunning() && (worker.State == StateStopped || worker.State == StateError || worker.State == StateThrottledByLicense) {
				worker.Start(s.ctx)
			}
			continue
		}

		if exists && (worker.IsRunning() || worker.State == StateRecording || worker.State == StateStarting || worker.State == StateError || worker.State == StatePaused) {
			worker.Stop()
			s.releaseLicenseLocked(worker)
		}
	}
}

func (s *RecordingArchiverService) ApplyManualState(camID, action string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cam, ok := s.cameraConfigs[camID]
	if !ok {
		return fmt.Errorf("camera not configured for recording: %s", camID)
	}
	w := s.ensureWorkerLocked(cam)
	switch action {
	case "START":
		if !s.acquireLicenseLocked(w, camID) {
			return nil
		}
		w.Start(s.ctx)
	case "STOP":
		w.Stop()
		s.releaseLicenseLocked(w)
	case "PAUSE":
		w.Pause()
	case "RESUME":
		if !s.acquireLicenseLocked(w, camID) {
			return nil
		}
		w.Resume(s.ctx)
	}
	return nil
}

func (s *RecordingArchiverService) UpsertCamera(cam CameraConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if old, ok := s.cameraConfigs[cam.ID]; ok {
		if worker, exists := s.workers[cam.ID]; exists && (old.RtspURL != cam.RtspURL || old.Enabled != cam.Enabled) {
			worker.Stop()
			delete(s.workers, cam.ID)
			s.license.Release()
		}
	}
	s.cameraConfigs[cam.ID] = cam
}

func (s *RecordingArchiverService) RemoveCamera(camID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cameraConfigs, camID)
	if worker, exists := s.workers[camID]; exists {
		worker.Stop()
		delete(s.workers, camID)
		s.license.Release()
	}
}

func (s *RecordingArchiverService) AttachCamera(cam CameraConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cam.ID == "" {
		return fmt.Errorf("camera id is required")
	}
	if cam.Enabled && cam.RtspURL == "" {
		return fmt.Errorf("camera rtsp_url is required when camera is enabled")
	}

	if old, ok := s.cameraConfigs[cam.ID]; ok {
		if worker, exists := s.workers[cam.ID]; exists && (old.RtspURL != cam.RtspURL || old.Enabled != cam.Enabled) {
			worker.Stop()
			delete(s.workers, cam.ID)
			s.license.Release()
		}
	}
	s.cameraConfigs[cam.ID] = cam

	if !cam.Enabled {
		if worker, ok := s.workers[cam.ID]; ok {
			worker.Stop()
			s.license.Release()
			delete(s.workers, cam.ID)
		}
		return nil
	}

	worker := s.ensureWorkerLocked(cam)
	if !s.scheduler.ShouldRecord(cam.ID) {
		return nil
	}
	if !s.acquireLicenseLocked(worker, cam.ID) {
		return nil
	}
	if s.ctx != nil && !worker.IsRunning() {
		worker.Start(s.ctx)
	}
	return nil
}

func (s *RecordingArchiverService) ReloadSchedules(cfgs []ScheduleConfig) {
	s.scheduler.UpdateConfigs(cfgs)
	log.Printf("[RecordingArchiverService] reloaded %d schedules", len(cfgs))
}

func (s *RecordingArchiverService) BulkAction(action string) {
	s.mu.RLock()
	ids := make([]string, 0, len(s.cameraConfigs))
	for id := range s.cameraConfigs {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	for _, id := range ids {
		_ = s.ApplyManualState(id, action)
	}
}

func (s *RecordingArchiverService) StopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.workers {
		w.Stop()
		s.releaseLicenseLocked(w)
	}
}

func (s *RecordingArchiverService) GetStatus() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	workers := make([]map[string]any, 0, len(s.workers))
	for _, w := range s.workers {
		workers = append(workers, w.Status())
	}
	return map[string]any{
		"workers":   workers,
		"schedules": s.scheduler.Snapshot(),
	}
}

func (s *RecordingArchiverService) ensureWorkerLocked(cam CameraConfig) *RecorderWorker {
	w, exists := s.workers[cam.ID]
	if !exists || w == nil {
		w = NewRecorderWorker(s.config, cam, s.store, s.keyring)
		s.workers[cam.ID] = w
		return w
	}
	w.Camera = cam
	w.Config = s.config
	w.Store = s.store
	return w
}

func (s *RecordingArchiverService) acquireLicenseLocked(w *RecorderWorker, camID string) bool {
	if w.HasLicense() {
		return true
	}
	if !s.license.TryAcquire(camID) {
		w.SetThrottled()
		return false
	}
	w.SetLicenseHeld(true)
	return true
}

func (s *RecordingArchiverService) releaseLicenseLocked(w *RecorderWorker) {
	if w == nil || !w.HasLicense() {
		return
	}
	s.license.Release()
	w.SetLicenseHeld(false)
}

func (s *RecordingArchiverService) upsertCameraLocked(cam CameraConfig) {
	s.cameraConfigs[cam.ID] = cam
	replaced := false
	for i := range s.config.Cameras {
		if s.config.Cameras[i].ID == cam.ID {
			s.config.Cameras[i] = cam
			replaced = true
			break
		}
	}
	if !replaced {
		s.config.Cameras = append(s.config.Cameras, cam)
	}
}
