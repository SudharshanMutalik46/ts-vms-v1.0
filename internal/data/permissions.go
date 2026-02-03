package data

import (
	"context"
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
	// Updated to match schema: user_roles(user_id, role_id, scope_type, scope_id)
	query := `
		SELECT 
			p.name,
			ur.scope_type,
			ur.scope_id
		FROM user_roles ur
		JOIN roles r ON ur.role_id = r.id
		JOIN role_permissions rp ON r.id = rp.role_id
		JOIN permissions p ON rp.permission_id = p.id
		WHERE ur.user_id = $1
		AND (
			(ur.scope_type = 'tenant' AND ur.scope_id = $2)
			OR 
			(ur.scope_type = 'site') 
		)
	`

	rows, err := m.DB.QueryContext(ctx, query, userID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	perms := make(map[string]PermissionGrant)

	for rows.Next() {
		var name string
		var scopeType string
		var scopeID string // UUID as string

		if err := rows.Scan(&name, &scopeType, &scopeID); err != nil {
			return nil, err
		}

		grant, exists := perms[name]
		if !exists {
			grant = PermissionGrant{
				SiteIDs: make(map[string]struct{}),
			}
		}

		if scopeType == "tenant" {
			grant.TenantWide = true
		} else if scopeType == "site" {
			grant.SiteIDs[scopeID] = struct{}{}
		}

		perms[name] = grant
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return perms, nil
}
