# Branch Protection Policy

This document outlines the recommended branch protection settings for this repository to maintain code quality and security standards.

## Recommended Settings for `main` Branch

### Protection Rules

The following protection rules should be configured in GitHub Settings > Branches > Branch protection rules:

#### Required Status Checks
- [x] Require status checks to pass before merging
- [x] Require branches to be up to date before merging

**Required checks:**
- `unit / unit tests (1.25.x, ubuntu-latest)`
- `unit / unit tests (1.25.x, windows-latest)`
- `unit / unit tests (1.25.x, macos-latest)`
- `unit / unit tests (1.26.x, ubuntu-latest)`
- `unit / unit tests (1.26.x, windows-latest)`
- `unit / unit tests (1.26.x, macos-latest)`
- `lint / golangci-lint`
- `vulncheck / govulncheck`
- `gosec / gosec`
- `trivy / trivy scan (fs)`
- `workflow-lint / actionlint`
- `integration / integration tests`
- `license-check / license compliance`

#### Code Review Requirements
- [x] Require pull request reviews before merging
- **Number of required approvals:** 1 (minimum)
- [x] Dismiss stale pull request approvals when new commits are pushed
- [x] Require review from Code Owners (enforces `.github/CODEOWNERS`)

#### Merge Requirements
- [x] Require conversation resolution before merging
- [x] Require signed commits (recommended)
- [x] Require linear history (prevents merge commits)

#### Push Restrictions
- [x] Restrict who can push to matching branches
- **Allowed to push:** Repository administrators and maintainer team only
- [x] Do not allow bypassing the above settings

#### Additional Protections
- [x] Require deployments to succeed before merging
- [x] Lock branch (make read-only) - for release branches only
- [x] Do not allow force pushes
- [x] Do not allow deletions

## Rationale

### Why These Settings?

1. **Required Status Checks**: Ensures all CI tests pass, including security scans (gosec, govulncheck, Trivy) before code reaches main branch.

2. **Code Review**: Implements the "two pairs of eyes" principle - all code changes must be reviewed by at least one maintainer before merging.

3. **CODEOWNERS Integration**: Ensures critical files (core logic, CI/CD, security configs) get reviewed by designated experts.

4. **Signed Commits**: Provides cryptographic verification of commit authorship, preventing impersonation attacks.

5. **Linear History**: Maintains clean, auditable git history without merge commits, making it easier to track changes and perform bisection.

6. **No Force Push**: Prevents history rewriting on main branch, ensuring audit trail integrity.

## Implementation Status

These branch protection rules can be configured by repository administrators in:
```
Settings > Branches > Add branch protection rule
```

Branch pattern: `main`

## Security Benefits

This configuration addresses several OpenSSF Scorecard checks:

- **Branch-Protection**: Protected main branch with required reviews
- **Code-Review**: Mandatory PR reviews before merge
- **Maintained**: Active CI/CD enforcement demonstrates active maintenance

## Exceptions

Automated dependency updates from Dependabot/Renovate are allowed to auto-merge after:
1. All required CI checks pass
2. Automatic approval from `.github/workflows/auto-merge-deps.yml`
3. No conflicts or security vulnerabilities detected

This is safe because:
- Only runs for bot accounts (`dependabot[bot]`, `renovate[bot]`)
- Still requires all CI security scans to pass
- Uses `pull_request_target` with proper permission scoping
- Protected by Harden Runner egress policy auditing

## Monitoring

Branch protection effectiveness is monitored through:
- OpenSSF Scorecard weekly runs (`.github/workflows/scorecard.yml`)
- GitHub Security tab for code scanning alerts
- Dependabot alerts for vulnerable dependencies
- CI workflow status on all PRs

## Questions?

For questions about branch protection configuration, contact:
- Repository maintainers: @netresearch/go-cron-maintainers
- Security concerns: See `SECURITY.md`
