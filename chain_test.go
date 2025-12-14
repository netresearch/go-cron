package cron

import (
	"context"
	"io"
	"log"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func appendingJob(slice *[]int, value int) Job {
	var m sync.Mutex
	return FuncJob(func() {
		m.Lock()
		*slice = append(*slice, value)
		m.Unlock()
	})
}

func appendingWrapper(slice *[]int, value int) JobWrapper {
	return func(j Job) Job {
		return FuncJob(func() {
			appendingJob(slice, value).Run()
			j.Run()
		})
	}
}

func TestChain(t *testing.T) {
	var nums []int
	var (
		append1 = appendingWrapper(&nums, 1)
		append2 = appendingWrapper(&nums, 2)
		append3 = appendingWrapper(&nums, 3)
		append4 = appendingJob(&nums, 4)
	)
	NewChain(append1, append2, append3).Then(append4).Run()
	if !reflect.DeepEqual(nums, []int{1, 2, 3, 4}) {
		t.Error("unexpected order of calls:", nums)
	}
}

// testLogCapture is a Logger that captures calls to Info and Error.
type testLogCapture struct {
	infoCalls  []string
	errorCalls []string
	mu         sync.Mutex
}

func (l *testLogCapture) Info(msg string, keysAndValues ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.infoCalls = append(l.infoCalls, msg)
}

func (l *testLogCapture) Error(err error, msg string, keysAndValues ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errorCalls = append(l.errorCalls, msg)
}

func (l *testLogCapture) InfoCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.infoCalls)
}

func (l *testLogCapture) ErrorCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.errorCalls)
}

func TestChainRecover(t *testing.T) {
	panickingJob := FuncJob(func() {
		panic("panickingJob panics")
	})

	t.Run("panic exits job by default", func(t *testing.T) {
		defer func() {
			if err := recover(); err == nil {
				t.Errorf("panic expected, but none received")
			}
		}()
		NewChain().Then(panickingJob).
			Run()
	})

	t.Run("Recovering JobWrapper recovers", func(t *testing.T) {
		NewChain(Recover(PrintfLogger(log.New(io.Discard, "", 0)))).
			Then(panickingJob).
			Run()
	})

	t.Run("composed with the *IfStillRunning wrappers", func(t *testing.T) {
		NewChain(Recover(PrintfLogger(log.New(io.Discard, "", 0)))).
			Then(panickingJob).
			Run()
	})
}

func TestChainRecoverWithLogLevel(t *testing.T) {
	panickingJob := FuncJob(func() {
		panic("test panic")
	})

	t.Run("default logs at Error level", func(t *testing.T) {
		logger := &testLogCapture{}
		NewChain(Recover(logger)).Then(panickingJob).Run()

		if logger.ErrorCount() != 1 {
			t.Errorf("expected 1 Error call, got %d", logger.ErrorCount())
		}
		if logger.InfoCount() != 0 {
			t.Errorf("expected 0 Info calls, got %d", logger.InfoCount())
		}
	})

	t.Run("explicit LogLevelError logs at Error level", func(t *testing.T) {
		logger := &testLogCapture{}
		NewChain(Recover(logger, WithLogLevel(LogLevelError))).Then(panickingJob).Run()

		if logger.ErrorCount() != 1 {
			t.Errorf("expected 1 Error call, got %d", logger.ErrorCount())
		}
		if logger.InfoCount() != 0 {
			t.Errorf("expected 0 Info calls, got %d", logger.InfoCount())
		}
	})

	t.Run("LogLevelInfo logs at Info level", func(t *testing.T) {
		logger := &testLogCapture{}
		NewChain(Recover(logger, WithLogLevel(LogLevelInfo))).Then(panickingJob).Run()

		if logger.InfoCount() != 1 {
			t.Errorf("expected 1 Info call, got %d", logger.InfoCount())
		}
		if logger.ErrorCount() != 0 {
			t.Errorf("expected 0 Error calls, got %d", logger.ErrorCount())
		}
	})

	t.Run("backward compatible - no options works same as before", func(t *testing.T) {
		logger := &testLogCapture{}
		// This is the original call pattern - should still work
		NewChain(Recover(logger)).Then(panickingJob).Run()

		if logger.ErrorCount() != 1 {
			t.Errorf("expected backward compatible behavior (Error), got %d Error calls", logger.ErrorCount())
		}
	})
}

type countJob struct {
	m       sync.Mutex
	started int
	done    int
	delay   time.Duration
}

