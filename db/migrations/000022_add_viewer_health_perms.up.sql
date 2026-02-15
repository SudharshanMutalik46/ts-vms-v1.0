-- 000022_add_viewer_health_perms.up.sql

DO $$
DECLARE
    v_tenant_id UUID := '00000000-0000-0000-0000-000000000001';
    v_view_role_id UUID;
BEGIN
    SELECT id INTO v_view_role_id FROM roles WHERE tenant_id = v_tenant_id AND name = 'Viewer';

    IF v_view_role_id IS NOT NULL THEN
        INSERT INTO role_permissions (role_id, permission_id)
        SELECT v_view_role_id, id FROM permissions
        WHERE name IN (
            'camera.health.read'
        )
        ON CONFLICT DO NOTHING;
    END IF;
END $$;
