# Testing Requirements

## 1. Coverage Policy
**Minimum Coverage:** **80%**
- This applies to Statement Coverage across Go, C++, and Rust projects.
- **Enforcement:** CI will fail if a PR drops coverage below the threshold or the delta is significantly negative.

## 2. Test Pyramid
### 2.1 Unit Tests (Fast)
- **Scope:** Single function/class.
- **Dependencies:** Mocked (DB, Network, FileSystem).
- **Execution:** Must run in <100ms per test.

### 2.2 Integration Tests (Medium)
- **Scope:** Interaction between modules (e.g., Service -> DB).
- **Dependencies:** Real DB (Dockerized/Local), Mocked External APIs.
- **Execution:** Must run in <10s.

## 3. Flakiness Policy
- **Zero Tolerance:** Flaky tests break trust.
- **Action:** If a test flakes:
    1. Fix it immediately.
    2. If fix is hard, mark as `Skip` or `Pending` with a ticket ID.
    3. Do NOT leave it failing randomly.

## 4. Performance Testing (Future)
- Streaming pipelines must have regression tests for FPS/Latency.
- Stress tests (100 cameras) will be required for Release Candidates.
