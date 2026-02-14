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

// String returns the human-readable name for the trigger condition.
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

// Valid reports whether c is a known trigger condition.
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
	// ResultPending means the job has not yet completed.
	ResultPending JobResult = iota
	// ResultSuccess means the job completed without error.
	ResultSuccess
	// ResultFailure means the job failed (returned an error or panicked).
	ResultFailure
	// ResultSkipped means the job was skipped because its trigger condition was not met.
	ResultSkipped
)

// String returns the human-readable name for the job result.
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

// IsTerminal reports whether the result represents a final state.
func (r JobResult) IsTerminal() bool {
	switch r {
	case ResultSuccess, ResultFailure, ResultSkipped:
		return true
	default:
		return false
	}
}

// Dependency represents a directed edge in the workflow DAG.
type Dependency struct {
	ParentID  EntryID
	Condition TriggerCondition
}

// WorkflowExecution tracks the state of a single workflow run.
// All fields are owned exclusively by the run() goroutine — no mutex needed.
type WorkflowExecution struct {
	ID        string
	RootID    EntryID
	StartTime time.Time
	Results   map[EntryID]JobResult
}

// IsComplete reports whether every job in the execution has reached a terminal state.
func (we *WorkflowExecution) IsComplete() bool {
	for _, r := range we.Results {
		if !r.IsTerminal() {
			return false
		}
	}
	return true
}

type workflowContextKey struct{}

// WorkflowExecutionID returns the workflow execution ID from the context,
// or empty string if the job is not part of a workflow.
func WorkflowExecutionID(ctx context.Context) string {
	if id, ok := ctx.Value(workflowContextKey{}).(string); ok {
		return id
	}
	return ""
}

// hasCycle checks whether adding an edge from newChild to newParent
// would create a cycle in the dependency graph. Uses DFS from newParent
// upward through the parent edges to see if newChild is reachable.
func hasCycle(deps map[EntryID][]Dependency, newChild, newParent EntryID) bool {
	if newChild == newParent {
		return true
	}

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
