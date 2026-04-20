//go:build integration

// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cron

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// This file contains real-clock integration tests that complement the
// existing realtime_test.go and stress_test.go suites.
//
// These scenarios specifically target bug classes that a mock clock
// cannot catch:
//   - Goroutine leaks across Start/Stop boundaries
//   - Races between schedule/remove and the tick loop
//   - Context cancellation propagation to long-running JobWithContext jobs
//   - Middleware/chain invocation order under real scheduling
//   - Resilience middleware (retry, circuit breaker) wall-clock behavior
//
// All tests run with `-tags=integration -race`. Each test targets <10s
// runtime so the full suite stays under ~2 min even under -race overhead.

// TestIntegrationPerSecondJobFiresThreeTimes schedules a per-second job,
// sleeps 3 seconds, and asserts it fired close to 3 times. The mock clock
// would make this trivially deterministic; the real clock catches
// goroutine scheduling hiccups, timer drift, and tick-alignment bugs.
func TestIntegrationPerSecondJobFiresThreeTimes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-clock integration test in short mode")
	}

	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	var fired atomic.Int32
	if _, err := c.AddFunc("* * * * * *", func() {
		fired.Add(1)
	}); err != nil {
		t.Fatalf("AddFunc: %v", err)
	}

	// Sleep slightly more than 3s to allow 3 ticks plus scheduling slack.
	time.Sleep(3200 * time.Millisecond)

	got := fired.Load()
	// Expect exactly 3 fires, but allow +/- 1 to absorb boundary jitter
	// (starting fractionally before/after a second boundary).
	if got < 2 || got > 4 {
		t.Fatalf("per-second job fired %d times in 3.2s, expected 3 ± 1", got)
	}
	t.Logf("per-second job fired %d times in 3.2s", got)
}

// TestIntegrationStartStopNoGoroutineLeak schedules a job, starts the
// scheduler, stops it mid-cycle, and asserts the run-loop goroutine exits
// within 1 second. A baseline goroutine count is taken before Start and
// compared after Stop completes; it must be back to the baseline (±small
// slack for runtime-internal goroutines).
func TestIntegrationStartStopNoGoroutineLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-clock integration test in short mode")
	}

	// Let any just-spawned goroutines from previous tests settle.
	time.Sleep(50 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	c := New(WithSeconds())

	var fired atomic.Int32
	if _, err := c.AddFunc("* * * * * *", func() {
		fired.Add(1)
	}); err != nil {
		t.Fatalf("AddFunc: %v", err)
	}

	c.Start()
	// Let the run loop actually schedule at least one tick.
	time.Sleep(1200 * time.Millisecond)

	stopStart := time.Now()
	ctx := c.Stop()
	select {
	case <-ctx.Done():
	case <-time.After(1 * time.Second):
		t.Fatalf("scheduler did not stop within 1s (fired=%d)", fired.Load())
	}
	stopElapsed := time.Since(stopStart)
	t.Logf("Stop() completed in %v (fired=%d)", stopElapsed, fired.Load())

	// Allow scheduler goroutines a brief moment to fully unwind.
	time.Sleep(100 * time.Millisecond)

	// Verify no persistent goroutine leak. Allow a small +slack because
	// the Go runtime may keep a few helper goroutines around.
	after := runtime.NumGoroutine()
	if after > baseline+2 {
		buf := make([]byte, 1<<15)
		n := runtime.Stack(buf, true)
		t.Fatalf("goroutine leak: baseline=%d, after Stop=%d\n%s",
			baseline, after, buf[:n])
	}
	t.Logf("goroutine count baseline=%d after_stop=%d", baseline, after)
}

// TestIntegrationMixedIntervalsCounts schedules two jobs (every 1s and
// every 2s), runs ~4s of real time, and asserts each fires the expected
// number of times. Catches coalescing or ordering bugs that would only
// appear under a real ticker.
func TestIntegrationMixedIntervalsCounts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-clock integration test in short mode")
	}

	c := New(WithSeconds())
	c.Start()
	defer c.Stop()

	var everySec, everyTwo atomic.Int32

	if _, err := c.AddFunc("* * * * * *", func() { everySec.Add(1) }); err != nil {
		t.Fatalf("AddFunc 1s: %v", err)
	}
	if _, err := c.AddFunc("*/2 * * * * *", func() { everyTwo.Add(1) }); err != nil {
		t.Fatalf("AddFunc 2s: %v", err)
	}

	time.Sleep(4200 * time.Millisecond)

	s1 := everySec.Load()
	s2 := everyTwo.Load()
	// 1s job should fire ~4 times (3-5 allowable).
	if s1 < 3 || s1 > 5 {
		t.Errorf("every-1s job fired %d times, expected 3-5", s1)
	}
	// 2s job should fire ~2 times (1-3 allowable: boundary alignment).
	if s2 < 1 || s2 > 3 {
		t.Errorf("every-2s job fired %d times, expected 1-3", s2)
	}
	if s1 <= s2 {
		t.Errorf("every-1s (%d) should fire more than every-2s (%d)", s1, s2)
	}
	t.Logf("every-1s=%d every-2s=%d in 4.2s", s1, s2)
}

