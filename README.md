[![Go Reference](https://pkg.go.dev/badge/github.com/netresearch/go-cron.svg)](https://pkg.go.dev/github.com/netresearch/go-cron)

# go-cron

A maintained fork of [robfig/cron](https://github.com/robfig/cron) with bug fixes and improvements.

## Installation

```bash
go get github.com/netresearch/go-cron
```

Import it in your program as:
```go
import "github.com/netresearch/go-cron"
```

Requires Go 1.25 or later.

## Why This Fork?

The original robfig/cron has been unmaintained since 2020, with 50+ open PRs and several
critical panic bugs. This fork addresses:

- **Panic fixes**: Fixed TZ= parsing panics (issues #554, #555)
- **Chain behavior**: Added `Entry.Run()` method to properly invoke chain decorators (issue #551)
- **DST handling**: Jobs scheduled during DST "spring forward" now run immediately (ISC cron behavior, PR #541)
- **Go 1.25**: Updated to latest Go version with modern toolchain

## Migrating from robfig/cron

Simply update your import path:
```go
// Before
import "github.com/robfig/cron/v3"

// After
import cron "github.com/netresearch/go-cron"
```

The API is fully compatible with robfig/cron v3.

## Documentation

Refer to the [package documentation](https://pkg.go.dev/github.com/netresearch/go-cron).

## Features (inherited from robfig/cron v3)

This fork maintains full compatibility with robfig/cron v3 while adding fixes.

Original v3 features:

- Standard cron spec parsing by default (first field is "minute"), with an easy
  way to opt into the seconds field (quartz-compatible). Although, note that the
  year field (optional in Quartz) is not supported.

- Extensible, key/value logging via an interface that complies with
  the https://github.com/go-logr/logr project.

- The new Chain & JobWrapper types allow you to install "interceptors" to add
  cross-cutting behavior like the following:
  - Recover any panics from jobs
  - Delay a job's execution if the previous run hasn't completed yet
  - Skip a job's execution if the previous run hasn't completed yet
  - Log each job's invocations
  - Notification when jobs are completed

## Usage Examples

```go
// Seconds field, required
cron.New(cron.WithSeconds())

// Seconds field, optional
cron.New(cron.WithParser(cron.NewParser(
	cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)))

// With panic recovery
cron.New(cron.WithChain(
  cron.Recover(logger),
))

// With verbose logging
cron.New(
  cron.WithLogger(cron.VerbosePrintfLogger(logger)))
```

## Timezone Support

CRON_TZ is the recommended way to specify the timezone of a single schedule:
```
CRON_TZ=America/New_York 0 0 * * *
```

The legacy "TZ=" prefix is also supported.

### Background - Cron spec format

There are two cron spec formats in common usage:

- The "standard" cron format, described on [the Cron wikipedia page] and used by
  the cron Linux system utility.

- The cron format used by [the Quartz Scheduler], commonly used for scheduled
  jobs in Java software

[the Cron wikipedia page]: https://en.wikipedia.org/wiki/Cron
[the Quartz Scheduler]: http://www.quartz-scheduler.org/documentation/quartz-2.3.0/tutorials/tutorial-lesson-06.html

The original version of this package included an optional "seconds" field, which
made it incompatible with both of these formats. Now, the "standard" format is
the default format accepted, and the Quartz format is opt-in.
