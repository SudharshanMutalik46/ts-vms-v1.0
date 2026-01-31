# Go Coding Standards

## 1. Project Structure
We follow the standard Go project layout:
- `cmd/`: Main entry points (e.g., `cmd/vms-control/main.go`).
- `internal/`: Private application code.
- `pkg/`: Library code safe for import by other projects.
- `generated/`: Output for `protoc` (do not edit manually).

## 2. Structured Logging
**Default Library:** `log/slog` (Standard Library).
**Usage:**
```go
slog.Info("camera connected", "camera_id", id, "ip", ip)
```
- **Forbidden:** `fmt.Printf` or `log.Println` in production code.
- **Allowed Exception:** `uber-go/zap` is permissible only in the **Media Plane** hot paths if CPU profiling proves `slog` is a bottleneck.

## 3. Error Handling
- **Wrap Errors:** Use `fmt.Errorf("action failed: %w", err)` to provide context.
- **Check Errors:** Never use `_` to ignore errors.
- **Sentinel Errors:** Define errors in `pkg/errors` for easy matching with `errors.Is()`.

## 4. Concurrency & Context
- **Context:** Every long-running function or I/O operation MUST accept `ctx context.Context` as the first argument.
- **Cancellation:** Always handle `ctx.Done()` in loops.
- **Goroutines:** Never start a goroutine without a known stop mechanism (WaitGroup + Context).

## 5. Linting Policy
- **Tool:** `golangci-lint`
- **Policy:** CI fails on any lint error.
- **Zero Tolerance:** No `nolint` comments without an attached issue ID explaining why.