// TestIntegrationPanickingJobKeepsSchedulerAlive verifies that a job that
// panics on every tick does not take down the scheduler. Uses the
// Recover wrapper and asserts a sibling job continues firing.
func TestIntegrationPanickingJobKeepsSchedulerAlive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-clock integration test in short mode")
	}

	c := New(WithSeconds(), WithChain(Recover(DiscardLogger)))
	c.Start()
	defer c.Stop()

	var panicked, sibling atomic.Int32

	if _, err := c.AddFunc("* * * * * *", func() {
		panicked.Add(1)
		panic("intentional panic for integration test")
	}); err != nil {
		t.Fatalf("AddFunc panicking: %v", err)
	}
	if _, err := c.AddFunc("* * * * * *", func() {
		sibling.Add(1)
	}); err != nil {
		t.Fatalf("AddFunc sibling: %v", err)
	}

	time.Sleep(3200 * time.Millisecond)

	p := panicked.Load()
	s := sibling.Load()
	// Both jobs must have fired multiple times despite panics.
	if p < 2 {
		t.Errorf("panicking job fired %d times, expected >=2 (scheduler may have died)", p)
	}
	if s < 2 {
		t.Errorf("sibling job fired %d times, expected >=2 (scheduler did not survive panic)", s)
	}
	t.Logf("panicking=%d sibling=%d in 3.2s (scheduler survived)", p, s)
}

// contextCancelJob is a JobWithContext that sleeps for a long time or
// until its context is canceled, whichever comes first. It may be
// invoked more than once by the scheduler, so `completed` is a buffered
// channel we send-to (rather than close) to signal the first invocation.
type contextCancelJob struct {
	started   chan struct{}
	completed chan struct{}
	canceled  atomic.Bool
}

func (j *contextCancelJob) Run() {
	j.RunWithContext(context.Background())
}

func (j *contextCancelJob) RunWithContext(ctx context.Context) {
	select {
	case j.started <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
		j.canceled.Store(true)
	case <-time.After(10 * time.Second):
		// Unexpected: ran to completion.
	}
	select {
	case j.completed <- struct{}{}:
	default:
	}
}

// TestIntegrationContextCancelOnStop verifies that a long-running
// JobWithContext receives cancellation when Stop() is called, its
// goroutine completes quickly (not after 10s), and nothing leaks.
func TestIntegrationContextCancelOnStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-clock integration test in short mode")
	}

	c := New(WithSeconds())

	job := &contextCancelJob{
		started:   make(chan struct{}, 1),
		completed: make(chan struct{}, 4),
	}
	if _, err := c.AddJob("* * * * * *", job); err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	c.Start()

	// Wait for the job to actually start.
	select {
	case <-job.started:
	case <-time.After(3 * time.Second):
		t.Fatal("long-running job never started")
	}

	// Cancel (via Stop) after ~1s; baseCtx cancellation cascades to the entry ctx.
	time.Sleep(1 * time.Second)
	stopStart := time.Now()
	ctx := c.Stop()

	// StopAndWait-equivalent: wait for the job waiter to drain through Stop().
	// Use a bounded wait on both the scheduler-stop ctx and the job itself.
	select {
	case <-job.completed:
	case <-time.After(3 * time.Second):
		t.Fatal("long-running job did not observe context cancellation within 3s")
	}
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not finish stopping within 2s")
	}
	stopElapsed := time.Since(stopStart)

	if !job.canceled.Load() {
		t.Error("job context was not observed as canceled")
	}
	// Cancellation should be well under the 10s sleep in the job.
	if stopElapsed > 3*time.Second {
		t.Errorf("Stop() took %v, expected <3s (context cancel not propagating?)", stopElapsed)
	}
	t.Logf("Stop() propagated context cancellation in %v", stopElapsed)
}

