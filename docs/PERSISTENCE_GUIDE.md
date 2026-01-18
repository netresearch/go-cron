# Persistence Integration Guide

This guide shows how to integrate go-cron with external persistence systems to achieve durable job scheduling that survives application restarts.

## Why Persistence is Out of Scope

go-cron intentionally does not include built-in persistence because:

1. **No forced dependencies**: Users shouldn't need Redis/PostgreSQL/MongoDB just to schedule jobs
2. **Drop-in compatibility**: Maintains simple migration from robfig/cron
3. **Choice of backend**: You choose the persistence layer that fits your stack
4. **Simpler core**: Fewer edge cases, easier to test and maintain

See [ADR-020](adr/ADR-020-feature-scope-boundary.md) for the full rationale.

## Integration Patterns

### Pattern 1: Persist Last Run Time (Recommended)

Store the last successful run time for each job. On restart, use this to determine if catch-up is needed.

```go
package main

import (
    "context"
    "database/sql"
    "log"
    "time"

    cron "github.com/netresearch/go-cron"
)

// JobStore persists job execution state
type JobStore struct {
    db *sql.DB
}

func (s *JobStore) GetLastRun(jobName string) (time.Time, error) {
    var lastRun time.Time
    err := s.db.QueryRow(
        "SELECT last_run FROM cron_jobs WHERE name = $1",
        jobName,
    ).Scan(&lastRun)
    if err == sql.ErrNoRows {
        return time.Time{}, nil // Never run before
    }
    return lastRun, err
}

func (s *JobStore) SetLastRun(jobName string, t time.Time) error {
    _, err := s.db.Exec(`
        INSERT INTO cron_jobs (name, last_run) VALUES ($1, $2)
        ON CONFLICT (name) DO UPDATE SET last_run = $2
    `, jobName, t)
    return err
}

func main() {
    store := &JobStore{db: openDB()}

    // Create scheduler with observability hooks for persistence
    c := cron.New(cron.WithObservability(cron.ObservabilityHooks{
        OnJobComplete: func(entry cron.Entry, err error) {
            if err == nil && entry.Name != "" {
                if err := store.SetLastRun(entry.Name, time.Now()); err != nil {
                    log.Printf("Failed to persist last run for %s: %v", entry.Name, err)
                }
            }
        },
    }))

    // Register jobs with names for tracking
    c.AddFunc("0 9 * * *", dailyReport, cron.WithName("daily-report"))
    c.AddFunc("0 * * * *", hourlySync, cron.WithName("hourly-sync"))

    // Check for missed jobs on startup
    checkMissedJobs(c, store)

    c.Start()
    // ... graceful shutdown handling
}

func checkMissedJobs(c *cron.Cron, store *JobStore) {
    for _, entry := range c.Entries() {
        if entry.Name == "" {
            continue
        }

        lastRun, err := store.GetLastRun(entry.Name)
        if err != nil {
            log.Printf("Error getting last run for %s: %v", entry.Name, err)
            continue
        }

        if lastRun.IsZero() {
            continue // Never run, let normal schedule handle it
        }

        // Calculate expected runs since last execution
        expectedNext := entry.Schedule.Next(lastRun)
        if expectedNext.Before(time.Now()) {
            log.Printf("Job %s missed execution at %v, running catch-up",
                entry.Name, expectedNext)
            go entry.Job.Run() // Run immediately
        }
    }
}
```

### Pattern 2: Full Schedule Persistence

Store the complete schedule definition for jobs that can be added/removed at runtime.

```go
package main

import (
    "encoding/json"

    cron "github.com/netresearch/go-cron"
)

// PersistedJob represents a job stored in the database
type PersistedJob struct {
    Name     string    `json:"name"`
    Schedule string    `json:"schedule"`  // Cron expression
    Endpoint string    `json:"endpoint"`  // What to call
    LastRun  time.Time `json:"last_run"`
    Enabled  bool      `json:"enabled"`
}

type SchedulerService struct {
    cron  *cron.Cron
    store JobRepository
    jobs  map[string]cron.EntryID // Track entry IDs by name
}

func (s *SchedulerService) LoadJobsFromDB(ctx context.Context) error {
    jobs, err := s.store.ListEnabled(ctx)
    if err != nil {
        return err
    }

    for _, job := range jobs {
        if err := s.registerJob(job); err != nil {
            log.Printf("Failed to register job %s: %v", job.Name, err)
        }
    }
    return nil
}

func (s *SchedulerService) registerJob(job PersistedJob) error {
    entryID, err := s.cron.AddFunc(
        job.Schedule,
        s.makeHandler(job),
        cron.WithName(job.Name),
    )
    if err != nil {
        return err
    }
    s.jobs[job.Name] = entryID
    return nil
}

func (s *SchedulerService) AddJob(ctx context.Context, job PersistedJob) error {
    // Persist first, then register
    if err := s.store.Create(ctx, job); err != nil {
        return err
    }
    return s.registerJob(job)
}

func (s *SchedulerService) RemoveJob(ctx context.Context, name string) error {
    if entryID, ok := s.jobs[name]; ok {
        s.cron.Remove(entryID)
        delete(s.jobs, name)
    }
    return s.store.Delete(ctx, name)
}

func (s *SchedulerService) makeHandler(job PersistedJob) func() {
    return func() {
        // Execute the job
        if err := callEndpoint(job.Endpoint); err != nil {
            log.Printf("Job %s failed: %v", job.Name, err)
            return
        }

        // Update last run time
        s.store.UpdateLastRun(context.Background(), job.Name, time.Now())
    }
}
```

