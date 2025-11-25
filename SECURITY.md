# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| main    | :white_check_mark: |
| < main  | :x:                |

Only the latest version on the `main` branch receives security updates.

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

Instead, please report them via one of these methods:

### Option 1: GitHub Security Advisories (Preferred)

1. Go to the [Security Advisories](https://github.com/netresearch/go-cron/security/advisories) page
2. Click "Report a vulnerability"
3. Provide details about the vulnerability

### Option 2: Email

Send an email to security@netresearch.de with:

- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Any suggested fixes (optional)

## What to Expect

1. **Acknowledgment**: We will acknowledge receipt within 48 hours
2. **Assessment**: We will assess the vulnerability and determine severity
3. **Updates**: We will keep you informed of our progress
4. **Resolution**: We aim to resolve critical issues within 7 days
5. **Disclosure**: We will coordinate disclosure timing with you

## Security Measures

This project employs several security practices:

### Automated Scanning

- **CodeQL**: Static analysis for security vulnerabilities
- **govulncheck**: Go vulnerability database checking
- **gosec**: Go security checker
- **gitleaks**: Secret detection in commits
- **Trivy**: Filesystem vulnerability scanning
- **Dependabot**: Dependency vulnerability alerts

### Code Review

- All changes require PR review
- Security-sensitive changes require additional scrutiny
- CI must pass before merging

### Best Practices

- No external dependencies (stdlib only)
- Input validation for cron expressions and timezones
- Panic recovery in job execution

## Security Considerations for Users

When using this library:

1. **Timezone Input**: Timezone names come from user input - validate with `time.LoadLocation` before use
2. **Panic Recovery**: Always use `cron.Recover()` wrapper in production
3. **Logging**: Be cautious about logging cron expressions that may contain sensitive scheduling patterns
4. **Goroutine Leaks**: Ensure `cron.Stop()` is called during shutdown

## Vulnerability Disclosure Policy

- We follow coordinated disclosure practices
- We credit reporters (unless they prefer anonymity)
- We aim to fix vulnerabilities before public disclosure
- We will publish security advisories for significant issues
