package hlsd_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/hlsd"
	"github.com/technosupport/ts-vms/internal/middleware"
)

// Mock Components
type MockPermissionProvider struct{}

func (m MockPermissionProvider) GetPermissionsForUser(ctx context.Context, tenantID, userID string) (map[string]data.PermissionGrant, error) {
	if userID == "unauthorized" {
		return nil, nil
	}
	return map[string]data.PermissionGrant{
		"camera.view": {TenantWide: true},
	}, nil
}

type MockCameraResolver struct{}

func (m MockCameraResolver) ResolveSiteID(ctx context.Context, cameraID string) (string, error) {
	if cameraID == "missing" {
		return "", fmt.Errorf("not found")
	}
	return "site-1", nil
}

func TestHLSDeliveryScenarios(t *testing.T) {
	// Setup Temp HLS Layout
	tmpDir, _ := os.MkdirTemp("", "hls-test-*")
	defer os.RemoveAll(tmpDir)

	camDir := filepath.Join(tmpDir, "live", "cam1", "sess1")
	os.MkdirAll(camDir, 0755)
	os.WriteFile(filepath.Join(camDir, "index.m3u8"), []byte("#EXTM3U"), 0644)
	os.WriteFile(filepath.Join(camDir, "segment_0.m4s"), []byte("0123456789"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "meta.json"), []byte("{}"), 0644)

	// Config
	hmacSecret := "test-secret"
	keys := &hlsd.MapKeyProvider{Keys: map[string][]byte{"v1": []byte(hmacSecret)}}
	perms := middleware.NewPermissionMiddleware(MockPermissionProvider{}, MockCameraResolver{})

	h := hlsd.NewHandler(hlsd.Config{
		HlsRoot:        tmpDir,
		AllowedOrigins: []string{"http://localhost:3000"},
		Keys:           keys,
	}, perms)

	r := chi.NewRouter()
	h.Register(r)

	// Helpers
	withAuth := func(r *http.Request, tenant, user string) *http.Request {
		ac := &middleware.AuthContext{TenantID: tenant, UserID: user}
		return r.WithContext(middleware.WithAuthContext(r.Context(), ac))
	}

	getSignedURL := func(base, cam, sess, kid, secret string, exp int64) string {
		canonical := fmt.Sprintf("hls|%s|%s|%d", cam, sess, exp)
		sig := hlsd.Sign(canonical, []byte(secret))
		return fmt.Sprintf("%s?sub=%s&sid=%s&exp=%d&scope=hls&kid=%s&sig=%s", base, cam, sess, exp, kid, sig)
	}

	// 1-4: JWT & Tenant Isolation
	t.Run("1. JWT: Missing", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/hls/live/tenant1/cam1/sess1/index.m3u8", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected 401, got %d", w.Code)
		}
	})

	t.Run("2. JWT: Tenant Mismatch", func(t *testing.T) {
		req := withAuth(httptest.NewRequest("GET", "/hls/live/tenant1/cam1/sess1/index.m3u8", nil), "tenant2", "user1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected 401, got %d", w.Code)
		}
	})

	t.Run("3. RBAC: Denied", func(t *testing.T) {
		req := withAuth(httptest.NewRequest("GET", "/hls/live/tenant1/cam1/sess1/index.m3u8", nil), "tenant1", "unauthorized")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("Expected 403, got %d", w.Code)
		}
	})

	// 5-11: HMAC (Playlist)
	t.Run("4. HMAC: Missing params", func(t *testing.T) {
		req := withAuth(httptest.NewRequest("GET", "/hls/live/tenant1/cam1/sess1/index.m3u8", nil), "tenant1", "user1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("Got %d", w.Code)
		}
	})

	t.Run("5. HMAC: Invalid sig", func(t *testing.T) {
		u := getSignedURL("/hls/live/tenant1/cam1/sess1/index.m3u8", "cam1", "sess1", "v1", "wrong-secret", time.Now().Add(time.Hour).Unix())
		req := withAuth(httptest.NewRequest("GET", u, nil), "tenant1", "user1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("Got %d", w.Code)
		}
	})

	t.Run("6. HMAC: Expired", func(t *testing.T) {
		u := getSignedURL("/hls/live/tenant1/cam1/sess1/index.m3u8", "cam1", "sess1", "v1", hmacSecret, time.Now().Add(-1*time.Hour).Unix())
		req := withAuth(httptest.NewRequest("GET", u, nil), "tenant1", "user1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("Got %d", w.Code)
		}
	})

	t.Run("7. HMAC: Mismatched ID", func(t *testing.T) {
		u := getSignedURL("/hls/live/tenant1/cam1/sess1/index.m3u8", "cam2", "sess1", "v1", hmacSecret, time.Now().Add(time.Hour).Unix())
		req := withAuth(httptest.NewRequest("GET", u, nil), "tenant1", "user1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("Got %d", w.Code)
		}
	})

	// 12-15: Cookie Propagation
	t.Run("8. Cookie: Valid segment access", func(t *testing.T) {
		exp := time.Now().Add(time.Hour).Unix()
		u := getSignedURL("/hls/live/tenant1/cam1/sess1/index.m3u8", "cam1", "sess1", "v1", hmacSecret, exp)

		// First get playlist to acquire cookie
		req1 := withAuth(httptest.NewRequest("GET", u, nil), "tenant1", "user1")
		w1 := httptest.NewRecorder()
		r.ServeHTTP(w1, req1)

		cookie := w1.Result().Cookies()[0]

		// Request segment with cookie
		req2 := withAuth(httptest.NewRequest("GET", "/hls/live/tenant1/cam1/sess1/segment_0.m4s", nil), "tenant1", "user1")
		req2.AddCookie(cookie)
		w2 := httptest.NewRecorder()
		r.ServeHTTP(w2, req2)

		if w2.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d", w2.Code)
		}
	})

	t.Run("9. Cookie: Missing on segment", func(t *testing.T) {
		req := withAuth(httptest.NewRequest("GET", "/hls/live/tenant1/cam1/sess1/segment_0.m4s", nil), "tenant1", "user1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("Got %d", w.Code)
		}
	})

	// 16-20: Pathing & Hardening
	t.Run("10. Path: Traversal .. injection", func(t *testing.T) {
		req := withAuth(httptest.NewRequest("GET", "/hls/live/tenant1/cam1/sess1/../../meta.json", nil), "tenant1", "user1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusOK {
			t.Error("Should have failed")
		}
	})

	t.Run("11. Path: Regex rejection (bad extension)", func(t *testing.T) {
		req := withAuth(httptest.NewRequest("GET", "/hls/live/tenant1/cam1/sess1/hack.exe", nil), "tenant1", "user1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("Got %d", w.Code)
		}
	})

	t.Run("12. Delivery: Range Request", func(t *testing.T) {
		// Mock valid session via cookie
		exp := time.Now().Add(time.Hour).Unix()
		u := getSignedURL("", "cam1", "sess1", "v1", hmacSecret, exp)
		cookie := &http.Cookie{Name: "hls_token_sess1", Value: u[1:]} // simulate valid query string in cookie

		req := withAuth(httptest.NewRequest("GET", "/hls/live/tenant1/cam1/sess1/segment_0.m4s", nil), "tenant1", "user1")
		req.Header.Set("Range", "bytes=0-4")
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusPartialContent {
			t.Errorf("Got %d", w.Code)
		}
		if w.Body.String() != "01234" {
			t.Errorf("Got %s", w.Body.String())
		}
	})

	t.Run("13. Delivery: CORS preflight", func(t *testing.T) {
		req := httptest.NewRequest("OPTIONS", "/hls/live/tenant1/cam1/sess1/index.m3u8", nil)
		req.Header.Set("Origin", "http://localhost:3000")
		req.Header.Set("Access-Control-Request-Method", "GET")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
			t.Error("CORS fail")
		}
	})

	t.Run("14. Hardening: block meta.json explicitly", func(t *testing.T) {
		// Even if they try to bypass via direct pathing if we served that dir
		req := withAuth(httptest.NewRequest("GET", "/hls/live/tenant1/cam1/sess1/meta.json", nil), "tenant1", "user1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Error("Should be blocked by extension filter")
		}
	})
}
