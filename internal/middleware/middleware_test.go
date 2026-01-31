package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/middleware"
	"github.com/technosupport/ts-vms/internal/tokens"
)

// Mock Permission Model
type MockPermissionModel struct{}

func (m MockPermissionModel) GetPermissionsForUser(ctx context.Context, tenantID, userID string) (map[string]data.PermissionGrant, error) {
	if userID == "admin-user" {
		return map[string]data.PermissionGrant{
			"site.view": {TenantWide: true},
		}, nil
	}
	if userID == "site-manager" {
		return map[string]data.PermissionGrant{
			"site.view": {
				SiteIDs: map[string]struct{}{"site-1": {}},
			},
		}, nil
	}
	return nil, nil // No perms
}

// Mock Token Validator
type MockTokenValidator struct{}

func (m MockTokenValidator) ValidateToken(token string) (*tokens.Claims, error) {
	if token == "valid-access" {
		return &tokens.Claims{
			TenantID:  "tenant-1",
			UserID:    "admin-user",
			TokenType: tokens.Access,
		}, nil
	}
	return nil, tokens.ErrInvalidToken // simplified
}

// Mock Blacklist
type MockBlacklist struct{}

func (m MockBlacklist) IsBlacklisted(ctx context.Context, tenant, jti string) (bool, error) {
	if jti == "revoked-jti" {
		return true, nil
	}
	return false, nil
}
func (m MockBlacklist) AddToBlacklist(ctx context.Context, tenant, jti string, ttl time.Duration) error {
	return nil
}

func TestJWTAuthMiddleware_Success(t *testing.T) {
	mw := middleware.NewJWTAuth(MockTokenValidator{}, MockBlacklist{})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer valid-access")
	w := httptest.NewRecorder()

	mw.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac, ok := middleware.GetAuthContext(r.Context())
		if !ok || ac.UserID != "admin-user" {
			t.Errorf("AuthContext missing or invalid")
		}
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
}

func TestJWTAuthMiddleware_MissingHeader(t *testing.T) {
	mw := middleware.NewJWTAuth(MockTokenValidator{}, MockBlacklist{})
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	mw.Middleware(nil).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", w.Code)
	}
}

func TestPermissionMiddleware_TenantWide(t *testing.T) {
	pm := middleware.NewPermissionMiddleware(MockPermissionModel{}, middleware.StubCameraResolver{})

	// Create context with Admin User
	ctx := middleware.WithAuthContext(context.Background(), &middleware.AuthContext{
		TenantID: "tenant-1",
		UserID:   "admin-user",
	})

	req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	// Require permission
	handler := pm.RequirePermission("site.view", "tenant")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Expected Allowed (200), got %d", w.Code)
	}
}

func TestPermissionMiddleware_SiteScoped_Allowed(t *testing.T) {
	pm := middleware.NewPermissionMiddleware(MockPermissionModel{}, middleware.StubCameraResolver{})

	ctx := middleware.WithAuthContext(context.Background(), &middleware.AuthContext{
		TenantID: "tenant-1",
		UserID:   "site-manager",
	})

	// Request for site-1
	req := httptest.NewRequest("GET", "/?site_id=site-1", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler := pm.RequirePermission("site.view", "site")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Expected Allowed (200), got %d", w.Code)
	}
}

func TestPermissionMiddleware_SiteScoped_Denied(t *testing.T) {
	pm := middleware.NewPermissionMiddleware(MockPermissionModel{}, middleware.StubCameraResolver{})

	ctx := middleware.WithAuthContext(context.Background(), &middleware.AuthContext{
		TenantID: "tenant-1",
		UserID:   "site-manager",
	})

	// Request for site-2 (Not allowed)
	req := httptest.NewRequest("GET", "/?site_id=site-2", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler := pm.RequirePermission("site.view", "site")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("Expected Forbidden (403), got %d", w.Code)
	}
}
