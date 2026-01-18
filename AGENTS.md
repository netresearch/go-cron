<!-- Managed by agent: keep sections and order; edit content, not structure. Last updated: 2026-01-18 -->

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
- DOM/DOW uses AND logic by default (consistent, enables useful patterns) (#277)
- Heap-based scheduling for O(log n) performance
- FakeClock for deterministic testing
- ObservabilityHooks for metrics integration

## Architecture Decision Records (ADRs)

**IMPORTANT:** Before making architectural changes, read the relevant ADRs in `docs/adr/`. These document key design decisions that MUST be respected.

| ADR | Decision | Summary |
|-----|----------|---------|
| [ADR-000](docs/adr/ADR-000-fork-rationale.md) | Fork Rationale | Why this fork exists, what it fixes, who maintains it |
| [ADR-001](docs/adr/ADR-001-heap-scheduling.md) | Min-Heap for Scheduling | O(log n) insert/remove, O(1) peek - do NOT revert to sorted slice |
| [ADR-002](docs/adr/ADR-002-panic-for-failures.md) | Panic-Based Failures | Jobs signal failure via panic, caught by wrappers - do NOT add error returns to Job interface |
| [ADR-003](docs/adr/ADR-003-async-observability.md) | Asynchronous Hooks | Hooks run in goroutines, non-blocking - do NOT make synchronous |
| [ADR-004](docs/adr/ADR-004-functional-options.md) | Functional Options | Use `WithX()` pattern for config - do NOT add config structs or setters |
| [ADR-005](docs/adr/ADR-005-decorator-pattern.md) | Decorator/Chain Pattern | JobWrapper composition - do NOT change Job interface signature |
| [ADR-006](docs/adr/ADR-006-sync-map-cache.md) | sync.Map for Cache | Lock-free reads for parser cache - do NOT use RWMutex |
| [ADR-007](docs/adr/ADR-007-nw-skip-invalid-days.md) | nW Skips Invalid Days | `31W` in February skips month - use `LW` for every-month behavior |
| [ADR-008](docs/adr/ADR-008-dom-dow-and-logic.md) | DOM/DOW AND Logic | Both must match when restricted - use `DowOrDom` for legacy OR |
| [ADR-009](docs/adr/ADR-009-entry-id-sentinel.md) | Entry ID Sentinel | `EntryID(0)` is invalid; `entry.Valid()` checks this |
| [ADR-010](docs/adr/ADR-010-channel-synchronization.md) | Channel Sync Model | Run loop owns state; channels serialize access - deadlock-free |
| [ADR-011](docs/adr/ADR-011-dual-index-maps.md) | Dual-Index Maps | O(1) lookup by ID and Name; memory compaction on high churn |
| [ADR-012](docs/adr/ADR-012-index-compaction.md) | Map Index Compaction | Threshold-based map recreation to reclaim memory |
| [ADR-013](docs/adr/ADR-013-heap-index-tracking.md) | Entry Heap Index | Entry stores heapIndex for O(log n) removal |
| [ADR-014](docs/adr/ADR-014-max-idle-duration.md) | Max Idle Duration | 100,000 hours as practical infinity for responsive idle |
| [ADR-015](docs/adr/ADR-015-zero-time-sentinel.md) | Zero Time Sentinel | time.Time{} signals schedule exhaustion |
| [ADR-016](docs/adr/ADR-016-dst-normalization.md) | DST Normalization | ISC cron behavior for spring-forward/fall-back |
| [ADR-017](docs/adr/ADR-017-job-with-context.md) | JobWithContext | Optional interface for context-aware jobs |
| [ADR-018](docs/adr/ADR-018-run-flags.md) | Run Flags | WithRunImmediately() and WithRunOnce() entry flags |
| [ADR-019](docs/adr/ADR-019-atomic-entry-limit.md) | Atomic Entry Limit | CAS loop for lock-free entry count limiting |

When proposing changes that conflict with an ADR, you MUST:
1. Read the full ADR including alternatives considered
2. Propose a new ADR that supersedes the old one
3. Document why the original decision no longer applies

## Global rules

- Keep PRs small (~300 net LOC max)
- Conventional Commits: `type(scope): subject`
- Ask before: adding dependencies, breaking API changes, repo-wide rewrites
- Never commit secrets or sensitive data
- Maintain backwards compatibility with `robfig/cron/v3` API

## Pre-commit checks

```bash
# Quick verification (recommended before commit)
make verify                    # Runs: tidy, lint, test-race

# Individual targets
make build                     # Typecheck
make lint                      # golangci-lint (gocyclo, govet, staticcheck, etc.)
make test-race                 # Tests with race detection
make test-coverage             # Tests with coverage report

# Integration tests (real time, slower)
go test -tags=integration -v -run "^TestRealTime"

# Coverage threshold: 70% (CI enforced)
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

### Patterns used (per ADRs)
- **Functional options**: `WithLocation()`, `WithSeconds()`, `WithChain()` (ADR-004)
- **Interface segregation**: `Job`, `Schedule`, `Logger` are minimal (ADR-002)
- **Chain/Decorator pattern**: `JobWrapper` for cross-cutting concerns (ADR-005)
- **Heap-based scheduling**: Min-heap for entry management (ADR-001)

## Architecture overview

| File | Purpose |
|------|---------|
| `cron.go` | Main Cron scheduler, entry management, run loop |
| `parser.go` | Cron expression parsing (standard + Quartz formats) |
| `spec.go` | `SpecSchedule` - cron spec to next-time calculation |
| `option.go` | Functional options for Cron configuration |
| `chain.go` | Job wrappers: `Recover`, `SkipIfStillRunning`, `Timeout`, etc. |
| `retry.go` | `RetryWithBackoff`, `CircuitBreaker` wrappers |
| `validate.go` | `ValidateSpec`, `AnalyzeSpec` for expression validation |
| `introspect.go` | Schedule introspection: `Bounds()`, `Fields()`, `Matches()` |
| `observability.go` | `ObservabilityHooks` for metrics integration |
| `logger.go` | Logger interface compatible with go-logr/logr |
| `clock.go` | Clock abstraction with `FakeClock` for testing |
| `heap.go` | Min-heap implementation for entry scheduling |

## Documentation index

| Document | Purpose |
|----------|---------|
| `docs/ARCHITECTURE.md` | Internal design, data structures, algorithms |
| `docs/COOKBOOK.md` | Practical recipes for common patterns |
| `docs/MIGRATION.md` | Migration guide from robfig/cron |
| `docs/API_REFERENCE.md` | Public API documentation |
| `docs/DST_HANDLING.md` | Daylight saving time behavior |
| `docs/TESTING_GUIDE.md` | Testing strategies and FakeClock usage |
| `docs/OPERATIONS.md` | Production deployment, shutdown, monitoring |
| `docs/TROUBLESHOOTING.md` | Common issues and debugging techniques |
| `docs/PROJECT_INDEX.md` | Complete project file index |
| `docs/adr/` | Architecture Decision Records (20 ADRs) |

## PR/commit checklist

- [ ] `make verify` passes (tidy, lint, test-race)
- [ ] Coverage >= 70% for new code (`make test-coverage`)
- [ ] No breaking changes to public API (or documented in PR)
- [ ] Test edge cases: DST transitions, timezone handling, panic recovery
- [ ] Conventional commit message format used
- [ ] PR template filled out completely
- [ ] **ADRs reviewed** if touching architectural areas

## Good vs bad examples

### Adding a new option (per ADR-004)
```go
// GOOD: Follows functional options pattern
func WithCustomLogger(l Logger) Option {
    return func(c *Cron) {
        c.logger = l
    }
}

// BAD: Doesn't follow ADR-004
func (c *Cron) SetCustomLogger(l Logger) {
    c.logger = l
}
```

### Adding a new wrapper (per ADR-005)
```go
// GOOD: Follows decorator pattern
func MyWrapper(logger Logger) JobWrapper {
    return func(j Job) Job {
        return FuncJob(func() {
            // wrapper logic
            j.Run()
        })
    }
}

// BAD: Modifying Job interface (violates ADR-002, ADR-005)
type JobWithError interface {
    Run() error  // DON'T DO THIS
}
```

### Testing schedule edge cases
```go
// GOOD: Tests DST transition explicitly with FakeClock
func TestDSTSpringForward(t *testing.T) {
    loc, _ := time.LoadLocation("America/New_York")
    clock := NewFakeClock(time.Date(2024, 3, 10, 1, 59, 0, 0, loc))
    c := New(WithClock(clock), WithLocation(loc))
    // Test job scheduled during non-existent hour
}

// BAD: Assumes local timezone behavior
func TestSchedule(t *testing.T) {
    // Uses time.Local implicitly - non-deterministic
}
```

## Security considerations

- Never log user-provided cron expressions without sanitization
- `Recover()` wrapper should be default for production use
- Timezone names are user-controlled input - validate with `time.LoadLocation`
- CI runs: govulncheck, gosec, CodeQL, gitleaks, trivy

## When stuck

1. **ADRs**: Check `docs/adr/` for design rationale before proposing changes
2. **Cookbook**: See `docs/COOKBOOK.md` for practical recipes
3. **Contributing**: See `CONTRIBUTING.md` for development workflow
4. **Cron expression syntax**: See `doc.go` for comprehensive documentation
5. **DST behavior**: Check `docs/DST_HANDLING.md` and `spec.go` tests
6. **Original design**: https://github.com/robfig/cron (issues/PRs reference context)
7. **Security issues**: See `SECURITY.md` for reporting vulnerabilities

## Index of scoped AGENTS.md

No scoped files needed - this is a flat library structure.

## When instructions conflict

1. **ADRs take precedence** for architectural decisions
2. Nearest `AGENTS.md` wins for operational guidance
3. Explicit user prompts override file instructions
4. For Go idioms, defer to standard library conventions
5. For cron behavior, match ISC cron specification
