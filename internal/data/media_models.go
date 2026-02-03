package data

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
)

type CameraMediaProfile struct {
	ID               uuid.UUID `json:"id"`
	TenantID         uuid.UUID `json:"tenant_id"`
	CameraID         uuid.UUID `json:"camera_id"`
	ProfileToken     string    `json:"profile_token"`
	ProfileName      string    `json:"profile_name"`
	VideoCodec       string    `json:"video_codec"`
	Width            int       `json:"width"`
	Height           int       `json:"height"`
	FPS              float64   `json:"fps"`
	BitrateKbps      int       `json:"bitrate_kbps"`
	RTSPURLSanitized string    `json:"rtsp_url_sanitized"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type CameraStreamSelection struct {
	ID       uuid.UUID `json:"id"`
	TenantID uuid.UUID `json:"tenant_id"`
	CameraID uuid.UUID `json:"camera_id"`

	MainProfileToken string `json:"main_profile_token"`
	MainRTSP         string `json:"main_rtsp_url_sanitized"`
	MainSupported    bool   `json:"main_supported"`

	SubProfileToken string `json:"sub_profile_token"`
	SubRTSP         string `json:"sub_rtsp_url_sanitized"`
	SubSupported    bool   `json:"sub_supported"`
	SubIsSameAsMain bool   `json:"sub_is_same_as_main"`

	UpdatedAt time.Time `json:"updated_at"`
}

type RTSPValidationResult struct {
	ID            uuid.UUID `json:"id"`
	TenantID      uuid.UUID `json:"tenant_id"`
	CameraID      uuid.UUID `json:"camera_id"`
	Variant       string    `json:"variant"` // main, sub
	Status        string    `json:"status"`
	LastErrorCode string    `json:"last_error_code"`
	RTT           int       `json:"rtt_ms"`
	AttemptCount  int       `json:"attempt_count"`
	ValidatedAt   time.Time `json:"validated_at"`
}

type MediaModel struct {
	DB *sql.DB
}

// Profiles
func (m *MediaModel) UpsertProfile(ctx context.Context, p *CameraMediaProfile) error {
	query := `
		INSERT INTO camera_media_profiles (
			tenant_id, camera_id, profile_token, profile_name,
			video_codec, width, height, fps, bitrate_kbps, rtsp_url_sanitized, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW())
		ON CONFLICT (tenant_id, camera_id, profile_token) DO UPDATE SET
			profile_name=EXCLUDED.profile_name,
			video_codec=EXCLUDED.video_codec,
			width=EXCLUDED.width,
			height=EXCLUDED.height,
			fps=EXCLUDED.fps,
			bitrate_kbps=EXCLUDED.bitrate_kbps,
			rtsp_url_sanitized=EXCLUDED.rtsp_url_sanitized,
			updated_at=NOW()
		RETURNING id
	`
	return m.DB.QueryRowContext(ctx, query,
		p.TenantID, p.CameraID, p.ProfileToken, p.ProfileName,
		p.VideoCodec, p.Width, p.Height, p.FPS, p.BitrateKbps, p.RTSPURLSanitized,
	).Scan(&p.ID)
}

func (m *MediaModel) ListProfiles(ctx context.Context, cameraID uuid.UUID) ([]*CameraMediaProfile, error) {
	query := `
		SELECT id, tenant_id, camera_id, profile_token, profile_name, video_codec, 
		       width, height, fps, bitrate_kbps, rtsp_url_sanitized, updated_at
		FROM camera_media_profiles 
		WHERE camera_id = $1
	`
	rows, err := m.DB.QueryContext(ctx, query, cameraID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*CameraMediaProfile
	for rows.Next() {
		p := &CameraMediaProfile{}
		if err := rows.Scan(
			&p.ID, &p.TenantID, &p.CameraID, &p.ProfileToken, &p.ProfileName, &p.VideoCodec,
			&p.Width, &p.Height, &p.FPS, &p.BitrateKbps, &p.RTSPURLSanitized, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, nil
}

// Selections
func (m *MediaModel) UpsertSelection(ctx context.Context, s *CameraStreamSelection) error {
	query := `
		INSERT INTO camera_stream_selections (
			tenant_id, camera_id, 
			main_profile_token, main_rtsp_url_sanitized, main_supported,
			sub_profile_token, sub_rtsp_url_sanitized, sub_supported, sub_is_same_as_main,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		ON CONFLICT (tenant_id, camera_id) DO UPDATE SET
			main_profile_token=EXCLUDED.main_profile_token,
			main_rtsp_url_sanitized=EXCLUDED.main_rtsp_url_sanitized,
			main_supported=EXCLUDED.main_supported,
			sub_profile_token=EXCLUDED.sub_profile_token,
			sub_rtsp_url_sanitized=EXCLUDED.sub_rtsp_url_sanitized,
			sub_supported=EXCLUDED.sub_supported,
			sub_is_same_as_main=EXCLUDED.sub_is_same_as_main,
			updated_at=NOW()
		RETURNING id
	`
	return m.DB.QueryRowContext(ctx, query,
		s.TenantID, s.CameraID,
		s.MainProfileToken, s.MainRTSP, s.MainSupported,
		s.SubProfileToken, s.SubRTSP, s.SubSupported, s.SubIsSameAsMain,
	).Scan(&s.ID)
}

func (m *MediaModel) GetSelection(ctx context.Context, cameraID uuid.UUID) (*CameraStreamSelection, error) {
	query := `
		SELECT id, tenant_id, camera_id, 
		       main_profile_token, main_rtsp_url_sanitized, main_supported,
		       sub_profile_token, sub_rtsp_url_sanitized, sub_supported, sub_is_same_as_main,
		       updated_at
		FROM camera_stream_selections WHERE camera_id = $1
	`
	s := &CameraStreamSelection{}
	err := m.DB.QueryRowContext(ctx, query, cameraID).Scan(
		&s.ID, &s.TenantID, &s.CameraID,
		&s.MainProfileToken, &s.MainRTSP, &s.MainSupported,
		&s.SubProfileToken, &s.SubRTSP, &s.SubSupported, &s.SubIsSameAsMain,
		&s.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil // Not found is clean nil
	}
	return s, err
}

// Validation
func (m *MediaModel) UpsertValidationResult(ctx context.Context, r *RTSPValidationResult) error {
	query := `
		INSERT INTO rtsp_validation_results (
			tenant_id, camera_id, variant, status, last_error_code, rtt_ms, validated_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (tenant_id, camera_id, variant) DO UPDATE SET
			status=EXCLUDED.status,
			last_error_code=EXCLUDED.last_error_code,
			rtt_ms=EXCLUDED.rtt_ms,
			validated_at=NOW(),
			attempt_count=rtsp_validation_results.attempt_count + 1
		RETURNING id, validated_at
	`
	return m.DB.QueryRowContext(ctx, query,
		r.TenantID, r.CameraID, r.Variant, r.Status, r.LastErrorCode, r.RTT,
	).Scan(&r.ID, &r.ValidatedAt)
}

func (m *MediaModel) GetValidationResults(ctx context.Context, cameraID uuid.UUID) ([]*RTSPValidationResult, error) {
	query := `
		SELECT id, tenant_id, camera_id, variant, status, last_error_code, rtt_ms, attempt_count, validated_at
		FROM rtsp_validation_results WHERE camera_id = $1
	`
	rows, err := m.DB.QueryContext(ctx, query, cameraID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*RTSPValidationResult
	for rows.Next() {
		r := &RTSPValidationResult{}
		rows.Scan(&r.ID, &r.TenantID, &r.CameraID, &r.Variant, &r.Status, &r.LastErrorCode, &r.RTT, &r.AttemptCount, &r.ValidatedAt)
		list = append(list, r)
	}
	return list, nil
}
