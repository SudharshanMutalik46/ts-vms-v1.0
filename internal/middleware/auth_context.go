package middleware

import (
	"context"

	"github.com/technosupport/ts-vms/internal/data"
)

type contextKey string

const (
	AuthContextKey contextKey = "auth_context"
)

// AuthContext holds the authenticated user's identity and permissions
type AuthContext struct {
	TenantID string
	UserID   string
	TokenID  string   // jti
	Roles    []string // Summary of role names (optional, useful for logs)

	// Permissions map for fast lookup
	Permissions map[string]data.PermissionGrant
}

// GetAuthContext retrieves the AuthContext from the context
func GetAuthContext(ctx context.Context) (*AuthContext, bool) {
	val, ok := ctx.Value(AuthContextKey).(*AuthContext)
	return val, ok
}

// WithAuthContext attaches the AuthContext to the context
func WithAuthContext(ctx context.Context, auth *AuthContext) context.Context {
	return context.WithValue(ctx, AuthContextKey, auth)
}
