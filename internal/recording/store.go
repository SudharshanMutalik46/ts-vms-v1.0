package recording

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/crypto"
	"github.com/technosupport/ts-vms/internal/data"
)

var ErrDBUnavailable = errors.New("recording database unavailable")

type PostgresStore struct {
	DB *sql.DB
}

type Segment struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id"`
	SiteID     string    `json:"site_id"`
	CameraID   string    `json:"camera_id"`
	StartTS    time.Time `json:"start_ts"`
	EndTS      time.Time `json:"end_ts"`
	DurationMs int64     `json:"duration_ms"`
	Path       string    `json:"path"`
	SizeBytes  int64     `json:"size_bytes"`
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{DB: db}
}

func (s *PostgresStore) Available() bool {
	return s != nil && s.DB != nil
}

func (s *PostgresStore) PingContext(ctx context.Context) error {
	if !s.Available() {
		return ErrDBUnavailable
	}
	return s.DB.PingContext(ctx)
}

func (s *PostgresStore) LoadSchedules(ctx context.Context) ([]ScheduleConfig, error) {
	if !s.Available() {
		return nil, ErrDBUnavailable
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT camera_id, schedule_type, COALESCE(days, '[]'::jsonb), COALESCE(start_time, ''), COALESCE(end_time, '')
		FROM recording_schedules
		ORDER BY camera_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ScheduleConfig
	for rows.Next() {
		var cfg ScheduleConfig
		var raw []byte
		if err := rows.Scan(&cfg.CameraID, &cfg.Type, &raw, &cfg.StartTime, &cfg.EndTime); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &cfg.Days)
		out = append(out, cfg)
	}
	return out, rows.Err()
}

func recordingRTSPURL(raw, ip string, port int) string {
	if raw != "" {
		return raw
	}
	if ip == "" {
		return ""
	}
	if port <= 0 {
		port = 554
	}
	return fmt.Sprintf("rtsp://%s:%d/live/0/MAIN", ip, port)
}

func (s *PostgresStore) LoadEnabledCameras(ctx context.Context) ([]CameraConfig, error) {
	if !s.Available() {
		return nil, ErrDBUnavailable
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id::text, COALESCE(rtsp_url, ''), COALESCE(ip_address::text, ''), COALESCE(port, 0)
		FROM cameras
		WHERE deleted_at IS NULL AND is_enabled = TRUE
		ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]CameraConfig, 0, 32)
	for rows.Next() {
		var cam CameraConfig
		var rawURL, ip string
		var port int
		if err := rows.Scan(&cam.ID, &rawURL, &ip, &port); err != nil {
			return nil, err
		}
		cam.RtspURL = recordingRTSPURL(rawURL, ip, port)
		cam.Enabled = true
		if cam.ID == "" || cam.RtspURL == "" {
			continue
		}
		out = append(out, cam)
	}
	return out, rows.Err()
}

func (s *PostgresStore) GetCredentials(ctx context.Context, cameraID string) (*data.CameraCredential, error) {
	if !s.Available() {
		return nil, ErrDBUnavailable
	}

	uid, err := uuid.Parse(cameraID)
	if err != nil {
		return nil, fmt.Errorf("invalid camera id: %w", err)
	}

	query := `
		SELECT id, tenant_id, camera_id, master_kid, 
		       dek_nonce, dek_ciphertext, dek_tag, 
		       data_nonce, data_ciphertext, data_tag
		FROM camera_credentials
		WHERE camera_id = $1
	`
	var c data.CameraCredential
	err = s.DB.QueryRowContext(ctx, query, uid).Scan(
		&c.ID, &c.TenantID, &c.CameraID, &c.MasterKID,
		&c.DEKNonce, &c.DEKCiphertext, &c.DEKTag,
		&c.DataNonce, &c.DataCiphertext, &c.DataTag,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // Not found is not an error here, just means no auth
		}
		return nil, err
	}
	return &c, nil
}

