package cron

import "time"

// MissedPolicy defines how to handle jobs that were scheduled to run
// while the scheduler was not running (e.g., application restart).
//
// This feature requires the user to provide the last run time via WithPrev().
// The scheduler does NOT persist state - users are responsible for storing
// and loading last run times from their own persistence layer.
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
	MissedRunOnce

	// MissedRunAll executes the job for every missed execution time.
	// Use with caution: this can cause a burst of executions if the scheduler
	// was down for a long time. Consider using MissedRunOnce instead.
	//
	// A safety limit of 100 missed executions is enforced to prevent
	// runaway loops from misconfigured schedules.
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

// calculateMissedRuns determines which scheduled times were missed between
// the entry's Prev time and now. It respects the entry's MissedGracePeriod.
//
// Returns nil if:
//   - Prev is zero (no last run time provided)
//   - MissedPolicy is MissedSkip
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
		c.logger.Info("missed runs capped at limit",
			"entry", e.ID,
			"name", e.Name,
			"limit", maxMissedRuns,
		)
	}

	return missed
}

// handleMissedRuns executes catch-up runs based on the entry's MissedPolicy.
// For MissedRunOnce, only the most recent missed time is executed.
// For MissedRunAll, all missed times are executed (up to maxMissedRuns).
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
		c.startJob(e.ID, e.Job, e.WrappedJob, lastMissed)

	case MissedRunAll:
		c.logger.Info("catching up all missed executions",
			"entry", e.ID,
			"name", e.Name,
			"policy", "RunAll",
			"count", len(missed),
		)
		for _, scheduledTime := range missed {
			c.startJob(e.ID, e.Job, e.WrappedJob, scheduledTime)
		}
	}
}

// processMissedRuns checks for and handles missed job executions for an entry.
// This is called when the scheduler starts and when new entries are added.
func (c *Cron) processMissedRuns(e *Entry, now time.Time) {
	missed := c.calculateMissedRuns(e, now)
	c.handleMissedRuns(e, missed)
}