func (j *countJob) Run() {
	j.m.Lock()
	j.started++
	j.m.Unlock()
	time.Sleep(j.delay)
	j.m.Lock()
	j.done++
	j.m.Unlock()
}

func (j *countJob) Started() int {
	defer j.m.Unlock()
	j.m.Lock()
	return j.started
}

func (j *countJob) Done() int {
	defer j.m.Unlock()
	j.m.Lock()
	return j.done
}

func TestChainDelayIfStillRunning(t *testing.T) {
	t.Run("runs immediately", func(t *testing.T) {
		var j countJob
		wrappedJob := NewChain(DelayIfStillRunning(DiscardLogger)).Then(&j)
		go wrappedJob.Run()
		time.Sleep(2 * time.Millisecond) // Give the job 2ms to complete.
		if c := j.Done(); c != 1 {
			t.Errorf("expected job run once, immediately, got %d", c)
		}
	})

	t.Run("second run immediate if first done", func(t *testing.T) {
		var j countJob
		wrappedJob := NewChain(DelayIfStillRunning(DiscardLogger)).Then(&j)
		go func() {
			go wrappedJob.Run()
			time.Sleep(time.Millisecond)
			go wrappedJob.Run()
		}()
		time.Sleep(3 * time.Millisecond) // Give both jobs 3ms to complete.
		if c := j.Done(); c != 2 {
			t.Errorf("expected job run twice, immediately, got %d", c)
		}
	})

	t.Run("second run delayed if first not done", func(t *testing.T) {
		var j countJob
		j.delay = 10 * time.Millisecond
		wrappedJob := NewChain(DelayIfStillRunning(DiscardLogger)).Then(&j)
		go func() {
			go wrappedJob.Run()
			time.Sleep(time.Millisecond)
			go wrappedJob.Run()
		}()

		// After 5ms, the first job is still in progress, and the second job was
		// run but should be waiting for it to finish.
		time.Sleep(5 * time.Millisecond)
		started, done := j.Started(), j.Done()
		if started != 1 || done != 0 {
			t.Error("expected first job started, but not finished, got", started, done)
		}

		// Verify that the second job completes.
		time.Sleep(25 * time.Millisecond)
		started, done = j.Started(), j.Done()
		if started != 2 || done != 2 {
			t.Error("expected both jobs done, got", started, done)
		}
	})
}

// TestSkipIfStillRunning_RunsImmediately tests that SkipIfStillRunning runs jobs immediately.
func TestSkipIfStillRunning_RunsImmediately(t *testing.T) {
	var j countJob
	wrappedJob := NewChain(SkipIfStillRunning(DiscardLogger)).Then(&j)
	go wrappedJob.Run()
	time.Sleep(2 * time.Millisecond) // Give the job 2ms to complete.
	if c := j.Done(); c != 1 {
		t.Errorf("expected job run once, immediately, got %d", c)
	}
}

// TestSkipIfStillRunning_SecondRunImmediateIfFirstDone tests sequential runs.
func TestSkipIfStillRunning_SecondRunImmediateIfFirstDone(t *testing.T) {
	var j countJob
	wrappedJob := NewChain(SkipIfStillRunning(DiscardLogger)).Then(&j)
	go func() {
		go wrappedJob.Run()
		time.Sleep(time.Millisecond)
		go wrappedJob.Run()
	}()
	time.Sleep(3 * time.Millisecond) // Give both jobs 3ms to complete.
	if c := j.Done(); c != 2 {
		t.Errorf("expected job run twice, immediately, got %d", c)
	}
}

// TestSkipIfStillRunning_SecondRunSkippedIfFirstNotDone tests skipping while running.
func TestSkipIfStillRunning_SecondRunSkippedIfFirstNotDone(t *testing.T) {
	// Use channels for proper synchronization instead of timing
	jobStarted := make(chan struct{})
	jobCanFinish := make(chan struct{})
	jobDone := make(chan struct{})

	var started, done int32
	var startedOnce, doneOnce sync.Once

	job := FuncJob(func() {
		atomic.AddInt32(&started, 1)
		startedOnce.Do(func() { close(jobStarted) })
		<-jobCanFinish
		atomic.AddInt32(&done, 1)
		doneOnce.Do(func() { close(jobDone) })
	})

	wrappedJob := NewChain(SkipIfStillRunning(DiscardLogger)).Then(job)

	// Start first job
	go wrappedJob.Run()

	// Wait for first job to actually start
	select {
	case <-jobStarted:
		// Job started, proceed
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for job to start")
	}

	// Now start second job while first is still running
	secondJobDone := make(chan struct{})
	go func() {
		wrappedJob.Run() // Should be skipped immediately
		close(secondJobDone)
	}()

	// Wait for second job to return (should be skipped immediately)
	select {
	case <-secondJobDone:
		// Second job was skipped, good
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for second job to be skipped")
	}

	// Verify first job still running, second was skipped (inner job ran only once)
	if s, d := atomic.LoadInt32(&started), atomic.LoadInt32(&done); s != 1 || d != 0 {
		t.Errorf("expected first job started but not done, got started=%d done=%d", s, d)
	}

	// Allow first job to finish
	close(jobCanFinish)

	// Wait for first job to complete
	select {
	case <-jobDone:
		// Job finished
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for job to finish")
	}

	// Verify only first job ran
	if s, d := atomic.LoadInt32(&started), atomic.LoadInt32(&done); s != 1 || d != 1 {
		t.Errorf("expected only first job to have run, got started=%d done=%d", s, d)
	}
}

