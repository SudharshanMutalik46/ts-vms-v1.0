# Operations: Windows Service Management

The Techno Support VMS stack is designed to run as a suite of native Windows services managed by the Service Control Manager (SCM).

## Service Identities

| Service Name       | Display Name                     | Binary                 | Description                      |
|--------------------|----------------------------------|------------------------|----------------------------------|
| `TS-VMS-Control`   | Techno Support VMS Control       | `server.exe`           | Central API and state manager    |
| `TS-VMS-Web`       | Techno Support VMS Web           | `web-server.exe`       | User Interface and Dashboard     |
| `TS-VMS-Media`     | Techno Support VMS Media         | `vms-media.exe`        | Video processing and RTSP        |
| `TS-VMS-SFU`       | Techno Support VMS SFU           | `node.exe` + `main.js` | WebRTC Signaling and SFU         |
| `TS-VMS-Recorder`  | Techno Support VMS Recorder      | `vms-recorder.exe`     | Video storage and finalization   |
| `TS-VMS-AI`        | Techno Support VMS AI            | `python.exe` + `main.py`| Analytics and object detection   |

## Dependency Chain

Services start in a deterministic order based on SCM dependencies:
1. `TS-VMS-Control` (Bootstrap)
2. `TS-VMS-Media`, `TS-VMS-Web`, `TS-VMS-Recorder`, `TS-VMS-AI` (Depend on Control)
3. `TS-VMS-SFU` (Depends on Control and Media)

## Recovery Policy

Each service is configured with the following auto-recovery actions:
- **First Failure**: Restart service after 60s.
- **Second Failure**: Restart service after 60s.
- **Subsequent Failures**: No action (Wait for operator intervention).
- **Reset Count**: Failure count is reset after 24 hours of stable uptime.

### Crash-Loop Prevention
If a service fails to stay running for more than 5 minutes after multiple restarts, **DO NOT FORCE RESTART**.
1. Check `C:\ProgramData\TechnoSupport\VMS\logs\` for application errors.
2. Verify PostgreSQL and Redis services are healthy.
3. Check Windows Event Viewer -> Application Log for source `TS-VMS-*`.

## Event Log Integration

All services log critical lifecycle events to the Windows Event Log:
- **Event ID 100**: Service Started / Running.
- **Event ID 101**: Service Stopped / Shutting Down.
- **Event ID 102**: Unrecoverable Error (Check details).

## Operator Commands

Use the provided PowerShell script in `scripts/service-manager.ps1`:

```powershell
# Install all services (Requires Admin)
.\scripts\service-manager.ps1 -Action Install

# Check status of all services
.\scripts\service-manager.ps1 -Action Status

# Restart a specific component
.\scripts\service-manager.ps1 -Action Restart -Component TS-VMS-Control

# Uninstall and purge all data
.\scripts\service-manager.ps1 -Action Uninstall -PurgeData
```

## External Dependencies
PostgreSQL and Redis are expected to be installed as Windows services. The VMS services implement health checks and will retry connections with exponential backoff if the database or cache is temporarily unavailable. If they are not running as services, ensure they are started before the VMS stack.
