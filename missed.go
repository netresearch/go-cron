package cron

import "time"

// MissedPolicy defines how to handle jobs that were scheduled to run
// while the scheduler was not running (e.g., application restart).
//
// This feature requires the user to provide the last run time via WithPrev().
// The scheduler does NOT persist state - users are responsible for storing
// and loading last run times from their own persistence layer.
//
// Important interactions:
//   - WithRunOnce: Run-once jobs skip catch-up to avoid unintended duplicate runs
//   - SkipIfStillRunning wrapper: With MissedRunAll, catch-up jobs start nearly
//     simultaneously, so most may be skipped. Use MissedRunOnce for predictable behavior.
//
// Example usage:
//
//	// Load last run time from your database
//	lastRun := loadFromDatabase("daily-report")
//
//	c.AddFunc("0 9 * * *", dailyReport,
//	    cron.WithPrev(lastRun),                      // When it last ran
//	    cron.WithMissedPolicy(cron.MissedRunOnce),   // Run once if missed
//	    cron.WithMissedGracePeriod(2*time.Hour),     // Only if within 2 hours
//	)
type MissedPolicy int

const (
	// MissedSkip does not catch up on missed executions (default).
	// The job simply waits for its next scheduled time.
	MissedSkip MissedPolicy = iota

	// MissedRunOnce runs the job once immediately if any executions were missed.
	// Only the most recent missed execution time is used.
	// This is the safest catch-up policy for most use cases.
	//
	// After catch-up, Entry.Prev is updated to the caught-up time to prevent
	// duplicate catch-ups on subsequent restarts.
	MissedRunOnce

	// MissedRunAll executes the job for every missed execution time.
	// Use with caution: this can cause a burst of executions if the scheduler
	// was down for a long time. Consider using MissedRunOnce instead.
	//
	// A safety limit of 100 missed executions is enforced to prevent
	// runaway loops from misconfigured schedules.
	//
	// Note: All catch-up jobs start nearly simultaneously. If the job uses
	// SkipIfStillRunning wrapper, most catch-up runs will be skipped.
	// For sequential catch-up execution, implement custom logic using
	// DelayIfStillRunning or manage execution order in your job.
	//
	// After catch-up, Entry.Prev is updated to the most recent caught-up time.
	MissedRunAll
)

// maxMissedRuns is the safety limit for MissedRunAll to prevent infinite loops
// or excessive catch-up runs when the scheduler was down for a very long time.
const maxMissedRuns = 100

// String returns a human-readable representation of the MissedPolicy.
func (p MissedPolicy) String() string {
	switch p {
	case MissedSkip:
		return "Skip"
	case MissedRunOnce:
		return "RunOnce"
	case MissedRunAll:
		return "RunAll"
	default:
		return "Unknown"
	}
}

// Valid returns true if the policy is a known valid value.
func (p MissedPolicy) Valid() bool {
	return p >= MissedSkip && p <= MissedRunAll
}

// calculateMissedRuns determines which scheduled times were missed between
// the entry's Prev time and now. It respects the entry's MissedGracePeriod.
//
// Returns nil if:
//   - Prev is zero (no last run time provided)
//   - MissedPolicy is MissedSkip or invalid
//   - Entry is run-once (to avoid unintended duplicate runs)
//   - No executions were missed
//   - All missed executions are outside the grace period
func (c *Cron) calculateMissedRuns(e *Entry, now time.Time) []time.Time {
	// No catch-up without a known last run time
	if e.Prev.IsZero() {
		return nil
	}

	// Skip policy means no catch-up
	if e.MissedPolicy == MissedSkip {
		return nil
	}

	// Validate policy - log warning for invalid values
	if !e.MissedPolicy.Valid() {
		c.logger.Info("invalid missed policy, skipping catch-up",
			"entry", e.ID,
			"name", e.Name,
			"policy", int(e.MissedPolicy),
		)
		return nil
	}

	// Run-once jobs skip catch-up to avoid unintended duplicate runs
	// (the job would run for catch-up AND for its scheduled time)
	if e.runOnce {
		c.logger.Info("skipping catch-up for run-once job",
			"entry", e.ID,
			"name", e.Name,
		)
		return nil
	}

	var missed []time.Time
	t := e.Prev

	for len(missed) < maxMissedRuns {
		t = e.Schedule.Next(t)

		// No more scheduled times, or next time is at or after now
		if t.IsZero() || !t.Before(now) {
			break
		}

		// Check grace period: skip if the missed time is too old
		if e.MissedGracePeriod > 0 && now.Sub(t) > e.MissedGracePeriod {
			continue
		}

		missed = append(missed, t)
	}

	// Log warning if we hit the limit
	if len(missed) >= maxMissedRuns {
		c.logger.Info("warning: missed runs capped at safety limit",
			"entry", e.ID,
			"name", e.Name,
			"limit", maxMissedRuns,
			"consider", "using MissedRunOnce or shorter grace period",
		)
	}

	return missed
}

// handleMissedRuns executes catch-up runs based on the entry's MissedPolicy.
// For MissedRunOnce, only the most recent missed time is executed.
// For MissedRunAll, all missed times are executed (up to maxMissedRuns).
//
// After execution, Entry.Prev is updated to prevent duplicate catch-ups
// on subsequent scheduler restarts.
func (c *Cron) handleMissedRuns(e *Entry, missed []time.Time) {
	if len(missed) == 0 {
		return
	}

	switch e.MissedPolicy {
	case MissedRunOnce:
		// Run only for the most recent missed time
		lastMissed := missed[len(missed)-1]
		c.logger.Info("catching up missed execution",
			"entry", e.ID,
			"name", e.Name,
			"policy", "RunOnce",
			"scheduledTime", lastMissed,
			"missedCount", len(missed),
		)
		c.startJob(e.entryCtx, e.ID, e.Job, e.WrappedJob, lastMissed)
		// Update Prev to prevent duplicate catch-ups on restart
		e.Prev = lastMissed

	case MissedRunAll:
		c.logger.Info("catching up all missed executions",
			"entry", e.ID,
			"name", e.Name,
			"policy", "RunAll",
			"count", len(missed),
		)
		for _, scheduledTime := range missed {
			c.startJob(e.entryCtx, e.ID, e.Job, e.WrappedJob, scheduledTime)
		}
		// Update Prev to the most recent catch-up time
		e.Prev = missed[len(missed)-1]

	default:
		// This shouldn't happen due to validation in calculateMissedRuns,
		// but handle gracefully just in case
		c.logger.Info("unexpected missed policy in handleMissedRuns",
			"entry", e.ID,
			"policy", int(e.MissedPolicy),
		)
	}
}

// processMissedRuns checks for and handles missed job executions for an entry.
// This is called when the scheduler starts and when new entries are added.
func (c *Cron) processMissedRuns(e *Entry, now time.Time) {
	missed := c.calculateMissedRuns(e, now)
	c.handleMissedRuns(e, missed)
}
