package cron

import (
	"time"
)

// Option represents a modification to the default behavior of a Cron.
type Option func(*Cron)

// WithLocation overrides the timezone of the cron instance.
func WithLocation(loc *time.Location) Option {
	return func(c *Cron) {
		c.location = loc
	}
}

// WithSeconds overrides the parser used for interpreting job schedules to
// include a seconds field as the first one.
func WithSeconds() Option {
	return WithParser(NewParser(
		Second | Minute | Hour | Dom | Month | Dow | Descriptor,
	))
}

// WithParser overrides the parser used for interpreting job schedules.
func WithParser(p ScheduleParser) Option {
	return func(c *Cron) {
		c.parser = p
	}
}

// WithChain specifies Job wrappers to apply to all jobs added to this cron.
// Refer to the Chain* functions in this package for provided wrappers.
func WithChain(wrappers ...JobWrapper) Option {
	return func(c *Cron) {
		c.chain = NewChain(wrappers...)
	}
}

// WithLogger uses the provided logger.
func WithLogger(logger Logger) Option {
	return func(c *Cron) {
		c.logger = logger
	}
}

// WithClock uses the provided clock function instead of time.Now.
// This is useful for testing time-dependent behavior without waiting.
//
// Example usage:
//
//	// For testing, use a fixed time
//	fixedTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
//	c := cron.New(cron.WithClock(func() time.Time { return fixedTime }))
//
//	// Or advance time manually
//	var currentTime atomic.Value
//	currentTime.Store(time.Now())
//	c := cron.New(cron.WithClock(func() time.Time { return currentTime.Load().(time.Time) }))
func WithClock(clock Clock) Option {
	return func(c *Cron) {
		c.clock = clock
	}
}
