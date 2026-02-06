# Manual Verification Checklist

Required outputs for technical sign-off of Phase 3.4 and 3.5.

## 1) Service Health Verification
Confirm all required endpoints are responsive.

### A) Control Plane Health
**Command:** `curl http://localhost:8080/api/v1/healthz`
**Expected:** `200 OK`

### B) SFU Health
**Command:** `curl http://localhost:8085/health` (or configured health endpoint)
**Expected:** `200 OK` or WS connectivity.

### C) HLS Delivery Health (vms-hlsd)
**Command:** `curl http://localhost:8081/healthz`
**Expected:** `200 OK`

---

## 2) Join Request Analysis
Retrieve server logs for the failing join attempt.

1. **Find REQ_ID**: Get `req_id` from the failed response JSON in your browser console.
2. **Retrieve Logs**: Locate the corresponding log entries in `logs/control_err.log` for the specific request ID.
3. **Analyze Error**: Identify the specific reason why the SFU join failed (e.g., timeout, auth, or media plane connection).

---

## 3) Browser Network Evidence
Provide proof of the failing signaling and media requests.

### A) /join request
**Capture DevTools Network Tab:**
- Status Code
- Response Body (should show `fallback_hint: true`)
- Request Headers (Verify `Authorization` presence)

### B) HLS Fallback Fetch
**Capture DevTools Network Tab:**
- URL attempted (check if it matches `/hls/live/...`)
- Status (failed/403/404)
- Browser Console Error Details

---

## 4) Hosting Environment Test
Ensure the test page is served via HTTP to avoid `file://` restrictions.

**Command:**
```powershell
cd C:\Users\sudha\Desktop\ts_vms_1.0\test
python -m http.server 8000
```
**Access:** Open `http://localhost:8000/webrtc_test.html`
---
