# Control Plane Architecture

## 1. Overview
The Control Plane (`vms-control`) is the central orchestrator of the VMS. It is written in **Go** to leverage its concurrency model (goroutines) and strong standard library. It does not handle heavy media processing but manages the lifecycle of streams, recordings, and AI jobs.

## 2. Service Boundaries

### 2.1 Core Responsibilities
- **API Server:** Hosts the REST/WebSocket API for the Web Client.
- **Data Access:** Exclusive writer to PostgreSQL configuration tables.
- **Service Orchestration:** Sends commands to Media, Recorder, and AI services.
- **Authentication:** Validates JWT tokens and user credentials.
- **State Management:** Maintains the "desired state" of the system vs "actual state".

### 2.2 Why Go?
- **Concurrency:** Efficient handling of multiple long-lived connections (WebSockets, gRPC).
- **Static Binary:** Easy deployment on Windows (single `.exe`).
- **Ecosystem:** Excellent database drivers (pgx) and web frameworks (Gin/Echo).

## 3. API Surface

The Control Plane exposes APIs in three categories:

### 3.1 Management API (REST/JSON)
Used by the Web Client for configuration.
- `GET /api/cameras` - List configured cameras.
- `POST /api/cameras` - Add a new camera.
- `GET /api/recordings` - Search recorded footage.
- `POST /api/login` - User authentication.

### 3.2 Streaming API (WebSocket / WebRTC Signaling)
Used by the Web Client to initiate live views.
- `WS /api/stream/{cameraId}` - Negotiate live stream playback.
  - Exchanges SDP offers/answers for WebRTC.
  - Handles ICE candidates.

### 3.3 Internal Command API (gRPC)
Used for reliable, typed communication with other backend services.
- `vms-control` acts as the **Client** in some cases (commanding Media) and **Server** in others (receiving status updates).

## 4. Communication Patterns

### 4.1 Control -> Media (gRPC)
- **Command:** `StartStream(RTSP_URL, Options)`
- **Command:** `StopStream(ID)`
- **Query:** `GetStreamHealth(ID)`

### 4.2 Control -> AI (NATS)
- **Pattern:** Pub/Sub config updates.
- **Topic:** `vms.config.ai.update` - When a user enables line-crossing on a camera, Control publishes a message. AI service subscribes and updates its pipeline.

### 4.3 Control -> Client (WebSockets/SSE)
- **Pattern:** Real-time event push.
- **Data:** System health, Alarms/Detections (forwarded from NATS).
