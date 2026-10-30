# Daylight Saving Time (DST) Handling

This document explains how go-cron handles Daylight Saving Time transitions and timezone-related scheduling complexities.

## Overview

DST transitions create two problematic scenarios for cron schedulers:

1. **Spring Forward** (clock jumps ahead): An hour is skipped, making some scheduled times non-existent
2. **Fall Back** (clock repeats): An hour occurs twice, creating ambiguity

This library implements ISC cron-compatible behavior to handle these cases predictably.

## Spring Forward (Clock Jumps Ahead)

When DST causes clocks to spring forward (e.g., 2:00 AM becomes 3:00 AM), a job scheduled during the skipped time runs once on that day: at its scheduled time shifted by the length of the gap.

### Example: US Eastern Time

```
DST transition: March 10, 2024
Clock jumps from 1:59:59 AM to 3:00:00 AM
The hour 2:00-2:59 AM does not exist
```

| Schedule | Behavior |
|----------|----------|
| `0 30 2 * * *` (2:30 AM) | Runs at 3:30 AM (shifted by the one-hour gap) |
| `0 */15 2 * * *` (2:00, 2:15, 2:30, 2:45 AM) | Runs once, at 3:00 AM (the first skipped time, shifted) |
| `0 0 3 * * *` (3:00 AM) | Runs at 3:00 AM (as scheduled) |
| `0 0 1 * * *` (1:00 AM) | Runs at 1:00 AM (unaffected) |

Gaps of other lengths and at other times follow the same rule:

| Zone and gap | Schedule | Run on the transition day |
|--------------|----------|---------------------------|
| `Australia/Lord_Howe`, 02:00 → 02:30 | `15 2 * * *` | 02:45 |
| `Australia/Lord_Howe`, 02:00 → 02:30 | `5,15 2 * * *` | 02:35 (once) |
| `America/Santiago`, 00:00 → 01:00 | `30 0 * * *` | 01:30 |
| `America/Los_Angeles`, 02:00 → 03:00 | `30 2 * * *` | 03:30 |

A time that the gap skips runs once per gap: the earliest skipped time that the
schedule matches, shifted by the length of the gap. The other skipped times of
the same gap merge into that run. When the shifted run falls on a time that the
schedule also matches after the gap, the job runs once at that time.

`Prev()` reports the same gap run that `Next()` produces, so walking a schedule backwards over the transition day yields the same runs as walking it forwards. `Next()` reports the gap run from any time before it, including the last second before the gap and the time between the gap and the run.

### Implementation

`Next` and `Prev` in `spec.go` first find the next or previous wall time that exists. They then walk the zone transitions between the start time and that result with `time.Time.ZoneBounds`. A transition that moves the clock forward is a gap; `gapRun` returns its run, and `Next` or `Prev` returns it when it comes first.

### Why ISC Behavior?

The original `robfig/cron` silently skipped jobs scheduled during spring-forward transitions. This fork implements ISC cron behavior (used by most Unix systems) where skipped-hour jobs run immediately, ensuring no scheduled work is lost.

To keep the robfig/cron behavior, add the `DSTSkip` parse option (or
`RobfigCompat`, which includes it). `Next` and `Prev` then skip runs in the gap.

## Fall Back (Clock Repeats Hour)

When DST causes clocks to fall back (e.g., 2:00 AM becomes 1:00 AM), an hour occurs twice. The scheduler decides per spec how often it runs in the second occurrence ([ADR-027](adr/ADR-027-fall-back-by-rhythm.md)): a job that runs every hour or more often keeps its rhythm, and a job at fixed times runs each time once that day.

### Example: US Eastern Time

```
DST transition: November 3, 2024
Clock falls back from 1:59:59 AM to 1:00:00 AM
The hour 1:00-1:59 AM occurs twice
```

| Schedule | Behavior |
|----------|----------|
| `0 30 1 * * *` (1:30 AM) | Runs once, during first occurrence (EDT) |
| `0 0 2 * * *` (2:00 AM) | Runs once at 2:00 AM EST |
| `0 0,30 1 * * *` (1:00 and 1:30 AM) | 1:00 and 1:30 EDT, once each |
| `0 */20 1 * * *` (1:00, 1:20, 1:40 AM) | 1:00, 1:20 and 1:40 EDT, once each |
| `0 */5 0-6 * * *` (every 5 minutes at night) | Every 5 minutes through both copies of the hour |
| `0 0 * * * *` (every hour) | 1:00 EDT and 1:00 EST, one hour apart; 25 runs that day |
| `0 30 1-3 * * *` (every hour at :30, 1:30 to 3:30) | 1:30 EDT, 1:30 EST, 2:30 EST, 3:30 EST |
| `0 0 */2 * * *` in Europe/Berlin (02:00 repeats) | 00:00 and 02:00 CEST, 04:00 CET: 02:00 CET would come one hour after 02:00 CEST |

