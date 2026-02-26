$ErrorActionPreference = "Stop"

Write-Host "=== TS-VMS Phase 4.2 DoD Verification ===" -ForegroundColor Cyan
Set-Location -Path "$PSScriptRoot\.."

# 1. Build
Write-Host "Building vms-recording..." -ForegroundColor Yellow
go build -o bin/vms-recording-bin.exe ./cmd/vms-recording

# 2. Run Tests
Write-Host "Running Unit Tests (Scheduler & License)..." -ForegroundColor Yellow
go test ./internal/recording/...

# 3. Start Service in Background
Write-Host "Starting Background Service..." -ForegroundColor Yellow
$process = Start-Process -FilePath ".\bin\vms-recording-bin.exe" -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 3 # Wait for boot

try {
    # 4. Check Health & 24x7 Schedule
    Write-Host "Checking /status (cam_01 should be RECORDING, cam_03 stopped)..." -ForegroundColor Green
    $status = Invoke-RestMethod -Uri "http://localhost:8082/status"
    $status | ConvertTo-Json

    # 5. Trigger Event (RBAC bypass is true in config)
    Write-Host "Triggering alarm on cam_03..." -ForegroundColor Green
    Invoke-RestMethod -Uri "http://localhost:8082/api/v1/recording/trigger?camera_id=cam_03" -Method POST

    Start-Sleep -Seconds 2

    # 6. Verify Event trigger and Quota Denial
    # Note: Config has max_cameras: 2. 
    # Cam 1 (24x7) = 1. Cam 3 (Event) = 1. Limit is 2.
    # Cam 2 (TimeWindow, assuming it falls into the window) will try to start and be THROTTLED.
    Write-Host "Checking /status after event trigger (Quota enforcement)..." -ForegroundColor Green
    $status2 = Invoke-RestMethod -Uri "http://localhost:8082/status"
    $status2 | ConvertTo-Json

    if ($status2.cam_02 -eq "THROTTLED_BY_LICENSE") {
        Write-Host "SUCCESS: Quota enforcement correctly throttled cam_02!" -ForegroundColor Green
    }
    else {
        Write-Host "WARNING: Quota enforcement state unexpected. (May depend on current time/TimeWindow)" -ForegroundColor Yellow
    }

}
finally {
    Write-Host "Cleaning up service..."
    Stop-Process -Id $process.Id -Force
}

Write-Host "Verification Complete!" -ForegroundColor Cyan
