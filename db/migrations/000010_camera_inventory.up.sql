-- 000010_camera_inventory.up.sql

-- Enable pg_trgm for Full Text Search (Trigram)
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Camera Groups Table (Option 1: Separate Table)
CREATE TABLE IF NOT EXISTS camera_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    site_id UUID REFERENCES sites(id) ON DELETE CASCADE, -- Optional: Groups can be site-specific or tenant-wide
    name VARCHAR(120) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT (NOW() AT TIME ZONE 'UTC'),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT (NOW() AT TIME ZONE 'UTC'),
    UNIQUE(tenant_id, name) -- Unique name per tenant (or per site? Plan didn't specify, likely per tenant is safer default)
);

CREATE INDEX idx_camera_groups_tenant ON camera_groups(tenant_id);
CREATE INDEX idx_camera_groups_site ON camera_groups(site_id);

-- Cameras Table
CREATE TABLE IF NOT EXISTS cameras (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    site_id UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    
    -- Identity & Network
    name VARCHAR(120) NOT NULL,
    ip_address INET,          -- Using INET for strict IP validation. FTS will cast to text.
    port INTEGER,             -- 1..65535
    
    -- Metadata (Optional)
    manufacturer VARCHAR(120),
    model VARCHAR(120),
    serial_number VARCHAR(120),
    mac_address VARCHAR(17),  -- 00:00:00:00:00:00
    
    -- State
    is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    
    -- Tags (Using text[] for simple high-perf filtering)
    tags TEXT[] DEFAULT '{}',
    
    -- Full Text Search Generated Column
    -- Concatenates Name + IP for single-field search.
    -- pg_trgm handles partial matches nicely.
    search_text TEXT GENERATED ALWAYS AS (lower(name) || ' ' || coalesce(host(ip_address), '')) STORED,

    -- Timestamps
    created_at TIMESTAMP WITH TIME ZONE DEFAULT (NOW() AT TIME ZONE 'UTC'),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT (NOW() AT TIME ZONE 'UTC'),
    deleted_at TIMESTAMP WITH TIME ZONE, -- Soft Delete

    -- Constraints
    CONSTRAINT chk_camera_port CHECK (port > 0 AND port <= 65535)
);

-- Indexes for Cameras
CREATE INDEX idx_cameras_tenant_site ON cameras(tenant_id, site_id);
CREATE INDEX idx_cameras_tags ON cameras USING GIN (tags);
CREATE INDEX idx_cameras_search ON cameras USING GIN (search_text gin_trgm_ops);

-- Unique Constraint: Prevent duplicate IP:Port within a Site
CREATE UNIQUE INDEX idx_cameras_unique_net ON cameras(tenant_id, site_id, ip_address, port) WHERE deleted_at IS NULL;

-- Group Members (M:N)
CREATE TABLE IF NOT EXISTS camera_group_members (
    group_id UUID NOT NULL REFERENCES camera_groups(id) ON DELETE CASCADE,
    camera_id UUID NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    joined_at TIMESTAMP WITH TIME ZONE DEFAULT (NOW() AT TIME ZONE 'UTC'),
    PRIMARY KEY (group_id, camera_id)
);

CREATE INDEX idx_group_members_cam ON camera_group_members(camera_id);