### Important Notes

- The rule is about how often a job runs, see
  [ADR-027](adr/ADR-027-fall-back-by-rhythm.md). For a time in the second copy
  of the clock, the scheduler checks whether the spec also matches the
  wall-clock time one repeat length earlier or later on a calendar without DST
  (`recursAcrossRepeat()` in `cron.go`). The repeat length is one hour in most
  zones, 30 minutes on Lord Howe Island and two hours on Antarctica/Troll.
  - If the spec matches, it runs in every such stretch of the day, so the
    second copy holds a regular run: it runs. This covers every-few-minutes and
    hourly jobs, `0-23` as well as `*` in the hour field.
  - If not, the spec runs at fixed times, and each runs once that day. The time
    is skipped when the entry fired it in the first copy or when it repeats the
    wall-clock time of the run just fired (`isDSTFallBackDuplicate()`).
- The scheduler's guard (`skipFallBackRepeats()`, `isFallBackRepeat()`) runs
  whenever the scheduler computes the next time of an entry: after a run, at
  start, when an entry is added or updated, while it is paused, after the clock
  moved back, and for the catch-up list of `WithMissedPolicy`.
- Each entry records the scheduled times it fired in the first copy. A fixed
  time it did not fire there (the entry was added later, paused, or the process
  was asleep) runs in the second copy. The record stays across `UpdateEntry` and
  `UpsertJob`, so a config reload in the repeated hour does not run a time
  twice. Catch-up runs count as runs of their time. The record lives in memory
  only: after a process restart inside the repeated hour, the fixed times of the
  first copy run again in the second copy.
- A spec that matches every hour only in a window runs hourly through both
  copies: `0 1,2 * * *` runs 01:00 EDT, 01:00 EST and 02:00 EST.
- On Lord Howe Island the clock repeats 30 minutes. A rhythm that does not
  repeat every 30 minutes, such as `*/20 * * * *`, runs 01:40 once there.
- Intervals (`@every`, `@every_start`, `@every_aligned`) are not deduplicated: they
  count elapsed time, not wall-clock time. `@every 1h` runs at 01:30 EDT and again
  one hour later at 01:30 EST (see ADR-026)
