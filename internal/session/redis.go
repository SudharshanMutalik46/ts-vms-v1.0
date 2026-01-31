package session

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	MaxSessionsPerUser = 5
	SessionTTL         = 7 * 24 * time.Hour // Matches Refresh Token
	LockoutTTL         = 15 * time.Minute
	LockoutThreshold   = 5
)

type Manager struct {
	client *redis.Client
}

func NewManager(addr string, password string) *Manager {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       0,
	})
	return &Manager{client: rdb}
}

// CreateSession registers a new session and enforces MaxSessionsPerUser
func (m *Manager) CreateSession(ctx context.Context, userID, tenantID, sessionID string) error {
	userKey := fmt.Sprintf("user_sessions:%s", userID)
	sessionKey := fmt.Sprintf("session:%s", sessionID)

	pipe := m.client.Pipeline()

	// 1. Add session to user set (score = timestamp for eviction)
	now := float64(time.Now().Unix())
	pipe.ZAdd(ctx, userKey, redis.Z{Score: now, Member: sessionID})
	pipe.Expire(ctx, userKey, SessionTTL)

	// 2. Store session details
	pipe.HSet(ctx, sessionKey, "tenant_id", tenantID, "user_id", userID, "created_at", now)
	pipe.Expire(ctx, sessionKey, SessionTTL)

	// 3. Enforce Bounding (Keep only recent MaxSessionsPerUser)
	// ZREMRANGEBYRANK user_sessions 0 -6 (removes all but last 5)
	// Actually we want to remove 0 to -(Max+1).
	// To keep N, we remove from 0 to -(N+1).
	removeCount := int64(-1 * (MaxSessionsPerUser + 1))
	pipe.ZRemRangeByRank(ctx, userKey, 0, removeCount)

	_, err := pipe.Exec(ctx)
	return err
}

func (m *Manager) RevokeSession(ctx context.Context, sessionID string) error {
	sessionKey := fmt.Sprintf("session:%s", sessionID)

	// Get UserID to clean up set
	userID, err := m.client.HGet(ctx, sessionKey, "user_id").Result()
	if err != nil && err != redis.Nil {
		return err
	}

	pipe := m.client.Pipeline()
	pipe.Del(ctx, sessionKey)
	if userID != "" {
		userKey := fmt.Sprintf("user_sessions:%s", userID)
		pipe.ZRem(ctx, userKey, sessionID)
	}
	_, err = pipe.Exec(ctx)
	return err
}

func (m *Manager) RevokeAllUserSessions(ctx context.Context, userID string) error {
	userKey := fmt.Sprintf("user_sessions:%s", userID)

	// Get all Session IDs
	sessionIDs, err := m.client.ZRange(ctx, userKey, 0, -1).Result()
	if err != nil {
		return err
	}

	if len(sessionIDs) == 0 {
		return nil
	}

	pipe := m.client.Pipeline()
	pipe.Del(ctx, userKey) // Clear the set

	// Delete individual session keys
	for _, sid := range sessionIDs {
		pipe.Del(ctx, fmt.Sprintf("session:%s", sid))
	}

	_, err = pipe.Exec(ctx)
	return err
}

// CheckLockout returns true if user is locked out
func (m *Manager) CheckLockout(ctx context.Context, tenantID, email string) (bool, error) {
	key := fmt.Sprintf("lockout:%s:%s", tenantID, email)
	val, err := m.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return val == "locked", nil
}

// RecordFailedAttempt increments failure count and locks if threshold reached
func (m *Manager) RecordFailedAttempt(ctx context.Context, tenantID, email string) error {
	key := fmt.Sprintf("lockout_count:%s:%s", tenantID, email)
	count, err := m.client.Incr(ctx, key).Result()
	if err != nil {
		return err
	}

	// Set expiry on first fail to ensure the window slides/resets
	if count == 1 {
		m.client.Expire(ctx, key, LockoutTTL)
	}

	if count >= LockoutThreshold {
		lockKey := fmt.Sprintf("lockout:%s:%s", tenantID, email)
		m.client.Set(ctx, lockKey, "locked", LockoutTTL) // Lock for 15 mins
		// Optional: Clear counter so after 15m they start fresh?
		// Or keep it to re-lock faster? Standard practice is hard lock for duration.
		m.client.Del(ctx, key)
	}
	return nil
}
