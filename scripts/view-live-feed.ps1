# view-live-feed.ps1
# Quick script to view the live camera feed

param(
    [string]$CameraId = "6ed6cf65-a421-4f5f-bfa3-363f33dbf23a"
)

Write-Host "`n=== Live Feed Viewer ===" -ForegroundColor Cyan
Write-Host "Camera ID: $CameraId`n" -ForegroundColor Yellow

# Get camera status and session ID
Write-Host "[1] Getting camera status..." -ForegroundColor Green
$env:CAM_ID = $CameraId
$status = go run scripts/check_camera_status.go 2>&1 | Out-String

if ($status -match "Session ID: (\w+)") {
    $sessionId = $matches[1]
    Write-Host "  Session ID: $sessionId" -ForegroundColor White
    
    # Check if HLS files exist
    $hlsPath = "C:\ProgramData\TechnoSupport\VMS\hls\$sessionId"
    $playlistPath = "$hlsPath\playlist.m3u8"
    
    if (Test-Path $playlistPath) {
        Write-Host "`n[2] HLS Stream Available!" -ForegroundColor Green
        Write-Host "  Path: $playlistPath" -ForegroundColor White
        
        # Count segments
        $segments = Get-ChildItem "$hlsPath\*.ts" -ErrorAction SilentlyContinue
        if ($segments) {
            Write-Host "  Segments: $($segments.Count) files" -ForegroundColor White
            $latestSegment = $segments | Sort-Object LastWriteTime -Descending | Select-Object -First 1
            $age = (Get-Date) - $latestSegment.LastWriteTime
            Write-Host "  Latest segment: $([math]::Round($age.TotalSeconds, 1))s ago" -ForegroundColor White
        }
        
        Write-Host "`n[3] How to view:" -ForegroundColor Green
        Write-Host "  Option A - VLC Player:" -ForegroundColor Cyan
        Write-Host "    1. Open VLC" -ForegroundColor White
        Write-Host "    2. Media → Open File" -ForegroundColor White
        Write-Host "    3. Browse to: $playlistPath" -ForegroundColor Yellow
        
        Write-Host "`n  Option B - FFplay (if installed):" -ForegroundColor Cyan
        Write-Host "    ffplay `"$playlistPath`"" -ForegroundColor Yellow
        
        Write-Host "`n  Option C - Open folder:" -ForegroundColor Cyan
        Write-Host "    explorer `"$hlsPath`"" -ForegroundColor Yellow
        
        # Ask if user wants to open
        Write-Host "`n"
        $choice = Read-Host "Open HLS folder in Explorer? (y/n)"
        if ($choice -eq 'y') {
            explorer $hlsPath
        }
        
    }
    else {
        Write-Host "`n[2] HLS files not found at: $hlsPath" -ForegroundColor Red
        Write-Host "  Camera may still be starting up. Wait a few seconds and try again." -ForegroundColor Yellow
    }
    
}
else {
    Write-Host "  [FAIL] Could not get camera session ID" -ForegroundColor Red
    Write-Host "  Camera may not be running. Check with: .\scripts\check-camera.ps1" -ForegroundColor Yellow
}

Write-Host "`n[4] Alternative: Direct RTSP Stream" -ForegroundColor Green
Write-Host "  URL: rtsp://192.168.1.181:554/live/0/MAIN" -ForegroundColor Yellow
Write-Host "  Open in VLC: Media → Open Network Stream" -ForegroundColor White

Write-Host "`n=== End ===`n" -ForegroundColor Cyan
