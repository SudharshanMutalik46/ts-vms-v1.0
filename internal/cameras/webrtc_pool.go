package cameras

import (
	"context"
	"fmt"
	"sync"
)

// WebRtcPool manages concurrent H.265 transcoding sessions to prevent GPU overload.
type WebRtcPool struct {
	mu       sync.Mutex
	capacity int
	active   map[string]bool
}

func NewWebRtcPool(capacity int) *WebRtcPool {
	if capacity <= 0 {
		capacity = 16 // Default for modern GPUs
	}
	return &WebRtcPool{
		capacity: capacity,
		active:   make(map[string]bool),
	}
}

// Acquire attempts to reserve a transcode slot for a camera.
func (p *WebRtcPool) Acquire(ctx context.Context, cameraID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.active[cameraID] {
		return nil // Already using a slot
	}

	if len(p.active) >= p.capacity {
		return fmt.Errorf("H265_TRANSCODE_POOL_FULL")
	}

	p.active[cameraID] = true
	fmt.Printf("[WebRtcPool] Acquired slot for camera=%s (Active=%d/%d)\n", cameraID, len(p.active), p.capacity)
	return nil
}

// Release frees a transcode slot.
func (p *WebRtcPool) Release(cameraID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, found := p.active[cameraID]; found {
		delete(p.active, cameraID)
		fmt.Printf("[WebRtcPool] Released slot for camera=%s (Active=%d/%d)\n", cameraID, len(p.active), p.capacity)
	}
}
