# Monitoring & Alerting Strategy

## 1. Monitoring Scope

### Service Health (USE Method)
- **Utilization:** CPU %, RAM %, Disk IOPS.
- **Saturation:** Queue Depth (Events), Active Connections (DB/Web).
- **Errors:** HTTP 5xx Rate, Panic Count, Restart Count.

### Business Metrics
- **Streaming:** Active Streams, Frame Drop Rate, Latency (ms).
- **Recording:** Active Recorders, Write Throughput, Storage Days Remaining.
- **Security:** Failed Login Rate, Audit Queue Depth, **Cert Expiry Days**.
- **System:** **NTP Offset (ms)**.

## 2. Alert Severity Model

| Severity | Definition | Action | Examples |
| :--- | :--- | :--- | :--- |
| **SEV1 (Critical)** | Core Feature Down. Data Loss Risk. | **Wake Operator.** 24x7 Response. | Disk Full, Service Crash Loop, DB Down. |
| **SEV2 (High)** | Degradation. Feature Partial. | Ticket (High Priority). Response < 4h. | AI Overload, Time Drift > 5s, Cert Exp < 7d. |
| **SEV3 (Warning)** | Minor Issue. No Customer Impact yet. | Ticket (Next Business Day). | One Camera Offline, Cert Exp < 30d. |

## 3. Signal Hygiene
- **Noisy Alerts:** If an alert fires > 10 times/day without action, it MUST be tuned or disabled.
- **Cardinality:** Do NOT label metrics with unique `SessionID` (Explodes DB). Use `CameraID` or `Service`.
- **Root Cause:** Alert on "High Error Rate" (Symptom), not just "Pod CPU High" (Cause).

## 4. SLO Targets
- **Availability:** 99.9% (approx 9h downtime/year allowed).
- **Latency:** Live Stream Start < 2s (P90).
- **Integrity:** 0.00% Audit Log Loss.
