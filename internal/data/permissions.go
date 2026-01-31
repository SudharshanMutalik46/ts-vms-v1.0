package data

import (
	"context"
	"database/sql"
)

// PermissionGrant defines the scope of a permission
type PermissionGrant struct {
	TenantWide bool
	SiteIDs    map[string]struct{}
}

type PermissionModel struct {
	DB DBTX
}

// GetPermissionsForUser retrieves all permissions for a user within a tenant.
// It resolves roles -> permissions and aggregates scopes.
func (m PermissionModel) GetPermissionsForUser(ctx context.Context, tenantID, userID string) (map[string]PermissionGrant, error) {
	// Query joins: user_roles -> roles -> role_permissions -> permissions
	// We need to fetch: permission slug, role scope (tenant-wide or specific sites)
	// Note: In strict RBAC, user_roles usually has (user_id, role_id, tenant_id, site_id).
	// If site_id is NULL, it's a tenant-wide role (if allowed for that role type).

	// Assumption based on Prompt 1.3 "User Context Injection":
	// "Roles are assigned via user_roles with scope: tenant scope OR site scope"

	query := `
		SELECT 
			p.slug,
			ur.site_id
		FROM user_roles ur
		JOIN roles r ON ur.role_id = r.id
		JOIN role_permissions rp ON r.id = rp.role_id
		JOIN permissions p ON rp.permission_id = p.id
		WHERE ur.user_id = $1 
		  AND ur.tenant_id = $2
	`

	_ = m.DB.QueryRowContext(ctx, "SELECT set_tenant_context($1)", tenantID) // Ensure context?
	// Actually, usually we set context on the Tx before calling this.
	// But `GetPermissionsForUser` takes tenantID explicitly.
	// Let's assume the caller handles `set_tenant_context` OR we filter by tenant_id in WHERE clause (which we do).
	// Safe to just run the query since we filter `ur.tenant_id = $2`.

	rows, err := m.DB.QueryContext(ctx, query, userID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	perms := make(map[string]PermissionGrant)

	for rows.Next() {
		var slug string
		var siteID sql.NullString

		if err := rows.Scan(&slug, &siteID); err != nil {
			return nil, err
		}

		grant, exists := perms[slug]
		if !exists {
			grant = PermissionGrant{
				SiteIDs: make(map[string]struct{}),
			}
		}

		if !siteID.Valid {
			// NULL site_id means Tenant-Wide for this role assignment
			grant.TenantWide = true
		} else {
			// Specific Site
			grant.SiteIDs[siteID.String] = struct{}{}
		}

		perms[slug] = grant
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return perms, nil
}
