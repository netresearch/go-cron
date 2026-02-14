package cron

import (
	"context"
	"errors"
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
