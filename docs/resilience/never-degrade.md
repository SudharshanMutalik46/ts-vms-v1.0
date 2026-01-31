# Never-Degrade Policy

## 1. Goal
Certain subsystems are **Mission Critical** and must never be sacrificed to save other components. They may stop only if physically impossible to continue (e.g., Disk Dead).

## 2. Invariants List

### A. Video Recording
- **Requirement:** Must capture video if bytes are arriving.
- **Independence:** Decoupled from AI, Web UI, and external API status.
- **Fail Mode:** If AI is overloaded -> **Kill AI**, Keep Recording.
- **Full Stop Condition:** Disk Full (after retention pruning fails) OR Write Permission Denied.

### B. Authentication
- **Requirement:** No bypass allowed.
- **Fail Closed:** If Auth DB is unreachable, **Reject All Logins**. Do NOT degrade to "Open Access".
- **Reason:** Security > Availability for Access Control.

### C. Audit Logging
- **Requirement:** Every sensitive action must be logged.
- **Backpressure:** If Audit DB is slow, **Buffer to Disk**.
- **Fail Closed:** If buffer is full, **Reject the Action**. Do not perform action without record.

## 3. Compliance Check
Every architectural change must be vetted against this list. "Does this feature risk stopping Recording?" If Yes -> Reject.
