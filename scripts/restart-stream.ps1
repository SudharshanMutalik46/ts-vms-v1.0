# restart-stream.ps1
$ErrorActionPreference = "Continue"

$Root = Split-Path $PSScriptRoot -Parent
$LogFile = Join-Path $Root "gst_launch.log"

# Use environment variable for test source, fallback to a public demo stream if not set
$Source = $env:VMS_TEST_RTSP_URL
if (!$Source) {
    Write-Host "VMS_TEST_RTSP_URL not set. Using built-in pattern generator." -ForegroundColor Gray
    $Source = "videotestsrc is-live=true ! x264enc bitrate=1000 tune=zerolatency ! rtph264pay"
}

Write-Host "Starting GStreamer Test Stream..." -ForegroundColor Yellow
Stop-Process -Name "gst-launch-1.0" -Force -ErrorAction SilentlyContinue

Start-Sleep -Seconds 1

# If it's a real URL, we use rtspsrc. If it's a pattern, we use the string directly.
if ($Source -match "^rtsp://") {
    $pipeline = "rtspsrc location=$Source latency=200 ! decodebin ! videoconvert ! autovideosink"
} else {
    $pipeline = "$Source ! decodebin ! videoconvert ! autovideosink"
}

Start-Process "gst-launch-1.0" -ArgumentList $pipeline -WindowStyle Hidden -RedirectStandardOutput $LogFile -RedirectStandardError $LogFile

Write-Host "Stream started with pipeline: $pipeline"
