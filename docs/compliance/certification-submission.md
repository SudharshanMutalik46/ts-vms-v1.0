# Certification Submission Process

## 1. Pre-Submission Gate
Before submitting to STQC or any lab, the **Internal Audit** must pass.
- [ ] All Critical/High Security bugs closed?
- [ ] Functional Critical Path 100% pass?
- [ ] SBOM generated and NDAA compliant?
- [ ] Evidence Repository populated and Signed?

## 2. Submission Package
The "Golden Package" sent to the Lab includes:
1. **Product Binary:** The specific Hash-locked `.zip` bundle.
2. **Documentation:** User Manual, Admin Guide, Architecture Docs (Phase 0.1).
3. **Compliance Docs:** NDAA Declaration, BOM/SBOM.
4. **Test Reports:** Internal Security & Functional reports.

## 3. Workflow
1. **Prepare:** QA & Compliance Lead assemble the package.
2. **Review:** Engineering VP reviews the "Known Issues" list.
3. **Submit:** Physical/Digital transfer to Lab.
4. **Respond:** Lab issues "Observation Report" (NCs).
5. **Remediate:** Team fixes NCs within 30 days.
6. **Certify:** Lab issues Certificate.

## 4. Maintenance
- **Minor Changes:** Internal Note to File.
- **Major Changes:** Re-submission (Delta Audit) required.

**Dependencies**: [Phase 0.4 Git Flow](/docs/standards/git-workflow.md), [Phase 0.6 Upgrades](/docs/offline/offline-upgrades.md).
