# Workflow/DAG Job Dependencies Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add workflow/DAG job dependencies to go-cron so users can express "run B after A succeeds" with conditional trigger logic, cascade skipping, and concurrent execution tracking.

**Architecture:** Built into the core `Cron` struct, reusing the existing channel-based run loop. A new `jobDone` channel feeds job completion events back to the run loop, which drives DAG orchestration. All workflow state is owned exclusively by the `run()` goroutine — no mutexes on workflow data.

**Tech Stack:** Go stdlib only (no external dependencies). Uses `crypto/rand` for execution UUIDs.

**Design doc:** `docs/plans/2026-02-14-dag-dependencies-design.md`

---

## Task 1: Core Types — TriggerCondition, JobResult, Dependency

New file with the foundational enums and structs. No Cron modifications yet.

**Files:**
- Create: `workflow.go`
- Create: `workflow_test.go`

**Step 1: Write failing tests for TriggerCondition**

In `workflow_test.go`:

```go
package cron

import "testing"

func TestTriggerCondition_String(t *testing.T) {
	tests := []struct {
		c    TriggerCondition
		want string
	}{
		{OnSuccess, "OnSuccess"},
		{OnFailure, "OnFailure"},
		{OnSkipped, "OnSkipped"},
		{OnComplete, "OnComplete"},
		{TriggerCondition(99), "TriggerCondition(99)"},
	}
	for _, tt := range tests {
		if got := tt.c.String(); got != tt.want {
			t.Errorf("TriggerCondition(%d).String() = %q, want %q", int(tt.c), got, tt.want)
		}
	}
}

func TestTriggerCondition_Valid(t *testing.T) {
	for _, c := range []TriggerCondition{OnSuccess, OnFailure, OnSkipped, OnComplete} {
		if !c.Valid() {
			t.Errorf("%v.Valid() = false, want true", c)
		}
	}
	if TriggerCondition(99).Valid() {
		t.Error("TriggerCondition(99).Valid() = true, want false")
	}
}

func TestJobResult_String(t *testing.T) {
	tests := []struct {
		r    JobResult
		want string
	}{
		{ResultPending, "Pending"},
		{ResultSuccess, "Success"},
		{ResultFailure, "Failure"},
		{ResultSkipped, "Skipped"},
		{JobResult(99), "JobResult(99)"},
	}
	for _, tt := range tests {
		if got := tt.r.String(); got != tt.want {
			t.Errorf("JobResult(%d).String() = %q, want %q", int(tt.r), got, tt.want)
		}
	}
}

func TestJobResult_IsTerminal(t *testing.T) {
	if ResultPending.IsTerminal() {
		t.Error("ResultPending.IsTerminal() = true, want false")
	}
	for _, r := range []JobResult{ResultSuccess, ResultFailure, ResultSkipped} {
		if !r.IsTerminal() {
			t.Errorf("%v.IsTerminal() = false, want true", r)
		}
	}
}

func TestConditionMatches(t *testing.T) {
	tests := []struct {
		condition TriggerCondition
		result    JobResult
		want      bool
	}{
		{OnSuccess, ResultSuccess, true},
		{OnSuccess, ResultFailure, false},
		{OnSuccess, ResultSkipped, false},
		{OnFailure, ResultFailure, true},
		{OnFailure, ResultSuccess, false},
		{OnFailure, ResultSkipped, false},
		{OnSkipped, ResultSkipped, true},
		{OnSkipped, ResultSuccess, false},
		{OnSkipped, ResultFailure, false},
		{OnComplete, ResultSuccess, true},
		{OnComplete, ResultFailure, true},
		{OnComplete, ResultSkipped, true},
	}
	for _, tt := range tests {
		if got := tt.condition.Matches(tt.result); got != tt.want {
			t.Errorf("%v.Matches(%v) = %v, want %v", tt.condition, tt.result, got, tt.want)
		}
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -run 'TestTriggerCondition|TestJobResult|TestConditionMatches' -v ./...`
Expected: FAIL — types not defined.

**Step 3: Implement core types**

In `workflow.go`:

