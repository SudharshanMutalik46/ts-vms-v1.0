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
			checksum_sha256,
			health_state,
			is_missing_on_disk,
			is_corrupt,
			is_finalized,
			created_at,
			updated_at,
			last_seen_on_disk
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,FALSE,FALSE,TRUE,NOW(),NOW(),NOW())
		ON CONFLICT (path) DO UPDATE SET
			start_ts           = EXCLUDED.start_ts,
			end_ts             = EXCLUDED.end_ts,
			duration_ms        = EXCLUDED.duration_ms,
			size_bytes         = EXCLUDED.size_bytes,
			container          = EXCLUDED.container,
			checksum_sha256    = EXCLUDED.checksum_sha256,
			health_state       = EXCLUDED.health_state,
			is_missing_on_disk = FALSE,
			is_corrupt         = FALSE,
			is_finalized       = TRUE,
			updated_at         = NOW(),
			last_seen_on_disk  = NOW()
	`, seg.TenantID, seg.SiteID, seg.CameraID, seg.StartTS, seg.EndTS, seg.DurationMs, path, sizeBytes, container, seg.ChecksumSHA256, healthState)
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
			COALESCE(rs.checksum_sha256, ''),
			COALESCE(rs.health_state, 'finalized'),
			COALESCE(rs.is_missing_on_disk, FALSE),
			COALESCE(rs.is_corrupt, FALSE),
			EXISTS (
				SELECT 1
				FROM recording_event_segments res
				WHERE res.segment_id = rs.id
			) AS is_protected
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
			&seg.ChecksumSHA256,
			&seg.HealthState,
			&seg.IsMissing,
			&seg.IsCorrupt,
			&seg.IsProtected,
		); err != nil {
			return nil, err
		}

		seg.Path = seg.FilePath
		seg.SizeBytes = seg.FileSize
		out = append(out, seg)
	}

	return out, rows.Err()
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
