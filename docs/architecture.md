# Techno Support VMS - System Architecture

## 1. High-Level Overview

Techno Support VMS is a Windows-native, offline-first Video Management System designed for high reliability and low latency. It is composed of loosely coupled services communicating over defined protocols (gRPC, NATS), running as independent Windows Services.

The system is designed to run entirely on-premise without cloud dependencies.

## 2. Component Boundaries & Responsibilities

The system is divided into four logical planes:

### 2.1 Control Plane
**Technology:** Go
**Responsibility:** The brain of the VMS.
- Manages configuration (Cameras, Users, Sites).
- Orchestrates other services (Media, AI, Recorder).
- Exposes API for the Web Client.
- Handles Authentication and Authorization.
- **Service Name:** `vms-control`

### 2.2 Media Plane
**Technology:** C++ (GStreamer)
**Responsibility:** Low-latency video ingestion and distribution.
- Injects RTSP streams from cameras.
- Demuxes streams for usage by AI, Recorder, and Client.
- **Pass-through design:** No transcoding of video (H.264/H.265 passed through).
- Proxies streams to WebRTC/MSE for client viewing via SFU.
- **Service Name:** `vms-media`

### 2.3 AI Plane
**Technology:** Python / C++ (ONNX Runtime / TensorRT)
**Responsibility:** Video analytics and inference.
- Consumes raw video frames or encoded streams from Media Plane.
- Performs object detection (Person, Vehicle, etc.) and specific analytics (Line Crossing).
- Publishes inference events to the data backbone (NATS).
- **Service Name:** `vms-ai`

### 2.4 Recording Engine
**Technology:** Rust
**Responsibility:** High-throughput disk I/O.
- Consumes stream packets from Media Plane.
- Writes metadata-tagged video to disk (MKV/MP4 segment format).
- Manages retention policy (cleanup of old footage).
- **Service Name:** `vms-recorder`

### 2.5 Web/UI Layer
**Technology:** React / Single Page Application (SPA) served by Go Control Plane.
**Responsibility:** User Interface.
- Dashboard for live view and monitoring.
- Configuration interfaces.
- Playback interface for recorded footage.

### 2.6 SFU / Signal Layer
**Technology:** Go (Pion) or C++ (part of Media Plane) - *Architecture Decision: Integrated into Media Plane or helper service.*
- Conceptual responsibility: Bridging RTSP to Browser-compatible formats (WebRTC).

## 3. Data Storage Layer
- **PostgreSQL:** Relational data (Tenants, Users, Camera config, Event metadata).
- **Redis:** Hot state (Active streams, ephemeral session data).
- **NATS:** Message bus for inter-service communication and event streaming.
- **Filesystem (NTFS):** Video recordings and snapshots.

## 4. Deployment Model
- **Target OS:** Windows Server 2019/2022 / Windows 10/11 Enterprise.
- **Format:** Native Windows Services (deployed via `sc.exe`).
- **No Docker/Container Dependency:** All binaries must run natively.
- **Offline-First:** No calls to public internet required for operation.

## 5. Runtime Boundaries & Isolation
- Each plane runs in a separate OS process.
- **Crash Containment:**
  - If `vms-ai` crashes, video recording and live view MUST continue.
  - If `vms-recorder` crashes, live view and AI must continue.
  - If `vms-control` crashes, existing streams *should* ideally persist (media plane autonomy), though control actions will fail.
- **Restart Strategy:** Windows Service Manager configures auto-restart (Restart on failure).

## 6. Assumptions & Non-Goals
- **Assumption:** Host machine has NVIDIA GPU for AI inference (preferred) or capable CPU.
- **Assumption:** Cameras generate H.264/H.265 RTSP streams.
- **Non-Goal:** Video Transcoding (CPU expensive). We rely on client-side decoding or substreams.
- **Non-Goal:** Cloud archiving (Phase 0 scope is local storage).
