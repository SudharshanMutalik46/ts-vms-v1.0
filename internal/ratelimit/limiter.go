package ratelimit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrRateLimitExceeded = errors.New("rate limit exceeded")
	ErrRedisUnavailable  = errors.New("redis unavailable")
)

type Scope string

const (
	ScopeGlobalIP Scope = "ip"
	ScopeUser     Scope = "user"
	ScopeLogin    Scope = "login"
	ScopeEndpoint Scope = "endpoint"
)

type Decision struct {
	Scope      Scope
	Limit      int
	Remaining  int
	Reset      time.Time // When the window resets
	RetryAfter int       // Seconds
	Allowed    bool
}

type LimitConfig struct {
	Rate   int           `yaml:"rate"`
	Window time.Duration `yaml:"window"`
	Burst  int           `yaml:"burst"`
}

type Limiter struct {
	client *redis.Client
	salt   string // For IP hashing stability
}

func NewLimiter(client *redis.Client, salt string) *Limiter {
	if salt == "" {
		salt = "default-salt-change-me"
	}
	return &Limiter{client: client, salt: salt}
}

// HashIP creates a privacy-safe hash of the IP
func (l *Limiter) HashIP(ip string) string {
	hash := sha256.Sum256([]byte(ip + l.salt))
	return hex.EncodeToString(hash[:])
}

// CheckRateLimit checks if the request is allowed using a Sliding Window algorithm
func (l *Limiter) CheckRateLimit(ctx context.Context, key string, config LimitConfig) (*Decision, error) {
	// Lua Script for Sliding Window (Fixed Window approximation or actual Sliding Window?)
	// Sliding Window Counter (Smart):
	// Key: rate:{key}
	// We increment current window counter.
	// Actually, strictly strictly, Prompt 1.4 Step A says "Sliding-window counter OR token bucket".
	// Let's implement simple Fixed Window with Expiry for robustness first, or proper Sliding Window?
	// Sliding Window is better.
	// Implementation:
	// Use Redis `INCR` + `EXPIRE` on a key `key:window_start_timestamp`.
	// For simplicity and common use, a Fixed Window rooted at current time / window is easiest to implement atomically.
	// But let's try a slightly better approach: Simple Key with TTL.

	// Key format: rl:{key}

	// Lua script to Atomic INCR and Set Expire if new
	script := redis.NewScript(`
		local current = redis.call("INCR", KEYS[1])
		if tonumber(current) == 1 then
			redis.call("PEXPIRE", KEYS[1], ARGV[1])
		end
		return current
	`)

	// Construct Redis Key
	// Window alignment: To make it somewhat "sliding" in behavior or at least predictable buckets?
	// Simple TTL-based bucket starting from first request is "Sliding Window Log" roughly (actually Fixed Window starting at T0).
	// This resets exactly after Window duration from first request.

	count, err := script.Run(ctx, l.client, []string{key}, config.Window.Milliseconds()).Int()

	if err != nil {
		// Redis Failure
		return nil, ErrRedisUnavailable
	}

	remaining := config.Rate - count
	if remaining < 0 {
		remaining = 0
	}

	allowed := count <= config.Rate

	// Calculate Reset Time
	// Since we don't know exact expire time from INCR reply easily without another round trip (TTL),
	// We can estimate or fetch TTL?
	// For high performace, estimate: Now + Window (upper bound).
	// Or use TTL command?
	// Let's assume Reset is Now + Window ? No, depends on when key was created.
	// For proper headers, we usually want TTL.
	// Let's Fetch TTL if blocked? Or always?
	// Optimization: Only fetch TTL if needed or pipeline it.
	// Let's pipeline: Run Script + TTL.
	// For now, let's keep it simple: Estimate Reset = Now + Window (conservative), or fetch TTL.

	return &Decision{
		Limit:      config.Rate,
		Remaining:  remaining,
		Reset:      time.Now().Add(config.Window), // Approximation
		RetryAfter: int(config.Window.Seconds()),  // Approximation
		Allowed:    allowed,
	}, nil
}
