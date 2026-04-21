package recording

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (s *PostgresStore) UpsertFinalizedSegment(ctx context.Context, seg *ArchiveSegment) error {
	if !s.Available() {
		return ErrDBUnavailable
	}

	path := seg.FilePath
	if path == "" {
		path = seg.Path
	}

	sizeBytes := seg.FileSize
	if sizeBytes == 0 {
		sizeBytes = seg.SizeBytes
	}

	container := seg.Container
	if container == "" {
		container = "mkv"
	}

	videoCodec := normalizeCodec(seg.VideoCodec)

	healthState := seg.HealthState
	if healthState == "" {
		healthState = "finalized"
	}

	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO recording_segments (
			tenant_id,
			site_id,
			camera_id,
			start_ts,
			end_ts,
			duration_ms,
			path,
			size_bytes,
			container,
			video_codec,
			checksum_sha256,
			health_state,
			is_missing_on_disk,
			is_corrupt,
			is_finalized,
			created_at,
			updated_at,
			last_seen_on_disk
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,FALSE,FALSE,TRUE,NOW(),NOW(),NOW())
		ON CONFLICT (path) DO UPDATE SET
			camera_id          = EXCLUDED.camera_id,
			start_ts           = EXCLUDED.start_ts,
			end_ts             = EXCLUDED.end_ts,
			duration_ms        = EXCLUDED.duration_ms,
			size_bytes         = EXCLUDED.size_bytes,
			container          = EXCLUDED.container,
			video_codec        = EXCLUDED.video_codec,
			checksum_sha256    = EXCLUDED.checksum_sha256,
			health_state       = EXCLUDED.health_state,
			is_missing_on_disk = FALSE,
			is_corrupt         = FALSE,
			is_finalized       = TRUE,
			updated_at         = NOW(),
			last_seen_on_disk  = NOW()
	`, seg.TenantID, seg.SiteID, seg.CameraID, seg.StartTS, seg.EndTS, seg.DurationMs, path, sizeBytes, container, videoCodec, seg.ChecksumSHA256, healthState)
	return err
}

func (s *PostgresStore) GetSegments(ctx context.Context, cameraID string, from, to time.Time) ([]ArchiveSegment, error) {
	if !s.Available() {
		return nil, ErrDBUnavailable
	}

	rows, err := s.DB.QueryContext(ctx, `
		SELECT
			rs.id,
			COALESCE(rs.id::text, ''),
			rs.tenant_id,
			rs.site_id,
			rs.camera_id,
			rs.start_ts,
			rs.end_ts,
			rs.duration_ms,
			rs.path,
			rs.size_bytes,
			COALESCE(rs.container, 'mkv'),
			COALESCE(rs.video_codec, ''),
			COALESCE(rs.checksum_sha256, ''),
			COALESCE(rs.health_state, 'finalized'),
			COALESCE(rs.is_missing_on_disk, FALSE),
			COALESCE(rs.is_corrupt, FALSE),
			EXISTS (
				SELECT 1
				FROM recording_event_segments res
				WHERE res.segment_id = rs.id
			) AS is_protected,
			COALESCE(rs.is_finalized, TRUE) AS is_finalized
		FROM recording_segments rs
		WHERE rs.camera_id = $1
		  AND rs.end_ts > $2
		  AND rs.start_ts < $3
		  AND COALESCE(rs.is_finalized, TRUE) = TRUE
		  AND COALESCE(rs.is_missing_on_disk, FALSE) = FALSE
		  AND COALESCE(rs.is_corrupt, FALSE) = FALSE
		ORDER BY rs.start_ts ASC
	`, cameraID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ArchiveSegment, 0, 64)
	for rows.Next() {
		var seg ArchiveSegment
		if err := rows.Scan(
			&seg.ID,
			&seg.SegmentID,
			&seg.TenantID,
			&seg.SiteID,
			&seg.CameraID,
			&seg.StartTS,
			&seg.EndTS,
			&seg.DurationMs,
			&seg.FilePath,
			&seg.FileSize,
			&seg.Container,
			&seg.VideoCodec,
			&seg.ChecksumSHA256,
			&seg.HealthState,
			&seg.IsMissing,
			&seg.IsCorrupt,
			&seg.IsProtected,
			&seg.Finalized,
		); err != nil {
			return nil, err
		}

		seg.Path = seg.FilePath
		seg.SizeBytes = seg.FileSize
		out = append(out, seg)
	}

	return out, rows.Err()
}

func (s *PostgresStore) GetRecordedCameras(ctx context.Context, tenantID string, from, to time.Time) ([]RecordedCamera, error) {
	if !s.Available() {
		return nil, ErrDBUnavailable
	}

	queryWithTenant := `
		SELECT DISTINCT
			rs.camera_id,
			COALESCE(NULLIF(c.name, ''), 'Deleted Camera (' || LEFT(rs.camera_id, 8) || ')') AS camera_name,
			COALESCE(c.ip_address::text, '') AS ip_address,
			COALESCE(c.model, '') AS model,
			(c.deleted_at IS NOT NULL OR c.id IS NULL) AS is_deleted
		FROM recording_segments rs
		LEFT JOIN cameras c
			ON c.id::text = rs.camera_id
		   AND c.tenant_id::text = $1
		WHERE rs.tenant_id = $1
		  AND rs.end_ts > $2
		  AND rs.start_ts < $3
		  AND COALESCE(rs.is_finalized, TRUE) = TRUE
		  AND COALESCE(rs.is_missing_on_disk, FALSE) = FALSE
		  AND COALESCE(rs.is_corrupt, FALSE) = FALSE
		ORDER BY camera_name ASC
	`

	rows, err := s.DB.QueryContext(ctx, queryWithTenant, tenantID, from, to)
	if err != nil {
		return nil, err
	}

	out := make([]RecordedCamera, 0, 16)
	for rows.Next() {
		var cam RecordedCamera
		if err := rows.Scan(&cam.CameraID, &cam.CameraName, &cam.IPAddress, &cam.Model, &cam.IsDeleted); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, cam)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	// Fallback: some legacy segments may have a tenant value that no longer matches
	// the current auth tenant. If strict query is empty, show cameras that still have
	// finalized segments in the requested window.
	if len(out) > 0 {
		return out, nil
	}

	queryFallback := `
		SELECT DISTINCT
			rs.camera_id,
			COALESCE(NULLIF(c.name, ''), 'Deleted Camera (' || LEFT(rs.camera_id, 8) || ')') AS camera_name,
			COALESCE(c.ip_address::text, '') AS ip_address,
			COALESCE(c.model, '') AS model,
			(c.deleted_at IS NOT NULL OR c.id IS NULL) AS is_deleted
		FROM recording_segments rs
		LEFT JOIN cameras c
			ON c.id::text = rs.camera_id
		WHERE rs.end_ts > $1
		  AND rs.start_ts < $2
		  AND COALESCE(rs.is_finalized, TRUE) = TRUE
		  AND COALESCE(rs.is_missing_on_disk, FALSE) = FALSE
		  AND COALESCE(rs.is_corrupt, FALSE) = FALSE
		ORDER BY camera_name ASC
	`
	rows, err = s.DB.QueryContext(ctx, queryFallback, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out = out[:0]
	for rows.Next() {
		var cam RecordedCamera
		if err := rows.Scan(&cam.CameraID, &cam.CameraName, &cam.IPAddress, &cam.Model, &cam.IsDeleted); err != nil {
			return nil, err
		}
		out = append(out, cam)
	}

	return out, rows.Err()
}

func (s *PostgresStore) GetLatestSegmentEnd(ctx context.Context, cameraID string) (time.Time, error) {
	if !s.Available() {
		return time.Time{}, ErrDBUnavailable
	}

	var endTS time.Time
	err := s.DB.QueryRowContext(ctx, `
		SELECT end_ts
		FROM recording_segments
		WHERE camera_id = $1
		  AND COALESCE(is_finalized, TRUE) = TRUE
		  AND COALESCE(is_missing_on_disk, FALSE) = FALSE
		  AND COALESCE(is_corrupt, FALSE) = FALSE
		ORDER BY end_ts DESC
		LIMIT 1
	`, cameraID).Scan(&endTS)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return endTS, nil
}

func (s *PostgresStore) GetSegmentByPath(ctx context.Context, path string) (*ArchiveSegment, error) {
	if !s.Available() {
		return nil, ErrDBUnavailable
	}

	var seg ArchiveSegment
	err := s.DB.QueryRowContext(ctx, `
		SELECT
			id,
			COALESCE(id::text, ''),
			tenant_id,
			site_id,
			camera_id,
			start_ts,
			end_ts,
			duration_ms,
			path,
			size_bytes,
			COALESCE(container, 'mkv'),
			COALESCE(video_codec, ''),
			COALESCE(checksum_sha256, ''),
			COALESCE(health_state, 'finalized'),
			COALESCE(is_missing_on_disk, FALSE),
			COALESCE(is_corrupt, FALSE),
			COALESCE(is_finalized, TRUE)
		FROM recording_segments
		WHERE path = $1
	`, path).Scan(
		&seg.ID,
		&seg.SegmentID,
		&seg.TenantID,
		&seg.SiteID,
		&seg.CameraID,
		&seg.StartTS,
		&seg.EndTS,
		&seg.DurationMs,
		&seg.FilePath,
		&seg.FileSize,
		&seg.Container,
		&seg.VideoCodec,
		&seg.ChecksumSHA256,
		&seg.HealthState,
		&seg.IsMissing,
		&seg.IsCorrupt,
		&seg.Finalized,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	seg.Path = seg.FilePath
	seg.SizeBytes = seg.FileSize
	return &seg, nil
}

func (s *PostgresStore) MarkMissing(ctx context.Context, path string) error {
	if !s.Available() {
		return ErrDBUnavailable
	}
	_, err := s.DB.ExecContext(ctx, `
		UPDATE recording_segments 
		SET is_missing_on_disk = TRUE, updated_at = NOW() 
		WHERE path = $1`, path)
	return err
}

func (s *PostgresStore) MarkCorrupt(ctx context.Context, path, quarantinePath string) error {
	if !s.Available() {
		return ErrDBUnavailable
	}
	_, err := s.DB.ExecContext(ctx, `
		UPDATE recording_segments 
		SET is_corrupt = TRUE, health_state = 'corrupt', quarantine_path = $2, updated_at = NOW() 
		WHERE path = $1`, path, quarantinePath)
	return err
}

func (s *PostgresStore) ExpectedPathsSince(ctx context.Context, since time.Time) ([]string, error) {
	if !s.Available() {
		return nil, ErrDBUnavailable
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT path 
		FROM recording_segments 
		WHERE (created_at >= $1 OR last_seen_on_disk >= $1)
		  AND is_finalized = TRUE 
		  AND is_missing_on_disk = FALSE`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}
