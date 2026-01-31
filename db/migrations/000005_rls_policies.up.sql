-- 000005_rls_policies.up.sql

-- Enable RLS on Tables
ALTER TABLE tenants ENABLE ROW LEVEL SECURITY;
ALTER TABLE sites ENABLE ROW LEVEL SECURITY;
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE roles ENABLE ROW LEVEL SECURITY;
ALTER TABLE refresh_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_logs ENABLE ROW LEVEL SECURITY;
-- Permissions are global read-only mostly, but technically could be scoped.
-- Role-Permissions / User-Roles are join tables. Usually we RLS them too if they contain sensitive mapping.
ALTER TABLE user_roles ENABLE ROW LEVEL SECURITY;
-- role_permissions implies access.

-- Policy: Tenants
-- A user can only see their own tenant.
-- Note: This assumes app.tenant_id is set.
CREATE POLICY tenant_isolation_policy ON tenants
    USING (id = current_setting('app.tenant_id', true)::uuid);

-- Policy: Sites
CREATE POLICY site_isolation_policy ON sites
    USING (tenant_id = current_setting('app.tenant_id', true)::uuid);

-- Policy: Users
CREATE POLICY user_isolation_policy ON users
    USING (tenant_id = current_setting('app.tenant_id', true)::uuid);

-- Policy: Roles
CREATE POLICY role_isolation_policy ON roles
    USING (tenant_id = current_setting('app.tenant_id', true)::uuid);

-- Policy: Refresh Tokens
CREATE POLICY token_isolation_policy ON refresh_tokens
    USING (tenant_id = current_setting('app.tenant_id', true)::uuid);

-- Policy: Audit Logs
-- Append only is enforcing via GRANT, but RLS hides other tenants.
CREATE POLICY audit_isolation_policy ON audit_logs
    USING (tenant_id = current_setting('app.tenant_id', true)::uuid);

-- Policy: User Roles
CREATE POLICY user_role_isolation_policy ON user_roles
    USING (user_id IN (SELECT id FROM users WHERE tenant_id = current_setting('app.tenant_id', true)::uuid));

-- Helper function to set tenant
CREATE OR REPLACE FUNCTION set_tenant_context(tenant_id UUID) RETURNS void AS $$
BEGIN
    PERFORM set_config('app.tenant_id', tenant_id::text, false);
END;
$$ LANGUAGE plpgsql SECURITY DEFINER;