// TestSkipIfStillRunning_Skip10JobsOnRapidFire tests skipping multiple rapid-fire jobs.
func TestSkipIfStillRunning_Skip10JobsOnRapidFire(t *testing.T) {
	var j countJob
	j.delay = 10 * time.Millisecond
	wrappedJob := NewChain(SkipIfStillRunning(DiscardLogger)).Then(&j)
	for i := 0; i < 11; i++ {
		go wrappedJob.Run()
	}
	time.Sleep(200 * time.Millisecond)
	done := j.Done()
	if done != 1 {
		t.Error("expected 1 jobs executed, 10 jobs dropped, got", done)
	}
}

// TestSkipIfStillRunning_DifferentJobsIndependent tests that different jobs are independent.
func TestSkipIfStillRunning_DifferentJobsIndependent(t *testing.T) {
	var j1, j2 countJob
	j1.delay = 10 * time.Millisecond
	j2.delay = 10 * time.Millisecond
	chain := NewChain(SkipIfStillRunning(DiscardLogger))
	wrappedJob1 := chain.Then(&j1)
	wrappedJob2 := chain.Then(&j2)
	for i := 0; i < 11; i++ {
		go wrappedJob1.Run()
		go wrappedJob2.Run()
	}
	time.Sleep(100 * time.Millisecond)
	var (
		done1 = j1.Done()
		done2 = j2.Done()
	)
	if done1 != 1 || done2 != 1 {
		t.Error("expected both jobs executed once, got", done1, "and", done2)
	}
}

