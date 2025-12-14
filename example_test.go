package cron_test

import (
	"context"
	"fmt"
	"log"
	"time"

	cron "github.com/netresearch/go-cron"
)

// This example demonstrates basic cron usage.
func Example() {
	c := cron.New()

	// Add a job that runs every minute
	c.AddFunc("* * * * *", func() {
		fmt.Println("Every minute")
	})

	// Start the scheduler
	c.Start()

	// Stop the scheduler when done
	c.Stop()
	// Output:
}

// This example demonstrates creating a new Cron instance with default settings.
func ExampleNew() {
	c := cron.New()

	c.AddFunc("@hourly", func() {
		fmt.Println("Every hour")
	})

	c.Start()
	defer c.Stop()
	// Output:
}

// This example demonstrates using WithSeconds to enable second-granularity scheduling.
func ExampleNew_withSeconds() {
	// Enable seconds field (Quartz-style 6-field expressions)
	c := cron.New(cron.WithSeconds())

	// Run every 30 seconds
	c.AddFunc("*/30 * * * * *", func() {
		fmt.Println("Every 30 seconds")
	})

	c.Start()
	defer c.Stop()
	// Output:
}

// This example demonstrates timezone-aware scheduling.
func ExampleNew_withLocation() {
	nyc, _ := time.LoadLocation("America/New_York")
	c := cron.New(cron.WithLocation(nyc))

	// Run at 9 AM New York time
	c.AddFunc("0 9 * * *", func() {
		fmt.Println("Good morning, New York!")
	})

	c.Start()
	defer c.Stop()
	// Output:
}

// This example demonstrates adding a function to the cron scheduler.
func ExampleCron_AddFunc() {
	c := cron.New()

	// Standard 5-field cron expression
	_, err := c.AddFunc("30 * * * *", func() {
		fmt.Println("Every hour at minute 30")
	})
	if err != nil {
		log.Fatal(err)
	}

	// Using predefined schedule
	_, err = c.AddFunc("@daily", func() {
		fmt.Println("Once a day at midnight")
	})
	if err != nil {
		log.Fatal(err)
	}

	// Using interval
	_, err = c.AddFunc("@every 1h30m", func() {
		fmt.Println("Every 1.5 hours")
	})
	if err != nil {
		log.Fatal(err)
	}

	c.Start()
	defer c.Stop()
	// Output:
}

// This example demonstrates inline timezone specification.
func ExampleCron_AddFunc_timezone() {
	c := cron.New()

	// Specify timezone inline with CRON_TZ prefix
	c.AddFunc("CRON_TZ=Asia/Tokyo 0 9 * * *", func() {
		fmt.Println("Good morning, Tokyo!")
	})

	// Legacy TZ prefix is also supported
	c.AddFunc("TZ=Europe/London 0 17 * * *", func() {
		fmt.Println("Good evening, London!")
	})

	c.Start()
	defer c.Stop()
	// Output:
}

// This example demonstrates implementing the Job interface for complex job logic.
func ExampleCron_AddJob() {
	c := cron.New()

	// Define a job type
	type cleanupJob struct {
		name string
	}

	// Implement the Job interface
	job := &cleanupJob{name: "temp files"}

	// AddJob accepts any type implementing cron.Job
	_, err := c.AddJob("0 0 * * *", cron.FuncJob(func() {
		fmt.Printf("Cleaning up %s\n", job.name)
	}))
	if err != nil {
		log.Fatal(err)
	}

	c.Start()
	defer c.Stop()
	// Output:
}

// This example demonstrates using job wrappers (middleware) with WithChain.
func ExampleWithChain() {
	// Create cron with job wrappers applied to all jobs
	c := cron.New(
		cron.WithChain(
			// Recover from panics and log them
			cron.Recover(cron.DefaultLogger),
			// Skip job execution if the previous run hasn't completed
			cron.SkipIfStillRunning(cron.DefaultLogger),
		),
	)

	c.AddFunc("* * * * *", func() {
		fmt.Println("This job is protected by Recover and SkipIfStillRunning")
	})

	c.Start()
	defer c.Stop()
	// Output:
}

