# VMS System Installation Guide

This document provides a concise summary of how to install and run the Techno Support VMS system locally on a Windows machine for development and testing.

## Prerequisites

Ensure the following tools are downloaded, extracted, and their `bin` directories are added to your System PATH:

1.  **Go 1.25+**
2.  **Node.js 20+ & npm**
3.  **Visual Studio 2022** (with C++ Desktop Development workload)
4.  **CMake 3.20+**
5.  **GStreamer 1.24+ (MSVC 64-bit)** (Must include `d3d11` and `openh264` plugins)
6.  **.NET 8 SDK**
7.  **PostgreSQL 14+**
8.  **Redis** (v3.0.504 portable or newer)
9.  **NATS Server**

## 1. Database & Cache Setup

Before running the VMS, ensure your infrastructure services are running.

1.  **PostgreSQL**: Ensure the PostgreSQL service is running on port `5432`.
    *   Create a database named `ts_vms`.
    *   Default user: `postgres`, password: `ts1234`.
2.  **Redis**: Run the `redis-server.exe` executable. It should listen on port `6379`.
3.  **NATS**: Run the `nats-server.exe` executable. It should listen on port `4222`.

## 2. Compiling the Services

Navigate to the root of the project (`ts_vms_1.0`) and compile the following services.

### Control Plane (Go)
```powershell
go build -o bin/vms-control.exe ./cmd/server
```

### HLS Daemon (Go)
```powershell
go build -o bin/vms-hlsd.exe ./cmd/hlsd
```

### AI Mock Service (Go)
```powershell
go build -o bin/vms-ai-mock.exe ./cmd/ai-service
```

### SFU Service (Node.js)
```powershell
cd sfu
npm install
npm run build
cd ..
```

### Media Plane (C++)
*Note: This step assumes `vcpkg` is installed at `C:/vcpkg`.*
```powershell
cd media-plane
cmake -B build -S . -DCMAKE_TOOLCHAIN_FILE=C:/vcpkg/scripts/buildsystems/vcpkg.cmake
cmake --build build --config Release
Copy-Item build/Release/vms-media.exe ../bin/ -Force
cd ..
```

## 3. Running the Backend Services

To start all backend services simultaneously, use the provided PowerShell helper script from the root directory:

```powershell
.\scripts\dev-restart.ps1
```
*This script will automatically set environment variables, route logs to the `logs/` directory, and launch `vms-control`, `vms-media`, `vms-hlsd`, the `sfu` Node app, and the AI Mock service.*

## 4. Running the Desktop Client

Once the backend services are running, launch the native Windows WPF client:

```powershell
cd desktop\TSVmsDesktop
dotnet build
dotnet run
```

## 5. Stopping the System

When you are finished, gracefully shut down all background VMS processes using:

```powershell
.\scripts\dev-stop.ps1
```