func (s *PostgresStore) DecryptCredentials(cred *data.CameraCredential, keyring *crypto.Keyring) (string, string, error) {
	// Construct the same AAD as used in cameras/credentials_service.go
	// Format: "tenant_uuid:camera_uuid:purpose"
	aad := []byte(fmt.Sprintf("%s:%s:camera_credential_v1", cred.TenantID.String(), cred.CameraID.String()))

	// 1. Unwrap DEK
	dek, err := keyring.UnwrapDEK(cred.MasterKID, cred.DEKNonce, cred.DEKCiphertext, cred.DEKTag, aad)
	if err != nil {
		return "", "", fmt.Errorf("failed to unwrap DEK: %w", err)
	}

	// 2. Decrypt Data
	plaintext, err := crypto.DecryptGCM(dek, cred.DataNonce, cred.DataCiphertext, cred.DataTag, aad)
	if err != nil {
		return "", "", fmt.Errorf("failed to decrypt credentials data: %w", err)
	}

	// 3. Parse JSON (Username, Password)
	var creds struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(plaintext, &creds); err != nil {
		return "", "", fmt.Errorf("failed to parse decrypted credentials JSON: %w", err)
	}

	return creds.Username, creds.Password, nil
}

func (s *PostgresStore) SaveSchedule(ctx context.Context, tenantID, siteID string, cfg ScheduleConfig) error {
	if !s.Available() {
		return ErrDBUnavailable
	}
	raw, err := json.Marshal(cfg.Days)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO recording_schedules (tenant_id, site_id, camera_id, schedule_type, days, start_time, end_time, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())
		ON CONFLICT (camera_id) DO UPDATE SET
			schedule_type = EXCLUDED.schedule_type,
			days          = EXCLUDED.days,
			start_time    = EXCLUDED.start_time,
			end_time      = EXCLUDED.end_time,
			updated_at    = NOW()
	`, tenantID, siteID, cfg.CameraID, cfg.Type, raw, nullIfEmpty(cfg.StartTime), nullIfEmpty(cfg.EndTime))
	return err
}

func (s *PostgresStore) FindSegmentByPath(ctx context.Context, path string) (*Segment, error) {
	if !s.Available() {
		return nil, ErrDBUnavailable
	}
	var seg Segment
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, tenant_id, site_id, camera_id, start_ts, end_ts, duration_ms, path, size_bytes
		FROM recording_segments
		WHERE path = $1`, path).
		Scan(&seg.ID, &seg.TenantID, &seg.SiteID, &seg.CameraID, &seg.StartTS, &seg.EndTS, &seg.DurationMs, &seg.Path, &seg.SizeBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &seg, nil
}

