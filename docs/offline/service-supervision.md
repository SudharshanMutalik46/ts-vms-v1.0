# Service Supervision & Recovery

## 1. Supervisor Model
We use the native **Windows Service Control Manager (SCM)** as the supervisor. We do not run a separate "watchdog" daemon to minimize complexity.

## 2. Recovery Policy
For each service (`vms-control`, `vms-media`, etc.), the SCM recovery actions are configured as:

| Attempt | Action | Delay |
| :--- | :--- | :--- |
| **First Failure** | Restart Service | 5 seconds |
| **Second Failure** | Restart Service | 30 seconds |
| **Subsequent** | Restart Service | 1 minute |

**Reset Count:** Failure counter resets after **1 day** of uptime.

## 3. Fail Scenarios
- **Fail Closed:** If Auth Service fails, all API requests receive 503/401. No degradation to insecure mode.
- **Fail Open:** (Not applicable to security). If AI service fails, Video Recording MUST continue unaffected.

## 4. Crash Loop Handling
If a service restarts > 10 times in 1 hour:
1. SCM will keep restarting (with 1m delay).
2. **Alert Trigger:** Event Log monitor (future telemetry) detects "Service Terminated Unexpectedly" frequency.
3. **Escalation:** Operator manual intervention required to analyse logs.
