# ADR-014: maxIdleDuration for Responsive Idle

## Status
Accepted

## Date
2025-12-14

## Context

When no entries are scheduled (empty heap or all entries exhausted), the scheduler run loop must wait. Two approaches:

**Option A: Block indefinitely**
```go
if len(entries) == 0 {
    select {
    case <-c.add:    // Wait for new entry
    case <-c.stop:   // Or stop signal
    }
}
```

**Option B: Sleep with timeout**
```go
const maxIdleDuration = 100000 * time.Hour  // ~11.4 years

timer := time.NewTimer(maxIdleDuration)
select {
case <-timer.C:      // Wake periodically
case <-c.add:        // New entry
case <-c.stop:       // Stop signal
}
```

**Problem with blocking:** The select statement with only channels works, but:
- Less consistent code structure (special case vs unified loop)
- Edge cases around timer management
- Some platforms have timer duration limits

## Decision

Use a very long but finite duration (100,000 hours ≈ 11.4 years) as "practical infinity":

```go
const maxIdleDuration = 100000 * time.Hour

func (c *Cron) run() {
    for {
        var timer *time.Timer
        if len(c.entries) == 0 {
            timer = time.NewTimer(maxIdleDuration)
        } else {
            timer = time.NewTimer(c.entries[0].Next.Sub(c.now()))
        }

        select {
        case <-timer.C:
            // Either job due or (unlikely) 11 years passed
        case <-c.add:
            // New entry added
        case <-c.stop:
            return
        }
    }
}
```

**Rationale:**
- Unified code path for all cases
- No special handling for empty heap
- Well within timer duration limits on all platforms
- Scheduler remains responsive to add/stop

## Consequences

### Positive

- **Consistent code**: Same select structure for all cases
- **Responsive**: Always handles add/stop promptly
- **Platform safe**: Avoids timer overflow concerns
- **Debuggable**: Timer always has a value

### Negative

- **Not truly idle**: Theoretical timer overhead (negligible)
- **Magic number**: 100,000 hours is arbitrary
- **Memory**: Timer allocated even when idle

### Neutral

- **11.4 years**: Effectively infinite for any practical deployment
- **No real wakeups**: Timer expiry is astronomically unlikely

## Alternatives Considered

### 1. True Blocking

```go
if empty {
    select {
    case <-c.add:
    case <-c.stop:
    }
} else {
    select {
    case <-timer.C:
    case <-c.add:
    case <-c.stop:
    }
}
```

- **Rejected**: Duplicate select statements
- More complex control flow
- Harder to maintain

### 2. Channel-Based Idle Signal

```go
idle := make(chan struct{})
if empty {
    close(idle)  // Unblock immediately on add
}
```

- **Rejected**: Added complexity for no benefit
- Must manage idle channel lifecycle

### 3. Shorter Idle Duration (e.g., 1 hour)

```go
const maxIdleDuration = time.Hour
```

- **Rejected**: Unnecessary wakeups in long-idle systems
- Wastes CPU on timer processing

### 4. Context with No Deadline

```go
ctx, cancel := context.WithCancel(context.Background())
<-ctx.Done()
```

- **Rejected**: Can't select on context without timeout
- Would need wrapper channel

## Platform Considerations

Maximum timer durations vary by platform:
- Most systems: ~292 years (int64 nanoseconds)
- Some embedded: May have shorter limits

100,000 hours is safely within all known limits.

## References

- Go time.Timer: https://pkg.go.dev/time#Timer
- Int64 duration limits: https://pkg.go.dev/time#Duration