// This example demonstrates wrapping individual jobs with chains.
func ExampleNewChain() {
	c := cron.New()

	// Create a chain for specific jobs
	chain := cron.NewChain(
		cron.DelayIfStillRunning(cron.DefaultLogger),
	)

	// Wrap a job with the chain
	wrappedJob := chain.Then(cron.FuncJob(func() {
		fmt.Println("This job will queue if still running")
	}))

	c.Schedule(cron.Every(time.Minute), wrappedJob)
	c.Start()
	defer c.Stop()
	// Output:
}

// This example demonstrates checking if the scheduler is running.
func ExampleCron_IsRunning() {
	c := cron.New()

	fmt.Printf("Before Start: %v\n", c.IsRunning())
	c.Start()
	fmt.Printf("After Start: %v\n", c.IsRunning())
	c.Stop()
	fmt.Printf("After Stop: %v\n", c.IsRunning())
	// Output:
	// Before Start: false
	// After Start: true
	// After Stop: false
}

// This example demonstrates retrieving all scheduled entries.
func ExampleCron_Entries() {
	c := cron.New()

	c.AddFunc("0 * * * *", func() { fmt.Println("hourly") })
	c.AddFunc("0 0 * * *", func() { fmt.Println("daily") })

	c.Start()

	// Get all entries
	entries := c.Entries()
	fmt.Printf("Number of jobs: %d\n", len(entries))
	// Output: Number of jobs: 2
}

// This example demonstrates removing a scheduled job.
func ExampleCron_Remove() {
	c := cron.New()

	// AddFunc returns an entry ID
	entryID, _ := c.AddFunc("* * * * *", func() {
		fmt.Println("This will be removed")
	})

	c.Start()

	// Remove the job using its ID
	c.Remove(entryID)

	fmt.Printf("Jobs after removal: %d\n", len(c.Entries()))
	// Output: Jobs after removal: 0
}

// This example demonstrates graceful shutdown with job completion.
func ExampleCron_Stop() {
	c := cron.New()

	c.AddFunc("* * * * *", func() {
		time.Sleep(time.Second)
		fmt.Println("Job completed")
	})

	c.Start()

	// Stop returns a context that completes when all running jobs finish
	ctx := c.Stop()

	// Wait for running jobs to complete
	<-ctx.Done()
	fmt.Println("All jobs completed")
	// Output: All jobs completed
}

// This example demonstrates creating a constant delay schedule.
func ExampleEvery() {
	c := cron.New()

	// Run every 5 minutes
	c.Schedule(cron.Every(5*time.Minute), cron.FuncJob(func() {
		fmt.Println("Every 5 minutes")
	}))

	c.Start()
	defer c.Stop()
	// Output:
}

// This example demonstrates parsing a cron expression.
func ExampleParseStandard() {
	schedule, err := cron.ParseStandard("0 9 * * MON-FRI")
	if err != nil {
		log.Fatal(err)
	}

	// Get the next scheduled time
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC) // Wednesday
	next := schedule.Next(now)
	fmt.Printf("Next run: %s\n", next.Format("Mon 15:04"))
	// Output: Next run: Wed 09:00
}

// This example demonstrates that Sunday can be specified as either 0 or 7
// in the day-of-week field, matching traditional cron behavior.
func ExampleParseStandard_sundayFormats() {
	// Both "0" and "7" represent Sunday
	schedSun0, _ := cron.ParseStandard("0 9 * * 0") // Sunday as 0
	schedSun7, _ := cron.ParseStandard("0 9 * * 7") // Sunday as 7

	// Start from Saturday
	saturday := time.Date(2025, 1, 4, 0, 0, 0, 0, time.UTC) // Saturday

	// Both find the same Sunday
	next0 := schedSun0.Next(saturday)
	next7 := schedSun7.Next(saturday)

	fmt.Printf("Using 0: %s\n", next0.Format("Mon Jan 2"))
	fmt.Printf("Using 7: %s\n", next7.Format("Mon Jan 2"))
	fmt.Printf("Same day: %v\n", next0.Equal(next7))
	// Output:
	// Using 0: Sun Jan 5
	// Using 7: Sun Jan 5
	// Same day: true
}