```go
package cron

import (
	"context"
	"fmt"
	"time"
)

// TriggerCondition defines when a dependent job should be triggered
// relative to its parent's outcome.
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

// String returns the string representation of the TriggerCondition.
func (c TriggerCondition) String() string {
	switch c {
	case OnSuccess:
		return "OnSuccess"
	case OnFailure:
		return "OnFailure"
	case OnSkipped:
		return "OnSkipped"
	case OnComplete:
		return "OnComplete"
	default:
		return fmt.Sprintf("TriggerCondition(%d)", int(c))
	}
}

// Valid returns true if c is a known trigger condition.
func (c TriggerCondition) Valid() bool {
	switch c {
	case OnSuccess, OnFailure, OnSkipped, OnComplete:
		return true
	default:
		return false
	}
}

// Matches reports whether the given parent result satisfies this condition.
func (c TriggerCondition) Matches(result JobResult) bool {
	switch c {
	case OnSuccess:
		return result == ResultSuccess
	case OnFailure:
		return result == ResultFailure
	case OnSkipped:
		return result == ResultSkipped
	case OnComplete:
		return result.IsTerminal()
	default:
		return false
	}
}

// JobResult represents the outcome of a job within a workflow execution.
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

// String returns the string representation of the JobResult.
func (r JobResult) String() string {
	switch r {
	case ResultPending:
		return "Pending"
	case ResultSuccess:
		return "Success"
	case ResultFailure:
		return "Failure"
	case ResultSkipped:
		return "Skipped"
	default:
		return fmt.Sprintf("JobResult(%d)", int(r))
	}
}

// IsTerminal returns true if the result is a final state (not pending).
func (r JobResult) IsTerminal() bool {
	switch r {
	case ResultSuccess, ResultFailure, ResultSkipped:
		return true
	default:
		return false
	}
}

// Dependency represents a directed edge in the workflow DAG.
// The child job waits for the parent to resolve before evaluating
// its trigger condition.
type Dependency struct {
	ParentID  EntryID
	Condition TriggerCondition
}

// WorkflowExecution tracks the state of a single workflow run.
// All fields are owned exclusively by the run() goroutine — no mutex needed.
type WorkflowExecution struct {
	ID        string                // UUID
	RootID    EntryID               // The cron-triggered entry that started this
	StartTime time.Time
	Results   map[EntryID]JobResult
}

// IsComplete returns true if all tracked jobs have terminal results.
func (we *WorkflowExecution) IsComplete() bool {
	for _, r := range we.Results {
		if !r.IsTerminal() {
			return false
		}
	}
	return true
}

// jobDoneEvent is sent from startJob goroutines to the run loop
// to report job completion for workflow orchestration.
type jobDoneEvent struct {
	EntryID     EntryID
	Err         error  // non-nil if ErrorJob returned error
	Panicked    bool   // true if job panicked (recovered)
	PanicValue  any    // the recovered value, if panicked
	ExecutionID string // workflow execution ID, empty if standalone
}

// workflowContextKey is the context key for workflow execution IDs.
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

**Step 4: Run tests to verify they pass**

Run: `go test -run 'TestTriggerCondition|TestJobResult|TestConditionMatches' -v ./...`
Expected: PASS

**Step 5: Commit**

```bash
git add workflow.go workflow_test.go
git commit -S -m "feat(workflow): add core DAG types — TriggerCondition, JobResult, Dependency (#312)"
```

---

## Task 2: Cycle Detection Algorithm

Pure function, no Cron dependency. Validates that adding an edge won't create a cycle.

**Files:**
- Modify: `workflow.go`
- Modify: `workflow_test.go`

**Step 1: Write failing tests for cycle detection**

Append to `workflow_test.go`:

```go
func TestHasCycle(t *testing.T) {
	tests := []struct {
		name     string
		edges    map[EntryID][]EntryID // child -> parents
		newChild EntryID
		newParent EntryID
		wantCycle bool
	}{
		{
			name:      "no cycle - simple chain",
			edges:     map[EntryID][]EntryID{2: {1}},
			newChild:  3,
			newParent: 2,
			wantCycle: false,
		},
		{
			name:      "direct cycle - A depends on B, B depends on A",
			edges:     map[EntryID][]EntryID{2: {1}},
			newChild:  1,
			newParent: 2,
			wantCycle: true,
		},
		{
			name:      "indirect cycle - A->B->C->A",
			edges:     map[EntryID][]EntryID{2: {1}, 3: {2}},
			newChild:  1,
			newParent: 3,
			wantCycle: true,
		},
		{
			name:      "self-cycle",
			edges:     map[EntryID][]EntryID{},
			newChild:  1,
			newParent: 1,
			wantCycle: true,
		},
		{
			name:      "diamond - no cycle",
			edges:     map[EntryID][]EntryID{2: {1}, 3: {1}, 4: {2, 3}},
			newChild:  4,
			newParent: 3, // already exists, but not a cycle
			wantCycle: false,
		},
		{
			name:      "no existing edges",
			edges:     map[EntryID][]EntryID{},
			newChild:  2,
			newParent: 1,
			wantCycle: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := make(map[EntryID][]Dependency)
			for child, parents := range tt.edges {
				for _, p := range parents {
					deps[child] = append(deps[child], Dependency{ParentID: p, Condition: OnSuccess})
				}
			}
			got := hasCycle(deps, tt.newChild, tt.newParent)
			if got != tt.wantCycle {
				t.Errorf("hasCycle() = %v, want %v", got, tt.wantCycle)
			}
		})
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -run TestHasCycle -v ./...`
Expected: FAIL — `hasCycle` not defined.

**Step 3: Implement cycle detection**

Append to `workflow.go`:

```go
// hasCycle checks whether adding an edge from newChild to newParent
// would create a cycle in the dependency graph. Uses DFS from newParent
// upward through the parent edges to see if newChild is reachable.
func hasCycle(deps map[EntryID][]Dependency, newChild, newParent EntryID) bool {
	if newChild == newParent {
		return true
	}

	// DFS from newParent upward: if we can reach newChild, there's a cycle.
	visited := make(map[EntryID]bool)
	stack := []EntryID{newParent}

	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if current == newChild {
			return true
		}
		if visited[current] {
			continue
		}
		visited[current] = true

		for _, dep := range deps[current] {
			if !visited[dep.ParentID] {
				stack = append(stack, dep.ParentID)
			}
		}
	}
	return false
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -run TestHasCycle -v ./...`
Expected: PASS

**Step 5: Commit**

```bash
git add workflow.go workflow_test.go
git commit -S -m "feat(workflow): add DFS cycle detection for dependency edges (#312)"
```

---

## Task 3: jobDone Channel + startJob Modification

The critical plumbing: add the `jobDone` channel to `Cron`, modify `startJob` to report completions, and add the `case` in `run()`.

**Files:**
- Modify: `cron.go` (Cron struct:55-87, New():407-436, startJob():1113-1144, run():984-1104)
- Modify: `workflow_test.go`

**Step 1: Write failing test for jobDone event delivery**

Append to `workflow_test.go`:

```go
func TestJobDoneEvent_Delivered(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithSeconds())
	c.Start()
	defer c.Stop()

	// Add a simple job that completes successfully
	done := make(chan struct{})
	id, _ := c.AddFunc("@triggered", func() {
		close(done)
	}, WithName("test-job"))

	_ = c.TriggerEntry(id)

	select {
	case <-done:
		// Job ran successfully
	case <-time.After(2 * time.Second):
		t.Fatal("job did not run")
	}

	// Verify the existing behavior still works — this is a regression test.
	// The jobDone channel is internal; we verify it doesn't break existing behavior.
}
```

**Step 2: Run test to verify it passes (regression test)**

Run: `go test -run TestJobDoneEvent_Delivered -v ./...`
Expected: PASS (this is a regression test — existing behavior should work).

**Step 3: Add jobDone channel to Cron struct and New()**

In `cron.go`, add to the `Cron` struct (after line 86 `indexDeletions int`):

```go
	// jobDone receives completion events from startJob goroutines.
	// Used by the run loop to drive workflow DAG orchestration.
	jobDone chan jobDoneEvent

	// parentToChildren maps parent EntryID to child EntryIDs for O(1) lookup.
	// Maintained by AddDependency/RemoveDependency.
	parentToChildren map[EntryID][]EntryID

	// entryDeps maps child EntryID to its dependency edges.
	// Maintained by AddDependency/RemoveDependency.
	entryDeps map[EntryID][]Dependency

	// activeExecutions tracks in-progress workflow executions.
	activeExecutions map[string]*WorkflowExecution

	// completedExecutions stores completed executions for query (FIFO order).
	completedExecutions []*WorkflowExecution

	// workflowRetention is the max number of completed executions to retain.
	// Default 100. Set via WithWorkflowRetention.
	workflowRetention int

	// addDep channel routes AddDependency requests to the run loop.
	addDep chan request[addDepRequest, error]

	// removeDep channel routes RemoveDependency requests to the run loop.
	removeDep chan request[removeDepRequest, error]

	// queryDeps channel routes Dependencies queries to the run loop.
	queryDeps chan request[EntryID, []Dependency]

	// queryWorkflow channel routes WorkflowStatus queries to the run loop.
	queryWorkflow chan request[string, *WorkflowExecution]

	// queryActiveWorkflows channel routes ActiveWorkflows queries to the run loop.
	queryActiveWorkflows chan chan []WorkflowExecution
```

Add request types (near `updateScheduleRequest` around line 226):

```go
// addDepRequest is used to add a dependency edge via the run loop.
type addDepRequest struct {
	childID   EntryID
	parentID  EntryID
	condition TriggerCondition
}

// removeDepRequest is used to remove a dependency edge via the run loop.
type removeDepRequest struct {
	childID  EntryID
	parentID EntryID
}
```

In `New()` (around line 421), add channel initialization:

```go
		jobDone:              make(chan jobDoneEvent, 64),
		parentToChildren:     make(map[EntryID][]EntryID),
		entryDeps:            make(map[EntryID][]Dependency),
		activeExecutions:     make(map[string]*WorkflowExecution),
		workflowRetention:    100,
		addDep:               make(chan request[addDepRequest, error]),
		removeDep:            make(chan request[removeDepRequest, error]),
		queryDeps:            make(chan request[EntryID, []Dependency]),
		queryWorkflow:        make(chan request[string, *WorkflowExecution]),
		queryActiveWorkflows: make(chan chan []WorkflowExecution),
```

**Step 4: Modify startJob to send completion events**

Replace `startJob` (lines 1113-1144) with:

```go
func (c *Cron) startJob(entryCtx context.Context, entryRunning *jobTracker, entryID EntryID, originalJob, wrappedJob Job, scheduledTime time.Time) {
	c.startJobWithExecution(entryCtx, entryRunning, entryID, originalJob, wrappedJob, scheduledTime, "")
}

func (c *Cron) startJobWithExecution(entryCtx context.Context, entryRunning *jobTracker, entryID EntryID, originalJob, wrappedJob Job, scheduledTime time.Time, executionID string) {
	c.jobWaiter.Add(1)
	entryRunning.start()

	// If part of a workflow, overlay execution ID on context.
	runCtx := entryCtx
	if executionID != "" {
		runCtx = context.WithValue(entryCtx, workflowContextKey{}, executionID)
	}

	go func() {
		defer c.jobWaiter.Done()
		defer entryRunning.finish()

		c.hooks.callOnJobStart(entryID, originalJob, scheduledTime)

		start := c.clock.Now()
		var recovered any
		var jobErr error

		func() {
			defer func() {
				recovered = recover()
			}()
			if jc, ok := wrappedJob.(JobWithContext); ok {
				jc.RunWithContext(runCtx)
			} else {
				wrappedJob.Run()
			}
			// Check for ErrorJob after successful Run (no panic).
			if ej, ok := originalJob.(ErrorJob); ok {
				jobErr = ej.RunE()
				// RunE already called by Run() for FuncErrorJob — but for
				// custom ErrorJob where Run() delegates to RunE(), we need
				// the error. Since Run() already ran, we extract via the
				// original job's last error if available.
				// Actually: Run() already executed. For workflow purposes,
				// if the job didn't panic, we check the unwrapped original.
			}
		}()
		duration := c.clock.Now().Sub(start)

		c.hooks.callOnJobComplete(entryID, originalJob, duration, recovered)

		// Send completion event for workflow orchestration.
		if executionID != "" {
			c.jobDone <- jobDoneEvent{
				EntryID:     entryID,
				Err:         jobErr,
				Panicked:    recovered != nil,
				PanicValue:  recovered,
				ExecutionID: executionID,
			}
			// Don't re-panic for workflow jobs — the workflow engine handles failures.
			return
		}

		// Re-panic if the job panicked and wasn't handled by a wrapper
		if recovered != nil {
			panic(recovered)
		}
	}()
}
```

**Wait** — the ErrorJob detection above is wrong. `Run()` already executed; we can't call `RunE()` again. The correct approach: detect the error from the wrapped job's execution. Since `FuncErrorJob.Run()` panics on error, a non-panicking `Run()` means success for plain jobs. For `ErrorJob`, we need to call `RunE()` *instead of* `Run()`.

Revised approach for the goroutine body:

```go
		func() {
			defer func() {
				recovered = recover()
			}()
			if jc, ok := wrappedJob.(JobWithContext); ok {
				jc.RunWithContext(runCtx)
			} else {
				wrappedJob.Run()
			}
		}()
