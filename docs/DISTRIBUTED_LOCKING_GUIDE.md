# Distributed Locking Guide

This guide shows how to ensure only one instance of a job runs across multiple application instances using external locking mechanisms.

## Why Distributed Locking is Out of Scope

go-cron intentionally does not include built-in distributed locking because:

1. **Requires external infrastructure**: Redis, etcd, PostgreSQL, or Consul
2. **Many valid backends**: No single "right" choice for all deployments
3. **Composable design**: Locking is easily added via job wrappers
4. **Drop-in compatibility**: Maintains simple robfig/cron migration path

See [ADR-020](adr/ADR-020-feature-scope-boundary.md) for the full rationale.

## The Problem

When running multiple instances of your application (for high availability or scaling), each instance has its own go-cron scheduler. Without coordination, the same job runs on every instance simultaneously:

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  Instance 1 │     │  Instance 2 │     │  Instance 3 │
│  go-cron    │     │  go-cron    │     │  go-cron    │
│             │     │             │     │             │
│  9:00 AM    │     │  9:00 AM    │     │  9:00 AM    │
│  daily-job  │     │  daily-job  │     │  daily-job  │
│  ──────────►│     │  ──────────►│     │  ──────────►│
│  RUNS!      │     │  RUNS!      │     │  RUNS!      │
└─────────────┘     └─────────────┘     └─────────────┘
        │                   │                   │
        └───────────────────┴───────────────────┘
                          │
                   All 3 run simultaneously!
                   (Usually not what you want)
```

## Solution: Job Wrapper with Lock

Create a wrapper that acquires a distributed lock before running:

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  Instance 1 │     │  Instance 2 │     │  Instance 3 │
│             │     │             │     │             │
│  9:00 AM    │     │  9:00 AM    │     │  9:00 AM    │
│  TRY LOCK   │     │  TRY LOCK   │     │  TRY LOCK   │
│  ────────►  │     │  ────────►  │     │  ────────►  │
│  GOT IT! ✓  │     │  BLOCKED ✗  │     │  BLOCKED ✗  │
│  RUNS!      │     │  skip       │     │  skip       │
└─────────────┘     └─────────────┘     └─────────────┘
        │
        └── Only one instance runs the job
```

## Implementation Patterns

### Pattern 1: Redis Lock with redsync

[redsync](https://github.com/go-redsync/redsync) implements the Redlock algorithm for distributed locking.

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/go-redsync/redsync/v4"
    "github.com/go-redsync/redsync/v4/redis/goredis/v9"
    "github.com/redis/go-redis/v9"
    cron "github.com/netresearch/go-cron"
)

// WithRedisLock creates a wrapper that acquires a Redis lock before running
func WithRedisLock(rs *redsync.Redsync, lockName string, ttl time.Duration) cron.JobWrapper {
    return func(job cron.Job) cron.Job {
        return cron.FuncJob(func() {
            mutex := rs.NewMutex(lockName,
                redsync.WithExpiry(ttl),
                redsync.WithTries(1), // Don't retry, just skip if locked
            )

            if err := mutex.Lock(); err != nil {
                log.Printf("Could not acquire lock %s, skipping: %v", lockName, err)
                return
            }
            defer mutex.Unlock()

            job.Run()
        })
    }
}

func main() {
    // Set up Redis client
    client := redis.NewClient(&redis.Options{
        Addr: "localhost:6379",
    })
    pool := goredis.NewPool(client)
    rs := redsync.New(pool)

    // Create scheduler with distributed lock wrapper
    c := cron.New(cron.WithChain(
        cron.Recover(cron.DefaultLogger),
    ))

    // Add job with lock - only one instance runs it
    c.AddJob("0 9 * * *",
        cron.NewChain(
            WithRedisLock(rs, "daily-report-lock", 5*time.Minute),
        ).Then(cron.FuncJob(dailyReport)),
        cron.WithName("daily-report"),
    )

    c.Start()
    select {} // Run forever
}

func dailyReport() {
    log.Println("Running daily report (only on one instance)")
    time.Sleep(2 * time.Minute) // Simulate work
}
```

### Pattern 2: PostgreSQL Advisory Locks

Use PostgreSQL's built-in advisory locks for coordination without additional infrastructure.

```go
package main

import (
    "context"
    "database/sql"
    "hash/fnv"
    "log"

    _ "github.com/lib/pq"
    cron "github.com/netresearch/go-cron"
)

