package cron

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"
)

// JobWrapper decorates the given Job with some behavior.
type JobWrapper func(Job) Job

// Chain is a sequence of JobWrappers that decorates submitted jobs with
// cross-cutting behaviors like logging or synchronization.
type Chain struct {
	wrappers []JobWrapper
}

// NewChain returns a Chain consisting of the given JobWrappers.
func NewChain(c ...JobWrapper) Chain {
	return Chain{c}
}

// Then decorates the given job with all JobWrappers in the chain.
//
// This:
//
//	NewChain(m1, m2, m3).Then(job)
//
// is equivalent to:
//
//	m1(m2(m3(job)))
func (c Chain) Then(j Job) Job {
	for i := range c.wrappers {
		j = c.wrappers[len(c.wrappers)-i-1](j)
	}
	return j
}

// Recover panics in wrapped jobs and log them with the provided logger.
func Recover(logger Logger) JobWrapper {
	return func(j Job) Job {
		return FuncJob(func() {
			defer func() {
				if r := recover(); r != nil {
					const size = 64 << 10
					buf := make([]byte, size)
					buf = buf[:runtime.Stack(buf, false)]
					err, ok := r.(error)
					if !ok {
						err = fmt.Errorf("%v", r)
					}
					logger.Error(err, "panic", "stack", "...\n"+string(buf))
				}
			}()
			j.Run()
		})
	}
}

// DelayIfStillRunning serializes jobs, delaying subsequent runs until the
// previous one is complete. Jobs running after a delay of more than a minute
// have the delay logged at Info.
func DelayIfStillRunning(logger Logger) JobWrapper {
	return func(j Job) Job {
		var mu sync.Mutex
		return FuncJob(func() {
			start := time.Now()
			mu.Lock()
			defer mu.Unlock()
			if dur := time.Since(start); dur > time.Minute {
				logger.Info("delay", "duration", dur)
			}
			j.Run()
		})
	}
}

// SkipIfStillRunning skips an invocation of the Job if a previous invocation is
// still running. It logs skips to the given logger at Info level.
func SkipIfStillRunning(logger Logger) JobWrapper {
	return func(j Job) Job {
		ch := make(chan struct{}, 1)
		ch <- struct{}{}
		return FuncJob(func() {
			select {
			case v := <-ch:
				defer func() { ch <- v }()
				j.Run()
			default:
				logger.Info("skip")
			}
		})
	}
}

// Timeout wraps a job with a timeout. If the job takes longer than the given
// duration, the wrapper returns and logs an error, but the underlying job
// goroutine continues running until completion.
//
// WARNING: This implements an "abandonment model" - when a timeout occurs,
// the wrapper returns but the job's goroutine is NOT canceled. The job will
// continue executing in the background until it naturally completes. This means:
//   - Resources held by the job will not be released until completion
//   - Side effects will still occur even after timeout
//   - Multiple abandoned goroutines can accumulate if jobs consistently timeout
//
// For jobs that need true cancellation support, consider implementing the Job
// interface with context awareness and checking for cancellation signals.
//
// A timeout of zero or negative disables the timeout and returns the job unchanged.
func Timeout(logger Logger, timeout time.Duration) JobWrapper {
	return func(j Job) Job {
		if timeout <= 0 {
			return j
		}
		return FuncJob(func() {
			done := make(chan struct{})
			var panicVal interface{}
			go func() {
				defer close(done)
				defer func() {
					if r := recover(); r != nil {
						panicVal = r
					}
				}()
				j.Run()
			}()

			timer := time.NewTimer(timeout)
			defer timer.Stop()

			select {
			case <-done:
				// Job completed within timeout - propagate any panic
				if panicVal != nil {
					panic(panicVal)
				}
			case <-timer.C:
				logger.Error(fmt.Errorf("job exceeded timeout of %v; goroutine still running in background", timeout), "timeout", "duration", timeout)
			}
		})
	}
}

// TimeoutWithContext wraps a job with a timeout that supports true cancellation.
// Unlike Timeout, this wrapper passes a context with deadline to jobs that implement
// JobWithContext, allowing them to check for cancellation and clean up gracefully.
//
// When the timeout expires:
//   - Jobs implementing JobWithContext receive a canceled context and can stop gracefully
//   - Jobs implementing only Job continue running (same as Timeout wrapper)
//
// A timeout of zero or negative disables the timeout and returns the job unchanged.
//
// Example:
//
//	c := cron.New(cron.WithChain(
//	    cron.TimeoutWithContext(cron.DefaultLogger, 5*time.Minute),
//	))
//
//	c.AddJob("@every 1h", cron.FuncJobWithContext(func(ctx context.Context) {
//	    // This job will receive the timeout context
//	    select {
//	    case <-ctx.Done():
//	        // Timeout or shutdown - clean up and return
//	        return
//	    case <-doWork():
//	        // Work completed
//	    }
//	}))
func TimeoutWithContext(logger Logger, timeout time.Duration) JobWrapper {
	return func(j Job) Job {
		if timeout <= 0 {
			return j
		}
		return &timeoutContextJob{
			inner:   j,
			timeout: timeout,
			logger:  logger,
		}
	}
}

// timeoutContextJob implements JobWithContext for the TimeoutWithContext wrapper.
type timeoutContextJob struct {
	inner   Job
	timeout time.Duration
	logger  Logger
}

// Run implements Job interface by calling RunWithContext with context.Background().
func (t *timeoutContextJob) Run() {
	t.RunWithContext(context.Background())
}

// RunWithContext implements JobWithContext interface with true timeout cancellation.
func (t *timeoutContextJob) RunWithContext(ctx context.Context) {
	// Create timeout context derived from the incoming context
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	done := make(chan struct{})
	var panicVal interface{}

	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				panicVal = r
			}
		}()
		// Pass context to inner job if it supports it
		if jc, ok := t.inner.(JobWithContext); ok {
			jc.RunWithContext(ctx)
		} else {
			t.inner.Run()
		}
	}()

	select {
	case <-done:
		// Job completed - propagate any panic
		if panicVal != nil {
			panic(panicVal)
		}
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			t.logger.Error(fmt.Errorf("job exceeded timeout of %v", t.timeout), "timeout", "duration", t.timeout)
		}
		// Context canceled - job may still be running if it doesn't check context
		// Wait for completion to avoid goroutine leaks if job respects context
		<-done
		if panicVal != nil {
			panic(panicVal)
		}
	}
}
