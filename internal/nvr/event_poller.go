package nvr

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/metrics"
)

type PollerConfig struct {
	Enabled          bool
	PollInterval     time.Duration
	MaxInflight      int
	MaxEventsPerPoll int
	TimeBudget       time.Duration
	Backoff          time.Duration
}

type NVRPoller struct {
	service  *Service
	repo     data.NVRRepository
	pub      *NATSPublisher
	enricher *EventEnricher
	dedup    *EventDedup
	cfg      PollerConfig

	sem      chan struct{}
	stopChan chan struct{}
	wg       sync.WaitGroup
}

func NewNVRPoller(s *Service, pub *NATSPublisher, enricher *EventEnricher, dedup *EventDedup, cfg PollerConfig) *NVRPoller {
	if cfg.MaxInflight <= 0 {
		cfg.MaxInflight = 10
	}
	return &NVRPoller{
		service:  s,
		repo:     s.GetRepo(),
		pub:      pub,
		enricher: enricher,
		dedup:    dedup,
		cfg:      cfg,
		sem:      make(chan struct{}, cfg.MaxInflight),
		stopChan: make(chan struct{}),
	}
}

func (p *NVRPoller) Start() {
	if !p.cfg.Enabled {
		return
	}
	p.wg.Add(1)
	go p.runLoop()
}

func (p *NVRPoller) Stop() {
	if !p.cfg.Enabled {
		return
	}
	close(p.stopChan)
	p.wg.Wait()
}

func (p *NVRPoller) runLoop() {
	defer p.wg.Done()
	ticker := time.NewTicker(p.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			return
		case <-ticker.C:
			p.pollAll()
		}
	}
}

func (p *NVRPoller) pollAll() {
	ctx := context.Background()
	nvrs, err := p.repo.ListAllNVRs(ctx)
	if err != nil {
		log.Printf("[ERROR] NVR Poller: Error listing NVRs: %v", err)
		return
	}

	for _, nvr := range nvrs {
		if !nvr.IsEnabled || nvr.Status != "online" {
			continue
		}

		select {
		case p.sem <- struct{}{}:
			p.wg.Add(1)
			go func(n *data.NVR) {
				defer p.wg.Done()
				defer func() { <-p.sem }()
				p.pollNVR(ctx, n)
			}(nvr)
		default:
			metrics.NVRChecksTotal.WithLabelValues("fail", "poller_capacity_full").Inc()
		}
	}
}

func (p *NVRPoller) pollNVR(ctx context.Context, n *data.NVR) {
	// Enforce Time Budget for the fetch operation
	fetchCtx, cancel := context.WithTimeout(ctx, p.cfg.TimeBudget)
	defer cancel()

	// 1. Get State
	state, err := p.repo.GetEventPollState(fetchCtx, n.ID)
	if err != nil {
		if err != context.DeadlineExceeded && err != context.Canceled {
			log.Printf("[ERROR] NVR Poller (%s): Error fetching state: %v", n.Name, err)
		}
		return
	}

	// Check Backoff logic
	if state != nil && state.ConsecutiveFailures > 0 {
		if time.Since(state.UpdatedAt) < p.cfg.Backoff {
			return
		}
	}

	since := time.Now().Add(-1 * time.Hour) // Default lookback window if first run
	if state != nil && state.SinceTS != nil {
		since = *state.SinceTS
	}

	// 2. Fetch via Adapter
	adapter, target, cred, err := p.service.getAdapterClient(fetchCtx, n.ID)
	if err != nil {
		p.recordFailure(ctx, n.ID, n.TenantID, err.Error(), state)
		return
	}

	// Fetch Events
	events, _, err := adapter.FetchEvents(fetchCtx, target, cred, since, p.cfg.MaxEventsPerPoll)
	if err != nil {
		p.recordFailure(ctx, n.ID, n.TenantID, err.Error(), state)
		return
	}

	if len(events) == 0 {
		t := time.Now()
		p.recordSuccess(ctx, n.ID, n.TenantID, t, state)
		return
	}

	var lastTime time.Time
	publishCount := 0

	for _, rawEvt := range events {
		vmsEvt, err := ConvertAdapterEvent(n.ID, n.TenantID, n.SiteID, rawEvt, n.Vendor)
		if err != nil {
			log.Printf("[DEBUG] NVR Poller (%s): Event conversion error: %v", n.Name, err)
			continue
		}

		p.enricher.Enrich(fetchCtx, vmsEvt)
		dedupKey := BuildDedupKey(n.TenantID.String(), n.ID.String(), vmsEvt.ChannelRef, vmsEvt.EventType, vmsEvt.OccurredAt)
		vmsEvt.DedupKey = dedupKey

		if p.dedup.IsDuplicate(dedupKey) {
			continue
		}

		if err := p.pub.Publish(vmsEvt); err != nil {
			p.recordFailure(ctx, n.ID, n.TenantID, fmt.Sprintf("publish_fail: %v", err), state)
			return
		}
		publishCount++

		if vmsEvt.OccurredAt.After(lastTime) {
			lastTime = vmsEvt.OccurredAt
		}
	}

	if !lastTime.IsZero() {
		p.recordSuccess(ctx, n.ID, n.TenantID, lastTime, state)
	} else {
		p.recordSuccess(ctx, n.ID, n.TenantID, time.Now(), state)
	}
}

func (p *NVRPoller) recordFailure(ctx context.Context, nvrID, tenantID uuid.UUID, errStr string, oldState *data.NVREventPollState) {
	failures := 1
	if oldState != nil {
		failures = oldState.ConsecutiveFailures + 1
	}

	dbCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	s := &data.NVREventPollState{
		TenantID:            tenantID,
		NVRID:               nvrID,
		LastErrorCode:       &errStr,
		ConsecutiveFailures: failures,
	}
	if oldState != nil {
		s.LastSuccessAt = oldState.LastSuccessAt
		s.SinceTS = oldState.SinceTS
		s.Cursor = oldState.Cursor
	}

	if err := p.repo.UpsertEventPollState(dbCtx, s); err != nil {
		log.Printf("[ERROR] NVR Poller (%s): Error saving failure state: %v", nvrID, err)
	}
}

func (p *NVRPoller) recordSuccess(ctx context.Context, nvrID, tenantID uuid.UUID, since time.Time, oldState *data.NVREventPollState) {
	now := time.Now()

	dbCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	s := &data.NVREventPollState{
		TenantID:            tenantID,
		NVRID:               nvrID,
		LastSuccessAt:       &now,
		SinceTS:             &since,
		ConsecutiveFailures: 0,
		LastErrorCode:       nil,
	}
	if err := p.repo.UpsertEventPollState(dbCtx, s); err != nil {
		log.Printf("[ERROR] NVR Poller (%s): Error saving success state: %v", nvrID, err)
	}
}
