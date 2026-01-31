# Technology Selection Rationale

## 1. Language Choices

### 1.1 Go (Control Plane)
- **Rationale:** Best-in-class for networked services and orchestration. Goroutines map perfectly to handling thousands of concurrent camera connections/requests. Strict typing prevents many runtime errors found in Node/Python.

### 1.2 C++ (Media Plane)
- **Rationale:** Video processing is CPU-bound. GStreamer is native C. Using C++ gives us zero-overhead access to GStreamer and raw memory pointers, essential for the high-throughput video pipeline (70+ cameras per server target).

### 1.3 Rust (Recording Engine)
- **Rationale:** Safety + Speed. Recording requires reliable buffer management and file I/O. Rust's ownership model prevents memory leaks and data races, which are common plaguing issues in long-running C++ recording services.

### 1.4 Python (AI Plane)
- **Rationale:** The de-facto standard for AI. Huge ecosystem of pre-trained models (YOLO, ResNet). While C++ is faster for inference, Python allows for rapid iteration of business logic around the inference.

## 2. Windows-Native (No Docker)

### 2.1 Deployment Simplicity
- **Why:** Many traditional security/IT environments are uncomfortable with Docker/Kubernetes on Windows. `Setup.exe` is the standard they expect.
- **Performance:** Native processes avoid the virtualization overhead of Docker Desktop for Windows (which runs a Linux VM). This provides better access to GPU resources for AI.

## 3. Tradeoffs & Mitigations

| Tradeoff | Risk | Mitigation |
| :--- | :--- | :--- |
| **Complexity of Polyglot** | Using Go, C++, Rust, Python increases build complexity. | Strict build scripts (Powershell), static binaries where possible. |
| **Windows-Specific Ops** | Linux tools (cron, bash) aren't available. | Use Windows native equivalents (Task Scheduler, Powershell). |
| **Dependency Hell** | Installing DLLs, runtimes on customer machines. | Ship a bundled installer that includes everything (VCRedist, etc.). Static linking where possible. |
