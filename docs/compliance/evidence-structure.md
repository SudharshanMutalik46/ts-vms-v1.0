# Evidence Collection Structure

## 1. Repository Layout
To maintain audit readiness, evidence is organized by **Release Version**.

```
/evidence/
  ├── releases/
  │   ├── v1.0.0/
  │   │   ├── functional/      # Screenshots, Videos, Test Reports
  │   │   ├── security/        # Pen-test Report, Scan Results
  │   │   └── bom/             # SBOM.json, Hardware-BOM.csv
  │   └── v1.1.0/
  ├── compliance/
  │   ├── ndaa/                # Vendor Attestations (Global)
  │   └── stqc/                # Lab Correspondence & Certificates
```

## 2. Naming Convention
- **Format:** `[Date]_[TestID]_[Status].ext`
- **Example:** `2026-03-15_F-STR-01_PASS.png`

## 3. Integrity Policy
- **Immutable:** Once a release is tagged, the evidence folder is locked (Read-Only).
- **Hashed:** A manifest file (`SHA256SUMS`) is generated for the evidence folder.
- **Sign-Off:** The `SHA256SUMS` file is GPG signed by the Release Manager.

## 4. Roles
- **Evidence Collector:** QA Engineer.
- **Evidence Reviewer:** Security Lead.
- **Approver:** Release Manager.
