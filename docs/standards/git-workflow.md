# Git Workflow

## 1. Trunk-Based Development
We follow **Trunk-Based Development**.
- **Main Branch:** `main` (Production-ready source of truth).
- **Feature Branches:** Use short-lived branches off `main`. Merge back via PR frequently (days, not weeks).

## 2. Branch Naming
Format: `type/short-description`
- `feat/add-camera-api`
- `fix/memory-leak-recorder`
- `refactor/optimize-database`
- `docs/update-readme`
- `chore/upgrade-deps`

## 3. Conventional Commits (Strict)
All commit messages MUST follow [Conventional Commits](https://www.conventionalcommits.org/).

Format: `<type>(<scope>): <description>`

**Allowed Types:**
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation only
- `style`: Formatting, missing semi-colons, etc.
- `refactor`: Code change that neither fixes a bug nor adds a feature
- `perf`: Performance improvement
- `test`: Adding missing tests
- `chore`: Build process, aux tools

**Example:**
`feat(camera): implement ONVIF discovery timeout`

## 4. Release Strategy
- **Tags:** Releases are tagged on `main` using SemVer (`v0.1.0`, `v1.2.3`).
- **Hotfixes:** Create a branch `hotfix/v1.2.4` from the tag `v1.2.3`, cherry-pick fix, tag `v1.2.4`, merge back to `main`.
