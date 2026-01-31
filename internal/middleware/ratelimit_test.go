package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/technosupport/ts-vms/internal/middleware"
	"github.com/technosupport/ts-vms/internal/ratelimit"
	"github.com/technosupport/ts-vms/internal/tokens"
)

// MockTokenValidatorRL for Rate Limit tests
type MockTokenValidatorRL struct{}

func (m MockTokenValidatorRL) ValidateToken(token string) (*tokens.Claims, error) {
	if token == "valid-user" {
		return &tokens.Claims{TenantID: "t1", UserID: "u1", TokenType: tokens.Access}, nil
	}
	if token == "service-token" {
		return nil, tokens.ErrInvalidToken
	}
	return nil, tokens.ErrInvalidToken
}

func TestRateLimit_GlobalIP(t *testing.T) {
	mr, _ := miniredis.Run()
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	limiter := ratelimit.NewLimiter(rdb, "salt")
	cfg := middleware.Config{
		GlobalIP: ratelimit.LimitConfig{Rate: 2, Window: time.Second},
	}

	mw := middleware.NewRateLimitMiddleware(limiter, MockTokenValidatorRL{}, cfg, nil)

	handler := mw.GlobalLimiter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	// 1. Allow
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	// 2. Allow
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	// 3. Block
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 429 {
		t.Errorf("Expected 429, got %d", w.Code)
	}

	// Check Headers
	if w.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Error("Expected remaining 0")
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("Expected Retry-After header")
	}
}

func TestRateLimit_RedisDown_FailOpen(t *testing.T) {
	// Closed Redis
	mr, _ := miniredis.Run()
	addr := mr.Addr() // Get addr while open
	mr.Close()        // Close it to simulate failure

	rdb := redis.NewClient(&redis.Options{Addr: addr})

	limiter := ratelimit.NewLimiter(rdb, "salt")
	cfg := middleware.Config{GlobalIP: ratelimit.LimitConfig{Rate: 1, Window: time.Second}}
	mw := middleware.NewRateLimitMiddleware(limiter, MockTokenValidatorRL{}, cfg, nil)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	mw.GlobalLimiter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})).ServeHTTP(w, req)

	// Should allow (Fail Open)
	if w.Code != 200 {
		t.Errorf("Expected 200 (Fail Open), got %d", w.Code)
	}
}

func TestRateLimit_User(t *testing.T) {
	mr, _ := miniredis.Run()
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	limiter := ratelimit.NewLimiter(rdb, "salt")
	cfg := middleware.Config{
		GlobalIP: ratelimit.LimitConfig{Rate: 100, Window: time.Second},
		User:     ratelimit.LimitConfig{Rate: 1, Window: time.Second},
	}
	mw := middleware.NewRateLimitMiddleware(limiter, MockTokenValidatorRL{}, cfg, nil)

	// Auth Context
	ctx := middleware.WithAuthContext(context.Background(), &middleware.AuthContext{TenantID: "t1", UserID: "u1"})
	req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	req.RemoteAddr = "10.0.0.1:123"

	handler := mw.GlobalLimiter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	// 1. Allow
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	// 2. Block User (Global IP is fine, but User limit hit)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 429 {
		t.Errorf("Expected 429 User Block, got %d", w.Code)
	}
}

func TestRateLimit_RedisDown_Auth_FailClosed(t *testing.T) {
	// Closed Redis
	mr, _ := miniredis.Run()
	addr := mr.Addr()
	mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: addr})

	limiter := ratelimit.NewLimiter(rdb, "salt")
	cfg := middleware.Config{GlobalIP: ratelimit.LimitConfig{Rate: 1, Window: time.Second}}
	mw := middleware.NewRateLimitMiddleware(limiter, MockTokenValidatorRL{}, cfg, nil)

	req := httptest.NewRequest("POST", "/api/v1/auth/login", nil) // Auth Path
	w := httptest.NewRecorder()

	mw.GlobalLimiter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})).ServeHTTP(w, req)

	// Should Block (Fail Closed)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503 (Fail Closed), got %d", w.Code)
	}
}
