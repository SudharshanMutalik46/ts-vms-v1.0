# Windows Service Architecture

## 1. Intended Services

The VMS is composed of the following native Windows Services:

| Service Name | Display Name | Binary | Description |
| :--- | :--- | :--- | :--- |
| `vms-control` | Techno Support VMS Control | `vms-control.exe` | Core API and Orchestrator. |
| `vms-media` | Techno Support VMS Media | `vms-media.exe` | Video Ingest & Routing. |
| `vms-ai` | Techno Support VMS AI | `vms-ai.exe` | Video Analytics. |
| `vms-recorder` | Techno Support VMS Recorder | `vms-recorder.exe` | Video Recording. |
| `postgresql-x64-14` | PostgreSQL | `postgres.exe` | Database (Dependency). |
| `redis` | Redis | `redis-server.exe` | Cache (Dependency). |
| `nats-server` | NATS Server | `nats-server.exe` | Message Bus (Dependency). |

## 2. Startup Order & Dependencies

Windows Service Manager (`sc.exe` or `Services.msc`) handles dependencies.

1. **Tier 0 (Infra):** Postgres, Redis, NATS must start first.
2. **Tier 1 (Core):** `vms-control` starts. It waits for DB connectivity.
3. **Tier 2 (Workers):** `vms-media`, `vms-recorder`, `vms-ai` start. They register themselves with `vms-control` via gRPC/NATS.

**Dependency Definition:**
- `vms-control` depends on `postgresql-x64-14`, `redis`, `nats-server`.
- `vms-media`, `vms-recorder`, `vms-ai` depend on `vms-control` (logically, though soft dependency is preferred to allow independent restarts).

## 3. Logging & Monitoring

### 3.1 Log Locations
- Logs are written to `%PROGRAMDATA%\TechnoSupport\VMS\Logs\`
- One file per service: `control.log`, `media.log`, etc.
- **Rotation:** Managed by the application (lumberjack or similar log roller) to prevent filling the disk.

### 3.2 Windows Event Log
- Critical failures (Panics, Crash loops) are written to the **Windows Application Event Log** for system administrator visibility.

## 4. Recovery Policy
- **Failure Action:** "Restart the Service" (configured via `sc failure`).
- **Reset Fail Count:** After 1 day.
- **Restart Delay:** 10 seconds (to prevent rapid crash loops).
