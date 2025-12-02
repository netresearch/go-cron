package cron

import (
	"context"
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
//	fakeClock := cron.NewFakeClock(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
//	c := cron.New(cron.WithClock(fakeClock))
//	// ... add jobs ...
//	c.Start()
//	fakeClock.Advance(time.Hour) // Advance time and trigger jobs deterministically
func WithClock(clock Clock) Option {
	return func(c *Cron) {
		c.clock = clock
	}
}

// WithObservability configures observability hooks for monitoring cron operations.
// Hooks are called asynchronously in separate goroutines to prevent slow callbacks
// from blocking the scheduler. This means callback execution order is not guaranteed.
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

// WithMaxSearchYears configures the maximum years into the future that schedule
// matching will search before giving up. This prevents infinite loops for
// unsatisfiable schedules (e.g., Feb 30).
//
// The default is 5 years. Values <= 0 will use the default.
//
// Use cases:
//   - Shorter limits for faster failure detection: WithMaxSearchYears(1)
//   - Longer limits for rare schedules: WithMaxSearchYears(10)
//
// Note: This option replaces the current parser. If you need custom parser options
// along with a custom max search years, use WithParser with a manually configured parser:
//
//	p := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor).
//	    WithMaxSearchYears(10)
//	c := cron.New(cron.WithParser(p))
//
// Example:
//
//	// Allow searching up to 10 years for rare schedules
//	c := cron.New(cron.WithMaxSearchYears(10))
//	c.AddFunc("0 0 13 * 5", func() { ... }) // Friday the 13th
func WithMaxSearchYears(years int) Option {
	return func(c *Cron) {
		c.parser = StandardParser().WithMaxSearchYears(years)
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

// WithContext sets the base context for all job executions.
// When Stop() is called, this context is canceled, signaling all running
// jobs that implement JobWithContext to shut down gracefully.
//
// If not specified, context.Background() is used as the base context.
//
// Use cases:
//   - Propagate application-wide cancellation to cron jobs
//   - Attach tracing context or correlation IDs to all jobs
//   - Integrate with application lifecycle management
//
// Example:
//
//	// Create cron with application context
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//	c := cron.New(cron.WithContext(ctx))
//
//	// Jobs implementing JobWithContext will receive this context
//	c.AddJob("@every 1m", cron.FuncJobWithContext(func(ctx context.Context) {
//	    select {
//	    case <-ctx.Done():
//	        return // Application shutting down
//	    default:
//	        // Do work
//	    }
//	}))
func WithContext(ctx context.Context) Option {
	return func(c *Cron) {
		c.baseCtx = ctx
	}
}
