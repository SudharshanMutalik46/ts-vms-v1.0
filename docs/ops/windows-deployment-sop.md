# SOP: Windows Native Deployment

**Document ID:** SOP-MASTER-003  
**Version:** 1.0  
**Last Updated:** 2026-02-04

## 1. Objective
This document details the procedure for building, deploying, and running the Techno Support VMS entirely on native Windows, eliminating Docker dependencies for core services.

## 2. Prerequisites

### Software Requirements
Ensure the following tools are installed and added to your System PATH:

1.  **Go 1.25+** (Control Plane & HLS Daemon)
2.  **Node.js 20+ & npm** (SFU Service)
3.  **Visual Studio 2022** with C++ Desktop Development workload (Media Plane)
4.  **CMake 3.20+** (Media Plane Build)
5.  **GStreamer 1.24+ (MSVC 64-bit)** (Media Plane Runtime)
    *   **Crucial:** Install "Complete" or ensure `d3d11` and `openh264` plugins are selected.
    *   Add `C:\gstreamer\1.0\msvc_x86_64\bin` to PATH.
6.  **PostgreSQL 14+** (Local Service)
7.  **Redis 6+** (Local Service or Memurai)

### Environment Variables
The following environment variables are required for the build and runtime scripts.
(Note: `scripts/dev-restart.ps1` sets defaults for Development).

| Variable | Default (Dev) | Description |
| :--- | :--- | :--- |
| `DB_HOST` | `localhost` | Postgres Host |
| `DB_PORT` | `5432` | Postgres Port |
| `DB_USER` | `postgres` | Postgres User |
| `DB_PASSWORD` | `ts1234` | Postgres Password |
| `DB_NAME` | `ts_vms` | Database Name |
| `REDIS_ADDR` | `localhost:6379` | Redis Address |
| `SFU_BASE_URL` | `http://localhost:8085` | Internal SFU URL |
| `MEDIA_PLANE_ADDR` | `localhost:50051` | Media Plane gRPC Address |

## 3. Build Instructions

The VMS consists of three core binaries that must be compiled.

### 3.1 Control Plane (Go)
```powershell
go build -o bin/vms-control.exe ./cmd/server
```

### 3.2 Media Plane (C++)
The Media Plane uses CMake and vcpkg for dependency management.
```powershell
# Navigate to media-plane directory
cd media-plane

# Configure (First time only)
cmake -B build -S . -DCMAKE_TOOLCHAIN_FILE=C:/vcpkg/scripts/buildsystems/vcpkg.cmake

# Build
cmake --build build --config Release

# Copy binary to root bin/
Copy-Item build/Release/vms-media.exe ../bin/ -Force
```

### 3.3 SFU (Node.js/TypeScript)
```powershell
cd sfu
npm install
npm run build
```

### 3.4 HLS Daemon (Go)
```powershell
go build -o bin/vms-hlsd.exe ./cmd/hlsd
```

### 3.5 AI Service (Go Mock Fallback)
Due to current DLL conflicts in the native C++ build, the system is configured to use the Go Mock AI by default.
```powershell
go build -o bin/vms-ai-mock.exe ./cmd/ai-service
```
*(Note: `dev-restart.ps1` runs this via `go run` automatically)*

## 4. Running the System

### Option A: Development Mode (Recommended for Testing)
Use the provided PowerShell script to restart all services in console mode. This script handles environment variables and proper shutdown of previous instances.

```powershell
.\scripts\dev-restart.ps1
```

**What this does:**
1.  Stops existing `vms-*` processes.
2.  Sets environment variables (DB, Redis, Secrets).
3.  Launches `vms-control`, `vms-media`, `vms-hlsd`, and `sfu` (node) in the background.
4.  Redirects logs to `logs/*.log`.

### Option B: Production (Windows Services)
For permanent deployment, install the components as Windows Services.

1.  **Install Services**:
    ```powershell
    .\scripts\service-manager.ps1 -Action Install
    ```
2.  **Start Services**:
    ```powershell
    .\scripts\service-manager.ps1 -Action Start
    ```
3.  **Check Status**:
    ```powershell
    .\scripts\service-manager.ps1 -Action Status
    ```

## 5. Directory Structure
The application uses standard Windows paths:
*   **Binaries**: `C:\Program Files\TechnoSupport\VMS` (or local `bin/` in Dev)
*   **Config/Data**: `C:\ProgramData\TechnoSupport\VMS`
*   **Logs**: `C:\ProgramData\TechnoSupport\VMS\logs`

## 6. Troubleshooting Common Issues

*   **"Consume Failed" / 404 Error**: Ensure `vms-control.exe` is RECOMPILED after any API route changes. `dev-restart.ps1` does NOT recompile Go code.
*   **GStreamer Plugin Missing**: Ensure `d3d11` and `openh264` are present in `gst-inspect-1.0`. Only MSVC builds of GStreamer are supported.
*   **SFU Connection Refused**: Ensure `sfu` service is running (`npm start`) and listening on 8085. Check `logs/sfu.log`.
*   **Database Connectivity**: Verify PostgreSQL service is running and `DB_PASSWORD` matches `dev-restart.ps1` settings.
