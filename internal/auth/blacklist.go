package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// TokenBlacklist defines interface for checking revoked tokens
type TokenBlacklist interface {
	IsBlacklisted(ctx context.Context, tenantID, jti string) (bool, error)
	AddToBlacklist(ctx context.Context, tenantID, jti string, ttl time.Duration) error
}

type RedisBlacklist struct {
	client *redis.Client
}

func NewRedisBlacklist(client *redis.Client) *RedisBlacklist {
	return &RedisBlacklist{client: client}
}

func (r *RedisBlacklist) IsBlacklisted(ctx context.Context, tenantID, jti string) (bool, error) {
	// Tenant scoped key: blacklist:tenant:jti
	key := fmt.Sprintf("blacklist:%s:%s", tenantID, jti)
	exists, err := r.client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}

func (r *RedisBlacklist) AddToBlacklist(ctx context.Context, tenantID, jti string, ttl time.Duration) error {
	key := fmt.Sprintf("blacklist:%s:%s", tenantID, jti)
	return r.client.Set(ctx, key, "revoked", ttl).Err()
}