func TestChainTimeout(t *testing.T) {
	t.Run("job completes within timeout", func(t *testing.T) {
		var j countJob
		j.delay = 5 * time.Millisecond
		wrappedJob := NewChain(Timeout(DiscardLogger, 50*time.Millisecond)).Then(&j)
		wrappedJob.Run()
		if c := j.Done(); c != 1 {
			t.Errorf("expected job to complete, got done count %d", c)
		}
	})

	t.Run("job exceeds timeout", func(t *testing.T) {
		var j countJob
		j.delay = 50 * time.Millisecond
		wrappedJob := NewChain(Timeout(DiscardLogger, 5*time.Millisecond)).Then(&j)

		start := time.Now()
		wrappedJob.Run()
		elapsed := time.Since(start)

		// Wrapper should return quickly after timeout
		if elapsed > 20*time.Millisecond {
			t.Errorf("expected wrapper to return after timeout (~5ms), took %v", elapsed)
		}

		// Job should not be done yet (still running in background)
		if c := j.Done(); c != 0 {
			t.Errorf("expected job not done yet (abandoned), got %d", c)
		}

		// Wait for abandoned goroutine to complete
		time.Sleep(100 * time.Millisecond)
		if c := j.Done(); c != 1 {
			t.Errorf("expected abandoned job to eventually complete, got %d", c)
		}
	})

	t.Run("zero timeout passes through unchanged", func(t *testing.T) {
		var j countJob
		j.delay = 5 * time.Millisecond
		wrappedJob := NewChain(Timeout(DiscardLogger, 0)).Then(&j)
		wrappedJob.Run()
		if c := j.Done(); c != 1 {
			t.Errorf("expected job to complete with zero timeout, got %d", c)
		}
	})

	t.Run("negative timeout passes through unchanged", func(t *testing.T) {
		var j countJob
		j.delay = 5 * time.Millisecond
		wrappedJob := NewChain(Timeout(DiscardLogger, -1*time.Second)).Then(&j)
		wrappedJob.Run()
		if c := j.Done(); c != 1 {
			t.Errorf("expected job to complete with negative timeout, got %d", c)
		}
	})

	t.Run("multiple jobs with different timeouts", func(t *testing.T) {
		var j1, j2 countJob
		j1.delay = 5 * time.Millisecond
		j2.delay = 100 * time.Millisecond

		// Short timeout - j1 should complete, j2 should timeout
		chain := NewChain(Timeout(DiscardLogger, 20*time.Millisecond))
		wrappedJob1 := chain.Then(&j1)
		wrappedJob2 := chain.Then(&j2)

		wrappedJob1.Run()
		wrappedJob2.Run()

		if c := j1.Done(); c != 1 {
			t.Errorf("expected j1 to complete, got %d", c)
		}
		if c := j2.Done(); c != 0 {
			t.Errorf("expected j2 to timeout (not done yet), got %d", c)
		}

		// Wait for abandoned j2 to complete
		time.Sleep(150 * time.Millisecond)
		if c := j2.Done(); c != 1 {
			t.Errorf("expected j2 to eventually complete, got %d", c)
		}
	})

	t.Run("combined with other wrappers", func(t *testing.T) {
		var j countJob
		j.delay = 5 * time.Millisecond
		// Timeout + Recover combination
		wrappedJob := NewChain(Recover(DiscardLogger), Timeout(DiscardLogger, 50*time.Millisecond)).Then(&j)
		wrappedJob.Run()
		if c := j.Done(); c != 1 {
			t.Errorf("expected job to complete with combined wrappers, got %d", c)
		}
	})

	t.Run("panic propagates to Recover wrapper when job completes within timeout", func(t *testing.T) {
		panicJob := FuncJob(func() {
			panic("test panic")
		})
		// Recover should catch the panic propagated from Timeout
		wrappedJob := NewChain(Recover(DiscardLogger), Timeout(DiscardLogger, 50*time.Millisecond)).Then(panicJob)
		// This should not panic because Recover catches it
		wrappedJob.Run()
	})

	t.Run("panic after timeout does not propagate", func(t *testing.T) {
		panicJob := FuncJob(func() {
			time.Sleep(20 * time.Millisecond)
			panic("delayed panic")
		})
		// Timeout returns before panic occurs
		wrappedJob := NewChain(Recover(DiscardLogger), Timeout(DiscardLogger, 5*time.Millisecond)).Then(panicJob)
		wrappedJob.Run()
		// Wait for abandoned goroutine's panic (won't propagate)
		time.Sleep(50 * time.Millisecond)
	})

	t.Run("callback invoked on timeout", func(t *testing.T) {
		var callbackCalled int32
		var callbackTimeout time.Duration

		callback := func(timeout time.Duration) {
			atomic.AddInt32(&callbackCalled, 1)
			callbackTimeout = timeout
		}

		var j countJob
		j.delay = 50 * time.Millisecond
		wrappedJob := NewChain(Timeout(DiscardLogger, 5*time.Millisecond, WithTimeoutCallback(callback))).Then(&j)
		wrappedJob.Run()

		// Callback should have been invoked
		if c := atomic.LoadInt32(&callbackCalled); c != 1 {
			t.Errorf("expected callback to be called once, got %d", c)
		}
		if callbackTimeout != 5*time.Millisecond {
			t.Errorf("expected callback to receive timeout of 5ms, got %v", callbackTimeout)
		}

		// Wait for abandoned goroutine
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("callback not invoked when job completes in time", func(t *testing.T) {
		var callbackCalled int32

		callback := func(timeout time.Duration) {
			atomic.AddInt32(&callbackCalled, 1)
		}

		var j countJob
		j.delay = 5 * time.Millisecond
		wrappedJob := NewChain(Timeout(DiscardLogger, 50*time.Millisecond, WithTimeoutCallback(callback))).Then(&j)
		wrappedJob.Run()

		// Callback should NOT have been invoked
		if c := atomic.LoadInt32(&callbackCalled); c != 0 {
			t.Errorf("expected callback not to be called, got %d calls", c)
		}
	})
}

// TestTimeoutWithContextCancellation tests that onTimeout callback is NOT called
// when context is canceled (vs when it times out). This kills the mutation at
// chain.go:418 where ctx.Err() == context.DeadlineExceeded could be negated.
func TestTimeoutWithContextCancellation(t *testing.T) {
	t.Run("callback not invoked on context cancellation", func(t *testing.T) {
		var callbackCalled int32
		var errorLogged int32

		callback := func(timeout time.Duration) {
			atomic.AddInt32(&callbackCalled, 1)
		}

		logger := &testLogCapture{}

		// Create a long-running job
		jobStarted := make(chan struct{})
		jobCanFinish := make(chan struct{})
		job := FuncJob(func() {
			close(jobStarted)
			<-jobCanFinish
		})

		// Use TimeoutWithContext with a long timeout
		wrapper := TimeoutWithContext(logger, 10*time.Second, WithTimeoutCallback(callback))
		wrappedJob := wrapper(job)

		// Get the timeoutContextJob to call RunWithContext
		tcj := wrappedJob.(*timeoutContextJob)

		// Create a cancellable context (NOT a timeout context)
		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan struct{})
		go func() {
			tcj.RunWithContext(ctx)
			close(done)
		}()

		// Wait for job to start
		<-jobStarted

		// Cancel the context (simulates parent cancellation, NOT timeout)
		cancel()

		// Allow job to finish
		close(jobCanFinish)

		// Wait for wrapper to complete
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for job wrapper to complete")
		}

		// Callback should NOT be called because context was canceled, not timed out
		if c := atomic.LoadInt32(&callbackCalled); c != 0 {
			t.Errorf("expected callback NOT called on cancellation, got %d calls", c)
		}

		// Error log should NOT be called (only called on DeadlineExceeded)
		if c := logger.ErrorCount(); c != 0 {
			t.Errorf("expected no error log on cancellation, got %d", c)
		}

		_ = errorLogged // silence unused variable
	})

	t.Run("callback invoked on actual timeout", func(t *testing.T) {
		var callbackCalled int32

		callback := func(timeout time.Duration) {
			atomic.AddInt32(&callbackCalled, 1)
		}

		logger := &testLogCapture{}

		// Create a job that takes longer than timeout
		job := FuncJob(func() {
			time.Sleep(100 * time.Millisecond)
		})

		wrapper := TimeoutWithContext(logger, 10*time.Millisecond, WithTimeoutCallback(callback))
		wrappedJob := wrapper(job)

		wrappedJob.Run()

		// Wait for any cleanup
		time.Sleep(50 * time.Millisecond)

		// Callback SHOULD be called because job actually timed out
		if c := atomic.LoadInt32(&callbackCalled); c != 1 {
			t.Errorf("expected callback called once on timeout, got %d calls", c)
		}

		// Error log SHOULD be called
		if c := logger.ErrorCount(); c != 1 {
			t.Errorf("expected error log on timeout, got %d", c)
		}
	})
}

