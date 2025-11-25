<!-- Managed by agent: keep sections and order; edit content, not structure. Last updated: 2025-11-25 -->

# AGENTS.md

Maintained fork of robfig/cron - a cron spec parser and job scheduler for Go.

**Precedence:** The **closest `AGENTS.md`** to the files you're changing wins. This root file holds global defaults.

## Project context

| Attribute | Value |
|-----------|-------|
| Language | Go 1.25+ |
| Module | `github.com/netresearch/go-cron` |
| Type | Library (zero external dependencies) |
| API | Drop-in replacement for `robfig/cron/v3` |
| CI | GitHub Actions (unit, lint, security scans) |

### Key improvements over robfig/cron
- Panic fixes for TZ= parsing (issues #554, #555)
- `Entry.Run()` method for proper chain decorator invocation (#551)
- DST "spring forward" jobs run immediately (ISC cron behavior, PR #541)

## Global rules

- Keep PRs small (~300 net LOC max)
- Conventional Commits: `type(scope): subject`
- Ask before: adding dependencies, breaking API changes, repo-wide rewrites
- Never commit secrets or sensitive data
- Maintain backwards compatibility with `robfig/cron/v3` API

## Pre-commit checks

```bash
# Required (CI enforced)
go build ./...                              # Typecheck
golangci-lint run                           # Lint (gocyclo, govet, staticcheck, etc.)
go test -race ./...                         # Tests with race detection

# Coverage threshold: 70% (CI enforced)
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | tail -1
```

## Code style

### Enforced by golangci-lint
- **gocyclo**: Max complexity 25 (relaxed for tests)
- **misspell**: US English spelling
- **staticcheck**: All checks except S1000, S1037
- **Formatters**: gofmt, gofumpt, goimports, gci

### Import order (gci)
```go
import (
    // 1. Standard library
    "context"
    "time"

    // 2. External packages (none in this project)

    // 3. Local packages
    "github.com/netresearch/go-cron"
)
```

### Patterns used
- **Functional options**: `WithLocation()`, `WithSeconds()`, `WithChain()`
- **Interface segregation**: `Job`, `Schedule`, `Logger` are minimal
- **Chain pattern**: `JobWrapper` for cross-cutting concerns

## Architecture overview

| File | Purpose |
|------|---------|
| `cron.go` | Main Cron scheduler, entry management, run loop |
| `parser.go` | Cron expression parsing (standard + Quartz formats) |
| `spec.go` | `SpecSchedule` - cron spec to next-time calculation |
| `option.go` | Functional options for Cron configuration |
| `chain.go` | Job wrappers: `Recover`, `SkipIfStillRunning`, `DelayIfStillRunning` |
| `logger.go` | Logger interface compatible with go-logr/logr |
| `constantdelay.go` | `@every` interval schedule implementation |

## PR/commit checklist

- [ ] `go mod tidy` - go.mod is clean
- [ ] `go build ./...` - Compiles without errors
- [ ] `golangci-lint run` - No linter warnings
- [ ] `go test -race ./...` - All tests pass
- [ ] Coverage >= 70% for new code
- [ ] No breaking changes to public API (or documented in PR)
- [ ] Test edge cases: DST transitions, timezone handling, panic recovery

## Good vs bad examples

### Adding a new option
```go
// GOOD: Follows existing pattern
func WithCustomLogger(l Logger) Option {
    return func(c *Cron) {
        c.logger = l
    }
}

// BAD: Doesn't follow functional options pattern
func (c *Cron) SetCustomLogger(l Logger) {
    c.logger = l
}
```

### Testing schedule edge cases
```go
// GOOD: Tests DST transition explicitly
func TestDSTSpringForward(t *testing.T) {
    loc, _ := time.LoadLocation("America/New_York")
    // Test job scheduled during non-existent hour
}

// BAD: Assumes local timezone behavior
func TestSchedule(t *testing.T) {
    // Uses time.Local implicitly
}
```

## Security considerations

- Never log user-provided cron expressions without sanitization
- `Recover()` wrapper should be default for production use
- Timezone names are user-controlled input - validate with `time.LoadLocation`
- CI runs: govulncheck, gosec, CodeQL, gitleaks, trivy

## When stuck

1. **Cron expression syntax**: See `doc.go` for comprehensive documentation
2. **DST behavior**: Check `spec.go` and related tests for edge cases
3. **Original design**: https://github.com/robfig/cron (issues/PRs reference context)
4. **Go scheduling**: https://pkg.go.dev/time#Timer

## Index of scoped AGENTS.md

No scoped files needed - this is a flat library structure.

## When instructions conflict

- Nearest `AGENTS.md` wins (this is the only one)
- Explicit user prompts override file instructions
- For Go idioms, defer to standard library conventions
- For cron behavior, match ISC cron specification
