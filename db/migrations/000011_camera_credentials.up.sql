CREATE TABLE IF NOT EXISTS camera_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    camera_id UUID NOT NULL,
    master_kid TEXT NOT NULL,
    dek_nonce BYTEA NOT NULL,
    dek_ciphertext BYTEA NOT NULL,
    dek_tag BYTEA NOT NULL,
    data_nonce BYTEA NOT NULL,
    data_ciphertext BYTEA NOT NULL,
    data_tag BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT fk_camera FOREIGN KEY (camera_id) REFERENCES cameras(id) ON DELETE CASCADE,
    CONSTRAINT uq_camera_credentials_camera_id UNIQUE (camera_id)
);

CREATE INDEX idx_camera_credentials_tenant_id ON camera_credentials(tenant_id);

-- RLS
ALTER TABLE camera_credentials ENABLE ROW LEVEL SECURITY;

CREATE POLICY camera_credentials_tenant_isolation ON camera_credentials
    USING (tenant_id = current_setting('app.current_tenant')::uuid);
