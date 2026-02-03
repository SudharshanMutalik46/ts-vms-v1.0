# Key Rotation Strategy

This document outlines the procedures for managing Master Keys used in Envelope Encryption for Camera Credentials.

## Configuration

Two environment variables control the Keyring:

- `MASTER_KEYS`: A JSON array of key objects.
- `ACTIVE_MASTER_KID`: The ID of the key used for **new** encryption operations.

### Key Format
Each key in `MASTER_KEYS` must have:
- `kid`: Unique Identifier (string).
- `material`: 32-byte AES-256 key, standard Base64 encoded.

**Example:**
```json
[
  {"kid": "2026-01-v1", "material": "base64-encoded-32-bytes..."},
  {"kid": "2026-06-v2", "material": "base64-encoded-32-bytes..."}
]
```

## Rotation Procedure

To rotate the Master Key (e.g., periodic rotation or compromise):

### 1. Generate New Key
Generate a new 32-byte key and assign a new KID (e.g., date-based).
```bash
openssl rand -base64 32
```

### 2. Append to Keyring
Update the `MASTER_KEYS` environment variable to include the **new** key alongside the **existing** keys.
**Do NOT remove old keys yet.** Old records still depend on them for decryption.

### 3. Deploy (Phase A - Introduction)
Deploy the service with the updated `MASTER_KEYS`.
- Keep `ACTIVE_MASTER_KID` pointing to the **OLD** key.
- This ensures the new key is available in memory but not used for writing yet.
- Verify the service starts successfully.

### 4. Switch Active Key (Phase B - Activation)
Update `ACTIVE_MASTER_KID` to the **NEW** KID.
Restart the service.
- **New Writes**: Will use the new key.
- **Reads**: Will use the new key (for new data) or old keys (for legacy data) automatically based on the record's `master_kid` field.

### 5. Verify & Monitor
Check audit logs for `camera.credential.read` failures related to decryption.
Ensure both old and new credentials are readable.

## Legacy Key Retirement

Old keys should remain in `MASTER_KEYS` as long as there are database records encrypted with them.
Removing a key while records still use it will render those credentials PERMANENTLY INACCESSIBLE.

To retire a key:
1. Ensure no records use the old `kid` (Scan database: `SELECT COUNT(*) FROM camera_credentials WHERE master_kid = '...'`).
2. If count > 0, you must update those records (read and re-write them via API) to re-wrap them with the active key.
3. Once count is 0, you can remove the key from `MASTER_KEYS` configuration.

## Disaster Recovery
If `MASTER_KEYS` configuration is lost, all encrypted credentials are lost.
**BACKUP YOUR ENVIRONMENT VARIABLES SECURELY.**
