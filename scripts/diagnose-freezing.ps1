# diagnose-freezing.ps1
# Diagnose video freezing issues

param(
    [string]$CameraId = "6ed6cf65-a421-4f5f-bfa3-363f33dbf23a"
)

Write-Host "`n=== Video Freezing Diagnostics ===" -ForegroundColor Cyan
Write-Host "Camera ID: $CameraId`n" -ForegroundColor Yellow

# 1. Check Media Plane Stats
Write-Host "[1] Media Plane Performance:" -ForegroundColor Green
try {
    $metrics = Invoke-WebRequest -Uri "http://localhost:8080/metrics" -UseBasicParsing
    
    # FPS
    $fpsPattern = 'vms_media_ingest_latency_ms\{camera_id="' + $CameraId + '"\}\s+([0-9\.]+)'
    if ($metrics.Content -match $fpsPattern) {
        Write-Host "  Latency: $($matches[1])ms" -ForegroundColor White
    }
    
    # Frames dropped
    $droppedPattern = 'vms_media_frames_dropped_total\{camera_id="' + $CameraId + '"\}\s+([0-9\.]+)'
    if ($metrics.Content -match $droppedPattern) {
        $dropped = $matches[1]
        Write-Host "  Frames Dropped: $dropped" -ForegroundColor $(if ($dropped -gt 0) { "Yellow" } else { "White" })
    }
    
    # Bitrate
    $bitratePattern = 'vms_media_bitrate_bps\{camera_id="' + $CameraId + '"\}\s+([0-9\.]+)'
    if ($metrics.Content -match $bitratePattern) {
        $bitrate = [math]::Round($matches[1] / 1000000, 2)
        Write-Host "  Bitrate: ${bitrate} Mbps" -ForegroundColor White
    }
    
    # Restarts
    $restartsPattern = 'vms_media_restarts_total\{camera_id="' + $CameraId + '"\}\s+([0-9\.]+)'
    if ($metrics.Content -match $restartsPattern) {
        $restarts = $matches[1]
        Write-Host "  Pipeline Restarts: $restarts" -ForegroundColor $(if ($restarts -gt 0) { "Red" } else { "White" })
    }
}
catch {
    Write-Host "  [FAIL] Could not fetch metrics" -ForegroundColor Red
}

# 2. Check SFU Stats
Write-Host "`n[2] SFU Performance:" -ForegroundColor Green
try {
    $sfuStats = Invoke-RestMethod -Uri "http://127.0.0.1:8085/stats" -Headers @{"X-Internal-Auth" = "sfu-internal-secret" }
    Write-Host "  Rooms: $($sfuStats.totals.rooms)" -ForegroundColor White
    Write-Host "  Viewers: $($sfuStats.totals.viewers)" -ForegroundColor White
    Write-Host "  Bytes In: $([math]::Round($sfuStats.totals.bytes_in / 1MB, 2)) MB" -ForegroundColor White
    Write-Host "  Bytes Out: $([math]::Round($sfuStats.totals.bytes_out / 1MB, 2)) MB" -ForegroundColor White
}
catch {
    Write-Host "  [FAIL] Could not fetch SFU stats" -ForegroundColor Red
}

# 3. Check Recent Media Logs
Write-Host "`n[3] Recent Media Plane Events:" -ForegroundColor Green
$mediaLogs = Get-Content logs/media.log -Tail 20 | Select-String -Pattern "stopping|starting|error|warn" -CaseSensitive:$false
if ($mediaLogs) {
    $mediaLogs | ForEach-Object {
        $line = $_.Line
        if ($line -match "error") {
            Write-Host "  [ERROR] $line" -ForegroundColor Red
        }
        elseif ($line -match "warn") {
            Write-Host "  [WARN] $line" -ForegroundColor Yellow
        }
        else {
            Write-Host "  $line" -ForegroundColor Gray
        }
    }
}
else {
    Write-Host "  No recent issues found" -ForegroundColor Green
}

# 4. Check System Resources
Write-Host "`n[4] System Resources:" -ForegroundColor Green
$mediaProc = Get-Process vms-media -ErrorAction SilentlyContinue
if ($mediaProc) {
    Write-Host "  Media Plane CPU: $([math]::Round($mediaProc.CPU, 2))s" -ForegroundColor White
    Write-Host "  Media Plane Memory: $([math]::Round($mediaProc.WorkingSet64/1MB, 2)) MB" -ForegroundColor White
}

$sfuProc = Get-Process node -ErrorAction SilentlyContinue | Where-Object { $_.Id -eq 424 }
if ($sfuProc) {
    Write-Host "  SFU CPU: $([math]::Round($sfuProc.CPU, 2))s" -ForegroundColor White
    Write-Host "  SFU Memory: $([math]::Round($sfuProc.WorkingSet64/1MB, 2)) MB" -ForegroundColor White
}

# 5. Recommendations
Write-Host "`n[5] Common Causes & Fixes:" -ForegroundColor Green
Write-Host "  Cause 1: Network congestion or packet loss" -ForegroundColor Cyan
Write-Host "    Fix: Check network connection to camera (192.168.1.181)" -ForegroundColor White
Write-Host ""
Write-Host "  Cause 2: Camera keyframe interval too high" -ForegroundColor Cyan
Write-Host "    Fix: Set camera to send keyframes every 1-2 seconds" -ForegroundColor White
Write-Host ""
Write-Host "  Cause 3: Browser WebRTC buffer issues" -ForegroundColor Cyan
Write-Host "    Fix: Try refreshing the page or use HLS fallback" -ForegroundColor White
Write-Host ""
Write-Host "  Cause 4: SFU egress restarting (viewer reconnecting)" -ForegroundColor Cyan
Write-Host "    Fix: This is normal when clicking Start View multiple times" -ForegroundColor White

Write-Host "`n=== End Diagnostics ===`n" -ForegroundColor Cyan
