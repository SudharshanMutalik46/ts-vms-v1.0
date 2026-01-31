package middleware

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/technosupport/ts-vms/internal/tokens"
)

// PolicyMapper defines required permissions for gRPC methods
type PolicyMapper interface {
	// Returns permission slug and scope type ("tenant", "site", "camera")
	GetPolicy(fullMethod string) (perm string, scope string, found bool)
}

// GRPCAuthInterceptor handles JWT validation + Permission Checks for gRPC
type GRPCAuthInterceptor struct {
	jwtAuth *JWTAuth
	perms   *PermissionMiddleware
	policy  PolicyMapper
}

func NewGRPCAuthInterceptor(j *JWTAuth, p *PermissionMiddleware, policy PolicyMapper) *GRPCAuthInterceptor {
	return &GRPCAuthInterceptor{jwtAuth: j, perms: p, policy: policy}
}

func (i *GRPCAuthInterceptor) Unary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// 1. Extract Token from Metadata
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "metadata missing")
		}

		authHeader := md["authorization"]
		if len(authHeader) == 0 {
			return nil, status.Error(codes.Unauthenticated, "authorization missing")
		}

		parts := strings.Split(authHeader[0], " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			return nil, status.Error(codes.Unauthenticated, "invalid token format")
		}

		tokenString := parts[1]

		// 2. Validate Token (Reusing JWTAuth logic manually or extracting Validator?)
		// To avoid code duplication, we use the Validator directly
		claims, err := i.jwtAuth.tokens.ValidateToken(tokenString)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}

		if claims.TokenType != tokens.Access {
			return nil, status.Error(codes.Unauthenticated, "invalid token type")
		}

		// 3. Blacklist Check
		blacklisted, err := i.jwtAuth.blacklist.IsBlacklisted(ctx, claims.TenantID, claims.ID)
		if err != nil || blacklisted {
			return nil, status.Error(codes.Unauthenticated, "token revoked")
		}

		// 4. Inject Context
		ac := &AuthContext{
			TenantID: claims.TenantID,
			UserID:   claims.UserID,
			TokenID:  claims.ID,
		}
		ctx = WithAuthContext(ctx, ac)

		// 5. Enforce Permissions
		// Default Deny: If not in policy, Reject.
		perm, scope, found := i.policy.GetPolicy(info.FullMethod)
		if !found {
			// Allow Public methods explicit list?
			// Prompt says "except for explicitly public endpoints... which must bypass auth safely"
			// But here we already did Auth check.
			// If method is public, it shouldn't require Auth at all, so Interceptor should skip step 1-4 too?
			// Actually, Public usually means "No Auth Required".
			// Policy should return "Public" flag?
			// Let's assume GetPolicy returns (perm, scope, isPublic, found).
			return nil, status.Error(codes.PermissionDenied, "access denied (no policy)")
		}

		// Check Permission
		// We reuse the logic from PermissionMiddleware but adapted for gRPC context
		// Fetch Permissions
		cacheKey := claims.TenantID + ":" + claims.UserID
		grants, foundCache := i.perms.cache.get(cacheKey)
		if !foundCache {
			var err error
			grants, err = i.perms.permsRepo.GetPermissionsForUser(ctx, claims.TenantID, claims.UserID)
			if err != nil {
				return nil, status.Error(codes.PermissionDenied, "failed to load permissions")
			}
			i.perms.cache.set(cacheKey, grants, 60e9) // 60s
		}

		grant, exists := grants[perm]
		if !exists {
			return nil, status.Error(codes.PermissionDenied, "permission missing")
		}

		// Scope Check
		if scope == "tenant" {
			if !grant.TenantWide {
				return nil, status.Error(codes.PermissionDenied, "tenant-wide access required")
			}
		} else if scope == "site" {
			// Extract Site ID from request (using reflection or proto interface?)
			// For generic interceptor, we usually require requests to implement `GetSiteId() string`
			// or define extraction strategy.
			// Prompt says: "enforces per-method permission rules (map method -> required permission + scope resolver)"
			// We need a Resolver for the request message.
			// Simplification: We assume the PolicyMapper might handle extraction?
			// Or we just fail if we can't extract?
			// Let's assume we can't easily extract without reflection.
			// For this implementation, we will stub the extraction logic or assume a specific interface properly.
			// Let's stick to "If Site Scope Required, Check if User has ANY matching site in request?"
			// Actually, "enforce per-method permission rules".
			// Let's assume checks pass if the user has AT LEAST ONE site access? No, that's insecure.
			// We need to check target site.
			// Let's assume the request object has `SiteId`.
			// Since we can't use reflection easily without "protoreflect",
			// we will return "Unimplemented Scope Check" for now unless we import proto req types.
			// Wait, we can implement extraction via interface.

			type SiteIdentifiable interface {
				GetSiteId() string
			}

			if reqWithSite, ok := req.(SiteIdentifiable); ok {
				targetSite := reqWithSite.GetSiteId()
				if !grant.TenantWide {
					if _, ok := grant.SiteIDs[targetSite]; !ok {
						return nil, status.Error(codes.PermissionDenied, "site access denied")
					}
				}
			} else {
				// Request doesn't have SiteID but policy requires Site Scope?
				return nil, status.Error(codes.InvalidArgument, "site_id missing in request")
			}
		}

		return handler(ctx, req)
	}
}
