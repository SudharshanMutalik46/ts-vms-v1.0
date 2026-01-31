# Code Review Process

## 1. Pull Request Checklist
Every PR must meet the following criteria before merge:

- [ ] **Tests Pass:** CI must be green (Unit tests + Lint).
- [ ] **Coverage:** 80% minimum coverage maintained.
- [ ] **Standards:** Complies with language-specific standards (Go/C++/Rust).
- [ ] **Docs:** `README.md` or API docs updated if public interface changed.
- [ ] **Secrets:** No hardcoded secrets or keys.

## 2. Reviewer Requirements
- **Approvals:** At least **1 approval** from a Code Owner.
- **Bot Checks:** All automated checks (Lint, Build, Test) must pass.

## 3. "No Merge If"
A PR is blocked if:
- Currently breaking the build.
- Contains `TODO` or `FIXME` comments without an associated JIRA/Issue ticket.
- Contains unbounded loops or unchecked recursion depth (Reviewer discretion).

## 4. Definition of Done
A feature is "Done" only when:
1. Rebased on `main`.
2. Reviewed & Approved.
3. Merged.
4. Deployed to Staging (Phase 1+).
