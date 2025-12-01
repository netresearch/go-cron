package cron

import (
	"io"
	"log"
	"reflect"
	"sync"
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

func TestChainSkipIfStillRunning(t *testing.T) {
	t.Run("runs immediately", func(t *testing.T) {
		var j countJob
		wrappedJob := NewChain(SkipIfStillRunning(DiscardLogger)).Then(&j)
		go wrappedJob.Run()
		time.Sleep(2 * time.Millisecond) // Give the job 2ms to complete.
		if c := j.Done(); c != 1 {
			t.Errorf("expected job run once, immediately, got %d", c)
		}
	})

	t.Run("second run immediate if first done", func(t *testing.T) {
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
	})

	t.Run("second run skipped if first not done", func(t *testing.T) {
		var j countJob
		j.delay = 10 * time.Millisecond
		wrappedJob := NewChain(SkipIfStillRunning(DiscardLogger)).Then(&j)
		go func() {
			go wrappedJob.Run()
			time.Sleep(time.Millisecond)
			go wrappedJob.Run()
		}()

		// After 5ms, the first job is still in progress, and the second job was
		// aleady skipped.
		time.Sleep(5 * time.Millisecond)
		started, done := j.Started(), j.Done()
		if started != 1 || done != 0 {
			t.Error("expected first job started, but not finished, got", started, done)
		}

		// Verify that the first job completes and second does not run.
		time.Sleep(25 * time.Millisecond)
		started, done = j.Started(), j.Done()
		if started != 1 || done != 1 {
			t.Error("expected second job skipped, got", started, done)
		}
	})

	t.Run("skip 10 jobs on rapid fire", func(t *testing.T) {
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
	})

	t.Run("different jobs independent", func(t *testing.T) {
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
	})
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
}
