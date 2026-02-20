# SOP: Quick Start - Clone, Setup, and Run

**Document ID:** SOP-QUICK-001  
**Target Audience:** Developers / New Users  
**Version:** 1.0  

---

## 1. Prerequisites (Must be installed first)

Ensure the following software is installed on your Windows machine:

1.  **Go 1.25+**: [Download here](https://go.dev/dl/)
2.  **Node.js 20+**: [Download here](https://nodejs.org/)
3.  **.NET 8 SDK**: [Download here](https://dotnet.microsoft.com/en-us/download/dotnet/8.0)
4.  **PostgreSQL 14+**: Install and ensure the service is running.
5.  **Redis (Stack or Open Source)**: Install via MSI or Memurai for Windows.
6.  **GStreamer (MSVC 64-bit)**: [Download here](https://gstreamer.freedesktop.org/download/#windows)
    *   **CRITICAL**: Choose "Complete Install" or ensure the `Developer` and `Gst-MSVC` packages are selected.
    *   Add `C:\gstreamer\1.0\msvc_x86_64\bin` to your system's Environment Variable **PATH**.

---

## 2. Setting Up the Database

1.  Open your PostgreSQL client (like pgAdmin or psql).
2.  Create a database named: `ts_vms`.
3.  Open a terminal in the project root and run the migrations:
    ```powershell
    .\migrator.exe -up
    ```
    *This creates all necessary tables and security policies.*

---

## 3. Building the Components

From the project root, run these commands to prepare the binaries:

### 3.1 Backend & HLS Daemon
```powershell
go build -o bin/vms-control.exe ./cmd/server
go build -o bin/vms-hlsd.exe ./cmd/hlsd
```

### 3.2 SFU Service (WebRTC Engine)
```powershell
cd sfu
npm install
npm run build
cd ..
```

### 3.3 Desktop Application
```powershell
cd desktop\TSVmsDesktop
dotnet build
cd ..\..
```

---

## 4. Running the System

### 4.1 Start the Backend Infrastructure
Run the following script to start Redis, NATS, and all background plane services:
```powershell
.\scripts\dev-restart.ps1
```
*Tip: Check the `logs/` folder to ensure everything says "Started successfully".*

### 4.2 Start the Desktop Client
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
*   **App won't build?**: Ensure you have the C++ Desktop Development workload installed in **Visual Studio 2022**.
