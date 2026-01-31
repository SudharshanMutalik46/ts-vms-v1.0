# API Documentation: [Service Name]

## Overview
This document describes internal APIs or library contracts not covered by the global OpenAPI spec.

## Go Interface
```go
type StreamHandler interface {
    // Connect establishes a new stream session
    Connect(ctx context.Context, url string) (Session, error)
}
```

## Internal gRPC
Describe any private RPC methods here.

## Error Codes
| Code | Meaning | Recovery |
| :--- | :--- | :--- |
| `ERR_BUSY` | Pipeline full | Retry with backoff |
