# deploy-to-prod.ps1
# Automates the movement of binaries to $env:ProgramFiles\TechnoSupport\VMS 
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
$DevRoot = Split-Path $PSScriptRoot -Parent
$InstallRoot = "$env:ProgramFiles\TechnoSupport\VMS"
$SvcManager = Join-Path $PSScriptRoot "service-manager.ps1"

# Binary Sources
$ControlSrc = Join-Path $DevRoot "vms-control.exe"
if (-not (Test-Path $ControlSrc)) { $ControlSrc = Join-Path $DevRoot "bin\vms-control.exe" }
if (-not (Test-Path $ControlSrc)) { $ControlSrc = Join-Path $DevRoot "server.exe" }

$MediaSrc = Join-Path $DevRoot "media-plane\build\Release\vms-media.exe"
$SfuSrcDir = Join-Path $DevRoot "sfu"


$NodeExeSrc = Get-Command node.exe -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source
if (!$NodeExeSrc) { $NodeExeSrc = "$env:ProgramFiles\nodejs\node.exe" }

Write-Host "--- Starting Deployment to $InstallRoot ---" -ForegroundColor Cyan

# 1. Create Directories
if (-not (Test-Path $InstallRoot)) {
    New-Item -ItemType Directory -Path $InstallRoot -Force | Out-Null
}

# 2. Stop Services (if running)
if (Test-Path $SvcManager) {
    Write-Host "Stopping existing services..."
    powershell -ExecutionPolicy Bypass -File $SvcManager Stop
}

# 3. Copy Binaries
Write-Host "Copying Control Plane..."
if (Test-Path $ControlSrc) {
    Copy-Item $ControlSrc (Join-Path $InstallRoot "server.exe") -Force
} else {
    Write-Warning "Control Plane binary not found at $ControlSrc"
}

Write-Host "Copying Media Plane..."
if (Test-Path $MediaSrc) {
    Copy-Item $MediaSrc (Join-Path $InstallRoot "vms-media.exe") -Force
} else {
    Write-Warning "Media Plane binary not found at $MediaSrc"
}

Write-Host "Copying Node.js runtime..."
if (Test-Path $NodeExeSrc) {
    Copy-Item $NodeExeSrc (Join-Path $InstallRoot "node.exe") -Force
}

Write-Host "Copying SFU Service (dist/node_modules)..."
$SfuDest = Join-Path $InstallRoot "sfu"
if (Test-Path $SfuSrcDir) {
    if (-not (Test-Path $SfuDest)) { New-Item -ItemType Directory -Path $SfuDest -Force | Out-Null }
    if (Test-Path (Join-Path $SfuSrcDir "dist")) {
        Copy-Item -Path (Join-Path $SfuSrcDir "dist") -Destination $SfuDest -Recurse -Force
    }
    if (Test-Path (Join-Path $SfuSrcDir "node_modules")) {
        Copy-Item -Path (Join-Path $SfuSrcDir "node_modules") -Destination $SfuDest -Recurse -Force
    }
}

# 4. Install & Start
if (Test-Path $SvcManager) {
    Write-Host "Registering Services..." -ForegroundColor Yellow
    powershell -ExecutionPolicy Bypass -File $SvcManager Install
    
    Write-Host "Starting VMS Stack..." -ForegroundColor Green
    powershell -ExecutionPolicy Bypass -File $SvcManager Start
}

Write-Host "--- Deployment Complete ---" -ForegroundColor Green
pause

