# How to Check Live Feed - Quick Guide

## Method 1: HLS Stream (Recommended for Testing)

The camera is generating HLS segments that you can play directly.

### Find the HLS Playlist:
```powershell
# Get the session ID from camera status
$env:CAM_ID = "6ed6cf65-a421-4f5f-bfa3-363f33dbf23a"
go run scripts/check_camera_status.go
# Look for "Session ID: xxxxx"

# Or find it directly
Get-ChildItem "C:\ProgramData\TechnoSupport\VMS\hls\" -Directory | Select-Object Name, CreationTime
```

### Play the HLS Stream:
```powershell
# Example if session ID is "tOOINOtwCZLZ"
# Open in VLC or any HLS player:
# File path: C:\ProgramData\TechnoSupport\VMS\hls\tOOINOtwCZLZ\playlist.m3u8

# Or use ffplay (if installed):
ffplay "C:\ProgramData\TechnoSupport\VMS\hls\<SESSION_ID>\playlist.m3u8"
```

## Method 2: WebRTC Stream (Browser)

The VMS has a web interface for live viewing.

### Steps:
1. **Get an access token:**
   ```powershell
   .\scripts\gen-admin-token.ps1
   # Token is saved to token.txt and copied to clipboard
   ```

2. **Open the web interface:**
   - URL: `http://localhost:8080` (or your server IP)
   - Login with the token

3. **Navigate to Live View:**
   - Go to Cameras → Live View
   - Select camera `6ed6cf65-a421-4f5f-bfa3-363f33dbf23a`

## Method 3: Direct RTSP Stream

You can view the original RTSP stream directly (bypasses VMS).

### Using VLC:
```
Media → Open Network Stream
URL: rtsp://192.168.1.181:554/live/0/MAIN
```

### Using ffplay:
```powershell
ffplay -rtsp_transport tcp rtsp://192.168.1.181:554/live/0/MAIN
```

## Method 4: Check HLS via HTTP (if HLSD is configured)

If the HLS daemon is serving files via HTTP:

```powershell
# Check if HLSD is serving
Invoke-WebRequest http://localhost:8088/health

# Access HLS stream
# http://localhost:8088/hls/<SESSION_ID>/playlist.m3u8
```

## Quick Test Script

I'll create a script to help you quickly access the live feed:
