package recording

import (
	"sync"
	"time"
)

type ScheduleEngine struct {
	configs        map[string]ScheduleConfig
	eventDeadlines map[string]time.Time
	mu             sync.RWMutex
}

func NewScheduleEngine(schedules []ScheduleConfig) *ScheduleEngine {
	cfgMap := make(map[string]ScheduleConfig)
	for _, s := range schedules {
		cfgMap[s.CameraID] = s
	}
	return &ScheduleEngine{
		configs:        cfgMap,
		eventDeadlines: make(map[string]time.Time),
	}
}

func (s *ScheduleEngine) TriggerEvent(cameraID string, durationSec int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventDeadlines[cameraID] = time.Now().Add(time.Duration(durationSec) * time.Second)
}

func (s *ScheduleEngine) ShouldRecord(cameraID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 1. Check Event Overrides first
	if deadline, exists := s.eventDeadlines[cameraID]; exists {
		if time.Now().Before(deadline) {
			return true
		}
	}

	cfg, exists := s.configs[cameraID]
	if !exists {
		return false
	}

	if cfg.Type == "24x7" {
		return true
	}

	if cfg.Type == "time_window" {
		now := time.Now()
		dayStr := now.Weekday().String()
		dayMatch := false
		for _, d := range cfg.Days {
			if d == dayStr {
				dayMatch = true
				break
			}
		}
		if !dayMatch {
			return false
		}

		currentHHMM := now.Format("15:04")
		if currentHHMM >= cfg.StartTime && currentHHMM <= cfg.EndTime {
			return true
		}
	}

	return false
}
