package nvr

import (
	"fmt"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

type EventDedup struct {
	cache *lru.Cache[string, time.Time]
	ttl   time.Duration
}

func NewEventDedup(maxKeys int, ttlSeconds int) *EventDedup {
	c, _ := lru.New[string, time.Time](maxKeys)
	return &EventDedup{
		cache: c,
		ttl:   time.Duration(ttlSeconds) * time.Second,
	}
}

func (d *EventDedup) IsDuplicate(key string) bool {
	if addedAt, ok := d.cache.Get(key); ok {
		if time.Since(addedAt) < d.ttl {
			return true // Duplicate within window
		}
		// Expired but still in LRU? Update it.
	}
	d.cache.Add(key, time.Now())
	return false
}

func BuildDedupKey(tenantID, nvrID, channelRef, eventType string, occurredAt time.Time) string {
	// Bucket time to 1 second to handle micro-timing diffs? Or exact?
	// Phase 2.10: "occurred_at_bucket"
	// Let's bucket to 1 second.
	ts := occurredAt.Truncate(time.Second).Unix()
	return fmt.Sprintf("%s|%s|%s|%s|%d", tenantID, nvrID, channelRef, eventType, ts)
}
