package cron

import "time"

// ObservabilityHooks provides callbacks for monitoring cron operations.
// All callbacks are optional; nil callbacks are safely ignored.
//
// Hooks are called synchronously, so implementations should be lightweight
// or dispatch to a separate goroutine for expensive operations.
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
func (h *ObservabilityHooks) callOnJobStart(entryID EntryID, job Job, scheduledTime time.Time) {
	if h != nil && h.OnJobStart != nil {
		h.OnJobStart(entryID, getJobName(job), scheduledTime)
	}
}

// callOnJobComplete safely calls the OnJobComplete hook if configured.
func (h *ObservabilityHooks) callOnJobComplete(entryID EntryID, job Job, duration time.Duration, recovered any) {
	if h != nil && h.OnJobComplete != nil {
		h.OnJobComplete(entryID, getJobName(job), duration, recovered)
	}
}

// callOnSchedule safely calls the OnSchedule hook if configured.
func (h *ObservabilityHooks) callOnSchedule(entryID EntryID, job Job, nextRun time.Time) {
	if h != nil && h.OnSchedule != nil {
		h.OnSchedule(entryID, getJobName(job), nextRun)
	}
}
