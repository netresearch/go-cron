package cron

import (
	"context"
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
