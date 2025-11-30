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

// WithClock uses the provided Clock implementation instead of the default RealClock.
// This is useful for testing time-dependent behavior without waiting.
//
// The Clock interface provides both Now() for current time and NewTimer() for
// creating timers, enabling fully deterministic testing of scheduled jobs.
//
// Example usage:
//
//	// For testing with FakeClock (recommended)
//	fakeClock := cron.NewFakeClock(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
//	c := cron.New(cron.WithClock(fakeClock))
//	// ... add jobs ...
//	c.Start()
//	fakeClock.Advance(time.Hour) // Advance time and trigger jobs deterministically
//
//	// For backward compatibility with the old function-based Clock
//	c := cron.New(cron.WithClock(cron.ClockFunc(time.Now)))
func WithClock(clock Clock) Option {
	return func(c *Cron) {
		c.clock = clock
	}
}

// WithObservability configures observability hooks for monitoring cron operations.
// Hooks are called synchronously at various points during job execution lifecycle.
//
// All hook callbacks are optional; nil callbacks are safely ignored.
//
// Example with Prometheus metrics:
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
func WithObservability(hooks ObservabilityHooks) Option {
	return func(c *Cron) {
		c.hooks = &hooks
	}
}

// WithMinEveryInterval configures the minimum interval allowed for @every expressions.
// This allows overriding the default 1-second minimum.
//
// Use cases:
//   - Sub-second intervals for testing: WithMinEveryInterval(0) or WithMinEveryInterval(100*time.Millisecond)
//   - Enforce longer minimums for rate limiting: WithMinEveryInterval(time.Minute)
//
// The interval affects:
//   - Parsing of "@every <duration>" expressions
//   - The EveryWithMin function when called via the parser
//
// Note: This option replaces the current parser. If you need custom parser options
// along with a custom minimum interval, use WithParser with a manually configured parser:
//
//	p := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor).
//	    WithMinEveryInterval(100 * time.Millisecond)
//	c := cron.New(cron.WithParser(p))
//
// Example:
//
//	// Allow sub-second intervals (useful for testing)
//	c := cron.New(cron.WithMinEveryInterval(0))
//	c.AddFunc("@every 100ms", func() { ... })
//
//	// Enforce minimum 1-minute intervals
//	c := cron.New(cron.WithMinEveryInterval(time.Minute))
//	c.AddFunc("@every 30s", func() { ... }) // Error: must be at least 1 minute
func WithMinEveryInterval(d time.Duration) Option {
	return func(c *Cron) {
		c.parser = StandardParser().WithMinEveryInterval(d)
	}
}

// WithMaxEntries limits the maximum number of entries that can be added to the Cron.
// When the limit is reached:
//   - AddFunc and AddJob return ErrMaxEntriesReached
//   - Schedule returns 0 (invalid EntryID) and logs an error
//
// A limit of 0 (the default) means unlimited entries.
//
// This option provides protection against memory exhaustion from excessive
// entry additions, which could occur from buggy code or untrusted input.
//
// Note: When the cron is running, the limit enforcement is approximate due to
// concurrent entry additions. The actual count may briefly exceed the limit.
//
// Example usage:
//
//	c := cron.New(cron.WithMaxEntries(1000))
//	for i := 0; i < 2000; i++ {
//	    _, err := c.AddFunc("* * * * *", func() {})
//	    if errors.Is(err, cron.ErrMaxEntriesReached) {
//	        log.Println("Entry limit reached")
//	        break
//	    }
//	}
func WithMaxEntries(max int) Option {
	return func(c *Cron) {
		c.maxEntries = max
	}
}
