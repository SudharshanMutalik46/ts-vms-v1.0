package middleware

import (
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// RequestLogger generates a req_id and logs trace info
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := uuid.New().String()
		start := time.Now()

		// Inject req_id into header for client debugging
		w.Header().Set("X-Request-ID", reqID)

		// Log Request Start
		log.Printf("[REQ:%s] %s %s from %s", reqID, r.Method, r.URL.Path, r.RemoteAddr)

		// Wrap Writer
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rw, r)

		duration := time.Since(start)

		// A1: Log Method, Path, Remote IP, Status, Duration
		// Also log auth failures if status is 401/403
		log.Printf("[REQ:%s] Completed %d in %v", reqID, rw.status, duration)
	})
}
