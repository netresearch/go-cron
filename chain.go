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

// LogLevel defines the severity level for logging recovered panics.
type LogLevel int

const (
	// LogLevelError logs panics at Error level (default).
	LogLevelError LogLevel = iota
	// LogLevelInfo logs panics at Info level.
	// Useful when combined with retry wrappers to reduce log noise
	// for expected transient failures.
	LogLevelInfo
)

// recoverOpts holds configuration for the Recover wrapper.
type recoverOpts struct {
	logLevel LogLevel
}

// RecoverOption configures the Recover wrapper.
type RecoverOption func(*recoverOpts)

// WithLogLevel sets the log level for recovered panics.
// Default is LogLevelError. Use LogLevelInfo to reduce noise when
// combined with retry wrappers like RetryWithBackoff.
//
// Example:
//
//	cron.Recover(logger, cron.WithLogLevel(cron.LogLevelInfo))
func WithLogLevel(level LogLevel) RecoverOption {
	return func(o *recoverOpts) {
		o.logLevel = level
	}
}

// panicInfo holds extracted information from a recovered panic value.
type panicInfo struct {
	err       error
	stack     string
	panicType string
}

// extractPanicInfo extracts error, stack trace, and type information from a panic value.
// It handles both PanicWithStack (from safeExecute) and direct panic values.
func extractPanicInfo(r any) panicInfo {
	// Handle PanicWithStack from safeExecute (preserves original stack)
	if pws, ok := r.(*PanicWithStack); ok {
		err := toError(pws.Value)
		return panicInfo{
			err:       err,
			stack:     "...\n" + string(pws.Stack),
			panicType: fmt.Sprintf("%T", pws.Value),
		}
	}

	// Direct panic - capture current stack
	const size = 64 << 10
	buf := make([]byte, size)
	buf = buf[:runtime.Stack(buf, false)]
	return panicInfo{
		err:       toError(r),
		stack:     "...\n" + string(buf),
		panicType: fmt.Sprintf("%T", r),
	}
}

// toError converts a panic value to an error.
func toError(v any) error {
	if err, ok := v.(error); ok {
		return err
	}
	return fmt.Errorf("%v", v)
}

