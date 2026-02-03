package nvr

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/data"
)

type EventEnricher struct {
	repo data.NVRRepository

	// Cache: key="tenant:nvr:channelRef" -> cameraID (or uuid.Nil if none)
	cache    sync.Map
	cleanTkr *time.Ticker
}

type cacheEntry struct {
	cameraID   uuid.UUID
	unmapped   bool
	expiryTime time.Time
}

func NewEventEnricher(repo data.NVRRepository) *EventEnricher {
	e := &EventEnricher{
		repo: repo,
	}
	e.cleanTkr = time.NewTicker(5 * time.Minute)
	go e.cleanupLoop()
	return e
}

func (e *EventEnricher) Enrich(ctx context.Context, evt *VmsEvent) {
	key := fmt.Sprintf("%s:%s:%s", evt.TenantID, evt.NVRID, evt.ChannelRef)

	// Check Cache
	if val, ok := e.cache.Load(key); ok {
		entry := val.(cacheEntry)
		if time.Now().Before(entry.expiryTime) {
			if !entry.unmapped {
				evt.CameraID = &entry.cameraID
			}
			return
		}
		e.cache.Delete(key)
	}

	// DB Failure Handling: If DB fails, we proceed with unmapped event to avoid dropping it?
	// Or we retry? For enrichment, best effort is usually acceptable to avoid blocking pipeline.
	// We'll log error if we had mechanism, but here we just leave CameraID nil.

	// Lookup DB
	// We need a method to lookup Camera ID by NVR+Channel.
	// ListLinks returns []NVRCameraLink.
	// This might be heavy if NVR has 256 channels?
	// Better: `GetCameraByChannel(ctx, nvrID, channelRef)`
	// If not exists in repository interface, we might need to add it or ListAll and cache all?
	// Let's assume ListLinks is cached or we ListLinks once per 60s?
	// For now, let's use ListLinks and finding the channel. This is suboptimal but functional for Phase 2.10.

	// ListLinks with high limit?
	links, err := e.repo.ListLinks(ctx, evt.NVRID, 200, 0)
	if err == nil {
		found := false
		for _, link := range links {
			// NVRChannelRef is a pointer
			if link.NVRChannelRef != nil && *link.NVRChannelRef == evt.ChannelRef {
				evt.CameraID = &link.CameraID
				e.cache.Store(key, cacheEntry{cameraID: link.CameraID, expiryTime: time.Now().Add(60 * time.Second)})
				found = true
				break
			}
		}
		if !found {
			e.cache.Store(key, cacheEntry{unmapped: true, expiryTime: time.Now().Add(60 * time.Second)})
		}
	}
}

func (e *EventEnricher) cleanupLoop() {
	for range e.cleanTkr.C {
		now := time.Now()
		e.cache.Range(func(k, v interface{}) bool {
			entry := v.(cacheEntry)
			if now.After(entry.expiryTime) {
				e.cache.Delete(k)
			}
			return true
		})
	}
}
