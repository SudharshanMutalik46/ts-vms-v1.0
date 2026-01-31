# Data Storage Layer Architecture

## 1. Overview
The Data Layer manages persistence for all subsystems. It uses a polyglot persistence approach, choosing the right tool for each data type.

## 2. Storage Responsibilities

### 2.1 Relational Data (PostgreSQL)
**Why:** ACID transactions, referential integrity.
- **Tenants & Sites:** Organization structure.
- **Cameras:** Configuration (RTSP URLs, credentials, stream profiles).
- **Users:** Credentials (hashed), roles, permissions.
- **Audit Logs:** Who did what and when.
- **Event Metadata:** "Person detected at 10:00:00". We store the *metadata* here, not the video.

### 2.2 Metrics & Telemetry (PostgreSQL / NATS)
- System health stats (CPU, RAM, Uptime) are ephemeral.
- Currently aggregated via NATS and potentially stored in a time-series table or purely ephemeral for dashboard live view.

### 2.3 Hot State (Redis)
**Why:** Low latency, ephemeral, pub/sub support.
- **Session Store:** Web user sessions.
- **Stream Registry:** "Which camera is currently being streamed?"
- **Distributed Locks:** Ensuring only one recorder records a specific camera.

### 2.4 Video Storage (Filesystem - NTFS)
**Why:** Cheapest, simplest high-throughput storage.
- Recordings are stored as `.mkv` or `.mp4` segments in a directory structure:
  `D:\Recordings\{tenant_id}\{camera_id}\{yyyy-mm-dd}\{hour}\segment_001.mkv`
- **Indexing:** The "seek table" is the filesystem itself or a lightweight index in Postgres.

## 3. Retention & TTL
- **Policy:** Defined per Tenant or Camera in Postgres.
- **Enforcement:** `vms-recorder` runs a background cleanup task.
  - "Delete files older than 30 days."
  - "Delete oldest files if disk usage > 90%."

## 4. Backup & Restore (On-Prem / Windows)
- **Configuration (PostgreSQL):** `pg_dump` runs nightly via Windows Task Scheduler.
- **Recordings:** Typically NOT backed up to cloud due to massive size. RAID is recommended for local redundancy.
