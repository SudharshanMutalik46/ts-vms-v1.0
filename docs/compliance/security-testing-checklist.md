# Security Testing Checklist

## 1. Authentication & Session
- [ ] **Brute Force:** Verify account lockout after 5 failed attempts.
- [ ] **Session Expiry:** Verify token works at T-1m and fails at T+1m.
- [ ] **Refresh Rotation:** Verify reusing an old refresh token revokes the family. (Ref: Phase 0.5)

## 2. RBAC & Tenant Isolation
- [ ] **Horizontal Escalation:** Verify User A cannot access User B's camera (different tenant).
- [ ] **Vertical Escalation:** Verify Operator cannot create new Users.
- [ ] **Default Deny:** Verify hitting an undefined API endpoint returns 401/404, not 500.

## 3. Encryption
- [ ] **At Rest:** Verify `passwords` column in DB is hashed (Argon2).
- [ ] **Transport:** Verify HTTP requests are rejected (must be HTTPS).
- [ ] **Secrets:** Verify `.env` or config files do not contain plaintext RTSP passwords.

## 4. Input Validation
- [ ] **Injection:** Attempt SQLi on Login field (`' OR 1=1 --`).
- [ ] **XSS:** Attempt script injection in Camera Name field.

## Severity Ratings
- **Critical:** Data Leak, Auth Bypass, Remote Code Execution.
- **High:** Tenant Isolation failure, DoS.
- **Medium:** Weak password policy, Information Disclosure (Stack trace).
- **Low:** UI bugs, Typo.
