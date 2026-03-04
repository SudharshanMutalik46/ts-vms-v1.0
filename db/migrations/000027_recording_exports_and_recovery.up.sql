ALTER TABLE recording_segments
    ADD COLUMN IF NOT EXISTS is_missing_on_disk BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS quarantine_path TEXT,
    ADD COLUMN IF NOT EXISTS last_seen_on_disk TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT NOW();

ALTER TABLE recording_segments
    ADD COLUMN IF NOT EXISTS is_corrupt BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS recording_exports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    camera_id VARCHAR(255) NOT NULL,
    from_ts TIMESTAMPTZ NOT NULL,
    to_ts TIMESTAMPTZ NOT NULL,
    state VARCHAR(50) NOT NULL,
    output_path TEXT,
    error TEXT,
    requested_by VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS recording_recovery_audit (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    path TEXT NOT NULL,
    state VARCHAR(50) NOT NULL,
    detail TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
