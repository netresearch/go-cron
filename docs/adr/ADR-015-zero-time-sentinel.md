# ADR-015: Zero Time as Schedule Exhaustion Sentinel

## Status
Accepted

## Date
2025-12-14

## Context

Schedules must indicate when no future execution times exist:
- One-shot schedules after their single run
- Schedules with end dates in the past
- Invalid schedule configurations

**Question:** How should `Schedule.Next(t)` signal "no more matches"?

Options:
1. Return zero time (`time.Time{}`)
2. Return error (`(time.Time, error)`)
3. Return pointer (`*time.Time`, nil = exhausted)
4. Panic

## Decision

Use zero time (`time.Time{}`) as the exhaustion sentinel:

```go
type Schedule interface {
    Next(time.Time) time.Time
}

// Implementation
func (s *SpecSchedule) Next(t time.Time) time.Time {
    // ... find next matching time ...
    if noMoreMatches {
        return time.Time{}  // Zero value
    }
    return nextTime
}

// Caller checks with IsZero()
next := schedule.Next(now)
if next.IsZero() {
    // Schedule exhausted
}
```

**Entry handling:**
```go
// Entries with zero Next sort to end of heap
func (h entryHeap) Less(i, j int) bool {
    if h[i].Next.IsZero() {
        return false  // Zero times sort last
    }
    if h[j].Next.IsZero() {
        return true
    }
    return h[i].Next.Before(h[j].Next)
}
```

## Consequences

### Positive

- **Simple interface**: No error return to handle
- **Go idiomatic**: Zero value as "not set" is common
- **Efficient comparison**: `IsZero()` is fast
- **No nil checks**: Value type, never nil
- **Clear semantics**: Zero time is obviously not a valid schedule

### Negative

- **Year 1 edge case**: Technically `time.Time{}` is year 1, not "no time"
- **Must check**: Callers must remember to check IsZero()
- **No error details**: Can't distinguish "exhausted" from "invalid"

### Neutral

- **Sorting convention**: Zero times need special handling in heap
- **Documentation**: Must clearly document the convention

## Implementation Details

### Heap Sorting

Zero-time entries sort to the end:
```go
// Heap order: [due entries...] [future entries...] [exhausted entries]
```

### Entry Validity

```go
func (e Entry) Valid() bool { return e.ID != 0 }  // ID check, not Next

// Next.IsZero() means exhausted, not invalid
entry := c.Entry(id)
if entry.Valid() && !entry.Next.IsZero() {
    // Entry exists and has future runs
}
```

### Run Loop

```go
for _, entry := range c.entries {
    if entry.Next.IsZero() {
        break  // No more due entries (sorted to end)
    }
    if entry.Next.After(now) {
        break  // Future entries
    }
    // Execute entry
}
```

## Alternatives Considered

### 1. Error Return

```go
func (s Schedule) Next(t time.Time) (time.Time, error) {
    if exhausted {
        return time.Time{}, ErrScheduleExhausted
    }
    return next, nil
}
```

- **Rejected**: Complicates interface
- Every caller must handle error
- Error values need definition

### 2. Pointer Return

```go
func (s Schedule) Next(t time.Time) *time.Time {
    if exhausted {
        return nil
    }
    return &next
}
```

- **Rejected**: Allocation on every call
- Nil pointer risks
- Less efficient

### 3. MaxTime Sentinel

```go
var MaxTime = time.Unix(1<<62-1, 0)

func (s Schedule) Next(t time.Time) time.Time {
    if exhausted {
        return MaxTime  // Far future
    }
    return next
}
```

- **Rejected**: Magic value is less obvious
- Could be confused with legitimate far-future schedule
- Platform-dependent maximum

### 4. Separate Exhausted Method

```go
type Schedule interface {
    Next(time.Time) time.Time
    Exhausted() bool
}
```

- **Rejected**: Stateful interface
- Race conditions between calls
- More complex implementation

## References

- Go time.Time zero value: https://pkg.go.dev/time#Time
- ADR-001: Min-Heap for Entry Scheduling
