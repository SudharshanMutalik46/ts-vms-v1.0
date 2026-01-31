# Component Origin Documentation

## 1. Requirement
We must prove the "Chain of Custody" and origin for all critical components to satisfy Trusted Supply Chain requirements.

## 2. Provenance Policy
- **Hardware:**
    - Must define **Country of Origin (CoO)** for final assembly.
    - Must identify **Chipset Manufacturer** for critical SoCs.
- **Software:**
    - All First-Party binaries must be traceable to a **Git Commit SHA**.
    - All Third-Party libs must come from trusted repos (official Go Proxy, vcpkg main).

## 3. Artifacts
For every Release Candidate, we generate `origin-audit.json`:
```json
{
  "release": "v1.2.0",
  "build_date": "2026-03-15",
  "builder_id": "CI-Worker-05",
  "git_sha": "a1b2c3d4",
  "components": [
    {
      "name": "ffmpeg.dll",
      "source": "vcpkg",
      "origin_url": "https://github.com/microsoft/vcpkg",
      "verification": "SHA256:..."
    }
  ]
}
```

## 4. Chain of Custody
- Artifacts are signed immediately after build.
- Stored in "Write-Once" artifact repository.
