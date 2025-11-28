# Testing Guide

> How to write deterministic tests for scheduled jobs using FakeClock

## Table of Contents

- [Overview](#overview)
- [FakeClock Basics](#fakeclock-basics)
- [Common Testing Patterns](#common-testing-patterns)
- [Advanced Techniques](#advanced-techniques)
- [Troubleshooting](#troubleshooting)

---

## Overview

Testing time-dependent code is challenging because:
- Real time tests are slow (waiting for timers)
- Real time tests are flaky (timing variations)
- Real time tests can't test edge cases (DST, year boundaries)

go-cron solves this with `FakeClock` - a controllable clock implementation that:
- Lets you advance time instantly
- Fires timers deterministically
- Enables testing of any time scenario

---

## FakeClock Basics

### Creating a FakeClock

```go
import (
    "testing"
    "time"

    "github.com/netresearch/go-cron"
)

func TestMyJob(t *testing.T) {
    // Create FakeClock at a specific time
    startTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
    fakeClock := cron.NewFakeClock(startTime)

    // Use it with cron
    c := cron.New(cron.WithClock(fakeClock))
}
```

### Key Methods

| Method | Description |
|--------|-------------|
| `NewFakeClock(t)` | Create clock at time t |
| `Now()` | Get current fake time |
| `Advance(d)` | Move time forward by duration d |
| `Set(t)` | Jump to specific time t |
| `BlockUntil(n)` | Wait until n timers registered |
| `TimerCount()` | Get number of active timers |

### Basic Test Structure

```go
func TestScheduledJob(t *testing.T) {
    // 1. Setup: Create FakeClock and Cron
    start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
    fakeClock := cron.NewFakeClock(start)
    c := cron.New(cron.WithClock(fakeClock))

    // 2. Add jobs
    var called bool
    c.AddFunc("@every 1h", func() { called = true })

    // 3. Start scheduler
    c.Start()
    defer c.Stop()

    // 4. Synchronize with scheduler
    fakeClock.BlockUntil(1)  // Wait for timer to be registered

    // 5. Advance time to trigger job
    fakeClock.Advance(time.Hour)

    // 6. Allow goroutine to execute
    time.Sleep(10 * time.Millisecond)

    // 7. Assert
    if !called {
        t.Error("job should have been called")
    }
}
```

---

## Common Testing Patterns

### Pattern 1: Testing Job Execution Count

```go
func TestJobRunsMultipleTimes(t *testing.T) {
    start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
    fakeClock := cron.NewFakeClock(start)
    c := cron.New(cron.WithClock(fakeClock))

    var count int32
    c.AddFunc("@every 1m", func() {
        atomic.AddInt32(&count, 1)
    })

    c.Start()
    defer c.Stop()

    fakeClock.BlockUntil(1)

    // Advance 5 minutes - job should run 5 times
    for i := 0; i < 5; i++ {
        fakeClock.Advance(time.Minute)
        time.Sleep(10 * time.Millisecond)
    }

    if got := atomic.LoadInt32(&count); got != 5 {
        t.Errorf("expected 5 runs, got %d", got)
    }
}
```

### Pattern 2: Testing Multiple Jobs

```go
func TestMultipleJobsDifferentIntervals(t *testing.T) {
    start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
    fakeClock := cron.NewFakeClock(start)
    c := cron.New(cron.WithClock(fakeClock))

    var (
        fastCount int32
        slowCount int32
    )

    c.AddFunc("@every 1m", func() { atomic.AddInt32(&fastCount, 1) })
    c.AddFunc("@every 5m", func() { atomic.AddInt32(&slowCount, 1) })

    c.Start()
    defer c.Stop()
    fakeClock.BlockUntil(1)

    // Advance 10 minutes
    for i := 0; i < 10; i++ {
        fakeClock.Advance(time.Minute)
        time.Sleep(10 * time.Millisecond)
    }

    if got := atomic.LoadInt32(&fastCount); got != 10 {
        t.Errorf("fast job: expected 10, got %d", got)
    }
    if got := atomic.LoadInt32(&slowCount); got != 2 {
        t.Errorf("slow job: expected 2, got %d", got)
    }
}
```

### Pattern 3: Testing Cron Expressions

```go
func TestCronExpression(t *testing.T) {
    // Start at 10:29:00
    start := time.Date(2024, 1, 15, 10, 29, 0, 0, time.UTC)
    fakeClock := cron.NewFakeClock(start)

    // Use seconds parser
    c := cron.New(
        cron.WithClock(fakeClock),
        cron.WithSeconds(),
    )

    var fired bool
    // Fire at second 0 of minute 30 of hour 10
    c.AddFunc("0 30 10 * * *", func() { fired = true })

    c.Start()
    defer c.Stop()
    fakeClock.BlockUntil(1)

    // Advance to 10:30:00
    fakeClock.Advance(time.Minute)
    time.Sleep(10 * time.Millisecond)

    if !fired {
        t.Error("job should have fired at 10:30:00")
    }
}
```

### Pattern 4: Testing Dynamic Job Addition

```go
func TestAddJobWhileRunning(t *testing.T) {
    start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
    fakeClock := cron.NewFakeClock(start)
    c := cron.New(cron.WithClock(fakeClock))

    var firstRan, secondRan bool
    c.AddFunc("@every 1h", func() { firstRan = true })

    c.Start()
    defer c.Stop()
    fakeClock.BlockUntil(1)

    // Add another job while running
    c.AddFunc("@every 30m", func() { secondRan = true })
    fakeClock.BlockUntil(1)

    // Advance 30 minutes - only second job should fire
    fakeClock.Advance(30 * time.Minute)
    time.Sleep(10 * time.Millisecond)

    if firstRan {
        t.Error("first job should not have run yet")
    }
    if !secondRan {
        t.Error("second job should have run")
    }
}
```

### Pattern 5: Testing Job Removal

```go
func TestRemoveJob(t *testing.T) {
    start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
    fakeClock := cron.NewFakeClock(start)
    c := cron.New(cron.WithClock(fakeClock))

    var count int32
    id, _ := c.AddFunc("@every 1m", func() {
        atomic.AddInt32(&count, 1)
    })

    c.Start()
    defer c.Stop()
    fakeClock.BlockUntil(1)

    // Run once
    fakeClock.Advance(time.Minute)
    time.Sleep(10 * time.Millisecond)

    // Remove the job
    c.Remove(id)

    // Advance more - should not increment
    fakeClock.Advance(time.Minute)
    time.Sleep(10 * time.Millisecond)

    if got := atomic.LoadInt32(&count); got != 1 {
        t.Errorf("expected 1 run, got %d", got)
    }
}
```

### Pattern 6: Using WaitGroup for Synchronization

```go
func TestWithWaitGroup(t *testing.T) {
    start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
    fakeClock := cron.NewFakeClock(start)
    c := cron.New(cron.WithClock(fakeClock))

    var wg sync.WaitGroup
    wg.Add(3)  // Expect 3 runs

    c.AddFunc("@every 1m", func() { wg.Done() })

    c.Start()
    defer c.Stop()
    fakeClock.BlockUntil(1)

    // Advance 3 minutes
    for i := 0; i < 3; i++ {
        fakeClock.Advance(time.Minute)
        time.Sleep(10 * time.Millisecond)
    }

    // Wait with timeout
    done := make(chan struct{})
    go func() {
        wg.Wait()
        close(done)
    }()

    select {
    case <-done:
        // Success
    case <-time.After(100 * time.Millisecond):
        t.Error("jobs did not complete in time")
    }
}
```

---

## Advanced Techniques

### Testing Time Zones

```go
func TestTimezone(t *testing.T) {
    tokyo, _ := time.LoadLocation("Asia/Tokyo")

    // 9:00 AM in Tokyo
    start := time.Date(2024, 1, 15, 9, 0, 0, 0, tokyo)
    fakeClock := cron.NewFakeClock(start)

    c := cron.New(
        cron.WithClock(fakeClock),
        cron.WithLocation(tokyo),
    )

    var fired bool
    // Fire at 10:00 Tokyo time
    c.AddFunc("0 10 * * *", func() { fired = true })

    c.Start()
    defer c.Stop()
    fakeClock.BlockUntil(1)

    // Advance 1 hour
    fakeClock.Advance(time.Hour)
    time.Sleep(10 * time.Millisecond)

    if !fired {
        t.Error("job should have fired at 10:00 Tokyo time")
    }
}
```

### Testing DST Transitions

```go
func TestDSTSpringForward(t *testing.T) {
    // US DST: March 10, 2024, 2:00 AM becomes 3:00 AM
    nyc, _ := time.LoadLocation("America/New_York")

    // Start just before DST transition
    start := time.Date(2024, 3, 10, 1, 30, 0, 0, nyc)
    fakeClock := cron.NewFakeClock(start)

    c := cron.New(
        cron.WithClock(fakeClock),
        cron.WithLocation(nyc),
    )

    var fired bool
    // This time doesn't exist during spring forward
    c.AddFunc("30 2 * * *", func() { fired = true })

    c.Start()
    defer c.Stop()
    fakeClock.BlockUntil(1)

    // Advance past DST transition
    fakeClock.Advance(2 * time.Hour)
    time.Sleep(10 * time.Millisecond)

    // Job should have run immediately after the skip
    if !fired {
        t.Error("job should have fired after DST skip")
    }
}
```

### Testing Year Boundaries

```go
func TestYearBoundary(t *testing.T) {
    // December 31, 11:59 PM
    start := time.Date(2024, 12, 31, 23, 59, 0, 0, time.UTC)
    fakeClock := cron.NewFakeClock(start)

    c := cron.New(cron.WithClock(fakeClock))

    var fired bool
    // Fire at midnight on Jan 1
    c.AddFunc("0 0 1 1 *", func() { fired = true })

    c.Start()
    defer c.Stop()
    fakeClock.BlockUntil(1)

    // Advance to midnight
    fakeClock.Advance(time.Minute)
    time.Sleep(10 * time.Millisecond)

    if !fired {
        t.Error("job should have fired at New Year")
    }
}
```

### Testing Job Wrappers

```go
func TestSkipIfStillRunning(t *testing.T) {
    start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
    fakeClock := cron.NewFakeClock(start)

    c := cron.New(
        cron.WithClock(fakeClock),
        cron.WithChain(cron.SkipIfStillRunning(cron.DiscardLogger)),
    )

    var (
        running  int32
        maxConc  int32
        runCount int32
    )

    c.AddFunc("@every 100ms", func() {
        current := atomic.AddInt32(&running, 1)
        atomic.AddInt32(&runCount, 1)

        // Track max concurrency
        for {
            old := atomic.LoadInt32(&maxConc)
            if current <= old || atomic.CompareAndSwapInt32(&maxConc, old, current) {
                break
            }
        }

        // Simulate slow job (longer than interval)
        time.Sleep(50 * time.Millisecond)
        atomic.AddInt32(&running, -1)
    })

    c.Start()
    defer c.Stop()
    fakeClock.BlockUntil(1)

    // Rapid advances should skip overlapping runs
    for i := 0; i < 10; i++ {
        fakeClock.Advance(100 * time.Millisecond)
        time.Sleep(10 * time.Millisecond)
    }

    // Wait for any running jobs
    time.Sleep(100 * time.Millisecond)

    if max := atomic.LoadInt32(&maxConc); max > 1 {
        t.Errorf("max concurrency should be 1, got %d", max)
    }
}
```

### Table-Driven Tests

```go
func TestScheduleNext(t *testing.T) {
    tests := []struct {
        name     string
        spec     string
        start    time.Time
        expected time.Time
    }{
        {
            name:     "every minute",
            spec:     "* * * * *",
            start:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
            expected: time.Date(2024, 1, 1, 12, 1, 0, 0, time.UTC),
        },
        {
            name:     "specific hour",
            spec:     "0 15 * * *",
            start:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
            expected: time.Date(2024, 1, 1, 15, 0, 0, 0, time.UTC),
        },
        {
            name:     "monthly",
            spec:     "0 0 1 * *",
            start:    time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
            expected: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            schedule, err := cron.ParseStandard(tt.spec)
            if err != nil {
                t.Fatalf("parse error: %v", err)
            }

            got := schedule.Next(tt.start)
            if !got.Equal(tt.expected) {
                t.Errorf("Next() = %v, want %v", got, tt.expected)
            }
        })
    }
}
```

---

## Troubleshooting

### Job Doesn't Fire

**Problem:** Advancing time but job doesn't run.

**Solutions:**
1. Ensure `BlockUntil(1)` is called after `Start()`
2. Add `time.Sleep(10 * time.Millisecond)` after `Advance()`
3. Verify the cron expression matches expected time

```go
// Wrong
c.Start()
fakeClock.Advance(time.Hour)  // Timer might not be registered yet

// Right
c.Start()
fakeClock.BlockUntil(1)       // Wait for timer
fakeClock.Advance(time.Hour)
time.Sleep(10 * time.Millisecond)  // Let goroutine execute
```

### WaitGroup Panic

**Problem:** `sync: negative WaitGroup counter`

**Cause:** Job runs more times than expected.

**Solution:** Count how many times job will actually fire:

```go
// If you have 2 every-second jobs and advance 1 second:
// Both fire = 2 Done() calls
wg.Add(2)  // Not 1!
```

### Race Conditions

**Problem:** Test passes sometimes, fails other times.

**Solutions:**
1. Use `atomic` package for counters
2. Use proper synchronization primitives
3. Add sufficient sleep after `Advance()`

```go
// Wrong
var count int
c.AddFunc("@every 1m", func() { count++ })

// Right
var count int32
c.AddFunc("@every 1m", func() { atomic.AddInt32(&count, 1) })
```

### Timer Not Registered

**Problem:** `BlockUntil(1)` hangs forever.

**Cause:** Job might not create a timer (e.g., zero schedule).

**Solution:** Check that schedule returns valid next time:

```go
schedule, _ := cron.ParseStandard(spec)
next := schedule.Next(time.Now())
if next.IsZero() {
    t.Fatal("schedule never fires")
}
```

---

## Best Practices

1. **Always use `BlockUntil(1)`** after starting cron
2. **Always add small sleep** after advancing time
3. **Use `atomic` operations** for shared counters
4. **Use table-driven tests** for schedule parsing
5. **Test edge cases**: DST, year boundaries, leap years
6. **Clean up with `defer c.Stop()`**
7. **Use `time.UTC`** for reproducible tests

---

*Generated: 2024-11-28*
