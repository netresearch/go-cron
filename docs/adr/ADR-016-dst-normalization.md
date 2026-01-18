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

### Detecting DST Overlap

```go
// Check if we're in the second occurrence of a repeated hour
func isSecondOccurrence(t time.Time) bool {
    // Compare offset before and after
    _, offset1 := t.Zone()
    _, offset2 := t.Add(-time.Hour).Zone()
    return offset1 != offset2
}
```

### Schedule Calculation

The `SpecSchedule.Next()` function:
1. Calculates next time based on cron fields
2. Normalizes for DST if needed
3. Returns normalized time

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

## References

- ISC cron: https://man.freebsd.org/cgi/man.cgi?query=cron
- Go time package DST handling: https://pkg.go.dev/time
- DST_HANDLING.md: Comprehensive documentation
