# Service Communication Protocols

## 1. Protocol Decision Matrix

| Link | Protocol | Rationale |
| :--- | :--- | :--- |
| **Control ↔ Client** | **REST (JSON) + WebSocket** | Standard for Web Browsers; WS for live events is efficient. |
| **Control ↔ Media** | **gRPC** | Strictly typed, high performance, bidirectional streaming capable. |
| **Control ↔ Recorder**| **gRPC** | Command/Control pattern fits RPC perfectly. |
| **Media ↔ SFU** | **RTP (UDP)** | Standard for real-time video transport. |
| **Media ↔ AI** | **Shared Memory / Socket** | Zero-copy is critical for raw video throughput. |
| **AI ↔ Control** | **NATS** | Component decoupling. AI produces events; Control consumes. |

## 2. Network Reliability Rules

### 2.1 Backpressure
- **Video Path:** If the Client cannot consume video fast enough,frames are dropped at the SFU. We do not buffer indefinitely.
- **Data Path:** NATS handles buffering for bursty event traffic.

### 2.2 Idempotency
- **Control Commands:** Retryable. `StartStream(cam1)` is safe to call twice; the second call returns "Already Running".

### 2.3 Message Formats
- **gRPC:** Protobuf (binary, fast, contract-first).
- **NATS:** JSON (debuggable, schema evolution is easier).
