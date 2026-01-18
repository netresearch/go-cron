# Frequently Asked Questions

> Common questions about go-cron

## Table of Contents

- [General](#general)
- [Migration](#migration)
- [Behavior](#behavior)
- [Performance](#performance)
- [Troubleshooting](#troubleshooting)

---

## General

### Why does this fork exist?

The original [robfig/cron](https://github.com/robfig/cron) has been effectively unmaintained since 2020:

- **165+ open issues** without responses
- **50+ open pull requests** with bug fixes that were never merged
- **Critical panic bugs** affecting production systems
- **Stuck on Go 1.13** without modern toolchain support

Rather than waiting indefinitely for upstream maintenance, this fork provides active maintenance with bug fixes, security updates, and modern Go support.

### Is this a drop-in replacement for robfig/cron?

**Yes, for the API.** The function signatures and types are identical:

```go
// Just change this:
import "github.com/robfig/cron/v3"

// To this:
import cron "github.com/netresearch/go-cron"
```

**However**, some runtime behavior has intentionally changed to fix bugs. See [Migration](#migration) below.

### Who maintains this fork?

[Netresearch](https://github.com/netresearch), a software development company based in Germany. We use this library in production and are committed to long-term maintenance.

### What Go versions are supported?

Go 1.25 and later. We follow Go's [release policy](https://go.dev/doc/devel/release#policy) and support the two most recent major versions.

### Does this library have any dependencies?

**No.** Zero external dependencies — stdlib only. Check `go.mod`:

```
module github.com/netresearch/go-cron

go 1.25
```

This means:
- No transitive dependency vulnerabilities
- No version conflicts
- Minimal binary size
- Fast compilation

### How is this different from other cron forks?

| Fork | Status | Key Difference |
|------|--------|----------------|
| robfig/cron | Unmaintained | Original, but abandoned |
| flc1125/go-cron | Active | Adds distributed features, external deps |
| **netresearch/go-cron** | Active | Bug fixes, zero deps, production-hardened |

We focus on being a better robfig/cron, not a different library.

---

## Migration

### What behavior changes should I know about?

| Change | robfig/cron | This Fork | Impact |
|--------|-------------|-----------|--------|
| DOM/DOW matching | OR logic | AND logic | "Friday the 13th" patterns now work |
| DST spring-forward | Jobs skipped | Jobs run immediately | More reliable scheduling |
| `Entry.Run()` | Bypasses chain | Respects chain | Wrappers always apply |
| Invalid TZ= | Panics | Returns error | No production crashes |

### Will my existing schedules work the same?

**Mostly yes.** The cron expression parsing is identical. However:

1. **DOM/DOW schedules** behave differently:
   ```go
   // "Run on the 15th OR on Friday"
   // robfig/cron: runs on 15th AND all Fridays
   // this fork: runs only on days that are both the 15th AND Friday
   c.AddFunc("0 0 15 * FRI", job)
   ```

2. **DST schedules** may fire at slightly different times during transitions

### How do I keep robfig/cron's DOM/DOW OR behavior?

Use the `DowOrDom` parser option:

```go
parser := cron.NewParser(
    cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow |
    cron.Descriptor | cron.DowOrDom, // <-- enables OR logic
)
c := cron.New(cron.WithParser(parser))
```

### Should I test before upgrading production?

**Yes.** We recommend:

1. Run your test suite with the new import
2. Review any schedules using both DOM and DOW
3. Check DST-sensitive schedules (1-3 AM)
4. Use `FakeClock` for deterministic testing

See [MIGRATION.md](MIGRATION.md) for a complete migration guide.

---

## Behavior

### How does DOM/DOW AND logic work?

When both day-of-month and day-of-week are specified, **both must match**:

```go
// Runs when: day is 13 AND day is Friday
c.AddFunc("0 0 13 * FRI", fridayThe13th)

// Practical: Last Friday of month (days 25-31 AND Friday)
c.AddFunc("0 0 25-31 * FRI", lastFriday)

// Practical: First Monday of month (days 1-7 AND Monday)
c.AddFunc("0 0 1-7 * MON", firstMonday)
```

This is consistent with how all other cron fields work (minute AND hour AND month, etc.).

### How does DST handling work?

**Spring Forward (hour skipped):**
```
2:00 AM → 3:00 AM (2:00-2:59 doesn't exist)
```
Jobs scheduled during the skipped hour run immediately at 3:00 AM.

**Fall Back (hour repeats):**
```
2:00 AM occurs twice
```
Jobs run once, during the first occurrence.

See [DST_HANDLING.md](DST_HANDLING.md) for comprehensive documentation.

### What happens if my job panics?

Without `Recover()`: The goroutine crashes, but the scheduler continues.

With `Recover()`: The panic is caught, logged, and the job is marked as completed.

```go
c := cron.New(cron.WithChain(
    cron.Recover(logger), // Always recommended
))
```

### Can I run a job only once?

Yes, use `WithRunOnce()`:

```go
c.AddFunc("0 0 * * *", job, cron.WithRunOnce())
// Job runs at next midnight, then is automatically removed
```

### How do I stop a job from overlapping?

Use `SkipIfStillRunning` or `DelayIfStillRunning`:

```go
// Skip if previous instance is still running
c := cron.New(cron.WithChain(
    cron.SkipIfStillRunning(logger),
))

// Or queue until previous finishes
c := cron.New(cron.WithChain(
    cron.DelayIfStillRunning(logger),
))
```

---

## Performance

### How does scheduling scale?

go-cron uses a min-heap for O(log n) operations:

| Jobs | Time per tick |
|------|---------------|
| 10 | ~470 ns |
| 100 | ~3,600 ns |
| 1,000 | ~37,000 ns |

See [PERFORMANCE.md](PERFORMANCE.md) for full benchmarks.

### Is there allocation overhead?

`Next()` calculations are **zero-allocation**. Most scheduling operations allocate nothing during steady-state operation.

### Should I use descriptors or cron expressions?

**Descriptors are faster** (~34ns vs ~510ns parse time):

```go
// Faster
c.AddFunc("@hourly", job)

// Slower (but more flexible)
c.AddFunc("0 * * * *", job)
```

For hot paths, consider pre-parsing:

```go
schedule, _ := cron.ParseStandard("*/5 * * * *")
c.Schedule(schedule, job1)
c.Schedule(schedule, job2) // Reuse parsed schedule
```

---

## Troubleshooting

### My job isn't running at the expected time

1. **Check timezone:**
   ```go
   // Is your system in the expected timezone?
   fmt.Println(time.Now().Location())

   // Explicitly set timezone:
   c := cron.New(cron.WithLocation(time.UTC))
   // Or per-schedule:
   c.AddFunc("CRON_TZ=America/New_York 0 9 * * *", job)
   ```

2. **Check DOM/DOW logic:**
   ```go
   // This only runs on days that are BOTH the 15th AND Friday
   c.AddFunc("0 0 15 * FRI", job)

   // Did you mean "15th OR Friday"? Use DowOrDom option.
   ```

3. **Check DST transitions:**
   ```go
   // Jobs at 2:30 AM may be skipped during spring forward
   // Consider scheduling outside 1-3 AM or use UTC
   ```

### My job panics but I don't see errors

Add the `Recover` wrapper and a logger:

```go
logger := cron.VerbosePrintfLogger(log.New(os.Stdout, "cron: ", log.LstdFlags))
c := cron.New(
    cron.WithLogger(logger),
    cron.WithChain(cron.Recover(logger)),
)
```

### How do I debug schedule timing?

Use the `Entries()` method to inspect scheduled jobs:

```go
for _, entry := range c.Entries() {
    fmt.Printf("Job %d: next=%v, prev=%v\n",
        entry.ID, entry.Next, entry.Prev)
}
```

Or calculate next times manually:

```go
schedule, _ := cron.ParseStandard("0 0 * * *")
next := schedule.Next(time.Now())
fmt.Println("Next run:", next)
```

### How do I write deterministic tests?

Use `FakeClock`:

```go
func TestMyJob(t *testing.T) {
    fake := cron.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
    c := cron.New(cron.WithClock(fake))

    var ran bool
    c.AddFunc("@hourly", func() { ran = true })
    c.Start()

    // Advance time
    fake.Advance(time.Hour)
    time.Sleep(10 * time.Millisecond) // Let goroutine run

    if !ran {
        t.Error("expected job to run")
    }
}
```

See [TESTING_GUIDE.md](TESTING_GUIDE.md) for comprehensive testing patterns.

### Where do I report bugs?

[GitHub Issues](https://github.com/netresearch/go-cron/issues) — please include:
- Go version (`go version`)
- Library version
- Minimal reproduction code
- Expected vs actual behavior

For security issues, see [SECURITY.md](../SECURITY.md).

---

## See Also

- [README](../README.md) — Quick start and overview
- [MIGRATION.md](MIGRATION.md) — Detailed migration guide
- [COOKBOOK.md](COOKBOOK.md) — Production-ready recipes
- [PERFORMANCE.md](PERFORMANCE.md) — Benchmarks and optimization
- [DST_HANDLING.md](DST_HANDLING.md) — Daylight saving time behavior
- [TESTING_GUIDE.md](TESTING_GUIDE.md) — Testing patterns
