# Contracts & Standards

## HLS Token Gating (Phase 3.2+)

This section defines the security contract for LL-HLS delivery. The **Media Plane (Generation)** is responsible for implementing this contract conceptually, even though validation happens at the **Delivery Layer (Phase 3.3)**.

### 1. Token Strategy
- **Mode**: Delivery-time Token Injection / Validation (Strategy A).
- **Generator Responsibility**: `vms-media.exe` writes "clean" playlists (e.g., `segment_0.m4s`) without tokens embedded in the filenames or existing internal URLs.
- **Validator Responsibility**: The HTTP delivery service (Phase 3.3) acts as a gatekeeper. It validates the token in the request query parameters or cookies before serving the static files.

### 2. Token Format
Tokens are stateless and cryptographically signed using a shared secret.

**Parameters:**
- `sub` (Subject): Camera ID (e.g., `cam-01`).
- `sid` (Session ID): Unique ID of the current streaming session (e.g., `1707123456_xc9`).
- `exp` (Expiry): Unix timestamp (seconds) when the token expires.
- `scope`: Fixed value `hls`.
- `kid` (Key ID - Optional): Identifier for the signing key.
- `sig` (Signature): Hex-encoded HMAC-SHA256 signature.

**Example Token String:**
`sub=cam-01&sid=1707123456_xc9&exp=1707127056&scope=hls&sig=a1b2c3d4...`

### 3. Signing Algorithm
The signature `sig` is calculated as follows:

1.  **Construct Canonical String**:
    The string to sign MUST be constructed by concatenating the values in the following strict order, separated by a vertical bar `|`:
    ```text
    hls|{sub}|{sid}|{exp}
    ```
    *(Note: `kid` is not part of the signed string, as it is metadata for key lookup)*

2.  **Compute HMAC**:
    ```text
    HMAC-SHA256(SecretKey, CanonicalString)
    ```

3.  **Encoding**:
    - The output hash is converted to a lowercase **Hexadecimal String**.

### 4. Secret Management
- **Media Plane**: Does NOT need the secret key for generation (Phase 3.2). It strictly produces content.
- **Control Plane**: Generates tokens for clients using the Secret Key.
- **Delivery Layer**: Validates tokens using the Secret Key.
- **Artifacts**: **NO secrets** shall be written to `meta.json` or logs.

## Disk Layout (HLS)

The Media Plane must strictly adhere to the following on-disk structure:

```text
{DataRoot}\hls\live\{camera_id}\{session_id}\
    ├── index.m3u8          # Master/Media Playlist (Rolling 10s window)
    ├── init.mp4            # CMAF Initialization Segment
    ├── segment_0.m4s       # Media Segments (fMP4)
    ├── segment_1.m4s
    ├── ...
    └── meta.json           # Session Metadata (No Secrets)
```

**Layout Rules:**
- **One active session** per camera variant.
- **Session ID** must change on every pipeline restart/reconnect to avoid stale playlist confusion.
- **Filenames**: Must be Windows-safe (no special characters).
- **Updates**: `meta.json` is written at start and updated on writes.

### `meta.json` Schema
```json
{
  "tenant_id": "string",
  "camera_id": "string",
  "session_id": "string",
  "created_at": "ISO8601_Timestamp",
  "last_write_at": "ISO8601_Timestamp",
  "hls_config": {
    "target_duration": 1.0,
    "part_duration": 0.2,
    "playlist_window": 10
  }
}
```

## SFU Strategy (Phase 3.4)

### 1. Router-per-Room
Each Camera corresponds to one Mediasoup Room. A Room contains exactly one Mediasoup Router.
- **Tenant Isolation**: Routers are created on Workers. Different tenants share Workers but have isolated Routers.
- **Worker Load Balancing**: Workers are selected round-robin.
- **Lifecycle**: A Room is created on the first `JoinRoom` request and destroyed after 60 seconds of inactivity (0 viewers).

### 2. Transport Configuration
- **WebRTC**:
  - UDP Range: `40000-49999`
  - TCP Fallback: Enabled
  - ICE: Lite (Server does not initiate connectivity checks)
- **Ingest (PlainTransport)**:
  - UDP Range: `50000-51000`
  - Comedia: True (Server learns remote IP:Port from incoming RTP)

### 3. Media Capabilities
- **Video**: H.264 (Baseline, Packetization Mode 1).
- **Audio**: Not currently enabled.
- **Simulcast**: Disabled (Single high-quality stream from Media Plane).

---

## Phase 3.6: Start Live View Contract

### 1. Endpoint: `POST /api/v1/cameras/{camera_id}/live/start`

Response JSON structure (Dual-Path):
```json
{
  "session_id": "viewer_session_xyz123",
  "expires_at": 1707127056,
  "primary": "webrtc",
  "fallback": "hls",
  "webrtc": {
    "sfu_url": "http://localhost:8080/api/v1/sfu",
    "room_id": "camera_id",
    "connect_timeout_ms": 5000
  },
  "hls": {
    "playlist_url": "http://localhost:8081/hls/live/tenant/cam/session/playlist.m3u8?token=...",
    "target_latency_ms": 4000
  },
  "fallback_policy": {
    "webrtc_connect_timeout_ms": 5000,
    "webrtc_track_timeout_ms": 3000,
    "max_auto_retries": 2,
    "retry_backoff_ms": [1000, 3000]
  },
  "telemetry_policy": {
    "client_event_endpoint": "/api/v1/live/events"
  }
}
```

### 2. Reason Codes (Standardized)
Used in telemetry and logs.

| Code | Description |
|------|-------------|
| `SFU_SIGNALING_FAILED` | API error when talking to SFU |
| `ICE_FAILED` | WebRTC ICE connection failed or disconnected |
| `DTLS_FAILED` | DTLS handshake failure |
| `TRACK_TIMEOUT` | Connected but no media received in time |
| `RTP_TIMEOUT` | Media stopped flowing after start |
| `SFU_BUSY` | SFU at capacity |
| `BROWSER_NOT_SUPPORTED` | Client browser missing features |
| `PERMISSION_DENIED` | Auth or RBAC failure |
| `UNKNOWN` | Fallback catch-all |

### 3. Telemetry Endpoint: `POST /api/v1/live/events`

Request Payload:
```json
{
  "viewer_session_id": "viewer_session_xyz123",
  "event": "fallback_to_hls",
  "reason": "ICE_FAILED",
  "meta": { "ice_state": "failed" }
}
```
