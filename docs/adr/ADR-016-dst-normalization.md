# ADR-016: DST Handling via Normalization

## Status
Accepted

## Date
2025-12-14

## Context

Daylight Saving Time (DST) creates two edge cases:

**Spring Forward (clock jumps ahead):**
- 2:00 AM → 3:00 AM (2:00-2:59 doesn't exist)
- Jobs scheduled for 2:30 AM have no valid time

**Fall Back (clock repeats):**
- 2:00 AM occurs twice (first in DST, then in standard time)
- Jobs scheduled for 2:30 AM could run twice

**Key question:** What should happen to jobs during these transitions?

## Decision

Implement ISC cron-compatible behavior using normalization functions:

### Spring Forward: Run Immediately

Jobs scheduled during the skipped hour run at the first moment after the transition:

```go
func normalizeDSTDay(t time.Time, loc *time.Location) time.Time {
    // If time is in DST gap, Go normalizes it forward
    // We detect this and use the normalized time
    normalized := time.Date(t.Year(), t.Month(), t.Day(),
        t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc)

    if normalized.Hour() != t.Hour() {
        // Time was in DST gap, use normalized result
        return normalized
    }
    return t
}
```

**Example:** Job at 2:30 AM on spring-forward day runs at 3:00 AM.

### Fall Back: Run Once (First Occurrence)

Jobs during the repeated hour run once, during the first occurrence:

```go
// The first 2:30 AM (DST) is used
// The second 2:30 AM (standard) is skipped
```

**Rationale:** ISC cron runs jobs once per scheduled time, not once per wall-clock occurrence.

## Consequences

### Positive

- **ISC cron compatible**: Matches expected Unix cron behavior
- **No skipped jobs**: Spring-forward jobs still run
- **No duplicate runs**: Fall-back jobs run exactly once
- **Predictable**: Users can rely on jobs running once per schedule

### Negative

- **Time shift**: Spring-forward jobs run "late" (at 3:00 instead of 2:30)
- **Complex logic**: DST detection requires time manipulation
- **Timezone dependent**: Only affects locations with DST

### Neutral

- **Documentation needed**: Users should understand DST behavior
- **Testing complexity**: Must test with real timezones

## Implementation Details

### Detecting DST Gap

Go's `time.Date()` automatically normalizes invalid times:

```go
loc, _ := time.LoadLocation("America/New_York")
// March 10, 2024: 2:00 AM → 3:00 AM

t := time.Date(2024, 3, 10, 2, 30, 0, 0, loc)
// t is actually 3:30 AM (normalized forward)
```

We use this normalization to detect and handle gaps.

### Preventing Duplicate Execution During Fall-Back

The `Next()` function correctly returns the second occurrence as a valid future
time (it IS chronologically after the first occurrence in UTC). Deduplication is
therefore handled in the **scheduler** rather than in the schedule calculation.

After dispatching a job, `postDispatchScheduled()` calls `isDSTFallBackDuplicate()`
to compare the just-fired time (`e.Prev`) with the newly computed next time
(`e.Next`). If both have the same wall-clock time in the schedule's effective
location (per-schedule `TZ=` override or the cron instance's default) but the
UTC offset decreased (indicating a fall-back transition), the scheduler
skips the duplicate by advancing to the next valid time:

```go
// isDSTFallBackDuplicate detects when the next scheduled time is the second
// occurrence of the same wall-clock time as the previous execution.
func isDSTFallBackDuplicate(prev, next time.Time, loc *time.Location) bool {
    if prev.IsZero() || next.IsZero() {
        return false
    }
    p := prev.In(loc)
    n := next.In(loc)
    y1, m1, d1 := p.Date()
    y2, m2, d2 := n.Date()
    h1, min1, s1 := p.Clock()
    h2, min2, s2 := n.Clock()
    if y1 == y2 && m1 == m2 && d1 == d2 && h1 == h2 && min1 == min2 && s1 == s2 {
        _, pOff := p.Zone()
        _, nOff := n.Zone()
        return nOff < pOff // offset decreased = fall-back transition
    }
    return false
}
```

**Design note:** An earlier draft proposed detecting second occurrences inside
`Next()` itself, but this was rejected because `Next()` is a pure function used
by callers outside the scheduler. Placing the guard in the dispatch path keeps
`Next()` correct (the second occurrence IS the next matching time) while
preventing duplicate execution at the scheduler level.

### Schedule Calculation

The `SpecSchedule.Next()` function:
1. Calculates next time based on cron fields
2. Normalizes for spring-forward via `checkHourDSTSkip()`
3. Returns the next matching time (may be in the second occurrence during fall-back)

The scheduler's `postDispatchScheduled()` then applies the fall-back guard.

## Alternatives Considered

### 1. Skip Spring-Forward Jobs

```go
if inDSTGap(t) {
    return next.Add(24 * time.Hour)  // Skip to next day
}
```

- **Rejected**: Jobs may not run for 24+ hours
- Violates user expectations

### 2. Run Fall-Back Jobs Twice

```go
// Run at both 2:30 AM DST and 2:30 AM standard
```

- **Rejected**: Duplicate execution is surprising
- Resource consumption doubles
- Stateful jobs may conflict

### 3. Use UTC Internally

```go
// Store all times in UTC, convert for display only
```

- **Rejected**: Users expect local time behavior
- "Run at 9 AM" should mean local 9 AM

### 4. Fail on DST Transitions

```go
if inDSTTransition(t) {
    return error
}
```

- **Rejected**: Too disruptive
- Users must handle errors
- Jobs still need to run

## Industry Context

**No standard exists.** POSIX (IEEE Std 1003.1) crontab specification contains zero
mentions of DST, timezone handling, or ambiguous times. Fall-back behavior is entirely
implementation-defined. The only IETF standard that addresses the ambiguity is
RFC 5545 (iCalendar), Section 3.3.5:

> "If the local time described occurs more than once (when changing from daylight
> to standard time), the DATE-TIME value refers to the first occurrence."

### Vixie cron (the reference implementation)

The [Vixie cron source](https://github.com/vixie/cron/blob/master/cron.c) explicitly
handles fall-back by distinguishing **wildcard jobs** (e.g., `* * * * *`) from
**fixed-time jobs** (e.g., `30 1 * * *`):

```c
case negative:  /* DST fall-back */
    /* Just run the wildcard jobs. The fixed-time jobs
     * probably have already run, and should not be repeated. */
    find_jobs(timeRunning, &database, TRUE, FALSE);
```

All BSD and Linux man pages agree:
- OpenBSD/NetBSD/Linux: *"if time has moved backward, care is taken to avoid running jobs twice"*
- FreeBSD: *"They are executed exactly once, they are not skipped nor executed twice"*

### Cross-ecosystem survey

| Implementation | Fall-back behavior |
|----------------|-------------------|
| Vixie/ISC cron (C) | Once (reference) |
| FreeBSD cron (C) | Once (per-entry `NOT_UNTIL` flag) |
| Quartz (Java) | Once (*"it only fires once"*) |
| go-cron (this project) | Once (`isDSTFallBackDuplicate`) |
| robfig/cron (Go) | Twice (unfixed since 2014) |
| APScheduler (Python) | Twice (documented, recommends UTC) |
| Celery Beat (Python) | Crashes (`AmbiguousTimeError`) |
| node-cron (Node.js) | Missed executions (fixed in 4.1.1) |
| systemd timers (Linux) | Skips entire repeated hour (open bug) |

Our implementation aligns with Vixie cron and Quartz — the two most widely deployed
schedulers. The wall-clock comparison in `isDSTFallBackDuplicate` naturally handles
the wildcard/fixed-time distinction: wildcard jobs have different minutes each tick
and are never flagged as duplicates, matching Vixie cron's behavior.

## References

- Vixie cron source: https://github.com/vixie/cron/blob/master/cron.c
- OpenBSD cron(8): https://man.openbsd.org/cron
- FreeBSD cron(8): https://man.freebsd.org/cgi/man.cgi?query=cron
- RFC 5545 (iCalendar) Section 3.3.5: https://datatracker.ietf.org/doc/html/rfc5545#section-3.3.5
- POSIX crontab (no DST mention): https://pubs.opengroup.org/onlinepubs/9699919799/utilities/crontab.html
- Go time package: https://pkg.go.dev/time
- DST_HANDLING.md: Comprehensive documentation
