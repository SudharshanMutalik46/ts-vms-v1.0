-- Runs
CREATE TABLE IF NOT EXISTS onvif_discovery_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    site_id UUID, -- Optional scope
    status TEXT NOT NULL DEFAULT 'running', -- running, completed, partially_completed, failed
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ,
    device_count INT DEFAULT 0,
    error_count INT DEFAULT 0,
    
    CONSTRAINT chk_status CHECK (status IN ('running', 'completed', 'partially_completed', 'failed'))
);

CREATE INDEX idx_discovery_runs_tenant_created ON onvif_discovery_runs(tenant_id, started_at DESC);

-- Discovered Devices
CREATE TABLE IF NOT EXISTS onvif_discovered_devices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    discovery_run_id UUID NOT NULL REFERENCES onvif_discovery_runs(id) ON DELETE CASCADE,
    ip_address TEXT NOT NULL,
    xaddrs JSONB NOT NULL DEFAULT '[]',
    endpoint_ref TEXT,
    manufacturer TEXT,
    model TEXT,
    firmware_version TEXT,
    serial_number TEXT,
    
    -- Profile Detection Flags
    supports_profile_s BOOLEAN DEFAULT FALSE,
    supports_profile_t BOOLEAN DEFAULT FALSE,
    supports_profile_g BOOLEAN DEFAULT FALSE,
    
    -- Large Data Caps: JSONB should be truncated app-side, but strict types here
    capabilities JSONB DEFAULT '{}', -- Size capped app side
    media_profiles JSONB DEFAULT '[]', -- Size capped app side
    rtsp_uris JSONB DEFAULT '[]', -- Array of {profile_token, uri} (Creds Stripped)
    
    last_probe_at TIMESTAMPTZ,
    last_error_code TEXT, -- ws_discovery_timeout, onvif_unauthorized, etc
    
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_discovered_devices_run ON onvif_discovered_devices(discovery_run_id);
CREATE INDEX idx_discovered_devices_tenant_ip ON onvif_discovered_devices(tenant_id, ip_address);

-- Bootstrap Credentials (Option A)
-- Structure mirrors camera_credentials but standalone
CREATE TABLE IF NOT EXISTS onvif_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    master_kid TEXT NOT NULL,
    dek_nonce BYTEA NOT NULL,
    dek_ciphertext BYTEA NOT NULL,
    dek_tag BYTEA NOT NULL,
    data_nonce BYTEA NOT NULL,
    data_ciphertext BYTEA NOT NULL,
    data_tag BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_onvif_credentials_tenant ON onvif_credentials(tenant_id);

-- RLS
ALTER TABLE onvif_discovery_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE onvif_discovered_devices ENABLE ROW LEVEL SECURITY;
ALTER TABLE onvif_credentials ENABLE ROW LEVEL SECURITY;

CREATE POLICY onvif_runs_isolation ON onvif_discovery_runs
    USING (tenant_id = current_setting('app.current_tenant')::uuid);

CREATE POLICY onvif_devices_isolation ON onvif_discovered_devices
    USING (tenant_id = current_setting('app.current_tenant')::uuid);

CREATE POLICY onvif_credentials_isolation ON onvif_credentials
    USING (tenant_id = current_setting('app.current_tenant')::uuid);
