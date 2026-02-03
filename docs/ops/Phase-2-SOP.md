# SOP: Phase 2 - Device Integration & Network Adaptations

**Document ID:** SOP-MASTER-002  
**Version:** 1.0  
**Last Updated:** 2026-02-03

## 1. Objective
Provide a unified reference for administrators to deploy, configure, and verify the device integration layer (Cameras/NVRs) and Windows-native adaptations implemented in Phase 2.

## 2. Infrastructure & Paths

### Windows-Native File System
The system now enforces standard Windows directory structures. Ensure the following roots are accessible:
- **Install Root**: `C:\Program Files\TechnoSupport\VMS`
- **Data Root**: `C:\ProgramData\TechnoSupport\VMS`

Initialize standard subdirectories:
```powershell
# This is automatically called on server startup
[TechnoSupport.VMS.Paths]::EnsureDirs()
```

## 3. Network & Security

### Firewall Orchestration (Administrator)
Manage VMS-required ports (8080, 8081, 4222) using the idempotent manager script:

1. **Install Rules**:
   ```powershell
   .\scripts\firewall-manager.ps1 -Action Install
   ```
2. **Check Status**:
   ```powershell
   .\scripts\firewall-manager.ps1 -Action Status
   ```
3. **Uninstall Rules**:
   ```powershell
   .\scripts\firewall-manager.ps1 -Action Uninstall
   ```

### Windows Discovery (API)
Trigger a native network scan (NICs + ARP) via the Control Plane API:
- **Endpoint**: `POST /api/v1/windows/discovery:scan`
- **Payload**: `{"probe": true}` (Optional active probing)

## 4. NVR & Event Integration

### Event Polling Configuration
Configure polling intervals and concurrency in `config/default.yaml`:
```yaml
events:
  nvr:
    enabled: true
    poll_interval_ms: 5000
    max_inflight_nvrs: 50
    nats_subject: "events.nvr"
```

### Manual Verification Scripts
Verify NVR connectivity and health:
```powershell
# Verify NVR CRUD and Event Stream
.\verify_nvr.ps1

# Verify NVR Health Monitoring
.\verify_nvr_health.ps1
```

## 5. Verification & Testing

### Go Verification Suite
Run all unit tests for Phase 2 components:
```powershell
# Paths & Security
go test -v ./internal/platform/paths

# WMI Discovery
go test -v ./internal/platform/windows

# API Handlers
go test -v ./internal/api -run TestWindowsDiscovery
```

### Firewall Dry-Run Test
Validate naming conventions and orchestration logic without applying changes:
```powershell
.\scripts\test-firewall.ps1 -SkipAdmin
```

## 6. Binary Compilation
Rebuild the server with Phase 2 integrations:
```powershell
go build -v -o server.exe ./cmd/server
```

## 7. Troubleshooting
- **Discovery Timeout**: Ensure `powershell.exe` is in the system PATH and not blocked by execution policy.
- **Firewall Naming Mismatch**: Verify the script is running with `-ConfigPath` pointing to a valid `default.yaml` if custom ports are used.
- **NVR Poll Failures**: Check NATS server connectivity and verify vendor-specific credentials via the `/test-connection` API.
