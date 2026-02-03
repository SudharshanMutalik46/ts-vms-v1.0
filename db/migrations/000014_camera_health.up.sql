CREATE TYPE camera_health_status AS ENUM ('ONLINE', 'OFFLINE', 'AUTH_FAILED', 'STREAM_ERROR');

CREATE TABLE IF NOT EXISTS camera_health_current (
    tenant_id UUID NOT NULL,
    camera_id UUID NOT NULL,
    status camera_health_status NOT NULL DEFAULT 'OFFLINE',
    last_checked_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    last_success_at TIMESTAMP WITH TIME ZONE,
    consecutive_failures INT NOT NULL DEFAULT 0,
    last_error_code VARCHAR(50),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    PRIMARY KEY (tenant_id, camera_id),
    CONSTRAINT fk_camera FOREIGN KEY (camera_id) REFERENCES cameras(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS camera_health_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    camera_id UUID NOT NULL,
    occurred_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    status camera_health_status NOT NULL,
    reason_code VARCHAR(50),
    rtt_ms INT,
    CONSTRAINT fk_camera_history FOREIGN KEY (camera_id) REFERENCES cameras(id) ON DELETE CASCADE
);

-- Index for history retention cleanup and querying
CREATE INDEX idx_health_history_camera_date ON camera_health_history(camera_id, occurred_at DESC);

CREATE TABLE IF NOT EXISTS camera_alerts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    camera_id UUID NOT NULL,
    type VARCHAR(50) NOT NULL, -- 'offline_over_5m'
    state VARCHAR(20) NOT NULL DEFAULT 'open', -- 'open', 'closed'
    started_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    ended_at TIMESTAMP WITH TIME ZONE,
    last_notified_at TIMESTAMP WITH TIME ZONE,
    CONSTRAINT fk_camera_alerts FOREIGN KEY (camera_id) REFERENCES cameras(id) ON DELETE CASCADE
);

-- RLS Policies
ALTER TABLE camera_health_current ENABLE ROW LEVEL SECURITY;
ALTER TABLE camera_health_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE camera_alerts ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_health_current ON camera_health_current
    USING (tenant_id = current_setting('app.current_tenant')::uuid);

CREATE POLICY tenant_isolation_health_history ON camera_health_history
    USING (tenant_id = current_setting('app.current_tenant')::uuid);

CREATE POLICY tenant_isolation_alerts ON camera_alerts
    USING (tenant_id = current_setting('app.current_tenant')::uuid);
