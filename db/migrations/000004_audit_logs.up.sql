-- 000004_audit_logs.up.sql

CREATE TABLE IF NOT EXISTS audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE, -- Tenant Isolation
    actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL, -- Nullable for System Actions
    action VARCHAR(100) NOT NULL, -- resource.action or system.event
    target_type VARCHAR(50),
    target_id VARCHAR(100), -- UUID as string usually
    result VARCHAR(20) NOT NULL CHECK (result IN ('success', 'failure')),
    reason_code VARCHAR(50),
    request_id VARCHAR(100),
    client_ip VARCHAR(50),
    user_agent VARCHAR(255),
    metadata JSONB, -- Flexible additional context
    created_at TIMESTAMP WITH TIME ZONE DEFAULT (NOW() AT TIME ZONE 'UTC')
);

CREATE INDEX idx_audit_tenant_time ON audit_logs(tenant_id, created_at DESC);
CREATE INDEX idx_audit_actor ON audit_logs(actor_user_id);