### Pattern 3: Redis-Based State (High Availability)

For distributed deployments, use Redis to coordinate state.

```go
package main

import (
    "context"
    "time"

    "github.com/redis/go-redis/v9"
    cron "github.com/netresearch/go-cron"
)

type RedisJobStore struct {
    client *redis.Client
    prefix string
}

func (s *RedisJobStore) GetLastRun(ctx context.Context, jobName string) (time.Time, error) {
    val, err := s.client.Get(ctx, s.prefix+":lastrun:"+jobName).Result()
    if err == redis.Nil {
        return time.Time{}, nil
    }
    if err != nil {
        return time.Time{}, err
    }
    return time.Parse(time.RFC3339, val)
}

func (s *RedisJobStore) SetLastRun(ctx context.Context, jobName string, t time.Time) error {
    return s.client.Set(ctx,
        s.prefix+":lastrun:"+jobName,
        t.Format(time.RFC3339),
        30*24*time.Hour, // TTL: 30 days
    ).Err()
}

// Track job as running (for distributed awareness)
func (s *RedisJobStore) MarkRunning(ctx context.Context, jobName string, ttl time.Duration) (bool, error) {
    return s.client.SetNX(ctx,
        s.prefix+":running:"+jobName,
        "1",
        ttl,
    ).Result()
}

func (s *RedisJobStore) MarkComplete(ctx context.Context, jobName string) error {
    return s.client.Del(ctx, s.prefix+":running:"+jobName).Err()
}
```

## Database Schema Examples

### PostgreSQL

```sql
CREATE TABLE cron_jobs (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) UNIQUE NOT NULL,
    schedule VARCHAR(255) NOT NULL,
    endpoint VARCHAR(1024),
    last_run TIMESTAMP WITH TIME ZONE,
    last_success TIMESTAMP WITH TIME ZONE,
    last_error TEXT,
    enabled BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_cron_jobs_enabled ON cron_jobs(enabled) WHERE enabled = true;
CREATE INDEX idx_cron_jobs_last_run ON cron_jobs(last_run);
```

### MySQL

```sql
CREATE TABLE cron_jobs (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) UNIQUE NOT NULL,
    schedule VARCHAR(255) NOT NULL,
    endpoint VARCHAR(1024),
    last_run DATETIME,
    last_success DATETIME,
    last_error TEXT,
    enabled BOOLEAN DEFAULT true,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_enabled (enabled),
    INDEX idx_last_run (last_run)
);
```

### SQLite

```sql
CREATE TABLE cron_jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL,
    schedule TEXT NOT NULL,
    endpoint TEXT,
    last_run TEXT,  -- ISO8601 format
    last_success TEXT,
    last_error TEXT,
    enabled INTEGER DEFAULT 1,
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now'))
);
```

## Graceful Shutdown with Persistence

Ensure jobs complete and state is persisted before shutdown:

```go
func main() {
    ctx, cancel := context.WithCancel(context.Background())

    c := cron.New(cron.WithContext(ctx))
    // ... register jobs
    c.Start()

    // Wait for shutdown signal
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh

    log.Println("Shutting down...")

    // Stop accepting new jobs
    stopCtx := c.Stop()

    // Wait for running jobs to complete (with timeout)
    select {
    case <-stopCtx.Done():
        log.Println("All jobs completed")
    case <-time.After(30 * time.Second):
        log.Println("Timeout waiting for jobs, forcing shutdown")
        cancel() // Cancel context to signal jobs
    }

    // Final persistence flush if needed
    flushPendingState()
}
```

## Testing with Persistence

Use FakeClock to test persistence logic deterministically:

```go
func TestMissedJobCatchUp(t *testing.T) {
    // Set up fake clock at 10:00 AM
    clock := cron.NewFakeClock(time.Date(2026, 1, 18, 10, 0, 0, 0, time.UTC))

    store := &MockJobStore{
        lastRuns: map[string]time.Time{
            "daily-report": time.Date(2026, 1, 17, 9, 0, 0, 0, time.UTC), // Yesterday 9 AM
        },
    }

    c := cron.New(cron.WithClock(clock))

    var catchUpRan bool
    c.AddFunc("0 9 * * *", func() { catchUpRan = true }, cron.WithName("daily-report"))

    // Check for missed jobs
    checkMissedJobs(c, store)

    // The job should have caught up (9 AM today was missed)
    time.Sleep(10 * time.Millisecond) // Let goroutine run

    if !catchUpRan {
        t.Error("Expected catch-up job to run")
    }
}
```

## Alternatives for Heavy Persistence Needs

If you need sophisticated persistence with clustering, consider these alternatives:

| Tool | Best For |
|------|----------|
| [Dkron](https://dkron.io/) | Distributed cron with built-in persistence |
| [Temporal](https://temporal.io/) | Complex workflows with durable execution |
| [Airflow](https://airflow.apache.org/) | Data pipeline scheduling with full UI |
| [Asynq](https://github.com/hibiken/asynq) | Redis-based task queue with scheduling |

These tools solve different problems than go-cron. Use go-cron for simple in-process scheduling; use these when you need distributed, persistent job orchestration.

## See Also

- [DISTRIBUTED_LOCKING_GUIDE.md](DISTRIBUTED_LOCKING_GUIDE.md) - Coordinate jobs across multiple instances
- [OPERATIONS.md](OPERATIONS.md) - Production deployment patterns
- [COOKBOOK.md](COOKBOOK.md) - Common integration recipes
- [ADR-020](adr/ADR-020-feature-scope-boundary.md) - Why persistence is out of scope
