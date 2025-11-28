package cron_test

import (
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

// This example demonstrates a job pattern that supports true cancellation.
// This is the recommended approach when jobs need to be stoppable.
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
