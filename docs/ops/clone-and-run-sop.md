# SOP: Quick Start - Clone, Setup, and Run

**Document ID:** SOP-QUICK-001  
**Target Audience:** Developers / New Users  
**Version:** 1.0  

---

## 1. Prerequisites (Must be installed first)

Ensure the following software is installed on your Windows machine:

1.  **Git**: [Download here](https://git-scm.com/download/win).
2.  **Go 1.25+**: [Download here](https://go.dev/dl/).
3.  **Node.js 20+ (LTS)**: [Download here](https://nodejs.org/).
4.  **.NET 8 SDK**: [Download here](https://dotnet.microsoft.com/en-us/download/dotnet/8.0).
5.  **PostgreSQL 14+**: [Download here](https://www.postgresql.org/download/windows/).
6.  **Redis 5.0+**: [Download here](https://github.com/tporadowski/redis/releases) or [Memurai for Windows](https://www.memurai.com/get-memurai).
7.  **NATS Server**: Messaging broker. [Download here](https://github.com/nats-io/nats-server/releases).
8.  **Visual Studio 2022**: [Download here](https://visualstudio.microsoft.com/downloads/).
    *   **REQUIRED**: Select the "Desktop development with C++" workload during installation.
9.  **CMake 3.25+**: Required for Media Plane build. [Download here](https://cmake.org/download/).
10. **GStreamer 1.22+ (MSVC 64-bit)**: [Download here](https://gstreamer.freedesktop.org/download/#windows) (**Complete Install** required).
    *   Add `C:\gstreamer\1.0\msvc_x86_64\bin` to your system's Environment Variable **PATH**.

---

## 2. Setting Up the Database

1.  Open your PostgreSQL client (like pgAdmin or psql).
2.  Create a database named: `ts_vms`.
3.  Build and run the migrations:
    ```powershell
    go build -o migrator.exe ./cmd/migrator
    .\migrator.exe -up
    ```
    *This creates all necessary tables and security policies.*

---

## 3. Building the Components

From the project root, run these commands to prepare the binaries:

### 3.1 Backend & Recording Engine
```powershell
# Backend Server
go build -o server.exe ./cmd/server

# Recording Engine
go build -o bin/vms-recording-bin.exe ./cmd/vms-recording
```

### 3.2 Media Plane (Native C++)
Ensure you have CMake installed or use Visual Studio to build the `media-plane` solution.
```powershell
cd media-plane
mkdir build
cd build
cmake ..
cmake --build . --config Release
cd ..\..
```

### 3.3 SFU Service (WebRTC Engine)
```powershell
cd sfu
npm install
npm run build
cd ..
```

### 3.4 Desktop Application
```powershell
cd desktop\TSVmsDesktop
dotnet build
cd ..\..
```

---

## 4. Running the System

### 4.1 Master Restart Script
The easiest way to start all backend services (Redis, NATS, Backend, Media Plane, SFU, Recording) is using the provided master script:
```powershell
.\scripts\dev-restart.ps1
```
*Tip: Check the `logs/` folder to ensure all services started successfully. Each service has its own `.log` file.*

### 4.2 Start the Desktop Client
Running the client requires the backend services to be active.
```powershell
cd desktop\TSVmsDesktop
dotnet run
```

---

## 5. First-Time Login

Use the following default administrator credentials:
*   **Email**: `admin@technosupport.com`
*   **Password**: `ts1234` (or the password configured in your `.env`)

---

## 6. Troubleshooting
*   **Video is black/frozen?**: Check if GStreamer is in your PATH. Run `gst-launch-1.0 --version` in CMD to verify.
*   **Database error?**: Ensure PostgreSQL service is running and the user `postgres` has permission to the `ts_vms` database.
*   **C++ Build fails?**: Ensure "Desktop development with C++" is installed in Visual Studio 2022.
*   **SFU Error?**: Ensure `node` is in your PATH and `npm install` was successful in the `sfu/` directory.
*   **NATS/Redis Connection Refused?**: Verify both services are running. `dev-restart.ps1` tries to start them, but if they are already running on different ports, it might fail.
