# ADR-003: Asynchronous Observability Hooks

## Status
Accepted

## Date
2024-10-01

## Context

Users need to integrate cron job metrics into their observability stack (Prometheus, DataDog, OpenTelemetry, etc.). The scheduler needs to emit events at key lifecycle points:

- Job scheduled (next run time calculated)
- Job started
- Job completed (with duration and panic info)

**Requirements:**
- Zero overhead when hooks are not configured
- Non-blocking: hook execution must not delay job execution
- Panic-safe: hook panics must not crash the scheduler
- Simple interface: easy to implement and understand

**Trade-offs to consider:**
- Synchronous hooks provide guaranteed ordering but can delay jobs
- Asynchronous hooks provide isolation but may lose events on shutdown

## Decision

Implement synchronous hook calls that execute in the scheduler's goroutine context, but design them for fast, non-blocking operations.

```go
type ObservabilityHooks struct {
    OnSchedule    func(entryID EntryID, name string, nextRun time.Time)
    OnJobStart    func(entryID EntryID, name string, scheduledTime time.Time)
    OnJobComplete func(entryID EntryID, name string, duration time.Duration, recovered any)
}
```

**Key design choices:**

1. **Synchronous calls**: Hooks are called directly, not via channels
2. **Caller responsibility**: Users must ensure hooks are fast (buffer/async internally if needed)
3. **Panic isolation**: Each hook call is wrapped in recover()
4. **Optional hooks**: Nil function pointers are checked before calling

```go
// Internal implementation
func (c *Cron) callOnJobComplete(e *Entry, duration time.Duration, recovered any) {
    if c.hooks.OnJobComplete == nil {
        return
    }
    defer func() {
        if r := recover(); r != nil {
            c.logger.Error(nil, "observability hook panicked", "hook", "OnJobComplete", "panic", r)
        }
    }()
    c.hooks.OnJobComplete(e.ID, e.Name, duration, recovered)
}
```

## Consequences

### Positive

- **Zero allocation** when hooks are nil
- **Simple mental model**: hooks run in expected order
- **Guaranteed delivery**: no events lost to channel buffers
- **Easy debugging**: stack traces show hook in context
- **Flexible implementation**: users can make hooks async if needed

### Negative

- **Blocking risk**: Slow hooks delay the scheduler
- **Thread safety**: Hook implementations must be thread-safe
- **No backpressure**: Slow consumer can't signal overload
- **Ordering within job**: OnJobStart and OnJobComplete are in different goroutines

### Neutral

- **User responsibility**: Performance characteristics depend on hook implementation
- **Documentation**: Must clearly state that hooks should be fast

## Alternatives Considered

### 1. Channel-Based Event Stream

```go
type ObservabilityEvent struct {
    Type      EventType
    EntryID   EntryID
    Timestamp time.Time
    // ...
}

func (c *Cron) Events() <-chan ObservabilityEvent
```

- **Rejected**: Adds complexity (channel management, buffer sizing)
- Risk of dropped events on slow consumers
- Requires users to run a separate goroutine

### 2. Callback Registration with Async Dispatch

```go
func (c *Cron) OnJobComplete(fn func(...)) {
    c.hooks = append(c.hooks, asyncWrapper(fn))
}
```

- **Rejected**: Hidden goroutine spawning
- Unpredictable ordering
- Resource leaks if hooks are slow

### 3. OpenTelemetry Integration

```go
c := cron.New(cron.WithTracer(otel.Tracer("cron")))
```

- **Rejected**: Adds external dependency
- Not all users want OpenTelemetry
- Can be built on top of the simple hooks

### 4. Event Sourcing Pattern

```go
type EventStore interface {
    Append(event Event)
}
```

- **Rejected**: Over-engineered for this use case
- Requires persistent storage decisions
- Can be built on top of simple hooks if needed

## Usage Guidance

```go
// DO: Fast, non-blocking hooks
hooks := cron.ObservabilityHooks{
    OnJobComplete: func(id cron.EntryID, name string, dur time.Duration, rec any) {
        // Counter increment is fast
        jobCounter.WithLabelValues(name, statusFromRecovered(rec)).Inc()
        jobDuration.WithLabelValues(name).Observe(dur.Seconds())
    },
}

// DO: Buffer if you need async processing
var eventChan = make(chan Event, 1000)

hooks := cron.ObservabilityHooks{
    OnJobComplete: func(id cron.EntryID, name string, dur time.Duration, rec any) {
        select {
        case eventChan <- Event{ID: id, Name: name, Duration: dur}:
        default:
            // Buffer full, drop event (or log)
        }
    },
}

// DON'T: Slow synchronous operations
hooks := cron.ObservabilityHooks{
    OnJobComplete: func(id cron.EntryID, name string, dur time.Duration, rec any) {
        http.Post("https://metrics.example.com", ...)  // SLOW!
    },
}
```

## References

- Prometheus client_golang patterns: https://prometheus.io/docs/guides/go-application/
- OpenTelemetry Go SDK design: https://opentelemetry.io/docs/instrumentation/go/
