# build-phase35.ps1
$ErrorActionPreference = "Stop"
$Root = "c:\Users\sudha\Desktop\ts_vms_1.0"

Write-Host "=== Phase 3.5 Build & Deploy ===" -ForegroundColor Cyan

# 1. Stop Services
Write-Host "Stopping services..."
Stop-Process -Name "vms-control", "server", "vms-media", "vms-hlsd", "node" -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 2

# 2. Build Control Plane
Write-Host "Building Control Plane (Go)..." -ForegroundColor Yellow
Push-Location $Root
go build -o bin/vms-control.exe ./cmd/server
if ($LASTEXITCODE -ne 0) { Write-Error "Control Plane Build Failed"; exit 1 }
Pop-Location

# 3. Build Media Plane
Write-Host "Building Media Plane (C++ Release)..." -ForegroundColor Yellow
Push-Location "$Root\media-plane\build"
cmake --build . --config Release
if ($LASTEXITCODE -ne 0) { Write-Error "Media Plane Build Failed"; exit 1 }
Pop-Location

# 4. Build SFU
Write-Host "Building SFU (TypeScript)..." -ForegroundColor Yellow
Push-Location "$Root\sfu"
npm run build
if ($LASTEXITCODE -ne 0) { Write-Error "SFU Build Failed"; exit 1 }
Pop-Location

# 5. Restart Services
Write-Host "Starting Services..." -ForegroundColor Green
& "$Root\scripts\dev-restart.ps1"
