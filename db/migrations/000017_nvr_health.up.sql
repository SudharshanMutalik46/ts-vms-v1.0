CREATE TABLE IF NOT EXISTS nvr_health_current (
    tenant_id UUID NOT NULL,
    nvr_id UUID NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('should_be_online', 'online', 'offline', 'auth_failed', 'error')),
    last_checked_at TIMESTAMPTZ NOT NULL,
    last_success_at TIMESTAMPTZ,
    consecutive_failures INT DEFAULT 0,
    last_error_code TEXT,
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (tenant_id, nvr_id)
);

CREATE TABLE IF NOT EXISTS nvr_channel_health_current (
    tenant_id UUID NOT NULL,
    nvr_id UUID NOT NULL,
    channel_id UUID NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('unknown', 'online', 'offline', 'auth_failed', 'stream_error')),
    last_checked_at TIMESTAMPTZ NOT NULL,
    last_success_at TIMESTAMPTZ,
    consecutive_failures INT DEFAULT 0,
    last_error_code TEXT,
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (tenant_id, nvr_id, channel_id)
);

-- Indexes for Dashboard queries
CREATE INDEX IF NOT EXISTS idx_nvr_health_status ON nvr_health_current (tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_nvr_channel_health_nvr ON nvr_channel_health_current (tenant_id, nvr_id);

-- RLS
ALTER TABLE nvr_health_current ENABLE ROW LEVEL SECURITY;
ALTER TABLE nvr_channel_health_current ENABLE ROW LEVEL SECURITY;

CREATE POLICY nvr_health_isolation ON nvr_health_current
    USING (tenant_id = current_setting('app.current_tenant')::uuid);

CREATE POLICY nvr_channel_health_isolation ON nvr_channel_health_current
    USING (tenant_id = current_setting('app.current_tenant')::uuid);
