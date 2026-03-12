ALTER TABLE recording_segments
    ADD COLUMN IF NOT EXISTS container TEXT NOT NULL DEFAULT 'mkv',
    ADD COLUMN IF NOT EXISTS checksum_sha256 TEXT,
    ADD COLUMN IF NOT EXISTS health_state TEXT NOT NULL DEFAULT 'finalized',
    ADD COLUMN IF NOT EXISTS is_missing_on_disk BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS is_corrupt BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS is_finalized BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS last_seen_on_disk TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS quarantine_path TEXT;

CREATE INDEX IF NOT EXISTS idx_recording_segments_camera_time_finalized
    ON recording_segments(camera_id, start_ts, end_ts)
    WHERE COALESCE(is_finalized, TRUE) = TRUE
      AND COALESCE(is_missing_on_disk, FALSE) = FALSE
      AND COALESCE(is_corrupt, FALSE) = FALSE;

CREATE INDEX IF NOT EXISTS idx_recording_segments_path
    ON recording_segments(path);

UPDATE recording_segments
SET container = COALESCE(NULLIF(container, ''), 'mkv'),
    health_state = COALESCE(NULLIF(health_state, ''), 'finalized'),
    is_missing_on_disk = COALESCE(is_missing_on_disk, FALSE),
    is_corrupt = COALESCE(is_corrupt, FALSE),
    is_finalized = COALESCE(is_finalized, TRUE);
