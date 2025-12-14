# ADR-005: Decorator Pattern for Job Wrappers

## Status
Accepted

## Date
2024-09-15

## Context

Jobs often need cross-cutting functionality:
- Panic recovery
- Overlap prevention (skip if still running)
- Timeout enforcement
- Retry with backoff
- Circuit breaker
- Logging/metrics

These concerns should be:
- **Reusable**: Apply the same logic to multiple jobs
- **Composable**: Combine multiple concerns in any order
- **Optional**: Only apply what's needed
- **Transparent**: Wrapped jobs look like regular jobs

**The Job interface must remain simple:**

```go
type Job interface {
    Run()
}
```

## Decision

Use the decorator (wrapper) pattern where each concern is a `JobWrapper` function that takes a `Job` and returns a wrapped `Job`:

```go
type JobWrapper func(Job) Job

// Example wrapper
func Recover(logger Logger) JobWrapper {
    return func(j Job) Job {
        return FuncJob(func() {
            defer func() {
                if r := recover(); r != nil {
                    logger.Error(nil, "job panicked", "panic", r)
                }
            }()
            j.Run()
        })
    }
}
```

**Chain composition:**

```go
type Chain struct {
    wrappers []JobWrapper
}

func NewChain(w ...JobWrapper) Chain {
    return Chain{wrappers: w}
}

func (c Chain) Then(j Job) Job {
    // Apply wrappers in reverse order (first wrapper is outermost)
    for i := len(c.wrappers) - 1; i >= 0; i-- {
        j = c.wrappers[i](j)
    }
    return j
}
```

**Wrapper order matters:**

```
Chain: Recover -> SkipIfStillRunning -> Timeout -> Job

Execution flow:
1. Recover.Run() starts
   2. SkipIfStillRunning.Run() checks running state
      3. Timeout.Run() sets timer
         4. Job.Run() executes
         4. (returns)
      3. (timeout check)
   2. (updates running state)
1. (recovers any panic)
```

## Consequences

### Positive

- **Composability**: Any combination of wrappers works
- **Separation of concerns**: Each wrapper does one thing
- **Testability**: Wrappers can be tested in isolation
- **Flexibility**: Apply different chains to different jobs
- **No interface changes**: Job interface stays simple
- **Order control**: First wrapper in chain is outermost

### Negative

- **Wrapper order matters**: Incorrect order causes subtle bugs
- **Stack depth**: Deep wrapper chains increase stack size
- **Debugging**: Stack traces include all wrapper layers
- **Runtime overhead**: Each wrapper adds function call overhead

### Neutral

- **Learning curve**: Developers must understand wrapper ordering
- **Documentation**: Must clearly explain correct wrapper order

## Alternatives Considered

### 1. Configuration-Based Concerns

```go
type JobConfig struct {
    Recover       bool
    Timeout       time.Duration
    SkipOverlap   bool
    RetryAttempts int
}

c.AddJobWithConfig("@hourly", job, JobConfig{
    Recover: true,
    Timeout: 5 * time.Minute,
})
```

- **Rejected**: Inflexible - can't add new concerns without changing struct
- Fixed ordering - can't control wrapper order
- Complex conditional logic in scheduler

### 2. Aspect-Oriented Programming

```go
type Job interface {
    Run()
    BeforeRun() error
    AfterRun(err error)
    OnPanic(r any)
}
```

- **Rejected**: Complicates Job interface
- Not idiomatic Go
- Forces all jobs to implement hooks

### 3. Event-Based Hooks

```go
c.OnBeforeJob(func(j Job) { /* before */ })
c.OnAfterJob(func(j Job, err error) { /* after */ })
```

- **Rejected**: Global hooks affect all jobs
- Can't apply different logic to different jobs
- Ordering is implicit

### 4. Inheritance (Embedding)

```go
type RecoverableJob struct {
    Job
}

func (j *RecoverableJob) Run() {
    defer recover()
    j.Job.Run()
}
```

- **Rejected**: Requires new types for each combination
- Combinatorial explosion: RecoverableTimeoutJob, RecoverableSkipJob, etc.
- Not composable at runtime

## Wrapper Ordering Guide

**Recommended order (outermost to innermost):**

```go
cron.WithChain(
    cron.Recover(logger),              // 1. Always outermost - catches everything
    cron.RetryWithBackoff(logger, ...), // 2. Retry before circuit breaker
    cron.CircuitBreaker(logger, ...),   // 3. Track consecutive failures
    cron.Timeout(logger, 5*time.Minute), // 4. Limit execution time
    cron.SkipIfStillRunning(logger),    // 5. Innermost - prevent overlap
)
```

**Why this order?**

1. **Recover**: Must be outermost to catch panics from any inner wrapper
2. **RetryWithBackoff**: Retries should be counted before circuit opens
3. **CircuitBreaker**: Opens based on failures, including exhausted retries
4. **Timeout**: Applies to each attempt individually
5. **SkipIfStillRunning**: Innermost because overlap check is per-execution

**Common mistakes:**

```go
// BAD: Retry inside Recover loses panic information for retries
cron.NewChain(cron.RetryWithBackoff(...), cron.Recover(logger))

// BAD: Timeout outside SkipIfStillRunning - timeout applies to skip check
cron.NewChain(cron.SkipIfStillRunning(logger), cron.Timeout(logger, ...))
```

## References

- Gang of Four Decorator pattern
- Go HTTP middleware patterns (net/http, chi, echo)
- gRPC interceptors
