# Daylight Saving Time (DST) Handling

This document explains how go-cron handles Daylight Saving Time transitions and timezone-related scheduling complexities.

## Overview

DST transitions create two problematic scenarios for cron schedulers:

1. **Spring Forward** (clock jumps ahead): An hour is skipped, making some scheduled times non-existent
2. **Fall Back** (clock repeats): An hour occurs twice, creating ambiguity

This library implements ISC cron-compatible behavior to handle these cases predictably.

## Spring Forward (Clock Jumps Ahead)

When DST causes clocks to spring forward (e.g., 2:00 AM becomes 3:00 AM), any jobs scheduled during the skipped hour will run immediately after the transition.

### Example: US Eastern Time

```
DST transition: March 10, 2024
Clock jumps from 1:59:59 AM to 3:00:00 AM
The hour 2:00-2:59 AM does not exist
```

| Schedule | Behavior |
|----------|----------|
| `0 30 2 * * *` (2:30 AM) | Runs at 3:00 AM (immediately after transition) |
| `0 0 3 * * *` (3:00 AM) | Runs at 3:00 AM (as scheduled) |
| `0 0 1 * * *` (1:00 AM) | Runs at 1:00 AM (unaffected) |

### Implementation

The `checkHourDSTSkip` function in `spec.go` detects when time advances by 2 hours (indicating a DST spring-forward) and allows jobs scheduled for the skipped hour to run immediately:

```go
// If prev.Hour() == 1 and curr.Hour() == 3, a job for 2:XX runs at 3:00
if curr.Hour()-prev.Hour() == 2 {
    // Job in skipped hour runs immediately
}
```

### Why ISC Behavior?

The original `robfig/cron` silently skipped jobs scheduled during spring-forward transitions. This fork implements ISC cron behavior (used by most Unix systems) where skipped-hour jobs run immediately, ensuring no scheduled work is lost.

## Fall Back (Clock Repeats Hour)

When DST causes clocks to fall back (e.g., 2:00 AM becomes 1:00 AM), an hour occurs twice. Go's `time` package resolves this ambiguity by using the **first occurrence** (the DST time, before the transition).

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

### Important Notes

- Jobs run **once** during fall-back transitions (not twice)
- The first occurrence (before transition) is used
- This matches Go's `time.Date()` behavior for ambiguous times

## Midnight DST Transitions

Some regions (e.g., Brazil, parts of Australia) have DST transitions at midnight, causing midnight to not exist on certain days.

### Example: Sao Paulo, Brazil

```
DST transition: November 3, 2018
Clock jumps from 11:59:59 PM to 1:00:00 AM
Midnight (00:00) does not exist on November 4th
```

The `normalizeDSTDay` function handles this by adjusting the computed time:

```go
// If midnight doesn't exist, adjust to the actual hour
func normalizeDSTDay(t time.Time) time.Time {
    if t.Hour() == 0 {
        return t
    }
    // Time was shifted - normalize back
    if t.Hour() > 12 {
        return t.Add(time.Duration(24-t.Hour()) * time.Hour)
    }
    return t.Add(time.Duration(-t.Hour()) * time.Hour)
}
```

## Timezone Configuration

### Setting Cron Timezone

```go
// Option 1: Configure at Cron level
nyc, _ := time.LoadLocation("America/New_York")
c := cron.New(cron.WithLocation(nyc))

// Option 2: Per-schedule timezone prefix
c.AddFunc("TZ=America/New_York 0 9 * * *", job)
c.AddFunc("CRON_TZ=Europe/London 0 9 * * *", job)
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

    // Should be 3:00 AM (first moment after skipped hour)
    expected := time.Date(2024, 3, 10, 3, 0, 0, 0, nyc)
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

1. **Fall-back ambiguity**: When clocks repeat, the first occurrence is always used
2. **Historical DST changes**: The library relies on Go's timezone database, which may not have complete historical DST data
3. **Leap seconds**: Not handled (Go's time package ignores leap seconds)

## References

- [ISC cron behavior specification](https://man.openbsd.org/cron.8)
- [Go time package documentation](https://pkg.go.dev/time)
- [IANA Time Zone Database](https://www.iana.org/time-zones)
- [Original robfig/cron DST issue](https://github.com/robfig/cron/issues/157)
