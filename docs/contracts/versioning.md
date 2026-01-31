# Contract Versioning Strategy

## 1. Semantic Versioning
- **Protos:** `ts.vms.<domain>.v<Major>` (e.g., `v1`).
- **REST:** `/api/v<Major>/` (e.g., `/api/v1/cameras`).

## 2. Backward Compatibility Rules (gRPC)
We follow the standard Protobuf compatibility guidelines:
- **ADDITIVE ONLY:** New fields can be added to messages. They must be optional.
- **NEVER REMOVE:** Fields should never be removed. Use `deprecated = true` instead.
- **NEVER RENUMBER:** Field tags/numbers must be immutable.
- **NEVER RENAME:** Field names can be changed technically, but avoid it to prevent JSON mapping confusion.

## 3. Backward Compatibility Rules (REST)
- **ADDITIVE:** New JSON properties are allowed.
- **IGNORE UNKNOWN:** Clients must be robust and ignore unknown fields.
- **DEPRECATION:** Use standard HTTP `Warning` header or `Deprecated: true` in OpenAPI.

## 4. Breaking Changes
- If a breaking change is strictly necessary (rare), a NEW version package must be created (e.g., `v2`).
- The system must support both `v1` and `v2` in parallel for at least 1 release cycle.
