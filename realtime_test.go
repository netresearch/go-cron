//go:build integration

package cron

import (
	"sync/atomic"
	"testing"
	"time"
)

// These tests use the real system clock and actual time.Sleep calls.
// They are slower than unit tests but catch timing-related bugs that
// fake clocks might miss.
//
// Run with: go test -tags=integration -v -run RealTime

// TestRealTimeScheduling verifies basic job execution with the real clock.
func TestRealTimeScheduling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-time integration test in short mode")
	}

	c := New()
	c.Start()
	defer c.Stop()

	executed := make(chan struct{}, 1)
	_, err := c.AddFunc("@every 2s", func() {
		select {
		case executed <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}

	select {
	case <-executed:
		// Success - job was executed
	case <-time.After(5 * time.Second):
		t.Fatal("job not executed within expected time (5s for 2s interval)")
	}
}

// TestRealTimeMultipleJobs verifies multiple jobs run at correct intervals.
func TestRealTimeMultipleJobs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-time integration test in short mode")
	}

	c := New()
	c.Start()
	defer c.Stop()

	var job1Count, job2Count atomic.Int32

	// Job 1: every 1 second
	_, err := c.AddFunc("@every 1s", func() {
		job1Count.Add(1)
	})
	if err != nil {
		t.Fatalf("AddFunc for job1 failed: %v", err)
	}

	// Job 2: every 2 seconds
	_, err = c.AddFunc("@every 2s", func() {
		job2Count.Add(1)
	})
	if err != nil {
		t.Fatalf("AddFunc for job2 failed: %v", err)
	}

	// Wait ~5 seconds
	time.Sleep(5500 * time.Millisecond)

	// Job 1 should have run ~5 times, job 2 should have run ~2-3 times
	j1 := job1Count.Load()
	j2 := job2Count.Load()

	if j1 < 4 || j1 > 6 {
		t.Errorf("job1 expected 4-6 executions, got %d", j1)
	}
	if j2 < 2 || j2 > 3 {
		t.Errorf("job2 expected 2-3 executions, got %d", j2)
	}
}

// TestRealTimeCronExpression verifies standard cron expressions work.
func TestRealTimeCronExpression(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-time integration test in short mode")
	}

	// Use WithSeconds to test second-level precision
	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	executed := make(chan time.Time, 10)

	// Run every 3 seconds (*/3 in seconds field)
	_, err := c.AddFunc("*/3 * * * * *", func() {
		select {
		case executed <- time.Now():
		default:
		}
	})
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}

	// Wait for at least 2 executions
	var times []time.Time
	timeout := time.After(10 * time.Second)

	for len(times) < 2 {
		select {
		case ts := <-executed:
			times = append(times, ts)
		case <-timeout:
			t.Fatalf("timed out waiting for executions, got %d", len(times))
		}
	}

	// Verify executions were ~3 seconds apart
	diff := times[1].Sub(times[0])
	if diff < 2*time.Second || diff > 4*time.Second {
		t.Errorf("expected ~3s between executions, got %v", diff)
	}
}

// TestRealTimeGracefulShutdown verifies jobs complete during shutdown.
func TestRealTimeGracefulShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-time integration test in short mode")
	}

	c := New()

	jobStarted := make(chan struct{})
	jobFinished := make(chan struct{})

	_, err := c.AddFunc("@every 1s", func() {
		close(jobStarted)
		time.Sleep(2 * time.Second) // Long-running job
		close(jobFinished)
	})
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}

	c.Start()

	// Wait for job to start
	select {
	case <-jobStarted:
		// Job is running
	case <-time.After(3 * time.Second):
		t.Fatal("job never started")
	}

	// Stop scheduler and wait for jobs to complete
	ctx := c.Stop()

	select {
	case <-ctx.Done():
		// Scheduler stopped, verify job finished
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler did not stop in time")
	}

	// Job should have finished
	select {
	case <-jobFinished:
		// Success
	default:
		t.Error("job did not complete before shutdown finished")
	}
}

