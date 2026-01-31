# Authentication Model

## 1. Token Policy (JWT)
We use a **Dual-Token System** (Access + Refresh).

### Access Token (Stateless)
- **Format:** JWT (Signed RS256)
- **TTL:** 15 minutes
- **Claims:**
  - `sub`: User UUID
  - `tenant_id`: Tenant UUID
  - `scopes`: List of roles/permissions `["admin", "camera.view"]`
  - `jti`: Unique Token ID (for one-off blacklist)
  - `exp`: Expiration time
  - `iss`: "ts-vms-auth"

### Refresh Token (Stateful / Opaque)
- **Format:** High-entropy random string (64 chars) or Encrypted JWT
- **TTL:** 7 days (Rolling)
- **Storage:** HttpOnly, Secure Cookie (Web) or Secure Storage (Mobile).

## 2. Refresh Rotation & Reuse Detection
**Security Critical:** We implement **Rotation Families**.
1. **Normal Flow:**
   - Client sends `RT_1`.
   - Server issues `RT_2` (and new Access Token).
   - Server invalidates `RT_1`.
2. **Attack Scenario (Reuse):**
   - Attacker steals `RT_1`.
   - Victim rotates to `RT_2`.
   - Attacker tries to use `RT_1`.
   - Server sees `RT_1` is already used (part of family `F_USER_DEVICE`).
   - Server **REVOKES** `RT_2` (and the whole family).
   - Both Attacker and Victim are logged out.

## 3. Logout
- **Explicit Logout:**
  - Blacklist the current Access Token `jti` in Redis (TTL = remaining exp).
  - Delete the Refresh Token from DB.
