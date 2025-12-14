# ADR-002: Panic-Based Job Failure Signaling

## Status
Accepted

## Date
2024-09-15

## Context

Jobs in a cron scheduler need a way to signal failure. The scheduler needs to:
1. Know when a job has failed
2. Optionally retry failed jobs
3. Apply circuit breaker patterns
4. Log failures for observability

The `Job` interface is intentionally minimal:

```go
type Job interface {
    Run()
}
```

Adding error returns would break this simplicity and require all job wrappers to handle errors explicitly.

**Constraints:**
- Must not change the existing `Job` interface (backward compatibility)
- Must work with the decorator/wrapper pattern
- Must enable retry and circuit breaker functionality

## Decision

Jobs signal transient failures by panicking. The panic is caught by wrapper chains (e.g., `Recover`, `RetryWithBackoff`, `CircuitBreaker`) which can then take appropriate action.

```go
// Job implementation
func (j *MyJob) Run() {
    if err := j.doWork(); err != nil {
        panic(err)  // Signals transient failure
    }
}

// Wrapper chain catches and handles
c := cron.New(cron.WithChain(
    cron.Recover(logger),           // Catches final panic, logs
    cron.RetryWithBackoff(logger, 3, ...), // Catches panic, retries
    cron.CircuitBreaker(logger, 5, ...),   // Tracks failures
))
```

**Error handling patterns:**
- **Transient failure (retry)**: `panic(err)` - triggers retry/circuit breaker
- **Expected failure (continue)**: Log and return - job completes, scheduler continues
- **Fatal failure (stop job)**: Return early - no retry, wait for next schedule

## Consequences

### Positive

- **Simple interface**: `Run()` has no return value, easy to implement
- **Composable wrappers**: Each wrapper can catch panics without changing signatures
- **Consistent behavior**: All failure handling flows through the same mechanism
- **Backward compatible**: Existing jobs without panic handling continue to work (with `Recover`)

### Negative

- **Unconventional**: Go idiom prefers explicit error returns
- **Stack traces**: Panics generate stack traces even for expected transient failures
- **Debugging confusion**: New users may not expect panic-based control flow
- **Performance**: Panic/recover has overhead compared to error returns

### Neutral

- **Documentation required**: Must clearly document the panic-based pattern
- **Testing**: Tests must use `recover()` or `Recover` wrapper to catch failures

## Alternatives Considered

### 1. Error Return Interface

```go
type Job interface {
    Run() error
}
```

- **Rejected**: Breaks backward compatibility
- All existing jobs would need modification
- All wrappers would need to handle both panic and error

### 2. Separate ErrorJob Interface

```go
type ErrorJob interface {
    Run() error
}
```

- **Rejected**: Two interfaces cause confusion
- Type assertions throughout codebase
- Wrappers must handle both types

### 3. Result Channel

```go
type Job interface {
    Run(result chan<- error)
}
```

- **Rejected**: Complicates simple use cases
- Channel management overhead
- Breaks existing interface

### 4. Context with Error

```go
type JobContext struct {
    Error error
}
func (j *Job) Run(ctx *JobContext) {
    ctx.Error = doWork()
}
```

- **Rejected**: Requires shared mutable state
- Thread-safety concerns
- Doesn't compose well with wrappers

## Usage Guidance

```go
// DO: Panic for transient failures that should trigger retry
func (j *APIJob) Run() {
    resp, err := http.Get(j.url)
    if err != nil {
        panic(fmt.Errorf("API call failed: %w", err))
    }
    // Process response...
}

// DO: Log and return for expected failures
func (j *CleanupJob) Run() {
    err := os.Remove(j.tempFile)
    if os.IsNotExist(err) {
        log.Println("File already removed, continuing")
        return  // Not a failure, just a no-op
    }
    if err != nil {
        panic(err)  // Unexpected failure
    }
}

// DON'T: Panic for non-transient failures
func (j *BadJob) Run() {
    panic("config is invalid")  // Won't help to retry!
}
```

## References

- Go error handling conventions: https://go.dev/blog/error-handling-and-go
- robfig/cron original design discussion
- Chain/middleware pattern in Go HTTP handlers
