# License File Format Specification

**Version:** 1.0  
**Status:** Approved for Phase 1.6

## File Structure
The license file (`.lic`) is a JSON object containing the signed payload and the cryptographic signature.

```json
{
  "payload_b64": "<base64_json_payload>",
  "sig_b64": "<base64_rsa_signature>",
  "alg": "RS256"
}
```

## Payload Structure
The decoded `payload_b64` is a JSON object:

```json
{
  "license_id": "uuid-string",
  "customer_name": "Acme Corp",       // Helper field (Do not log)
  "tenant_scope": "all",              // "all" or specific tenant-id
  "issued_at_utc": "2026-01-01T00:00:00Z",
  "valid_until_utc": "2027-01-01T00:00:00Z",
  "limits": {
    "max_cameras": 100,
    "max_nvrs": 10
  },
  "features": {
    "ai_analytics": true,
    "mobile_app": false
  }
}
```

## Cryptography
- **Algorithm**: RSA PKCS#1 v1.5 with SHA-256 (`RS256`).
- **Signature Input**: Raw bytes of the JSON payload (decoded from `payload_b64`).
- **Verification**: Public Key (PEM) configured in VMS.

## Validation Rules
1. **Format**: Must match JSON schema.
2. **Signature**: Must verify against configured Public Key.
3. **Time**: `issued_at <= Now < valid_until_utc`.
(Grace Period logic handles 30 days post-expiry).
