-- 000005_rls_policies.down.sql
DROP POLICY IF EXISTS tenant_isolation_policy ON tenants;
DROP POLICY IF EXISTS site_isolation_policy ON sites;
DROP POLICY IF EXISTS user_isolation_policy ON users;
DROP POLICY IF EXISTS role_isolation_policy ON roles;
DROP POLICY IF EXISTS token_isolation_policy ON refresh_tokens;
DROP POLICY IF EXISTS audit_isolation_policy ON audit_logs;
DROP POLICY IF EXISTS user_role_isolation_policy ON user_roles;

ALTER TABLE tenants DISABLE ROW LEVEL SECURITY;
ALTER TABLE sites DISABLE ROW LEVEL SECURITY;
ALTER TABLE users DISABLE ROW LEVEL SECURITY;
ALTER TABLE roles DISABLE ROW LEVEL SECURITY;
ALTER TABLE refresh_tokens DISABLE ROW LEVEL SECURITY;
ALTER TABLE audit_logs DISABLE ROW LEVEL SECURITY;
ALTER TABLE user_roles DISABLE ROW LEVEL SECURITY;

DROP FUNCTION IF EXISTS set_tenant_context;
