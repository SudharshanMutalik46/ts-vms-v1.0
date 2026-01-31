# Security Compliance Checklist

Devs and Ops must verify these items before any release.

## 1. Secrets Handling
- [ ] No hardcoded secrets in source code.
- [ ] `.env` files added to `.gitignore`.
- [ ] CI/CD secrets Redacted in logs.

## 2. Authentication & Session
- [ ] Password Hashing uses Argon2id.
- [ ] Refresh Tokens are HttpOnly / Secure cookies.
- [ ] Logout invalidates both Access and Refresh tokens.
- [ ] Testing: Reuse of old refresh token triggers family revocation.

## 3. Authorization (RBAC)
- [ ] Cross-tenant access returns 404 (not just 403).
- [ ] Default Deny enabled for all new API endpoints.
- [ ] Privilege Escalation check: Users cannot assign roles higher than their own.

## 4. Audit Logging
- [ ] Retention policy set to 7 Years.
- [ ] Sensitive fields (passwords) are NOT logging.
- [ ] Audit logs are backed up to write-once storage.

## 5. Encryption at Rest
- [ ] RTSP Credentials encrypted with AES-256-GCM.
- [ ] AAD (Context) used for all encryption operations.
- [ ] DB disks encrypted (BitLocker/LUKS).

## 6. Tenant Isolation
- [ ] DB Queries always include `WHERE tenant_id = ?`.
- [ ] Cache keys prefixed with `{tenant_id}:`.

## 7. Secure Defaults
- [ ] Service binds to `127.0.0.1` unless external access explicit.
- [ ] TLS 1.2+ enforced for all external connections.

## 8. Incident Response
- [ ] Playbook exists for "Key Compromise" (Rotate KEK).
- [ ] Playbook exists for "Data Leak" (Identify logs).
