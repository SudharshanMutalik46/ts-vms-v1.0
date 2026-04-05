package recording

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/crypto"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/media"
)

var ErrDBUnavailable = errors.New("recording database unavailable")

type PostgresStore struct {
	DB                      *sql.DB
	preferredCodecColKnown  bool
	preferredCodecColExists bool
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
		return media.SanitizeRTSPURL(raw)
	}
	if ip == "" {
		return ""
	}
	return ""
}

type RecordingSource struct {
	ProfileToken string
	RTSPURL      string
	Codec        string
}

func (s *PostgresStore) LoadCameraRecordingSources(ctx context.Context, cameraID, rawURL string) ([]RecordingSource, error) {
	fallbackURL := media.SanitizeRTSPURL(strings.TrimSpace(rawURL))
	fallbackCodec := inferCodecFromRTSPURL(fallbackURL)

	if !s.Available() {
		if fallbackURL == "" {
			return nil, nil
		}
		return []RecordingSource{{RTSPURL: fallbackURL, Codec: fallbackCodec}}, nil
	}

	var mainCodec, subCodec, mainRTSP, subRTSP string
	var mainToken, subToken string
	err := s.DB.QueryRowContext(ctx, `
		SELECT
			COALESCE(s.main_profile_token, ''),
			COALESCE(s.sub_profile_token, ''),
			COALESCE(s.main_rtsp_url_sanitized, ''),
			COALESCE(s.sub_rtsp_url_sanitized, ''),
			COALESCE(mp.video_codec, ''),
			COALESCE(sp.video_codec, '')
		FROM camera_stream_selections s
		LEFT JOIN camera_media_profiles mp ON s.camera_id = mp.camera_id AND s.main_profile_token = mp.profile_token
		LEFT JOIN camera_media_profiles sp ON s.camera_id = sp.camera_id AND s.sub_profile_token = sp.profile_token
		WHERE s.camera_id = $1
		LIMIT 1`, cameraID).Scan(&mainToken, &subToken, &mainRTSP, &subRTSP, &mainCodec, &subCodec)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return []RecordingSource{{RTSPURL: fallbackURL, Codec: fallbackCodec}}, err
	}

	if mediaCodec, codecErr := s.loadPreferredMediaCodec(ctx, cameraID); codecErr == nil && mediaCodec != "" {
		fallbackCodec = mediaCodec
	}

	out := make([]RecordingSource, 0, 3)
	add := func(profileToken, rtspURL, codec string) {
		rtspURL = media.SanitizeRTSPURL(strings.TrimSpace(rtspURL))
		codec = normalizeCodec(codec)
		if codec == "" {
			codec = fallbackCodec
		}
		if rtspURL == "" {
			return
		}
		for _, existing := range out {
			if existing.RTSPURL == rtspURL {
				return
			}
		}
		out = append(out, RecordingSource{ProfileToken: strings.TrimSpace(profileToken), RTSPURL: rtspURL, Codec: codec})
	}

	add(mainToken, mainRTSP, mainCodec)
	add(subToken, subRTSP, subCodec)
	if len(out) == 0 && fallbackURL != "" {
		out = append(out, RecordingSource{RTSPURL: fallbackURL, Codec: fallbackCodec})
	}
	return out, nil
}

func (s *PostgresStore) loadPreferredMediaCodec(ctx context.Context, cameraID string) (string, error) {
	if !s.Available() {
		return "", ErrDBUnavailable
	}

	var codec string
	err := s.DB.QueryRowContext(ctx, `
		SELECT COALESCE(video_codec, '')
		FROM camera_media_profiles
		WHERE camera_id = $1 AND COALESCE(video_codec, '') <> ''
		ORDER BY
			CASE
				WHEN profile_name ILIKE '%main%' THEN 0
				WHEN profile_name ILIKE '%sub%' THEN 1
				ELSE 2
			END,
			bitrate_kbps DESC,
			updated_at DESC
		LIMIT 1`, cameraID).Scan(&codec)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return normalizeCodec(codec), nil
}

func (s *PostgresStore) LoadCameraRecordingSource(ctx context.Context, cameraID, rawURL string) (string, string, error) {
	sources, err := s.LoadCameraRecordingSources(ctx, cameraID, rawURL)
	if len(sources) == 0 {
		return "", "", err
	}
	return sources[0].RTSPURL, sources[0].Codec, err
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
		if preferredCodec, err := s.loadPreferredRecordingCodec(ctx, cam.ID); err == nil {
			cam.PreferredRecordingCodec = preferredCodec
		}
		if sources, err := s.LoadCameraRecordingSources(ctx, cam.ID, cam.RtspURL); err == nil && len(sources) > 0 {
			cam.RtspURL = sources[0].RTSPURL
			if cam.PreferredRecordingCodec == "" && sources[0].Codec != "" {
				cam.Codec = sources[0].Codec
			}
		}
		if cam.Codec == "" {
			cam.Codec = cam.PreferredRecordingCodec
		}
		out = append(out, cam)
	}
	return out, rows.Err()
}

func (s *PostgresStore) UpdatePreferredRecordingCodec(ctx context.Context, cameraID, codec string) error {
	if !s.Available() {
		return ErrDBUnavailable
	}
	exists, err := s.hasPreferredRecordingCodecColumn(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	_, err = s.DB.ExecContext(ctx, `
		UPDATE cameras
		SET preferred_recording_codec = NULLIF($2, ''), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`, cameraID, normalizeCodec(codec))
	return err
}

func (s *PostgresStore) loadPreferredRecordingCodec(ctx context.Context, cameraID string) (string, error) {
	if !s.Available() {
		return "", ErrDBUnavailable
	}
	exists, err := s.hasPreferredRecordingCodecColumn(ctx)
	if err != nil || !exists {
		return "", err
	}

	var codec string
	err = s.DB.QueryRowContext(ctx, `
		SELECT COALESCE(preferred_recording_codec, '')
		FROM cameras
		WHERE id = $1 AND deleted_at IS NULL`, cameraID).Scan(&codec)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return normalizeCodec(codec), nil
}

func (s *PostgresStore) hasPreferredRecordingCodecColumn(ctx context.Context) (bool, error) {
	if !s.Available() {
		return false, ErrDBUnavailable
	}
	if s.preferredCodecColKnown {
		return s.preferredCodecColExists, nil
	}

	var exists bool
	err := s.DB.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_name = 'cameras' AND column_name = 'preferred_recording_codec'
		)`).Scan(&exists)
	if err != nil {
		return false, err
	}
	s.preferredCodecColKnown = true
	s.preferredCodecColExists = exists
	return exists, nil
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

func (s *PostgresStore) AuditRecoveryEvent(ctx context.Context, path, state, detail string) error {
	if !s.Available() {
		return ErrDBUnavailable
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO recording_recovery_audit (path, state, detail, created_at)
		VALUES ($1,$2,$3,NOW())`, path, state, detail)
	return err
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
