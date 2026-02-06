# Start all VMS Services (Development Mode)

$ErrorActionPreference = "Stop"
$ScriptRoot = $PSScriptRoot
$Root = Resolve-Path "$PSScriptRoot/.."
$UserProfile = $env:USERPROFILE

Write-Host "Starting Techno Support VMS Stack (Phase 3.8)..." -ForegroundColor Cyan

# 0. Redis Server
$RedisPath = "$UserProfile\Downloads\Redis-x64-5.0.14.1\redis-server.exe" 
if (Test-Path $RedisPath) {
    Write-Host "Starting Redis Server..." -ForegroundColor Green
    Start-Process -FilePath $RedisPath
}
else {
    Write-Warning "Redis Server not found at $RedisPath. Please ensure Redis is running."
}

# 1. NATS Server
$NatsPath = "$Root/src/vms-ai/nats-server.exe"
if (Test-Path $NatsPath) {
    Write-Host "Starting NATS Server..." -ForegroundColor Green
    Start-Process -FilePath $NatsPath -ArgumentList "-p 4222" 
}
else {
    Write-Warning "NATS Server not found at $NatsPath"
}

# 2. Media Plane
$MediaPath = "$Root/media-plane/build/Release/vms-media.exe"
if (Test-Path $MediaPath) {
    Write-Host "Starting Media Plane..." -ForegroundColor Green
    Start-Process powershell -ArgumentList "-NoExit", "-Command", "cd '$Root/media-plane'; & '$MediaPath'"
}
else {
    Write-Warning "Media Plane not found at $MediaPath"
}

# 3. Control Plane
Write-Host "Starting Control Plane..." -ForegroundColor Green
Start-Process powershell -ArgumentList "-NoExit", "-Command", "cd '$Root'; & '$Root/scripts/start_server.ps1'"

# 4. AI Service
$AiPath = "$Root/src/vms-ai/build/Release/vms-ai.exe"
if (Test-Path $AiPath) {
    Write-Host "Starting AI Service..." -ForegroundColor Green
    Start-Process powershell -ArgumentList "-NoExit", "-Command", "cd '$Root/src/vms-ai'; & '$AiPath'"
}
else {
    Write-Warning "AI Service binary not found at $AiPath"
}

# 5. HLS Daemon
$HlsPath = "$Root/vms-hlsd.exe"
if (Test-Path $HlsPath) {
    Write-Host "Starting HLS Daemon..." -ForegroundColor Green
    Start-Process powershell -ArgumentList "-NoExit", "-Command", "cd '$Root'; & '$HlsPath'"
}
else {
    Write-Warning "HLS Daemon not found at $HlsPath"
}

# 6. SFU Service (Node.js)
$SfuDir = "$Root/sfu"
if (Test-Path $SfuDir) {
    Write-Host "Starting SFU Service..." -ForegroundColor Green
    # Using 'npm run build; npm start' to avoid ts-node ESM issues in dev
    Start-Process powershell -ArgumentList "-NoExit", "-Command", "cd '$SfuDir'; Write-Host 'Installing...'; npm install; Write-Host 'Building...'; npm run build; Write-Host 'Starting...'; npm start"
}
else {
    Write-Warning "SFU directory not found at $SfuDir"
}

Write-Host "Request to start all services completed." -ForegroundColor Cyan
