# WebRTC Test Page Debugging Guide (Phase 3.4)

## Overview
The `test/webrtc_test.html` page is a standalone client for verifying VMS WebRTC streaming.
As of Phase 3.4, usage of WebSocket signaling is **optional** and **disabled by default**.

## Usage

### 1. Serving the Page
**IMPORTANT:** Do NOT open this file directly via `file://` protocol. Browser security policies (CORS, mixed content) will block connections.

Run a local HTTP server:
```powershell
cd c:\Users\sudha\Desktop\ts_vms_1.0\test
python -m http.server 8000
```
Then open: [http://localhost:8000/webrtc_test.html](http://localhost:8000/webrtc_test.html)

### 2. Configuration
- **Camera ID:** UUID of the camera (e.g., `6ed6cf65-a421-4f5f-bfa3-363f33dbf23a` for H.264)
- **Token:** JWT Access Token (must be valid)

### 3. Verification Output
Open the Browser Console (F12) to verify behavior.

#### Success Indicators
- **HTTP-Only (Default):**
  - `WS disabled; using HTTP-only signaling`
  - `Transport State: connected`
  - `Streaming live via WebRTC.`

- **WS Enabled (if configured):**
  - `WS state: open`
  - `Transport State: connected`
  - `Streaming live via WebRTC.`

#### Error Handling verification
If WebSocket fails (e.g. if enabled but server down):
- Console: `WS failed; continuing with HTTP-only signaling`
- UI Status: `WS unavailable; using HTTP-only.`
- **Critical:** Video playback should proceed seamlessly.

## Architecture Notes
- **Signaling:** Uses `POST /rooms/{id}/join`, `/transports`, `/consume` for SDP exchange.
- **WebSocket:** Used *only* for non-critical connection state updates (metrics). It is NOT required for video flow.
- **Security:** JWT is **never** passed in the WebSocket URL query string.
