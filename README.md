[![Go Reference](https://pkg.go.dev/badge/github.com/netresearch/go-cron.svg)](https://pkg.go.dev/github.com/netresearch/go-cron)
[![CI](https://github.com/netresearch/go-cron/actions/workflows/ci.yml/badge.svg)](https://github.com/netresearch/go-cron/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/netresearch/go-cron/graph/badge.svg)](https://codecov.io/gh/netresearch/go-cron)
[![CodeQL](https://img.shields.io/github/actions/workflow/status/netresearch/go-cron/codeql.yml?label=CodeQL)](https://github.com/netresearch/go-cron/security/code-scanning)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/netresearch/go-cron/badge)](https://scorecard.dev/viewer/?uri=github.com/netresearch/go-cron)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/11696/badge)](https://www.bestpractices.dev/projects/11696)
[![Go Report Card](https://goreportcard.com/badge/github.com/netresearch/go-cron)](https://goreportcard.com/report/github.com/netresearch/go-cron)
[![Go Version](https://img.shields.io/github/go-mod/go-version/netresearch/go-cron)](go.mod)
[![Latest Release](https://img.shields.io/github/v/release/netresearch/go-cron)](https://github.com/netresearch/go-cron/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](CODE_OF_CONDUCT.md)
[![SLSA 3](https://slsa.dev/images/gh-badge-level3.svg)](https://slsa.dev)

# go-cron

A maintained fork of [robfig/cron](https://github.com/robfig/cron) — the most popular cron library for Go — with bug fixes, runtime schedule updates, per-entry context, resilience middleware, and modern toolchain support.

## Why?

The original `robfig/cron` has been unmaintained since 2020, accumulating 50+ open PRs and several critical panic bugs that affect production systems. This fork fixes those issues and adds features demanded by real-world dependents like [weaviate](https://github.com/weaviate/weaviate):

| Area | Original | This Fork |
|------|----------|-----------|
| TZ= parsing | Panics on malformed input | Fixed (#554, #555) |
| Chain decorators | `Entry.Run()` bypasses chains | Properly invokes wrappers (#551) |
| DST spring-forward | Jobs silently skipped | Runs immediately (ISC behavior, #541) |
| DOM/DOW logic | OR (confusing) | AND (logical, consistent) |
| Runtime updates | Remove + re-add | `UpdateSchedule`, `UpsertJob` |
| Pause/Resume | None | `PauseEntry`, `ResumeEntry` |
| Triggered jobs | None | `@triggered`, `TriggerEntry` |
| Context support | None | Per-entry context, `FuncJobWithContext` |
| Resilience | None | Retry, circuit breaker, timeout |
| Observability | None | Hooks for metrics (Prometheus, etc.) |
| Go version | Stuck on 1.13 | Go 1.25+ with modern toolchain |

## Installation

```bash
go get github.com/netresearch/go-cron
```

```go
import cron "github.com/netresearch/go-cron"
```

> [!NOTE]
> Requires Go 1.25 or later.

## Migrating from robfig/cron

Drop-in replacement — just change the import path:

```go
// Before
import "github.com/robfig/cron/v3"

// After
import cron "github.com/netresearch/go-cron"
```

The API is 100% compatible with robfig/cron v3. However, this fork includes
knowingly accepted behavior changes that fix bugs and inconsistencies in the
unmaintained upstream — see the [comparison table above](#why) for a summary.

> [!WARNING]
> **Behavior differences exist.** While the API is compatible, some runtime behavior
> has changed (DOM/DOW matching, DST handling, chain execution). Review
> [docs/MIGRATION.md](docs/MIGRATION.md) before upgrading production systems.

## Quick Start

```go
package main

import (
    "fmt"
    "time"

    cron "github.com/netresearch/go-cron"
)

func main() {
    c := cron.New()

    // Run every minute
    c.AddFunc("* * * * *", func() {
        fmt.Println("Every minute:", time.Now())
    })

    // Run at specific times
    c.AddFunc("30 3-6,20-23 * * *", func() {
        fmt.Println("In the range 3-6am, 8-11pm")
    })

    // With timezone
    c.AddFunc("CRON_TZ=Asia/Tokyo 30 04 * * *", func() {
        fmt.Println("4:30 AM Tokyo time")
    })

    c.Start()

    // Keep running...
    select {}
}
```

## Cron Expression Format

Standard 5-field cron format (minute-first):

| Field | Required | Values | Special Characters |
|-------|----------|--------|-------------------|
| Minutes | Yes | 0-59 | `* / , -` |
| Hours | Yes | 0-23 | `* / , -` |
| Day of month | Yes | 1-31 | `* / , - ?` |
| Month | Yes | 1-12 or JAN-DEC | `* / , -` |
| Day of week | Yes | 0-6 or SUN-SAT | `* / , - ?` |

### Predefined Schedules

| Entry | Description | Equivalent |
|-------|-------------|------------|
| `@yearly` | Once a year, midnight, Jan 1 | `0 0 1 1 *` |
| `@monthly` | Once a month, midnight, first day | `0 0 1 * *` |
| `@weekly` | Once a week, midnight Sunday | `0 0 * * 0` |
| `@daily` | Once a day, midnight | `0 0 * * *` |
| `@hourly` | Once an hour, beginning of hour | `0 * * * *` |
| `@every <duration>` | Every interval | e.g., `@every 1h30m` |
| `@triggered` | Never auto-runs; manual only | aliases: `@manual`, `@none` |

### Wraparound Ranges

For cyclic fields, ranges where start > end wrap around the boundary:

```go
// Run from 10pm to 2am (spans midnight)
c.AddFunc("0 22-2 * * *", nightJob)

// Run Friday through Monday (spans weekend)
c.AddFunc("0 9 * * FRI-MON", weekendJob)

// Run November through February (spans year boundary)
c.AddFunc("0 0 1 NOV-FEB *", winterJob)
```

Supported fields: seconds, minutes, hours, day-of-month, day-of-week, month.
Non-existent days (e.g., Feb 31) are simply skipped.

### Seconds Field (Optional)

Enable Quartz-compatible seconds field:

```go
// Seconds field required
cron.New(cron.WithSeconds())

// Seconds field optional
cron.New(cron.WithParser(cron.NewParser(
    cron.SecondOptional | cron.Minute | cron.Hour |
    cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)))
```

### Day Matching (DOM/DOW)

When both day-of-month and day-of-week are specified, **both must match** (AND logic). This is consistent with all other cron fields and enables useful patterns:

```go
// Last Friday of month (days 25-31 AND Friday)
c.AddFunc("0 0 25-31 * FRI", lastFridayJob)

// First Monday of month (days 1-7 AND Monday)
c.AddFunc("0 0 1-7 * MON", firstMondayJob)

// Friday the 13th
c.AddFunc("0 0 13 * FRI", unluckyJob)
```

> [!NOTE]
> This differs from robfig/cron which uses OR logic. For migration compatibility,
> use the `DowOrDom` option or see [docs/MIGRATION.md](docs/MIGRATION.md).

## Timezone Support

Specify timezone per-schedule using `CRON_TZ=` prefix:

```go
// Runs at 6am New York time
c.AddFunc("CRON_TZ=America/New_York 0 6 * * *", myFunc)

// Legacy TZ= prefix also supported
c.AddFunc("TZ=Europe/Berlin 0 9 * * *", myFunc)
```

Or set default timezone for all jobs:

```go
nyc, _ := time.LoadLocation("America/New_York")
c := cron.New(cron.WithLocation(nyc))
```

### Daylight Saving Time (DST) Handling

This library implements ISC cron-compatible DST behavior:

| Transition | Behavior |
|------------|----------|
| **Spring Forward** (hour skipped) | Jobs in skipped hour run immediately after transition |
| **Fall Back** (hour repeats) | Jobs run once, during first occurrence |
| **Midnight DST** (midnight doesn't exist) | Automatically normalized to valid time |

> [!TIP]
> For DST-sensitive applications, schedule jobs outside typical transition hours (1-3 AM) or use UTC.

See [docs/DST_HANDLING.md](docs/DST_HANDLING.md) for comprehensive DST documentation including examples, testing strategies, and edge cases.

## Named Jobs and Lookup

Assign names and tags to entries for lookup, update, and removal:

```go
c.AddFunc("0 9 * * *", dailyReport,
    cron.WithName("daily-report"),
    cron.WithTags("reports", "daily"),
)

// Lookup by name (O(1))
entry := c.EntryByName("daily-report")

// Filter by tag
entries := c.EntriesByTag("reports")

// Remove by name
c.RemoveByName("daily-report")
```

## Runtime Updates

Update schedules and jobs without remove+re-add:

```go
// Update schedule only (preserves job and context)
c.UpdateScheduleByName("daily-report", cron.Every(5*time.Minute))

// Update both schedule and job atomically (cancels old context)
c.UpdateEntryJobByName("daily-report", "30 10 * * *", newJob)

// Create-or-update in one call
id, err := c.UpsertJob("0 9 * * *", myJob, cron.WithName("my-job"))
```

For graceful replacement of long-running jobs:

```go
c.WaitForJobByName("my-job")  // Block until current execution finishes
c.UpsertJob(newSpec, newJob, cron.WithName("my-job"))
```

## Pause/Resume

Temporarily suspend individual entries without removing them:

```go
// Pause a running entry
c.PauseEntryByName("sync-job")

// Check if paused
if c.IsEntryPausedByName("sync-job") {
    fmt.Println("Job is paused")
}

// Resume when ready
c.ResumeEntryByName("sync-job")

// Add entry in paused state (activate later)
c.AddFunc("@every 5m", syncData, cron.WithPaused(), cron.WithName("sync"))
```

Paused entries remain registered with their schedule advancing, but execution is skipped. No catch-up flood occurs on resume.

## Triggered Jobs

Jobs that never fire automatically — only when you say so:

```go
// Register a triggered job
c.AddFunc("@triggered", deploy, cron.WithName("deploy"))
c.Start()

// Trigger on demand (e.g., from an HTTP handler)
c.TriggerEntryByName("deploy")

// Works on regular entries too — "run now"
c.TriggerEntry(scheduledEntryID)
```

Triggered entries benefit from the full middleware chain (retry, timeout, skip-if-running). Use `@triggered`, `@manual`, or `@none` — all are aliases.

## Context Support

Jobs implementing `JobWithContext` receive a per-entry context that is automatically canceled on removal or job replacement:

```go
c.AddJob("@every 1m", cron.FuncJobWithContext(func(ctx context.Context) {
    select {
    case <-ctx.Done():
        return // Entry removed or job replaced
    case <-time.After(10 * time.Second):
        // Work completed
    }
}))
```

All chain wrappers propagate context through the wrapper chain, so per-entry context reaches the innermost job.

## Job Wrappers (Middleware)

Add cross-cutting behavior using chains:

```go
// Apply to all jobs
c := cron.New(cron.WithChain(
    cron.Recover(logger),              // Recover panics
    cron.SkipIfStillRunning(logger),   // Skip if previous still running
))

// Apply to specific job
job := cron.NewChain(
    cron.DelayIfStillRunning(logger),  // Queue if previous still running
).Then(myJob)
```

Available wrappers:

| Wrapper | Description |
|---------|-------------|
| `Recover` | Catch panics, log, and continue |
| `SkipIfStillRunning` | Skip if previous run hasn't finished |
| `DelayIfStillRunning` | Queue until previous run finishes |
| `Timeout` | Abandon after duration (goroutine keeps running) |
| `TimeoutWithContext` | Cancel context after duration (cooperative cancellation) |
| `Jitter` / `JitterWithLogger` | Random delay to prevent thundering herd |
| `RetryWithBackoff` | Retry on panic with exponential backoff; `WithRetryCallback` for metrics |
| `RetryOnError` | Retry on error return (`ErrorJob` interface); `WithRetryCallback` for metrics |
| `CircuitBreaker` | Stop execution after consecutive failures; `WithStateChangeCallback` + `CircuitBreakerWithHandle` for monitoring |

Concurrency and resilience wrappers (`Recover`, `SkipIfStillRunning`, `DelayIfStillRunning`, `Timeout`, `TimeoutWithContext`, `Jitter`, `JitterWithLogger`) implement `JobWithContext` and propagate the incoming context to inner jobs. Retry and circuit breaker wrappers (`RetryWithBackoff`, `RetryOnError`, `CircuitBreaker`) do not currently forward context.

## Validation

Validate cron expressions before scheduling:

```go
// Package-level validation (no Cron instance needed)
if err := cron.ValidateSpec("0 9 * * MON-FRI"); err != nil {
    log.Fatal(err)
}

// Instance-level validation (uses configured parser)
c := cron.New(cron.WithSeconds())
if err := c.ValidateSpec("0 30 * * * *"); err != nil {
    log.Fatal(err)
}

// Detailed analysis
result := cron.AnalyzeSpec("0 9 * * MON-FRI")
fmt.Println("Next run:", result.NextRun)
fmt.Println("Fields:", result.Fields)
```

## Observability

Monitor cron operations with hooks:

```go
c := cron.New(cron.WithObservability(cron.ObservabilityHooks{
    OnJobStart: func(id cron.EntryID, name string, scheduled time.Time) {
        jobsStarted.WithLabelValues(name).Inc()
    },
    OnJobComplete: func(id cron.EntryID, name string, dur time.Duration, recovered any) {
        jobDuration.WithLabelValues(name).Observe(dur.Seconds())
    },
}))
```

Monitor circuit breaker state and retry attempts:

```go
wrapper, handle := cron.CircuitBreakerWithHandle(logger, 5, 5*time.Minute,
    cron.WithStateChangeCallback(func(e cron.CircuitBreakerEvent) {
        circuitState.WithLabelValues(e.NewState.String()).Set(1)
    }),
)
// handle.State(), handle.Failures(), handle.CooldownEnds() for health checks

cron.RetryWithBackoff(logger, 3, time.Second, time.Minute, 2.0,
    cron.WithRetryCallback(func(a cron.RetryAttempt) {
        retryCounter.WithLabelValues(fmt.Sprint(a.Attempt)).Inc()
    }),
)
```

Query job status at runtime:

```go
if c.IsJobRunningByName("my-job") {
    fmt.Println("Job is currently running")
}
```

## Testing with FakeClock

Deterministic testing without real time waits:

```go
fakeClock := cron.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
c := cron.New(cron.WithClock(fakeClock))
c.AddFunc("0 * * * *", myJob)
c.Start()

fakeClock.BlockUntil(1)        // Wait for scheduler to register timer
fakeClock.Advance(time.Hour)   // Trigger the job deterministically
```

## Logging

Compatible with [go-logr/logr](https://github.com/go-logr/logr) and `log/slog`:

```go
// Printf-style
c := cron.New(cron.WithLogger(
    cron.VerbosePrintfLogger(log.New(os.Stdout, "cron: ", log.LstdFlags)),
))

// slog
c := cron.New(cron.WithLogger(cron.NewSlogLogger(slog.Default())))
```

## Graceful Shutdown

```go
// Block until all jobs finish
c.StopAndWait()

// With timeout
if !c.StopWithTimeout(30 * time.Second) {
    log.Println("Warning: some jobs did not complete within 30s")
}
```

## Missed Job Catch-Up

Handle jobs that were missed while the scheduler was not running (e.g., application restart):

```go
// Load last run time from your database
lastRun := loadFromDatabase("daily-report")

c.AddFunc("0 9 * * *", dailyReport,
    cron.WithPrev(lastRun),                      // When it last ran
    cron.WithMissedPolicy(cron.MissedRunOnce),   // Run once if missed
    cron.WithMissedGracePeriod(2*time.Hour),     // Only if within 2 hours
)
```

**Policies:**
- `MissedSkip` (default) — No catch-up, wait for next scheduled time
- `MissedRunOnce` — Run once immediately for the most recent missed execution
- `MissedRunAll` — Run for every missed execution (capped at 100 for safety)

> [!IMPORTANT]
> The scheduler does NOT persist state. You must provide the last run time via `WithPrev()`
> and store it yourself (database, file, etc.). See [docs/PERSISTENCE_GUIDE.md](docs/PERSISTENCE_GUIDE.md)
> for complete integration patterns.

## Documentation

- [API Reference](docs/API_REFERENCE.md) — Complete type and method documentation
- [Cookbook](docs/COOKBOOK.md) — Recipes for common patterns
- [Migration Guide](docs/MIGRATION.md) — Migrating from robfig/cron
- [DST Handling](docs/DST_HANDLING.md) — Daylight Saving Time behavior
- [Persistence Guide](docs/PERSISTENCE_GUIDE.md) — Storing and restoring job state
- [Changelog](CHANGELOG.md) — Release history
- [pkg.go.dev](https://pkg.go.dev/github.com/netresearch/go-cron) — Go reference documentation

## Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) before submitting PRs.

## Security

For security issues, please see [SECURITY.md](SECURITY.md).

## License

MIT License — see [LICENSE](LICENSE) for details.

---

*This fork is maintained by [Netresearch](https://github.com/netresearch). The original cron library was created by [Rob Figueiredo](https://github.com/robfig).*
