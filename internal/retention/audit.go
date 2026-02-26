package retention

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"
)

type AuditRecord struct {
	Timestamp          time.Time `json:"timestamp"`
	EventType          string    `json:"event_type"`
	TenantID           string    `json:"tenant_id"`
	SiteID             string    `json:"site_id"`
	CameraID           string    `json:"camera_id"`
	SegmentPath        string    `json:"segment_path"`
	Reason             string    `json:"reason"`
	PolicySnapshot     any       `json:"policy_snapshot"`
	BytesFreedExpected int64     `json:"bytes_freed_expected"`
	BytesFreedVerified int64     `json:"bytes_freed_verified"`
}

type IAuditWriter interface {
	Write(record AuditRecord)
}

type JSONAuditWriter struct {
	filePath string
	mu       sync.Mutex
}

func NewJSONAuditWriter(path string) *JSONAuditWriter {
	return &JSONAuditWriter{filePath: path}
}

func (a *JSONAuditWriter) Write(record AuditRecord) {
	a.mu.Lock()
	defer a.mu.Unlock()

	f, err := os.OpenFile(a.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("[ERROR] retention.audit failed to open audit log: %v", err)
		return
	}
	defer f.Close()

	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now()
	}

	encoder := json.NewEncoder(f)
	if err := encoder.Encode(record); err != nil {
		log.Printf("[ERROR] retention.audit failed to write entry: %v", err)
	}
}