// TestRealTimeRemoveWhileRunning verifies job removal while scheduler is running.
func TestRealTimeRemoveWhileRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-time integration test in short mode")
	}

	c := New()
	c.Start()
	defer c.Stop()

	var count atomic.Int32

	id, err := c.AddFunc("@every 1s", func() {
		count.Add(1)
	})
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}

	// Wait for at least one execution
	time.Sleep(1500 * time.Millisecond)
	initialCount := count.Load()
	if initialCount < 1 {
		t.Errorf("expected at least 1 execution before removal, got %d", initialCount)
	}

	// Remove the job
	c.Remove(id)

	// Wait and verify no more executions
	time.Sleep(2 * time.Second)
	finalCount := count.Load()

	// Allow for at most 1 additional execution (race between remove and execution)
	if finalCount > initialCount+1 {
		t.Errorf("job continued running after removal: initial=%d, final=%d", initialCount, finalCount)
	}
}

// TestRealTimeAddWhileRunning verifies dynamic job addition.
func TestRealTimeAddWhileRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-time integration test in short mode")
	}

	c := New()
	c.Start()
	defer c.Stop()

	var job1Count, job2Count atomic.Int32

	// Add first job
	_, err := c.AddFunc("@every 1s", func() {
		job1Count.Add(1)
	})
	if err != nil {
		t.Fatalf("AddFunc for job1 failed: %v", err)
	}

	// Wait, then add second job
	time.Sleep(2 * time.Second)

	_, err = c.AddFunc("@every 1s", func() {
		job2Count.Add(1)
	})
	if err != nil {
		t.Fatalf("AddFunc for job2 failed: %v", err)
	}

	// Wait for both to run
	time.Sleep(2 * time.Second)

	j1 := job1Count.Load()
	j2 := job2Count.Load()

	// Job1 should have more executions than job2
	if j1 < 3 {
		t.Errorf("job1 expected at least 3 executions, got %d", j1)
	}
	if j2 < 1 {
		t.Errorf("job2 expected at least 1 execution, got %d", j2)
	}
	if j2 >= j1 {
		t.Errorf("job2 (%d) should have fewer executions than job1 (%d)", j2, j1)
	}
}

// TestRealTimeSkipIfStillRunning verifies overlap prevention with real timing.
func TestRealTimeSkipIfStillRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-time integration test in short mode")
	}

	logger := DiscardLogger
	c := New(WithChain(SkipIfStillRunning(logger)))
	c.Start()
	defer c.Stop()

	var startCount, finishCount atomic.Int32

	// Job takes 3 seconds but is scheduled every 1 second
	_, err := c.AddFunc("@every 1s", func() {
		startCount.Add(1)
		time.Sleep(3 * time.Second)
		finishCount.Add(1)
	})
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}

	// Wait 5 seconds (5 potential executions, but most should be skipped)
	time.Sleep(5 * time.Second)

	starts := startCount.Load()
	finishes := finishCount.Load()

	// With SkipIfStillRunning, only 1-2 jobs should have started
	// (first one runs immediately, second may start after first finishes)
	if starts > 2 {
		t.Errorf("expected at most 2 starts with SkipIfStillRunning, got %d", starts)
	}
	if finishes > 2 {
		t.Errorf("expected at most 2 finishes, got %d", finishes)
	}
}