// This example demonstrates using 7 in day-of-week ranges.
// The range "5-7" means Friday through Sunday.
func ExampleParseStandard_weekendRange() {
	// "5-7" covers Friday(5), Saturday(6), Sunday(7->0)
	schedule, _ := cron.ParseStandard("0 10 * * 5-7")

	// Start from Wednesday
	wednesday := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	next := schedule.Next(wednesday)
	fmt.Printf("Next after Wed: %s\n", next.Format("Mon"))

	next = schedule.Next(next)
	fmt.Printf("Then: %s\n", next.Format("Mon"))

	next = schedule.Next(next)
	fmt.Printf("Then: %s\n", next.Format("Mon"))
	// Output:
	// Next after Wed: Fri
	// Then: Sat
	// Then: Sun
}

// This example demonstrates verbose logging for debugging.
func ExampleVerbosePrintfLogger() {
	logger := cron.VerbosePrintfLogger(log.Default())

	c := cron.New(cron.WithLogger(logger))

	c.AddFunc("@hourly", func() {
		fmt.Println("hourly job")
	})

	c.Start()
	defer c.Stop()
	// Output:
}

// This example demonstrates the Timeout wrapper and its limitations.
// Note: Timeout uses an "abandonment model" - the job continues running
// in the background even after the timeout is reached.
func ExampleTimeout() {
	logger := cron.DefaultLogger

	c := cron.New(cron.WithChain(
		// Jobs that exceed 30 seconds will be "abandoned" (wrapper returns,
		// but the goroutine keeps running until the job completes naturally)
		cron.Timeout(logger, 30*time.Second),
		// Recover panics from timed-out jobs
		cron.Recover(logger),
	))

	c.AddFunc("@hourly", func() {
		// This job may run longer than 30 seconds.
		// If it does, the timeout wrapper will return early and log an error,
		// but this goroutine continues until completion.
		fmt.Println("Starting long job")
		time.Sleep(45 * time.Second) // Exceeds timeout
		fmt.Println("Job completed (even after timeout)")
	})

	c.Start()
	defer c.Stop()
	// Output:
}

// This example demonstrates a job pattern that supports true cancellation
// using channels. This approach works well for simple cancellation needs.
func ExampleTimeout_cancellable() {
	// CancellableWorker demonstrates a job that can be cleanly canceled
	type CancellableWorker struct {
		cancel chan struct{}
		done   chan struct{}
	}

	worker := &CancellableWorker{
		cancel: make(chan struct{}),
		done:   make(chan struct{}),
	}

	c := cron.New()

	// Wrap the worker in a FuncJob
	c.Schedule(cron.Every(time.Minute), cron.FuncJob(func() {
		defer close(worker.done)
		for i := 0; i < 100; i++ {
			select {
			case <-worker.cancel:
				fmt.Println("Job canceled cleanly")
				return
			default:
				// Do a small chunk of work
				time.Sleep(100 * time.Millisecond)
			}
		}
		fmt.Println("Job completed normally")
	}))

	c.Start()

	// Later, to cancel the job:
	// close(worker.cancel)
	// <-worker.done  // Wait for clean shutdown

	defer c.Stop()
	// Output:
}

// This example demonstrates the recommended pattern for cancellable jobs
// using context.Context. This is the idiomatic Go approach for jobs that
// need to respect cancellation signals, especially when calling external
// services or performing long-running operations.
func ExampleTimeout_withContext() {
	// ContextAwareJob wraps job execution with context-based cancellation.
	// This pattern is recommended when jobs make external calls (HTTP, DB, etc.)
	// that accept context for cancellation.
	type ContextAwareJob struct {
		ctx    context.Context
		cancel context.CancelFunc
	}

	// Create a job with its own cancellation context
	ctx, cancel := context.WithCancel(context.Background())
	job := &ContextAwareJob{ctx: ctx, cancel: cancel}

	c := cron.New()

	c.Schedule(cron.Every(time.Minute), cron.FuncJob(func() {
		// Create a timeout context for this execution
		execCtx, execCancel := context.WithTimeout(job.ctx, 30*time.Second)
		defer execCancel()

		// Use NewTimer instead of time.After to avoid timer leak on early return
		workTimer := time.NewTimer(10 * time.Second)
		defer workTimer.Stop()

		// Simulate work that respects context cancellation
		select {
		case <-execCtx.Done():
			if execCtx.Err() == context.DeadlineExceeded {
				fmt.Println("Job timed out")
			} else {
				fmt.Println("Job canceled")
			}
			return
		case <-workTimer.C:
			// Simulated work completed
			fmt.Println("Job completed successfully")
		}
	}))

	c.Start()

	// To gracefully shutdown:
	// job.cancel()  // Signal cancellation to all running jobs
	// c.Stop()      // Stop scheduling new jobs

	defer c.Stop()
	// Output:
}

