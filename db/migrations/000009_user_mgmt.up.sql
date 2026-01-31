-- 000009_user_mgmt.up.sql

-- 1. Soft Delete for Users
ALTER TABLE users ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP WITH TIME ZONE NULL;

-- Update Unique Constraint to be partial (allow re-use of email if previous is deleted)
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_tenant_id_email_key;
DROP INDEX IF EXISTS idx_users_tenant_email; -- Dropping old index if it exists

-- Create Partial Unique Index
CREATE UNIQUE INDEX idx_users_tenant_email_active 
ON users (tenant_id, email) 
WHERE deleted_at IS NULL;

-- 2. Password Reset Tokens
CREATE TABLE IF NOT EXISTS password_reset_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash VARCHAR(255) NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    used_at TIMESTAMP WITH TIME ZONE NULL,
    created_by_user_id UUID, -- Nullable if system initiated? Or enforce admin ID.
    created_at TIMESTAMP WITH TIME ZONE DEFAULT (NOW() AT TIME ZONE 'UTC'),
    UNIQUE(token_hash)
);

CREATE INDEX idx_pwd_tokens_tenant_user ON password_reset_tokens(tenant_id, user_id);
CREATE INDEX idx_pwd_tokens_expires ON password_reset_tokens(expires_at);

-- 3. RLS for Password Reset Tokens
ALTER TABLE password_reset_tokens ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_policy ON password_reset_tokens
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

-- 4. Audit Support (if needed, usually audit_logs is central)
-- No schema changes needed for audit, just usage.