func (s *PostgresStore) UpsertSegmentFromDisk(ctx context.Context, seg *Segment) error {
	if !s.Available() {
		return ErrDBUnavailable
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO recording_segments (tenant_id, site_id, camera_id, start_ts, end_ts, duration_ms, path, size_bytes, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NOW())
		ON CONFLICT (path) DO UPDATE SET
			start_ts         = EXCLUDED.start_ts,
			end_ts           = EXCLUDED.end_ts,
			duration_ms      = EXCLUDED.duration_ms,
			size_bytes       = EXCLUDED.size_bytes,
			last_seen_on_disk = NOW(),
			is_missing_on_disk = FALSE
	`, seg.TenantID, seg.SiteID, seg.CameraID, seg.StartTS, seg.EndTS, seg.DurationMs, seg.Path, seg.SizeBytes)
	return err
}

func (s *PostgresStore) MarkMissing(ctx context.Context, path string) error {
	if !s.Available() {
		return ErrDBUnavailable
	}
	_, err := s.DB.ExecContext(ctx, `
		UPDATE recording_segments
		SET is_missing_on_disk = TRUE, updated_at = NOW()
		WHERE path = $1`, path)
	if err == nil {
		_ = s.AuditRecoveryEvent(ctx, path, "missing_on_disk", "")
	}
	return err
}

func (s *PostgresStore) MarkCorrupt(ctx context.Context, path, quarantinePath string) error {
	if !s.Available() {
		return ErrDBUnavailable
	}
	_, err := s.DB.ExecContext(ctx, `
		UPDATE recording_segments
		SET is_corrupt = TRUE, quarantine_path = $2, updated_at = NOW()
		WHERE path = $1`, path, nullIfEmpty(quarantinePath))
	if err == nil {
		_ = s.AuditRecoveryEvent(ctx, path, "corrupt", quarantinePath)
	}
	return err
}

func (s *PostgresStore) ExpectedPathsSince(ctx context.Context, since time.Time) ([]string, error) {
	if !s.Available() {
		return nil, ErrDBUnavailable
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT path
		FROM recording_segments
		WHERE end_ts >= $1`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *PostgresStore) GetSegments(ctx context.Context, cameraID string, from, to time.Time) ([]Segment, error) {
	if !s.Available() {
		return nil, ErrDBUnavailable
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, tenant_id, site_id, camera_id, start_ts, end_ts, duration_ms, path, size_bytes
		FROM recording_segments
		WHERE camera_id = $1
		  AND end_ts > $2
		  AND start_ts < $3
		  AND is_missing_on_disk = FALSE
		  AND is_corrupt = FALSE
		ORDER BY start_ts ASC`, cameraID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Segment
	for rows.Next() {
		var seg Segment
		if err := rows.Scan(&seg.ID, &seg.TenantID, &seg.SiteID, &seg.CameraID, &seg.StartTS, &seg.EndTS, &seg.DurationMs, &seg.Path, &seg.SizeBytes); err != nil {
			return nil, err
		}
		out = append(out, seg)
	}
	return out, rows.Err()
}

func (s *PostgresStore) AuditRecoveryEvent(ctx context.Context, path, state, detail string) error {
	if !s.Available() {
		return ErrDBUnavailable
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO recording_recovery_audit (path, state, detail, created_at)
		VALUES ($1,$2,$3,NOW())`, path, state, detail)
	return err
}

type ExportJob struct {
	ID         string    `json:"id"`
	CameraID   string    `json:"camera_id"`
	FromTS     time.Time `json:"from_ts"`
	ToTS       time.Time `json:"to_ts"`
	State      string    `json:"state"`
	OutputPath string    `json:"output_path"`
	Error      string    `json:"error,omitempty"`
}

func (s *PostgresStore) CreateExportJob(ctx context.Context, job *ExportJob, requestedBy string) error {
	if !s.Available() {
		return ErrDBUnavailable
	}
	return s.DB.QueryRowContext(ctx, `
		INSERT INTO recording_exports (camera_id, from_ts, to_ts, state, output_path, requested_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,NOW(),NOW())
		RETURNING id`,
		job.CameraID, job.FromTS, job.ToTS, job.State, nullIfEmpty(job.OutputPath), requestedBy).
		Scan(&job.ID)
}

func (s *PostgresStore) UpdateExportJob(ctx context.Context, job *ExportJob) error {
	if !s.Available() {
		return ErrDBUnavailable
	}
	_, err := s.DB.ExecContext(ctx, `
		UPDATE recording_exports
		SET state = $2, output_path = $3, error = $4, updated_at = NOW()
		WHERE id = $1`,
		job.ID, job.State, nullIfEmpty(job.OutputPath), nullIfEmpty(job.Error))
	return err
}

func (s *PostgresStore) GetExportJob(ctx context.Context, id string) (*ExportJob, error) {
	if !s.Available() {
		return nil, ErrDBUnavailable
	}
	var job ExportJob
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, camera_id, from_ts, to_ts, state, COALESCE(output_path,''), COALESCE(error,'')
		FROM recording_exports
		WHERE id = $1`, id).
		Scan(&job.ID, &job.CameraID, &job.FromTS, &job.ToTS, &job.State, &job.OutputPath, &job.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (s *PostgresStore) OpenExportArtifact(ctx context.Context, id string) (*os.File, *ExportJob, error) {
	if !s.Available() {
		return nil, nil, ErrDBUnavailable
	}
	job, err := s.GetExportJob(ctx, id)
	if err != nil || job == nil {
		return nil, job, err
	}
	f, err := os.Open(filepath.Clean(job.OutputPath))
	return f, job, err
}

func (s *PostgresStore) CreateEvent(ctx context.Context, ev *Event) error {
	if !s.Available() {
		return ErrDBUnavailable
	}
	return s.DB.QueryRowContext(ctx, `
		INSERT INTO recording_events (camera_id, event_ts, type, data, created_at)
		VALUES ($1, $2, $3, $4, NOW())
		RETURNING id`, ev.CameraID, ev.EventTS, ev.Type, ev.Data).Scan(&ev.ID)
}

func (s *PostgresStore) LinkSegmentToEvent(ctx context.Context, eventID, segmentID string) error {
	if !s.Available() {
		return ErrDBUnavailable
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO event_segments (event_id, segment_id, created_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT DO NOTHING`, eventID, segmentID)
	return err
}

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}