// This example demonstrates using observability hooks for metrics collection.
// In production, you would integrate with Prometheus, StatsD, or similar systems.
func ExampleWithObservability() {
	var jobsStarted, jobsCompleted int

	hooks := cron.ObservabilityHooks{
		OnJobStart: func(id cron.EntryID, name string, scheduled time.Time) {
			// In production: prometheus.Counter.Inc()
			jobsStarted++
		},
		OnJobComplete: func(id cron.EntryID, name string, duration time.Duration, recovered any) {
			// In production: prometheus.Histogram.Observe(duration.Seconds())
			jobsCompleted++
		},
		OnSchedule: func(id cron.EntryID, name string, nextRun time.Time) {
			// In production: prometheus.Gauge.Set(nextRun.Unix())
		},
	}

	c := cron.New(cron.WithObservability(hooks))

	c.AddFunc("@hourly", func() {
		// Job logic here
	})

	c.Start()
	c.Stop()

	fmt.Println("Hooks configured successfully")
	// Output: Hooks configured successfully
}

// This example demonstrates implementing NamedJob for better observability.
// Named jobs have their name passed to observability hooks, which is useful
// for metrics labeling (e.g., Prometheus labels).
func ExampleNamedJob() {
	// myJob implements both Job and NamedJob interfaces
	type myJob struct {
		name string
	}

	// Run implements cron.Job
	run := func(j *myJob) {
		fmt.Printf("Running %s\n", j.name)
	}
	_ = run

	// Name implements cron.NamedJob
	name := func(j *myJob) string {
		return j.name
	}
	_ = name

	// When used with observability hooks, the name is passed to callbacks:
	// OnJobStart(id, "my-job-name", scheduledTime)
	// OnJobComplete(id, "my-job-name", duration, recovered)
	fmt.Println("NamedJob provides names for observability hooks")
	// Output: NamedJob provides names for observability hooks
}

// This example demonstrates using EveryWithMin to create schedules with custom
// minimum intervals. This is useful for testing (sub-second) or rate limiting.
func ExampleEveryWithMin() {
	c := cron.New()

	// Allow sub-second intervals (useful for testing)
	// The second parameter (0) disables the minimum interval check
	schedule := cron.EveryWithMin(100*time.Millisecond, 0)
	c.Schedule(schedule, cron.FuncJob(func() {
		fmt.Println("Running every 100ms")
	}))

	// Enforce minimum 1-minute intervals (useful for rate limiting)
	// If duration < minInterval, it's rounded up to minInterval
	rateLimited := cron.EveryWithMin(30*time.Second, time.Minute)
	c.Schedule(rateLimited, cron.FuncJob(func() {
		fmt.Println("Running every minute (30s was rounded up)")
	}))

	c.Start()
	defer c.Stop()
	// Output:
}

// This example demonstrates using WithMinEveryInterval to configure
// the minimum interval for @every expressions at the cron level.
func ExampleWithMinEveryInterval() {
	// Allow sub-second @every intervals (useful for testing)
	c := cron.New(cron.WithMinEveryInterval(0))

	_, err := c.AddFunc("@every 100ms", func() {
		fmt.Println("Running every 100ms")
	})
	if err != nil {
		log.Fatal(err)
	}

	// With default settings, sub-second would fail:
	// c := cron.New() // default minimum is 1 second
	// _, err := c.AddFunc("@every 100ms", ...) // returns error

	c.Start()
	defer c.Stop()
	// Output:
}

// This example demonstrates using WithMinEveryInterval to enforce
// longer minimum intervals for rate limiting purposes.
func ExampleWithMinEveryInterval_rateLimit() {
	// Enforce minimum 1-minute intervals
	c := cron.New(cron.WithMinEveryInterval(time.Minute))

	// This will fail because 30s < 1m minimum
	_, err := c.AddFunc("@every 30s", func() {
		fmt.Println("This won't be added")
	})
	if err != nil {
		fmt.Println("Error:", err)
	}

	// This will succeed because 2m >= 1m minimum
	_, err = c.AddFunc("@every 2m", func() {
		fmt.Println("Running every 2 minutes")
	})
	if err != nil {
		log.Fatal(err)
	}

	c.Start()
	defer c.Stop()
	// Output: Error: @every duration must be at least 1m0s: "@every 30s"
}

