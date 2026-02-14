# Workflow/DAG Job Dependencies

**Issue:** [#312](https://github.com/netresearch/go-cron/issues/312)
**Date:** 2026-02-14
**Status:** Design approved

## Problem

Users need to express job dependencies: "run job B after job A succeeds," "run cleanup regardless of outcome," "skip downstream jobs when a parent fails." Today, go-cron treats every entry as independent. Coordinating dependent jobs requires external orchestration code.

Primary demand comes from [netresearch/ofelia](https://github.com/netresearch/ofelia), which needs multi-step Docker workflows with conditional execution.

## Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Integration | Built into core `Cron` struct | Avoids separate coordinator; reuses existing run loop, entry management, and observability |
| Execution model | Execution-tracked (UUID per workflow run) | Supports concurrent workflow instances; each run tracks its own job results |
| API style | Both imperative + declarative | Imperative for programmatic wiring; declarative builder for ergonomic multi-step workflows |
| Error detection | Allow both `Job` and `ErrorJob` | Plain `Job` completing = success, panic = failure. `ErrorJob.RunE()` returning error = failure. Silent failures documented as limitation. |
| Trigger conditions | 4 per-dependency edges | OnSuccess, OnFailure, OnSkipped, OnComplete cover all real-world patterns |
| Concurrency model | Channel-based, no mutexes on workflow state | All DAG state managed exclusively in `run()` goroutine, matching existing architecture |

## Core Types

### TriggerCondition

```go
type TriggerCondition int

const (
    // OnSuccess triggers when the parent job completes without error.
    OnSuccess TriggerCondition = iota
    // OnFailure triggers when the parent job fails (error or panic).
    OnFailure
    // OnSkipped triggers when the parent job was skipped
    // (its own trigger condition was not met).
    OnSkipped
    // OnComplete triggers after the parent job resolves to any terminal state
    // (success, failure, or skipped). Use for cleanup/finalization steps.
    OnComplete
)
```

### JobResult

```go
type JobResult int

const (
    // ResultPending means the job has not yet been evaluated in this execution.
    ResultPending JobResult = iota
    // ResultSuccess means the job completed without error.
    ResultSuccess
    // ResultFailure means the job returned an error or panicked.
    ResultFailure
    // ResultSkipped means the job's trigger condition was not met.
    ResultSkipped
)
```

### Dependency

```go
// Dependency represents a directed edge in the workflow DAG.
// The child job waits for the parent to resolve before evaluating
// its trigger condition.
type Dependency struct {
    ParentID  EntryID
    Condition TriggerCondition
}
```

Stored on `Entry` as `Dependencies []Dependency`. Reverse index (`parentToChildren map[EntryID][]EntryID`) maintained on `Cron` for O(1) child lookup.

### WorkflowExecution

```go
// WorkflowExecution tracks the state of a single workflow run.
// All fields are owned exclusively by the run() goroutine — no mutex needed.
type WorkflowExecution struct {
    ID        string              // UUID
    RootID    EntryID             // The cron-triggered entry that started this
    StartTime time.Time
    Results   map[EntryID]JobResult
}
```

**No mutex.** All reads and writes happen in the `run()` goroutine. External queries go through channels (same pattern as `Entries()` snapshots).

**Retention:** Completed executions are cleaned up via a configurable retention policy:

```go
cron.New(
    cron.WithWorkflowRetention(100), // keep last 100 executions (default)
)
```

Count-based: when a new execution completes and the count exceeds the limit, the oldest completed execution is removed. Time-based retention (`WithWorkflowRetentionTTL`) can be added later if needed.

## Imperative API

All methods route through the run loop channel for thread safety.

```go
// AddDependency adds a dependency edge: child waits for parent.
// Returns error if either entry doesn't exist or if the edge would create a cycle.
// Cycle detection uses DFS at add time.
func (c *Cron) AddDependency(child, parent EntryID, condition TriggerCondition) error

// AddDependencyByName is the name-based variant.
func (c *Cron) AddDependencyByName(child, parent string, condition TriggerCondition) error

// RemoveDependency removes a dependency edge.
// Safe to call even if no such edge exists (no-op).
// Returns error if called during an active execution involving these entries.
func (c *Cron) RemoveDependency(child, parent EntryID) error

// RemoveDependencyByName is the name-based variant.
func (c *Cron) RemoveDependencyByName(child, parent string) error

// Dependencies returns the dependency edges for an entry.
func (c *Cron) Dependencies(id EntryID) []Dependency

// DependenciesByName is the name-based variant.
func (c *Cron) DependenciesByName(name string) []Dependency
```

### Cycle Detection

DFS from child through parent edges at `AddDependency` time. If the traversal reaches the child again, return `ErrCycleDetected`. No runtime re-check needed since edges are immutable during execution.

```go
var ErrCycleDetected = errors.New("cron: dependency would create a cycle")
```

## Declarative API (Workflow Builder)

```go
// Workflow groups related steps into a DAG registered atomically.
type Workflow struct {
    Name  string
    steps []*WorkflowStep
}

// WorkflowStep represents a named job within a workflow.
type WorkflowStep struct {
    name    string
    spec    string
    job     Job
    deps    []stepDep       // parent name + condition
    isFinal bool
}

type stepDep struct {
    parentName string
    condition  TriggerCondition
}
```

### Builder API

```go
// NewWorkflow creates a new workflow builder.
func NewWorkflow(name string) *Workflow

// Step adds a named step with a cron spec and job.
// The first step added with no After() calls becomes a root.
// Root steps use spec for cron scheduling; non-root steps use @triggered.
func (w *Workflow) Step(name, spec string, job Job) *WorkflowStep

// StepFunc is a convenience that wraps a func() as a FuncJob.
func (w *Workflow) StepFunc(name, spec string, fn func()) *WorkflowStep

// After declares that this step depends on the named parent step
// with the given trigger condition.
func (s *WorkflowStep) After(parentName string, condition TriggerCondition) *WorkflowStep

// Final marks this step as a finalization step.
// Equivalent to adding After(parent, OnComplete) for every non-final step.
// A workflow may have at most one Final step.
func (s *WorkflowStep) Final() *WorkflowStep
```

### Registration

```go
// AddWorkflow validates and registers all steps atomically.
// 1. Validates all specs parse correctly.
// 2. Validates no duplicate step names (within workflow or existing entries).
// 3. Validates DAG is acyclic (DFS).
// 4. Registers all entries via AddJob with @triggered schedule for non-root steps.
// 5. Wires all dependency edges.
// If any validation fails, no entries are registered (all-or-nothing).
func (c *Cron) AddWorkflow(w *Workflow) error
```

### Example

```go
wf := cron.NewWorkflow("etl-pipeline")

extract := wf.Step("extract", "0 2 * * *", extractJob)
transform := wf.Step("transform", "@triggered", transformJob).
    After("extract", cron.OnSuccess)
load := wf.Step("load", "@triggered", loadJob).
    After("transform", cron.OnSuccess)
cleanup := wf.Step("cleanup", "@triggered", cleanupJob).
    Final()

err := c.AddWorkflow(wf)
```

This registers 4 entries with edges:
- extract (cron-scheduled at 2 AM)
- transform (triggered on extract success)
- load (triggered on transform success)
- cleanup (triggered OnComplete from extract, transform, and load)

## Execution Engine

### Job Completion Channel

The critical architectural addition: a channel for `startJob` to notify the run loop when jobs finish.

```go
// In Cron struct
type jobDoneEvent struct {
    EntryID     EntryID
    Err         error    // non-nil if ErrorJob returned error
    Panicked    bool     // true if job panicked (recovered)
    PanicValue  any      // the recovered value, if panicked
    ExecutionID string   // workflow execution ID, empty if standalone
}

// New channel on Cron
jobDone chan jobDoneEvent
```

`startJob` modifications:
1. Accept an optional `executionID string` parameter.
2. After job completes, determine result (success/failure) from ErrorJob error or panic recovery.
3. Send `jobDoneEvent` to `c.jobDone`.
4. The existing re-panic behavior is preserved for non-workflow jobs.

The `run()` loop gains a new case:

```go
case event := <-c.jobDone:
    if event.ExecutionID != "" {
        c.processWorkflowEvent(event)
    }
```

### Execution Flow

1. **Timer fires** for a root entry that has children in `parentToChildren`.
2. `processDueEntries` detects the entry has dependents → creates `WorkflowExecution` with UUID.
3. Root job starts via `startJob` with the execution ID injected into context.
4. Root job completes → `jobDoneEvent` arrives on `c.jobDone`.
5. `processWorkflowEvent`:
   a. Record result in `execution.Results[entryID]`.
   b. For each child of this entry (via `parentToChildren`):
      - Check if ALL parents are resolved (not `ResultPending`).
      - Evaluate trigger conditions against parent results.
      - If all conditions met → trigger child via `TriggerEntry` with execution context.
      - If any condition not met → mark child `ResultSkipped` → recurse for its children.
6. When all entries in the execution are resolved, mark execution complete and apply retention.

### Context Propagation

Each job in a workflow receives the execution ID via context:

```go
type workflowContextKey struct{}

// WorkflowExecutionID returns the workflow execution ID from the context,
// or empty string if the job is not part of a workflow.
func WorkflowExecutionID(ctx context.Context) string {
    if id, ok := ctx.Value(workflowContextKey{}).(string); ok {
        return id
    }
    return ""
}
```

`startJob` creates a derived context overlaying the entry's context:

```go
runCtx := context.WithValue(entryCtx, workflowContextKey{}, executionID)
```

This means a job can be both cron-scheduled independently AND participate in workflows. Each invocation gets its own execution context.

### Error Detection

| Job Type | Success | Failure |
|---|---|---|
| `ErrorJob` (implements `RunE() error`) | `RunE()` returns nil | `RunE()` returns non-nil error |
| Plain `Job` | `Run()` returns normally | `Run()` panics |
| `JobWithContext` + `ErrorJob` | `RunWithContext()` returns nil | Returns error or panics |

For workflow jobs, panics are recovered in `startJob`, recorded as failure in the workflow execution, and **not re-panicked** (the Recover wrapper handles logging). For standalone jobs, existing behavior is preserved.

### Cascade Skip Rules

When a parent resolves, each child evaluates its trigger conditions:

| Parent Result | OnSuccess child | OnFailure child | OnSkipped child | OnComplete child |
|---|---|---|---|---|
| Success | Fires | Skipped | Skipped | Fires |
| Failure | Skipped | Fires | Skipped | Fires |
| Skipped | Skipped | Skipped | Fires | Fires |

For multi-parent children (AND semantics): ALL parent conditions must be satisfiable. If any parent's result doesn't match the condition for that edge, the child is skipped.

Skipping cascades: when a child is marked `ResultSkipped`, its own children are evaluated with the same rules. `OnSkipped` and `OnComplete` children of a skipped parent fire; `OnSuccess` and `OnFailure` children are skipped.

### Interaction with Existing Features

| Feature | Interaction |
|---|---|
| `SkipIfStillRunning` | Works per-entry. If a workflow child is still running from a previous execution, the new trigger is skipped per normal semantics. |
| `DelayIfStillRunning` | Works per-entry. The trigger queues until the previous run finishes. |
| `WaitForJob` | Works per-entry. Waits for the specific entry's current invocation to finish, regardless of workflow. |
| `PauseEntry` | A paused child is treated as `ResultSkipped` in the workflow execution. |
| `IsJobRunning` | Reports per-entry running status, unaware of workflow context. |
| `RemoveEntry` | Removing an entry that participates in a workflow removes its edges. Active executions involving the entry mark it `ResultSkipped`. |
| `ObservabilityHooks` | `OnJobStart`/`OnJobComplete` fire for each workflow step individually. A new `OnWorkflowComplete` hook fires when all entries in an execution are resolved. |

## Observability

```go
// New hook
type ObservabilityHooks struct {
    // ... existing hooks ...

    // OnWorkflowComplete is called when all jobs in a workflow execution
    // have resolved (success, failure, or skipped).
    OnWorkflowComplete func(executionID string, rootID EntryID, results map[EntryID]JobResult)
}
```

### Query API

```go
// WorkflowStatus returns the current state of a workflow execution.
// Returns nil if the execution ID is not found.
// Routed through run loop channel for thread safety.
func (c *Cron) WorkflowStatus(executionID string) *WorkflowExecution

// ActiveWorkflows returns all currently active (not fully resolved) executions.
func (c *Cron) ActiveWorkflows() []WorkflowExecution
```

## Testing Strategy

1. **Unit tests** for cycle detection (DFS), trigger condition evaluation, cascade skip logic
2. **Integration tests** for end-to-end workflow execution with mock clock
3. **Concurrency tests** for concurrent workflow executions, `AddDependency` during execution
4. **Edge cases**: root job in multiple workflows, paused children, removed entries mid-execution, empty workflows, single-step workflows
5. **Existing test compatibility**: all existing tests must pass unchanged

## Out of Scope

- **Timeout per workflow execution** — use existing `Timeout`/`TimeoutWithContext` per-entry wrappers
- **Retry per workflow step** — use existing `RetryOnError`/`RetryWithBackoff` wrappers
- **Workflow persistence/recovery** — in-memory only; restart loses active executions
- **Dynamic DAG modification during execution** — edges are immutable during an active execution
- **OR semantics for multi-parent** — only AND (all parents must resolve); OR can be added later
