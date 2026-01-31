# Graceful Degradation Scenarios

## 1. Live Streaming Ladder
When Network or CPU is constrained:
1. **Stage 1 (Normal):** Low-Latency WebRTC (Sub-500ms).
2. **Stage 2 (High Load):** Switch to HLS (3-5s High Latency).
3. **Stage 3 (Severe):** Drop to Low-Res MJPEG (1fps).
4. **Stage 4 (Outage):** Show "Signal Lost" placeholder. **UI must not crash.**

## 2. AI Analytics Ladder
When GPU/CPU is saturated:
1. **Stage 1 (Normal):** Process all frames at 15fps.
2. **Stage 2 (Load > 80%):** Reduce sampling to 5fps.
3. **Stage 3 (Load > 90%):** Drop non-critical cameras (Hallways) to prioritize Perimeters.
4. **Stage 4 (Load > 95%):** Disable Heavy Models (Face Rec). Keep only Motion Detection.
5. **Stage 5 (Emergency):** Disable AI Service. **Recording Must Continue.**

## 3. Event Ingestion Ladder
When Event Bus (NATS) or DB is backed up:
1. **Stage 1:** Buffer in RAM (Max 100MB).
2. **Stage 2:** Spill to Disk (Max 1GB, WAL).
3. **Stage 3:** Drop `DEBUG` / `INFO` events. Keep `WARN` / `ERROR`.
4. **Stage 4:** Drop `MOTION` events. Keep `ALARM` / `SYSTEM`.
5. **Stage 5 (Full):** Alert Operator "Events Dropped".
