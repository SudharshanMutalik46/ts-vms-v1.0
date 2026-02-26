CREATE TABLE IF NOT EXISTS recording_segments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id VARCHAR(255) NOT NULL,
    site_id VARCHAR(255) NOT NULL,
    camera_id VARCHAR(255) NOT NULL,
    start_ts TIMESTAMPTZ NOT NULL,
    end_ts TIMESTAMPTZ NOT NULL,
    duration_ms BIGINT NOT NULL,
    path TEXT NOT NULL UNIQUE,
    size_bytes BIGINT NOT NULL,
    is_corrupt BOOLEAN DEFAULT false,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_recording_segments_cam_start ON recording_segments(camera_id, start_ts);
CREATE INDEX IF NOT EXISTS idx_recording_segments_tenant_site ON recording_segments(tenant_id, site_id, start_ts);

CREATE TABLE IF NOT EXISTS recording_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id VARCHAR(255) NOT NULL,
    site_id VARCHAR(255) NOT NULL,
    camera_id VARCHAR(255) NOT NULL,
    event_type VARCHAR(50) NOT NULL,
    event_ts TIMESTAMPTZ NOT NULL,
    payload JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS recording_event_segments (
    event_id UUID REFERENCES recording_events(id) ON DELETE CASCADE,
    segment_id UUID REFERENCES recording_segments(id) ON DELETE CASCADE,
    PRIMARY KEY (event_id, segment_id)
);
