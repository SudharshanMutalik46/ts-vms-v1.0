# Rate Limiting Operator Guide

**Version:** 1.0  
**Last Updated:** 2026-02-01

## Overview
Phase 1.4 introduces a global, user, and endpoint-specific rate limiting system backed by Redis.

## Configuration
Limits are defined in `config/default.yaml`.
- **Global IP**: 100 req/s
- **User**: 1000 req/h
- **Login**: 5 attempts / 15m (per tenant+IP)

## Tuning
To adjust limits, modify `config/default.yaml` and restart the service. Keys generally use a "sliding window" logic (TTL expiration).

## Failure Modes
- **Auth Endpoints (Fail Closed)**: If Redis is down, `/api/v1/auth/*` requests return `503 Service Unavailable` to prevent abuse.
- **API Endpoints (Fail Open)**: Other endpoints allow traffic but log `RateLimit Redis Error`.

## Monitoring
Check these metrics in Prometheus:
- `rate_limit_requests_total{result="block"}`: High rate indicates active abuse (or low limits).
- `rate_limit_redis_errors_total`: Non-zero indicates Redis connectivity issues.

## Internal Bypass
Internal services bypass limits using a specific JWT:
- Signed with: `INTERNAL_SERVICE_KEY`
- Claims: `token_type=service`, `aud=internal`
