package data

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type HealthModel struct {
	DB *sql.DB
}

func (m *HealthModel) UpsertStatus(ctx context.Context, h *CameraHealthCurrent) error {
	query := `
		INSERT INTO camera_health_current (tenant_id, camera_id, status, last_checked_at, last_success_at, consecutive_failures, last_error_code, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (tenant_id, camera_id) DO UPDATE SET
			status = EXCLUDED.status,
			last_checked_at = EXCLUDED.last_checked_at,
			last_success_at = EXCLUDED.last_success_at,
			consecutive_failures = EXCLUDED.consecutive_failures,
			last_error_code = EXCLUDED.last_error_code,
			updated_at = EXCLUDED.updated_at
	`
	_, err := m.DB.ExecContext(ctx, query, h.TenantID, h.CameraID, h.Status, h.LastCheckedAt, h.LastSuccessAt, h.ConsecutiveFailures, h.LastErrorCode, h.UpdatedAt)
	return err
}

func (m *HealthModel) GetStatus(ctx context.Context, cameraID uuid.UUID) (*CameraHealthCurrent, error) {
	query := `
		SELECT tenant_id, camera_id, status, last_checked_at, last_success_at, consecutive_failures, last_error_code, updated_at
		FROM camera_health_current
		WHERE camera_id = $1
	`
	var h CameraHealthCurrent
	var lastSuccess pq.NullTime
	var lastError sql.NullString

	err := m.DB.QueryRowContext(ctx, query, cameraID).Scan(
		&h.TenantID, &h.CameraID, &h.Status, &h.LastCheckedAt, &lastSuccess, &h.ConsecutiveFailures, &lastError, &h.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil // Return nil if not exists
	}
	if err != nil {
		return nil, err
	}
	if lastSuccess.Valid {
		h.LastSuccessAt = &lastSuccess.Time
	}
	if lastError.Valid {
		h.LastErrorCode = lastError.String
	}
	return &h, nil
}

