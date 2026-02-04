$ErrorActionPreference = "Stop"

# Configuration
$HlsRoot = "C:\ProgramData\TechnoSupport\VMS\hls"
$CameraID = "8b579ed0-aaca-4c19-945c-bf48454b92a6"
$SessionID = [Guid]::NewGuid().ToString("N").Substring(0, 10) # Simulating NanoID
$RtspUrl = "rtsp://192.168.1.7:554/live/stream1"

# Paths
$SessionDir = Join-Path $HlsRoot "live\$CameraID\$SessionID"
$LogFile = "C:\Users\sudha\Desktop\ts_vms_1.0\gst_launch.log"

Write-Host "Starting HLS Stream..."
Write-Host "Camera: $CameraID"
Write-Host "Session: $SessionID"
Write-Host "Output: $SessionDir"

# 1. Kill existing
Write-Host "Killing existing gst-launch..."
Get-Process gst-launch-1.0 -ErrorAction SilentlyContinue | Stop-Process -Force

# 2. Create Directory
if (!(Test-Path $SessionDir)) {
    New-Item -ItemType Directory -Force -Path $SessionDir | Out-Null
}

# 3. Construct GStreamer Command
# Note: hlssink3 sometimes better, but splitmuxsink was used. 
# We use splitmuxsink for playlist generation if configured, but hlssink3 is standard for HLS.
# Based on project stack, we used splitmuxsink with a playlist signal or hlssink3?
# The C++ code used splitmuxsink.
# Let's use hlssink3 for simplicity in this script if available, OR splitmuxsink replicating the command.
# Actually, the user's manual command worked previously.
# Let's try standard hlssink3 first as it handles playlist management automatically.

# Command for hlssink3 (standard HLS)
# location is segment pattern, playlist-location is m3u8 path
$SegmentLocation = "$SessionDir\segment_%05d.mp4"
$PlaylistLocation = "$SessionDir\playlist.m3u8"

$Bt709 = "video/x-raw,colorimetry=bt709"

$Args = @(
    "rtspsrc", "location=$RtspUrl", "latency=200", 
    "!", "rtph264depay", 
    "!", "h264parse", 
    "!", "splitmuxsink", 
    "location=$SegmentLocation", 
    "max-size-time=2000000000", # 2s segments
    "muxer=mp4mux", 
    "template=segment_%05d.mp4" # This might need full path depending on cwd, but let's stick to location
)

# Wait! splitmuxsink does NOT generate playlist.m3u8 automatically unless we enable `generate-playlist` property (if available) or handle messages.
# Standard GStreamer splitmuxsink doesn't generate m3u8 easily from CLI without helper.
# hlssink3 IS designed for this.
# Let's check `gst-inspect-1.0 hlssink3`. If available, use it.
# Assuming it is available (installed via vcpkg/gstreamer).

$GstArgs = "rtspsrc location=$RtspUrl latency=200 ! rtph264depay ! h264parse ! hlssink3 location=$SessionDir\segment_%05d.ts playlist-location=$SessionDir\playlist.m3u8 target-duration=2 max-files=5"

Write-Host "Executing: gst-launch-1.0 $GstArgs"

Start-Process -FilePath "gst-launch-1.0" -ArgumentList $GstArgs -RedirectStandardOutput "${LogFile}_out.txt" -RedirectStandardError "${LogFile}_err.txt" -WindowStyle Hidden

Write-Host "Stream started. Verify at: $SessionDir"
