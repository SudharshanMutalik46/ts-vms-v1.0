# Video Freezing Solutions - Complete Guide

## Current Status
- ✅ Camera I-frame interval: 10 (2 keyframes/second)
- ✅ Media Plane: Running smoothly
- ✅ SFU: Active and streaming
- ⚠️ Still experiencing periodic freezes

## Root Causes & Solutions

### 1. **Network Jitter (Most Likely)**

**Problem**: Packets arrive at irregular intervals causing brief stalls.

**Solution A - Increase Browser Jitter Buffer:**

Edit `test/webrtc_test.html` and modify the consumer creation (around line 339):

```javascript
const consumer = await transport.consume({
    id: consumerInfo.id,
    producerId: consumerInfo.producerId,
    kind: consumerInfo.kind,
    rtpParameters: consumerInfo.rtpParameters,
    // ADD THESE LINES:
    appData: {
        jitterBufferTarget: 300,  // 300ms buffer
        jitterBufferMaxPackets: 200
    }
});
```

**Solution B - Use TCP for RTSP (More Stable):**

The camera is currently using UDP for RTSP. TCP is more reliable but slightly higher latency.

Check if Media Plane is using TCP: Look for `prefer_tcp: true` in the ingest configuration.

### 2. **WebRTC ICE Connection Issues**

**Problem**: ICE connection state changes causing brief disconnections.

**Check**: Open browser console (F12) and look for:
- "Transport State: disconnected"
- "Transport State: failed"
- ICE connection warnings

**Solution**: Add STUN/TURN server configuration to improve NAT traversal.

### 3. **Mediasoup Buffer Settings**

**Problem**: Default mediasoup settings might be too aggressive.

**Solution**: Modify SFU configuration in `sfu/src/mediasoup.ts`:

```typescript
// In createRouter() method, add:
const router = await worker.createRouter({
    mediaCodecs: [...],
    // ADD THIS:
    rtcpMux: true,
    rtcpReducedSize: true
});
```

### 4. **GStreamer Pipeline Buffering**

**Problem**: Media Plane GStreamer pipeline might have buffer underruns.

**Check logs**: Look for "buffer underrun" or "late buffer" messages.

**Solution**: The pipeline already has buffering, but you can verify it's working.

## Quick Fixes (Try These First)

### Fix 1: Restart Ingest with Fresh Connection
```powershell
# Stop and restart the camera ingest
.\scripts\dev-restart.ps1
Start-Sleep -Seconds 10
go run scripts/start_ingest.go
```

### Fix 2: Use Lower Resolution
If your camera supports it, try using SubStream instead of MainStream:
- Lower resolution = less bandwidth = fewer freezes
- Change RTSP URL to SubStream endpoint

### Fix 3: Check Network Path
```powershell
# Test network to camera
ping 192.168.1.181 -n 100

# Look for:
# - Packet loss (should be 0%)
# - Jitter (time variation)
```

### Fix 4: Try HLS Instead of WebRTC
HLS is more tolerant of network issues:

In the WebRTC test page, the system will auto-fallback to HLS if WebRTC fails. Or force HLS by:
- Stopping WebRTC
- The page will automatically switch to HLS

## Advanced Diagnostics

### Monitor Real-Time Stats
```powershell
# Run this in a separate PowerShell window
while ($true) {
    Clear-Host
    Write-Host "=== Live Monitoring ===" -ForegroundColor Cyan
    .\scripts\diagnose-freezing.ps1
    Start-Sleep -Seconds 3
}
```

### Check WebRTC Stats in Browser
Add this to browser console while video is playing:

```javascript
// Get WebRTC stats
const stats = await transport.getStats();
stats.forEach(stat => {
    if (stat.type === 'inbound-rtp') {
        console.log('Packets Lost:', stat.packetsLost);
        console.log('Jitter:', stat.jitter);
        console.log('Frames Dropped:', stat.framesDropped);
    }
});
```

## Expected Behavior

**Normal**: Occasional 100-200ms micro-freezes during network congestion  
**Abnormal**: Regular 2+ second freezes every few seconds

If you're seeing regular 2-second freezes even after:
- Setting I-frame interval to 10
- Good network connection (0% packet loss)
- No errors in logs

Then the issue is likely **WebRTC jitter buffer** settings, which can be fixed by modifying the test page as shown in Solution A above.

## Recommended Next Steps

1. **Immediate**: Try Fix 1 (restart ingest)
2. **Short-term**: Implement Solution A (increase jitter buffer)
3. **Long-term**: Consider using HLS for more stable playback

## Performance Targets

- **Excellent**: < 100ms freezes, rare
- **Good**: < 500ms freezes, occasional
- **Acceptable**: < 1s freezes, infrequent
- **Poor**: > 2s freezes, regular ← **Your current state**

The goal is to get to "Good" or "Excellent" level.