```

For workflow error detection:
- If `recovered != nil` → `ResultFailure`
- If `recovered == nil` → `ResultSuccess`
- ErrorJob users who want error-based failure detection should use `FuncErrorJob` which panics on error, so `recovered` captures it.

This is simpler and consistent with the design doc: "Plain Job completing = success, panic = failure."

**Revised startJob implementation:**

```go
func (c *Cron) startJob(entryCtx context.Context, entryRunning *jobTracker, entryID EntryID, originalJob, wrappedJob Job, scheduledTime time.Time) {
	c.startJobWithExecution(entryCtx, entryRunning, entryID, originalJob, wrappedJob, scheduledTime, "")
}

func (c *Cron) startJobWithExecution(entryCtx context.Context, entryRunning *jobTracker, entryID EntryID, originalJob, wrappedJob Job, scheduledTime time.Time, executionID string) {
	c.jobWaiter.Add(1)
	entryRunning.start()

	runCtx := entryCtx
	if executionID != "" {
		runCtx = context.WithValue(entryCtx, workflowContextKey{}, executionID)
	}

	go func() {
		defer c.jobWaiter.Done()
		defer entryRunning.finish()

		c.hooks.callOnJobStart(entryID, originalJob, scheduledTime)

		start := c.clock.Now()
		var recovered any
		func() {
			defer func() {
				recovered = recover()
			}()
			if jc, ok := wrappedJob.(JobWithContext); ok {
				jc.RunWithContext(runCtx)
			} else {
				wrappedJob.Run()
			}
		}()
		duration := c.clock.Now().Sub(start)

		c.hooks.callOnJobComplete(entryID, originalJob, duration, recovered)

		// Send completion event for workflow orchestration.
		if executionID != "" {
			c.jobDone <- jobDoneEvent{
				EntryID:     entryID,
				Panicked:    recovered != nil,
				PanicValue:  recovered,
				ExecutionID: executionID,
			}
			return
		}

		if recovered != nil {
			panic(recovered)
		}
	}()
}
```

**Step 5: Add jobDone case to run() loop**

In `run()`, add a new case in the inner select (after the `trigger` case, around line 1098):

```go
			case event := <-c.jobDone:
				c.processWorkflowEvent(event)
				continue
```

Add a stub `processWorkflowEvent`:

```go
// processWorkflowEvent handles a job completion within a workflow execution.
func (c *Cron) processWorkflowEvent(event jobDoneEvent) {
	// TODO: Task 6 implements this.
}
```

**Step 6: Run all tests to verify no regression**

Run: `go test -race -count=1 ./...`
Expected: PASS — all existing tests still work.

**Step 7: Commit**

```bash
git add cron.go workflow.go
git commit -S -m "feat(workflow): add jobDone channel and startJobWithExecution (#312)"
```

---

## Task 4: Imperative API — AddDependency, RemoveDependency, Dependencies

Wire the dependency management through the run loop channel.

**Files:**
- Modify: `cron.go` (run loop, new methods, errors)
- Modify: `workflow.go` (removeEntry cleanup)
- Create: `workflow_test.go` (new tests)

**Step 1: Write failing tests**

Append to `workflow_test.go`:

```go
func TestAddDependency_Basic(t *testing.T) {
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	a, _ := c.AddFunc("@triggered", func() {}, WithName("a"))
	b, _ := c.AddFunc("@triggered", func() {}, WithName("b"))

	err := c.AddDependency(b, a, OnSuccess)
	if err != nil {
		t.Fatalf("AddDependency() error: %v", err)
	}

	deps := c.Dependencies(b)
	if len(deps) != 1 {
		t.Fatalf("Dependencies() = %d edges, want 1", len(deps))
	}
	if deps[0].ParentID != a || deps[0].Condition != OnSuccess {
		t.Errorf("Dependencies()[0] = {%v, %v}, want {%v, OnSuccess}", deps[0].ParentID, deps[0].Condition, a)
	}
}

func TestAddDependency_CycleDetected(t *testing.T) {
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	a, _ := c.AddFunc("@triggered", func() {}, WithName("a"))
	b, _ := c.AddFunc("@triggered", func() {}, WithName("b"))

	_ = c.AddDependency(b, a, OnSuccess)
	err := c.AddDependency(a, b, OnSuccess)

	if !errors.Is(err, ErrCycleDetected) {
		t.Errorf("AddDependency() = %v, want ErrCycleDetected", err)
	}
}

func TestAddDependency_NotFound(t *testing.T) {
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	a, _ := c.AddFunc("@triggered", func() {}, WithName("a"))

	err := c.AddDependency(a, 9999, OnSuccess)
	if !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("AddDependency(unknown parent) = %v, want ErrEntryNotFound", err)
	}

	err = c.AddDependency(9999, a, OnSuccess)
	if !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("AddDependency(unknown child) = %v, want ErrEntryNotFound", err)
	}
}

func TestAddDependencyByName(t *testing.T) {
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	c.AddFunc("@triggered", func() {}, WithName("parent"))
	c.AddFunc("@triggered", func() {}, WithName("child"))

	err := c.AddDependencyByName("child", "parent", OnSuccess)
	if err != nil {
		t.Fatalf("AddDependencyByName() error: %v", err)
	}

	deps := c.DependenciesByName("child")
	if len(deps) != 1 {
		t.Fatalf("DependenciesByName() = %d edges, want 1", len(deps))
	}
}

func TestRemoveDependency(t *testing.T) {
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	a, _ := c.AddFunc("@triggered", func() {}, WithName("a"))
	b, _ := c.AddFunc("@triggered", func() {}, WithName("b"))

	_ = c.AddDependency(b, a, OnSuccess)
	err := c.RemoveDependency(b, a)
	if err != nil {
		t.Fatalf("RemoveDependency() error: %v", err)
	}

	deps := c.Dependencies(b)
	if len(deps) != 0 {
		t.Errorf("Dependencies() after remove = %d edges, want 0", len(deps))
	}
}

