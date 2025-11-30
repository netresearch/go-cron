[![Go Reference](https://pkg.go.dev/badge/github.com/netresearch/go-cron.svg)](https://pkg.go.dev/github.com/netresearch/go-cron)
[![CI](https://github.com/netresearch/go-cron/actions/workflows/ci.yml/badge.svg)](https://github.com/netresearch/go-cron/actions/workflows/ci.yml)
[![CodeQL](https://github.com/netresearch/go-cron/actions/workflows/codeql.yml/badge.svg)](https://github.com/netresearch/go-cron/actions/workflows/codeql.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/netresearch/go-cron/badge)](https://scorecard.dev/viewer/?uri=github.com/netresearch/go-cron)
[![Go Report Card](https://goreportcard.com/badge/github.com/netresearch/go-cron)](https://goreportcard.com/report/github.com/netresearch/go-cron)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

# go-cron

A maintained fork of [robfig/cron](https://github.com/robfig/cron) — the most popular cron library for Go — with critical bug fixes, DST handling improvements, and modern toolchain support.

## Why?

The original `robfig/cron` has been unmaintained since 2020, accumulating 50+ open PRs and several critical panic bugs that affect production systems. Rather than waiting indefinitely, this fork provides:

| Issue | Original | This Fork |
|-------|----------|-----------|
| TZ= parsing panics | Crashes on malformed input | Fixed (#554, #555) |
| Chain decorators | `Entry.Run()` bypasses chains | Properly invokes wrappers (#551) |
| DST spring-forward | Jobs silently skipped | Runs immediately (ISC behavior, #541) |
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

The API is 100% compatible with robfig/cron v3.

> [!TIP]
> See [docs/MIGRATION.md](docs/MIGRATION.md) for a comprehensive migration guide including behavioral differences, type changes, and troubleshooting.

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
- **Recover** — Catch panics, log, and continue
- **SkipIfStillRunning** — Skip execution if previous run hasn't finished
- **DelayIfStillRunning** — Queue execution until previous run finishes

## Logging

Compatible with [go-logr/logr](https://github.com/go-logr/logr):

```go
// Verbose logging
c := cron.New(cron.WithLogger(
    cron.VerbosePrintfLogger(log.New(os.Stdout, "cron: ", log.LstdFlags)),
))
```

## API Reference

Full documentation: [pkg.go.dev/github.com/netresearch/go-cron](https://pkg.go.dev/github.com/netresearch/go-cron)

## Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) before submitting PRs.

## Security

For security issues, please see [SECURITY.md](SECURITY.md).

## License

MIT License — see [LICENSE](LICENSE) for details.

---

*This fork is maintained by [Netresearch](https://github.com/netresearch). The original cron library was created by [Rob Figueiredo](https://github.com/robfig).*