// This example demonstrates RetryWithBackoff for jobs that may fail transiently.
// The wrapper catches panics and retries with exponential backoff.
func ExampleRetryWithBackoff() {
	logger := cron.DefaultLogger

	c := cron.New(cron.WithChain(
		// Outermost: catches final re-panic after retries exhausted
		cron.Recover(logger),
		// Retry up to 3 times with exponential backoff
		// Initial delay: 1s, max delay: 30s, multiplier: 2.0
		cron.RetryWithBackoff(logger, 3, time.Second, 30*time.Second, 2.0),
	))

	attempts := 0
	c.AddFunc("@hourly", func() {
		attempts++
		// Simulate transient failure that succeeds on 3rd attempt
		if attempts < 3 {
			panic(fmt.Sprintf("attempt %d failed", attempts))
		}
		fmt.Printf("Succeeded on attempt %d\n", attempts)
	})

	c.Start()
	defer c.Stop()
	// Output:
}

// This example demonstrates RetryWithBackoff with maxRetries=0 (no retries).
// This is the safe default - jobs execute once and fail immediately on panic.
func ExampleRetryWithBackoff_noRetries() {
	logger := cron.DefaultLogger

	c := cron.New(cron.WithChain(
		cron.Recover(logger),
		// maxRetries=0 means no retries - execute once, fail immediately
		cron.RetryWithBackoff(logger, 0, time.Second, 30*time.Second, 2.0),
	))

	c.AddFunc("@hourly", func() {
		// This will execute once, panic, and not retry
		panic("immediate failure")
	})

	c.Start()
	defer c.Stop()
	// Output:
}

// This example demonstrates CircuitBreaker to prevent cascading failures.
// After consecutive failures, the circuit opens and skips execution until cooldown.
func ExampleCircuitBreaker() {
	logger := cron.DefaultLogger

	c := cron.New(cron.WithChain(
		// Outermost: catches re-panic from circuit breaker
		cron.Recover(logger),
		// Open circuit after 3 consecutive failures, cooldown for 5 minutes
		cron.CircuitBreaker(logger, 3, 5*time.Minute),
	))

	c.AddFunc("@every 1m", func() {
		// Simulate a job calling an external service
		if err := callExternalAPI(); err != nil {
			panic(err) // After 3 failures, circuit opens for 5 minutes
		}
		fmt.Println("API call succeeded")
	})

	c.Start()
	defer c.Stop()
	// Output:
}

// callExternalAPI is a mock function for the CircuitBreaker example
func callExternalAPI() error {
	// In real code, this would call an external service
	return nil
}

// This example demonstrates TimeoutWithContext for true context-based cancellation.
// Jobs implementing JobWithContext receive a context that is canceled on timeout.
func ExampleTimeoutWithContext() {
	logger := cron.DefaultLogger

	c := cron.New(cron.WithChain(
		// Outermost: catches any panics
		cron.Recover(logger),
		// Jobs have 5 minutes to complete; context is canceled on timeout
		cron.TimeoutWithContext(logger, 5*time.Minute),
	))

	// Use FuncJobWithContext for jobs that need context support
	c.AddJob("@hourly", cron.FuncJobWithContext(func(ctx context.Context) {
		// Create a timer for simulated work
		workTimer := time.NewTimer(10 * time.Minute)
		defer workTimer.Stop()

		select {
		case <-ctx.Done():
			// Context canceled - clean up and exit gracefully
			fmt.Println("Job canceled:", ctx.Err())
			return
		case <-workTimer.C:
			// Work completed normally
			fmt.Println("Job completed")
		}
	}))

	c.Start()
	defer c.Stop()
	// Output:
}

