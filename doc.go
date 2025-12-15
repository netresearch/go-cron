/*
Package cron implements a cron spec parser and job runner.

# Installation

To download the package, run:

	go get github.com/netresearch/go-cron

Import it in your program as:

	import "github.com/netresearch/go-cron"

It requires Go 1.25 or later.

# Usage

Callers may register Funcs to be invoked on a given schedule.  Cron will run
them in their own goroutines.

	c := cron.New()
	c.AddFunc("30 * * * *", func() { fmt.Println("Every hour on the half hour") })
	c.AddFunc("30 3-6,20-23 * * *", func() { fmt.Println(".. in the range 3-6am, 8-11pm") })
	c.AddFunc("CRON_TZ=Asia/Tokyo 30 04 * * *", func() { fmt.Println("Runs at 04:30 Tokyo time every day") })
	c.AddFunc("@hourly",      func() { fmt.Println("Every hour, starting an hour from now") })
	c.AddFunc("@every 1h30m", func() { fmt.Println("Every hour thirty, starting an hour thirty from now") })
	c.Start()
	..
	// Funcs are invoked in their own goroutine, asynchronously.
	...
	// Funcs may also be added to a running Cron
	c.AddFunc("@daily", func() { fmt.Println("Every day") })
	..
	// Inspect the cron job entries' next and previous run times.
	inspect(c.Entries())
	..
	c.Stop()  // Stop the scheduler (does not stop any jobs already running).

# CRON Expression Format

A cron expression represents a set of times, using 5 space-separated fields.

	Field name   | Mandatory? | Allowed values  | Allowed special characters
	----------   | ---------- | --------------  | --------------------------
	Minutes      | Yes        | 0-59            | * / , -
	Hours        | Yes        | 0-23            | * / , -
	Day of month | Yes        | 1-31            | * / , - ?
	Month        | Yes        | 1-12 or JAN-DEC | * / , -
	Day of week  | Yes        | 0-6 or SUN-SAT  | * / , - ?

Month and Day-of-week field values are case insensitive.  "SUN", "Sun", and
"sun" are equally accepted.

The specific interpretation of the format is based on the Cron Wikipedia page:
https://en.wikipedia.org/wiki/Cron

# Alternative Formats

Alternative Cron expression formats support other fields like seconds. You can
implement that by creating a custom Parser as follows.

	cron.New(
		cron.WithParser(
			cron.NewParser(
				cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)))

Since adding Seconds is the most common modification to the standard cron spec,
cron provides a builtin function to do that, which is equivalent to the custom
parser you saw earlier, except that its seconds field is REQUIRED:

	cron.New(cron.WithSeconds())

That emulates Quartz, the most popular alternative Cron schedule format:
http://www.quartz-scheduler.org/documentation/quartz-2.x/tutorials/crontrigger.html

# Special Characters

Asterisk ( * )

The asterisk indicates that the cron expression will match for all values of the
field; e.g., using an asterisk in the 5th field (month) would indicate every
month.

Slash ( / )

Slashes are used to describe increments of ranges. For example 3-59/15 in the
1st field (minutes) would indicate the 3rd minute of the hour and every 15
minutes thereafter. The form "*\/..." is equivalent to the form "first-last/...",
that is, an increment over the largest possible range of the field.  The form
"N/..." is accepted as meaning "N-MAX/...", that is, starting at N, use the
increment until the end of that specific range.  It does not wrap around.

Comma ( , )

Commas are used to separate items of a list. For example, using "MON,WED,FRI" in
the 5th field (day of week) would mean Mondays, Wednesdays and Fridays.

Hyphen ( - )

Hyphens are used to define ranges. For example, 9-17 would indicate every
hour between 9am and 5pm inclusive.

Question mark ( ? )

Question mark may be used instead of '*' for leaving either day-of-month or
day-of-week blank.

# Extended Syntax (Optional)

The following extended syntax is available when enabled via parser options.
These provide Quartz/Jenkins-style cron expression features.

To enable extended syntax, use the Extended parser option:

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Extended)
	c := cron.New(cron.WithParser(parser))

Or enable individual features:

	// Enable only L syntax for last day of month
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.DomL)

	// Enable nth weekday syntax (#n)
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.DowNth)

Hash-Number ( #n ) - Day of Week Field

Used in the day-of-week field to specify the nth occurrence of a weekday in a month.
Requires DowNth option to be enabled.

	FRI#3    - Third Friday of every month
	MON#1    - First Monday of every month
	0#2      - Second Sunday of every month

Hash-L ( #L ) - Day of Week Field

Used in the day-of-week field to specify the last occurrence of a weekday in a month.
Requires DowLast option to be enabled.

	FRI#L    - Last Friday of every month
	SUN#L    - Last Sunday of every month
	1#L      - Last Monday of every month

# L ( L ) - Day of Month Field

Specifies the last day of the month. Requires DomL option to be enabled.

	L        - Last day of every month (Jan 31, Feb 28/29, etc.)
	L-3      - Third from last day of month
	L-1      - Second to last day of month

# W ( W ) - Day of Month Field

Specifies the nearest weekday to a given day. Requires DomW option to be enabled.
The nearest weekday stays within the same month (won't cross month boundaries).

	15W      - Nearest weekday to the 15th
	1W       - Nearest weekday to the 1st (could be Mon/Tue/Wed if 1st is weekend)
	LW       - Last weekday of the month

Combined Examples

	0 12 L * *        - Noon on the last day of every month
	0 12 L-3 * *      - Noon on the third from last day of every month
	0 12 LW * *       - Noon on the last weekday of every month
	0 12 15W * *      - Noon on the nearest weekday to the 15th
	0 12 * * FRI#3    - Noon on the third Friday of every month
	0 12 * * MON#L    - Noon on the last Monday of every month
	0 12 1,15,L * *   - Noon on the 1st, 15th, and last day of every month

# Predefined schedules

You may use one of several pre-defined schedules in place of a cron expression.

	Entry                  | Description                                | Equivalent To
	-----                  | -----------                                | -------------
	@yearly (or @annually) | Run once a year, midnight, Jan. 1st        | 0 0 1 1 *
	@monthly               | Run once a month, midnight, first of month | 0 0 1 * *
	@weekly                | Run once a week, midnight between Sat/Sun  | 0 0 * * 0
	@daily (or @midnight)  | Run once a day, midnight                   | 0 0 * * *
	@hourly                | Run once an hour, beginning of hour        | 0 * * * *

# Intervals

You may also schedule a job to execute at fixed intervals, starting at the time it's added
or cron is run. This is supported by formatting the cron spec like this:

	@every <duration>

where "duration" is a string accepted by time.ParseDuration
(http://golang.org/pkg/time/#ParseDuration).

For example, "@every 1h30m10s" would indicate a schedule that activates after
1 hour, 30 minutes, 10 seconds, and then every interval after that.

Note: The interval does not take the job runtime into account.  For example,
if a job takes 3 minutes to run, and it is scheduled to run every 5 minutes,
it will have only 2 minutes of idle time between each run.

# Time zones

By default, all interpretation and scheduling is done in the machine's local
time zone (time.Local). You can specify a different time zone on construction:

	cron.New(
	    cron.WithLocation(time.UTC))

Individual cron schedules may also override the time zone they are to be
interpreted in by providing an additional space-separated field at the beginning
of the cron spec, of the form "CRON_TZ=Asia/Tokyo".

For example:

	# Runs at 6am in time.Local
	cron.New().AddFunc("0 6 * * ?", ...)

	# Runs at 6am in America/New_York
	nyc, _ := time.LoadLocation("America/New_York")
	c := cron.New(cron.WithLocation(nyc))
	c.AddFunc("0 6 * * ?", ...)

	# Runs at 6am in Asia/Tokyo
	cron.New().AddFunc("CRON_TZ=Asia/Tokyo 0 6 * * ?", ...)

	# Runs at 6am in Asia/Tokyo, overriding the cron's default location
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	c := cron.New(cron.WithLocation(tokyo))
	c.AddFunc("0 6 * * ?", ...)

The prefix "TZ=(TIME ZONE)" is also supported for legacy compatibility.

Jobs scheduled during daylight-savings leap-ahead transitions will run
immediately after the skipped hour (ISC cron-compatible behavior).

# Daylight Saving Time (DST) Handling

This library follows ISC cron-compatible DST behavior. Understanding these edge
cases is critical for time-sensitive scheduling.

Spring Forward (clocks skip an hour):
  - Jobs scheduled during the skipped hour run immediately after the transition
  - Example: A 2:30 AM job during US spring DST runs at 3:00 AM
  - Jobs scheduled exactly at the transition boundary may run immediately

Fall Back (clocks repeat an hour):
  - Jobs run only during the first occurrence of the repeated hour
  - The second occurrence is skipped to prevent duplicate runs
  - ⚠️ Note: This means jobs scheduled in the repeated hour run once, not twice

Midnight Doesn't Exist:
  - Some DST transitions skip midnight entirely (e.g., São Paulo, Brazil)
  - Jobs scheduled at midnight run at the first valid time after transition
  - This affects daily (@daily) and midnight-scheduled jobs in those timezones

30-Minute Offset Timezones:
  - Some regions (e.g., Lord Howe Island, Australia) use 30-minute DST changes
  - The same DST handling rules apply, but at 30-minute boundaries

⚠️ Important Edge Cases:
  - Jobs during spring-forward gap: Run immediately after transition
  - Jobs during fall-back repeat: Run only on first occurrence
  - Multi-timezone systems: Each job uses its configured timezone independently
  - Leap seconds: Not handled; use NTP-synced systems for best results

Testing DST scenarios:

	// Use FakeClock for deterministic DST testing
	loc, _ := time.LoadLocation("America/New_York")
	// Start just before spring DST transition (2024: March 10, 2:00 AM)
	clock := cron.NewFakeClock(time.Date(2024, 3, 10, 1, 59, 0, 0, loc))
	c := cron.New(cron.WithClock(clock), cron.WithLocation(loc))
	// ... test behavior

Best practices for DST-sensitive schedules:
  - Use UTC (CRON_TZ=UTC) for critical jobs that must run exactly once
  - Use explicit timezones (CRON_TZ=America/New_York) rather than local time
  - Avoid scheduling jobs between 2:00-3:00 AM in DST-observing timezones
  - Test with FakeClock around DST transitions before production deployment
  - Consider using @every intervals for tasks where exact wall-clock time is less important
  - Monitor job execution times during DST transition periods

# Error Handling

Jobs in go-cron signal failure by panicking rather than returning errors. This design:

  - Keeps the Job interface simple (Run() has no return value)
  - Enables consistent recovery and retry behavior via wrapper chains
  - Allows adding retry/circuit-breaker logic without modifying job code
  - Matches Go's convention of panicking for unrecoverable errors

Best practices:

  - Use panic() for transient failures that should trigger retries
  - Use log-and-continue for errors that shouldn't affect the next run
  - Always wrap jobs with Recover() to prevent scheduler crashes
  - Combine with RetryWithBackoff for automatic retry of transient failures
  - Use CircuitBreaker to prevent hammering failing external services

Error flow through wrapper chain:

	Recover → CircuitBreaker → RetryWithBackoff → Job
	   ↑           ↑                 ↑              │
	   │           │                 └── catches ───┤ (panic)
	   │           └── tracks/opens ────────────────┤ (panic)
	   └── logs/swallows ───────────────────────────┘ (panic)

# Job Wrappers

A Cron runner may be configured with a chain of job wrappers to add
cross-cutting functionality to all submitted jobs. For example, they may be used
to achieve the following effects:

  - Recover any panics from jobs
  - Delay a job's execution if the previous run hasn't completed yet
  - Skip a job's execution if the previous run hasn't completed yet
  - Log each job's invocations
  - Add random delay (jitter) to prevent thundering herd

Install wrappers for all jobs added to a cron using the `cron.WithChain` option:

	cron.New(cron.WithChain(
		cron.Recover(logger),  // Recommended: recover panics to prevent crashes
		cron.SkipIfStillRunning(logger),
	))

Install wrappers for individual jobs by explicitly wrapping them:

	job = cron.NewChain(
		cron.SkipIfStillRunning(logger),
	).Then(job)

# Wrapper Composition Patterns

Wrappers are applied in reverse order (outermost first). Understanding the correct
ordering is critical for proper behavior:

Production-Ready Chain (recommended):

	c := cron.New(cron.WithChain(
		cron.Recover(logger),              // 1. Outermost: catches all panics
		cron.RetryWithBackoff(logger, 3,   // 2. Retry transient failures
			time.Second, time.Minute, 2.0),
		cron.CircuitBreaker(logger, 5,     // 3. Stop hammering failing services
			5*time.Minute),
		cron.SkipIfStillRunning(logger),   // 4. Innermost: prevent overlap
	))

Context-Aware Chain (for graceful shutdown):

	c := cron.New(cron.WithChain(
		cron.Recover(logger),
		cron.TimeoutWithContext(logger, 5*time.Minute),
	))
	c.AddJob("@every 1h", cron.FuncJobWithContext(func(ctx context.Context) {
		select {
		case <-ctx.Done():
			return // Shutdown or timeout - exit gracefully
		case <-doWork():
			// Work completed
		}
	}))

Wrapper Ordering Pitfalls:

	// BAD: Retry inside Recover loses panic information
	cron.NewChain(cron.RetryWithBackoff(...), cron.Recover(logger))

	// GOOD: Recover catches re-panics from exhausted retries
	cron.NewChain(cron.Recover(logger), cron.RetryWithBackoff(...))

Available Wrappers:
  - Recover: Catches panics and logs them
  - SkipIfStillRunning: Skip if previous run is still active
  - DelayIfStillRunning: Queue runs, serializing execution
  - Timeout: Abandon long-running jobs (see caveats below)
  - TimeoutWithContext: True cancellation via context
  - RetryWithBackoff: Retry panicking jobs with exponential backoff
  - CircuitBreaker: Stop execution after consecutive failures
  - Jitter: Add random delay to prevent thundering herd

# Timeout Wrapper Caveats

The Timeout wrapper uses an "abandonment model" - when a job exceeds its timeout,
the wrapper returns but the job's goroutine continues running in the background.
This design has important implications:

  - The job is NOT canceled; it runs to completion even after timeout
  - Resources held by the job are not released until the job naturally completes
  - Side effects (database writes, API calls) still occur after timeout
  - Multiple abandoned goroutines can accumulate if jobs consistently timeout

This is the only practical approach without context.Context support in the Job
interface. For jobs that need true cancellation:

  - Implement your own cancellation mechanism using channels or atomic flags
  - Have your job check for cancellation signals at safe points
  - Consider using shorter timeout values as a circuit breaker rather than for cancellation

Example of a cancellable job pattern:

	type CancellableJob struct {
		cancel chan struct{}
	}

	func (j *CancellableJob) Run() {
		for {
			select {
			case <-j.cancel:
				return // Clean exit on cancellation
			default:
				// Do work in small chunks
				if done := doWorkChunk(); done {
					return
				}
			}
		}
	}

# Thread Safety

Cron is safe for concurrent use. Multiple goroutines may call methods on a Cron
instance simultaneously without external synchronization.

Specific guarantees:
  - AddJob/AddFunc: Safe to call while scheduler is running
  - Remove: Safe to call while scheduler is running
  - Entries: Returns a snapshot; safe but may be stale
  - Start/Stop: Safe to call multiple times (idempotent)
  - Entry: Safe to call; returns copy of entry data

Job Execution:
  - Jobs may run concurrently by default
  - Use SkipIfStillRunning or DelayIfStillRunning for serialization
  - Jobs should not block indefinitely (use Timeout or TimeoutWithContext)

The scheduler uses an internal channel-based synchronization model. All
operations that modify scheduler state are serialized through this channel.

# Logging

Cron defines a Logger interface that is a subset of the one defined in
github.com/go-logr/logr. It has two logging levels (Info and Error), and
parameters are key/value pairs. This makes it possible for cron logging to plug
into structured logging systems. An adapter, [Verbose]PrintfLogger, is provided
to wrap the standard library *log.Logger.

For additional insight into Cron operations, verbose logging may be activated
which will record job runs, scheduling decisions, and added or removed jobs.
Activate it with a one-off logger as follows:

	cron.New(
		cron.WithLogger(
			cron.VerbosePrintfLogger(log.New(os.Stdout, "cron: ", log.LstdFlags))))

# Run-Once Jobs

Run-once jobs execute exactly once at their scheduled time and are automatically
removed from the scheduler after execution. This is useful for:

  - One-time maintenance tasks (schema migrations, cleanup jobs)
  - Deferred execution triggered by user actions
  - Temporary scheduled events (promotions, time-limited features)
  - Testing and debugging scheduled behavior

Using the WithRunOnce option:

	c := cron.New()
	c.Start()

	// Job runs at next matching time, then removes itself
	c.AddFunc("0 3 * * *", migrateDatabase, cron.WithRunOnce())

	// Combining with other options
	c.AddFunc("@every 5m", sendReminder, cron.WithRunOnce(), cron.WithName("reminder"))

Convenience methods for cleaner code:

	// These are equivalent:
	c.AddFunc("@hourly", task, cron.WithRunOnce())
	c.AddOnceFunc("@hourly", task)

	// For Job interface implementations:
	c.AddOnceJob("@daily", myJob)

	// For pre-parsed schedules:
	c.ScheduleOnceJob(cron.Every(time.Hour), myJob)

Run-once with immediate execution:

	// Run immediately AND only once - useful for deferred tasks
	c.AddFunc("@hourly", processOrder, cron.WithRunOnce(), cron.WithRunImmediately())

Behavior notes:
  - The entry is removed AFTER the job is dispatched (job continues in its goroutine)
  - Works correctly with Recover, RetryWithBackoff, and other wrappers
  - Entry removal is logged at Info level: "run-once", "entry", id, "removed", true
  - Manual Remove() before execution prevents the job from running
  - Entry count decrements immediately upon removal

# Resource Management

Use WithMaxEntries to limit the number of scheduled jobs and prevent resource exhaustion:

	c := cron.New(cron.WithMaxEntries(100))
	id, err := c.AddFunc("@every 1m", myJob)
	if errors.Is(err, cron.ErrMaxEntriesReached) {
		// Handle limit reached - remove old jobs or reject new ones
	}

Behavior when limit is reached:
  - AddFunc, AddJob, ScheduleJob return ErrMaxEntriesReached
  - Existing jobs continue running normally
  - Counter decrements when jobs are removed via Remove(id)

The entry limit is checked atomically but may briefly exceed the limit during
concurrent additions by the number of in-flight ScheduleJob calls.

# Observability Hooks

ObservabilityHooks provide integration points for metrics, tracing, and monitoring:

	hooks := cron.ObservabilityHooks{
		OnSchedule: func(entryID cron.EntryID, name string, nextRun time.Time) {
			// Called when a job's next execution time is calculated
			log.Printf("Job %d (%s) scheduled for %v", entryID, name, nextRun)
		},
		OnJobStart: func(entryID cron.EntryID, name string, scheduledTime time.Time) {
			// Called just before a job starts running
			metrics.IncrCounter("cron.job.started", "job", name)
		},
		OnJobComplete: func(entryID cron.EntryID, name string, duration time.Duration, recovered any) {
			// Called after a job completes (successfully or with panic)
			metrics.RecordDuration("cron.job.duration", duration, "job", name)
			if recovered != nil {
				metrics.IncrCounter("cron.job.panic", "job", name)
			}
		},
	}
	c := cron.New(cron.WithObservability(hooks))

# Testing with FakeClock

FakeClock enables deterministic time control for testing cron jobs:

	func TestJobExecution(t *testing.T) {
		clock := cron.NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
		c := cron.New(cron.WithClock(clock))

		executed := make(chan struct{})
		c.AddFunc("@every 1h", func() { close(executed) })
		c.Start()
		defer c.Stop()

		// Advance time to trigger job
		clock.Advance(time.Hour)

		select {
		case <-executed:
			// Success
		case <-time.After(time.Second):
			t.Fatal("job not executed")
		}
	}

FakeClock methods:
  - NewFakeClock(initial time.Time): Create clock at specific time
  - Advance(d time.Duration): Move time forward, triggering timers
  - Set(t time.Time): Jump to specific time
  - BlockUntil(n int): Wait for n timers to be registered
  - Now(), Since(), After(), AfterFunc(), NewTicker(), Sleep(): Standard time operations

Use BlockUntil for synchronization in tests with multiple timers:

	clock.BlockUntil(2) // Wait for 2 timers to be registered
	clock.Advance(time.Hour) // Now safely advance

# Security Considerations

Input Validation:
  - Cron specifications are limited to 1024 characters (MaxSpecLength)
  - Timezone specifications are validated against Go's time.LoadLocation
  - Path traversal attempts in timezone strings (e.g., "../etc/passwd") are rejected

Resource Protection:
  - Use WithMaxEntries to limit scheduled jobs in multi-tenant environments
  - Use WithMaxSearchYears to limit schedule search time for complex expressions
  - Timeout wrappers prevent runaway jobs from consuming resources indefinitely

Recommended Patterns:
  - Validate user-provided cron expressions before scheduling
  - Use named jobs with duplicate prevention for user-defined schedules
  - Monitor entry counts and job durations in production
  - Run the cron service with minimal privileges

# Migration from robfig/cron

This library is a maintained fork of github.com/robfig/cron/v3 with full
backward compatibility. To migrate:

	// Before
	import "github.com/robfig/cron/v3"

	// After
	import "github.com/netresearch/go-cron"

New features available after migration:
  - RetryWithBackoff: Automatic retry with exponential backoff
  - CircuitBreaker: Protect failing jobs from overwhelming services
  - TimeoutWithContext: True cancellation support via context
  - ObservabilityHooks: Integrated metrics and tracing support
  - FakeClock: Deterministic time control for testing
  - WithMaxEntries: Resource protection for entry limits
  - WithMaxSearchYears: Configurable schedule search limits
  - Named jobs: Unique job names with duplicate prevention
  - Tagged jobs: Categorization and bulk operations
  - Context support: Graceful shutdown via context cancellation
  - Run-once jobs: Single-execution jobs that auto-remove after running

All existing code will work unchanged. The migration is a drop-in replacement.

# Implementation

Cron entries are stored in a min-heap ordered by their next activation time,
providing O(log n) insertion/removal and O(1) access to the next entry.
Cron sleeps until the next job is due to be run.

Upon waking:
  - it runs each entry that is active on that second
  - it calculates the next run times for the jobs that were run
  - it re-heapifies the entries by next activation time
  - it goes to sleep until the soonest job.
*/
package cron
