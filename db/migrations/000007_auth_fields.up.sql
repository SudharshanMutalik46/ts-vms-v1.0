-- 000007_auth_fields.up.sql

-- Users Table Updates
ALTER TABLE users 
ADD COLUMN IF NOT EXISTS password_hash TEXT,
ADD COLUMN IF NOT EXISTS password_algo VARCHAR(50) DEFAULT 'argon2id',
ADD COLUMN IF NOT EXISTS password_updated_at TIMESTAMP WITH TIME ZONE DEFAULT (NOW() AT TIME ZONE 'UTC');

-- Refresh Tokens Table Updates
ALTER TABLE refresh_tokens
ADD COLUMN IF NOT EXISTS session_id UUID;

-- We don't enforce foreign key on session_id to Redis (obviously), 
-- but we might want to index it if we query by session_id often (e.g. detailed logout).
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_session_id ON refresh_tokens(session_id);
