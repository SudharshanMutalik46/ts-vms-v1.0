-- 000003_refresh_tokens.up.sql

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash VARCHAR(255) NOT NULL UNIQUE, -- SHA256 Hash of the actual token
    issued_at TIMESTAMP WITH TIME ZONE DEFAULT (NOW() AT TIME ZONE 'UTC'),
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    revoked_at TIMESTAMP WITH TIME ZONE,
    replaced_by_token_id UUID REFERENCES refresh_tokens(id) ON DELETE SET NULL, -- Rotation Chain
    last_used_at TIMESTAMP WITH TIME ZONE,
    device_info VARCHAR(255) -- Optional User-Agent/IP hash
);

CREATE INDEX idx_refresh_tokens_user ON refresh_tokens(user_id);
CREATE INDEX idx_refresh_tokens_lookup ON refresh_tokens(token_hash);
