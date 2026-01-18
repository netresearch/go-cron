# ADR-018: Run-Immediately and Run-Once Entry Flags

## Status
Accepted

## Date
2025-12-14

## Context

Common scheduling patterns require special first-run behavior:

**Run Immediately:** Execute once now, then follow schedule
```go
// User wants: run now, then every hour
c.AddFunc("@hourly", job, cron.WithRunImmediately())
```

**Run Once:** Execute once and remove entry
```go
// User wants: run at next midnight, then never again
c.AddFunc("0 0 * * *", job, cron.WithRunOnce())
```

**Problem:** How to implement these without complicating the Schedule interface?

## Decision

Use internal Entry flags that modify scheduling behavior:

```go
type Entry struct {
    // ... other fields

    runImmediately bool  // Set by WithRunImmediately()
    runOnce        bool  // Set by WithRunOnce()
}
```

**Flag behavior:**

| Flag | First Schedule | After First Run |
|------|----------------|-----------------|
| `runImmediately` | Sets Next = now | Flag cleared, normal scheduling |
| `runOnce` | Normal scheduling | Entry removed after execution |

**Implementation:**

```go
func (c *Cron) scheduleEntry(e *Entry, now time.Time) {
    if e.runImmediately {
        e.Next = now
        e.runImmediately = false  // Clear flag
        return
    }
    e.Next = e.Schedule.Next(now)
}

func (c *Cron) runEntry(e *Entry) {
    e.WrappedJob.Run()

    if e.runOnce {
        c.removeEntry(e.ID)
        return
    }
    c.scheduleEntry(e, c.now())
}
```

## Consequences

### Positive

- **Simple API**: Just add option to existing methods
- **No Schedule changes**: Schedule interface unchanged
- **One-time effect**: Flags auto-clear after use
- **Composable**: Can combine with other options

### Negative

- **Internal state**: Entry has hidden mutable state
- **Single use**: Flags cleared after first effect
- **Not queryable**: Can't check if entry was run-once after removal

### Neutral

- **Option pattern**: Follows ADR-004 functional options
- **Clear semantics**: Flag names are self-documenting

## Implementation Details

### WithRunImmediately

```go
func WithRunImmediately() JobOption {
    return func(e *Entry) {
        e.runImmediately = true
    }
}

// Usage
c.AddFunc("@hourly", job, cron.WithRunImmediately())
// Runs: now, +1h, +2h, +3h, ...
```

### WithRunOnce

```go
func WithRunOnce() JobOption {
    return func(e *Entry) {
        e.runOnce = true
    }
}

// Usage
c.AddFunc("@hourly", job, cron.WithRunOnce())
// Runs: +1h (then entry removed)
```

### Combination

```go
c.AddFunc("@hourly", job,
    cron.WithRunImmediately(),
    cron.WithRunOnce(),
)
// Runs: now (then entry removed)
```

## Alternatives Considered

### 1. Separate Methods

```go
c.AddFuncImmediate("@hourly", job)
c.AddFuncOnce("@hourly", job)
```

- **Rejected**: Combinatorial explosion of methods
- `AddFuncImmediateOnce`? `AddJobImmediate`?

### 2. Schedule Wrappers

```go
type ImmediateSchedule struct {
    Schedule
    fired bool
}
```

- **Rejected**: Complicates Schedule interface
- Stateful schedules are harder to reason about

### 3. Entry Configuration Struct

```go
c.AddFunc("@hourly", job, EntryConfig{
    RunImmediately: true,
    RunOnce:        true,
})
```

- **Rejected**: Config struct is less composable
- Doesn't follow ADR-004 functional options

### 4. Separate Run Method

```go
c.AddFunc("@hourly", job)
c.Run(id)  // Manual trigger
```

- **Rejected**: Requires two calls
- Entry ID not known until after Add
- Race condition between Add and Run

## Edge Cases

### Run-Once with Run-Immediately

```go
c.AddFunc("@hourly", job,
    cron.WithRunImmediately(),
    cron.WithRunOnce(),
)
```

Behavior: Runs immediately, then entry is removed. Never waits for schedule.

### Run-Immediately with Past Schedule

```go
// At 10:30 AM
c.AddFunc("0 10 * * *", job, cron.WithRunImmediately())
```

Behavior:
1. Runs immediately (10:30 AM)
2. Next scheduled run: tomorrow 10:00 AM

## References

- ADR-004: Functional Options Pattern
