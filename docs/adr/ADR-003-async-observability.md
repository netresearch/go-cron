# ADR-003: Asynchronous Observability Hooks

## Status
Accepted

## Date
2025-12-14

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

Implement asynchronous hook calls that execute in separate goroutines, ensuring the scheduler is never blocked by slow callbacks.

```go
type ObservabilityHooks struct {
    OnSchedule    func(entryID EntryID, name string, nextRun time.Time)
    OnJobStart    func(entryID EntryID, name string, scheduledTime time.Time)
    OnJobComplete func(entryID EntryID, name string, duration time.Duration, recovered any)
}
```

**Key design choices:**

1. **Asynchronous calls**: Hooks are spawned in goroutines (`go h.OnJobStart(...)`)
2. **Non-blocking**: Scheduler never waits for hook completion
3. **Optional hooks**: Nil function pointers are checked before spawning goroutines
4. **Thread-safety required**: Hook implementations must be safe for concurrent execution

```go
// Internal implementation
func (h *ObservabilityHooks) callOnJobStart(entryID EntryID, job Job, scheduledTime time.Time) {
    if h != nil && h.OnJobStart != nil {
        name := getJobName(job)
        go h.OnJobStart(entryID, name, scheduledTime)
    }
}
```

## Consequences

### Positive

- **Non-blocking**: Slow hooks never delay the scheduler or job execution
- **Isolation**: Hook panics don't crash the scheduler (contained in their own goroutine)
- **Simple API**: Users don't need to manage channels or goroutines
- **Zero overhead when nil**: No goroutines spawned if hooks are not configured

### Negative

- **No ordering guarantees**: Events may be processed out of order
- **Events may be lost on shutdown**: In-flight goroutines may not complete
- **Thread-safety required**: Hook implementations must handle concurrent calls
- **Slight latency**: Hooks execute after event, not during

### Neutral

- **Goroutine overhead**: Each event spawns a goroutine (acceptable for typical job frequencies)
- **No backpressure**: System can't signal to slow consumers

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

### 2. Synchronous Hook Calls

```go
func (c *Cron) callOnJobComplete(e *Entry, duration time.Duration, recovered any) {
    if c.hooks.OnJobComplete != nil {
        c.hooks.OnJobComplete(e.ID, e.Name, duration, recovered)
    }
}
```

- **Rejected**: Slow hooks would block the scheduler
- Users could make the scheduler unresponsive
- Would require complex timeout logic to prevent blocking

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

Since hooks run in their own goroutines, they won't block the scheduler. However, keep hooks efficient to avoid unbounded goroutine growth.

```go
// DO: Fast operations (counters, gauges)
hooks := cron.ObservabilityHooks{
    OnJobComplete: func(id cron.EntryID, name string, dur time.Duration, rec any) {
        // Prometheus counter/histogram updates are thread-safe and fast
        jobCounter.WithLabelValues(name, statusFromRecovered(rec)).Inc()
        jobDuration.WithLabelValues(name).Observe(dur.Seconds())
    },
}

// DO: Bounded buffering for slow consumers
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

// CAUTION: Slow operations spawn long-lived goroutines
hooks := cron.ObservabilityHooks{
    OnJobComplete: func(id cron.EntryID, name string, dur time.Duration, rec any) {
        // Each call spawns a goroutine that lives until HTTP completes
        // Many jobs + slow endpoint = goroutine accumulation
        http.Post("https://metrics.example.com", ...)
    },
}
```

## References

- Prometheus client_golang patterns: https://prometheus.io/docs/guides/go-application/
- OpenTelemetry Go SDK design: https://opentelemetry.io/docs/instrumentation/go/
