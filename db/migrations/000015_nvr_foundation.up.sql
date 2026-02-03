CREATE TABLE IF NOT EXISTS nvrs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    site_id UUID NOT NULL REFERENCES sites(id),
    name TEXT NOT NULL CHECK (length(name) <= 120),
    vendor TEXT NOT NULL CHECK (length(vendor) < 50), -- e.g. "hikvision", "dahua", "onvif"
    ip_address INET NOT NULL, -- or TEXT if we want to allow hostnames? Prompt says "inet or validated text". INET is safer for IPs.
    port INT DEFAULT 80, 
    is_enabled BOOLEAN DEFAULT true,
    status TEXT DEFAULT 'unknown' CHECK (status IN ('unknown', 'online', 'offline', 'auth_failed', 'error')),
    last_status_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    
    CONSTRAINT nvrs_tenant_ip_port_key UNIQUE (tenant_id, ip_address, port)
);

CREATE TABLE IF NOT EXISTS camera_nvr_links (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    camera_id UUID NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    nvr_id UUID NOT NULL REFERENCES nvrs(id) ON DELETE CASCADE,
    nvr_channel_ref TEXT, -- e.g. "1", "101", "Camera 1"
    recording_mode TEXT NOT NULL CHECK (recording_mode IN ('vms', 'nvr')),
    is_enabled BOOLEAN DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT camera_nvr_links_camera_key UNIQUE (tenant_id, camera_id) -- One NVR per camera
);

CREATE TABLE IF NOT EXISTS nvr_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    nvr_id UUID NOT NULL REFERENCES nvrs(id) ON DELETE CASCADE,
    master_kid TEXT NOT NULL,
    dek_nonce BYTEA NOT NULL,
    dek_ciphertext BYTEA NOT NULL,
    dek_tag BYTEA NOT NULL,
    data_nonce BYTEA NOT NULL,
    data_ciphertext BYTEA NOT NULL,
    data_tag BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT nvr_credentials_nvr_key UNIQUE (tenant_id, nvr_id) -- One credential set per NVR
);

-- RLS
ALTER TABLE nvrs ENABLE ROW LEVEL SECURITY;
ALTER TABLE camera_nvr_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE nvr_credentials ENABLE ROW LEVEL SECURITY;

-- Policies (Standard Tenant Isolation)
CREATE POLICY nvrs_tenant_isolation ON nvrs
    FOR ALL
    USING (tenant_id = current_setting('app.current_tenant')::uuid);

CREATE POLICY camera_nvr_links_tenant_isolation ON camera_nvr_links
    FOR ALL
    USING (tenant_id = current_setting('app.current_tenant')::uuid);

CREATE POLICY nvr_credentials_tenant_isolation ON nvr_credentials
    FOR ALL
    USING (tenant_id = current_setting('app.current_tenant')::uuid);

-- Indexes
CREATE INDEX idx_nvrs_tenant_site ON nvrs(tenant_id, site_id);
CREATE INDEX idx_links_tenant_nvr ON camera_nvr_links(tenant_id, nvr_id);

-- Permissions (Insert into permissions table logic is usually separate or handled by seeds, 
-- but we can assume seeds handle it or insert here if strictly migration based. 
-- Standard pattern in this project seems to be 'seed_data' migration. 
-- Phase 2.6 requires defining them. I will insert them if not exists to be safe.)

INSERT INTO permissions (name, description) VALUES
('nvr.read', 'View NVRs'),
('nvr.write', 'Create/Update NVRs'),
('nvr.delete', 'Delete NVRs'),
('nvr.link.read', 'View NVR Links'),
('nvr.link.write', 'Manage NVR Links'),
('nvr.credential.read', 'View NVR Credentials'),
('nvr.credential.write', 'Manage NVR Credentials'),
('nvr.credential.delete', 'Delete NVR Credentials')
ON CONFLICT (name) DO NOTHING;

-- Assign to Admin Role (Standard)
-- We need role_id of 'Admin'.
DO $$
DECLARE
    admin_role_id UUID;
    perm RECORD;
BEGIN
    SELECT id INTO admin_role_id FROM roles WHERE name = 'Admin';
    IF admin_role_id IS NOT NULL THEN
        FOR perm IN SELECT id FROM permissions WHERE name LIKE 'nvr.%' LOOP
            INSERT INTO role_permissions (role_id, permission_id) 
            VALUES (admin_role_id, perm.id)
            ON CONFLICT DO NOTHING;
        END LOOP;
    END IF;
END $$;