// WithPgAdvisoryLock creates a wrapper using PostgreSQL advisory locks
func WithPgAdvisoryLock(db *sql.DB, lockName string) cron.JobWrapper {
    // Convert lock name to int64 for pg_advisory_lock
    lockID := hashStringToInt64(lockName)

    return func(job cron.Job) cron.Job {
        return cron.FuncJob(func() {
            // Try to acquire lock (non-blocking)
            var acquired bool
            err := db.QueryRow("SELECT pg_try_advisory_lock($1)", lockID).Scan(&acquired)
            if err != nil {
                log.Printf("Error checking advisory lock: %v", err)
                return
            }

            if !acquired {
                log.Printf("Could not acquire advisory lock %s, skipping", lockName)
                return
            }

            // Ensure we release the lock when done
            defer func() {
                _, err := db.Exec("SELECT pg_advisory_unlock($1)", lockID)
                if err != nil {
                    log.Printf("Error releasing advisory lock: %v", err)
                }
            }()

            job.Run()
        })
    }
}

func hashStringToInt64(s string) int64 {
    h := fnv.New64a()
    h.Write([]byte(s))
    return int64(h.Sum64())
}

func main() {
    db, _ := sql.Open("postgres", "postgres://localhost/mydb?sslmode=disable")

    c := cron.New()

    c.AddJob("*/5 * * * *",
        cron.NewChain(
            WithPgAdvisoryLock(db, "sync-inventory"),
        ).Then(cron.FuncJob(syncInventory)),
        cron.WithName("sync-inventory"),
    )

    c.Start()
    select {}
}
```

### Pattern 3: etcd Distributed Lock

Use etcd for coordination in Kubernetes or cloud-native environments.

```go
package main

import (
    "context"
    "log"
    "time"

    clientv3 "go.etcd.io/etcd/client/v3"
    "go.etcd.io/etcd/client/v3/concurrency"
    cron "github.com/netresearch/go-cron"
)

// WithEtcdLock creates a wrapper using etcd distributed locks
func WithEtcdLock(client *clientv3.Client, lockName string, ttl int) cron.JobWrapper {
    return func(job cron.Job) cron.Job {
        return cron.FuncJob(func() {
            // Create a session with TTL
            session, err := concurrency.NewSession(client,
                concurrency.WithTTL(ttl),
            )
            if err != nil {
                log.Printf("Failed to create etcd session: %v", err)
                return
            }
            defer session.Close()

            // Try to acquire the lock
            mutex := concurrency.NewMutex(session, "/cron-locks/"+lockName)

            ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
            defer cancel()

            if err := mutex.TryLock(ctx); err != nil {
                log.Printf("Could not acquire etcd lock %s, skipping: %v", lockName, err)
                return
            }
            defer mutex.Unlock(context.Background())

            job.Run()
        })
    }
}

func main() {
    client, _ := clientv3.New(clientv3.Config{
        Endpoints:   []string{"localhost:2379"},
        DialTimeout: 5 * time.Second,
    })
    defer client.Close()

    c := cron.New()

    c.AddJob("0 */6 * * *",
        cron.NewChain(
            WithEtcdLock(client, "cleanup-job", 600), // 10 min TTL
        ).Then(cron.FuncJob(cleanupJob)),
        cron.WithName("cleanup-job"),
    )

    c.Start()
    select {}
}
```

### Pattern 4: File-Based Lock (Single Host)

For multiple processes on the same host (not distributed), use file locks:

```go
package main

import (
    "log"
    "os"
    "syscall"

    cron "github.com/netresearch/go-cron"
)

// WithFileLock creates a wrapper using filesystem locks (single host only)
func WithFileLock(lockPath string) cron.JobWrapper {
    return func(job cron.Job) cron.Job {
        return cron.FuncJob(func() {
            // Open or create lock file
            f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
            if err != nil {
                log.Printf("Failed to open lock file: %v", err)
                return
            }
            defer f.Close()

            // Try to acquire exclusive lock (non-blocking)
            err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
            if err != nil {
                log.Printf("Could not acquire file lock %s, skipping", lockPath)
                return
            }
            defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

            job.Run()
        })
    }
}

