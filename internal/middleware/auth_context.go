package middleware

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
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

// GetUserFromContext constructs a partial User object from AuthContext
// This is a helper for Handlers that need a User struct for Service calls
// Note: This User object only contains ID and TenantID from the token
func GetUserFromContext(ctx context.Context) (*data.User, error) {
	ac, ok := GetAuthContext(ctx)
	if !ok {
		return nil, fmt.Errorf("no auth context found")
	}

	uid, err := uuid.Parse(ac.UserID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id in context: %v", err)
	}

	tid, err := uuid.Parse(ac.TenantID)
	if err != nil {
		return nil, fmt.Errorf("invalid tenant id in context: %v", err)
	}

	return &data.User{
		ID:       uid,
		TenantID: tid,
	}, nil
}

// HasRole checks if the user has a specific role (case-insensitive)
func (ac *AuthContext) HasRole(role string) bool {
	if ac == nil {
		return false
	}
	for _, r := range ac.Roles {
		if strings.EqualFold(r, role) {
			return true
		}
	}
	return false
}

// HasPermission checks for a permission slug
func (ac *AuthContext) HasPermission(perm string) bool {
	if ac == nil || ac.Permissions == nil {
		return false
	}
	_, ok := ac.Permissions[perm]
	return ok
}
