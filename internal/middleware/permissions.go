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
	expiresAt time.Time
}

func newPermissionCache(maxItems int) *permissionCache {
	return &permissionCache{
		items:    make(map[string]cacheItem),
		maxItems: maxItems,
	}
}

func (c *permissionCache) get(key string) (map[string]data.PermissionGrant, bool) {
	c.Lock()
	defer c.Unlock()

	item, found := c.items[key]
	if !found {
		return nil, false
	}
	if time.Now().After(item.expiresAt) {
		delete(c.items, key)
		return nil, false
	}
	return item.perms, true
}

func (c *permissionCache) set(key string, perms map[string]data.PermissionGrant, ttl time.Duration) {
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

// RequirePermission returns a middleware that enforces the permission
// scopeType: "tenant", "site", "camera"
func (m *PermissionMiddleware) RequirePermission(permSlug string, scopeType string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ac, ok := GetAuthContext(r.Context())
			if !ok {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			// 1. Fetch Permissions (Cached)
			cacheKey := fmt.Sprintf("%s:%s", ac.TenantID, ac.UserID)
			grants, found := m.cache.get(cacheKey)
			if !found {
				var err error
				grants, err = m.permsRepo.GetPermissionsForUser(r.Context(), ac.TenantID, ac.UserID)
				if err != nil {
					http.Error(w, "Forbidden", http.StatusForbidden)
					return
				}
				m.cache.set(cacheKey, grants, 60*time.Second)
			}

			// 2. Check Permission Exists
			grant, exists := grants[permSlug]
			if !exists {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			// 3. Hierarchical Check
			if scopeType == "tenant" {
				// Base existence is enough (implies at least one site or tenant-wide)
				// Wait, if users only have Site A access, can they list "Tenant Users"?
				// Ideally "tenant" scope implies TenantWide=true requirement?
				// The prompt says "Tenant boundary: user can only access resources in tenant_id".
				// But "RequirePermission('users.list', 'tenant')" usually means "I need tenant-wide access".
				// Let's enforce: If scope is tenant, you generally need TenantWide access
				// UNLESS the action is inherently purely tenant-scoped but safe?
				// Usually "Tenant Admin" has TenantWide=true. "Site Admin" has SiteIDs={...}.
				// If I ask for "tenant" scope, I usually mean "Access to Tenant Resource".
				// If I am a Site Admin, can I list Tenant Users? Probably No.
				if !grant.TenantWide {
					http.Error(w, "Forbidden", http.StatusForbidden)
					return
				}
			} else if scopeType == "site" {
				// Need Target Site ID from Request
				// We assume it's in a header or query or context?
				// Middleware usually doesn't parse body.
				// Assuming standard "X-Site-ID" header or query param "site_id"?
				// Prompt says: "Do NOT rely on client-provided tenant/site IDs" (for boundary).
				// Wait. The client MUST provide which site they want to access.
				// The SERVER verifies if they have access to THAT site.
				// Let's assume URL param extraction happen before? No, middleware wraps.
				// Let's try to extract `site_id` from Query or Header for now.
				targetSiteID := r.URL.Query().Get("site_id")
				if targetSiteID == "" {
					// Fallback context?
					// If no site specified, maybe listing sites? That would be 'tenant' scope usually?
					// If endpoint is `/sites/{site_id}/...`, we need valid router to extract params.
					// Since we are standard `http.Handler`, we don't have Chi/Mux parsing context easily without deps.
					// Let's assume helper `GetResourceID(r)`?
					// For strictness, if we can't determine target site, we DENY.
					http.Error(w, "Forbidden (Target Site Missing)", http.StatusForbidden)
					return
				}

				if !grant.TenantWide {
					if _, ok := grant.SiteIDs[targetSiteID]; !ok {
						http.Error(w, "Forbidden", http.StatusForbidden)
						return
					}
				}
			} else if scopeType == "camera" {
				// Need Target Camera ID
				cameraID := r.URL.Query().Get("camera_id")
				if cameraID == "" {
					http.Error(w, "Forbidden (Target Camera Missing)", http.StatusForbidden)
					return
				}

				// Resolve Camera -> Site
				siteID, err := m.cameraResolver.ResolveSiteID(r.Context(), cameraID)
				if err != nil {
					// If camera doesn't exist or resolver fails
					http.Error(w, "Forbidden", http.StatusForbidden)
					return
				}

				if !grant.TenantWide {
					if _, ok := grant.SiteIDs[siteID]; !ok {
						http.Error(w, "Forbidden", http.StatusForbidden)
						return
					}
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}
