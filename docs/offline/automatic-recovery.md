# Automatic Recovery Procedures

## 1. Principles
- **Automate Common Faults:** Disk full, process crash, network flake.
- **Escalate Rare Faults:** Corrupt DB, hardware failure.
- **Bounded Retries:** Never retry infinitely. Give up and alert.

## 2. Recovery Playbooks

### A. Service Crash Loop
- **Trigger:** Service exits with non-zero code.
- **Auto-Action:** SCM restarts service (up to 3 times/hour).
- **Escalation:** If still failing, stop service and log "CRITICAL: Service Dead".

### B. Disk Near-Full (90%)
- **Trigger:** Storage Monitored Metric > 90%.
- **Auto-Action:** 
    1. Signal Recorder to aggressive-prune.
    2. Enforce Retention Policy (delete oldest hour).
- **Escalation:** If disk hits 98%, **Stop Recording** to save OS/DB stability.

### C. Time Drift
- **Trigger:** NTP Offset > 5s.
- **Auto-Action:** Attempt `w32tm /resync` (Force Sync).
- **Escalation:** If drift persists > 5m, raise Alert "System Time Untrusted".

### D. Camera Reconnect Storm
- **Trigger:** Network outage ends, 500 cameras reconnect simultaneously.
- **Auto-Action:** Circuit Breaker limits concurrency. Accept 50/sec, queue rest.
- **Backpressure:** Clients retry with jitter/backoff.
