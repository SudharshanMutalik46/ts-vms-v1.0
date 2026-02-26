$ErrorActionPreference = "Continue"

Write-Host "Stopping Services..." -ForegroundColor Yellow
Stop-Process -Name "vms-mosaic", "vms-control", "server", "vms-media", "vms-hlsd", "node", "vms-ai", "nats-server" -Force -ErrorAction SilentlyContinue

Start-Sleep -Seconds 2

$Root = "c:\Users\sudha\Desktop\ts_vms_1.0"
$LogDir = "$Root\logs"
if (!(Test-Path $LogDir)) { New-Item -ItemType Directory -Path $LogDir | Out-Null }

# DB Config
$env:DB_HOST = "localhost"
$env:DB_PORT = "5432"
$env:DB_USER = "postgres"
$env:DB_PASSWORD = "ts1234"
$env:DB_NAME = "ts_vms"
$env:REDIS_ADDR = "127.0.0.1:6379"
$env:NATS_URL = "nats://localhost:4222"
$env:SFU_BASE_URL = "http://127.0.0.1:8085"
$env:MEDIA_PLANE_ADDR = "localhost:50051"
$env:METRICS_PER_CAMERA = "true"
$env:MASTER_KEYS = '[{"kid":"dev-1","material":"MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE="}]'
$env:ACTIVE_MASTER_KID = "dev-1"
$env:AI_SERVICE_TOKEN = "dev_ai_secret"
$env:SFU_SECRET = "sfu-internal-secret"

Write-Host "Starting Redis..." -ForegroundColor Cyan
if (!(Get-Process redis-server -ErrorAction SilentlyContinue)) {
    Start-Process "c:\Users\sudha\Downloads\Redis-x64-5.0.14.1\redis-server.exe" -WindowStyle Hidden -RedirectStandardOutput "$LogDir\redis.log" -RedirectStandardError "$LogDir\redis_err.log"
}

Write-Host "Starting NATS..." -ForegroundColor Cyan
Start-Process "$Root\src\vms-ai\nats-server.exe" -WindowStyle Minimized

Start-Sleep -Seconds 2

Write-Host "Rebuilding Backend Server (Phase 2)..." -ForegroundColor DarkGray
if (Test-Path "$Root\server.exe") { Remove-Item "$Root\server.exe" -Force }
pushd $Root
go build -o server.exe ./cmd/server
popd

if (Test-Path "$Root\server.exe") {
    $env:PORT = "8080"
    Start-Process "$Root\server.exe" -WorkingDirectory $Root -WindowStyle Hidden -RedirectStandardOutput "$LogDir\server.log" -RedirectStandardError "$LogDir\server_err.log"
}
else {
    Write-Error "Failed to build server.exe"
}

Start-Sleep -Seconds 2

Write-Host "Starting Media Plane..." -ForegroundColor Cyan
$MediaExe = "$Root\media-plane\build\Release\vms-media.exe"
if (Test-Path $MediaExe) {
    # Run from Release dir to find DLLs if needed, or Root? 
    # Usually config is in Root. Media Plane needs config.
    # It takes --config arg.
    Start-Process $MediaExe -ArgumentList "--config `"$Root\config.yaml`"" -WorkingDirectory "$Root\media-plane\build\Release" -WindowStyle Hidden -RedirectStandardOutput "$LogDir\media.log" -RedirectStandardError "$LogDir\media_err.log"
}
else {
    Write-Error "vms-media.exe not found at $MediaExe"
}

Write-Host "Starting Mosaic Server..." -ForegroundColor Cyan
$MosaicExe = "$Root\media-plane\src\mosaic\Debug\vms-mosaic.exe"
if (Test-Path $MosaicExe) {
    Start-Process $MosaicExe -ArgumentList "`"$Root\config\mosaic_8x8.yaml`"" -WorkingDirectory "$Root\media-plane\src\mosaic\Debug" -WindowStyle Hidden -RedirectStandardOutput "$LogDir\mosaic.log" -RedirectStandardError "$LogDir\mosaic_err.log"
}
else {
    Write-Warning "vms-mosaic.exe not found at $MosaicExe. Skipping."
}

Write-Host "Starting SFU..." -ForegroundColor Cyan
Start-Process powershell -WindowStyle Hidden -ArgumentList "-Command", "`$env:SFU_SECRET='sfu-internal-secret'; `$env:PORT='8085'; Set-Location 'c:\Users\sudha\Desktop\ts_vms_1.0\sfu'; node dist/main.js > '..\logs\sfu.log' 2>&1"

Write-Host "Starting AI Service (Go Mock)..." -ForegroundColor Cyan
Start-Process powershell -WindowStyle Hidden -ArgumentList "-Command", "`$env:NATS_URL='nats://localhost:4222'; `$env:CP_BASE_URL='http://localhost:8080'; Set-Location 'c:\Users\sudha\Desktop\ts_vms_1.0'; go run ./cmd/ai-service > 'logs\ai_mock.log' 2>&1"

Write-Host "Starting HLSD..." -ForegroundColor Cyan
Write-Host "Rebuilding HLSD..." -ForegroundColor DarkGray
if (Test-Path "$Root\bin\vms-hlsd.exe") { Remove-Item "$Root\bin\vms-hlsd.exe" -Force }
pushd $Root
go build -o bin/vms-hlsd.exe ./cmd/hlsd
popd

if (Test-Path "$Root\bin\vms-hlsd.exe") {
    Start-Process "$Root\bin\vms-hlsd.exe" -WorkingDirectory $Root -WindowStyle Hidden -RedirectStandardOutput "$LogDir\hlsd.log" -RedirectStandardError "$LogDir\hlsd_err.log"
}
else {
    Write-Error "Failed to build vms-hlsd.exe"
}

Write-Host "All Services Restarted." -ForegroundColor Green
Get-Process -Name "vms-mosaic", "server", "vms-control", "vms-media", "vms-hlsd", "node", "vms-ai", "nats-server" -ErrorAction SilentlyContinue | Format-Table Id, ProcessName, StartTime
