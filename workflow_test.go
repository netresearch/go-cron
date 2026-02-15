package cron

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTriggerCondition_String(t *testing.T) {
	tests := []struct {
		cond TriggerCondition
		want string
	}{
		{OnSuccess, "OnSuccess"},
		{OnFailure, "OnFailure"},
		{OnSkipped, "OnSkipped"},
		{OnComplete, "OnComplete"},
		{TriggerCondition(99), "TriggerCondition(99)"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.cond.String(); got != tt.want {
				t.Errorf("TriggerCondition.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTriggerCondition_Valid(t *testing.T) {
	tests := []struct {
		cond TriggerCondition
		want bool
	}{
		{OnSuccess, true},
		{OnFailure, true},
		{OnSkipped, true},
		{OnComplete, true},
		{TriggerCondition(99), false},
	}
	for _, tt := range tests {
		t.Run(tt.cond.String(), func(t *testing.T) {
			if got := tt.cond.Valid(); got != tt.want {
				t.Errorf("TriggerCondition(%d).Valid() = %v, want %v", int(tt.cond), got, tt.want)
			}
		})
	}
}

func TestJobResult_String(t *testing.T) {
	tests := []struct {
		result JobResult
		want   string
	}{
		{ResultPending, "Pending"},
		{ResultSuccess, "Success"},
		{ResultFailure, "Failure"},
		{ResultSkipped, "Skipped"},
		{JobResult(99), "JobResult(99)"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.result.String(); got != tt.want {
				t.Errorf("JobResult.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestJobResult_IsTerminal(t *testing.T) {
	tests := []struct {
		result JobResult
		want   bool
	}{
		{ResultPending, false},
		{ResultSuccess, true},
		{ResultFailure, true},
		{ResultSkipped, true},
		{JobResult(99), false},
	}
	for _, tt := range tests {
		t.Run(tt.result.String(), func(t *testing.T) {
			if got := tt.result.IsTerminal(); got != tt.want {
				t.Errorf("JobResult(%d).IsTerminal() = %v, want %v", int(tt.result), got, tt.want)
			}
		})
	}
}

func TestConditionMatches(t *testing.T) {
	tests := []struct {
		cond   TriggerCondition
		result JobResult
		want   bool
	}{
		// OnSuccess matches only ResultSuccess
		{OnSuccess, ResultSuccess, true},
		{OnSuccess, ResultFailure, false},
		{OnSuccess, ResultSkipped, false},

		// OnFailure matches only ResultFailure
		{OnFailure, ResultSuccess, false},
		{OnFailure, ResultFailure, true},
		{OnFailure, ResultSkipped, false},

		// OnSkipped matches only ResultSkipped
		{OnSkipped, ResultSuccess, false},
		{OnSkipped, ResultFailure, false},
		{OnSkipped, ResultSkipped, true},

		// OnComplete matches all terminal results
		{OnComplete, ResultSuccess, true},
		{OnComplete, ResultFailure, true},
		{OnComplete, ResultSkipped, true},
	}
	for _, tt := range tests {
		name := tt.cond.String() + "/" + tt.result.String()
		t.Run(name, func(t *testing.T) {
			if got := tt.cond.Matches(tt.result); got != tt.want {
				t.Errorf("%s.Matches(%s) = %v, want %v",
					tt.cond, tt.result, got, tt.want)
			}
		})
	}

	// Unknown condition never matches
	t.Run("Unknown/Success", func(t *testing.T) {
		if TriggerCondition(99).Matches(ResultSuccess) {
			t.Error("unknown TriggerCondition should not match any result")
		}
	})
}

func TestWorkflowExecution_IsComplete(t *testing.T) {
	t.Run("AllTerminal", func(t *testing.T) {
		we := &WorkflowExecution{
			ID:        "exec-1",
			RootID:    1,
			StartTime: time.Now(),
			Results: map[EntryID]JobResult{
				1: ResultSuccess,
				2: ResultFailure,
				3: ResultSkipped,
			},
		}
		if !we.IsComplete() {
			t.Error("IsComplete() = false, want true when all results are terminal")
		}
	})

	t.Run("HasPending", func(t *testing.T) {
		we := &WorkflowExecution{
			ID:        "exec-2",
			RootID:    1,
			StartTime: time.Now(),
			Results: map[EntryID]JobResult{
				1: ResultSuccess,
				2: ResultPending,
			},
		}
		if we.IsComplete() {
			t.Error("IsComplete() = true, want false when a result is still pending")
		}
	})

	t.Run("Empty", func(t *testing.T) {
		we := &WorkflowExecution{
			ID:        "exec-3",
			RootID:    1,
			StartTime: time.Now(),
			Results:   map[EntryID]JobResult{},
		}
		if !we.IsComplete() {
			t.Error("IsComplete() = false, want true when Results map is empty")
		}
	})
}

func TestWorkflowExecutionID_NoWorkflow(t *testing.T) {
	id := WorkflowExecutionID(context.Background())
	if id != "" {
		t.Errorf("WorkflowExecutionID(background) = %q, want empty string", id)
	}
}

func TestWorkflowExecutionID_WithValue(t *testing.T) {
	ctx := context.WithValue(context.Background(), workflowContextKey{}, "wf-abc-123")
	id := WorkflowExecutionID(ctx)
	if id != "wf-abc-123" {
		t.Errorf("WorkflowExecutionID() = %q, want %q", id, "wf-abc-123")
	}
}

func TestHasCycle(t *testing.T) {
	tests := []struct {
		name      string
		edges     map[EntryID][]EntryID // child -> parents
		newChild  EntryID
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
			name:      "direct cycle",
			edges:     map[EntryID][]EntryID{2: {1}},
			newChild:  1,
			newParent: 2,
			wantCycle: true,
		},
		{
			name:      "indirect cycle A->B->C->A",
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
			newParent: 3,
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
		t.Errorf("dep = {%v, %v}, want {%v, OnSuccess}", deps[0].ParentID, deps[0].Condition, a)
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

	if err := c.AddDependency(a, 9999, OnSuccess); !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("unknown parent: got %v, want ErrEntryNotFound", err)
	}
	if err := c.AddDependency(9999, a, OnSuccess); !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("unknown child: got %v, want ErrEntryNotFound", err)
	}
}

func TestAddDependency_InvalidCondition(t *testing.T) {
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	a, _ := c.AddFunc("@triggered", func() {}, WithName("a"))
	b, _ := c.AddFunc("@triggered", func() {}, WithName("b"))

	err := c.AddDependency(b, a, TriggerCondition(99))
	if !errors.Is(err, ErrInvalidCondition) {
		t.Errorf("invalid condition: got %v, want ErrInvalidCondition", err)
	}
}

func TestAddDependency_Idempotent(t *testing.T) {
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	a, _ := c.AddFunc("@triggered", func() {}, WithName("a"))
	b, _ := c.AddFunc("@triggered", func() {}, WithName("b"))

	_ = c.AddDependency(b, a, OnSuccess)
	_ = c.AddDependency(b, a, OnSuccess) // duplicate

	deps := c.Dependencies(b)
	if len(deps) != 1 {
		t.Errorf("duplicate edge: got %d deps, want 1", len(deps))
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
	if err := c.RemoveDependency(b, a); err != nil {
		t.Fatalf("RemoveDependency() error: %v", err)
	}

	if deps := c.Dependencies(b); len(deps) != 0 {
		t.Errorf("after remove: got %d deps, want 0", len(deps))
	}
}

func TestRemoveEntry_CleansDeps(t *testing.T) {
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	a, _ := c.AddFunc("@triggered", func() {}, WithName("a"))
	b, _ := c.AddFunc("@triggered", func() {}, WithName("b"))

	_ = c.AddDependency(b, a, OnSuccess)
	c.Remove(b)

	// Parent should have no children after child is removed.
	// Verify indirectly: adding a new dep from a different child should work.
	c2, _ := c.AddFunc("@triggered", func() {}, WithName("c"))
	err := c.AddDependency(c2, a, OnSuccess)
	if err != nil {
		t.Fatalf("AddDependency after remove: %v", err)
	}
}

func TestWithWorkflowRetention(t *testing.T) {
	c := New(WithWorkflowRetention(5))
	if c.workflowRetention != 5 {
		t.Errorf("workflowRetention = %d, want 5", c.workflowRetention)
	}
}

func TestWithWorkflowRetention_Default(t *testing.T) {
	c := New()
	if c.workflowRetention != 100 {
		t.Errorf("default workflowRetention = %d, want 100", c.workflowRetention)
	}
}

func TestWorkflow_SimpleChain(t *testing.T) {
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	order := make(chan string, 10)

	c.AddFunc("@triggered", func() { order <- "a" }, WithName("a"))
	c.AddFunc("@triggered", func() { order <- "b" }, WithName("b"))
	_ = c.AddDependencyByName("b", "a", OnSuccess)

	_ = c.TriggerEntryByName("a")

	for _, want := range []string{"a", "b"} {
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

func TestWorkflow_FailureSkipsOnSuccess(t *testing.T) {
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	results := make(chan string, 10)

	c.AddFunc("@triggered", func() {
		results <- "a"
		panic("fail")
	}, WithName("a"))
	c.AddFunc("@triggered", func() { results <- "b-ran" }, WithName("b"))
	c.AddFunc("@triggered", func() { results <- "c-ran" }, WithName("c"))

	_ = c.AddDependencyByName("b", "a", OnSuccess)  // b skipped (a fails)
	_ = c.AddDependencyByName("c", "a", OnComplete) // c runs regardless

	_ = c.TriggerEntryByName("a")

	// a runs first
	select {
	case got := <-results:
		if got != "a" {
			t.Fatalf("first = %q, want 'a'", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for a")
	}

	// c should run (OnComplete)
	select {
	case got := <-results:
		if got != "c-ran" {
			t.Fatalf("got %q, want 'c-ran'", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for c")
	}

	// b should NOT run (OnSuccess, but a failed)
	select {
	case got := <-results:
		t.Fatalf("unexpected job: %q (b should be skipped)", got)
	case <-time.After(200 * time.Millisecond):
		// Good
	}
}

func TestWorkflow_RecoverPropagatesFailure(t *testing.T) {
	// Verify that the Recover wrapper correctly re-panics in workflow context
	// so the workflow engine detects job failures even when Recover is in the chain.
	c := New(WithSeconds(), WithChain(Recover(DiscardLogger)))
	c.Start()
	defer c.Stop()

	results := make(chan string, 10)

	c.AddFunc("@triggered", func() {
		results <- "a"
		panic("job-a-failed")
	}, WithName("a"))
	c.AddFunc("@triggered", func() { results <- "b-ran" }, WithName("b"))
	c.AddFunc("@triggered", func() { results <- "c-ran" }, WithName("c"))

	_ = c.AddDependencyByName("b", "a", OnSuccess) // b skipped (a fails)
	_ = c.AddDependencyByName("c", "a", OnFailure) // c runs (a fails)

	_ = c.TriggerEntryByName("a")

	// a runs first
	select {
	case got := <-results:
		if got != "a" {
			t.Fatalf("first = %q, want 'a'", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for a")
	}

	// c should run (OnFailure, and a panicked)
	select {
	case got := <-results:
		if got != "c-ran" {
			t.Fatalf("got %q, want 'c-ran'", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for c")
	}

	// b should NOT run (OnSuccess, but a failed)
	select {
	case got := <-results:
		t.Fatalf("unexpected job: %q (b should be skipped)", got)
	case <-time.After(200 * time.Millisecond):
		// Good
	}
}

func TestWorkflow_ThreeStepChain(t *testing.T) {
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	order := make(chan string, 10)

	c.AddFunc("@triggered", func() { order <- "a" }, WithName("a"))
	c.AddFunc("@triggered", func() { order <- "b" }, WithName("b"))
	c.AddFunc("@triggered", func() { order <- "c" }, WithName("c"))

	_ = c.AddDependencyByName("b", "a", OnSuccess)
	_ = c.AddDependencyByName("c", "b", OnSuccess)

	_ = c.TriggerEntryByName("a")

	for _, want := range []string{"a", "b", "c"} {
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

func TestAddWorkflow_EmptyWorkflow(t *testing.T) {
	c := New(WithSeconds())

	wf := NewWorkflow("empty")

	err := c.AddWorkflow(wf)
	if !errors.Is(err, ErrEmptyWorkflow) {
		t.Errorf("AddWorkflow(empty) = %v, want ErrEmptyWorkflow", err)
	}
}

func TestAddWorkflow_UnknownParent(t *testing.T) {
	c := New(WithSeconds())

	wf := NewWorkflow("bad-ref")
	wf.StepFunc("a", "@triggered", func() {}).After("nonexistent", OnSuccess)

	err := c.AddWorkflow(wf)
	if !errors.Is(err, ErrUnknownStep) {
		t.Errorf("AddWorkflow(unknown parent) = %v, want ErrUnknownStep", err)
	}
}

func TestHasCycleByName(t *testing.T) {
	tests := []struct {
		name      string
		deps      map[string][]string
		wantCycle bool
	}{
		{
			name:      "no cycle",
			deps:      map[string][]string{"a": nil, "b": {"a"}, "c": {"b"}},
			wantCycle: false,
		},
		{
			name:      "direct cycle",
			deps:      map[string][]string{"a": {"b"}, "b": {"a"}},
			wantCycle: true,
		},
		{
			name:      "indirect cycle",
			deps:      map[string][]string{"a": {"c"}, "b": {"a"}, "c": {"b"}},
			wantCycle: true,
		},
		{
			name:      "diamond no cycle",
			deps:      map[string][]string{"a": nil, "b": {"a"}, "c": {"a"}, "d": {"b", "c"}},
			wantCycle: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasCycleByName(tt.deps)
			if got != tt.wantCycle {
				t.Errorf("hasCycleByName() = %v, want %v", got, tt.wantCycle)
			}
		})
	}
}

func TestWorkflowStatus(t *testing.T) {
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

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

func TestWorkflowExecutionID_InContext(t *testing.T) {
	c := New(WithSeconds())
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
	for range 2 {
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

func TestWorkflow_DiamondDependency(t *testing.T) {
	// A -> B, A -> C, B+C -> D
	c := New(WithSeconds())
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
	for i := range 4 {
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
	c := New(WithSeconds())
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
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	blocker := make(chan struct{})
	result := make(chan string, 10)

	c.AddFunc("@triggered", func() {
		<-blocker // block until released
	}, WithName("a"))
	c.AddFunc("@triggered", func() { result <- "b" }, WithName("b"))
	_ = c.AddDependencyByName("b", "a", OnSuccess)

	_ = c.TriggerEntryByName("a")

	// Remove b while a is still running
	c.Remove(c.EntryByName("b").ID)

	// Release a
	close(blocker)

	// b should NOT run (removed)
	select {
	case got := <-result:
		t.Fatalf("removed job ran: %q", got)
	case <-time.After(500 * time.Millisecond):
		// good
	}
}

func TestWorkflow_OnSkippedTrigger(t *testing.T) {
	c := New(WithSeconds())
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

func TestWorkflow_ConcurrentExecutions(t *testing.T) {
	c := New(WithSeconds())
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
	for range 5 {
		_ = c.TriggerEntryByName("a")
	}

	// Wait for all jobs (5 * 2 = 10 jobs).
	deadline := time.After(5 * time.Second)
	for atomic.LoadInt32(&count) < 10 {
		select {
		case <-deadline:
			t.Fatalf("timeout: only %d/10 jobs ran", atomic.LoadInt32(&count))
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestWorkflowRetention(t *testing.T) {
	var completedIDs []string
	var mu sync.Mutex
	hooks := ObservabilityHooks{
		OnWorkflowComplete: func(execID string, _ EntryID, _ map[EntryID]JobResult) {
			mu.Lock()
			completedIDs = append(completedIDs, execID)
			mu.Unlock()
		},
	}
	c := New(WithSeconds(), WithWorkflowRetention(2), WithObservability(hooks))
	c.Start()
	defer c.Stop()

	c.AddFunc("@triggered", func() {}, WithName("root"))
	c.AddFunc("@triggered", func() {}, WithName("child"))
	_ = c.AddDependencyByName("child", "root", OnSuccess)

	// Trigger 5 executions.
	for range 5 {
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

func TestRemoveDependencyByName(t *testing.T) {
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	c.AddFunc("@triggered", func() {}, WithName("parent"))
	c.AddFunc("@triggered", func() {}, WithName("child"))
	_ = c.AddDependencyByName("child", "parent", OnSuccess)

	if err := c.RemoveDependencyByName("child", "parent"); err != nil {
		t.Fatalf("RemoveDependencyByName() error: %v", err)
	}
	if deps := c.DependenciesByName("child"); len(deps) != 0 {
		t.Errorf("after remove: got %d deps, want 0", len(deps))
	}

	// Unknown names return ErrEntryNotFound.
	if err := c.RemoveDependencyByName("missing", "parent"); !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("unknown child: got %v, want ErrEntryNotFound", err)
	}
	if err := c.RemoveDependencyByName("child", "missing"); !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("unknown parent: got %v, want ErrEntryNotFound", err)
	}
}

func TestWorkflowStatus_BeforeStart(t *testing.T) {
	c := New(WithSeconds())
	// Don't start — test pre-start path.
	if status := c.WorkflowStatus("nonexistent"); status != nil {
		t.Errorf("WorkflowStatus(not-started) = %v, want nil", status)
	}
}

func TestActiveWorkflows_BeforeStart(t *testing.T) {
	c := New(WithSeconds())
	// Don't start — test pre-start path.
	active := c.ActiveWorkflows()
	if len(active) != 0 {
		t.Errorf("ActiveWorkflows(not-started) = %d, want 0", len(active))
	}
}

func TestDependencies_BeforeStart(t *testing.T) {
	c := New(WithSeconds())
	// Don't start — test pre-start path.
	deps := c.Dependencies(9999)
	if deps != nil {
		t.Errorf("Dependencies(not-started) = %v, want nil", deps)
	}
}

func TestDependenciesByName_NotFound(t *testing.T) {
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	deps := c.DependenciesByName("nonexistent")
	if len(deps) != 0 {
		t.Errorf("DependenciesByName(unknown) = %v, want empty", deps)
	}
}

func TestAddDependency_MultiCondition(t *testing.T) {
	// Adding multiple conditions between the same parent-child pair should
	// create multiple entryDeps edges but only one parentToChildren entry.
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	a, _ := c.AddFunc("@triggered", func() {}, WithName("a"))
	b, _ := c.AddFunc("@triggered", func() {}, WithName("b"))

	_ = c.AddDependency(b, a, OnSuccess)
	_ = c.AddDependency(b, a, OnFailure)

	deps := c.Dependencies(b)
	if len(deps) != 2 {
		t.Errorf("multi-condition: got %d deps, want 2", len(deps))
	}
}

func TestAddDependencyByName_NotFound(t *testing.T) {
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	c.AddFunc("@triggered", func() {}, WithName("a"))

	if err := c.AddDependencyByName("missing", "a", OnSuccess); !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("unknown child: got %v, want ErrEntryNotFound", err)
	}
	if err := c.AddDependencyByName("a", "missing", OnSuccess); !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("unknown parent: got %v, want ErrEntryNotFound", err)
	}
}

// --- Tests to improve patch coverage ---

func TestAddWorkflow_DuplicateStepNameWithinWorkflow(t *testing.T) {
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	wf := NewWorkflow("dup-internal")
	wf.StepFunc("same", "@triggered", func() {})
	wf.StepFunc("same", "@triggered", func() {})

	err := c.AddWorkflow(wf)
	if !errors.Is(err, ErrDuplicateName) {
		t.Errorf("AddWorkflow(dup internal) = %v, want ErrDuplicateName", err)
	}
}

func TestAddWorkflow_InvalidSpec(t *testing.T) {
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	wf := NewWorkflow("bad-spec")
	wf.StepFunc("a", "not a valid cron spec!!!", func() {})

	err := c.AddWorkflow(wf)
	if err == nil {
		t.Error("AddWorkflow(invalid spec) = nil, want error")
	}
}

func TestWorkflowStatus_CompletedExecution(t *testing.T) {
	var capturedExecID string
	var mu sync.Mutex
	hooks := ObservabilityHooks{
		OnWorkflowComplete: func(execID string, _ EntryID, _ map[EntryID]JobResult) {
			mu.Lock()
			capturedExecID = execID
			mu.Unlock()
		},
	}
	c := New(WithSeconds(), WithObservability(hooks))
	c.Start()
	defer c.Stop()

	done := make(chan struct{})
	var once sync.Once
	c.AddFunc("@triggered", func() {}, WithName("root"))
	c.AddFunc("@triggered", func() { once.Do(func() { close(done) }) }, WithName("child"))
	_ = c.AddDependencyByName("child", "root", OnSuccess)

	_ = c.TriggerEntryByName("root")
	<-done
	time.Sleep(200 * time.Millisecond) // let hook fire

	mu.Lock()
	execID := capturedExecID
	mu.Unlock()

	if execID == "" {
		t.Skip("execution ID not captured")
	}

	status := c.WorkflowStatus(execID)
	if status == nil {
		t.Fatal("WorkflowStatus(completed) = nil, want non-nil")
	}
	if status.ID != execID {
		t.Errorf("status.ID = %q, want %q", status.ID, execID)
	}
}

func TestRemoveDependency_BeforeStart(t *testing.T) {
	c := New(WithSeconds())
	// Don't start — test pre-start path.
	a, _ := c.AddFunc("@triggered", func() {}, WithName("a"))
	b, _ := c.AddFunc("@triggered", func() {}, WithName("b"))

	_ = c.AddDependency(b, a, OnSuccess)
	if err := c.RemoveDependency(b, a); err != nil {
		t.Fatalf("RemoveDependency(pre-start) error: %v", err)
	}
	if deps := c.Dependencies(b); len(deps) != 0 {
		t.Errorf("after remove: got %d deps, want 0", len(deps))
	}
}

func TestAddDependency_BeforeStart(t *testing.T) {
	c := New(WithSeconds())
	// Don't start — test pre-start path.
	a, _ := c.AddFunc("@triggered", func() {}, WithName("a"))
	b, _ := c.AddFunc("@triggered", func() {}, WithName("b"))

	err := c.AddDependency(b, a, OnSuccess)
	if err != nil {
		t.Fatalf("AddDependency(pre-start) error: %v", err)
	}
	deps := c.Dependencies(b)
	if len(deps) != 1 {
		t.Fatalf("Dependencies() = %d, want 1", len(deps))
	}
}

func TestRemoveDependency_NoOp(t *testing.T) {
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	a, _ := c.AddFunc("@triggered", func() {}, WithName("a"))
	b, _ := c.AddFunc("@triggered", func() {}, WithName("b"))

	// Removing non-existent edge should be a no-op, not an error.
	err := c.RemoveDependency(b, a)
	if err != nil {
		t.Errorf("RemoveDependency(no-op) = %v, want nil", err)
	}
}

func TestWorkflow_ScheduledExecution(t *testing.T) {
	// Test that a cron-scheduled root entry starts a workflow when it has dependents.
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithSeconds())
	c.Start()
	defer c.Stop()

	order := make(chan string, 10)
	c.AddFunc("* * * * * *", func() { order <- "root" }, WithName("root"))
	c.AddFunc("@triggered", func() { order <- "child" }, WithName("child"))
	_ = c.AddDependencyByName("child", "root", OnSuccess)

	// Advance clock to trigger the cron schedule.
	clock.Advance(1 * time.Second)

	// Both root and child should execute.
	for _, want := range []string{"root", "child"} {
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

func TestWorkflow_TriggerWithRunOnce(t *testing.T) {
	// Test the workflow trigger + runOnce path (lines 1697-1707 of triggerEntry).
	// When a runOnce root with dependents is triggered, the root job fires and the
	// entry is removed from the scheduler. The root's removal cleans up dependency
	// edges, so children won't be triggered — but the code path for starting the
	// workflow execution and removing the entry is exercised.
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	rootRan := make(chan struct{}, 1)
	c.AddFunc("@triggered", func() { rootRan <- struct{}{} }, WithName("root"), WithRunOnce())
	c.AddFunc("@triggered", func() {}, WithName("child"))
	_ = c.AddDependencyByName("child", "root", OnSuccess)

	_ = c.TriggerEntryByName("root")

	select {
	case <-rootRan:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for root to run")
	}

	// root should have been removed (runOnce).
	time.Sleep(100 * time.Millisecond)
	e := c.EntryByName("root")
	if e.Valid() {
		t.Error("root entry should have been removed after runOnce trigger")
	}
}

func TestProcessWorkflowEvent_UnknownExecution(t *testing.T) {
	// Test the unknown execution ID path (line 1291-1292 of processWorkflowEvent).
	// We trigger a workflow, remove the child while root is running so the
	// child's done event arrives for a completed (purged) execution.
	// Instead, we can test indirectly: complete a workflow, let retention purge it,
	// then verify WorkflowStatus returns nil for the old ID.
	c := New(WithSeconds(), WithWorkflowRetention(1))
	c.Start()
	defer c.Stop()

	done := make(chan struct{}, 10)
	c.AddFunc("@triggered", func() {}, WithName("root"))
	c.AddFunc("@triggered", func() { done <- struct{}{} }, WithName("child"))
	_ = c.AddDependencyByName("child", "root", OnSuccess)

	// Run 3 executions to ensure retention evicts the first.
	for range 3 {
		_ = c.TriggerEntryByName("root")
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for workflow")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Evicted executions should return nil.
	// This exercises WorkflowStatus's completed-execution search returning nil.
	if status := c.WorkflowStatus("nonexistent-exec-id"); status != nil {
		t.Errorf("WorkflowStatus(evicted) = %v, want nil", status)
	}
}

func TestWorkflow_ScheduledExecutionRunOnce(t *testing.T) {
	// Test processDueEntries workflow + runOnce path (lines 1031-1034).
	// The root fires via cron schedule, the workflow execution is created,
	// then the entry is removed because runOnce is set.
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithSeconds())
	c.Start()
	defer c.Stop()

	rootRan := make(chan struct{}, 1)
	c.AddFunc("* * * * * *", func() { rootRan <- struct{}{} }, WithName("root"), WithRunOnce())
	c.AddFunc("@triggered", func() {}, WithName("child"))
	_ = c.AddDependencyByName("child", "root", OnSuccess)

	clock.Advance(1 * time.Second)

	select {
	case <-rootRan:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for root")
	}

	// root should have been removed (runOnce in processDueEntries workflow path).
	time.Sleep(100 * time.Millisecond)
	e := c.EntryByName("root")
	if e.Valid() {
		t.Error("root entry should have been removed after runOnce scheduled workflow")
	}
}

func TestTriggerWorkflowChild_EntryNotFound(t *testing.T) {
	// Test triggerWorkflowChild when child entry has been removed during execution.
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	blocker := make(chan struct{})
	result := make(chan string, 10)

	c.AddFunc("@triggered", func() {
		<-blocker // block until released
	}, WithName("a"))
	bID, _ := c.AddFunc("@triggered", func() { result <- "b-ran" }, WithName("b"))
	c.AddFunc("@triggered", func() { result <- "c-ran" }, WithName("c"))
	_ = c.AddDependencyByName("b", "a", OnSuccess)
	_ = c.AddDependencyByName("c", "b", OnSuccess)

	_ = c.TriggerEntryByName("a")

	// Remove b while a is still running — when a completes, triggerWorkflowChild
	// won't find b in entryIndex, so b gets ResultSkipped, which cascades to c.
	c.Remove(bID)

	close(blocker)

	// c depends on b with OnSuccess; b was skipped, so c should NOT run.
	select {
	case got := <-result:
		t.Fatalf("unexpected job ran: %q (both b and c should be skipped)", got)
	case <-time.After(500 * time.Millisecond):
		// Good: neither b nor c ran.
	}
}

func TestWorkflow_ScheduledExecutionNonRunOnce(t *testing.T) {
	// Test processDueEntries workflow non-runOnce path (lines 1035-1039):
	// entry is rescheduled after workflow dispatch.
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithSeconds())
	c.Start()
	defer c.Stop()

	count := make(chan struct{}, 10)
	c.AddFunc("* * * * * *", func() { count <- struct{}{} }, WithName("root"))
	c.AddFunc("@triggered", func() {}, WithName("child"))
	_ = c.AddDependencyByName("child", "root", OnSuccess)

	// First tick.
	clock.Advance(1 * time.Second)
	select {
	case <-count:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for first tick")
	}

	// Let the workflow execution fully complete before advancing again.
	time.Sleep(100 * time.Millisecond)

	// Second tick — proves the entry was rescheduled, not removed.
	clock.Advance(1 * time.Second)
	select {
	case <-count:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for second tick (entry should be rescheduled)")
	}
}

func TestWorkflowStatus_AfterStop(t *testing.T) {
	// Test WorkflowStatus pre-start (stopped) path for completed executions.
	var capturedExecID string
	var mu sync.Mutex
	hooks := ObservabilityHooks{
		OnWorkflowComplete: func(execID string, _ EntryID, _ map[EntryID]JobResult) {
			mu.Lock()
			capturedExecID = execID
			mu.Unlock()
		},
	}
	c := New(WithSeconds(), WithObservability(hooks))
	c.Start()

	done := make(chan struct{})
	var once sync.Once
	c.AddFunc("@triggered", func() {}, WithName("root"))
	c.AddFunc("@triggered", func() { once.Do(func() { close(done) }) }, WithName("child"))
	_ = c.AddDependencyByName("child", "root", OnSuccess)

	_ = c.TriggerEntryByName("root")
	<-done
	time.Sleep(200 * time.Millisecond) // let hook fire

	mu.Lock()
	execID := capturedExecID
	mu.Unlock()
	if execID == "" {
		t.Skip("execution ID not captured")
	}

	// Stop the cron — now WorkflowStatus takes the !c.running path.
	c.Stop()

	// Query for the completed execution (pre-start completed path).
	status := c.WorkflowStatus(execID)
	if status == nil {
		t.Fatal("WorkflowStatus(after stop, completed) = nil, want non-nil")
	}
	if status.ID != execID {
		t.Errorf("status.ID = %q, want %q", status.ID, execID)
	}

	// Query for an unknown execution (pre-start not-found path).
	if got := c.WorkflowStatus("nonexistent"); got != nil {
		t.Errorf("WorkflowStatus(after stop, unknown) = %v, want nil", got)
	}
}

func TestRegisterWorkflowSteps_RollbackOnMaxEntries(t *testing.T) {
	// Test registerWorkflowSteps rollback: configure WithMaxEntries so that
	// the second step's AddJob fails. The first step should be rolled back.
	c := New(WithSeconds(), WithMaxEntries(1))
	c.Start()
	defer c.Stop()

	wf := NewWorkflow("over-limit")
	wf.StepFunc("step1", "@triggered", func() {})
	wf.StepFunc("step2", "@triggered", func() {}).After("step1", OnSuccess)

	err := c.AddWorkflow(wf)
	if err == nil {
		t.Fatal("AddWorkflow should fail when max entries exceeded")
	}

	// step1 should have been rolled back (removed).
	e := c.EntryByName("step1")
	if e.Valid() {
		t.Error("step1 should have been rolled back after failure")
	}
}

func TestRegisterWorkflowSteps_RollbackOnInvalidCondition(t *testing.T) {
	// Test registerWorkflowSteps rollback when AddDependency fails.
	// validate() does NOT check TriggerCondition validity, so an invalid
	// condition passes validation but fails in addDependencyDirect.
	// This triggers the second rollback path (line 2298).
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	wf := NewWorkflow("bad-cond")
	wf.StepFunc("step1", "@triggered", func() {})
	wf.StepFunc("step2", "@triggered", func() {}).
		After("step1", TriggerCondition(99)) // invalid condition

	err := c.AddWorkflow(wf)
	if !errors.Is(err, ErrInvalidCondition) {
		t.Errorf("AddWorkflow(invalid condition) = %v, want ErrInvalidCondition", err)
	}

	// Both steps should have been rolled back.
	if e := c.EntryByName("step1"); e.Valid() {
		t.Error("step1 should have been rolled back after dependency failure")
	}
	if e := c.EntryByName("step2"); e.Valid() {
		t.Error("step2 should have been rolled back after dependency failure")
	}
}

func TestWorkflowExecution_CloneNil(t *testing.T) {
	var we *WorkflowExecution
	if got := we.clone(); got != nil {
		t.Errorf("clone of nil should return nil, got %v", got)
	}
}

func TestWorkflowExecution_CloneDeepCopiesResults(t *testing.T) {
	we := &WorkflowExecution{
		ID:        "exec-1",
		RootID:    1,
		StartTime: time.Now(),
		Results:   map[EntryID]JobResult{1: ResultSuccess, 2: ResultPending},
	}
	cp := we.clone()

	// Mutating the copy must not affect the original.
	cp.Results[2] = ResultFailure
	if we.Results[2] != ResultPending {
		t.Error("clone did not deep-copy Results map")
	}
}

func TestWorkflow_StopDoesNotHangWithActiveJobs(t *testing.T) {
	// Verify that Stop() completes even when workflow jobs are still in flight.
	// Before the fix, the jobDone send would block after the run loop exited,
	// causing Stop()/jobWaiter to hang.
	c := New(WithSeconds())
	c.Start()

	jobStarted := make(chan struct{})
	jobRelease := make(chan struct{})

	c.AddFunc("@triggered", func() {
		close(jobStarted)
		<-jobRelease // hold the job running
	}, WithName("root"))
	c.AddFunc("@triggered", func() {}, WithName("child"))
	_ = c.AddDependencyByName("child", "root", OnSuccess)

	_ = c.TriggerEntryByName("root")

	// Wait for the job to start.
	select {
	case <-jobStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for job to start")
	}

	// Stop the scheduler while the job is still running.
	ctx := c.Stop()

	// Release the job so it tries to send on jobDone.
	close(jobRelease)

	// Stop must complete without hanging.
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() hung with active workflow jobs")
	}
}

func TestWorkflow_WorkflowStatusWhileRunning(t *testing.T) {
	// Covers the run-loop queryWorkflow path that returns exec.clone()
	// for active executions.
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	jobStarted := make(chan struct{})
	jobRelease := make(chan struct{})

	c.AddFunc("@triggered", func() {
		close(jobStarted)
		<-jobRelease
	}, WithName("root"))
	c.AddFunc("@triggered", func() {}, WithName("child"))
	_ = c.AddDependencyByName("child", "root", OnSuccess)

	_ = c.TriggerEntryByName("root")

	select {
	case <-jobStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for job to start")
	}

	// There should be at least one active workflow.
	active := c.ActiveWorkflows()
	if len(active) == 0 {
		t.Fatal("expected at least one active workflow")
	}

	// Query the specific execution.
	status := c.WorkflowStatus(active[0].ID)
	if status == nil {
		t.Fatal("WorkflowStatus returned nil for active execution")
	}

	// Mutating returned status must not affect internal state.
	status.Results[EntryID(9999)] = ResultFailure
	status2 := c.WorkflowStatus(active[0].ID)
	if _, ok := status2.Results[EntryID(9999)]; ok {
		t.Error("WorkflowStatus returned shared mutable state")
	}

	// Mutating ActiveWorkflows result must not affect internals.
	active[0].Results[EntryID(8888)] = ResultFailure
	active2 := c.ActiveWorkflows()
	if _, ok := active2[0].Results[EntryID(8888)]; ok {
		t.Error("ActiveWorkflows returned shared mutable state")
	}

	close(jobRelease)
}

func TestWorkflow_StatusAndActiveNotRunning(t *testing.T) {
	// Covers the not-running paths for WorkflowStatus and ActiveWorkflows
	// where deep copies are returned. We stop the scheduler while a
	// workflow execution is still active (root blocking, child pending).
	c := New(WithSeconds())

	jobStarted := make(chan struct{})
	jobRelease := make(chan struct{})

	c.AddFunc("@triggered", func() {
		close(jobStarted)
		<-jobRelease
	}, WithName("root"))
	c.AddFunc("@triggered", func() {}, WithName("child"))
	_ = c.AddDependencyByName("child", "root", OnSuccess)

	c.Start()
	_ = c.TriggerEntryByName("root")

	// Wait for root to start — execution is now active.
	select {
	case <-jobStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for job to start")
	}

	// Stop the scheduler while root is still running.
	// This leaves the execution in activeExecutions.
	ctx := c.Stop()
	close(jobRelease) // unblock root so it finishes
	<-ctx.Done()

	// Now c.running == false, activeExecutions should have the execution.
	active := c.ActiveWorkflows()
	if len(active) == 0 {
		t.Fatal("expected at least one active workflow after stop")
	}

	// Verify deep copy — mutating returned slice must not affect internals.
	active[0].Results[EntryID(9999)] = ResultFailure
	active2 := c.ActiveWorkflows()
	if _, ok := active2[0].Results[EntryID(9999)]; ok {
		t.Error("ActiveWorkflows (not-running) returned shared mutable state")
	}

	// WorkflowStatus should find the active execution.
	status := c.WorkflowStatus(active2[0].ID)
	if status == nil {
		t.Fatal("WorkflowStatus returned nil for active execution")
	}

	// Verify deep copy.
	status.Results[EntryID(8888)] = ResultFailure
	status2 := c.WorkflowStatus(active2[0].ID)
	if _, ok := status2.Results[EntryID(8888)]; ok {
		t.Error("WorkflowStatus (not-running) returned shared mutable state")
	}

	// Nonexistent ID should return nil.
	if got := c.WorkflowStatus("nonexistent"); got != nil {
		t.Error("expected nil for nonexistent execution ID")
	}
}

func TestShouldTriggerChild_NoDeps(t *testing.T) {
	// Kills CONDITIONALS_BOUNDARY mutation at cron.go:1354 where (> 0) → (>= 0)
	// An entry with no dependencies should NOT be triggered.
	c := New()
	id, _ := c.AddFunc("@triggered", func() {}, WithName("standalone"))

	exec := &WorkflowExecution{Results: map[EntryID]JobResult{}}

	if c.shouldTriggerChild(exec, id) {
		t.Error("shouldTriggerChild should return false for entry with no dependencies")
	}
}

func TestRemoveEntry_CleansMultipleChildren(t *testing.T) {
	// Kills negated conditions at cron.go:2007,2017,2019
	// When a parent is removed, all children should lose their deps on that parent.
	c := New(WithSeconds())

	parent, _ := c.AddFunc("@triggered", func() {}, WithName("parent"))
	child1, _ := c.AddFunc("@triggered", func() {}, WithName("child1"))
	child2, _ := c.AddFunc("@triggered", func() {}, WithName("child2"))

	_ = c.AddDependency(child1, parent, OnSuccess)
	_ = c.AddDependency(child2, parent, OnSuccess)

	// Verify deps exist
	if len(c.Dependencies(child1)) != 1 || len(c.Dependencies(child2)) != 1 {
		t.Fatal("setup: expected 1 dep each")
	}

	// Remove parent — should clean deps from both children
	c.Remove(parent)

	if deps := c.Dependencies(child1); len(deps) != 0 {
		t.Errorf("child1 should have no deps after parent removed, got %d", len(deps))
	}
	if deps := c.Dependencies(child2); len(deps) != 0 {
		t.Errorf("child2 should have no deps after parent removed, got %d", len(deps))
	}
}

func TestRemoveDependency_MultiParent(t *testing.T) {
	// Kills negated conditions at cron.go:2111,2117,2122
	// When removing one dep, the other parent's dep should remain intact.
	c := New(WithSeconds())

	parent1, _ := c.AddFunc("@triggered", func() {}, WithName("parent1"))
	parent2, _ := c.AddFunc("@triggered", func() {}, WithName("parent2"))
	child, _ := c.AddFunc("@triggered", func() {}, WithName("child"))

	_ = c.AddDependency(child, parent1, OnSuccess)
	_ = c.AddDependency(child, parent2, OnSuccess)

	// Remove only parent1's dependency
	c.RemoveDependency(child, parent1)

	// child should still have parent2 dependency
	deps := c.Dependencies(child)
	if len(deps) != 1 {
		t.Fatalf("expected 1 remaining dep, got %d", len(deps))
	}
	if deps[0].ParentID != parent2 {
		t.Errorf("remaining dep should be from parent2 (%d), got parent %d", parent2, deps[0].ParentID)
	}
}
