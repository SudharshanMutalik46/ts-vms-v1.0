# Token Security Policy

## 1. Signing Algorithm
We use **RS256** (RSA Signature with SHA-256).
- **Private Key:** Held only by `vms-control` (Auth Service).
- **Public Key:** Distributed to other services to verify tokens.

## 2. Key Rotation
- **KID (Key ID):** Every JWT header includes `kid`.
- **Strategy:**
    1. Generate new Keypair (Active).
    2. Move old Keypair to "Verification Only".
    3. Wait 24h (Max token TTL).
    4. Delete old Keypair.

## 3. Blacklisting (Revocation)
Since JWTs are stateless, we need a mechanism to revoke them instantly (e.g., Admin blocks user).

**Mechanism:** Redis Blacklist.
- **Key:** `blacklist:jti:{token_jti}`
- **Value:** `revoked`
- **TTL:** Set to `exp - now`.
- **Check:** API Gateway checks Blacklist for every request.

## 4. Rate Limiting
To prevent Brute Force attacks on Login:
- **Login Endpoint:** 5 attempts per IP per minute.
- **Lockout:** After 5 failures, exponential backoff (1m, 5m, 15m).