// TestTimeoutZeroBoundary tests the boundary condition at chain.go:361
// where timeout <= 0 returns the original job unchanged.
// This kills mutations that change <= to < (boundary mutation).
func TestTimeoutZeroBoundary(t *testing.T) {
	tests := []struct {
		name            string
		timeout         time.Duration
		shouldBeWrapped bool
	}{
		{"negative timeout returns original", -1 * time.Second, false},
		{"zero timeout returns original", 0, false},
		{"positive timeout wraps job", 1 * time.Millisecond, true},
	}

	// Test Timeout wrapper using a marker job to detect wrapping
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalJob := FuncJob(func() {})

			wrapper := Timeout(DiscardLogger, tt.timeout)
			result := wrapper(originalJob)

			// For FuncJob, we can't easily detect wrapping by type.
			// Instead, check that when timeout <= 0, the returned job
			// is literally the same FuncJob (reflect.ValueOf comparison)
			originalVal := reflect.ValueOf(originalJob)
			resultVal := reflect.ValueOf(result)

			// If not wrapped, the pointers should be equal
			// If wrapped, the result is a new FuncJob
			isSameJob := originalVal.Pointer() == resultVal.Pointer()
			isWrapped := !isSameJob

			if isWrapped != tt.shouldBeWrapped {
				t.Errorf("Timeout(%v): wrapped=%v, want wrapped=%v",
					tt.timeout, isWrapped, tt.shouldBeWrapped)
			}
		})
	}

	// Test TimeoutWithContext wrapper
	for _, tt := range tests {
		t.Run("WithContext_"+tt.name, func(t *testing.T) {
			originalJob := FuncJob(func() {})
			wrapper := TimeoutWithContext(DiscardLogger, tt.timeout)
			result := wrapper(originalJob)

			// TimeoutWithContext returns *timeoutContextJob when wrapping
			_, isWrapped := result.(*timeoutContextJob)

			if isWrapped != tt.shouldBeWrapped {
				t.Errorf("TimeoutWithContext(%v): wrapped=%v, want wrapped=%v",
					tt.timeout, isWrapped, tt.shouldBeWrapped)
			}
		})
	}
}
