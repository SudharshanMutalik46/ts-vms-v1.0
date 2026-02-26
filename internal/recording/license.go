package recording

import (
	"log"
	"sync"
)

type LicenseGate struct {
	mu          sync.Mutex
	activeCount int
	maxCameras  int
}

func NewLicenseGate(max int) *LicenseGate {
	return &LicenseGate{maxCameras: max}
}

func (l *LicenseGate) TryAcquire(camID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.activeCount >= l.maxCameras {
		log.Printf("[ALERT] recording.license.denied | camera_id=%s reason=quota_exceeded active=%d max=%d", camID, l.activeCount, l.maxCameras)
		return false
	}
	l.activeCount++
	return true
}

func (l *LicenseGate) Release() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.activeCount > 0 {
		l.activeCount--
	}
}
