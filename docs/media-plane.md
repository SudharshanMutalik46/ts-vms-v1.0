# Media Plane Architecture

## 1. Overview
The Media Plane (`vms-media`) is the high-performance engine responsible for video ingestion and routing. It is built using **C++** and **GStreamer**.

## 2. Design Principles

### 2.1 Pass-Through Philosophy
To minimize CPU usage, the Media Plane **avoids transcoding** whenever possible.
- **Ingest:** Accept H.264/H.265 streams via RTSP.
- **Internal Routing:** Keep video packets compressed.
- **Egress:** Repackage into suitable containers (RTP for WebRTC, MKV for Recording) without decoding/re-encoding pixels.

### 2.2 Resource Isolation
Each active camera stream runs in its own GStreamer pipeline or thread context.
- A failure in Camera A's pipeline (e.g., bad RTSP source) MUST NOT affect Camera B.
- Dynamic pipeline construction allows adding/removing cameras without restarting the process.

## 3. GStreamer Pipeline Concept

```
[rtspsrc] -> [rtph264depay] -> [parse] -> [tee name=t]
                                          |
                                          +-> [queue] -> [appsink (for AI processing)]
                                          |
                                          +-> [queue] -> [rtph264pay] -> [udpsink (to SFU/Client)]
                                          |
                                          +-> [queue] -> [matroskamux] -> [filesink (Recording)]
```
*Note: The actual implementation splits Recording and AI into separate services, so the Tee sends to shared memory or inter-process socket sink.*

## 4. Failure Handling

- **RTSP Disconnect:** The pipeline monitors for EOS (End of Stream) or timeout.
- **Auto-Reconnect:** On failure, the pipeline enters a "Backoff/Retry" loop (e.g., retry every 5s).
- **Bad Data:** Corrupt packets are dropped silently to prevent pipeline stalls.

## 5. Interfaces

### 5.1 To Control Plane (gRPC Server)
The Media Plane exposes a gRPC server to receive commands:
- `SetupStream(id, url)`: Builds and starts a pipeline.
- `TeardownStream(id)`: Stops and frees resources.

### 5.2 To SFU
- Delivers RTP packets via UDP loopback or shared memory to the SFU component for web delivery.

### 5.3 To AI Plane
- Provides access to decoded raw frames (Video/x-raw) via Shared Memory (Shm) or TCP socket for inference, ensuring low latency.
