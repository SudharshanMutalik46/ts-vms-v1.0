# Circuit Breaker Patterns

## 1. Goal
Isolate failures to prevent cascading outages (e.g., Database slow -> Control Plane hangs -> NVRs disconnect).

## 2. States
- **Closed (Normal):** Requests pass through.
- **Open (Tripped):** Requests fail immediately (Fail Fast).
- **Half-Open (Probe):** Allow limited test traffic to check if downstream is healthy.

## 3. Application Areas
| Source | Dest | Threshold | Action |
| :--- | :--- | :--- | :--- |
| **Control** | **Media** | 5 timeouts / 10s | Mark Media Service "Unhealthy". Redirect new streams elsewhere. |
| **Control** | **DB** | 10 conns failed | Reject API writes. Serve reads from Cache if possible. |
| **UI** | **Control** | 400 Bad Gateway | Show "Reconnecting..." to user. Exponential backoff retry. |

## 4. Backpressure Policy
- **Bounded Queues:** System buffers (channels) must have fixed size (e.g., 1000 events).
- **Shed Load:** If queue is full, **Reject New Work**.
    - APIs return `429 Too Many Requests`.
    - Internal events are dropped (with "Drop Counter" metric incremented).
    - **Goal:** Protect the Control Plane's memory stability at all costs.