// This example demonstrates using Schedule.Prev() to find the previous execution time.
// This is useful for detecting missed executions or determining when a job last ran.
func ExampleSpecSchedule_Prev() {
	// Parse a schedule that runs at 9 AM on weekdays
	schedule, err := cron.ParseStandard("0 9 * * MON-FRI")
	if err != nil {
		log.Fatal(err)
	}

	// Find the previous execution time from a given point
	// Starting from Wednesday at noon
	now := time.Date(2025, 1, 8, 12, 0, 0, 0, time.UTC) // Wednesday

	prev := schedule.Prev(now)
	fmt.Printf("Previous run: %s at %s\n", prev.Format("Mon"), prev.Format("15:04"))

	// Can chain to find earlier executions
	prev2 := schedule.Prev(prev)
	fmt.Printf("Before that: %s at %s\n", prev2.Format("Mon"), prev2.Format("15:04"))
	// Output:
	// Previous run: Wed at 09:00
	// Before that: Tue at 09:00
}

// This example demonstrates using Prev() to detect missed job executions.
// By comparing Prev() to the last known run time, you can determine if
// any scheduled executions were missed.
func ExampleSpecSchedule_Prev_detectMissed() {
	schedule, _ := cron.ParseStandard("0 * * * *") // Every hour

	// Simulate: job was last run at 10:00, it's now 14:30
	lastRun := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	now := time.Date(2025, 1, 1, 14, 30, 0, 0, time.UTC)

	// Find when the job should have last run
	shouldHaveRun := schedule.Prev(now)

	if shouldHaveRun.After(lastRun) {
		// Count missed executions
		missed := 0
		checkTime := now
		for {
			prev := schedule.Prev(checkTime)
			if !prev.After(lastRun) {
				break
			}
			missed++
			checkTime = prev
		}
		fmt.Printf("Missed %d execution(s)\n", missed)
		fmt.Printf("Most recent missed: %s\n", shouldHaveRun.Format("15:04"))
	}
	// Output:
	// Missed 4 execution(s)
	// Most recent missed: 14:00
}

// This example demonstrates the symmetry between Next() and Prev().
// For any time t where schedule.Next(t) returns n, schedule.Prev(n + 1 second) returns n.
func ExampleSpecSchedule_Prev_symmetry() {
	schedule, _ := cron.ParseStandard("30 9 * * *") // Daily at 9:30

	// Start from Monday midnight
	start := time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC) // Monday

	// Get next scheduled time
	next := schedule.Next(start)
	fmt.Printf("Next: %s\n", next.Format("Mon 15:04"))

	// Get prev from just after that time - should return the same time
	prev := schedule.Prev(next.Add(time.Second))
	fmt.Printf("Prev: %s\n", prev.Format("Mon 15:04"))

	fmt.Printf("Symmetric: %v\n", next.Equal(prev))
	// Output:
	// Next: Mon 09:30
	// Prev: Mon 09:30
	// Symmetric: true
}

// This example demonstrates using ConstantDelaySchedule.Prev().
// For constant delays, Prev() simply subtracts the delay interval.
func ExampleConstantDelaySchedule_Prev() {
	// Create a schedule that runs every 5 minutes
	schedule := cron.Every(5 * time.Minute)

	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	// Get the previous execution time
	prev := schedule.Prev(now)
	fmt.Printf("Previous: %s\n", prev.Format("15:04:05"))

	// Chain to get earlier times
	prev2 := schedule.Prev(prev)
	fmt.Printf("Before that: %s\n", prev2.Format("15:04:05"))
	// Output:
	// Previous: 11:55:00
	// Before that: 11:50:00
}

// This example demonstrates using WithMaxEntries to limit the number of jobs.
// This provides protection against memory exhaustion from excessive entry additions.
func ExampleWithMaxEntries() {
	c := cron.New(cron.WithMaxEntries(2))

	// Add first job - succeeds
	_, err := c.AddFunc("@hourly", func() { fmt.Println("Job 1") })
	if err != nil {
		fmt.Println("Job 1 failed:", err)
	}

	// Add second job - succeeds
	_, err = c.AddFunc("@hourly", func() { fmt.Println("Job 2") })
	if err != nil {
		fmt.Println("Job 2 failed:", err)
	}

	// Add third job - fails (limit reached)
	_, err = c.AddFunc("@hourly", func() { fmt.Println("Job 3") })
	if err != nil {
		fmt.Println("Job 3 failed:", err)
	}

	fmt.Printf("Total jobs: %d\n", len(c.Entries()))
	// Output:
	// Job 3 failed: cron: max entries limit reached
	// Total jobs: 2
}
