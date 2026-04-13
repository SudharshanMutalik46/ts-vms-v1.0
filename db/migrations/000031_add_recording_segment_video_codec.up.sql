ALTER TABLE recording_segments
    ADD COLUMN IF NOT EXISTS video_codec TEXT NOT NULL DEFAULT '';

UPDATE recording_segments
SET video_codec = COALESCE(NULLIF(video_codec, ''), '');
