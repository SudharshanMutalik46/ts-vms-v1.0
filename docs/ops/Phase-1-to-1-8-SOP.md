# SOP: Phase 1.1 to 1.8 Deployment

**Document ID:** SOP-MASTER-001  
**Version:** 1.0  
**Last Updated:** 2026-02-01

## 1. Objective
Provide a unified, end-to-end command reference for administrators to deploy and verify Phase 1 of the Techno Support VMS (Phases 1.1 through 1.8).

## 2. Environment Preparation

### System Requirements
- OS: Windows Server 2019+ or Windows 10/11
- Go 1.25+
- PostgreSQL 14+
- Redis 6+

### Configuration (Environment Variables)
Set these in your Administrator PowerShell for the session:
```powershell
$env:DB_HOST = "localhost"
$env:DB_USER = "postgres"
$env:DB_PASSWORD = "your_password"
$env:DB_NAME = "ts_vms"
$env:REDIS_ADDR = "localhost:6379"
$env:JWT_SIGNING_KEY = "your-secure-key"
```

## 3. Phase-by-Phase Execution

### Phase 1.1: Database Initialization
1. **Create Database**:
   ```powershell
   createdb -U postgres ts_vms
   ```
2. **Build Migrator**:
   ```powershell
   go build -v -o migrator.exe ./cmd/migrator
   ```
3. **Apply Migrations**:
   ```powershell
   ./migrator.exe -up
   ```
4. **Verification**:
   ```powershell
   psql -U postgres -d ts_vms -f verification.sql
   ```

### Phase 1.2 - 1.7: Binary Compilation
Build the main VMS Control Plane server:
```powershell
go build -v -o server.exe ./cmd/server
```

### Phase 1.8: Windows Service Installation (Administrator)
1. **Change Directory**:
   ```powershell
   cd .\scripts
   ```
2. **Install SCM Registration**:
   ```powershell
   .\service-manager.ps1 -Action Install
   ```
3. **Verify Registration**:
   ```powershell
   .\service-manager.ps1 -Action Status
   ```
4. **Start the Stack**:
   ```powershell
   .\service-manager.ps1 -Action Start
   ```

## 4. Operational Maintenance

### Service Control
- **Restart**: `.\service-manager.ps1 -Action Restart`
- **Stop**: `.\service-manager.ps1 -Action Stop`
- **Uninstall**: `.\service-manager.ps1 -Action Uninstall`

### Logging & Auditing
- **Event Log**: Check `Windows Event Viewer -> Application` for source `TS-VMS-*`.
- **Audit Spool**: Located at `C:\ProgramData\TechnoSupport\VMS\audit_spool`.
- **Retention**: System enforces a 2557-day (7-year) minimum audit log retention.

### License Management
- **Status**: `GET /api/v1/license/status` (Requires `license.read`)
- **Reload**: `POST /api/v1/license/reload` (Requires `license.manage`)

## 5. Troubleshooting
- **SC Error 1060**: Service not installed. Run `-Action Install`.
- **DB Connection Error**: Verify `$env:DB_PASSWORD` and Postgres service state.
- **Failover Mode**: If "Audit Failover" appears in logs, verify DB disk space or connectivity.
