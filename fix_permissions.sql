DO $$
DECLARE
    tid UUID := '00000000-0000-0000-0000-000000000001';
    uid UUID := '00000000-0000-0000-0000-000000000002'; -- Admin
    rid UUID;
    pid UUID;
BEGIN
    -- 1. Ensure Role
    SELECT id INTO rid FROM roles WHERE tenant_id = tid AND name = 'System Admin';
    IF rid IS NULL THEN
        rid := gen_random_uuid();
        INSERT INTO roles (id, tenant_id, name) VALUES (rid, tid, 'System Admin');
    END IF;

    -- 2. Ensure Permissions
    -- nvr.health.read
    SELECT id INTO pid FROM permissions WHERE name = 'nvr.health.read';
    IF pid IS NULL THEN
        pid := gen_random_uuid();
        INSERT INTO permissions (id, name, description) VALUES (pid, 'nvr.health.read', 'Read Health');
    END IF;
    INSERT INTO role_permissions (role_id, permission_id) VALUES (rid, pid) ON CONFLICT DO NOTHING;

    -- nvr.discovery.read
    SELECT id INTO pid FROM permissions WHERE name = 'nvr.discovery.read';
    IF pid IS NULL THEN pid := gen_random_uuid(); INSERT INTO permissions (id, name, description) VALUES (pid, 'nvr.discovery.read', 'Read Discovery'); END IF;
    INSERT INTO role_permissions (role_id, permission_id) VALUES (rid, pid) ON CONFLICT DO NOTHING;
    
    -- nvr.read (Assuming needed for GetNVR)
    SELECT id INTO pid FROM permissions WHERE name = 'nvr.read';
    IF pid IS NULL THEN pid := gen_random_uuid(); INSERT INTO permissions (id, name, description) VALUES (pid, 'nvr.read', 'Read NVR'); END IF;
    INSERT INTO role_permissions (role_id, permission_id) VALUES (rid, pid) ON CONFLICT DO NOTHING;

    -- 3. Assign Role (Note: created_at / updated_at MIGHT be needed if defined in code but let's assume default or null is allowed if not in schema output?)
    -- Schema output for role_permissions? better safe than sorry, let's assume simple join table.
    -- user_roles (user_id, role_id, scope_type, scope_id)
    INSERT INTO user_roles (user_id, role_id, scope_type, scope_id) VALUES (uid, rid, 'tenant', tid) ON CONFLICT DO NOTHING;
END $$;
