-- 000006_seed_data.up.sql

-- 1. Insert Global Permissions (Resource.Action)
INSERT INTO permissions (name, description) VALUES
    ('users.create', 'Create new users'),
    ('users.read', 'View user details'),
    ('users.update', 'Edit user details'),
    ('users.delete', 'Delete users'),
    ('roles.read', 'View roles'),
    ('roles.manage', 'Create/Edit/Delete roles'),
    ('cameras.read', 'View cameras'),
    ('cameras.manage', 'Add/Edit/Delete cameras'),
    ('video.view', 'View live/recorded video'),
    ('audit.read', 'View audit logs')
ON CONFLICT (name) DO NOTHING;

-- 2. Create Bootstrap Tenant (Dev Only)
-- We insert a specific UUID so we can reference it.
-- In production, this might be skipped or handled by an onboarding API.
DO $$
DECLARE
    v_tenant_id UUID := '00000000-0000-0000-0000-000000000001';
    v_admin_role_id UUID;
BEGIN
    -- Only insert if not exists
    IF NOT EXISTS (SELECT 1 FROM tenants WHERE id = v_tenant_id) THEN
        INSERT INTO tenants (id, name) VALUES (v_tenant_id, 'TechnoSupport Dev Tenant');

        -- 3. Create Default System Roles for this Tenant
        INSERT INTO roles (tenant_id, name, is_system) VALUES
            (v_tenant_id, 'Admin', true)
        RETURNING id INTO v_admin_role_id;

        INSERT INTO roles (tenant_id, name, is_system) VALUES
            (v_tenant_id, 'Operator', true);
        
        INSERT INTO roles (tenant_id, name, is_system) VALUES
            (v_tenant_id, 'Viewer', true);

        -- 4. Assign All Permissions to Admin
        INSERT INTO role_permissions (role_id, permission_id)
        SELECT v_admin_role_id, id FROM permissions;

        -- 5. Create Bootstrap Admin User
        -- Password hash is specific to dev (e.g., "admin123" hashed with Argon2id)
        -- For now, use a placeholder that will need reset.
        INSERT INTO users (id, tenant_id, email, display_name, password_hash) VALUES
            ('00000000-0000-0000-0000-000000000002', v_tenant_id, 'admin@technosupport.com', 'System Admin', '$argon2id$v=19$m=65536,t=3,p=2$placeholderhash$placeholderhash');

        -- 6. Assign Admin Role to User
        INSERT INTO user_roles (user_id, role_id, scope_type, scope_id) VALUES
            ('00000000-0000-0000-0000-000000000002', v_admin_role_id, 'tenant', v_tenant_id);
            
    END IF;
END $$;