- For fixed times this aligns with [RFC 5545 Section 3.3.5](https://datatracker.ietf.org/doc/html/rfc5545#section-3.3.5):
  *"If the local time described occurs more than once [...] the DATE-TIME value
  refers to the first occurrence"*

## Midnight DST Transitions

Some regions (e.g., Brazil, parts of Australia) have DST transitions at midnight, causing midnight to not exist on certain days.

### Example: Sao Paulo, Brazil

```
DST transition: November 3, 2018
Clock jumps from 11:59:59 PM to 1:00:00 AM
Midnight (00:00) does not exist on November 4th
```

When `Next` moves to a new day, month or year, it starts that date at its first
instant (`dateStart` in `spec.go`):

- Where midnight is skipped, the date starts at the zone change, 01:00 on
  November 4th in the example above.
- Where midnight is repeated, the date starts at the first copy, so `Next`
  returns the first copy of 00:30 for `30 0 1 10 *` as it does for
  `30 0 * * *`.
- Where the clock goes back across midnight to the previous date (Pacific/Guam
  on 1969-01-26), a copy of midnight that lies before the search start is skipped.

`time.Date` alone can return 23:00 of the day before for a skipped midnight and
either copy of a repeated one; a month step from such an instant kept that time
of day and could skip a month (#457).

## Timezone Configuration

### Setting Cron Timezone

```go
// Option 1: Configure at Cron level
nyc, _ := time.LoadLocation("America/New_York")
c := cron.New(cron.WithLocation(nyc))

// Option 2: Per-schedule timezone prefix
c.AddFunc("TZ=America/New_York 0 9 * * *", job)
c.AddFunc("CRON_TZ=Europe/London 0 9 * * *", job)

// Quoted values also accepted (common shell convention)
c.AddFunc(`TZ="America/New_York" 0 9 * * *`, job)
c.AddFunc(`CRON_TZ='Europe/London' 0 9 * * *`, job)
```

### Location Priority

1. `TZ=` or `CRON_TZ=` prefix in spec string (highest priority)
2. `WithLocation()` option on Cron instance
3. `time.Local` (system default)

### Interaction with SpecSchedule.Location

When a schedule is parsed:
- `SpecSchedule.Location` stores the effective timezone
- `Next()` calculations use this location
- Final times are converted back to the caller's timezone

## Testing DST Scenarios

### Spring Forward Test

```go
func TestSpringForward(t *testing.T) {
    nyc, _ := time.LoadLocation("America/New_York")

    // March 10, 2024: 2:00 AM -> 3:00 AM
    beforeDST := time.Date(2024, 3, 10, 1, 30, 0, 0, nyc)

    sched, _ := cron.ParseStandard("30 2 * * *") // 2:30 AM
    next := sched.Next(beforeDST)

    // Should be 3:30 AM (same minute, in the hour after the skipped hour)
    expected := time.Date(2024, 3, 10, 3, 30, 0, 0, nyc)
    if !next.Equal(expected) {
        t.Errorf("expected %v, got %v", expected, next)
    }
}
```

### Fall Back Test

```go
func TestFallBack(t *testing.T) {
    nyc, _ := time.LoadLocation("America/New_York")

    // November 3, 2024: 2:00 AM -> 1:00 AM
    beforeDST := time.Date(2024, 11, 3, 0, 30, 0, 0, nyc)

    sched, _ := cron.ParseStandard("30 1 * * *") // 1:30 AM
    next := sched.Next(beforeDST)

    // Should be 1:30 AM first occurrence
    // Note: exact behavior depends on Go's time handling
}
```

### Midnight Transition Test

```go
func TestMidnightDST(t *testing.T) {
    sp, _ := time.LoadLocation("America/Sao_Paulo")

    // Test that midnight jobs still run when midnight doesn't exist
    sched, _ := cron.ParseStandard("TZ=America/Sao_Paulo 0 0 * * *")

    // Use a date near Sao Paulo's DST transition
    // The Next() function should handle non-existent midnight
}
```

## Best Practices

### 1. Avoid DST-Sensitive Times

When possible, schedule jobs outside typical DST transition hours (usually 1-3 AM):

```go
// Prefer
c.AddFunc("0 0 4 * * *", job)  // 4:00 AM - rarely affected by DST

// Avoid
c.AddFunc("0 30 2 * * *", job) // 2:30 AM - often affected by DST
```

### 2. Use UTC for Critical Jobs

For jobs that must run exactly once at precise intervals:

```go
c := cron.New(cron.WithLocation(time.UTC))
c.AddFunc("0 0 * * * *", criticalJob) // Every hour UTC
```

### 3. Handle Transition Days Explicitly

For business-critical scheduling, consider checking if today is a DST transition day:

```go
func isDSTTransitionDay(loc *time.Location, date time.Time) bool {
    // Check if UTC offset changes during this day
    start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
    end := start.Add(24 * time.Hour)
    _, startOffset := start.Zone()
    _, endOffset := end.Zone()
    return startOffset != endOffset
}
```

### 4. Log Timezone Information

Include timezone in job logging for debugging:

```go
c.AddFunc("0 0 2 * * *", func() {
    log.Printf("Job executed at %v (zone: %s)",
        time.Now(), time.Now().Location())
})
```

## Known Limitations

1. **Historical DST changes**: The library relies on Go's timezone database, which may not have complete historical DST data
2. **Leap seconds**: Not handled (Go's time package ignores leap seconds)

## References

- [Vixie cron source (DST logic)](https://github.com/vixie/cron/blob/master/cron.c)
- [OpenBSD cron(8)](https://man.openbsd.org/cron) — *"care is taken to avoid running jobs twice"*
- [FreeBSD cron(8)](https://man.freebsd.org/cgi/man.cgi?query=cron) — *"executed exactly once"*
- [RFC 5545 Section 3.3.5](https://datatracker.ietf.org/doc/html/rfc5545#section-3.3.5) — *"refers to the first occurrence"*
- [POSIX crontab](https://pubs.opengroup.org/onlinepubs/9699919799/utilities/crontab.html) — no DST specification (implementation-defined)
- [Go time package documentation](https://pkg.go.dev/time)
- [IANA Time Zone Database](https://www.iana.org/time-zones)
- [Original robfig/cron DST issue](https://github.com/robfig/cron/issues/157)