func main() {
    c := cron.New()

    c.AddJob("* * * * *",
        cron.NewChain(
            WithFileLock("/var/lock/my-cron-job.lock"),
        ).Then(cron.FuncJob(myJob)),
    )

    c.Start()
    select {}
}
```

## Combining with Other Wrappers

Chain the lock wrapper with other job wrappers:

```go
c.AddJob("0 9 * * *",
    cron.NewChain(
        cron.Recover(cron.DefaultLogger),           // 1. Recover from panics
        WithRedisLock(rs, "daily-report", 10*time.Minute), // 2. Acquire lock
        cron.SkipIfStillRunning(cron.DefaultLogger), // 3. Skip if already running locally
    ).Then(cron.FuncJob(dailyReport)),
    cron.WithName("daily-report"),
)
```

**Order matters!** The wrappers execute from outside in:
1. Recover catches any panic
2. Lock is acquired
3. Local skip check (prevents concurrent local runs)
4. Job runs

## Lock TTL Considerations

Set lock TTL appropriately:

| Scenario | TTL Recommendation |
|----------|-------------------|
| Quick job (<1 min) | 2-5 minutes |
| Medium job (1-10 min) | Job duration × 2 |
| Long job (>10 min) | Job duration + buffer, or use lock extension |
| Unknown duration | Use heartbeat/extension pattern |

### Lock Extension for Long Jobs

For jobs with unpredictable duration, extend the lock periodically:

```go
func WithRedisLockExtending(rs *redsync.Redsync, lockName string, ttl time.Duration) cron.JobWrapper {
    return func(job cron.Job) cron.Job {
        return cron.FuncJob(func() {
            mutex := rs.NewMutex(lockName, redsync.WithExpiry(ttl))

            if err := mutex.Lock(); err != nil {
                return
            }

            // Start extension goroutine
            done := make(chan struct{})
            go func() {
                ticker := time.NewTicker(ttl / 2)
                defer ticker.Stop()
                for {
                    select {
                    case <-done:
                        return
                    case <-ticker.C:
                        if ok, err := mutex.Extend(); !ok || err != nil {
                            log.Printf("Failed to extend lock: %v", err)
                        }
                    }
                }
            }()

            defer func() {
                close(done)
                mutex.Unlock()
            }()

            job.Run()
        })
    }
}
```

## Leader Election Alternative

Instead of locking each job, elect a single leader to run all cron jobs:

```go
package main

import (
    "context"
    "log"
    "time"

    clientv3 "go.etcd.io/etcd/client/v3"
    "go.etcd.io/etcd/client/v3/concurrency"
    cron "github.com/netresearch/go-cron"
)

func main() {
    client, _ := clientv3.New(clientv3.Config{
        Endpoints: []string{"localhost:2379"},
    })

    session, _ := concurrency.NewSession(client, concurrency.WithTTL(10))
    election := concurrency.NewElection(session, "/cron-leader")

    // Campaign for leadership
    ctx := context.Background()
    if err := election.Campaign(ctx, "instance-1"); err != nil {
        log.Fatal(err)
    }

    log.Println("I am the leader, starting cron scheduler")

    // Only the leader runs the scheduler
    c := cron.New()
    c.AddFunc("0 9 * * *", dailyReport)
    c.AddFunc("*/5 * * * *", syncData)
    c.Start()

    // Resign leadership on shutdown
    defer election.Resign(context.Background())

    select {} // Run forever
}
```

## Testing Distributed Locks

Test lock behavior with FakeClock:

```go
func TestDistributedLockSkipsWhenLocked(t *testing.T) {
    // Simulate a lock that's already held
    mockLock := &MockDistributedLock{
        locked: true, // Already locked by another instance
    }

    var jobRan bool
    wrapper := WithMockLock(mockLock)
    wrappedJob := wrapper(cron.FuncJob(func() {
        jobRan = true
    }))

    wrappedJob.Run()

    if jobRan {
        t.Error("Job should have been skipped when lock unavailable")
    }
}

func TestDistributedLockRunsWhenAvailable(t *testing.T) {
    mockLock := &MockDistributedLock{
        locked: false, // Lock available
    }

    var jobRan bool
    wrapper := WithMockLock(mockLock)
    wrappedJob := wrapper(cron.FuncJob(func() {
        jobRan = true
    }))

    wrappedJob.Run()

    if !jobRan {
        t.Error("Job should have run when lock available")
    }
    if !mockLock.wasAcquired {
        t.Error("Lock should have been acquired")
    }
    if !mockLock.wasReleased {
        t.Error("Lock should have been released")
    }
}
```

## When to Use Which Backend

| Backend | Best For | Pros | Cons |
|---------|----------|------|------|
| **Redis (redsync)** | Most deployments | Fast, well-tested, Redlock algorithm | Requires Redis |
| **PostgreSQL** | Already using Postgres | No extra infra | Single point of failure |
| **etcd** | Kubernetes environments | Built for coordination | More complex setup |
| **Consul** | HashiCorp stack | Service mesh integration | Another service to run |
| **File locks** | Single host, multi-process | Simple, no external deps | Not distributed |

## Alternatives for Heavy Coordination Needs

If you need sophisticated distributed scheduling with built-in coordination:

| Tool | Description |
|------|-------------|
| [Dkron](https://dkron.io/) | Distributed cron with Raft consensus |
| [Temporal](https://temporal.io/) | Workflow orchestration with durable execution |
| [Kubernetes CronJob](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/) | K8s-native scheduled jobs |

## See Also

- [PERSISTENCE_GUIDE.md](PERSISTENCE_GUIDE.md) - Persist job state across restarts
- [OPERATIONS.md](OPERATIONS.md) - Production deployment patterns
- [COOKBOOK.md](COOKBOOK.md) - Common integration recipes
- [ADR-020](adr/ADR-020-feature-scope-boundary.md) - Why distributed locking is out of scope
