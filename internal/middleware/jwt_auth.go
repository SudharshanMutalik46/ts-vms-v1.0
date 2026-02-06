package middleware

import (
	"net/http"
	"strings"

	"github.com/technosupport/ts-vms/internal/auth"
	"github.com/technosupport/ts-vms/internal/tokens"
)

type TokenValidator interface {
	ValidateToken(tokenString string) (*tokens.Claims, error)
}

type JWTAuth struct {
	tokens    TokenValidator
	blacklist auth.TokenBlacklist
}

func NewJWTAuth(t TokenValidator, b auth.TokenBlacklist) *JWTAuth {
	return &JWTAuth{tokens: t, blacklist: b}
}

// Middleware verifies the JWT and injects AuthContext
func (m *JWTAuth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"step":"auth", "error_code":"ERR_AUTH_MISSING"}`))
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"step":"auth", "error_code":"ERR_AUTH_FORMAT"}`))
			return
		}

		tokenString := parts[1]

		// 1. Validate Signature & Claims
		claims, err := m.tokens.ValidateToken(tokenString)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"step":"auth", "error_code":"ERR_AUTH_INVALID"}`))
			return
		}

		if claims.TokenType != tokens.Access {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"step":"auth", "error_code":"ERR_AUTH_TYPE"}`))
			return
		}

		// 2. Check Blacklist
		blacklisted, err := m.blacklist.IsBlacklisted(r.Context(), claims.TenantID, claims.ID)
		if err != nil {
			// dev-stability: Fail open on Redis error
			// log.Printf("[WARN] Blacklist check failed: %v", err)
		} else if blacklisted {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"step":"auth", "error_code":"ERR_AUTH_BLACKLISTED"}`))
			return
		}

		// 3. Inject Context
		ac := &AuthContext{
			TenantID: claims.TenantID,
			UserID:   claims.UserID,
			TokenID:  claims.ID,
		}

		ctx := WithAuthContext(r.Context(), ac)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
