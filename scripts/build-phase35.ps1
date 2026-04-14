# build-phase35.ps1
$ErrorActionPreference = "Stop"
$Root = Split-Path $PSScriptRoot -Parent

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
if (Test-Path "$Root\media-plane\build") {
    Push-Location "$Root\media-plane\build"
    cmake --build . --config Release
    if ($LASTEXITCODE -ne 0) { Write-Error "Media Plane Build Failed"; exit 1 }
    Pop-Location
} else {
    Write-Warning "media-plane\build directory not found. Skipping CMake build."
}

# 4. Build SFU
Write-Host "Building SFU (TypeScript)..." -ForegroundColor Yellow
if (Test-Path "$Root\sfu") {
    Push-Location "$Root\sfu"
    npm run build
    if ($LASTEXITCODE -ne 0) { Write-Error "SFU Build Failed"; exit 1 }
    Pop-Location
} else {
    Write-Warning "sfu directory not found. Skipping SFU build."
}

# 5. Restart Services
Write-Host "Starting Services..." -ForegroundColor Green
$DevRestartScript = Join-Path $PSScriptRoot "dev-restart.ps1"
if (Test-Path $DevRestartScript) {
    & $DevRestartScript
} else {
    Write-Warning "dev-restart.ps1 not found in scripts folder."
}
