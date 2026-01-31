# Data Flow Legend & Standards

## 1. Visual Notation (Mermaid)

We use specific shapes and styles in Mermaid to distinguish between system boundaries, data stores, and communication patterns.

### 1.1 Shapes
- **Rectangle (`[Service]`)**: Stateless Service Process (e.g., `vms-control`, `vms-media`).
- **Cylinder (`[(Database)]`)**: Persistent Data Store (e.g., PostgreSQL, Redis, Disk).
- **Circle (`((Bus))`)**: Message Bus (NATS).
- **Rhombus (`{Decision}`)**: Logic Branch / Decision Point.
- **Actor (`Client`)**: Human User or Web Browser.

### 1.2 Communication Arrows
Different arrow styles represent different protocols:

| Arrow Style | Protocol Group | Description |
| :--- | :--- | :--- |
| `-->` (Solid) | **Synchronous / RPC** | Expects immediate response. (gRPC, REST, Function Call). |
| `-.->` (Dotted) | **Asynchronous / Event** | Fire-and-forget or Pub/Sub. (NATS, WebHook). |
| `==>` (Thick) | **Streaming / High-Bandwidth** | Continuous flow of data. (RTP, RTSP, WebSocket Stream). |

### 1.3 Naming Conventions
- **Services:** Lowercase, hyphenated (e.g., `vms-control`).
- **Databases:** Capitalized (e.g., `Postgres`, `Redis`).
- **Topics (NATS):** Dot-separated (e.g., `vms.event.person.detected`).
- **Functions/RPC:** PascalCase (e.g., `StartStream()`).

## 2. Protocol Legend

This document uses high-level protocol identifiers:

- **gRPC**: Internal service-to-service command (Strict contract).
- **REST**: Client-to-Service configuration (JSON over HTTP).
- **WS (WebSocket)**: Client-to-Service bidirectional live data.
- **NATS**: Internal event bus (Pub/Sub).
- **RTSP**: Camera-to-Server video transport.
- **WebRTC**: Server-to-Client low-latency video.
- **Shm (Shared Memory)**: Intra-node zero-copy video transfer.

## 3. Error Handling Patterns

### 3.1 Retry / Backoff
- **Notation:** "Retry 3x" or "Exp. Backoff"
- **Meaning:** If a synchronous call (`-->`) fails, the caller MUST retry with exponential backoff (e.g., 1s, 2s, 4s) before giving up.
- **Bounded:** Retries are never infinite for user-facing actions.

### 3.2 Dead Letter
- **Notation:** "DLQ"
- **Meaning:** If an async event (`-.->`) cannot be processed after N tries, it is moved to a "Dead Letter Queue" for manual inspection, preserving system stability.

### 3.3 Idempotency
- **Requirement:** All `vms-control` state-mutating commands (e.g., `CreateCamera`) must be safe to call multiple times with the same ID, resulting in the same final state.
