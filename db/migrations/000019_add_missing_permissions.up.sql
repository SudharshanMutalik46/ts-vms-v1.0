
-- 000007_add_missing_permissions.up.sql

-- 1. Insert Missing Permissions
INSERT INTO permissions (name, description) VALUES
    ('license.read', 'View license status'),
    ('license.manage', 'Manage license (upload/reload)'),
    ('users.manage', 'Manage users (disable/enable/reset status)'),
    ('user.create', 'Create single user (Alias)'),
    ('user.read', 'Read single user (Alias)'),
    ('user.disable', 'Disable user (Alias)'),
    ('user.password.reset', 'Reset password (Alias)'),
    ('user.role.assign', 'Assign role (Alias)')
ON CONFLICT (name) DO NOTHING;

-- 2. Assign New Permissions to Admin Role
DO $$
DECLARE
    v_tenant_id UUID := '00000000-0000-0000-0000-000000000001';
    v_admin_role_id UUID;
BEGIN
    SELECT id INTO v_admin_role_id FROM roles WHERE tenant_id = v_tenant_id AND name = 'Admin';

    IF v_admin_role_id IS NOT NULL THEN
        INSERT INTO role_permissions (role_id, permission_id)
        SELECT v_admin_role_id, id FROM permissions
        WHERE name IN (
            'license.read', 'license.manage', 'users.manage',
            'user.create', 'user.read', 'user.disable', 'user.password.reset', 'user.role.assign'
        )
        ON CONFLICT DO NOTHING;
    END IF;
END $$;