// Recover panics in wrapped jobs and log them with the provided logger.
//
// By default, panics are logged at Error level. Use WithLogLevel to
// change this behavior, for example when combined with retry wrappers.
//
// Example:
//
//	// Default behavior - logs at Error level
//	cron.NewChain(cron.Recover(logger)).Then(job)
//
//	// Log at Info level (useful with retries)
//	cron.NewChain(cron.Recover(logger, cron.WithLogLevel(cron.LogLevelInfo))).Then(job)
func Recover(logger Logger, opts ...RecoverOption) JobWrapper {
	// Default configuration
	config := recoverOpts{
		logLevel: LogLevelError,
	}

	// Apply options
	for _, opt := range opts {
		opt(&config)
	}

	return func(j Job) Job {
		return FuncJob(func() {
			defer func() {
				if r := recover(); r != nil {
					info := extractPanicInfo(r)
					if config.logLevel == LogLevelInfo {
						logger.Info("panic recovered", "error", info.err, "panic_type", info.panicType, "stack", info.stack)
					} else {
						logger.Error(info.err, "panic", "panic_type", info.panicType, "stack", info.stack)
					}
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

// timeoutConfig holds configuration for timeout wrappers.
type timeoutConfig struct {
	onTimeout func(timeout time.Duration) // Called when job times out (abandoned)
}

// TimeoutOption configures Timeout and TimeoutWithContext wrappers.
type TimeoutOption func(*timeoutConfig)

// WithTimeoutCallback sets a callback invoked when a job times out and is abandoned.
// This is useful for metrics collection and alerting on goroutine accumulation.
//
// Example with Prometheus:
//
//	abandonedGoroutines := prometheus.NewCounter(prometheus.CounterOpts{
//	    Name: "cron_abandoned_goroutines_total",
//	    Help: "Number of job goroutines abandoned due to timeout",
//	})
//
//	c := cron.New(cron.WithChain(
//	    cron.Timeout(logger, 5*time.Minute,
//	        cron.WithTimeoutCallback(func(timeout time.Duration) {
//	            abandonedGoroutines.Inc()
//	        }),
//	    ),
//	))
func WithTimeoutCallback(fn func(timeout time.Duration)) TimeoutOption {
	return func(c *timeoutConfig) {
		c.onTimeout = fn
	}
}

// Timeout wraps a job with a timeout. If the job takes longer than the given
// duration, the wrapper returns and logs an error, but the underlying job
// goroutine continues running until completion.
//
// # ⚠️ IMPORTANT: Abandonment Model
//
// This wrapper implements an "abandonment model" - when a timeout occurs,
// the wrapper returns but the job's goroutine is NOT canceled. The job will
// continue executing in the background until it naturally completes. This means:
//   - Resources held by the job will not be released until completion
//   - Side effects will still occur even after timeout
//   - Multiple abandoned goroutines can accumulate if jobs consistently timeout
//
// # Goroutine Accumulation Risk
//
// If a job consistently takes longer than its schedule interval, abandoned
// goroutines will accumulate:
//
//	// DANGER: This pattern causes goroutine accumulation!
//	c.AddFunc("@every 1s", func() {
//	    time.Sleep(5 * time.Second) // Takes 5x longer than schedule
//	})
//	// With Timeout(2s), a new abandoned goroutine is created every second
//
// # Tracking Abandoned Goroutines
//
// Use [WithTimeoutCallback] to track timeout events for metrics and alerting:
//
//	cron.Timeout(logger, 5*time.Minute,
//	    cron.WithTimeoutCallback(func(timeout time.Duration) {
//	        abandonedGoroutines.Inc() // Prometheus counter
//	    }),
//	)
//
// # Recommended Alternatives
//
// For jobs that need true cancellation support, use [TimeoutWithContext] with
// jobs that implement [JobWithContext]:
//
//	c := cron.New(cron.WithChain(
//	    cron.TimeoutWithContext(logger, 5*time.Minute),
//	))
//	c.AddJob("@every 1h", cron.FuncJobWithContext(func(ctx context.Context) {
//	    select {
//	    case <-ctx.Done():
//	        return // Timeout - clean up and exit
//	    case <-doWork():
//	        // Work completed
//	    }
//	}))
//
// To prevent accumulation without context support, combine with [SkipIfStillRunning]:
//
//	c := cron.New(cron.WithChain(
//	    cron.Recover(logger),
//	    cron.Timeout(logger, 5*time.Minute),
//	    cron.SkipIfStillRunning(logger), // Prevents overlapping executions
//	))
//
// A timeout of zero or negative disables the timeout and returns the job unchanged.
func Timeout(logger Logger, timeout time.Duration, opts ...TimeoutOption) JobWrapper {
	// Apply options
	var config timeoutConfig
	for _, opt := range opts {
		opt(&config)
	}

	return func(j Job) Job {
		if timeout <= 0 {
			return j
		}
		return FuncJob(func() {
			done := make(chan struct{})
			var panicVal any
			go func() {
				defer close(done)
				panicVal = safeExecute(j.Run)
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
				// Invoke callback for metrics/alerting
				if config.onTimeout != nil {
					config.onTimeout(timeout)
				}
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
// Use [WithTimeoutCallback] to track timeout/abandonment events:
//
//	cron.TimeoutWithContext(logger, 5*time.Minute,
//	    cron.WithTimeoutCallback(func(timeout time.Duration) {
//	        timeoutCounter.Inc()
//	    }),
//	)
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
func TimeoutWithContext(logger Logger, timeout time.Duration, opts ...TimeoutOption) JobWrapper {
	// Apply options
	var config timeoutConfig
	for _, opt := range opts {
		opt(&config)
	}

	return func(j Job) Job {
		if timeout <= 0 {
			return j
		}
		return &timeoutContextJob{
			inner:     j,
			timeout:   timeout,
			logger:    logger,
			onTimeout: config.onTimeout,
		}
	}
}

// cleanupTimeout is the grace period for jobs to finish after context cancellation.
// This prevents TimeoutWithContext from blocking indefinitely if a job ignores
// the context. After this timeout, the wrapper returns and the job goroutine
// is abandoned (same behavior as the non-context Timeout wrapper).
const cleanupTimeout = 5 * time.Second

// timeoutContextJob implements JobWithContext for the TimeoutWithContext wrapper.
type timeoutContextJob struct {
	inner     Job
	timeout   time.Duration
	logger    Logger
	onTimeout func(time.Duration) // Optional callback for timeout events
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
	var panicVal any

	go func() {
		defer close(done)
		// Pass context to inner job if it supports it, using consolidated helper
		if jc, ok := t.inner.(JobWithContext); ok {
			panicVal = safeExecute(func() { jc.RunWithContext(ctx) })
		} else {
			panicVal = safeExecute(t.inner.Run)
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
			// Invoke callback for metrics/alerting (timeout occurred)
			if t.onTimeout != nil {
				t.onTimeout(t.timeout)
			}
		}
		// Context canceled - give job a grace period to clean up.
		// If it doesn't finish, abandon it (same as Timeout wrapper behavior).
		cleanupTimer := time.NewTimer(cleanupTimeout)
		defer cleanupTimer.Stop()

		select {
		case <-done:
			// Job finished within grace period
			if panicVal != nil {
				panic(panicVal)
			}
		case <-cleanupTimer.C:
			// Job didn't finish - abandon it to prevent indefinite blocking
			t.logger.Info("job abandoned", "timeout", t.timeout, "cleanup_timeout", cleanupTimeout)
		}
	}
}
