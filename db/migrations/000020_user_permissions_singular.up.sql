-- 000020_user_permissions_singular.up.sql

-- 1. Insert Missing Permissions (Singular)
INSERT INTO permissions (name, description) VALUES
    ('user.create', 'Create user'),
    ('user.read', 'Read user details and list'),
    ('user.update', 'Update user'),
    ('user.delete', 'Delete user'),
    ('user.disable', 'Disable/Enable user'),
    ('user.password.reset', 'Reset user password'),
    ('user.role.assign', 'Assign roles to user')
ON CONFLICT (name) DO NOTHING;

-- 2. Assign these permissions to the Admin role for the default tenant
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
            'user.create', 'user.read', 'user.update', 'user.delete', 
            'user.disable', 'user.password.reset', 'user.role.assign'
        )
        ON CONFLICT DO NOTHING;
    END IF;
END $$;
