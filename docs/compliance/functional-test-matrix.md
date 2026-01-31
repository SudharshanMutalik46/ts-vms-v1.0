# Functional Test Matrix

## 1. Traceability
All tests map to requirements in Phase 0.x docs.

## 2. Matrix Structure

| Feature Area | Test Case ID | Test Description | Expected Result | Evidence Link |
| :--- | :--- | :--- | :--- | :--- |
| **Onboarding** | F-ONB-01 | Discover ONVIF Camera | Camera listed with IP/Model | `evidence/func/onb-01.png` |
| **Onboarding** | F-ONB-02 | Add Camera with Bad Creds | Error "Auth Failed" | `evidence/func/onb-02.log` |
| **Streaming** | F-STR-01 | Live View (WebRTC) | Video plays < 500ms latency | `evidence/func/str-01.mp4` |
| **Streaming** | F-STR-02 | Live View (HLS) | Video plays (fallback) | `evidence/func/str-02.mp4` |
| **Recording** | F-REC-01 | Continuous Record | Segments created on disk | `evidence/func/rec-01.ls` |
| **Offline** | F-OFF-01 | Time Drift Alert | Alert generated when clock skewed | `evidence/func/off-01.json` |
| **Offline** | F-OFF-02 | Cert Rotation | New cert applied without reinstall | `evidence/func/off-02.log` |

## 3. Coverage Goal
- **Critical Path:** 100% (must pass).
- **Edge Cases:** 80% (best effort).
