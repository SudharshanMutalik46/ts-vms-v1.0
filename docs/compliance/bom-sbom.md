# BOM / SBOM Tracking System

## 1. Definitions
- **BOM (Hardware):** List of physical components (Motherboard, CPU, HDD, Camera Sensor).
- **SBOM (Software):** List of software libraries, frameworks, and runtimes (Go mods, C++ libs, OS packages).

## 2. SBOM Specification
**Format:** [CycloneDX](https://cyclonedx.org/) JSON.
**Required Fields:**
- `Component Name`
- `Version`
- `Supplier / Vendor`
- `License` (e.g., MIT, Apache 2.0, GPL)
- `PURL` (Package URL)
- `Hash` (SHA256)
- `Approval Status` (Approved / Pending / Rejected)
- `Last Reviewed Date`

## 3. Hardware BOM Specification
**Format:** Custom CSV.
**Required Fields:**
- `Part Number`
- `Description`
- `Manufacturer`
- `Country of Origin`
- `NDAA Compliant` (Yes/No)

## 4. Workflow
- **Phase 0.7 (Now):** Manual CSV maintenance in `/docs/compliance/bom/`.
- **Phase 1+:** Automated generation via `govulncheck` and CI/CD pipelines.
- **Audit Gate:** Release cannot proceed if any item is `Rejected` or `License Conflict`.
