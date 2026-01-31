# Architecture Diagrams

## 1. System Component Diagram

```mermaid
graph TD
    subgraph "Clients"
        WebUI[Web Browser / Client]
    end

    subgraph "VMS Host (Windows)"
        subgraph "Control Plane"
            Control[vms-control (Go)]
        end

        subgraph "Media Plane"
            Media[vms-media (C++/GStreamer)]
            SFU[SFU / WebRTC Bridge]
        end

        subgraph "AI Plane"
            AI[vms-ai (Python/C++)]
        end

        subgraph "Recording Plane"
            Recorder[vms-recorder (Rust)]
        end

        subgraph "Data Backbone"
            NATS((NATS JetStream))
            Postgres[(PostgreSQL)]
            Redis[(Redis)]
            FS[File System (Recordings)]
        end
    end

    subgraph "Edge Devices"
        Camera[RTSP Camera]
    end

    %% Flows
    Camera -->|RTSP Stream| Media
    Media -->|Shared Memory / Stream| AI
    Media -->|Stream Packets| Recorder
    Media -->|WebRTC/HLS| SFU
    SFU -->|WebRTC| WebUI

    Control -->|gRPC commands| Media
    Control -->|gRPC commands| Recorder
    Control -->|Control / Config| AI
    
    AI -->|Inference Events| NATS
    Recorder -->|Write| FS
    Control -->|Read/Write| Postgres
    Control -->|Cache| Redis
    
    WebUI -- HTTP/WS --> Control
    NATS -->|Events| Control
```

## 2. Streaming Path High-Level

```mermaid
sequenceDiagram
    participant Cam as Camera
    participant Media as Media Engine (C++)
    participant SFU as SFU/Bridge
    participant Client as Web Client

    Cam->>Media: RTSP (H.264/H.265)
    Note over Media: Demux / Parse
    par Live View
        Media->>SFU: RTP Packets
        SFU->>Client: WebRTC / MSE
    and AI Analysis
        Media->>AI Plane: Raw Frames / Buffer
    and Recording
        Media->>Recorder: Encoded Packets
    end
```

## 3. Data Layer Diagram

```mermaid
graph LR
    subgraph "Storage"
        DB[(PostgreSQL)]
        Cache[(Redis)]
        Bus((NATS))
        Disk[Disk Storage]
    end

    subgraph "Data Types"
        Config[Configuration & Users] --> DB
        Meta[Event Metadata] --> DB
        Hot[Live Streams State] --> Cache
        Msg[Inter-service Msgs] --> Bus
        Video[Video Files .mkv] --> Disk
    end
```

## 4. Windows Service Dependency & Startup

```mermaid
graph TD
    Start((System Boot))
    
    subgraph "Infrastructure"
        PgSQL[PostgreSQL Service]
        RedisSvc[Redis Service]
        NatsSvc[NATS Service]
    end

    subgraph "VMS Services"
        ControlSvc[vms-control]
        MediaSvc[vms-media]
        AISvc[vms-ai]
        RecSvc[vms-recorder]
    end

    Start --> PgSQL
    Start --> RedisSvc
    Start --> NatsSvc

    PgSQL --> ControlSvc
    RedisSvc --> ControlSvc
    NatsSvc --> ControlSvc

    ControlSvc --> MediaSvc
    ControlSvc --> AISvc
    ControlSvc --> RecSvc

    %% Logic: Control plane usually needs to be up to serve config to others, 
    %% or others wait for Control.
```
