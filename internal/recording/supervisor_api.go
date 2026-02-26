package recording

import (
	"log"
)

// State modifications for Phase 4.7
const StatePaused WorkerState = "PAUSED"

// Extension to the Phase 4.2 Supervisor
type SupervisorExt struct {
	*Supervisor
	manualOverrides map[string]string // camera_id -> "START" or "STOP" or "PAUSE"
}

func NewSupervisorExt(base *Supervisor) *SupervisorExt {
	return &SupervisorExt{
		Supervisor:      base,
		manualOverrides: make(map[string]string),
	}
}

func (s *SupervisorExt) ApplyManualState(camID, action string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.manualOverrides[camID] = action
	worker, exists := s.workers[camID]

	switch action {
	case "START", "RESUME":
		if !exists || worker.State == StateStopped || worker.State == StatePaused {
			if s.license.TryAcquire(camID) {
				if !exists {
					worker = &CameraWorker{CameraID: camID}
					s.workers[camID] = worker
				}
				// worker.Start(context.Background()) - Stubbed for test
				worker.State = StateRecording
			} else {
				if !exists {
					s.workers[camID] = &CameraWorker{CameraID: camID, State: StateThrottledByLicense}
				} else {
					worker.State = StateThrottledByLicense
				}
			}
		}
	case "STOP":
		if exists && worker.State == StateRecording {
			// worker.Stop()
			s.license.Release()
		}
		if exists {
			worker.State = StateStopped
		}
	case "PAUSE":
		if exists && worker.State == StateRecording {
			// worker.Pause() - Suspend pipeline but keep license
			worker.State = StatePaused
		}
	}
	return nil
}

func (s *SupervisorExt) BulkAction(action string) {
	s.mu.Lock()
	cameras := make([]string, 0, len(s.config.Cameras))
	for _, c := range s.config.Cameras {
		cameras = append(cameras, c.ID)
	}
	s.mu.Unlock()

	for _, c := range cameras {
		s.ApplyManualState(c, action)
	}
}

func (s *SupervisorExt) ReloadSchedules(newConfigs []ScheduleConfig) {
	log.Printf("[Supervisor] Reloading %d schedules dynamically", len(newConfigs))
	// In reality: s.scheduler.UpdateConfigs(newConfigs)
}
