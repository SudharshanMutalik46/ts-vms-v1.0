CREATE TABLE IF NOT EXISTS nvr_event_poll_state (
    tenant_id UUID NOT NULL,
    nvr_id UUID NOT NULL,
    last_success_at TIMESTAMPTZ,
    cursor TEXT,
    since_ts TIMESTAMPTZ,
    consecutive_failures INT DEFAULT 0,
    last_error_code TEXT,
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (tenant_id, nvr_id)
);

CREATE INDEX IF NOT EXISTS idx_nvr_event_poll_updated ON nvr_event_poll_state(updated_at);

-- RLS
ALTER TABLE nvr_event_poll_state ENABLE ROW LEVEL SECURITY;

CREATE POLICY nvr_event_poll_isolation ON nvr_event_poll_state
    USING (tenant_id = current_setting('app.current_tenant')::uuid);
