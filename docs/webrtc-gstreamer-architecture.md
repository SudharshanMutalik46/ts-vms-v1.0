# WebRTC and GStreamer Architecture Overview

## Current Status

### GStreamer
✅ **Installed**: Version 1.26.10
- **Location**: Embedded in the Media Plane service
- **Usage**: Running inside `vms-media` process (PID 1716)
- **Function**: RTSP ingestion, video decoding, HLS segmentation

**How it works:**
- GStreamer is a **library**, not a separate process
- It's compiled into the `vms-media.exe` C++ application
- Currently processing video from camera `6ed6cf65-a421-4f5f-bfa3-363f33dbf23a`
- Running at ~21 FPS with 0ms latency

### WebRTC (mediasoup)
✅ **Running**: Inside Node.js SFU service (PID 424)
- **Port**: 8085 (HTTP API), 40000-49999 (RTP/UDP)
- **Function**: Real-time video streaming to browsers
- **Workers**: 16 mediasoup workers (one per CPU core)

**How it works:**
- mediasoup is a **Node.js library** embedded in the SFU service
- Creates WebRTC transports for browser clients
- Receives RTP from Media Plane, forwards to browsers
- No separate process - runs inside the `node` process

## Architecture Flow

```
Camera (RTSP)
    ↓
┌─────────────────────────────────────┐
│  vms-media (Media Plane)            │
│  - GStreamer Pipeline                │
│  - RTSP Client → Decoder             │
│  - HLS Segmenter                     │
│  - RTP Sender (to SFU)               │
└─────────────────────────────────────┘
    ↓ (RTP/UDP)
┌─────────────────────────────────────┐
│  node (SFU Service)                  │
│  - mediasoup (WebRTC)                │
│  - 16 Workers                        │
│  - PlainTransport (receives RTP)     │
│  - WebRtcTransport (sends to browser)│
└─────────────────────────────────────┘
    ↓ (WebRTC)
Browser Client
```

## GStreamer Pipeline (Inside vms-media)

The Media Plane uses this GStreamer pipeline:

```
rtspsrc → rtph264depay → h264parse → 
    ├→ tee → queue → hlssink2 (HLS segments)
    └→ queue → rtph264pay → udpsink (to SFU)
```

**Active Pipeline:**
- Input: `rtsp://192.168.1.181:554/live/0/MAIN`
- Output 1: HLS segments in `C:\ProgramData\TechnoSupport\VMS\hls\`
- Output 2: RTP stream to SFU on dynamic port

## WebRTC Components (Inside SFU)

**mediasoup Architecture:**
```
┌─────────────────────────────────────┐
│  Router (per room/camera)            │
│    ├─ PlainTransport (ingest)        │
│    │   └─ Producer (H.264 video)     │
│    └─ WebRtcTransport (viewer)       │
│        └─ Consumer (sends to browser)│
└─────────────────────────────────────┘
```

**Current State:**
- Rooms: 0 (no active viewers)
- Producers: 1 (camera feed)
- Consumers: 0 (no viewers connected)
- Transports: 1 PlainTransport (receiving from Media Plane)

## Verification Commands

### Check GStreamer
```powershell
# Version
gst-inspect-1.0 --version

# Available plugins
gst-inspect-1.0 | Select-String "rtspsrc|h264|hls"

# Check if Media Plane is using GStreamer
Get-Process vms-media | Select-Object CPU, WorkingSet64
```

### Check WebRTC/mediasoup
```powershell
# SFU stats
Invoke-RestMethod -Uri "http://127.0.0.1:8085/stats" -Headers @{"X-Internal-Auth"="sfu-internal-secret"}

# Check mediasoup workers (inside node process)
Get-Process node | Select-Object Id, CPU, @{Name="Threads";Expression={$_.Threads.Count}}
```

### Check Active Streams
```powershell
# Media Plane metrics
Invoke-WebRequest http://localhost:8080/metrics | Select-String "vms_media"

# Check camera status
.\scripts\check-camera.ps1
```

## Resource Usage

| Component | Process | Memory | CPU | Notes |
|-----------|---------|--------|-----|-------|
| GStreamer | vms-media | 45.88 MB | 7.27s | Actively decoding H.264 |
| mediasoup | node | 61.41 MB | 1.34s | 16 workers idle |

## Key Points

1. **No Separate Processes**: Both GStreamer and mediasoup are libraries embedded in their host processes
2. **GStreamer**: Handles RTSP → HLS + RTP conversion
3. **mediasoup**: Handles RTP → WebRTC conversion for browsers
4. **Performance**: Low latency (0-26ms), efficient resource usage
5. **Scalability**: mediasoup uses 16 workers for multi-core utilization

## Logs

- **GStreamer logs**: `logs/media.log` and `logs/media_err.log`
- **mediasoup logs**: `logs/sfu.log`
- **HLS segments**: `C:\ProgramData\TechnoSupport\VMS\hls\<session-id>\`
