# Failure Matrix

| ID | Trigger | Impacted | Detection | Degradation Behavior | Integrity Risk | Boundedness | Never-Degrade Check |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **F-01** | **Service Crash Loop** | Control Plane | Process Exit Code | SCM Restarts service. API unavailable during boot. | Low | 3 restarts/hr cap | **PASS** (Restores self) |
| **F-02** | **Disk Full (99%)** | **Recording** | Disk Usage Metric | Stop Write. Close Segments. Mark Camera "Error". | **HIGH** | No writes allowed | **FAIL** (Stopping is unavoidable) |
| **F-03** | **DB Connection Lost** | Auth / Metadata | Conn Refused Logs | Reject Logins (503). Read-Only metadata if cached. | Low | Retry w/ backoff | **PASS** (Auth Fails Closed) |
| **F-04** | **Camera Storm** | Network / Control | Socket Count Spike | Accept 50/sec. Queue rest. Slow connect time. | Low | Max queue 1000 | **PASS** |
| **F-05** | **NTP Drift > 5s** | Audit / Certs | Time Offset Metric | Log "Untrusted Time". Flag Evidence. | **MED** | Alert only | **PASS** |
| **F-06** | **Cert Expired** | TLS / Web UI | Browser Warnings | Users blocked by Browser. API rejects w/o skip-verify. | Low | N/A | **PASS** (Fail Closed) |
| **F-07** | **AI Overload** | Search / Alerts | GPU Util > 95% | Reduce FPS. Skip Cameras. | Low | Drop frames | **PASS** (Recording safe) |
| **F-08** | **Audit DB Full** | All Actions | Write Error | Buffer to Disk. If Disk full -> Block Actions. | Low | Max 1GB Disk Buffer | **PASS** (Fail Closed) |
| **F-09** | **Event Bus Down** | Real-time Stream | Conn Timeout | Buffer RAM/Disk. UI shows "Events Disconnected". | Low | Max Buffer | **PASS** |
| **F-10** | **SFU Crash** | Live View | Process Exit | Stream stops. UI retries WebRTC. | Low | 3 restarts | **PASS** |
| **F-11** | **Network Partition** | Control ↔ Media | PING Timeout | Mark Media Plane Offline. Re-route if HA exists. | Low | N/A | **PASS** |
| **F-12** | **Memory Leak** | Any Service | OOM Killer | OS Kills process. SCM Restarts. | Low | RAM Limit | **PASS** |
| **F-13** | **Corrupt Config** | Control Plane | JSON Parse Err | Panic on boot. SCM gives up after 3 tries. | Low | Manual fix needed | **PASS** |
| **F-14** | **RTSP Auth Fail** | Ingest | 401 Unauth | Retry 3x. Mark Camera "Needs Creds". | Low | Exp backoff | **PASS** |
| **F-15** | **Snapshot Full** | Storage | Disk Usage | Stop saving snaps. Log Error. | Low | NO overwrite active | **PASS** |
