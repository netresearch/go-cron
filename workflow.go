package cron

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"
)

// ErrEmptyWorkflow is returned by AddWorkflow when the workflow has no steps.
var ErrEmptyWorkflow = errors.New("cron: workflow has no steps")

// ErrMultipleFinalSteps is returned by AddWorkflow when more than one step is marked Final.
var ErrMultipleFinalSteps = errors.New("cron: workflow has multiple final steps")

// ErrUnknownStep is returned by AddWorkflow when a step references an unknown parent via After.
var ErrUnknownStep = errors.New("cron: workflow step references unknown parent")

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

// jobDoneEvent is sent from startJob goroutines to the run loop
// to report job completion for workflow orchestration.
type jobDoneEvent struct {
	EntryID     EntryID
	Panicked    bool
	PanicValue  any
	ExecutionID string // workflow execution ID, empty if standalone
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

// generateExecutionID creates a random UUID v4 for workflow execution tracking.
func generateExecutionID() string {
	var uuid [16]byte
	_, _ = rand.Read(uuid[:])
	uuid[6] = (uuid[6] & 0x0f) | 0x40 // version 4
	uuid[8] = (uuid[8] & 0x3f) | 0x80 // variant 2
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16])
}

// Workflow defines a multi-step DAG of named jobs with dependency edges.
// Use NewWorkflow to create a workflow, then Step/StepFunc to add steps,
// and AddWorkflow on a Cron instance to register it atomically.
type Workflow struct {
	Name  string
	steps []*WorkflowStep
}

// WorkflowStep is a single step within a Workflow.
type WorkflowStep struct {
	name    string
	spec    string
	job     Job
	deps    []stepDep
	isFinal bool
}

// stepDep represents a dependency edge within a workflow, using step names
// (since EntryIDs are not yet assigned at build time).
type stepDep struct {
	parentName string
	condition  TriggerCondition
}

// NewWorkflow creates a new Workflow with the given name.
func NewWorkflow(name string) *Workflow {
	return &Workflow{Name: name}
}

// Step adds a named step to the workflow with the given schedule spec and Job.
func (w *Workflow) Step(name, spec string, job Job) *WorkflowStep {
	s := &WorkflowStep{name: name, spec: spec, job: job}
	w.steps = append(w.steps, s)
	return s
}

// StepFunc adds a named step with a plain function as its job.
func (w *Workflow) StepFunc(name, spec string, fn func()) *WorkflowStep {
	return w.Step(name, spec, FuncJob(fn))
}

// After declares that this step depends on the named parent step with the given condition.
// Multiple After calls can be chained to create fan-in dependencies.
func (s *WorkflowStep) After(parentName string, condition TriggerCondition) *WorkflowStep {
	s.deps = append(s.deps, stepDep{parentName: parentName, condition: condition})
	return s
}

// Final marks this step as a finalization step. A final step receives an
// OnComplete edge from every non-final step, ensuring it runs after all
// other steps have resolved regardless of their outcome.
// At most one step per workflow may be marked Final.
func (s *WorkflowStep) Final() *WorkflowStep {
	s.isFinal = true
	return s
}

// validate checks the workflow's structural integrity: non-empty, at most one
// final step, no duplicate step names, all After references exist, and no cycles.
// It also expands the Final step by adding OnComplete edges from every non-final step.
func (w *Workflow) validate() error {
	if len(w.steps) == 0 {
		return ErrEmptyWorkflow
	}

	if err := w.validateFinalStep(); err != nil {
		return err
	}

	stepIndex, err := w.buildStepIndex()
	if err != nil {
		return err
	}

	return w.validateDeps(stepIndex)
}

// validateFinalStep checks for at most one final step and expands it
// by adding OnComplete edges from every non-final step.
func (w *Workflow) validateFinalStep() error {
	var finalStep *WorkflowStep
	for _, s := range w.steps {
		if s.isFinal {
			if finalStep != nil {
				return ErrMultipleFinalSteps
			}
			finalStep = s
		}
	}

	if finalStep != nil {
		for _, s := range w.steps {
			if s != finalStep {
				finalStep.deps = append(finalStep.deps, stepDep{
					parentName: s.name,
					condition:  OnComplete,
				})
			}
		}
	}
	return nil
}

// buildStepIndex builds a name index and checks for duplicate step names.
func (w *Workflow) buildStepIndex() (map[string]struct{}, error) {
	stepIndex := make(map[string]struct{}, len(w.steps))
	for _, s := range w.steps {
		if _, exists := stepIndex[s.name]; exists {
			return nil, ErrDuplicateName
		}
		stepIndex[s.name] = struct{}{}
	}
	return stepIndex, nil
}

// validateDeps checks that all After references exist and that there are no cycles.
func (w *Workflow) validateDeps(stepIndex map[string]struct{}) error {
	for _, s := range w.steps {
		for _, dep := range s.deps {
			if _, ok := stepIndex[dep.parentName]; !ok {
				return ErrUnknownStep
			}
		}
	}

	nameDeps := make(map[string][]string, len(w.steps))
	for _, s := range w.steps {
		for _, dep := range s.deps {
			nameDeps[s.name] = append(nameDeps[s.name], dep.parentName)
		}
		if _, ok := nameDeps[s.name]; !ok {
			nameDeps[s.name] = nil
		}
	}
	if hasCycleByName(nameDeps) {
		return ErrCycleDetected
	}

	return nil
}

// hasCycleByName checks whether a name-based adjacency list contains a cycle.
// Used during AddWorkflow validation before EntryIDs are assigned.
// deps maps step name -> list of parent names.
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
