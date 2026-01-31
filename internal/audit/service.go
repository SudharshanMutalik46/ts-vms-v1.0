package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"

	"github.com/google/uuid"
)

func (s *Service) WriteEvent(ctx context.Context, evt AuditEvent) error {
	// Idempotency: If EventID is empty, generate it.
	if evt.EventID == uuid.Nil {
		evt.EventID = uuid.New()
	}

	// 1. Try DB Write
	query := `
		INSERT INTO audit_logs (
			event_id, tenant_id, actor_user_id, action, target_type, target_id,
			result, reason_code, request_id, client_ip, user_agent, metadata, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (event_id) DO NOTHING
	`

	_, err := s.DB.ExecContext(ctx, query,
		evt.EventID, evt.TenantID, evt.ActorUserID, evt.Action, evt.TargetType, evt.TargetID,
		evt.Result, evt.ReasonCode, evt.RequestID, evt.ClientIP, evt.UserAgent, evt.Metadata, evt.CreatedAt,
	)

	if err != nil {
		// 2. Failover to Spool
		log.Printf("Audit DB Write Failed: %v. Spooling event %s", err, evt.EventID)
		if spoolErr := SpoolEvent(evt); spoolErr != nil {
			log.Printf("CRITICAL: Audit Spool FAILED for event %s: %v", evt.EventID, spoolErr)
			return fmt.Errorf("audit critical failure: %v", spoolErr)
		}
		return nil // Swallow DB error if spooled successfully
	}

	return nil
}

// Append-only enforcement: No Update or Delete methods exposed.

// QueryEvents implements filters and cursor pagination
func (s *Service) QueryEvents(ctx context.Context, f AuditFilter) ([]AuditEvent, string, error) {
	// Build Query
	q := `SELECT id, event_id, tenant_id, actor_user_id, action, result, created_at, metadata 
	      FROM audit_logs 
	      WHERE tenant_id = $1`
	args := []interface{}{f.TenantID}
	idx := 2

	if f.Result != "" {
		q += fmt.Sprintf(" AND result = $%d", idx)
		args = append(args, f.Result)
		idx++
	}

	// Cursor (ID based scrolling)
	if f.Cursor != "" {
		q += fmt.Sprintf(" AND id < $%d", idx)
		args = append(args, f.Cursor)
		idx++
	}

	q += " ORDER BY created_at DESC, id DESC LIMIT " + fmt.Sprintf("$%d", idx)
	args = append(args, f.Limit)

	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var events []AuditEvent
	var lastID string

	for rows.Next() {
		var evt AuditEvent
		var meta []byte
		if err := rows.Scan(&evt.ID, &evt.EventID, &evt.TenantID, &evt.ActorUserID, &evt.Action, &evt.Result, &evt.CreatedAt, &meta); err != nil {
			return nil, "", err
		}
		if len(meta) > 0 {
			_ = json.Unmarshal(meta, &evt.Metadata)
		}
		events = append(events, evt)
		lastID = evt.ID.String()
	}

	return events, lastID, nil
}

func (s *Service) ExportEvents(ctx context.Context, f AuditFilter, w io.Writer) error {
	q := `SELECT id, event_id, tenant_id, actor_user_id, action, result, created_at, metadata 
	      FROM audit_logs 
	      WHERE tenant_id = $1`
	args := []interface{}{f.TenantID}

	// Streaming without Limit (or Hard Cap)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	enc := json.NewEncoder(w)
	count := 0
	MaxRecords := 10000 // Safety Bound

	for rows.Next() {
		if count >= MaxRecords {
			break
		}
		var evt AuditEvent
		var meta []byte
		rows.Scan(&evt.ID, &evt.EventID, &evt.TenantID, &evt.ActorUserID, &evt.Action, &evt.Result, &evt.CreatedAt, &meta)
		if len(meta) > 0 {
			json.Unmarshal(meta, &evt.Metadata)
		}
		if err := enc.Encode(evt); err != nil {
			return err
		}
		count++
	}
	return nil
}