// TestIntegrationMiddlewareChainOrder registers three middleware and
// verifies they run in the documented outermost-first order around the
// wrapped job, under the real clock and scheduler.
func TestIntegrationMiddlewareChainOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-clock integration test in short mode")
	}

	var (
		mu     sync.Mutex
		events []string
	)
	record := func(s string) {
		mu.Lock()
		events = append(events, s)
		mu.Unlock()
	}

	// Wrapper factory: records "pre:<name>" before invoking, "post:<name>" after.
	wrap := func(name string) JobWrapper {
		return func(j Job) Job {
			return FuncJob(func() {
				record("pre:" + name)
				j.Run()
				record("post:" + name)
			})
		}
	}

	// NewChain(m1, m2, m3).Then(job) == m1(m2(m3(job))), so m1 is outermost.
	c := New(WithSeconds(), WithChain(wrap("m1"), wrap("m2"), wrap("m3")))

	done := make(chan struct{}, 1)
	if _, err := c.AddFunc("* * * * * *", func() {
		record("job")
		select {
		case done <- struct{}{}:
		default:
		}
	}); err != nil {
		t.Fatalf("AddFunc: %v", err)
	}

	c.Start()
	defer c.Stop()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("job did not run within 3s")
	}

	// Let any post-hooks finish recording.
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	expected := []string{
		"pre:m1", "pre:m2", "pre:m3",
		"job",
		"post:m3", "post:m2", "post:m1",
	}
	// Only compare the first `len(expected)` events: the scheduler may tick
	// again before Stop is reached.
	if len(events) < len(expected) {
		t.Fatalf("captured %d events, expected at least %d: %v", len(events), len(expected), events)
	}
	for i, want := range expected {
		if events[i] != want {
			t.Errorf("event[%d]=%q, want %q (full: %v)", i, events[i], want, events)
			break
		}
	}
	t.Logf("middleware chain executed in order: %v", events[:len(expected)])
}

// TestIntegrationRetryOnErrorSucceedsAfterTransientFailures uses the
// RetryOnError resilience middleware and a job that fails twice then
// succeeds. Asserts it is retried and eventually observed as successful.
func TestIntegrationRetryOnErrorSucceedsAfterTransientFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-clock integration test in short mode")
	}

	// 3 max retries, 50ms initial delay, 500ms max delay, multiplier 2.
	c := New(WithSeconds(), WithChain(
		Recover(DiscardLogger),
		RetryOnError(DiscardLogger, 3, 50*time.Millisecond, 500*time.Millisecond, 2.0),
	))
	c.Start()
	defer c.Stop()

	var attempts atomic.Int32
	succeeded := make(chan struct{}, 1)
	transientErr := errors.New("transient failure")

	if _, err := c.AddJob("* * * * * *", FuncErrorJob(func() error {
		n := attempts.Add(1)
		if n <= 2 {
			return transientErr
		}
		select {
		case succeeded <- struct{}{}:
		default:
		}
		return nil
	})); err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	select {
	case <-succeeded:
	case <-time.After(5 * time.Second):
		t.Fatalf("retry never reached success, attempts=%d", attempts.Load())
	}

	got := attempts.Load()
	if got < 3 {
		t.Errorf("expected >=3 attempts (2 failures + 1 success), got %d", got)
	}
	t.Logf("RetryOnError made %d attempts before success", got)
}

// TestIntegrationCircuitBreakerOpensAfterThreshold installs a
// CircuitBreaker around a job that always panics. The breaker threshold
// is low (2) so it should transition to open quickly and then *skip*
// subsequent executions for the cooldown duration.
func TestIntegrationCircuitBreakerOpensAfterThreshold(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-clock integration test in short mode")
	}

	breaker, handle := CircuitBreakerWithHandle(
		DiscardLogger,
		2,             // threshold
		3*time.Second, // cooldown
	)

	// Recover must be OUTERMOST so the panic from the wrapped job is recovered
	// AFTER the breaker records the failure.
	c := New(WithSeconds(), WithChain(Recover(DiscardLogger), breaker))
	c.Start()
	defer c.Stop()

	var executed atomic.Int32
	if _, err := c.AddFunc("* * * * * *", func() {
		executed.Add(1)
		panic("always fails")
	}); err != nil {
		t.Fatalf("AddFunc: %v", err)
	}

	// Wait long enough for the breaker threshold to be crossed and for
	// additional tick attempts to be skipped while the circuit is open.
	// threshold=2 panics + at least one skipped tick.
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if handle.State() == CircuitOpen {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if handle.State() != CircuitOpen {
		t.Fatalf("circuit did not open within 4s, state=%v failures=%d executed=%d",
			handle.State(), handle.Failures(), executed.Load())
	}

	// Capture execution count when the circuit just opened, then wait
	// another second to confirm the breaker actually skips executions.
	execAtOpen := executed.Load()
	time.Sleep(1 * time.Second)
	execAfter := executed.Load()

	if execAfter > execAtOpen+1 {
		t.Errorf("circuit is open but job still executed: at_open=%d after=%d (breaker not skipping)",
			execAtOpen, execAfter)
	}
	t.Logf("circuit opened after %d executions, skipped further ticks (state=%v failures=%d)",
		execAtOpen, handle.State(), handle.Failures())
}
