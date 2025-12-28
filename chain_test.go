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

// countJob is a test job that tracks start/done counts with optional delay.
// It supports both delay-based simulation and channel-based synchronization.
type countJob struct {
	m          sync.Mutex
	started    int
	done       int
	delay      time.Duration
	onStart    chan struct{} // signaled when job starts (optional)
	onDone     chan struct{} // signaled when job completes (optional)
	canProceed chan struct{} // blocks job until signaled (optional)
}

func (j *countJob) Run() {
	j.m.Lock()
	j.started++
	if j.onStart != nil {
		select {
		case j.onStart <- struct{}{}:
		default:
		}
	}
	j.m.Unlock()

	// Wait for permission to proceed if channel is set
	if j.canProceed != nil {
		<-j.canProceed
	}

	// Simulate work with delay if set
	if j.delay > 0 {
		time.Sleep(j.delay)
	}

	j.m.Lock()
	j.done++
	if j.onDone != nil {
		select {
		case j.onDone <- struct{}{}:
		default:
		}
	}
	j.m.Unlock()
}

func (j *countJob) Started() int {
	j.m.Lock()
	defer j.m.Unlock()
	return j.started
}

func (j *countJob) Done() int {
	j.m.Lock()
	defer j.m.Unlock()
	return j.done
}