func (m *HealthModel) AddHistory(ctx context.Context, h *CameraHealthHistory) error {
	query := `
		INSERT INTO camera_health_history (tenant_id, camera_id, occurred_at, status, reason_code, rtt_ms)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := m.DB.ExecContext(ctx, query, h.TenantID, h.CameraID, h.OccurredAt, h.Status, h.ReasonCode, h.RTTMS)
	return err
}

func (m *HealthModel) PruneHistory(ctx context.Context, cameraID uuid.UUID, maxRecords int) error {
	// Delete records that are NOT in the top N newest
	query := `
		DELETE FROM camera_health_history
		WHERE camera_id = $1 AND id NOT IN (
			SELECT id FROM camera_health_history
			WHERE camera_id = $1
			ORDER BY occurred_at DESC
			LIMIT $2
		)
	`
	_, err := m.DB.ExecContext(ctx, query, cameraID, maxRecords)
	return err
}

func (m *HealthModel) GetHistory(ctx context.Context, cameraID uuid.UUID, limit, offset int) ([]*CameraHealthHistory, error) {
	query := `
		SELECT id, tenant_id, camera_id, occurred_at, status, reason_code, rtt_ms
		FROM camera_health_history
		WHERE camera_id = $1
		ORDER BY occurred_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := m.DB.QueryContext(ctx, query, cameraID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []*CameraHealthHistory
	for rows.Next() {
		var h CameraHealthHistory
		var reason sql.NullString
		if err := rows.Scan(&h.ID, &h.TenantID, &h.CameraID, &h.OccurredAt, &h.Status, &reason, &h.RTTMS); err != nil {
			return nil, err
		}
		if reason.Valid {
			h.ReasonCode = reason.String
		}
		history = append(history, &h)
	}
	return history, nil
}

func (m *HealthModel) UpsertAlert(ctx context.Context, a *CameraAlert) error {
	// Insert new alert
	query := `
		INSERT INTO camera_alerts (tenant_id, camera_id, type, state, started_at, ended_at, last_notified_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`
	return m.DB.QueryRowContext(ctx, query, a.TenantID, a.CameraID, a.Type, a.State, a.StartedAt, a.EndedAt, a.LastNotifiedAt).Scan(&a.ID)
}

func (m *HealthModel) GetOpenAlert(ctx context.Context, cameraID uuid.UUID, alertType string) (*CameraAlert, error) {
	query := `
		SELECT id, tenant_id, camera_id, type, state, started_at, ended_at, last_notified_at
		FROM camera_alerts
		WHERE camera_id = $1 AND type = $2 AND state = 'open'
		LIMIT 1
	`
	var a CameraAlert
	var ended, notified pq.NullTime

	err := m.DB.QueryRowContext(ctx, query, cameraID, alertType).Scan(
		&a.ID, &a.TenantID, &a.CameraID, &a.Type, &a.State, &a.StartedAt, &ended, &notified,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if ended.Valid {
		a.EndedAt = &ended.Time
	}
	if notified.Valid {
		a.LastNotifiedAt = &notified.Time
	}
	return &a, nil
}

func (m *HealthModel) CloseAlert(ctx context.Context, alertID uuid.UUID) error {
	query := `
		UPDATE camera_alerts
		SET state = 'closed', ended_at = NOW()
		WHERE id = $1
	`
	_, err := m.DB.ExecContext(ctx, query, alertID)
	return err
}

func (m *HealthModel) ListAlerts(ctx context.Context, tenantID uuid.UUID, state string) ([]*CameraAlert, error) {
	query := `
		SELECT id, tenant_id, camera_id, type, state, started_at, ended_at, last_notified_at
		FROM camera_alerts
		WHERE tenant_id = $1
	`
	args := []interface{}{tenantID}
	if state != "" {
		query += " AND state = $2"
		args = append(args, state)
	}
	query += " ORDER BY started_at DESC LIMIT 50"

	rows, err := m.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []*CameraAlert
	for rows.Next() {
		var a CameraAlert
		var ended, notified pq.NullTime
		if err := rows.Scan(&a.ID, &a.TenantID, &a.CameraID, &a.Type, &a.State, &a.StartedAt, &ended, &notified); err != nil {
			return nil, err
		}
		if ended.Valid {
			a.EndedAt = &ended.Time
		}
		if notified.Valid {
			a.LastNotifiedAt = &notified.Time
		}
		alerts = append(alerts, &a)
	}
	return alerts, nil
}

func (m *HealthModel) ListTargets(ctx context.Context) ([]CameraHealthTarget, error) {
	// Simplified logic: Construct RTSP URL from IP and Port.
	// We ignore camera_stream_selections for now to support Phase 2.1 cameras.
	query := `
		SELECT c.tenant_id, c.id, 
		       format('rtsp://%s:%s/live/stream1', c.ip_address, c.port) as rtsp_url,
		       COALESCE(h.status, 'OFFLINE'), COALESCE(h.last_checked_at, '1970-01-01'), COALESCE(h.consecutive_failures, 0)
		FROM cameras c
		LEFT JOIN camera_health_current h ON c.id = h.camera_id
		WHERE c.status = 'enabled'
	`
	rows, err := m.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var targets []CameraHealthTarget
	for rows.Next() {
		var t CameraHealthTarget
		var url string

		if err := rows.Scan(&t.TenantID, &t.CameraID, &url, &t.Status, &t.LastCheckedAt, &t.ConsecutiveFailures); err != nil {
			return nil, err
		}
		t.RTSPURL = url
		targets = append(targets, t)
	}
	return targets, nil
}

func (m *HealthModel) GetTarget(ctx context.Context, cameraID uuid.UUID) (*CameraHealthTarget, error) {
	query := `
		SELECT c.tenant_id, c.id, 
		       format('rtsp://%s:%s/live/stream1', c.ip_address, c.port) as rtsp_url,
		       COALESCE(h.status, 'OFFLINE'), COALESCE(h.last_checked_at, '1970-01-01'), COALESCE(h.consecutive_failures, 0)
		FROM cameras c
		LEFT JOIN camera_health_current h ON c.id = h.camera_id
		WHERE c.id = $1
	`
	var t CameraHealthTarget
	var url string

	err := m.DB.QueryRowContext(ctx, query, cameraID).Scan(
		&t.TenantID, &t.CameraID, &url, &t.Status, &t.LastCheckedAt, &t.ConsecutiveFailures,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("camera not found")
	}
	if err != nil {
		return nil, err
	}
	t.RTSPURL = url
	return &t, nil
}

func (m *HealthModel) ListStatuses(ctx context.Context, tenantID uuid.UUID) ([]*CameraHealthCurrent, error) {
	query := `
		SELECT tenant_id, camera_id, status, last_checked_at, last_success_at, consecutive_failures, last_error_code, updated_at
		FROM camera_health_current
		WHERE tenant_id = $1
		ORDER BY last_checked_at DESC
	`
	rows, err := m.DB.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var statuses []*CameraHealthCurrent
	for rows.Next() {
		var h CameraHealthCurrent
		var lastSuccess pq.NullTime
		var lastError sql.NullString

		if err := rows.Scan(&h.TenantID, &h.CameraID, &h.Status, &h.LastCheckedAt, &lastSuccess, &h.ConsecutiveFailures, &lastError, &h.UpdatedAt); err != nil {
			return nil, err
		}
		if lastSuccess.Valid {
			h.LastSuccessAt = &lastSuccess.Time
		}
		if lastError.Valid {
			h.LastErrorCode = lastError.String
		}
		statuses = append(statuses, &h)
	}
	return statuses, nil
}
