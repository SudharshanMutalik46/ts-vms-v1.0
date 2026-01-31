# Airgap Deployment Specification

## 1. Core Assumption
The system must function indefinitely with **Zero Internet Access**.
- No `npm install`, `go get`, or `docker pull` at runtime.
- No NTP servers reachable outside the LAN.
- No generic Cloud Licensing checks.

## 2. Inbound/Outbound Policy
Since the VMS runs on a private LAN:
- **Inbound:** Only specific ports allowed (e.g., tcp/443, tcp/8554 [RTSP]). All others blocked by Windows Firewall.
- **Outbound:** Blocked by default, except for strictly defined allow-list (e.g., SMTP or Webhook to specific internal IP).

## 3. Dependency Strategy: "The Bundle"
We ship a single, self-contained **Upgrade Bundle** (`.zip` or `.msi`).
- **Contains:** All `.exe` binaries, Web UI (pre-built static assets), PostgreSQL installer/binaries, Redis binaries, and default config templates.
- **Validation:** Operator runs a checksum verification tool (SHASUM) against the bundle before transfer to the airgapped server.

## 4. Operator Checklist
Before deployment acceptance:
- [ ] confirm server is disconnected from public WAN.
- [ ] confirm Local NTP source is reachable.
- [ ] verify Bundle checksum matches Release Notes.
