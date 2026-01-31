# Windows Time Service Configuration (Offline)

## 1. Requirement
Accurate system time is mandatory for:
- **TLS Certificates:** Validity period checks.
- **Audit Logs:** Forensic sequencing of events.
- **Retention:** Correct deletion of old video.
- **Tokens:** Expiry validation (future phases).

## 2. Offline Time Source Options
The VMS Server must be configured to sync from a **Local Authoritative Source**.

### Option A: Local NTP Appliance (Recommended)
- A hardware GPS clock or dedicated NTP server on the LAN.
- Config: `w32tm /config /manualpeerlist:"192.168.1.50" /syncfromflags:MANUAL`

### Option B: Domain Controller
- If joined to an Offline AD, sync from Domain Hierarchy.

### Option C: CMOS / Local Quartz (Not Recommended)
- Only if strictly standalone. Drift will occur.
- **Mitigation:** Operator must manually correct time weekly.

## 3. Drift Policy
- **Threshold:** > 5 seconds deviation from Source.
- **Action:**
    - Log "Critical Time Skew" audit event.
    - If skew > 24 hours, **Halt new recordings** to prevent overwriting valid history (Policy decision).
