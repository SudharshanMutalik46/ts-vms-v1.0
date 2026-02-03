-- NVR Channels Table (Phase 2.8)
CREATE TABLE IF NOT EXISTS nvr_channels (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    site_id UUID NOT NULL,
    nvr_id UUID NOT NULL REFERENCES nvrs(id) ON DELETE CASCADE,
    channel_ref TEXT NOT NULL,
    name TEXT NOT NULL,
    is_enabled BOOLEAN DEFAULT TRUE,
    supports_substream BOOLEAN,
    rtsp_main_url_sanitized TEXT,
    rtsp_sub_url_sanitized TEXT,
    
    -- Metadata
    discovered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_synced_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Validation Status
    validation_status TEXT NOT NULL DEFAULT 'unknown', -- 'ok', 'unauthorized', 'timeout', 'error'
    last_validation_at TIMESTAMPTZ,
    last_error_code TEXT,

    -- Provisioning State
    provision_state TEXT NOT NULL DEFAULT 'not_created', -- 'not_created', 'created'
    
    metadata JSONB DEFAULT '{}'::jsonb,
    
    -- Uniqueness: One record per channel ref per NVR
    UNIQUE(tenant_id, nvr_id, channel_ref)
);

-- RLS
ALTER TABLE nvr_channels ENABLE ROW LEVEL SECURITY;

CREATE POLICY nvr_channels_isolation_policy ON nvr_channels
    USING (tenant_id = current_setting('app.current_tenant')::uuid);

-- Update Links to support Channel ID FK (Stronger Link)
ALTER TABLE camera_nvr_links
ADD COLUMN IF NOT EXISTS nvr_channel_id UUID REFERENCES nvr_channels(id) ON DELETE SET NULL;

-- Index for fast lookup by NVR
CREATE INDEX IF NOT EXISTS idx_nvr_channels_nvr_id ON nvr_channels(nvr_id);
-- Index for discovery runs (sync jobs)
CREATE INDEX IF NOT EXISTS idx_nvr_channels_sync ON nvr_channels(tenant_id, nvr_id, last_synced_at);
