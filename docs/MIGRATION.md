# Migrating from robfig/cron

This guide helps you migrate from [robfig/cron](https://github.com/robfig/cron) v3 to [netresearch/go-cron](https://github.com/netresearch/go-cron).

> [!WARNING]
> **This fork includes knowingly accepted behavior changes.** While the API is 100%
> compatible, runtime behavior differs in several areas to fix bugs and inconsistencies
> in the unmaintained upstream. Review the [Behavioral Differences](#behavioral-differences)
> section before upgrading production systems.

## Quick Start

For most users, migration requires only two changes:

### 1. Update import path

```go
// Before
import "github.com/robfig/cron/v3"

// After
import cron "github.com/netresearch/go-cron"
```

### 2. Update go.mod

```bash
go get github.com/netresearch/go-cron@latest
```

The API is 100% compatible with robfig/cron v3 — no code changes required for typical use cases.

## Go Version Requirements

| Library | Minimum Go Version |
|---------|-------------------|
| robfig/cron v3 | Go 1.13 |
| netresearch/go-cron | Go 1.25 |

## Behavioral Differences

While the API is compatible, there are knowingly accepted behavior changes that fix bugs or improve reliability. These changes may affect your application if you were (knowingly or unknowingly) depending on the original behavior.

### Bug Fixes That Change Behavior

| Issue | robfig/cron v3 | netresearch/go-cron |
|-------|----------------|---------------------|
| **TZ= parsing panics** | Crashes on empty or malformed timezone | Returns descriptive error |
| **Entry.Run() bypasses chains** | `entry.Run()` calls job directly | `entry.Run()` honors chain wrappers |
| **DST spring-forward** | Jobs silently skipped | Jobs run immediately after transition (ISC behavior) |
| **DOM/DOW logic** | OR when both restricted | AND (logical, consistent with other fields) |
| **NewParser with no fields** | Panics | Returns error |

#### TZ= Panic Fixes (#554, #555)

**Before (robfig/cron):**
```go
// These would panic:
c.AddFunc("TZ= 0 6 * * *", myFunc)        // Empty timezone
c.AddFunc("CRON_TZ=Invalid/Zone", myFunc) // Invalid timezone
c.AddFunc("TZ=America/New_York", myFunc)  // Timezone only, no schedule
```

**After (netresearch/go-cron):**
```go
// These return errors instead of panicking:
_, err := c.AddFunc("TZ= 0 6 * * *", myFunc)
// err: "empty time zone specification"

_, err := c.AddFunc("CRON_TZ=Invalid/Zone 0 6 * * *", myFunc)
// err: "unknown time zone Invalid/Zone"

_, err := c.AddFunc("TZ=America/New_York", myFunc)
// err: "empty schedule specification"
```

#### Entry.Run() Chain Behavior (#551)

**Before (robfig/cron):**
```go
c := cron.New(cron.WithChain(
    cron.SkipIfStillRunning(logger),
    cron.Recover(logger),
))
id, _ := c.AddFunc("* * * * *", myFunc)

entry := c.Entry(id)
entry.Job.Run() // Bypasses chain — no skip check, no panic recovery!
```

**After (netresearch/go-cron):**
```go
entry := c.Entry(id)
entry.Run() // Honors chain wrappers — skip check and panic recovery applied
```

Use `entry.Run()` instead of `entry.Job.Run()` to ensure chain decorators are respected.

#### DST Spring-Forward Handling (#541)

**Before (robfig/cron):**
```
Schedule: "0 30 2 * * *" (2:30 AM)
DST transition: 2:00 AM → 3:00 AM
Result: Job SKIPPED (time 2:30 never exists)
```

**After (netresearch/go-cron):**
```
Schedule: "0 30 2 * * *" (2:30 AM)
DST transition: 2:00 AM → 3:00 AM
Result: Job runs at 3:00 AM (immediately after transition)
```

This follows ISC cron behavior used by most Unix systems.

#### DOM/DOW AND Logic (#277)

**Before (robfig/cron):**
```
Expression: "0 0 15 * FRI"
Behavior: Runs on the 15th OR any Friday (OR logic)
Result: ~4-5 runs per month
```

**After (netresearch/go-cron):**
```
Expression: "0 0 15 * FRI"
Behavior: Runs on the 15th that is ALSO a Friday (AND logic)
Result: Runs only when the 15th is a Friday (~once per 7 months)
```

The AND logic is consistent with how all other cron fields work and enables useful patterns:

```go
// Last Friday of month (days 25-31 AND Friday)
c.AddFunc("0 0 25-31 * FRI", lastFridayJob)

// First Monday of month (days 1-7 AND Monday)
c.AddFunc("0 0 1-7 * MON", firstMondayJob)

// Friday the 13th
c.AddFunc("0 0 13 * FRI", unluckyJob)
```

**Migration option:** For legacy OR behavior, use the `DowOrDom` parser option:

```go
parser := cron.NewParser(
    cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.DowOrDom,
)
c := cron.New(cron.WithParser(parser))
// Now "0 0 15 * FRI" means "15th OR any Friday" (legacy behavior)
```

### Validation Improvements

These changes make the library stricter about invalid input:

#### Step Range Validation (#543)

**Before (robfig/cron):**
```go
// Accepted but semantically incorrect:
c.AddFunc("*/60 * * * *", myFunc) // Step 60 in 0-59 range
```

**After (netresearch/go-cron):**
```go
_, err := c.AddFunc("*/60 * * * *", myFunc)
// err: "step (60) must be less than range size (60)"
```

#### Minimum @every Duration

**Before (robfig/cron):**
```go
c.AddFunc("@every 100ms", myFunc) // Allowed, but could overwhelm system
```

**After (netresearch/go-cron):**
```go
_, err := c.AddFunc("@every 100ms", myFunc)
// err: "@every duration must be at least 1s: @every 100ms"

// To allow sub-second intervals (e.g., for testing):
c := cron.New(cron.WithMinEveryInterval(0))
c.AddFunc("@every 100ms", myFunc) // Now allowed
```

#### Input Length Limits

**Before (robfig/cron):**
```go
// No limit on spec length — potential DoS vector
c.AddFunc(veryLongString, myFunc)
```

**After (netresearch/go-cron):**
```go
// Specs limited to 1024 characters
_, err := c.AddFunc(veryLongString, myFunc)
// err: "spec exceeds maximum length of 1024 characters"
```

#### RetryWithBackoff Semantics (v0.6.0)

**Before (robfig/cron):**
```go
// maxRetries=0 meant unlimited retries (DoS risk)
c := cron.New(cron.WithChain(
    cron.RetryWithBackoff(logger, 0, time.Second, time.Minute, 2.0),
))
// A failing job would retry forever
```

**After (netresearch/go-cron v0.6.0+):**
```go
// maxRetries=0 now means no retries (safe default)
c := cron.New(cron.WithChain(
    cron.RetryWithBackoff(logger, 0, time.Second, time.Minute, 2.0),
))
// A failing job fails immediately after first attempt

// For unlimited retries, use -1 (explicit opt-in)
c := cron.New(cron.WithChain(
    cron.RetryWithBackoff(logger, -1, time.Second, time.Minute, 2.0),
))
// Now retries forever (with backoff)
```

**Impact:** If you relied on zero-value configs (`maxRetries=0`) for unlimited retries, update to `-1`.
This is a security improvement: zero-value is now fail-safe instead of a potential DoS vector.

## Type Changes

### EntryID: int → uint64

The `EntryID` type changed from `int` to `uint64` for larger capacity and overflow safety.

**Before (robfig/cron):**
```go
var id int = int(c.Schedule(schedule, job))
```

**After (netresearch/go-cron):**
```go
// Option 1: Use the type directly (recommended)
var id cron.EntryID = c.Schedule(schedule, job)

// Option 2: Use uint64
var id uint64 = uint64(c.Schedule(schedule, job))
```

**Impact:** If you're storing EntryID in an `int` variable, update to `cron.EntryID` or `uint64`.

## New Features

These features are additions that don't affect existing code:

### Deterministic Testing with FakeClock

```go
import "github.com/netresearch/go-cron"

// Create a fake clock for testing
clock := cron.NewFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
c := cron.New(cron.WithClock(clock))

c.AddFunc("0 * * * *", myFunc)
c.Start()

// Advance time in tests
clock.Advance(time.Hour)
// Job executes at 1:00
```

### StopAndWait() Convenience Method

```go
// Before: Manual wait pattern
ctx := c.Stop()
<-ctx.Done()

// After: Convenience method
c.StopAndWait()
```

### Timeout Wrapper

```go
c := cron.New(cron.WithChain(
    cron.Timeout(logger, 30*time.Second),
    cron.Recover(logger),
))
```

**Note:** The Timeout wrapper uses an "abandonment model" — the wrapper returns after timeout, but the job goroutine continues running. See [doc.go](https://pkg.go.dev/github.com/netresearch/go-cron#hdr-Timeout_Wrapper_Caveats) for details.

### Heap-Based Scheduling (Performance)

The scheduler now uses a min-heap instead of sorted slice:

| Operation | robfig/cron | netresearch/go-cron |
|-----------|-------------|---------------------|
| Insert entry | O(n log n) | O(log n) |
| Remove entry | O(n) | O(log n) |
| Get next entry | O(1) | O(1) |

No code changes required — this is an internal optimization.

### slog Adapter

```go
import "log/slog"

c := cron.New(cron.WithLogger(
    cron.SlogLogger(slog.Default()),
))
```

## Migration Checklist

- [ ] Update import path to `github.com/netresearch/go-cron`
- [ ] Update go.mod with `go get github.com/netresearch/go-cron@latest`
- [ ] Verify Go version is 1.25+
- [ ] Review timezone handling for empty/invalid timezone cases
- [ ] Update `entry.Job.Run()` calls to `entry.Run()` if chain behavior is expected
- [ ] Review cron expressions for step validation (`*/60` style patterns)
- [ ] Update all code storing `EntryID` to use the new `cron.EntryID` type (changed from `int` to `uint64`)
- [ ] Audit and update any type assertions or conversions involving `EntryID` to prevent runtime panics
- [ ] Verify database columns storing `EntryID` are migrated to a type compatible with `uint64`
- [ ] Audit `RetryWithBackoff` usage: change `maxRetries=0` to `-1` if unlimited retries are intended
- [ ] Test DST transitions if your application runs DST-sensitive schedules
- [ ] Run existing tests to verify compatibility

## Testing Your Migration

### 1. Run Existing Tests

```bash
go test ./...
```

### 2. Verify Timezone Handling

If you use timezone features, test edge cases:

```go
func TestTimezoneEdgeCases(t *testing.T) {
    c := cron.New()

    // These should return errors, not panic
    _, err := c.AddFunc("TZ= 0 6 * * *", func() {})
    if err == nil {
        t.Error("expected error for empty timezone")
    }

    _, err = c.AddFunc("CRON_TZ=Invalid/Zone 0 6 * * *", func() {})
    if err == nil {
        t.Error("expected error for invalid timezone")
    }
}
```

### 3. Test DST Transitions

If DST behavior matters to your application:

```go
func TestDSTBehavior(t *testing.T) {
    loc, _ := time.LoadLocation("America/New_York")

    // Create schedule for 2:30 AM
    schedule, _ := cron.ParseStandard("30 2 * * *")

    // Time just before spring-forward transition
    before := time.Date(2024, 3, 10, 1, 59, 0, 0, loc)

    next := schedule.Next(before)

    // Should be 3:00 AM (immediately after transition), not next day
    if next.Hour() != 3 || next.Day() != 10 {
        t.Errorf("expected 3:00 AM same day, got %v", next)
    }
}
```

### 4. Benchmark Performance (Optional)

If performance is critical:

```bash
go test -bench=. -benchmem
```

## Troubleshooting

### "unknown time zone" errors

Ensure timezone names are valid IANA timezone identifiers:

```go
// Wrong
c.AddFunc("TZ=EST 0 6 * * *", myFunc)

// Correct
c.AddFunc("TZ=America/New_York 0 6 * * *", myFunc)
```

### Step validation errors

Review expressions using `/` step syntax:

```go
// Invalid: step equals or exceeds range
"*/60 * * * *"  // Error: 60 >= 60
"0-10/15 * * * *" // Error: 15 > 10

// Valid alternatives
"* * * * *"     // Every minute
"0-10/5 * * * *" // Every 5 minutes within 0-10
```

### Jobs running at unexpected times during DST

The new ISC-compatible behavior runs skipped-hour jobs immediately. If you prefer the old behavior (skip the job), schedule outside DST transition hours:

```go
// Avoid 1-3 AM for DST-sensitive jobs
c.AddFunc("0 4 * * *", myFunc) // 4:00 AM — safe

// Or use UTC
c.AddFunc("CRON_TZ=UTC 0 6 * * *", myFunc)
```

## Getting Help

- [GitHub Issues](https://github.com/netresearch/go-cron/issues)
- [API Reference](https://pkg.go.dev/github.com/netresearch/go-cron)
- [CHANGELOG](../CHANGELOG.md)
