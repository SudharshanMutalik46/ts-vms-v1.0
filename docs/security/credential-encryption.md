# Credential Encryption Strategy

## 1. Algorithm
We use **AES-256-GCM** (Galois/Counter Mode) for all secrets at rest.
- **Why?** Provides Authenticated Encryption (confidentiality + integrity).

## 2. Envelope Encryption
Secrets are never encrypted directly with the master key.

1. **KEK (Key Encryption Key):**
   - Scope: **Per Tenant**.
   - Storage: Secure Vault / Environment Variable (Phase 0).
   - Rotation: 90 days.

2. **DEK (Data Encryption Key):**
   - Scope: **Per Credential**.
   - Generation: Random 32 bytes.
   - Storage: Encrypted by KEK, stored alongside the data.

## 3. AAD (Additional Authenticated Data)
To prevent "Copy-Paste Attacks" (splicing encrypted credentials from Camera A to Camera B), we enforce **AAD**.
- **Context:** `tenant_id + camera_id`
- **Result:** If an attacker copies the encrypted password blob from Camera A to Camera B, decryption will FAIL because the ID check fails inside GCM.

## 4. Storage Rules
- **Non-Recoverable:** User passwords are hashed (Argon2id), never encrypted.
- **Recoverable:** Camera RTSP passwords MUST be encrypted (we need plaintext to connect).
- **Logging:** NEVER log plaintext credentials.

## 5. Rotation Policy
- **Key Rotation:** When KEK rotates, we re-encrypt all DEKs in a background job. The credentials themselves do not change.
