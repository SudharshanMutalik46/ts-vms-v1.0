# Recovery Procedures

## 1. Automated Recovery (Self-Healing)
These actions happen **without** Operator intervention. They must be safe and idempotent.

- **Service Restart:** SCM attempts to restart `vms-control` etc. (Limit: 3 restarts/hr).
- **Circuit Breaker Reset:** After "Open" state, system probes dependency. If Success -> Close Breaker.
- **Cache Flush:** If AI Metadata DB is corrupt, flush Redis cache on boot.
- **Reconnect:** RTSP Client uses exponential backoff (1s, 2s, 4s, 8s, 16s, 30s cap) to reconnect to cameras.
- **Session Cleanup:** Garbage collect "Zombie" WebRTC sessions every 5 minutes.

## 2. Manual Recovery (Operator Runbooks)

### Runbook A: Disk Full Recovery
**Symptom:** "Recording Stopped - Disk Full" alert.
1. Log into Server.
2. Check `D:\Recordings` usage.
3. Run `Force-Prune.ps1 -DaysToKeep 30` (Reduces retention).
4. Verify recording resumes (check logs).

### Runbook B: Certificate Expiry
**Symptom:** "Browser Security Warning" / "API TLS Fail".
1. Generate new `server.crt` via Offline CA.
2. Place in `C:\VMS\Certs\`.
3. Restart `vms-control` service.
4. Distribute new Public Cert to client workstations if Root CA changed.

### Runbook C: Database Corruption
**Symptom:** "Postgres Startup Failed" logs.
1. Stop all VMS Services.
2. Rename `data` folder to `data_backup`.
3. Run `pg_restore` from last nightly Snapshot.
4. Start Services. **Note:** Recent dataloss (gap since snapshot) is expected.

### Runbook D: Time Drift Correction
**Symptom:** "Critical Time Offsets" audit log.
1. Stop all Services (Prevent timestamp poisoning).
2. Force Sync: `w32tm /resync`.
3. Start Services.
