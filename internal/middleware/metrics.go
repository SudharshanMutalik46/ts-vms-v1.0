package middleware

// Metrics Interface (Future Prometheus integration)
// For now, we rely on Logs, but this struct holds the place for CounterVecs.

func RecordRateLimit(scope string, result string) {
	// In future: rateLimitRequestsTotal.WithLabelValues(scope, result).Inc()
	// For MVP: We already log "RateLimit Error" or "Rate Limit Exceeded" in middleware.
	// This function can be used for explicit metric incrementing if we add Prom client.
}

func RecordRedisError() {
	// In future: rateLimitRedisErrorsTotal.Inc()
}