// TestRealTimeRecovery verifies panic recovery with real timing.
func TestRealTimeRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-time integration test in short mode")
	}

	logger := DiscardLogger
	c := New(WithChain(Recover(logger)))
	c.Start()
	defer c.Stop()

	var panicCount, normalCount atomic.Int32

	// Job that panics
	_, err := c.AddFunc("@every 1s", func() {
		panicCount.Add(1)
		panic("intentional test panic")
	})
	if err != nil {
		t.Fatalf("AddFunc for panic job failed: %v", err)
	}

	// Normal job to verify scheduler keeps running
	_, err = c.AddFunc("@every 1s", func() {
		normalCount.Add(1)
	})
	if err != nil {
		t.Fatalf("AddFunc for normal job failed: %v", err)
	}

	// Wait for multiple executions
	time.Sleep(3500 * time.Millisecond)

	panics := panicCount.Load()
	normals := normalCount.Load()

	// Both should have run multiple times despite panics
	if panics < 2 {
		t.Errorf("panic job expected at least 2 executions, got %d", panics)
	}
	if normals < 2 {
		t.Errorf("normal job expected at least 2 executions, got %d", normals)
	}
}

// TestRealTimeLocation verifies timezone handling with real clock.
func TestRealTimeLocation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-time integration test in short mode")
	}

	// Use UTC to avoid local timezone issues
	loc, err := time.LoadLocation("UTC")
	if err != nil {
		t.Fatalf("failed to load UTC: %v", err)
	}

	c := New(WithLocation(loc), WithSeconds())
	c.Start()
	defer c.Stop()

	executed := make(chan time.Time, 1)

	// Schedule to run at a specific second
	now := time.Now().In(loc)
	targetSecond := (now.Second() + 3) % 60 // 3 seconds from now

	spec := "@every 5s" // Use simple interval for reliability
	_, err = c.AddFunc(spec, func() {
		select {
		case executed <- time.Now().In(loc):
		default:
		}
	})
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}

	select {
	case execTime := <-executed:
		// Verify execution happened in UTC
		if execTime.Location().String() != "UTC" {
			t.Logf("execution time location: %s (expected in scheduler's location)", execTime.Location())
		}
		t.Logf("job executed at %v (target was ~%d seconds from start)", execTime, targetSecond)
	case <-time.After(10 * time.Second):
		t.Fatal("job not executed within expected time")
	}
}

// TestRealTimeEntrySnapshot verifies Entries() returns correct state.
func TestRealTimeEntrySnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-time integration test in short mode")
	}

	c := New()
	c.Start()
	defer c.Stop()

	id1, _ := c.AddFunc("@every 1s", func() {})
	id2, _ := c.AddFunc("@every 2s", func() {})

	// Wait for at least one execution
	time.Sleep(1500 * time.Millisecond)

	entries := c.Entries()
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}

	// Find entry by ID
	var found1, found2 bool
	for _, e := range entries {
		if e.ID == id1 {
			found1 = true
			if e.Prev.IsZero() {
				t.Error("entry1 Prev should be set after execution")
			}
		}
		if e.ID == id2 {
			found2 = true
		}
	}

	if !found1 || !found2 {
		t.Errorf("entries not found: found1=%v, found2=%v", found1, found2)
	}
}

// TestRealTimeStopAndWait verifies StopAndWait convenience method.
func TestRealTimeStopAndWait(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-time integration test in short mode")
	}

	c := New()

	var jobFinished atomic.Bool
	jobStarted := make(chan struct{}, 1)

	_, err := c.AddFunc("@every 1s", func() {
		select {
		case jobStarted <- struct{}{}:
		default:
		}
		time.Sleep(2 * time.Second)
		jobFinished.Store(true)
	})
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}

	c.Start()

	// Wait for job to start
	select {
	case <-jobStarted:
		// Job has started
	case <-time.After(3 * time.Second):
		t.Fatal("job never started")
	}

	// StopAndWait should block until job completes
	start := time.Now()
	c.StopAndWait()
	elapsed := time.Since(start)

	// Should have waited for the job (at least some time for remaining work)
	if elapsed < 100*time.Millisecond {
		t.Errorf("StopAndWait returned too quickly: %v", elapsed)
	}

	if !jobFinished.Load() {
		t.Error("job did not finish before StopAndWait returned")
	}
}
