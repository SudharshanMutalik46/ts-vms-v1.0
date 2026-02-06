# check-camera.ps1
# Check if a specific camera is running

param(
    [string]$CameraId = "6ed6cf65-a421-4f5f-bfa3-363f33dbf23a"
)

Write-Host "`n=== Checking Camera Status ===" -ForegroundColor Cyan
Write-Host "Camera ID: $CameraId`n" -ForegroundColor Yellow

# Method 1: Check via gRPC (Media Plane)
Write-Host "[1] Media Plane Status:" -ForegroundColor Green
$env:CAM_ID = $CameraId
go run scripts/check_camera_status.go

# Method 2: Check Prometheus Metrics
Write-Host "`n[2] Prometheus Metrics:" -ForegroundColor Green
try {
    $metrics = Invoke-WebRequest -Uri "http://localhost:8080/metrics" -UseBasicParsing
    $pattern = 'vms_media_ingest_latency_ms\{camera_id="' + $CameraId + '"\}\s+([0-9\.]+)'
    if ($metrics.Content -match $pattern) {
        $latency = $matches[1]
        Write-Host "  [OK] Camera is RUNNING" -ForegroundColor Green
        Write-Host "  - Latency: ${latency}ms" -ForegroundColor White
        
        # Get additional metrics
        $framesPattern = 'vms_media_frames_processed_total\{camera_id="' + $CameraId + '"\}\s+([0-9\.]+)'
        if ($metrics.Content -match $framesPattern) {
            Write-Host "  - Frames Processed: $($matches[1])" -ForegroundColor White
        }
        $bitratePattern = 'vms_media_bitrate_bps\{camera_id="' + $CameraId + '"\}\s+([0-9\.]+)'
        if ($metrics.Content -match $bitratePattern) {
            $bitrate = [math]::Round($matches[1] / 1000000, 2)
            Write-Host "  - Bitrate: ${bitrate} Mbps" -ForegroundColor White
        }
    }
    else {
        Write-Host "  [FAIL] Camera NOT found in metrics" -ForegroundColor Red
    }
}
catch {
    Write-Host "  [FAIL] Failed to fetch metrics: $_" -ForegroundColor Red
}

# Method 3: Check Database
Write-Host "`n[3] Database Record:" -ForegroundColor Green
$env:PGPASSWORD = 'ts1234'
$dbCheck = psql -h localhost -U postgres -d ts_vms -t -c "SELECT name, is_enabled FROM cameras WHERE id = '$CameraId';"
if ($dbCheck) {
    Write-Host "  [OK] Camera exists in database" -ForegroundColor Green
    Write-Host "  $dbCheck" -ForegroundColor White
}
else {
    Write-Host "  [FAIL] Camera NOT found in database" -ForegroundColor Red
}

Write-Host "`n=== Check Complete ===`n" -ForegroundColor Cyan
