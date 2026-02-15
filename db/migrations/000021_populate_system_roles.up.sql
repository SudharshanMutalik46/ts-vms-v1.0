-- 000021_populate_system_roles.up.sql

DO $$
DECLARE
    v_tenant_id UUID := '00000000-0000-0000-0000-000000000001';
    v_op_role_id UUID;
    v_view_role_id UUID;
BEGIN
    -- 1. Get Role IDs
    SELECT id INTO v_op_role_id FROM roles WHERE tenant_id = v_tenant_id AND name = 'Operator';
    SELECT id INTO v_view_role_id FROM roles WHERE tenant_id = v_tenant_id AND name = 'Viewer';

    -- 2. Populate Operator Role
    IF v_op_role_id IS NOT NULL THEN
        INSERT INTO role_permissions (role_id, permission_id)
        SELECT v_op_role_id, id FROM permissions
        WHERE name IN (
            'cameras.read',
            'video.view',
            'nvr.read',
            'audit.read',
            'camera.view',
            'camera.media.read',
            'camera.health.read'
        )
        ON CONFLICT DO NOTHING;
    END IF;

    -- 3. Populate Viewer Role
    IF v_view_role_id IS NOT NULL THEN
        INSERT INTO role_permissions (role_id, permission_id)
        SELECT v_view_role_id, id FROM permissions
        WHERE name IN (
            'cameras.read',
            'video.view',
            'camera.view'
        )
        ON CONFLICT DO NOTHING;
    END IF;
END $$;
