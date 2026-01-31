# Rust Coding Standards

## 1. Style & Linting
**Tool:** `cargo clippy`
**Policy:** `clippy::pedantic` warnings are treated as **Errors** in CI.
- **Waivers:** If a lint rule is incorrect or unhelpful, allow-list it at the module level with a comment justifying why.

## 2. Error Handling
- **Application Code:** Use `anyhow::Result` for top-level application logic (`vms-recorder`).
- **Library Code:** Use `thiserror` to define strongly typed enums for library crates (`ts-video-segmenter`).
- **Panic:** `unwrap()` and `expect()` are **FORBIDDEN** in production code. Use `?` operator or graceful fallback.

## 3. Unsafe Code Policy
- **Default:** `unsafe` is forbidden.
- **Justification:** Any usage of `unsafe` requires:
    1. A comment `// SAFETY: explain why invariants hold`.
    2. Explicit approval from a Principal Engineer during Code Review.

## 4. Workspace Layout
We use a Cargo Workspace:
- `crates/`: Reusable libraries (e.g., `crates/rtsp-client`).
- `services/`: Executable binaries (e.g., `services/recorder`).