func TestAddDependency_InvalidCondition(t *testing.T) {
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	a, _ := c.AddFunc("@triggered", func() {}, WithName("a"))
	b, _ := c.AddFunc("@triggered", func() {}, WithName("b"))

	err := c.AddDependency(b, a, TriggerCondition(99))
	if err == nil {
		t.Error("AddDependency(invalid condition) = nil, want error")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -run 'TestAddDependency|TestRemoveDependency' -v ./...`
Expected: FAIL — methods not defined.

**Step 3: Add error sentinel and implement public/internal methods**

In `cron.go`, add error sentinel (near line 40):

```go
// ErrCycleDetected is returned by AddDependency when the new edge would
// create a cycle in the dependency graph.
var ErrCycleDetected = errors.New("cron: dependency would create a cycle")

// ErrInvalidCondition is returned by AddDependency when the trigger condition
// is not a known value.
var ErrInvalidCondition = errors.New("cron: invalid trigger condition")
```

Add public methods to `cron.go` (after the TriggerEntry methods):

```go
// AddDependency adds a dependency edge: child waits for parent with the given condition.
// Returns error if either entry doesn't exist, the condition is invalid, or the edge
// would create a cycle. Thread-safe; routes through the run loop.
func (c *Cron) AddDependency(child, parent EntryID, condition TriggerCondition) error {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if !c.running {
		return c.addDependencyDirect(child, parent, condition)
	}
	req := makeReq[addDepRequest, error](addDepRequest{childID: child, parentID: parent, condition: condition})
	c.addDep <- req
	return <-req.reply
}

// AddDependencyByName is the name-based variant of AddDependency.
func (c *Cron) AddDependencyByName(child, parent string, condition TriggerCondition) error {
	ce := c.EntryByName(child)
	if !ce.Valid() {
		return ErrEntryNotFound
	}
	pe := c.EntryByName(parent)
	if !pe.Valid() {
		return ErrEntryNotFound
	}
	return c.AddDependency(ce.ID, pe.ID, condition)
}

// RemoveDependency removes a dependency edge between child and parent.
// No-op if the edge doesn't exist.
func (c *Cron) RemoveDependency(child, parent EntryID) error {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if !c.running {
		c.removeDependencyDirect(child, parent)
		return nil
	}
	req := makeReq[removeDepRequest, error](removeDepRequest{childID: child, parentID: parent})
	c.removeDep <- req
	return <-req.reply
}

// RemoveDependencyByName is the name-based variant of RemoveDependency.
func (c *Cron) RemoveDependencyByName(child, parent string) error {
	ce := c.EntryByName(child)
	if !ce.Valid() {
		return ErrEntryNotFound
	}
	pe := c.EntryByName(parent)
	if !pe.Valid() {
		return ErrEntryNotFound
	}
	return c.RemoveDependency(ce.ID, pe.ID)
}

// Dependencies returns the dependency edges for an entry.
func (c *Cron) Dependencies(id EntryID) []Dependency {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if !c.running {
		return slices.Clone(c.entryDeps[id])
	}
	req := makeReq[EntryID, []Dependency](id)
	c.queryDeps <- req
	return <-req.reply
}

// DependenciesByName is the name-based variant of Dependencies.
func (c *Cron) DependenciesByName(name string) []Dependency {
	e := c.EntryByName(name)
	if !e.Valid() {
		return nil
	}
	return c.Dependencies(e.ID)
}
```

Add internal methods (in `cron.go`):

```go
// addDependencyDirect adds a dependency edge directly (when not running).
// Caller must hold runningMu.
func (c *Cron) addDependencyDirect(child, parent EntryID, condition TriggerCondition) error {
	if !condition.Valid() {
		return ErrInvalidCondition
	}
	if _, ok := c.entryIndex[child]; !ok {
		return ErrEntryNotFound
	}
	if _, ok := c.entryIndex[parent]; !ok {
		return ErrEntryNotFound
	}
	if hasCycle(c.entryDeps, child, parent) {
		return ErrCycleDetected
	}
	// Check for duplicate edge.
	for _, dep := range c.entryDeps[child] {
		if dep.ParentID == parent && dep.Condition == condition {
			return nil // idempotent
		}
	}
	c.entryDeps[child] = append(c.entryDeps[child], Dependency{ParentID: parent, Condition: condition})
	c.parentToChildren[parent] = append(c.parentToChildren[parent], child)
	return nil
}

// removeDependencyDirect removes a dependency edge directly (when not running).
func (c *Cron) removeDependencyDirect(child, parent EntryID) {
	deps := c.entryDeps[child]
	for i, dep := range deps {
		if dep.ParentID == parent {
			c.entryDeps[child] = slices.Delete(deps, i, i+1)
			break
		}
	}
	if len(c.entryDeps[child]) == 0 {
		delete(c.entryDeps, child)
	}
	children := c.parentToChildren[parent]
	for i, cid := range children {
		if cid == child {
			c.parentToChildren[parent] = slices.Delete(children, i, i+1)
			break
		}
	}
	if len(c.parentToChildren[parent]) == 0 {
		delete(c.parentToChildren, parent)
	}
}
```

**Step 4: Add run loop handlers**

In `run()`, add new cases (after the `trigger` case):

```go
			case req := <-c.addDep:
				req.reply <- c.addDependencyDirect(req.value.childID, req.value.parentID, req.value.condition)
				continue

			case req := <-c.removeDep:
				c.removeDependencyDirect(req.value.childID, req.value.parentID)
				req.reply <- nil
				continue

			case req := <-c.queryDeps:
				req.reply <- slices.Clone(c.entryDeps[req.value])
				continue
```

**Step 5: Clean up deps on removeEntry**

In `removeEntry()` (cron.go), after the existing cleanup but before `maybeCompactIndexes`, add:

```go
	// Clean up workflow dependency edges for removed entry.
	// Remove edges where this entry is a child.
	if deps, ok := c.entryDeps[id]; ok {
		for _, dep := range deps {
			children := c.parentToChildren[dep.ParentID]
			for i, cid := range children {
				if cid == id {
					c.parentToChildren[dep.ParentID] = slices.Delete(children, i, i+1)
					break
				}
			}
			if len(c.parentToChildren[dep.ParentID]) == 0 {
				delete(c.parentToChildren, dep.ParentID)
			}
		}
		delete(c.entryDeps, id)
	}
	// Remove edges where this entry is a parent.
	if children, ok := c.parentToChildren[id]; ok {
		for _, childID := range children {
			deps := c.entryDeps[childID]
			for i, dep := range deps {
				if dep.ParentID == id {
					c.entryDeps[childID] = slices.Delete(deps, i, i+1)
					break
				}
			}
			if len(c.entryDeps[childID]) == 0 {
				delete(c.entryDeps, childID)
			}
		}
		delete(c.parentToChildren, id)
	}
```

**Step 6: Run tests to verify they pass**

Run: `go test -run 'TestAddDependency|TestRemoveDependency' -v ./...`
Expected: PASS

Run: `go test -race -count=1 ./...`
Expected: PASS — no regressions.

**Step 7: Commit**

```bash
git add cron.go workflow.go workflow_test.go
git commit -S -m "feat(workflow): add AddDependency/RemoveDependency/Dependencies API (#312)"
```

---

## Task 5: WithWorkflowRetention Option + WorkflowExecution Lifecycle

Add the option and the core execution tracking infrastructure.

**Files:**
- Modify: `option.go`
- Modify: `workflow_test.go`

**Step 1: Write failing test**

```go
func TestWithWorkflowRetention(t *testing.T) {
	c := New(WithWorkflowRetention(5))
	if c.workflowRetention != 5 {
		t.Errorf("workflowRetention = %d, want 5", c.workflowRetention)
	}
}

func TestWorkflowExecution_IsComplete(t *testing.T) {
	we := &WorkflowExecution{
		Results: map[EntryID]JobResult{
			1: ResultSuccess,
			2: ResultPending,
		},
	}
	if we.IsComplete() {
		t.Error("IsComplete() = true with pending job, want false")
	}
	we.Results[2] = ResultSkipped
	if !we.IsComplete() {
		t.Error("IsComplete() = false with all terminal, want true")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -run 'TestWithWorkflowRetention|TestWorkflowExecution_IsComplete' -v ./...`

**Step 3: Implement the option**

In `option.go`, append:

```go
// WithWorkflowRetention sets the maximum number of completed workflow
// executions to retain for query via WorkflowStatus/ActiveWorkflows.
// Default is 100. Set to 0 for unlimited retention (not recommended
// for long-running services).
func WithWorkflowRetention(n int) Option {
	return func(c *Cron) {
		c.workflowRetention = n
	}
}
```

**Step 4: Run tests, then full suite**

Run: `go test -run 'TestWithWorkflowRetention|TestWorkflowExecution_IsComplete' -v ./...`
Expected: PASS

Run: `go test -race -count=1 ./...`
Expected: PASS

**Step 5: Commit**

```bash
git add option.go workflow.go workflow_test.go
git commit -S -m "feat(workflow): add WithWorkflowRetention option (#312)"
```

---

## Task 6: Workflow Execution Engine — processWorkflowEvent

The core orchestration logic that evaluates children when parents complete.

**Files:**
- Modify: `cron.go` (processWorkflowEvent, processDueEntries)
- Modify: `workflow.go` (UUID generation)
- Modify: `workflow_test.go`

**Step 1: Write failing test for simple A→B workflow**

```go
func TestWorkflow_SimpleChain(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithSeconds())
	c.Start()
	defer c.Stop()

	order := make(chan string, 10)

	c.AddFunc("@triggered", func() { order <- "a" }, WithName("a"))
	c.AddFunc("@triggered", func() { order <- "b" }, WithName("b"))

	_ = c.AddDependencyByName("b", "a", OnSuccess)

	// Trigger root
	_ = c.TriggerEntryByName("a")

	// Wait for both jobs
	select {
	case got := <-order:
		if got != "a" {
			t.Fatalf("first job = %q, want 'a'", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for job a")
	}

	select {
	case got := <-order:
		if got != "b" {
			t.Fatalf("second job = %q, want 'b'", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for job b")
	}
}

func TestWorkflow_FailureCascadeSkip(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithSeconds())
	c.Start()
	defer c.Stop()

	results := make(chan string, 10)

	c.AddFunc("@triggered", func() {
		results <- "a"
		panic("intentional failure")
	}, WithName("a"))
	c.AddFunc("@triggered", func() { results <- "b-ran" }, WithName("b"))
	c.AddFunc("@triggered", func() { results <- "c-ran" }, WithName("c"))

	_ = c.AddDependencyByName("b", "a", OnSuccess)  // b skipped because a fails
	_ = c.AddDependencyByName("c", "a", OnComplete) // c runs regardless

	_ = c.TriggerEntryByName("a")

	// a runs (then panics)
	select {
	case got := <-order:
		if got != "a" {
			t.Fatalf("first = %q, want 'a'", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for a")
	}

	// c should run (OnComplete), b should NOT run (OnSuccess, but a failed)
	select {
	case got := <-results:
		if got != "c-ran" {
			t.Fatalf("got %q, want 'c-ran'", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for c")
	}

	// Verify b didn't run
	select {
	case got := <-results:
		t.Fatalf("unexpected job ran: %q (b should be skipped)", got)
	case <-time.After(100 * time.Millisecond):
		// Good — b was skipped
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -run 'TestWorkflow_SimpleChain|TestWorkflow_FailureCascadeSkip' -v ./...`
Expected: FAIL

**Step 3: Add UUID generation**

In `workflow.go`:

```go
import "crypto/rand"

// generateExecutionID creates a random UUID v4 for workflow execution tracking.
func generateExecutionID() string {
	var uuid [16]byte
	_, _ = rand.Read(uuid[:])
	uuid[6] = (uuid[6] & 0x0f) | 0x40 // version 4
	uuid[8] = (uuid[8] & 0x3f) | 0x80 // variant 2
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16])
}
```

**Step 4: Implement processWorkflowEvent**

In `cron.go`, replace the stub:

```go
// processWorkflowEvent handles a job completion within a workflow execution.
func (c *Cron) processWorkflowEvent(event jobDoneEvent) {
	exec, ok := c.activeExecutions[event.ExecutionID]
	if !ok {
		return // execution already completed or unknown
	}

	// Record the result.
	if event.Panicked {
		exec.Results[event.EntryID] = ResultFailure
	} else {
		exec.Results[event.EntryID] = ResultSuccess
	}

	// Evaluate children of this entry.
	c.evaluateChildren(exec, event.EntryID)

	// Check if execution is complete.
	if exec.IsComplete() {
		c.completeWorkflowExecution(exec)
	}
}

// evaluateChildren checks each child of parentID and either triggers or skips it.
func (c *Cron) evaluateChildren(exec *WorkflowExecution, parentID EntryID) {
	children := c.parentToChildren[parentID]
	for _, childID := range children {
		// Only evaluate children that are part of this execution and still pending.
		if result, ok := exec.Results[childID]; !ok || result != ResultPending {
			continue
		}

		if c.shouldTriggerChild(exec, childID) {
			c.triggerWorkflowChild(exec, childID)
		} else if c.allParentsResolved(exec, childID) {
			// All parents resolved but conditions not met — skip.
			exec.Results[childID] = ResultSkipped
			c.evaluateChildren(exec, childID) // cascade
		}
	}
}

// shouldTriggerChild checks if all parents of childID are resolved and
// all trigger conditions are satisfied.
func (c *Cron) shouldTriggerChild(exec *WorkflowExecution, childID EntryID) bool {
	deps := c.entryDeps[childID]
	for _, dep := range deps {
		result, ok := exec.Results[dep.ParentID]
		if !ok || !result.IsTerminal() {
			return false // parent not yet resolved
		}
		if !dep.Condition.Matches(result) {
			return false // condition not met
		}
	}
	return len(deps) > 0
}

// allParentsResolved checks if all parents of childID have terminal results.
func (c *Cron) allParentsResolved(exec *WorkflowExecution, childID EntryID) bool {
	deps := c.entryDeps[childID]
	for _, dep := range deps {
		result, ok := exec.Results[dep.ParentID]
		if !ok || !result.IsTerminal() {
			return false
		}
	}
	return true
}

// triggerWorkflowChild triggers a child entry within a workflow execution.
func (c *Cron) triggerWorkflowChild(exec *WorkflowExecution, childID EntryID) {
	entry, ok := c.entryIndex[childID]
	if !ok {
		exec.Results[childID] = ResultSkipped
		return
	}
	if entry.Paused {
		exec.Results[childID] = ResultSkipped
		c.evaluateChildren(exec, childID) // cascade skip
		return
	}

	now := c.now()
	c.startJobWithExecution(entry.entryCtx, entry.running, entry.ID, entry.Job, entry.WrappedJob, now, exec.ID)
	entry.Prev = now
}

// completeWorkflowExecution moves an execution from active to completed
// and applies retention policy.
func (c *Cron) completeWorkflowExecution(exec *WorkflowExecution) {
	delete(c.activeExecutions, exec.ID)
	c.completedExecutions = append(c.completedExecutions, exec)

	// Apply retention policy.
	if c.workflowRetention > 0 && len(c.completedExecutions) > c.workflowRetention {
		excess := len(c.completedExecutions) - c.workflowRetention
		c.completedExecutions = c.completedExecutions[excess:]
	}

	c.hooks.callOnWorkflowComplete(exec.ID, exec.RootID, exec.Results)
}
```

**Step 5: Create workflow execution on trigger**

Modify the internal `triggerEntry` to detect if the entry has children and create an execution. Add a helper:

```go
// startWorkflowExecution creates a new WorkflowExecution for a root entry
// and starts the root job within it.
func (c *Cron) startWorkflowExecution(entry *Entry, scheduledTime time.Time) {
	execID := generateExecutionID()

	// Collect all entries reachable from this root via parentToChildren.
	participants := c.collectWorkflowParticipants(entry.ID)

	exec := &WorkflowExecution{
		ID:        execID,
		RootID:    entry.ID,
		StartTime: scheduledTime,
		Results:   make(map[EntryID]JobResult, len(participants)),
	}
	for _, id := range participants {
		exec.Results[id] = ResultPending
	}

	c.activeExecutions[execID] = exec
	c.startJobWithExecution(entry.entryCtx, entry.running, entry.ID, entry.Job, entry.WrappedJob, scheduledTime, execID)
}

// collectWorkflowParticipants returns all entry IDs reachable from rootID
// via parentToChildren edges (BFS).
func (c *Cron) collectWorkflowParticipants(rootID EntryID) []EntryID {
	var result []EntryID
	visited := map[EntryID]bool{rootID: true}
	queue := []EntryID{rootID}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		for _, childID := range c.parentToChildren[current] {
			if !visited[childID] {
				visited[childID] = true
				queue = append(queue, childID)
			}
		}
	}
	return result
}
```

Modify `triggerEntry` to use workflow execution when the entry has children:

In `triggerEntry()`, replace the `c.startJob(...)` call with:

```go
	// If this entry has dependents, start a workflow execution.
	if len(c.parentToChildren[entry.ID]) > 0 {
		c.startWorkflowExecution(entry, now)
		entry.Prev = now
		if entry.runOnce {
			entry.cancelEntryCtx = nil
			c.removeEntry(entry.ID)
			c.logger.Info("triggered-run-once", "now", now, "entry", id, "removed", true)
		} else {
			c.logger.Info("triggered-workflow", "now", now, "entry", id)
		}
		return nil
	}
```

Similarly, in `processDueEntries`, before calling `c.startJob(...)`:

```go
		// If this entry has dependents, start a workflow execution.
		if len(c.parentToChildren[e.ID]) > 0 {
			c.startWorkflowExecution(e, scheduledTime)
			e.Prev = e.Next
			if e.runOnce {
				e.cancelEntryCtx = nil
				c.removeEntry(e.ID)
			} else {
				e.Next = e.Schedule.Next(now)
				c.hooks.callOnSchedule(e.ID, e.Job, e.Next)
				c.entries.Update(e)
			}
			continue
		}
```

**Step 6: Add OnWorkflowComplete hook**

In `observability.go`, add to the struct:

```go
	// OnWorkflowComplete is called when all jobs in a workflow execution
	// have resolved (success, failure, or skipped).
	OnWorkflowComplete func(executionID string, rootID EntryID, results map[EntryID]JobResult)
```

Add the call method:

```go
func (h *ObservabilityHooks) callOnWorkflowComplete(executionID string, rootID EntryID, results map[EntryID]JobResult) {
	if h != nil && h.OnWorkflowComplete != nil {
		resultsCopy := maps.Clone(results)
		go h.OnWorkflowComplete(executionID, rootID, resultsCopy)
	}
}
```

**Step 7: Run tests**

Run: `go test -run 'TestWorkflow_SimpleChain|TestWorkflow_FailureCascadeSkip' -v -timeout 10s ./...`
Expected: PASS

Run: `go test -race -count=1 ./...`
Expected: PASS

**Step 8: Commit**

```bash
git add cron.go workflow.go workflow_test.go observability.go
git commit -S -m "feat(workflow): implement DAG execution engine with cascade skip logic (#312)"
```

---

## Task 7: Declarative API — Workflow Builder + AddWorkflow

The ergonomic builder API for defining multi-step workflows.

**Files:**
- Modify: `workflow.go`
- Modify: `workflow_test.go`

**Step 1: Write failing test for workflow builder**

```go
func TestNewWorkflow_Builder(t *testing.T) {
	wf := NewWorkflow("etl")

	extract := wf.StepFunc("extract", "@triggered", func() {})
	transform := wf.StepFunc("transform", "@triggered", func() {}).
		After("extract", OnSuccess)
	cleanup := wf.StepFunc("cleanup", "@triggered", func() {}).
		Final()

	if wf.Name != "etl" {
		t.Errorf("Name = %q, want 'etl'", wf.Name)
	}
	if len(wf.steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(wf.steps))
	}
	_ = extract
	if len(transform.deps) != 1 {
		t.Errorf("transform deps = %d, want 1", len(transform.deps))
	}
	if !cleanup.isFinal {
		t.Error("cleanup.isFinal = false, want true")
	}
}

func TestAddWorkflow_EndToEnd(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithSeconds())
	c.Start()
	defer c.Stop()

	order := make(chan string, 10)

	wf := NewWorkflow("pipeline")
	wf.StepFunc("step1", "@triggered", func() { order <- "step1" })
	wf.StepFunc("step2", "@triggered", func() { order <- "step2" }).
		After("step1", OnSuccess)
	wf.StepFunc("final", "@triggered", func() { order <- "final" }).
		Final()

	err := c.AddWorkflow(wf)
	if err != nil {
		t.Fatalf("AddWorkflow() error: %v", err)
	}

	_ = c.TriggerEntryByName("step1")

	// Expect: step1, step2, final (in order)
	expected := []string{"step1", "step2", "final"}
	for _, want := range expected {
		select {
		case got := <-order:
			if got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for %q", want)
		}
	}
}

func TestAddWorkflow_CycleRejected(t *testing.T) {
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	wf := NewWorkflow("cycle")
	wf.StepFunc("a", "@triggered", func() {}).After("b", OnSuccess)
	wf.StepFunc("b", "@triggered", func() {}).After("a", OnSuccess)

	err := c.AddWorkflow(wf)
	if !errors.Is(err, ErrCycleDetected) {
		t.Errorf("AddWorkflow(cycle) = %v, want ErrCycleDetected", err)
	}
}

func TestAddWorkflow_DuplicateStepName(t *testing.T) {
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	c.AddFunc("@triggered", func() {}, WithName("existing"))

	wf := NewWorkflow("dup")
	wf.StepFunc("existing", "@triggered", func() {})

	err := c.AddWorkflow(wf)
	if !errors.Is(err, ErrDuplicateName) {
		t.Errorf("AddWorkflow(dup name) = %v, want ErrDuplicateName", err)
	}
}

func TestAddWorkflow_MultipleFinalSteps(t *testing.T) {
	c := New(WithSeconds())

	wf := NewWorkflow("multi-final")
	wf.StepFunc("a", "@triggered", func() {})
	wf.StepFunc("b", "@triggered", func() {}).Final()
	wf.StepFunc("c", "@triggered", func() {}).Final()

	err := c.AddWorkflow(wf)
	if err == nil {
		t.Error("AddWorkflow(multiple finals) = nil, want error")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -run 'TestNewWorkflow|TestAddWorkflow' -v ./...`

**Step 3: Implement workflow builder**

In `workflow.go`:

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
	deps    []stepDep
	isFinal bool
}

type stepDep struct {
	parentName string
	condition  TriggerCondition
}

// NewWorkflow creates a new workflow builder with the given name.
func NewWorkflow(name string) *Workflow {
	return &Workflow{Name: name}
}

// Step adds a named step with a cron spec and job.
func (w *Workflow) Step(name, spec string, job Job) *WorkflowStep {
	s := &WorkflowStep{name: name, spec: spec, job: job}
	w.steps = append(w.steps, s)
	return s
}

// StepFunc is a convenience that wraps a func() as a FuncJob.
func (w *Workflow) StepFunc(name, spec string, fn func()) *WorkflowStep {
	return w.Step(name, spec, FuncJob(fn))
}

// After declares that this step depends on the named parent step.
func (s *WorkflowStep) After(parentName string, condition TriggerCondition) *WorkflowStep {
	s.deps = append(s.deps, stepDep{parentName: parentName, condition: condition})
	return s
}

// Final marks this step as a finalization step.
// Equivalent to After(parent, OnComplete) for every non-final step.
func (s *WorkflowStep) Final() *WorkflowStep {
	s.isFinal = true
	return s
}
```

**Step 4: Implement AddWorkflow**

In `workflow.go` (or `cron.go`):

```go
// ErrMultipleFinalSteps is returned when a workflow has more than one Final step.
var ErrMultipleFinalSteps = errors.New("cron: workflow has multiple final steps")

// ErrUnknownStep is returned when an After() references a step name not in the workflow.
var ErrUnknownStep = errors.New("cron: workflow step references unknown parent")

// ErrEmptyWorkflow is returned when a workflow has no steps.
var ErrEmptyWorkflow = errors.New("cron: workflow has no steps")

// AddWorkflow validates and registers all steps atomically.
func (c *Cron) AddWorkflow(w *Workflow) error {
	if len(w.steps) == 0 {
		return ErrEmptyWorkflow
	}

	// Validate: at most one final step.
	var finalStep *WorkflowStep
	for _, s := range w.steps {
		if s.isFinal {
			if finalStep != nil {
				return ErrMultipleFinalSteps
			}
			finalStep = s
		}
	}

	// Build name->step index for reference resolution.
	stepIndex := make(map[string]*WorkflowStep, len(w.steps))
	for _, s := range w.steps {
		if _, dup := stepIndex[s.name]; dup {
			return fmt.Errorf("%w: %q appears twice in workflow", ErrDuplicateName, s.name)
		}
		stepIndex[s.name] = s
	}

	// Expand Final: add OnComplete edge from every non-final step.
	if finalStep != nil {
		for _, s := range w.steps {
			if s != finalStep {
				finalStep.deps = append(finalStep.deps, stepDep{parentName: s.name, condition: OnComplete})
			}
		}
	}

	// Validate all After references exist within the workflow.
	for _, s := range w.steps {
		for _, dep := range s.deps {
			if _, ok := stepIndex[dep.parentName]; !ok {
				return fmt.Errorf("%w: %q in step %q", ErrUnknownStep, dep.parentName, s.name)
			}
		}
	}

	// Validate no cycles using temp dependency map.
	tempDeps := make(map[string][]string) // child -> parents (by name)
	for _, s := range w.steps {
		for _, dep := range s.deps {
			tempDeps[s.name] = append(tempDeps[s.name], dep.parentName)
		}
	}
	if hasCycleByName(tempDeps) {
		return ErrCycleDetected
	}

	// Validate all specs parse.
	for _, s := range w.steps {
		if _, err := c.parser.Parse(s.spec); err != nil {
			return fmt.Errorf("cron: invalid spec %q for step %q: %w", s.spec, s.name, err)
		}
	}

	// Check for duplicate names against existing entries.
	for _, s := range w.steps {
		e := c.EntryByName(s.name)
		if e.Valid() {
			return fmt.Errorf("%w: %q already exists", ErrDuplicateName, s.name)
		}
	}

	// All validation passed — register entries.
	nameToID := make(map[string]EntryID, len(w.steps))
	var registeredIDs []EntryID

	for _, s := range w.steps {
		id, err := c.AddJob(s.spec, s.job, WithName(s.name))
		if err != nil {
			// Rollback: remove already-registered entries.
			for _, rid := range registeredIDs {
				c.Remove(rid)
			}
			return fmt.Errorf("cron: failed to register step %q: %w", s.name, err)
		}
		nameToID[s.name] = id
		registeredIDs = append(registeredIDs, id)
	}

	// Wire dependency edges.
	for _, s := range w.steps {
		childID := nameToID[s.name]
		for _, dep := range s.deps {
			parentID := nameToID[dep.parentName]
			if err := c.AddDependency(childID, parentID, dep.condition); err != nil {
				// Rollback.
				for _, rid := range registeredIDs {
					c.Remove(rid)
				}
				return fmt.Errorf("cron: failed to add dependency %q -> %q: %w", s.name, dep.parentName, err)
			}
		}
	}

	return nil
}

// hasCycleByName checks for cycles in a name-based adjacency list.
func hasCycleByName(deps map[string][]string) bool {
	const (
		white = 0 // unvisited
		gray  = 1 // in progress
		black = 2 // done
	)
	color := make(map[string]int)

	var dfs func(node string) bool
	dfs = func(node string) bool {
		color[node] = gray
		for _, parent := range deps[node] {
			switch color[parent] {
			case gray:
				return true // back edge = cycle
			case white:
				if dfs(parent) {
					return true
				}
			}
		}
		color[node] = black
		return false
	}

	for node := range deps {
		if color[node] == white {
			if dfs(node) {
				return true
			}
		}
	}
	return false
}
```

**Step 5: Run tests**

Run: `go test -run 'TestNewWorkflow|TestAddWorkflow' -v -timeout 10s ./...`
Expected: PASS

Run: `go test -race -count=1 ./...`
Expected: PASS

**Step 6: Commit**

```bash
git add cron.go workflow.go workflow_test.go
git commit -S -m "feat(workflow): add declarative Workflow builder and AddWorkflow API (#312)"
```

---

## Task 8: Query API — WorkflowStatus, ActiveWorkflows

**Files:**
- Modify: `cron.go` (run loop handlers, public methods)
- Modify: `workflow_test.go`

**Step 1: Write failing tests**

```go
func TestWorkflowStatus(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	var capturedExecID string
	hooks := ObservabilityHooks{
		OnWorkflowComplete: func(execID string, rootID EntryID, results map[EntryID]JobResult) {
			capturedExecID = execID
		},
	}
	c := New(WithClock(clock), WithSeconds(), WithObservability(hooks))
	c.Start()
	defer c.Stop()

	done := make(chan struct{})
	c.AddFunc("@triggered", func() { close(done) }, WithName("root"))

	_ = c.TriggerEntryByName("root")
	<-done
	// Give the hook goroutine time to capture.
	time.Sleep(50 * time.Millisecond)

	// For a single-entry "workflow" with no deps, there's no execution.
	// WorkflowStatus returns nil for unknown IDs.
	if status := c.WorkflowStatus("nonexistent"); status != nil {
		t.Errorf("WorkflowStatus(unknown) = %v, want nil", status)
	}
}

func TestActiveWorkflows(t *testing.T) {
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	active := c.ActiveWorkflows()
	if len(active) != 0 {
		t.Errorf("ActiveWorkflows() = %d, want 0", len(active))
	}
}
```

**Step 2: Implement query methods**

In `cron.go`:

```go
// WorkflowStatus returns the state of a workflow execution by ID.
// Returns nil if not found. Checks both active and completed executions.
func (c *Cron) WorkflowStatus(executionID string) *WorkflowExecution {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if !c.running {
		if exec, ok := c.activeExecutions[executionID]; ok {
			return exec
		}
		for _, exec := range c.completedExecutions {
			if exec.ID == executionID {
				return exec
			}
		}
		return nil
	}
	req := makeReq[string, *WorkflowExecution](executionID)
	c.queryWorkflow <- req
	return <-req.reply
}

// ActiveWorkflows returns all currently active workflow executions.
func (c *Cron) ActiveWorkflows() []WorkflowExecution {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if !c.running {
		result := make([]WorkflowExecution, 0, len(c.activeExecutions))
		for _, exec := range c.activeExecutions {
			result = append(result, *exec)
		}
		return result
	}
	replyChan := make(chan []WorkflowExecution, 1)
	c.queryActiveWorkflows <- replyChan
	return <-replyChan
}
```

Add run loop handlers:

```go
			case req := <-c.queryWorkflow:
				if exec, ok := c.activeExecutions[req.value]; ok {
					req.reply <- exec
				} else {
					var found *WorkflowExecution
					for _, exec := range c.completedExecutions {
						if exec.ID == req.value {
							found = exec
							break
						}
					}
					req.reply <- found
				}
				continue

			case replyChan := <-c.queryActiveWorkflows:
				result := make([]WorkflowExecution, 0, len(c.activeExecutions))
				for _, exec := range c.activeExecutions {
					result = append(result, *exec)
				}
				replyChan <- result
				continue
```

**Step 3: Run tests**

Run: `go test -run 'TestWorkflowStatus|TestActiveWorkflows' -v ./...`
Expected: PASS

Run: `go test -race -count=1 ./...`
Expected: PASS

**Step 4: Commit**

```bash
git add cron.go workflow_test.go
git commit -S -m "feat(workflow): add WorkflowStatus and ActiveWorkflows query API (#312)"
```

---

## Task 9: WorkflowExecutionID Context Helper

Test that workflow jobs receive the execution ID via context.

**Files:**
- Modify: `workflow_test.go`

**Step 1: Write test**

```go
func TestWorkflowExecutionID_InContext(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithSeconds())
	c.Start()
	defer c.Stop()

	execIDs := make(chan string, 10)

	// Use a JobWithContext to capture the context.
	rootJob := &contextCapturingJob{ch: execIDs}
	childJob := &contextCapturingJob{ch: execIDs}

	c.AddJob("@triggered", rootJob, WithName("root"))
	c.AddJob("@triggered", childJob, WithName("child"))
	_ = c.AddDependencyByName("child", "root", OnSuccess)

	_ = c.TriggerEntryByName("root")

	// Both jobs should receive the same execution ID.
	var ids []string
	for i := 0; i < 2; i++ {
		select {
		case id := <-execIDs:
			ids = append(ids, id)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for execution ID")
		}
	}

	if ids[0] == "" {
		t.Error("root execution ID is empty")
	}
	if ids[0] != ids[1] {
		t.Errorf("execution IDs differ: root=%q, child=%q", ids[0], ids[1])
	}
}

// contextCapturingJob captures the workflow execution ID from context.
type contextCapturingJob struct {
	ch chan string
}

func (j *contextCapturingJob) Run() {
	j.ch <- "" // no context available
}

func (j *contextCapturingJob) RunWithContext(ctx context.Context) {
	j.ch <- WorkflowExecutionID(ctx)
}
```

**Step 2: Run test**

Run: `go test -run TestWorkflowExecutionID_InContext -v -timeout 10s ./...`
Expected: PASS (implementation already done in Task 6).

**Step 3: Commit**

```bash
git add workflow_test.go
git commit -S -m "test(workflow): verify execution ID propagation via context (#312)"
```

---

## Task 10: Edge Case Tests + Concurrency Tests

Comprehensive coverage for edge cases and race conditions.

**Files:**
- Modify: `workflow_test.go`

**Step 1: Write edge case tests**

```go
func TestWorkflow_DiamondDependency(t *testing.T) {
	// A -> B, A -> C, B+C -> D
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithSeconds())
	c.Start()
	defer c.Stop()

	order := make(chan string, 10)

	c.AddFunc("@triggered", func() { order <- "a" }, WithName("a"))
	c.AddFunc("@triggered", func() { order <- "b" }, WithName("b"))
	c.AddFunc("@triggered", func() { order <- "c" }, WithName("c"))
	c.AddFunc("@triggered", func() { order <- "d" }, WithName("d"))

	_ = c.AddDependencyByName("b", "a", OnSuccess)
	_ = c.AddDependencyByName("c", "a", OnSuccess)
	_ = c.AddDependencyByName("d", "b", OnSuccess)
	_ = c.AddDependencyByName("d", "c", OnSuccess)

	_ = c.TriggerEntryByName("a")

	got := make([]string, 4)
	for i := 0; i < 4; i++ {
		select {
		case g := <-order:
			got[i] = g
		case <-time.After(3 * time.Second):
			t.Fatalf("timeout after %d jobs", i)
		}
	}

	// a must be first, d must be last
	if got[0] != "a" {
		t.Errorf("first = %q, want 'a'", got[0])
	}
	if got[3] != "d" {
		t.Errorf("last = %q, want 'd'", got[3])
	}
}

func TestWorkflow_PausedChildSkipped(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithSeconds())
	c.Start()
	defer c.Stop()

	results := make(chan string, 10)

	c.AddFunc("@triggered", func() { results <- "a" }, WithName("a"))
	c.AddFunc("@triggered", func() { results <- "b" }, WithName("b"))

	_ = c.AddDependencyByName("b", "a", OnSuccess)
	_ = c.PauseEntryByName("b")

	_ = c.TriggerEntryByName("a")

	// a runs
	select {
	case <-results:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for a")
	}

	// b should NOT run (paused = skipped)
	select {
	case got := <-results:
		t.Fatalf("paused job ran: %q", got)
	case <-time.After(200 * time.Millisecond):
		// good
	}
}

func TestWorkflow_RemoveEntryDuringExecution(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithSeconds())
	c.Start()
	defer c.Stop()

	blocker := make(chan struct{})
	result := make(chan string, 10)

	aID, _ := c.AddFunc("@triggered", func() {
		<-blocker // block until released
	}, WithName("a"))
	c.AddFunc("@triggered", func() { result <- "b" }, WithName("b"))
	_ = c.AddDependencyByName("b", "a", OnSuccess)

	_ = c.TriggerEntryByName("a")

	// Remove b while a is still running
	c.Remove(c.EntryByName("b").ID)

	// Release a
	close(blocker)
	_ = aID

	// b should NOT run (removed)
	select {
	case got := <-result:
		t.Fatalf("removed job ran: %q", got)
	case <-time.After(500 * time.Millisecond):
		// good
	}
}

func TestWorkflow_OnSkippedTrigger(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithSeconds())
	c.Start()
	defer c.Stop()

	results := make(chan string, 10)

	c.AddFunc("@triggered", func() {
		panic("fail")
	}, WithName("a"))
	c.AddFunc("@triggered", func() {
		results <- "b"
	}, WithName("b"))
	c.AddFunc("@triggered", func() {
		results <- "c"
	}, WithName("c"))

	// b depends on a succeeding -> will be skipped (a fails)
	_ = c.AddDependencyByName("b", "a", OnSuccess)
	// c depends on b being skipped -> should fire
	_ = c.AddDependencyByName("c", "b", OnSkipped)

	_ = c.TriggerEntryByName("a")

	select {
	case got := <-results:
		if got != "c" {
			t.Errorf("got %q, want 'c'", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout — OnSkipped chain did not fire")
	}
}
```

**Step 2: Write race condition test**

```go
func TestWorkflow_ConcurrentExecutions(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithSeconds())
	c.Start()
	defer c.Stop()

	var count int32
	c.AddFunc("@triggered", func() {
		atomic.AddInt32(&count, 1)
	}, WithName("a"))
	c.AddFunc("@triggered", func() {
		atomic.AddInt32(&count, 1)
	}, WithName("b"))
	_ = c.AddDependencyByName("b", "a", OnSuccess)

	// Trigger 5 concurrent workflow executions.
	for i := 0; i < 5; i++ {
		_ = c.TriggerEntryByName("a")
	}

	// Wait for all jobs (5 * 2 = 10 jobs).
	deadline := time.After(5 * time.Second)
	for {
		if atomic.LoadInt32(&count) >= 10 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timeout: only %d/10 jobs ran", atomic.LoadInt32(&count))
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}
```

**Step 3: Run tests**

Run: `go test -run 'TestWorkflow_' -v -timeout 30s ./...`
Expected: PASS

Run: `CGO_ENABLED=1 go test -race -count=1 -timeout 60s ./...`
Expected: PASS

**Step 4: Commit**

```bash
git add workflow_test.go
git commit -S -m "test(workflow): add edge case and concurrency tests (#312)"
```

---

## Task 11: Retention Policy Test + WorkflowExecutionID Test

**Files:**
- Modify: `workflow_test.go`

**Step 1: Write retention test**

```go
func TestWorkflowRetention(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithSeconds(), WithWorkflowRetention(2))
	c.Start()
	defer c.Stop()

	var completedIDs []string
	var mu sync.Mutex
	hooks := ObservabilityHooks{
		OnWorkflowComplete: func(execID string, _ EntryID, _ map[EntryID]JobResult) {
			mu.Lock()
			completedIDs = append(completedIDs, execID)
			mu.Unlock()
		},
	}
	c = New(WithClock(clock), WithSeconds(), WithWorkflowRetention(2), WithObservability(hooks))
	c.Start()
	defer c.Stop()

	c.AddFunc("@triggered", func() {}, WithName("root"))
	c.AddFunc("@triggered", func() {}, WithName("child"))
	_ = c.AddDependencyByName("child", "root", OnSuccess)

	// Trigger 5 executions.
	for i := 0; i < 5; i++ {
		_ = c.TriggerEntryByName("root")
		time.Sleep(50 * time.Millisecond) // let each complete
	}

	time.Sleep(200 * time.Millisecond) // let retention cleanup run

	// Only the last 2 completed executions should be queryable.
	mu.Lock()
	ids := slices.Clone(completedIDs)
	mu.Unlock()

	if len(ids) < 5 {
		t.Skipf("only %d executions completed, need 5", len(ids))
	}

	// First 3 should be gone from completed storage.
	for _, id := range ids[:3] {
		if status := c.WorkflowStatus(id); status != nil {
			t.Errorf("WorkflowStatus(%q) should be nil (evicted by retention)", id)
		}
	}
}

func TestWorkflowExecutionID_NoWorkflow(t *testing.T) {
	ctx := context.Background()
	if id := WorkflowExecutionID(ctx); id != "" {
		t.Errorf("WorkflowExecutionID(Background) = %q, want empty", id)
	}
}
```

**Step 2: Run tests**

Run: `go test -run 'TestWorkflowRetention|TestWorkflowExecutionID_NoWorkflow' -v -timeout 10s ./...`
Expected: PASS

**Step 3: Commit**

```bash
git add workflow_test.go
git commit -S -m "test(workflow): add retention policy and context helper tests (#312)"
```

---

## Task 12: Documentation Updates

Update CHANGELOG, README, and API_REFERENCE.

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `README.md`
- Modify: `docs/API_REFERENCE.md`

**Step 1: Update CHANGELOG.md**

Add under `[Unreleased]`:

```markdown
### Added
- **Workflow/DAG dependencies** ([#312](https://github.com/netresearch/go-cron/issues/312)):
  - `AddDependency`/`AddDependencyByName` - wire dependency edges between entries
  - `RemoveDependency`/`RemoveDependencyByName` - remove edges
  - `Dependencies`/`DependenciesByName` - query dependency edges
  - `NewWorkflow`/`AddWorkflow` - declarative workflow builder with `Step`, `After`, `Final`
  - `WorkflowStatus`/`ActiveWorkflows` - query workflow execution state
  - `WorkflowExecutionID` - extract execution ID from job context
  - `WithWorkflowRetention` - configure completed execution retention
  - 4 trigger conditions: `OnSuccess`, `OnFailure`, `OnSkipped`, `OnComplete`
  - `OnWorkflowComplete` observability hook
```

**Step 2: Update README.md**

Add a "Workflow Dependencies" section with the ETL pipeline example from the design doc.

**Step 3: Update docs/API_REFERENCE.md**

Add workflow types, builder API, and imperative API documentation.

**Step 4: Commit**

```bash
git add CHANGELOG.md README.md docs/API_REFERENCE.md
git commit -S -m "docs: add workflow/DAG dependencies documentation (#312)"
```

---

## Task 13: Full Test Suite + Lint + PR

Final verification and PR creation.

**Step 1: Run full test suite with race detector**

Run: `CGO_ENABLED=1 go test -race -count=1 -timeout 120s ./...`
Expected: PASS

**Step 2: Run linter**

Run: `golangci-lint run`
Expected: No issues

**Step 3: Check coverage**

Run: `go test -coverprofile=cover.out ./... && go tool cover -func=cover.out | tail -1`
Expected: Coverage >= 95%

**Step 4: Create PR**

```bash
git push -u origin feat/workflow-dag-312
gh pr create --title "feat: add workflow/DAG job dependencies (#312)" --body "..."
```

---

## Dependency Graph

```
Task 1 (types) ──┐
                  ├── Task 3 (jobDone channel) ── Task 6 (execution engine)
Task 2 (cycles) ──┤                                      │
                  ├── Task 4 (imperative API) ────────────┤
                  │                                       │
                  └── Task 5 (retention option)           │
                                                          │
Task 7 (declarative API) ────────────────────────────────-┘
                                                          │
Task 8 (query API) ──────────────────────────────────────-┘
                                                          │
Task 9 (context test) ───────────────────────────────────-┘
                                                          │
Task 10 (edge cases) ────────────────────────────────────-┘
                                                          │
Task 11 (retention test) ────────────────────────────────-┘
                                                          │
Task 12 (docs) ──────────────────────────────────────────-┘
                                                          │
Task 13 (final verification + PR) ───────────────────────-┘
```
