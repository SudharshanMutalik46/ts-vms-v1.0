package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/audit"
)

type AuditMiddleware struct {
	service *audit.Service
}

func NewAuditMiddleware(s *audit.Service) *AuditMiddleware {
	return &AuditMiddleware{service: s}
}

// LogRequest captures request details and writes an audit log.
// Wraps specific routes or global.
// Usage: Apply to all Authenticated Routes.
func (m *AuditMiddleware) LogRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Response Capture Wrapper
		ww := &responseCapture{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(ww, r)

		// Filter: Only Mutating methods OR Auth endpoints
		isMutating := r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" || r.Method == "DELETE"
		isAuth := strings.HasPrefix(r.URL.Path, "/api/v1/auth/")

		if !isMutating && !isAuth {
			return
		}

		// Prepare Audit Event
		evt := audit.AuditEvent{
			EventID:    uuid.New(),
			Action:     truncate(fmt.Sprintf("http.%s", strings.ToLower(r.Method)), 100),
			TargetType: "http_route",
			TargetID:   truncate(r.URL.Path, 100),
			Result:     "success",
			RequestID:  truncate(r.Header.Get("X-Request-ID"), 100),
			ClientIP:   truncate(extractIP(r), 50),
			UserAgent:  truncate(r.UserAgent(), 255),
			CreatedAt:  time.Now(),
		}

		// Add Latency to Metadata
		duration := time.Since(start)
		evt.Metadata = json.RawMessage(fmt.Sprintf(`{"latency_ms": %d}`, duration.Milliseconds()))

		if ww.status >= 400 {
			evt.Result = "failure"
			evt.ReasonCode = truncate(fmt.Sprintf("http_%d", ww.status), 50)
		}

		// Auth Context
		if ac, ok := GetAuthContext(r.Context()); ok {
			if tid, err := uuid.Parse(ac.TenantID); err == nil {
				evt.TenantID = tid
			}
			if uid, err := uuid.Parse(ac.UserID); err == nil {
				evt.ActorUserID = &uid
			}
		}

		// Async Write
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = m.service.WriteEvent(ctx, evt)
		}()
	})
}

type responseCapture struct {
	http.ResponseWriter
	status int
}

func (w *responseCapture) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func extractIP(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}
	// Hash? Requirement says "ip_hash" in 1.4 but 1.5 service uses string.
	// 1.5 prompt says "ip_hash ... (hashed; optional)".
	// We pass raw, let Service hash if desired or store?
	// Phase 1.4 enforced privacy.
	// Let's assume we store raw here if permitted, or hash if 1.4 context implies.
	// 1.5 locked rules: "NO secrets...". IP is PII. Hashing recommended.
	// But without salt from Limiter, we can't match?
	// Let's store raw for Audit unless configured otherwise, as "ClientIP" is standard audit field often internal.
	// Wait, 1.4 "ip_hash" was mandatory for limiter keys. 1.5 says "ip_hash... optional".
	// Let's just return what we have.
	return ip
}

func truncate(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}
