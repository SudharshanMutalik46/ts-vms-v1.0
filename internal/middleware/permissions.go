package middleware

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/technosupport/ts-vms/internal/data"
)

// Internal Bounded Cache
type permissionCache struct {
	sync.Mutex
	items    map[string]cacheItem
	maxItems int
}

type cacheItem struct {
	perms     map[string]data.PermissionGrant
	roles     []string
	expiresAt time.Time
}

func newPermissionCache(maxItems int) *permissionCache {
	return &permissionCache{
		items:    make(map[string]cacheItem),
		maxItems: maxItems,
	}
}

func (c *permissionCache) get(key string) (map[string]data.PermissionGrant, []string, bool) {
	c.Lock()
	defer c.Unlock()

	item, found := c.items[key]
	if !found {
		return nil, nil, false
	}
	if time.Now().After(item.expiresAt) {
		delete(c.items, key)
		return nil, nil, false
	}
	return item.perms, item.roles, true
}

func (c *permissionCache) set(key string, perms map[string]data.PermissionGrant, roles []string, ttl time.Duration) {
	c.Lock()
	defer c.Unlock()

	// Simple eviction if full: delete random or iterator (map iteration is random)
	if len(c.items) >= c.maxItems {
		for k := range c.items {
			delete(c.items, k)
			break
		}
	}

	c.items[key] = cacheItem{
		perms:     perms,
		roles:     roles,
		expiresAt: time.Now().Add(ttl),
	}
}

// CameraResolver interface for Stub
type CameraResolver interface {
	ResolveSiteID(ctx context.Context, cameraID string) (string, error)
}

// StubCameraResolver for when DB table missing
type StubCameraResolver struct{}

func (s StubCameraResolver) ResolveSiteID(ctx context.Context, cameraID string) (string, error) {
	return "", fmt.Errorf("camera resolution not supported yet")
}

// PermissionProvider interface for fetching permissions
type PermissionProvider interface {
	GetPermissionsForUser(ctx context.Context, tenantID, userID string) (map[string]data.PermissionGrant, error)
	GetFullIdentity(ctx context.Context, tenantID, userID string) ([]string, []string, error)
}

// PermissionMiddleware handles hierarchical checks
type PermissionMiddleware struct {
	permsRepo      PermissionProvider
	cameraResolver CameraResolver
	cache          *permissionCache
}

func NewPermissionMiddleware(pm PermissionProvider, cam CameraResolver) *PermissionMiddleware {
	// Ensure data.PermissionModel implements PermissionProvider
	// data.PermissionModel matches the signature.
	return &PermissionMiddleware{
		permsRepo:      pm,
		cameraResolver: cam,
		cache:          newPermissionCache(1000), // Bounded to 1000 users per instance
	}
}

// CheckPermission verifies if the user in context has the required permission for the scope
func (m *PermissionMiddleware) CheckPermission(ctx context.Context, permSlug, scopeType, scopeID string) (bool, error) {
	ac, ok := GetAuthContext(ctx)
	if !ok {
		return false, nil
	}

	// 1. Fetch Permissions (Cached)
	cacheKey := fmt.Sprintf("%s:%s", ac.TenantID, ac.UserID)
	grants, roles, found := m.cache.get(cacheKey)
	if !found {
		var err error
		grants, err = m.permsRepo.GetPermissionsForUser(ctx, ac.TenantID, ac.UserID)
		if err != nil {
			return false, err
		}
		// Also fetch roles to populate ac.Roles
		pm, ok := m.permsRepo.(data.PermissionModel)
		if ok {
			roles, _, _ = pm.GetFullIdentity(ctx, ac.TenantID, ac.UserID)
		}

		m.cache.set(cacheKey, grants, roles, 60*time.Second)
	}

	// Always sync roles to ac for downstream handlers
	ac.Roles = roles
	ac.Permissions = grants

	// 1.5 Short-circuit for elevated roles (Global Bypass)
	if ac.HasRole("admin") || ac.HasRole("operator") {
		return true, nil
	}

	// 2. Check Permission Exists
	grant, exists := grants[permSlug]
	if !exists {
		return false, nil
	}

	// 3. Hierarchical Check
	if scopeType == "tenant" {
		return grant.TenantWide, nil
	} else if scopeType == "site" {
		if grant.TenantWide {
			return true, nil
		}
		_, ok := grant.SiteIDs[scopeID]
		return ok, nil
	} else if scopeType == "camera" {
		siteID, err := m.cameraResolver.ResolveSiteID(ctx, scopeID)
		if err != nil {
			return false, nil
		}
		if grant.TenantWide {
			return true, nil
		}
		_, ok := grant.SiteIDs[siteID]
		return ok, nil
	}
	return false, nil
}

// RequirePermission returns a middleware that enforces the permission
// scopeType: "tenant", "site", "camera"
func (m *PermissionMiddleware) RequirePermission(permSlug string, scopeType string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var scopeID string
			if scopeType == "site" {
				scopeID = r.URL.Query().Get("site_id")
				if scopeID == "" {
					http.Error(w, "Forbidden (Target Site Missing)", http.StatusForbidden)
					return
				}
			} else if scopeType == "camera" {
				scopeID = r.URL.Query().Get("camera_id")
				if scopeID == "" {
					http.Error(w, "Forbidden (Target Camera Missing)", http.StatusForbidden)
					return
				}
			}

			allowed, err := m.CheckPermission(r.Context(), permSlug, scopeType, scopeID)
			if err != nil || !allowed {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"step":"rbac", "error_code":"ERR_RBAC_DENIED"}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// LoadIdentity populates AuthContext with roles/permissions without enforcing a specific slug.
// Useful for generic handlers like ListUsers that do their own internal RBAC checks.
func (m *PermissionMiddleware) LoadIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Just call CheckPermission with dummy slug to trigger population
		// CheckPermission already syncs roles/perms to AuthContext
		_, _ = m.CheckPermission(r.Context(), "", "", "")
		next.ServeHTTP(w, r)
	})
}
