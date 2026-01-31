# Audit Logging Operator Guide

**Version:** 1.0  
**Last Updated:** 2026-02-01

## Overview
Phase 1.5 implements a tamper-resistant, append-only Audit Logging system with automatic Failover to disk and 7-year retention enforcement.

## Architecture
- **Primary Sink**: Postgres `audit_logs` table (Insert Only).
- **Secondary Sink (Failover)**: Local Disk Spool at configured `SpoolDir`.
- **Replay**: Application background worker retries inserting spooled events.
- **Idempotency**: `event_id` unique constraint prevents duplicates.

## Configuration
Limits are defined in `config/default.yaml` (or via Env Vars in future).
- `audit.spool_dir`: Default `C:\ProgramData\TechnoSupport\VMS\audit_spool`
- `audit.retention_years`: 7 (Enforced Minimum)
- `audit.max_spool_size_mb`: 1024 (1GB)

## Operations

### Retention Policy
The system enforces a **strict 7-year retention** (2557 days). Any attempt to purge data newer than this limit via future API/jobs will be blocked by the `retention.Guard` logic.
- **Purging**: Currently manual or external script, but must respect the safe date.
- **Archival**: Old data can be exported using the Export API before external purging.

### Handling Failover
If DB is down:
1. Logs are written to `SpoolDir`.
2. `audit_spool_dropped_total` metric stays 0 unless disk is full.
3. When DB returns, the **Replayer** automatically flushes events.
4. Check logs for `Audit Replay: X events flushed`.

### Metrics
- `audit_events_written_total`: Success rate.
- `audit_spool_bytes`: Disk usage.
- `audit_export_total`: Export activity.

### Exporting Data
- **API**: `POST /api/v1/audit/exports`
- **Format**: JSONL (Streaming).
- **Permission**: Requires `audit.export` role.
