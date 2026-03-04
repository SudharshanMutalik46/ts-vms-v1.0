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
	s := &ScheduleEngine{
		configs:        make(map[string]ScheduleConfig),
		eventDeadlines: make(map[string]time.Time),
	}
	s.UpdateConfigs(schedules)
	return s
}

func (s *ScheduleEngine) UpdateConfigs(schedules []ScheduleConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configs = make(map[string]ScheduleConfig, len(schedules))
	for _, cfg := range schedules {
		s.configs[cfg.CameraID] = cfg
	}
}

func (s *ScheduleEngine) Snapshot() []ScheduleConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ScheduleConfig, 0, len(s.configs))
	for _, cfg := range s.configs {
		out = append(out, cfg)
	}
	return out
}

func (s *ScheduleEngine) TriggerEvent(cameraID string, durationSec int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventDeadlines[cameraID] = time.Now().Add(time.Duration(durationSec) * time.Second)
}

func (s *ScheduleEngine) ShouldRecord(cameraID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if deadline, ok := s.eventDeadlines[cameraID]; ok && time.Now().Before(deadline) {
		return true
	}

	cfg, ok := s.configs[cameraID]
	if !ok {
		return true
	}
	if cfg.Type == "24x7" || cfg.Type == "" {
		return true
	}
	if cfg.Type == "event_triggered" {
		return false
	}
	if cfg.Type == "time_window" {
		now := time.Now()
		dayMatch := len(cfg.Days) == 0
		for _, d := range cfg.Days {
			if d == now.Weekday().String() {
				dayMatch = true
				break
			}
		}
		if !dayMatch {
			return false
		}
		hhmm := now.Format("15:04")
		return hhmm >= cfg.StartTime && hhmm <= cfg.EndTime
	}
	return false
}
