# deploy-to-prod.ps1
# Automates the movement of binaries to C:\Program Files\TechnoSupport\VMS 
# and runs the service manager to install/start the stack.

$ErrorActionPreference = "Stop"

# --- Elevation Check ---
$currentPrincipal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Host "Elevating to Administrator..." -ForegroundColor Yellow
    Start-Process powershell.exe -ArgumentList "-NoProfile -ExecutionPolicy Bypass -File `"$PSCommandPath`"" -Verb RunAs
    exit
}

# --- Configuration ---
$DevRoot = "c:\Users\sudha\Desktop\ts_vms_1.0"
$InstallRoot = "C:\Program Files\TechnoSupport\VMS"
$SvcManager = Join-Path $DevRoot "scripts\service-manager.ps1"

# Binary Sources
$ControlSrc = Join-Path $DevRoot "vms-control.exe"
$MediaSrc = Join-Path $DevRoot "media-plane\build\Release\vms-media.exe"
$SfuSrcDir = Join-Path $DevRoot "sfu"
$NodeExeSrc = "C:\Program Files\nodejs\node.exe"

Write-Host "--- Starting Deployment to $InstallRoot ---" -ForegroundColor Cyan

# 1. Create Directories
if (-not (Test-Path $InstallRoot)) {
    New-Item -ItemType Directory -Path $InstallRoot -Force | Out-Null
}

# 2. Stop Services (if running)
Write-Host "Stopping existing services..."
powershell -ExecutionPolicy Bypass -File $SvcManager Stop

# 3. Copy Binaries
Write-Host "Copying Control Plane..."
Copy-Item $ControlSrc (Join-Path $InstallRoot "server.exe") -Force

Write-Host "Copying Media Plane..."
Copy-Item $MediaSrc (Join-Path $InstallRoot "vms-media.exe") -Force

Write-Host "Copying Node.js runtime..."
Copy-Item $NodeExeSrc (Join-Path $InstallRoot "node.exe") -Force

Write-Host "Copying SFU Service (dist/node_modules)..."
$SfuDest = Join-Path $InstallRoot "sfu"
if (-not (Test-Path $SfuDest)) { New-Item -ItemType Directory -Path $SfuDest -Force | Out-Null }
Copy-Item -Path (Join-Path $SfuSrcDir "dist") -Destination $SfuDest -Recurse -Force
Copy-Item -Path (Join-Path $SfuSrcDir "node_modules") -Destination $SfuDest -Recurse -Force

# 4. Install & Start
Write-Host "Registering Services..." -ForegroundColor Yellow
powershell -ExecutionPolicy Bypass -File $SvcManager Install

Write-Host "Starting VMS Stack..." -ForegroundColor Green
powershell -ExecutionPolicy Bypass -File $SvcManager Start

Write-Host "--- Deployment Complete ---" -ForegroundColor Green
Write-Host "You can now test WebRTC at: http://localhost:8082/test/webrtc_test.html (if served by VMS Web)"
Write-Host "Or open the local file: $DevRoot\test\webrtc_test.html"
pause
