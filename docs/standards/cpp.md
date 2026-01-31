# C++ Coding Standards

## 1. Style Guide
We adhere strictly to the **Google C++ Style Guide**.
- **Naming:** `ClassName`, `functionName`, `variable_name`, `kConstant`.
- **Headers:** `.h` for headers, `.cc` for implementation. Use `#pragma once`.

## 2. Modern C++ Features (C++17/20)
- **Smart Pointers:** `std::unique_ptr` and `std::shared_ptr` are mandatory. **NO `new` / `delete`**.
- **RAII:** Resources (sockets, handles) must be managed by objects that release them in destructors.
- **Strings:** use `std::string` and `std::string_view` over `char*`.

## 3. Static Analysis
**Tool:** `clang-tidy`
**Enforcement:** CI fails on warnings.
**Required Check Groups:**
- `modernize-*`: Enforce `override`, `auto`, `nullptr`.
- `bugprone-*`: Catch common logic errors.
- `performance-*`: Catch unnecessary copies.

## 4. Thread Safety
- **Mutexes:** Prefer `std::scoped_lock` (C++17) or `std::lock_guard`.
- **Atomics:** Use `std::atomic` for simple counters; avoid `volatile`.

## 5. GStreamer Specifics
- Use `g_autoptr` or C++ wrappers (`gst-mm` equivalent) where possible to avoid reference counting leaks.
