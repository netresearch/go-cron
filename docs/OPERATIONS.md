# Operations Guide

This guide covers running go-cron in production: shutdown patterns, job lifecycle, monitoring, and deployment considerations.

## Table of Contents

- [Graceful Shutdown](#graceful-shutdown)
- [Job Lifecycle](#job-lifecycle)
- [Concurrency and Thread Safety](#concurrency-and-thread-safety)
- [Resource Management](#resource-management)
- [Monitoring and Observability](#monitoring-and-observability)
- [Deployment Patterns](#deployment-patterns)
- [Production Checklist](#production-checklist)

---

## Graceful Shutdown

### Basic Shutdown

`Stop()` signals the scheduler to stop and returns a context that completes when all running jobs finish:

```go
c := cron.New()
c.AddFunc("@every 1m", myJob)
c.Start()

// Later, during shutdown:
ctx := c.Stop()

// Wait for jobs to finish (with timeout)
select {
case <-ctx.Done():
    log.Println("All jobs completed")
case <-time.After(30 * time.Second):
    log.Println("Timeout: some jobs still running")
}
```

### Convenience Methods

```go
// Block until all jobs complete (no timeout)
c.StopAndWait()

// Block with timeout, returns true if all jobs completed
if c.StopWithTimeout(30 * time.Second) {
    log.Println("Clean shutdown")
} else {
    log.Println("Some jobs did not complete in time")
}
```

### Context-Aware Jobs

Jobs implementing `JobWithContext` receive cancellation signals:

```go
type MyJob struct{}

func (j *MyJob) Run() {
    j.RunWithContext(context.Background())
}

func (j *MyJob) RunWithContext(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            log.Println("Job received shutdown signal")
            return
        case <-time.After(time.Second):
            // Do periodic work
        }
    }
}
```

When `Stop()` is called, the cron's base context is canceled, which propagates to all `RunWithContext` calls.

### Signal Handling Example

```go
func main() {
    c := cron.New()
    c.AddFunc("@every 5s", doWork)
    c.Start()

    // Wait for interrupt signal
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    <-sigChan

    log.Println("Shutting down...")
    if c.StopWithTimeout(30 * time.Second) {
        log.Println("Shutdown complete")
    } else {
        log.Println("Shutdown timed out")
        os.Exit(1)
    }
}
```

---

## Job Lifecycle

### Entry States

```
[Added] → [Scheduled] → [Running] → [Completed] → [Rescheduled]
              ↓                          ↓
          [Skipped]               [Removed (if RunOnce)]
```

### Adding Jobs While Running

Jobs can be safely added while the scheduler is running:

```go
c.Start()

// Safe: uses channel-based synchronization
id, _ := c.AddFunc("@hourly", newJob)

// Job will be scheduled on next tick
```

### Removing Jobs While Running

```go
// Safe: synchronous removal
entry := c.Remove(id)
if entry.Valid() {
    log.Printf("Removed job %s", entry.Name)
}
```

**Note:** If the job is currently executing, it will complete. Only future executions are prevented.

### Run-Once Jobs

Jobs can be configured to run once and self-remove:

```go
c.AddFunc("@every 1h", handler, cron.WithRunOnce())
// Job runs once at the next hour, then is automatically removed
```

### Run Immediately

```go
c.AddFunc("@daily", handler, cron.WithRunImmediately())
// Job runs immediately, then follows the daily schedule
```

---

## Concurrency and Thread Safety

### Guaranteed Thread-Safe Operations

All public methods are safe to call from any goroutine:

| Method | Thread-Safe | Notes |
|--------|-------------|-------|
| `AddFunc`, `AddJob`, `ScheduleJob` | Yes | Uses channel sync when running |
| `Remove` | Yes | Synchronous, blocks until complete |
| `Entry`, `EntryByName` | Yes | O(1) lookup |
| `Entries` | Yes | Returns snapshot copy |
| `Start` | Yes | Idempotent; second call is no-op |
| `Stop` | Yes | Idempotent |

### Job Execution Concurrency

By default, jobs run concurrently in separate goroutines:

```go
// These can run at the same time if schedules overlap
c.AddFunc("@every 1s", job1)
c.AddFunc("@every 1s", job2)
```

### Preventing Overlap

Use `SkipIfStillRunning` to skip executions if the previous is still running:

```go
c := cron.New(cron.WithChain(
    cron.SkipIfStillRunning(logger),
))
```

Or `DelayIfStillRunning` to queue the next execution:

```go
c := cron.New(cron.WithChain(
    cron.DelayIfStillRunning(logger),
))
```

### Shared State in Jobs

Jobs accessing shared state must use their own synchronization:

```go
type Counter struct {
    mu    sync.Mutex
    count int
}

func (c *Counter) Run() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.count++
}
```

---

## Resource Management

### Memory Considerations

1. **Entry storage**: Each entry uses ~200-300 bytes
2. **Index maps**: Two maps (by ID and name) for O(1) lookup
3. **Map compaction**: After 1000 deletions, maps are rebuilt to reclaim memory

For high-churn scenarios (frequent add/remove), the scheduler automatically compacts index maps.

### Pre-allocation for Bulk Operations

When adding many entries at once:

```go
c := cron.New(cron.WithCapacity(1000))
for i := 0; i < 1000; i++ {
    c.AddFunc("@hourly", jobs[i])
}
```

### Entry Limits

Prevent unbounded growth:

```go
c := cron.New(cron.WithMaxEntries(100))

_, err := c.AddFunc("@hourly", job)
if errors.Is(err, cron.ErrMaxEntriesReached) {
    log.Println("Too many scheduled jobs")
}
```

### Goroutine Management

- Each job execution spawns one goroutine
- Goroutines are tracked via `sync.WaitGroup`
- `Stop()` waits for all goroutines via the returned context
- No goroutine leaks if `Stop()` is called

---

## Monitoring and Observability

### ObservabilityHooks

Integrate with your metrics system:

```go
hooks := cron.ObservabilityHooks{
    OnJobStart: func(id cron.EntryID, name string, scheduled time.Time) {
        jobsStarted.WithLabelValues(name).Inc()
        scheduleLag.WithLabelValues(name).Observe(
            time.Since(scheduled).Seconds(),
        )
    },
    OnJobComplete: func(id cron.EntryID, name string, dur time.Duration, recovered any) {
        jobDuration.WithLabelValues(name).Observe(dur.Seconds())
        if recovered != nil {
            jobPanics.WithLabelValues(name).Inc()
        }
    },
    OnSchedule: func(id cron.EntryID, name string, next time.Time) {
        nextRunGauge.WithLabelValues(name).Set(float64(next.Unix()))
    },
}

c := cron.New(cron.WithObservability(hooks))
```

**Note:** Hooks run asynchronously in goroutines (see ADR-003).

### Logging

Configure structured logging:

```go
// Using slog (Go 1.21+)
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
c := cron.New(cron.WithLogger(cron.NewSlogLogger(logger)))

// Using logr interface
c := cron.New(cron.WithLogger(myLogrLogger))
```

### Health Checks

Expose scheduler state for health endpoints:

```go
func healthHandler(c *cron.Cron) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        entries := c.Entries()
        status := map[string]any{
            "scheduled_jobs": len(entries),
            "next_run":       entries[0].Next, // Entries sorted by next run
        }
        json.NewEncoder(w).Encode(status)
    }
}
```

---

## Deployment Patterns

### Single Instance

The scheduler is designed for single-instance deployment. For distributed systems, use external coordination:

```go
// Use distributed lock before job execution
c.AddFunc("@hourly", func() {
    lock, err := redisLock.Acquire(ctx, "job-lock", time.Minute)
    if err != nil {
        return // Another instance has the lock
    }
    defer lock.Release()

    doWork()
})
```

### Container Deployments

Handle container lifecycle:

```go
func main() {
    c := cron.New()
    // ... add jobs

    c.Start()

    // Kubernetes sends SIGTERM before killing
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGTERM)
    <-sigChan

    // Kubernetes gives 30s by default (terminationGracePeriodSeconds)
    c.StopWithTimeout(25 * time.Second)
}
```

### Timezone Handling

Always specify timezone explicitly in production:

```go
loc, _ := time.LoadLocation("America/New_York")
c := cron.New(cron.WithLocation(loc))

// Or per-schedule
c.AddFunc("TZ=Europe/London 0 9 * * *", ukJob)
```

---

## Production Checklist

### Before Deployment

- [ ] Set explicit timezone (`WithLocation` or `TZ=` prefix)
- [ ] Add `Recover()` wrapper to prevent panics from crashing scheduler
- [ ] Configure logging (`WithLogger`)
- [ ] Set up observability hooks for metrics
- [ ] Test DST transitions if timezone-sensitive
- [ ] Set `WithMaxEntries` if entries are user-controlled

### Shutdown

- [ ] Handle SIGTERM/SIGINT signals
- [ ] Call `Stop()` or `StopWithTimeout()` before exit
- [ ] Implement `JobWithContext` for long-running jobs
- [ ] Log incomplete jobs on timeout

### Monitoring

- [ ] Track job execution duration
- [ ] Alert on job panics (via hooks)
- [ ] Monitor schedule lag (time between scheduled and actual execution)
- [ ] Track entry count if dynamic

### Testing

- [ ] Use `FakeClock` for deterministic tests
- [ ] Test job overlap behavior
- [ ] Verify shutdown with running jobs
- [ ] Test DST transitions with real timezone

---

## Related Documentation

- [ARCHITECTURE.md](ARCHITECTURE.md) - Internal design details
- [TESTING_GUIDE.md](TESTING_GUIDE.md) - FakeClock and testing strategies
- [DST_HANDLING.md](DST_HANDLING.md) - Daylight saving time behavior
- [ADR-003](adr/ADR-003-async-observability.md) - Observability hooks design
- [ADR-010](adr/ADR-010-channel-synchronization.md) - Concurrency model
