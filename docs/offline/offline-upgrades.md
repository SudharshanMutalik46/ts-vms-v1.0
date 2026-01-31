# Offline Upgrade Procedures

## 1. Upgrade Bundle
All updates are delivered as a single signed archive (`ts-vms-v1.2.0.zip`).
- **Integrity:** SHA256 checksum validation is mandatory before extraction.

## 2. Atomic Deployment (Blue/Green)
We mitigate risk by never overwriting the running version.

1. **Current State:** `C:\Program Files\VMS\Current` -> symbolic link to `\V1.0`.
2. **Design:**
    - Extract Bundle to `C:\Program Files\VMS\V1.1`.
    - Copy persistent config/keys from `\V1.0` to `\V1.1`.
    - Run DB Migrations (Schema Update).
    - **Stop Services**.
    - Update Link: `\Current` -> `\V1.1`.
    - **Start Services**.

## 3. Rollback Plan
If the new version fails to start or passes health checks:
1. **Stop Services**.
2. Revert Link: `\Current` -> `\V1.0`.
3. **Start Services**.
4. (Optional) Reverse DB Migrations if specific "Down" scripts exist.

## 4. Operator Runbook
1. Upload Bundle to Server.
2. Run `Verify-Bundle.ps1`.
3. Execute `Upgrade.ps1`.
4. Monitor "Health API" for green status.
5. If Red > 5 mins, Execute `Rollback.ps1`.
