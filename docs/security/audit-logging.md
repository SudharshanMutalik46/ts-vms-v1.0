# Audit Logging Requirements

## 1. Requirement
**Retention:** All audit logs must be retained for **7 Years** to meet legal/compliance standards (e.g., evidence in investigations).

## 2. Event Scope
The following actions MUST generate an audit record:
- **Auth:** Login success/failure, Logout, Refresh Token Reuse.
- **Identity:** User created, User disabled, Role assigned, Permissions changed.
- **Resources:** Camera created/deleted, NVR sync.
- **Data:** Recording exported/downloaded, Live View session started.

## 3. Logical Schema
Every log entry must contain:

| Field | Type | Description |
| :--- | :--- | :--- |
| `timestamp_utc` | ISO8601 | When it happened. |
| `id` | UUID | Unique Event ID. |
| `tenant_id` | UUID | Scope of the event. |
| `actor_user_id` | UUID | Who did it? |
| `ip_address` | String | Source IP. |
| `action` | String | e.g. `camera.update` |
| `target_type` | String | e.g. `camera` |
| `target_id` | UUID | ID of the object affected. |
| `result` | String | `SUCCESS` / `FAILURE` |
| `reason_code` | String | e.g. `AUTH_FAILED` |
| `request_id` | UUID | Trace ID for debugging. |

## 4. Integrity
- **Write-Once:** Logs are appended to daily/hourly files.
- **Tamper-Evident:** (Future) Hash chaining of log files.
