package audit

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// AuditEvent represents a single audit log entry
type AuditEvent struct {
	ID          uuid.UUID       `json:"id"`       // DB Primary Key
	EventID     uuid.UUID       `json:"event_id"` // Idempotency Key
	TenantID    uuid.UUID       `json:"tenant_id"`
	ActorUserID *uuid.UUID      `json:"actor_user_id,omitempty"`
	Action      string          `json:"action"`
	TargetType  string          `json:"target_type,omitempty"`
	TargetID    string          `json:"target_id,omitempty"`
	Result      string          `json:"result"` // success/failure
	ReasonCode  string          `json:"reason_code,omitempty"`
	RequestID   string          `json:"request_id,omitempty"`
	ClientIP    string          `json:"client_ip,omitempty"` // Hashed ideally, or raw if policy allows (Hashed required by 1.4, here we store what passed)
	UserAgent   string          `json:"user_agent,omitempty"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

// FailoverEvent wrapper for JSONL spooling
type FailoverEvent struct {
	EventID   string     `json:"event_id"`
	TenantID  string     `json:"tenant_id"`
	Payload   AuditEvent `json:"payload"`
	Timestamp time.Time  `json:"timestamp"`
}

// AuditFilter for querying
type AuditFilter struct {
	TenantID    uuid.UUID
	ActorUserID *uuid.UUID
	DateFrom    *time.Time
	DateTo      *time.Time
	Result      string
	Limit       int
	Cursor      string // ID-based cursor
}

// Service is the main interface
type Service struct {
	DB *sql.DB
	// Spooler injected later
}

func NewService(db *sql.DB) *Service {
	return &Service{DB: db}
}

// EnsureRetention checks if db supports 7 year retention policy enforcement
// (For this phase, it's a policy guard, logic implemented in separate file or here).
func (s *Service) EnsureRetention(years int) error {
	if years < 7 {
		return fmt.Errorf("retention policy restriction: minimum 7 years required")
	}
	return nil
}
