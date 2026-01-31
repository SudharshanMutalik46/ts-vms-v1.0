-- 000007_auth_fields.down.sql

ALTER TABLE users 
DROP COLUMN IF EXISTS password_hash,
DROP COLUMN IF EXISTS password_algo,
DROP COLUMN IF EXISTS password_updated_at;

ALTER TABLE refresh_tokens
DROP COLUMN IF EXISTS session_id;