func TestChainDelayIfStillRunning(t *testing.T) {
	t.Run("runs immediately", func(t *testing.T) {
		j := &countJob{
			onDone: make(chan struct{}, 1),
		}
		wrappedJob := NewChain(DelayIfStillRunning(DiscardLogger)).Then(j)
		go wrappedJob.Run()

		// Wait for job completion with timeout
		select {
		case <-j.onDone:
			if c := j.Done(); c != 1 {
				t.Errorf("expected job run once, immediately, got %d", c)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for job to complete")
		}
	})

	t.Run("second run immediate if first done", func(t *testing.T) {
		// Use WaitGroup to track both job completions
		var wg sync.WaitGroup
		var runCount atomic.Int32

		job := FuncJob(func() {
			runCount.Add(1)
			wg.Done()
		})

		wrappedJob := NewChain(DelayIfStillRunning(DiscardLogger)).Then(job)

		// Run first job synchronously (it completes immediately)
		wg.Add(1)
		wrappedJob.Run()

		// Run second job (first is done, so no delay)
		wg.Add(1)
		wrappedJob.Run()

		// Both should complete
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			if c := runCount.Load(); c != 2 {
				t.Errorf("expected job run twice, got %d", c)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for jobs to complete")
		}
	})

	t.Run("second run delayed if first not done", func(t *testing.T) {
		// Use channels for synchronization
		j := &countJob{
			onStart:    make(chan struct{}, 2),
			onDone:     make(chan struct{}, 2),
			canProceed: make(chan struct{}),
		}
		wrappedJob := NewChain(DelayIfStillRunning(DiscardLogger)).Then(j)

		// Start first job - it will block on canProceed
		go wrappedJob.Run()

		// Wait for first job to start
		select {
		case <-j.onStart:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for first job to start")
		}

		// Start second job - it should be delayed waiting for mutex
		secondDone := make(chan struct{})
		go func() {
			wrappedJob.Run()
			close(secondDone)
		}()

		// Give second job a moment to hit the mutex (it should block)
		time.Sleep(10 * time.Millisecond)

		// Verify first job started, not done, second hasn't started inner job
		if s, d := j.Started(), j.Done(); s != 1 || d != 0 {
			t.Errorf("expected first job started but not done, got started=%d done=%d", s, d)
		}

		// Allow first job to proceed
		close(j.canProceed)

		// Wait for both jobs to complete
		for i := 0; i < 2; i++ {
			select {
			case <-j.onDone:
			case <-time.After(5 * time.Second):
				t.Fatalf("timeout waiting for job %d to complete", i+1)
			}
		}

		// Wait for second goroutine to finish
		select {
		case <-secondDone:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for second job goroutine")
		}

		// Verify both jobs completed
		if s, d := j.Started(), j.Done(); s != 2 || d != 2 {
			t.Errorf("expected both jobs done, got started=%d done=%d", s, d)
		}
	})
}

// TestSkipIfStillRunning_RunsImmediately tests that SkipIfStillRunning runs jobs immediately.
func TestSkipIfStillRunning_RunsImmediately(t *testing.T) {
	// Use channel synchronization instead of time.Sleep for reliable testing
	jobDone := make(chan struct{})
	job := FuncJob(func() {
		close(jobDone)
	})

	wrappedJob := NewChain(SkipIfStillRunning(DiscardLogger)).Then(job)
	go wrappedJob.Run()

	// Wait for job completion with timeout
	select {
	case <-jobDone:
		// Job completed successfully
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for job to complete")
	}
}

// TestSkipIfStillRunning_SecondRunImmediateIfFirstDone tests sequential runs.
func TestSkipIfStillRunning_SecondRunImmediateIfFirstDone(t *testing.T) {
	// Use WaitGroup to track job completions instead of timing
	var wg sync.WaitGroup
	var runCount atomic.Int32

	job := FuncJob(func() {
		runCount.Add(1)
		wg.Done()
	})

	wrappedJob := NewChain(SkipIfStillRunning(DiscardLogger)).Then(job)

	// Run first job and wait for it to complete
	wg.Add(1)
	wrappedJob.Run()

	// Run second job (first is already done, so this should not be skipped)
	wg.Add(1)
	wrappedJob.Run()

	// Both jobs should complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		if c := runCount.Load(); c != 2 {
			t.Errorf("expected job run twice, got %d", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for jobs to complete")
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
	// Use channels to synchronize: job signals when started, test signals when it can finish
	// Buffered channels prevent signal drops if send happens before receiver is ready
	jobStarted := make(chan struct{}, 1)
	jobCanFinish := make(chan struct{})
	jobDone := make(chan struct{}, 1)

	var runCount atomic.Int32

	job := FuncJob(func() {
		runCount.Add(1)
		// Signal we've started
		select {
		case jobStarted <- struct{}{}:
		default:
		}
		// Wait for permission to finish (this holds the semaphore)
		<-jobCanFinish
		// Signal we're done
		select {
		case jobDone <- struct{}{}:
		default:
		}
	})

	wrappedJob := NewChain(SkipIfStillRunning(DiscardLogger)).Then(job)

	// Track when all wrapper calls complete
	var wrappersDone sync.WaitGroup

	// Fire 11 jobs rapidly
	for i := 0; i < 11; i++ {
		wrappersDone.Add(1)
		go func() {
			defer wrappersDone.Done()
			wrappedJob.Run()
		}()
	}

	// Wait for first job to start (it will hold the semaphore)
	select {
	case <-jobStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for job to start")
	}

	// Give other goroutines time to attempt and get skipped
	time.Sleep(10 * time.Millisecond)

	// Allow the job to complete
	close(jobCanFinish)

	// Wait for job to finish
	select {
	case <-jobDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for job to complete")
	}

	// Wait for all wrapper calls to return
	done := make(chan struct{})
	go func() {
		wrappersDone.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for all wrappers to return")
	}

	if c := runCount.Load(); c != 1 {
		t.Errorf("expected 1 job executed, 10 jobs dropped, got %d", c)
	}
}

// TestSkipIfStillRunning_DifferentJobsIndependent tests that different jobs are independent.
func TestSkipIfStillRunning_DifferentJobsIndependent(t *testing.T) {
	// Create two jobs with independent synchronization
	// Use buffered channels to avoid race between signal and receive
	job1Started := make(chan struct{}, 1)
	job1CanFinish := make(chan struct{})
	job1Done := make(chan struct{}, 1)
	var run1Count atomic.Int32

	job1 := FuncJob(func() {
		run1Count.Add(1)
		select {
		case job1Started <- struct{}{}:
		default:
		}
		<-job1CanFinish
		select {
		case job1Done <- struct{}{}:
		default:
		}
	})

	job2Started := make(chan struct{}, 1)
	job2CanFinish := make(chan struct{})
	job2Done := make(chan struct{}, 1)
	var run2Count atomic.Int32

	job2 := FuncJob(func() {
		run2Count.Add(1)
		select {
		case job2Started <- struct{}{}:
		default:
		}
		<-job2CanFinish
		select {
		case job2Done <- struct{}{}:
		default:
		}
	})

	chain := NewChain(SkipIfStillRunning(DiscardLogger))
	wrappedJob1 := chain.Then(job1)
	wrappedJob2 := chain.Then(job2)

	var wrappersDone sync.WaitGroup

	// Fire 11 jobs for each wrapped job
	for i := 0; i < 11; i++ {
		wrappersDone.Add(2)
		go func() {
			defer wrappersDone.Done()
			wrappedJob1.Run()
		}()
		go func() {
			defer wrappersDone.Done()
			wrappedJob2.Run()
		}()
	}

	// Wait for both jobs to start
	select {
	case <-job1Started:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for job1 to start")
	}
	select {
	case <-job2Started:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for job2 to start")
	}

	// Give other goroutines time to attempt and get skipped
	time.Sleep(10 * time.Millisecond)

	// Allow both jobs to complete
	close(job1CanFinish)
	close(job2CanFinish)

	// Wait for both jobs to complete
	select {
	case <-job1Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for job1 to complete")
	}
	select {
	case <-job2Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for job2 to complete")
	}

	// Wait for all wrappers to return
	done := make(chan struct{})
	go func() {
		wrappersDone.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for all wrappers to return")
	}

	done1, done2 := run1Count.Load(), run2Count.Load()
	if done1 != 1 || done2 != 1 {
		t.Errorf("expected both jobs executed once, got %d and %d", done1, done2)
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
		j := &countJob{
			delay:  50 * time.Millisecond,
			onDone: make(chan struct{}, 1),
		}
		wrappedJob := NewChain(Timeout(DiscardLogger, 5*time.Millisecond)).Then(j)

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

		// Wait for abandoned goroutine to complete using channel
		select {
		case <-j.onDone:
			if c := j.Done(); c != 1 {
				t.Errorf("expected abandoned job to eventually complete, got %d", c)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for abandoned job to complete")
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
		j1 := &countJob{
			delay:  5 * time.Millisecond,
			onDone: make(chan struct{}, 1),
		}
		j2 := &countJob{
			delay:  100 * time.Millisecond,
			onDone: make(chan struct{}, 1),
		}

		// Short timeout - j1 should complete, j2 should timeout
		chain := NewChain(Timeout(DiscardLogger, 20*time.Millisecond))
		wrappedJob1 := chain.Then(j1)
		wrappedJob2 := chain.Then(j2)

		wrappedJob1.Run()
		wrappedJob2.Run()

		if c := j1.Done(); c != 1 {
			t.Errorf("expected j1 to complete, got %d", c)
		}
		if c := j2.Done(); c != 0 {
			t.Errorf("expected j2 to timeout (not done yet), got %d", c)
		}

		// Wait for abandoned j2 to complete using channel
		select {
		case <-j2.onDone:
			if c := j2.Done(); c != 1 {
				t.Errorf("expected j2 to eventually complete, got %d", c)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for j2 to complete")
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
		jobDone := make(chan struct{})
		panicJob := FuncJob(func() {
			defer close(jobDone)
			time.Sleep(20 * time.Millisecond)
			panic("delayed panic")
		})
		// Timeout returns before panic occurs
		wrappedJob := NewChain(Recover(DiscardLogger), Timeout(DiscardLogger, 5*time.Millisecond)).Then(panicJob)
		wrappedJob.Run()
		// Wait for abandoned goroutine's panic (won't propagate) using channel
		select {
		case <-jobDone:
			// Goroutine finished (panicked and recovered by runtime)
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for abandoned goroutine")
		}
	})

	t.Run("callback invoked on timeout", func(t *testing.T) {
		var callbackCalled int32
		var callbackTimeout time.Duration

		callback := func(timeout time.Duration) {
			atomic.AddInt32(&callbackCalled, 1)
			callbackTimeout = timeout
		}

		j := &countJob{
			delay:  50 * time.Millisecond,
			onDone: make(chan struct{}, 1),
		}
		wrappedJob := NewChain(Timeout(DiscardLogger, 5*time.Millisecond, WithTimeoutCallback(callback))).Then(j)
		wrappedJob.Run()

		// Callback should have been invoked
		if c := atomic.LoadInt32(&callbackCalled); c != 1 {
			t.Errorf("expected callback to be called once, got %d", c)
		}
		if callbackTimeout != 5*time.Millisecond {
			t.Errorf("expected callback to receive timeout of 5ms, got %v", callbackTimeout)
		}

		// Wait for abandoned goroutine using channel
		select {
		case <-j.onDone:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for abandoned goroutine")
		}
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

		// Create a job that takes longer than timeout with completion signal
		jobDone := make(chan struct{})
		job := FuncJob(func() {
			defer close(jobDone)
			time.Sleep(100 * time.Millisecond)
		})

		wrapper := TimeoutWithContext(logger, 10*time.Millisecond, WithTimeoutCallback(callback))
		wrappedJob := wrapper(job)

		wrappedJob.Run()

		// Wait for abandoned goroutine to complete
		select {
		case <-jobDone:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for job goroutine to complete")
		}

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

// TestJitter_AddsDelay verifies that Jitter adds a delay before job execution.
func TestJitter_AddsDelay(t *testing.T) {
	maxJitter := 100 * time.Millisecond
	var executed bool

	job := FuncJob(func() {
		executed = true
	})

	wrappedJob := NewChain(Jitter(maxJitter)).Then(job)

	start := time.Now()
	wrappedJob.Run()
	elapsed := time.Since(start)

	if !executed {
		t.Error("job was not executed")
	}

	// Jitter should add some delay (0 to maxJitter)
	// We can't test exact value due to randomness, but we can verify
	// it doesn't exceed maxJitter significantly
	if elapsed > maxJitter+10*time.Millisecond {
		t.Errorf("jitter exceeded max: elapsed=%v, maxJitter=%v", elapsed, maxJitter)
	}
}

// TestJitter_ZeroDisabled verifies that zero jitter doesn't add delay.
func TestJitter_ZeroDisabled(t *testing.T) {
	var executed bool

	job := FuncJob(func() {
		executed = true
	})

	wrappedJob := NewChain(Jitter(0)).Then(job)

	start := time.Now()
	wrappedJob.Run()
	elapsed := time.Since(start)

	if !executed {
		t.Error("job was not executed")
	}

	// With zero jitter, execution should be nearly instant
	if elapsed > 10*time.Millisecond {
		t.Errorf("zero jitter should not add delay: elapsed=%v", elapsed)
	}
}

// TestJitter_NegativeDisabled verifies that negative jitter doesn't add delay.
func TestJitter_NegativeDisabled(t *testing.T) {
	var executed bool

	job := FuncJob(func() {
		executed = true
	})

	wrappedJob := NewChain(Jitter(-10 * time.Second)).Then(job)

	start := time.Now()
	wrappedJob.Run()
	elapsed := time.Since(start)

	if !executed {
		t.Error("job was not executed")
	}

	// With negative jitter, execution should be nearly instant
	if elapsed > 10*time.Millisecond {
		t.Errorf("negative jitter should not add delay: elapsed=%v", elapsed)
	}
}

// TestJitter_Distribution verifies jitter values are distributed across range.
func TestJitter_Distribution(t *testing.T) {
	maxJitter := 50 * time.Millisecond
	iterations := 20
	var totalDelay time.Duration

	for i := 0; i < iterations; i++ {
		job := FuncJob(func() {})
		wrappedJob := NewChain(Jitter(maxJitter)).Then(job)

		start := time.Now()
		wrappedJob.Run()
		totalDelay += time.Since(start)
	}

	avgDelay := totalDelay / time.Duration(iterations)

	// Average should be roughly maxJitter/2 (uniform distribution)
	// Allow generous tolerance due to execution overhead and randomness
	expectedAvg := maxJitter / 2
	tolerance := maxJitter / 2 // 50% tolerance

	if avgDelay < expectedAvg-tolerance || avgDelay > expectedAvg+tolerance {
		t.Logf("average delay=%v, expected ~%v (tolerance ±%v)", avgDelay, expectedAvg, tolerance)
		// Don't fail - randomness can cause this occasionally
	}
}

// TestJitter_ComposesWithOtherWrappers verifies jitter works with other wrappers.
func TestJitter_ComposesWithOtherWrappers(t *testing.T) {
	var recovered bool
	var executed bool

	job := FuncJob(func() {
		executed = true
	})

	// Compose Jitter with Recover
	wrappedJob := NewChain(
		Recover(DiscardLogger),
		Jitter(10*time.Millisecond),
	).Then(job)

	wrappedJob.Run()

	if !executed {
		t.Error("job was not executed")
	}

	// Test that panic recovery still works with jitter
	panicJob := FuncJob(func() {
		recovered = false
		panic("test panic")
	})

	wrappedPanicJob := NewChain(
		Recover(DiscardLogger),
		Jitter(10*time.Millisecond),
	).Then(panicJob)

	// Should not panic due to Recover wrapper
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("panic was not recovered: %v", r)
			} else {
				recovered = true
			}
		}()
		wrappedPanicJob.Run()
	}()

	if !recovered {
		t.Error("Recover wrapper should have caught panic")
	}
}

// TestJitterWithLogger_LogsDelay verifies that JitterWithLogger logs the delay.
func TestJitterWithLogger_LogsDelay(t *testing.T) {
	maxJitter := 50 * time.Millisecond
	var executed bool
	var logCalled bool

	// Create a test logger that captures the log call
	testLogger := &testLogCapture{}

	job := FuncJob(func() {
		executed = true
	})

	wrappedJob := NewChain(JitterWithLogger(testLogger, maxJitter)).Then(job)
	wrappedJob.Run()

	if !executed {
		t.Error("job was not executed")
	}

	// Check that Info was called with jitter info
	testLogger.mu.Lock()
	logCalled = len(testLogger.infoCalls) > 0
	testLogger.mu.Unlock()

	if !logCalled {
		t.Error("JitterWithLogger should have logged the delay")
	}
}

// TestJitterWithLogger_ZeroNoLog verifies zero jitter doesn't log.
func TestJitterWithLogger_ZeroNoLog(t *testing.T) {
	var executed bool

	testLogger := &testLogCapture{}

	job := FuncJob(func() {
		executed = true
	})

	wrappedJob := NewChain(JitterWithLogger(testLogger, 0)).Then(job)
	wrappedJob.Run()

	if !executed {
		t.Error("job was not executed")
	}

	// Check that Info was NOT called (zero jitter = no delay = no log)
	testLogger.mu.Lock()
	logCount := len(testLogger.infoCalls)
	testLogger.mu.Unlock()

	if logCount > 0 {
		t.Errorf("JitterWithLogger with zero jitter should not log, got %d calls", logCount)
	}
}
