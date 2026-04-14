package audit

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/google/uuid"
)

func (s *Service) WriteEvent(ctx context.Context, evt AuditEvent) error {
	// Idempotency: If EventID is empty, generate it.
	if evt.EventID == uuid.Nil {
		evt.EventID = uuid.New()
	}

	// Result Normalization (DB Constraint: CHECK (result IN ('success', 'failure')))
	if evt.Result == "fail" {
		evt.Result = "failure"
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
		log.Printf("Audit DB Write Failed (Action: %s, Result: %s): %v. Spooling event %s", evt.Action, evt.Result, err, evt.EventID)
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
	q := `SELECT a.id, a.event_id, a.tenant_id, a.actor_user_id, u.display_name, a.action, a.target_type, a.target_id, a.result, a.created_at, a.metadata, a.client_ip 
	      FROM audit_logs a
	      LEFT JOIN users u ON a.actor_user_id = u.id
	      WHERE a.tenant_id = $1`
	args := []interface{}{f.TenantID}
	idx := 2

	if f.Result != "" {
		q += fmt.Sprintf(" AND a.result = $%d", idx)
		args = append(args, f.Result)
		idx++
	}

	// Cursor (ID based scrolling)
	if f.Cursor != "" {
		q += fmt.Sprintf(" AND a.id < $%d", idx)
		args = append(args, f.Cursor)
		idx++
	}

	q += " ORDER BY a.created_at DESC, a.id DESC LIMIT " + fmt.Sprintf("$%d", idx)
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
		var actorName sql.NullString
		if err := rows.Scan(&evt.ID, &evt.EventID, &evt.TenantID, &evt.ActorUserID, &actorName, &evt.Action, &evt.TargetType, &evt.TargetID, &evt.Result, &evt.CreatedAt, &meta, &evt.ClientIP); err != nil {
			return nil, "", err
		}
		if len(meta) > 0 {
			_ = json.Unmarshal(meta, &evt.Metadata)
		}

		// If ActorUserID is present, use DisplayName if found, else UUID
		if actorName.Valid {
			evt.ActorDisplayName = actorName.String
		}

		events = append(events, evt)
		lastID = evt.ID.String()
	}

	return events, lastID, nil
}

func (s *Service) ExportEvents(ctx context.Context, f AuditFilter, format string, w io.Writer) error {
	q := `SELECT a.id, a.event_id, a.tenant_id, a.actor_user_id, u.display_name, a.action, a.target_type, a.target_id, a.result, a.created_at, a.metadata, a.client_ip 
	      FROM audit_logs a
	      LEFT JOIN users u ON a.actor_user_id = u.id
	      WHERE a.tenant_id = $1`
	args := []interface{}{f.TenantID}
	idx := 2

	// Apply Time Filters
	if f.DateFrom != nil {
		q += fmt.Sprintf(" AND a.created_at >= $%d", idx)
		args = append(args, f.DateFrom)
		idx++
	}
	if f.DateTo != nil {
		q += fmt.Sprintf(" AND a.created_at <= $%d", idx)
		args = append(args, f.DateTo)
		idx++
	}

	q += " ORDER BY a.created_at DESC"

	// Streaming query
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	if format == "csv" {
		cw := csv.NewWriter(w)
		// Header
		if err := cw.Write([]string{"Timestamp", "Event ID", "Actor", "Action", "Resource", "IP Address", "Result", "Metadata"}); err != nil {
			return err
		}
		defer cw.Flush()

		count := 0
		MaxRecords := 50000

		for rows.Next() {
			if count >= MaxRecords {
				break
			}
			var evt AuditEvent
			var meta []byte
			var actorName sql.NullString
			// Use temporary variables for scan
			rows.Scan(&evt.ID, &evt.EventID, &evt.TenantID, &evt.ActorUserID, &actorName, &evt.Action, &evt.TargetType, &evt.TargetID, &evt.Result, &evt.CreatedAt, &meta, &evt.ClientIP)

			// Format Metadata as string
			metaStr := ""
			if len(meta) > 0 {
				metaStr = string(meta)
			}

			actor := "system"
			if actorName.Valid {
				actor = actorName.String
			} else if evt.ActorUserID != nil {
				actor = evt.ActorUserID.String()
			}

			resource := fmt.Sprintf("%s:%s", evt.TargetType, evt.TargetID)

			record := []string{
				evt.CreatedAt.Format(time.RFC3339),
				evt.EventID.String(),
				actor,
				evt.Action,
				resource,
				evt.ClientIP,
				evt.Result,
				metaStr,
			}

			if err := cw.Write(record); err != nil {
				return err
			}
			count++
		}
		return nil
	} else {
		// Default to JSONL
		enc := json.NewEncoder(w)
		count := 0
		MaxRecords := 10000

		for rows.Next() {
			if count >= MaxRecords {
				break
			}
			var evt AuditEvent
			var meta []byte
			var actorName sql.NullString
			rows.Scan(&evt.ID, &evt.EventID, &evt.TenantID, &evt.ActorUserID, &actorName, &evt.Action, &evt.TargetType, &evt.TargetID, &evt.Result, &evt.CreatedAt, &meta, &evt.ClientIP)
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
}
