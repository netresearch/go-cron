package cron

import "time"

// ConstantDelaySchedule represents a simple recurring duty cycle, e.g. "Every 5 minutes".
// It does not support jobs more frequent than once a second.
type ConstantDelaySchedule struct {
	Delay time.Duration
}

// Every returns a crontab Schedule that activates once every duration.
// Delays of less than a second are not supported (will round up to 1 second).
// Any fields less than a Second are truncated.
//
// For custom minimum intervals, use EveryWithMin instead.
func Every(duration time.Duration) ConstantDelaySchedule {
	return EveryWithMin(duration, time.Second)
}

// EveryWithMin returns a crontab Schedule that activates once every duration,
// with a configurable minimum interval.
//
// The minInterval parameter controls the minimum allowed duration:
//   - If minInterval > 0, durations below minInterval are rounded up to minInterval
//   - If minInterval <= 0, no minimum is enforced (allows sub-second intervals)
//
// Any fields less than a Second are truncated unless minInterval allows sub-second.
//
// Example:
//
//	// Standard usage (1 second minimum)
//	sched := EveryWithMin(500*time.Millisecond, time.Second) // rounds to 1s
//
//	// Sub-second intervals (for testing)
//	sched := EveryWithMin(100*time.Millisecond, 0) // allows 100ms
//
//	// Enforce minimum 1-minute intervals
//	sched := EveryWithMin(30*time.Second, time.Minute) // rounds to 1m
func EveryWithMin(duration, minInterval time.Duration) ConstantDelaySchedule {
	if minInterval > 0 && duration < minInterval {
		duration = minInterval
	}

	// Truncate sub-second precision unless sub-second intervals are allowed
	if minInterval >= time.Second || minInterval <= 0 && duration >= time.Second {
		duration -= time.Duration(duration.Nanoseconds()) % time.Second
	}

	return ConstantDelaySchedule{
		Delay: duration,
	}
}

// Next returns the next time this should be run.
// For delays of 1 second or more, this rounds to the next second boundary.
// For sub-second delays, no rounding is performed.
//
// If the delay is zero or negative (invalid), returns t + 1 second as a
// safe fallback to prevent CPU spin loops in the scheduler.
func (schedule ConstantDelaySchedule) Next(t time.Time) time.Time {
	// Defensive: prevent CPU spin loop if delay is zero or negative
	if schedule.Delay <= 0 {
		return t.Add(time.Second)
	}
	// For sub-second intervals, don't round to second boundary
	if schedule.Delay < time.Second {
		return t.Add(schedule.Delay)
	}
	// For second+ intervals, round to the second
	return t.Add(schedule.Delay - time.Duration(t.Nanosecond())*time.Nanosecond)
}

// Prev returns the previous activation time, earlier than the given time.
// For ConstantDelaySchedule, this simply subtracts the delay.
// If the delay is zero or negative (invalid), returns t - 1 second as a safe fallback.
func (schedule ConstantDelaySchedule) Prev(t time.Time) time.Time {
	// Defensive: prevent invalid results if delay is zero or negative
	if schedule.Delay <= 0 {
		return t.Add(-time.Second)
	}
	// For sub-second intervals, don't round to second boundary
	if schedule.Delay < time.Second {
		return t.Add(-schedule.Delay)
	}
	// For second+ intervals, round to the second boundary
	return t.Add(-schedule.Delay + time.Duration(t.Nanosecond())*time.Nanosecond)
}
