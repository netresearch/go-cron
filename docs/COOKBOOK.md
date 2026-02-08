# go-cron Cookbook

Practical recipes for common scheduling patterns. Each recipe is self-contained and production-ready.

## Table of Contents

1. [Basic Setup](#1-basic-setup)
2. [Retry Pattern](#2-retry-pattern)
3. [Circuit Breaker](#3-circuit-breaker)
4. [Metrics Integration](#4-metrics-integration)
5. [Graceful Shutdown](#5-graceful-shutdown)
6. [Testing Jobs](#6-testing-jobs)
7. [Dynamic Scheduling](#7-dynamic-scheduling)
8. [Multi-Timezone](#8-multi-timezone)
9. [Production-Ready Template](#9-production-ready-template)

---

## 1. Basic Setup

**Problem**: You need a simple scheduler with logging to get started.

**Solution**:

```go
package main

import (
    "log"
    "os"
    "os/signal"
    "syscall"

    cron "github.com/netresearch/go-cron"
)

func main() {
    // Create a logger
    logger := cron.VerbosePrintfLogger(log.New(os.Stdout, "cron: ", log.LstdFlags))

    // Create scheduler with logging and panic recovery
    c := cron.New(
        cron.WithLogger(logger),
        cron.WithChain(cron.Recover(logger)),
    )

    // Add jobs
    c.AddFunc("@every 1m", func() {
        log.Println("Running every minute")
    })

    c.AddFunc("0 * * * *", func() {
        log.Println("Running at the start of every hour")
    })

    // Start scheduler
    c.Start()
    log.Println("Scheduler started")

    // Wait for shutdown signal
    sig := make(chan os.Signal, 1)
    signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
    <-sig

    // Graceful shutdown
    log.Println("Shutting down...")
    c.Stop()
}
```

**Key Points**:
- Always use `cron.Recover(logger)` to prevent panics from crashing the scheduler
- `VerbosePrintfLogger` logs job executions for debugging
- Handle OS signals for clean shutdown

---

## 2. Retry Pattern

**Problem**: You need to call an external API that occasionally fails due to transient errors.

**Solution**:

```go
package main

import (
    "fmt"
    "io"
    "log"
    "net/http"
    "os"
    "time"

    cron "github.com/netresearch/go-cron"
)

func main() {
    logger := cron.VerbosePrintfLogger(log.New(os.Stdout, "cron: ", log.LstdFlags))

    c := cron.New(
        cron.WithChain(
            cron.Recover(logger),                        // Outermost: catch final panics
            cron.RetryWithBackoff(logger,
                3,              // maxRetries: try up to 3 times
                time.Second,    // initialDelay: 1s before first retry
                time.Minute,    // maxDelay: cap backoff at 1 minute
                2.0,            // multiplier: double delay each retry
            ),
        ),
    )

    c.AddFunc("@every 5m", func() {
        resp, err := http.Get("https://api.example.com/data")
        if err != nil {
            panic(fmt.Sprintf("API call failed: %v", err)) // Triggers retry
        }
        defer resp.Body.Close()

        if resp.StatusCode >= 500 {
            panic(fmt.Sprintf("Server error: %d", resp.StatusCode)) // Triggers retry
        }

        body, _ := io.ReadAll(resp.Body)
        log.Printf("Got data: %s", body[:min(len(body), 100)])
    })

    c.Start()
    select {} // Run forever
}
```

**Retry Timeline** (with 2.0 multiplier):
1. Initial run fails → wait 1s
2. Retry 1 fails → wait 2s
3. Retry 2 fails → wait 4s
4. Retry 3 fails → panic propagates to Recover

**Key Points**:
- Jobs signal failure by panicking
- `RetryWithBackoff` catches panics and retries with exponential backoff
- `Recover` catches the final panic if all retries are exhausted
- Set `maxRetries=-1` for unlimited retries (use with caution)

---

## 3. Circuit Breaker

**Problem**: An external service is down, and you want to stop hammering it.

**Solution**:

```go
package main

import (
    "fmt"
    "log"
    "net/http"
    "os"
    "time"

    cron "github.com/netresearch/go-cron"
)

func main() {
    logger := cron.VerbosePrintfLogger(log.New(os.Stdout, "cron: ", log.LstdFlags))

    c := cron.New(
        cron.WithChain(
            cron.Recover(logger),                    // Catch all panics
            cron.CircuitBreaker(logger,
                5,              // threshold: open after 5 consecutive failures
                5*time.Minute,  // cooldown: stay open for 5 minutes
            ),
            cron.RetryWithBackoff(logger, 2, time.Second, 30*time.Second, 2.0),
        ),
    )

    c.AddFunc("@every 30s", func() {
        resp, err := http.Get("https://flaky-service.example.com/health")
        if err != nil {
            panic(fmt.Sprintf("Health check failed: %v", err))
        }
        defer resp.Body.Close()

        if resp.StatusCode != 200 {
            panic(fmt.Sprintf("Unhealthy: %d", resp.StatusCode))
        }

        log.Println("Service is healthy")
    })

    c.Start()
    select {}
}
```

**Circuit States**:
1. **Closed**: Normal operation, failures are counted
2. **Open**: After threshold failures, job is skipped entirely
3. **Half-Open**: After cooldown, allows one test execution
4. **Closed**: Success in half-open state resets the circuit

**Key Points**:
- Protects downstream services from being overwhelmed
- Order matters: `CircuitBreaker` should be outside `RetryWithBackoff`
- Combine with alerting on circuit open events for operational awareness

---

## 4. Metrics Integration

**Problem**: You need to integrate job metrics with Prometheus or another monitoring system.

**Solution using ObservabilityHooks**:

```go
package main

import (
    "log"
    "net/http"
    "time"

    cron "github.com/netresearch/go-cron"
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
    jobsScheduled = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "cron_jobs_scheduled_total",
            Help: "Total number of job scheduling events",
        },
        []string{"job"},
    )
    jobsStarted = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "cron_jobs_started_total",
            Help: "Total number of job starts",
        },
        []string{"job"},
    )
    jobDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "cron_job_duration_seconds",
            Help:    "Job execution duration",
            Buckets: []float64{.1, .5, 1, 5, 10, 30, 60, 300},
        },
        []string{"job"},
    )
    jobPanics = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "cron_job_panics_total",
            Help: "Total number of job panics",
        },
        []string{"job"},
    )
)

func init() {
    prometheus.MustRegister(jobsScheduled, jobsStarted, jobDuration, jobPanics)
}

func main() {
    hooks := cron.ObservabilityHooks{
        OnSchedule: func(id cron.EntryID, name string, next time.Time) {
            jobsScheduled.WithLabelValues(name).Inc()
        },
        OnJobStart: func(id cron.EntryID, name string, scheduled time.Time) {
            jobsStarted.WithLabelValues(name).Inc()
        },
        OnJobComplete: func(id cron.EntryID, name string, duration time.Duration, recovered any) {
            jobDuration.WithLabelValues(name).Observe(duration.Seconds())
            if recovered != nil {
                jobPanics.WithLabelValues(name).Inc()
            }
        },
    }

    c := cron.New(cron.WithObservability(hooks))

    c.AddFunc("@every 1m", func() {
        // Your job logic
        log.Println("Job executed")
    })

    c.Start()

    // Expose metrics endpoint
    http.Handle("/metrics", promhttp.Handler())
    log.Fatal(http.ListenAndServe(":9090", nil))
}
```

**Key Points**:
- `ObservabilityHooks` integrates without modifying job code
- Track schedule events, starts, duration, and panics
- Use histogram buckets appropriate for your job durations
- Name your jobs for meaningful metrics labels

---

## 5. Graceful Shutdown

**Problem**: You need Kubernetes-style graceful termination with job completion.

**Solution**:

```go
package main

import (
    "context"
    "log"
    "os"
    "os/signal"
    "syscall"
    "time"

    cron "github.com/netresearch/go-cron"
)

func main() {
    logger := cron.VerbosePrintfLogger(log.New(os.Stdout, "cron: ", log.LstdFlags))

    c := cron.New(
        cron.WithLogger(logger),
        cron.WithChain(
            cron.Recover(logger),
            cron.TimeoutWithContext(logger, 5*time.Minute), // Pass context to jobs
        ),
    )

    // Use FuncJobWithContext for context-aware jobs
    c.AddJob("@every 1m", cron.FuncJobWithContext(func(ctx context.Context) {
        log.Println("Starting long job...")

        for i := 0; i < 60; i++ {
            select {
            case <-ctx.Done():
                log.Println("Job cancelled, cleaning up...")
                // Perform cleanup
                return
            case <-time.After(time.Second):
                log.Printf("Working... %d/60", i+1)
            }
        }

        log.Println("Job completed normally")
    }))

    c.Start()
    log.Println("Scheduler started")

    // Wait for shutdown signal
    sig := make(chan os.Signal, 1)
    signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
    <-sig

    log.Println("Received shutdown signal")

    // Create shutdown context with timeout
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    // Stop accepting new jobs and wait for running jobs
    stopCtx := c.Stop()

    select {
    case <-stopCtx.Done():
        log.Println("All jobs completed gracefully")
    case <-shutdownCtx.Done():
        log.Println("Shutdown timeout - some jobs may still be running")
    }
}
```

**Kubernetes Integration**:

```yaml
# Add to your Deployment spec
spec:
  terminationGracePeriodSeconds: 60
  containers:
    - name: scheduler
      lifecycle:
        preStop:
          exec:
            command: ["/bin/sh", "-c", "sleep 5"]
```

**Key Points**:
- Use `cron.FuncJobWithContext` for jobs that respect cancellation
- `c.Stop()` returns a context that's done when all jobs finish
- Set timeout to match your longest job + buffer
- Configure Kubernetes `terminationGracePeriodSeconds` accordingly

---

## 6. Testing Jobs

**Problem**: You need to unit test job functions and scheduling behavior.

**Solution - Testing Job Logic**:

```go
package myjobs

import (
    "testing"
    "time"

    cron "github.com/netresearch/go-cron"
)

// TestJobExecution tests that a job runs at the expected time
func TestJobExecution(t *testing.T) {
    // Create a fake clock starting at a known time
    start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
    clock := cron.NewFakeClock(start)

    c := cron.New(cron.WithClock(clock))

    executed := make(chan struct{}, 1)
    c.AddFunc("@every 1h", func() {
        executed <- struct{}{}
    })

    c.Start()
    defer c.Stop()

    // Wait for the timer to be registered
    clock.BlockUntil(1)

    // Advance time by 1 hour
    clock.Advance(time.Hour)

    // Verify job was executed
    select {
    case <-executed:
        // Success
    case <-time.After(time.Second):
        t.Fatal("Job was not executed within expected time")
    }
}

// TestJobExecutionCount tests that a job runs the expected number of times
func TestJobExecutionCount(t *testing.T) {
    clock := cron.NewFakeClock(time.Now())
    c := cron.New(cron.WithClock(clock))

    count := 0
    c.AddFunc("@every 10m", func() {
        count++
    })

    c.Start()
    defer c.Stop()

    clock.BlockUntil(1)

    // Advance 1 hour (should trigger 6 executions)
    clock.Advance(time.Hour)
    time.Sleep(100 * time.Millisecond) // Allow goroutines to complete

    if count != 6 {
        t.Errorf("Expected 6 executions, got %d", count)
    }
}

// TestDSTBehavior tests job behavior during DST transitions
func TestDSTBehavior(t *testing.T) {
    loc, _ := time.LoadLocation("America/New_York")

    // Just before spring DST (March 10, 2024, 1:59 AM)
    start := time.Date(2024, 3, 10, 1, 59, 0, 0, loc)
    clock := cron.NewFakeClock(start)

    c := cron.New(
        cron.WithClock(clock),
        cron.WithLocation(loc),
    )

    var executionTime time.Time
    c.AddFunc("30 2 * * *", func() { // 2:30 AM
        executionTime = clock.Now()
    })

    c.Start()
    defer c.Stop()

    clock.BlockUntil(1)
    clock.Advance(2 * time.Hour) // Cross the DST boundary

    time.Sleep(100 * time.Millisecond)

    // During spring DST, 2:30 AM doesn't exist
    // Job should run at 3:00 AM (ISC cron behavior)
    if executionTime.Hour() != 3 {
        t.Errorf("Expected job to run at 3 AM, got %v", executionTime)
    }
}

// TestJobPanic tests that panics are properly handled
func TestJobPanic(t *testing.T) {
    clock := cron.NewFakeClock(time.Now())

    panicRecovered := make(chan struct{}, 1)
    logger := &testLogger{onError: func() { panicRecovered <- struct{}{} }}

    c := cron.New(
        cron.WithClock(clock),
        cron.WithChain(cron.Recover(logger)),
    )

    c.AddFunc("@every 1m", func() {
        panic("test panic")
    })

    c.Start()
    defer c.Stop()

    clock.BlockUntil(1)
    clock.Advance(time.Minute)

    select {
    case <-panicRecovered:
        // Success - panic was recovered
    case <-time.After(time.Second):
        t.Fatal("Panic was not recovered")
    }
}

// testLogger implements cron.Logger for testing
type testLogger struct {
    onError func()
}

func (l *testLogger) Info(msg string, keysAndValues ...interface{})  {}
func (l *testLogger) Error(err error, msg string, keysAndValues ...interface{}) {
    if l.onError != nil {
        l.onError()
    }
}
```

**Key Points**:
- Use `FakeClock` for deterministic, fast tests
- `BlockUntil(n)` waits for n timers to be registered
- Test DST transitions with specific timezone and date
- Test error handling and panic recovery

---

## 7. Dynamic Scheduling

**Problem**: You need to add/remove jobs at runtime based on configuration changes.

**Solution**:

```go
package main

import (
    "encoding/json"
    "log"
    "net/http"
    "sync"

    cron "github.com/netresearch/go-cron"
)

type Scheduler struct {
    cron *cron.Cron
    jobs map[string]cron.EntryID
    mu   sync.RWMutex
}

func NewScheduler() *Scheduler {
    return &Scheduler{
        cron: cron.New(cron.WithChain(cron.Recover(cron.DefaultLogger))),
        jobs: make(map[string]cron.EntryID),
    }
}

func (s *Scheduler) Start() {
    s.cron.Start()
}

func (s *Scheduler) Stop() {
    s.cron.Stop()
}

// AddJob adds or updates a job with the given name
func (s *Scheduler) AddJob(name, spec string, fn func()) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    // Remove existing job if it exists
    if existingID, exists := s.jobs[name]; exists {
        s.cron.Remove(existingID)
    }

    // Add new job
    id, err := s.cron.AddFunc(spec, fn)
    if err != nil {
        return err
    }

    s.jobs[name] = id
    log.Printf("Job %q scheduled with spec %q (ID: %d)", name, spec, id)
    return nil
}

// RemoveJob removes a job by name
func (s *Scheduler) RemoveJob(name string) bool {
    s.mu.Lock()
    defer s.mu.Unlock()

    id, exists := s.jobs[name]
    if !exists {
        return false
    }

    s.cron.Remove(id)
    delete(s.jobs, name)
    log.Printf("Job %q removed", name)
    return true
}

// ListJobs returns all registered job names
func (s *Scheduler) ListJobs() []string {
    s.mu.RLock()
    defer s.mu.RUnlock()

    names := make([]string, 0, len(s.jobs))
    for name := range s.jobs {
        names = append(names, name)
    }
    return names
}

// HTTP API for dynamic job management
func main() {
    scheduler := NewScheduler()
    scheduler.Start()
    defer scheduler.Stop()

    // POST /jobs - Add/update a job
    http.HandleFunc("/jobs", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
            return
        }

        var req struct {
            Name string `json:"name"`
            Spec string `json:"spec"`
        }
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            http.Error(w, err.Error(), http.StatusBadRequest)
            return
        }

        err := scheduler.AddJob(req.Name, req.Spec, func() {
            log.Printf("Executing job: %s", req.Name)
        })
        if err != nil {
            http.Error(w, err.Error(), http.StatusBadRequest)
            return
        }

        w.WriteHeader(http.StatusCreated)
    })

    // DELETE /jobs/{name} - Remove a job
    // PATCH  /jobs/{name} - Update job spec
    http.HandleFunc("/jobs/", func(w http.ResponseWriter, r *http.Request) {
        switch r.Method {
        case http.MethodPatch:
            name := r.URL.Path[len("/jobs/"):]
            var req struct{ Spec string `json:"spec"` }
            if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
                http.Error(w, err.Error(), http.StatusBadRequest)
                return
            }

            scheduler.mu.RLock()
            id, exists := scheduler.jobs[name]
            scheduler.mu.RUnlock()

            if !exists {
                http.Error(w, "Job not found", http.StatusNotFound)
                return
            }

            if err := scheduler.cron.UpdateJob(id, req.Spec); err != nil {
                if errors.Is(err, cron.ErrEntryNotFound) {
                    // Job was removed after lookup; clean up stale mapping only if ID still matches
                    scheduler.mu.Lock()
                    if currentID, ok := scheduler.jobs[name]; ok && currentID == id {
                        delete(scheduler.jobs, name)
                    }
                    scheduler.mu.Unlock()
                    http.Error(w, "Job not found", http.StatusNotFound)
                    return
                }
                http.Error(w, err.Error(), http.StatusBadRequest)
                return
            }
            w.WriteHeader(http.StatusNoContent)
        case http.MethodDelete:
            name := r.URL.Path[len("/jobs/"):]
            if !scheduler.RemoveJob(name) {
                http.Error(w, "Job not found", http.StatusNotFound)
                return
            }
            w.WriteHeader(http.StatusNoContent)
        default:
            http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        }
    })

    // GET /jobs - List all jobs
    http.HandleFunc("/jobs/list", func(w http.ResponseWriter, r *http.Request) {
        json.NewEncoder(w).Encode(scheduler.ListJobs())
    })

    log.Println("API server starting on :8080")
    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

**Key Points**:
- Use a map to track job names to `EntryID`s
- Remove existing job before adding updated version
- `cron.Remove(id)` is safe to call while scheduler is running
- Protect map access with mutex for concurrent safety

---

## 8. Multi-Timezone

**Problem**: You need jobs running in different timezones for a global application.

**Solution**:

```go
package main

import (
    "log"
    "os"
    "time"

    cron "github.com/netresearch/go-cron"
)

func main() {
    logger := cron.VerbosePrintfLogger(log.New(os.Stdout, "cron: ", log.LstdFlags))

    // Default scheduler uses UTC for consistency
    c := cron.New(
        cron.WithLocation(time.UTC),
        cron.WithLogger(logger),
        cron.WithChain(cron.Recover(logger)),
    )

    // Job 1: Run at 9 AM New York time (business hours start)
    c.AddFunc("CRON_TZ=America/New_York 0 9 * * MON-FRI", func() {
        log.Println("NYC business hours started")
    })

    // Job 2: Run at 9 AM Tokyo time
    c.AddFunc("CRON_TZ=Asia/Tokyo 0 9 * * MON-FRI", func() {
        log.Println("Tokyo business hours started")
    })

    // Job 3: Run at 9 AM London time
    c.AddFunc("CRON_TZ=Europe/London 0 9 * * MON-FRI", func() {
        log.Println("London business hours started")
    })

    // Job 4: Global daily report at midnight UTC (no timezone override)
    c.AddFunc("0 0 * * *", func() {
        log.Println("Generating daily report (UTC midnight)")
    })

    // Job 5: Using pre-loaded timezone for complex schedules
    sydney, _ := time.LoadLocation("Australia/Sydney")
    schedule, _ := cron.ParseStandard("0 9 * * MON-FRI")
    // Override schedule's location
    if specSched, ok := schedule.(*cron.SpecSchedule); ok {
        specSched.Location = sydney
    }
    c.Schedule(schedule, cron.FuncJob(func() {
        log.Println("Sydney business hours started")
    }))

    c.Start()
    log.Println("Multi-timezone scheduler started")

    // Log current time in all zones for debugging
    ticker := time.NewTicker(time.Hour)
    for range ticker.C {
        now := time.Now()
        log.Printf("Current times - UTC: %s, NYC: %s, Tokyo: %s, London: %s",
            now.UTC().Format("15:04"),
            now.In(mustLoadLocation("America/New_York")).Format("15:04"),
            now.In(mustLoadLocation("Asia/Tokyo")).Format("15:04"),
            now.In(mustLoadLocation("Europe/London")).Format("15:04"),
        )
    }
}

func mustLoadLocation(name string) *time.Location {
    loc, err := time.LoadLocation(name)
    if err != nil {
        panic(err)
    }
    return loc
}
```

**Best Practices for Multi-Timezone**:

```go
// DO: Use explicit timezones for clarity
c.AddFunc("CRON_TZ=America/New_York 0 9 * * *", job)

// DO: Use UTC for system-level jobs
c.AddFunc("0 0 * * *", dailyCleanup) // With cron.WithLocation(time.UTC)

// DON'T: Mix local time assumptions
c.AddFunc("0 9 * * *", job) // Which 9 AM? Depends on server location!

// DO: Handle DST-sensitive times with care
// Avoid 2-3 AM in DST-observing timezones
c.AddFunc("CRON_TZ=America/New_York 0 4 * * *", job) // 4 AM is safe
```

**Key Points**:
- Set default location to UTC for predictability
- Use `CRON_TZ=` prefix for per-job timezone
- Use IANA timezone names (not abbreviations like "EST")
- Avoid scheduling during DST transition hours (2-3 AM)
- Log times in multiple zones for debugging

---

## 9. Production-Ready Template

**Problem**: You need a complete, production-ready scheduler setup.

**Solution**:

```go
package main

import (
    "context"
    "errors"
    "log"
    "log/slog"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    cron "github.com/netresearch/go-cron"
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics
var (
    jobDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "cron_job_duration_seconds",
            Help:    "Job execution duration",
            Buckets: prometheus.DefBuckets,
        },
        []string{"job"},
    )
    jobStatus = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "cron_job_status_total",
            Help: "Job completion status",
        },
        []string{"job", "status"},
    )
)

func init() {
    prometheus.MustRegister(jobDuration, jobStatus)
}

func main() {
    // Structured logging
    logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
    cronLogger := cron.NewSlogLogger(logger)

    // Observability hooks
    hooks := cron.ObservabilityHooks{
        OnJobStart: func(id cron.EntryID, name string, scheduled time.Time) {
            logger.Info("job started", "job", name, "entry_id", id)
        },
        OnJobComplete: func(id cron.EntryID, name string, duration time.Duration, recovered any) {
            jobDuration.WithLabelValues(name).Observe(duration.Seconds())
            status := "success"
            if recovered != nil {
                status = "panic"
                logger.Error("job panicked", "job", name, "panic", recovered)
            }
            jobStatus.WithLabelValues(name, status).Inc()
        },
    }

    // Create scheduler with all production features
    c := cron.New(
        cron.WithLocation(time.UTC),
        cron.WithLogger(cronLogger),
        cron.WithObservability(hooks),
        cron.WithMaxEntries(1000), // Prevent unbounded growth
        cron.WithChain(
            cron.Recover(cronLogger),                     // Always recover panics
            cron.RetryWithBackoff(cronLogger,
                3, time.Second, time.Minute, 2.0),        // Retry transient failures
            cron.CircuitBreaker(cronLogger, 5, 5*time.Minute), // Protect external services
            cron.SkipIfStillRunning(cronLogger),          // Prevent overlap
        ),
    )

    // Add your jobs
    if _, err := c.AddFunc("@every 1m", healthCheck); err != nil {
        logger.Error("failed to add job", "error", err)
        os.Exit(1)
    }

    if _, err := c.AddFunc("0 * * * *", hourlyReport); err != nil {
        logger.Error("failed to add job", "error", err)
        os.Exit(1)
    }

    // Start scheduler
    c.Start()
    logger.Info("scheduler started", "entries", len(c.Entries()))

    // Start metrics server
    go func() {
        http.Handle("/metrics", promhttp.Handler())
        http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
            w.WriteHeader(http.StatusOK)
            w.Write([]byte("OK"))
        })
        http.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
            if len(c.Entries()) > 0 {
                w.WriteHeader(http.StatusOK)
                w.Write([]byte("READY"))
            } else {
                w.WriteHeader(http.StatusServiceUnavailable)
                w.Write([]byte("NO JOBS"))
            }
        })
        if err := http.ListenAndServe(":9090", nil); err != nil && !errors.Is(err, http.ErrServerClosed) {
            logger.Error("metrics server failed", "error", err)
        }
    }()

    // Graceful shutdown
    sig := make(chan os.Signal, 1)
    signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
    <-sig

    logger.Info("shutdown initiated")

    // Create shutdown context
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    // Stop scheduler and wait for jobs
    stopCtx := c.Stop()

    select {
    case <-stopCtx.Done():
        logger.Info("all jobs completed")
    case <-ctx.Done():
        logger.Warn("shutdown timeout reached")
    }

    logger.Info("shutdown complete")
}

func healthCheck() {
    // Your health check logic
    log.Println("Health check executed")
}

func hourlyReport() {
    // Your report generation logic
    log.Println("Hourly report generated")
}
```

**Features Included**:
- Structured JSON logging with slog
- Prometheus metrics integration
- Health and readiness endpoints
- Panic recovery with retry and circuit breaker
- Job overlap prevention
- Entry limit protection
- Graceful shutdown with timeout
- UTC timezone for consistency

---

## Summary

| Recipe | When to Use |
|--------|-------------|
| Basic Setup | Getting started, simple scheduling |
| Retry Pattern | External API calls, transient failures |
| Circuit Breaker | Protecting failing external services |
| Metrics Integration | Production monitoring with Prometheus |
| Graceful Shutdown | Kubernetes deployments, clean termination |
| Testing Jobs | Unit and integration testing |
| Dynamic Scheduling | Runtime job management via API |
| Multi-Timezone | Global applications, regional schedules |
| Production Template | Complete production setup |

For more details, see:
- [API Reference](API_REFERENCE.md)
- [Architecture](ARCHITECTURE.md)
- [DST Handling](DST_HANDLING.md)
- [Migration Guide](MIGRATION.md)
