package health

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/technosupport/ts-vms/internal/data"
)

// SchedulerConfig defines parameters
type SchedulerConfig struct {
	Interval       time.Duration
	WorkerPoolSize int
}

type Scheduler struct {
	config  SchedulerConfig
	service *Service // Scheduler calls Service.CheckCamera
	quit    chan struct{}
	wg      sync.WaitGroup
}

func NewScheduler(cfg SchedulerConfig, svc *Service) *Scheduler {
	if cfg.Interval == 0 {
		cfg.Interval = 60 * time.Second
	}
	if cfg.WorkerPoolSize == 0 {
		cfg.WorkerPoolSize = 50
	}
	return &Scheduler{
		config:  cfg,
		service: svc,
		quit:    make(chan struct{}),
	}
}

// Start initiates the scheduling loop
func (s *Scheduler) Start() {
	s.wg.Add(1)
	go s.run()
}

func (s *Scheduler) Stop() {
	close(s.quit)
	s.wg.Wait()
}

func (s *Scheduler) run() {
	defer s.wg.Done()

	// Worker Pool
	// We use a fixed number of workers consuming from a Job Queue.
	// The dispatch loop pushes to this queue non-blocking.

	jobQueue := make(chan data.CameraHealthTarget, s.config.WorkerPoolSize*2) // Buffer 2x pool

	// Start Workers
	for i := 0; i < s.config.WorkerPoolSize; i++ {
		s.wg.Add(1)
		go s.worker(jobQueue)
	}

	ticker := time.NewTicker(s.config.Interval)
	defer ticker.Stop()

	// Initial Run
	s.dispatchChecks(jobQueue)

	for {
		select {
		case <-ticker.C:
			s.dispatchChecks(jobQueue)
		case <-s.quit:
			close(jobQueue) // Signal workers to stop
			return
		}
	}
}

func (s *Scheduler) worker(jobs <-chan data.CameraHealthTarget) {
	defer s.wg.Done()
	ctx := context.Background()

	for job := range jobs {
		// Jitter to avoid thundering herd on DB
		// Sleep 0-1000ms
		jitter := time.Duration(rand.Intn(1000)) * time.Millisecond
		time.Sleep(jitter)

		s.service.PerformCheck(ctx, job.TenantID, job.CameraID, job.RTSPURL)
	}
}

func (s *Scheduler) dispatchChecks(queue chan<- data.CameraHealthTarget) {
	ctx := context.Background()
	cameras, err := s.service.ListTargets(ctx)
	if err != nil {
		// Log error
		return
	}

	skipped := 0
	queued := 0

	for _, c := range cameras {
		// 1. Backoff Check (Optimization: Don't queue if backing off)
		if s.shouldSkip(c) {
			continue
		}

		// 2. Non-Blocking Push
		select {
		case queue <- c:
			queued++
		default:
			// Queue Full -> Drop/Skip (Backpressure)
			skipped++
			// Metric: CameraHealthQueueDropped++
		}
	}
	// Metric: CameraHealthQueueDepth = len(queue)
}

func (s *Scheduler) shouldSkip(c data.CameraHealthTarget) bool {
	// Backoff Logic
	// if LastChecked + Backoff > Now -> Skip
	// Backoff = base * 2^failures (capped)
	// Base = 60s.
	// Failures = c.ConsecutiveFailures

	if c.Status == data.HealthStatusOnline {
		return false
	}

	backoff := 60 * time.Second
	// Simple multiplier: 60, 120, 300 cap.
	if c.ConsecutiveFailures > 0 {
		if c.ConsecutiveFailures > 1 {
			backoff = 120 * time.Second
		}
		if c.ConsecutiveFailures > 5 {
			backoff = 300 * time.Second
		}
	}

	nextCheck := c.LastCheckedAt.Add(backoff)
	return time.Now().Before(nextCheck)
}
