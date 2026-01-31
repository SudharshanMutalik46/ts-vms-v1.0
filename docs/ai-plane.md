# AI Plane Architecture

## 1. Overview
The AI Plane (`vms-ai`) runs computer vision models to extract metadata from video streams. It operates typically in **Python** (for rich library support like PyTorch/TensorFlow/YOLO) or **C++** (for max performance with TensorRT).

## 2. Responsibilities

### 2.1 Inference Pipeline
- **Input:** Consumes video frames from the Media Plane.
- **Processing:** Runs pre-loaded models (e.g., YOLOv8 for object detection).
- **Logic:** Applies business logic (e.g., "Is the person inside the ROI polygon?").
- **Output:** Generates JSON metadata event.

### 2.2 Process Isolation
- The AI service runs as a separate process.
- **Why?** AI inference is heavy and can be unstable (OOM, CUDA errors).
- **Safety:** If `vms-ai` crashes, `vms-media` keeps streaming and recording. The VMS only loses intelligent alerts, not basic functionality.

## 3. Integration Points

### 3.1 Input: Shared Memory / Socket
- To avoid copying large video frames, the AI Plane reads directly from a shared buffer or high-speed socket provided by the Media Plane.

### 3.2 Output: NATS Message Bus
- When an event occurs (e.g., PROHIBITED_ZONE_ENTRY), the service publishes a message to NATS.
- **Topic:** `vms.events.ai.detection`
- **Payload:**
  ```json
  {
    "camera_id": "cam_01",
    "timestamp": 1234567890,
    "type": "person",
    "confidence": 0.95,
    "bbox": [100, 200, 50, 80]
  }
  ```
- The Control Plane and other subscribers listen to this topic.

## 4. Model Lifecycle
- **Phase 0:** Models are baked into the Docker image or installer. No dynamic model uploading.
- **Format:** ONNX is preferred for cross-platform and hardware-accelerated inference (DirectML on Windows, CUDA if available).
