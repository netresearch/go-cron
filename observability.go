package cron

import (
	"maps"
	"time"
)

// ObservabilityHooks provides callbacks for monitoring cron operations.
// All callbacks are optional; nil callbacks are safely ignored.
//
// Hooks are called asynchronously in separate goroutines to prevent
// slow callbacks from blocking the scheduler. This means:
//   - Callbacks may execute slightly after the event occurred
//   - Callback execution order is not guaranteed across events
//   - Callbacks should be safe for concurrent execution
//
// If you need synchronous execution, use channels or sync primitives
// within your callback implementation.
//
// Example with Prometheus:
//
//	hooks := cron.ObservabilityHooks{
//	    OnJobStart: func(id cron.EntryID, name string, scheduled time.Time) {
//	        jobsStarted.WithLabelValues(name).Inc()
//	    },
//	    OnJobComplete: func(id cron.EntryID, name string, dur time.Duration, recovered any) {
//	        jobDuration.WithLabelValues(name).Observe(dur.Seconds())
//	        if recovered != nil {
//	            jobPanics.WithLabelValues(name).Inc()
//	        }
//	    },
//	}
//	c := cron.New(cron.WithObservability(hooks))
type ObservabilityHooks struct {
	// OnJobStart is called immediately before a job begins execution.
	// Parameters:
	//   - entryID: the unique identifier for the scheduled entry
	//   - name: job name (from NamedJob interface, or empty string)
	//   - scheduledTime: the time the job was scheduled to run
	OnJobStart func(entryID EntryID, name string, scheduledTime time.Time)

	// OnJobComplete is called when a job finishes execution.
	// Parameters:
	//   - entryID: the unique identifier for the scheduled entry
	//   - name: job name (from NamedJob interface, or empty string)
	//   - duration: how long the job took to execute
	//   - recovered: the value from recover() if the job panicked, or nil
	OnJobComplete func(entryID EntryID, name string, duration time.Duration, recovered any)

	// OnSchedule is called when a job's next execution time is calculated.
	// Parameters:
	//   - entryID: the unique identifier for the scheduled entry
	//   - name: job name (from NamedJob interface, or empty string)
	//   - nextRun: the next scheduled execution time
	OnSchedule func(entryID EntryID, name string, nextRun time.Time)

	// OnWorkflowComplete is called when all jobs in a workflow execution
	// have resolved (success, failure, or skipped).
	OnWorkflowComplete func(executionID string, rootID EntryID, results map[EntryID]JobResult)
}

// NamedJob is an optional interface that jobs can implement to provide
// a name for observability purposes. If a job doesn't implement this
// interface, an empty string is used for the name in hook callbacks.
type NamedJob interface {
	Job
	Name() string
}

// getJobName extracts the name from a job if it implements NamedJob,
// otherwise returns an empty string.
func getJobName(j Job) string {
	if nj, ok := j.(NamedJob); ok {
		return nj.Name()
	}
	return ""
}

// callOnJobStart safely calls the OnJobStart hook if configured.
// The hook is called asynchronously to prevent blocking job execution.
func (h *ObservabilityHooks) callOnJobStart(entryID EntryID, job Job, scheduledTime time.Time) {
	if h != nil && h.OnJobStart != nil {
		name := getJobName(job)
		go h.OnJobStart(entryID, name, scheduledTime)
	}
}

// callOnJobComplete safely calls the OnJobComplete hook if configured.
// The hook is called asynchronously to prevent blocking.
func (h *ObservabilityHooks) callOnJobComplete(entryID EntryID, job Job, duration time.Duration, recovered any) {
	if h != nil && h.OnJobComplete != nil {
		name := getJobName(job)
		go h.OnJobComplete(entryID, name, duration, recovered)
	}
}

// callOnSchedule safely calls the OnSchedule hook if configured.
// The hook is called asynchronously to prevent blocking the scheduler.
func (h *ObservabilityHooks) callOnSchedule(entryID EntryID, job Job, nextRun time.Time) {
	if h != nil && h.OnSchedule != nil {
		name := getJobName(job)
		go h.OnSchedule(entryID, name, nextRun)
	}
}

// callOnWorkflowComplete safely calls the OnWorkflowComplete hook if configured.
// The hook is called asynchronously to prevent blocking the scheduler.
func (h *ObservabilityHooks) callOnWorkflowComplete(executionID string, rootID EntryID, results map[EntryID]JobResult) {
	if h != nil && h.OnWorkflowComplete != nil {
		resultsCopy := maps.Clone(results)
		go h.OnWorkflowComplete(executionID, rootID, resultsCopy)
	}
}
