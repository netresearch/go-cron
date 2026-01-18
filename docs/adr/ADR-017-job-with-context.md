# ADR-017: Optional JobWithContext Interface

## Status
Accepted

## Date
2025-12-14

## Context

Jobs need to support graceful shutdown. When `Stop()` is called, long-running jobs should:
- Receive a cancellation signal
- Clean up resources
- Exit promptly

**The core Job interface is minimal:**
```go
type Job interface {
    Run()
}
```

**Problem:** How to add context support without breaking existing code?

## Decision

Define an optional `JobWithContext` interface that jobs can implement:

```go
type JobWithContext interface {
    Job  // Embeds Run() for compatibility
    RunWithContext(ctx context.Context)
}

// Scheduler checks and uses appropriate method
func (c *Cron) startJob(job Job) {
    if jc, ok := job.(JobWithContext); ok {
        jc.RunWithContext(c.baseCtx)
    } else {
        job.Run()
    }
}
```

**Usage:**
```go
type MyJob struct{}

func (j *MyJob) Run() {
    j.RunWithContext(context.Background())
}

func (j *MyJob) RunWithContext(ctx context.Context) {
    select {
    case <-ctx.Done():
        log.Println("Job cancelled")
        return
    case <-time.After(time.Minute):
        // Do work
    }
}
```

## Consequences

### Positive

- **Backward compatible**: Existing jobs work unchanged
- **Opt-in complexity**: Simple jobs stay simple
- **Standard pattern**: Uses Go's context.Context
- **Graceful shutdown**: Jobs can respond to Stop()
- **Timeout support**: Context can carry deadlines

### Negative

- **Two interfaces**: Increases API surface
- **Runtime check**: Type assertion on every job run
- **Dual implementation**: Jobs implementing both must keep them in sync

### Neutral

- **Embedding pattern**: `Job` embedded in `JobWithContext`
- **Context passed by scheduler**: Job doesn't create its own

## Implementation Details

### Base Context

The Cron instance maintains a cancellable context:

```go
type Cron struct {
    baseCtx   context.Context
    cancelCtx context.CancelFunc
}

func New(opts ...Option) *Cron {
    c := &Cron{...}
    c.baseCtx, c.cancelCtx = context.WithCancel(context.Background())
    return c
}
```

### Stop Cancellation

```go
func (c *Cron) Stop() context.Context {
    c.cancelCtx()  // Cancel base context
    // ... wait for jobs
}
```

### Job Execution

```go
func (c *Cron) startJob(job Job) {
    c.jobWaiter.Add(1)
    go func() {
        defer c.jobWaiter.Done()

        if jc, ok := job.(JobWithContext); ok {
            jc.RunWithContext(c.baseCtx)
        } else {
            job.Run()
        }
    }()
}
```

## Alternatives Considered

### 1. Change Job Interface

```go
type Job interface {
    Run(ctx context.Context)
}
```

- **Rejected**: Breaking change for all users
- Every job must accept context even if unused

### 2. Wrapper Function

```go
c.AddFunc("@hourly", func(ctx context.Context) {
    // Job code
})
```

- **Rejected**: Changes AddFunc signature
- Breaking change

### 3. Context in Job Struct

```go
type Job interface {
    Run()
    SetContext(context.Context)
}
```

- **Rejected**: Stateful interface is error-prone
- Race conditions between Set and Run

### 4. Context via Middleware

```go
func WithContext(j Job) JobWithContext {
    return &contextJob{j, context.Background()}
}
```

- **Rejected**: Wrapper doesn't help job use context
- Job code can't access context

### 5. No Context Support

- **Rejected**: No graceful shutdown capability
- Jobs can't respond to cancellation

## Migration Path

Existing jobs continue to work:
```go
// Before: works
type OldJob struct{}
func (j *OldJob) Run() { ... }

// After: still works, plus context support
type NewJob struct{}
func (j *NewJob) Run() { j.RunWithContext(context.Background()) }
func (j *NewJob) RunWithContext(ctx context.Context) { ... }
```

## References

- Go context: https://pkg.go.dev/context
- Context best practices: https://go.dev/blog/context
