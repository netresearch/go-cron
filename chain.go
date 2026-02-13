package cron

import (
	"context"
	"fmt"
	"math/rand/v2"
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
// It handles both PanicError (from safeExecute) and direct panic values.
func extractPanicInfo(r any) panicInfo {
	// Handle PanicError from safeExecute (preserves original stack)
	if pws, ok := r.(*PanicError); ok {
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

// runJob executes a job, passing ctx if it implements JobWithContext.
func runJob(ctx context.Context, j Job) {
	if jc, ok := j.(JobWithContext); ok {
		jc.RunWithContext(ctx)
	} else {
		j.Run()
	}
}

// recoverJob implements Job and JobWithContext for the Recover wrapper.
type recoverJob struct {
	inner  Job
	logger Logger
	config recoverOpts
}

// Run implements Job by delegating to RunWithContext with context.Background().
func (r *recoverJob) Run() {
	r.RunWithContext(context.Background())
}

// RunWithContext implements JobWithContext, propagating ctx to the inner job.
func (r *recoverJob) RunWithContext(ctx context.Context) {
	defer func() {
		if rv := recover(); rv != nil {
			info := extractPanicInfo(rv)
			if r.config.logLevel == LogLevelInfo {
				r.logger.Info("panic recovered", "error", info.err, "panic_type", info.panicType, "stack", info.stack)
			} else {
				r.logger.Error(info.err, "panic", "panic_type", info.panicType, "stack", info.stack)
			}
		}
	}()
	runJob(ctx, r.inner)
}

// Recover panics in wrapped jobs and log them with the provided logger.
//
// By default, panics are logged at Error level. Use WithLogLevel to
// change this behavior, for example when combined with retry wrappers.
//
// The returned wrapper implements JobWithContext, propagating the incoming
// context to the inner job if it also implements JobWithContext.
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
		return &recoverJob{inner: j, logger: logger, config: config}
	}
}

// delayJob implements Job and JobWithContext for the DelayIfStillRunning wrapper.
type delayJob struct {
	inner        Job
	logger       Logger
	mu           *sync.Mutex
	logThreshold time.Duration // delay duration after which a log message is emitted
}

// Run implements Job by delegating to RunWithContext with context.Background().
func (d *delayJob) Run() {
	d.RunWithContext(context.Background())
}

// RunWithContext implements JobWithContext, propagating ctx to the inner job.
func (d *delayJob) RunWithContext(ctx context.Context) {
	start := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	if dur := time.Since(start); dur > d.logThreshold {
		d.logger.Info("delay", "duration", dur)
	}
	runJob(ctx, d.inner)
}

// DelayIfStillRunning serializes jobs, delaying subsequent runs until the
// previous one is complete. Jobs running after a delay of more than a minute
// have the delay logged at Info.
//
// The returned wrapper implements JobWithContext, propagating the incoming
// context to the inner job if it also implements JobWithContext.
func DelayIfStillRunning(logger Logger) JobWrapper {
	return func(j Job) Job {
		return &delayJob{inner: j, logger: logger, mu: &sync.Mutex{}, logThreshold: time.Minute}
	}
}

// skipJob implements Job and JobWithContext for the SkipIfStillRunning wrapper.
type skipJob struct {
	inner  Job
	logger Logger
	ch     chan struct{}
}

// Run implements Job by delegating to RunWithContext with context.Background().
func (s *skipJob) Run() {
	s.RunWithContext(context.Background())
}

// RunWithContext implements JobWithContext, propagating ctx to the inner job.
func (s *skipJob) RunWithContext(ctx context.Context) {
	select {
	case v := <-s.ch:
		defer func() { s.ch <- v }()
		runJob(ctx, s.inner)
	default:
		s.logger.Info("skip")
	}
}

// SkipIfStillRunning skips an invocation of the Job if a previous invocation is
// still running. It logs skips to the given logger at Info level.
//
// The returned wrapper implements JobWithContext, propagating the incoming
// context to the inner job if it also implements JobWithContext.
func SkipIfStillRunning(logger Logger) JobWrapper {
	return func(j Job) Job {
		ch := make(chan struct{}, 1)
		ch <- struct{}{}
		return &skipJob{inner: j, logger: logger, ch: ch}
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
		return &timeoutJob{
			inner:     j,
			timeout:   timeout,
			logger:    logger,
			onTimeout: config.onTimeout,
		}
	}
}

// timeoutJob implements Job and JobWithContext for the Timeout wrapper.
type timeoutJob struct {
	inner     Job
	timeout   time.Duration
	logger    Logger
	onTimeout func(time.Duration)
}

// Run implements Job by delegating to RunWithContext with context.Background().
func (t *timeoutJob) Run() {
	t.RunWithContext(context.Background())
}

// RunWithContext implements JobWithContext, propagating ctx to the inner job.
func (t *timeoutJob) RunWithContext(ctx context.Context) {
	done := make(chan struct{})
	var panicVal any
	go func() {
		defer close(done)
		panicVal = safeExecute(func() { runJob(ctx, t.inner) })
	}()

	timer := time.NewTimer(t.timeout)
	defer timer.Stop()

	select {
	case <-done:
		// Job completed within timeout - propagate any panic
		if panicVal != nil {
			panic(panicVal)
		}
	case <-timer.C:
		t.logger.Error(fmt.Errorf("job exceeded timeout of %v; goroutine still running in background", t.timeout), "timeout", "duration", t.timeout)
		// Invoke callback for metrics/alerting
		if t.onTimeout != nil {
			t.onTimeout(t.timeout)
		}
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
		panicVal = safeExecute(func() { runJob(ctx, t.inner) })
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

// jitterJob implements Job and JobWithContext for the Jitter wrapper.
type jitterJob struct {
	inner     Job
	maxJitter time.Duration
}

// Run implements Job by delegating to RunWithContext with context.Background().
func (j *jitterJob) Run() {
	j.RunWithContext(context.Background())
}

// RunWithContext implements JobWithContext, propagating ctx to the inner job.
func (j *jitterJob) RunWithContext(ctx context.Context) {
	if j.maxJitter > 0 {
		// #nosec G404 -- math/rand is appropriate for jitter; cryptographic randomness not needed
		jitter := time.Duration(rand.Int64N(int64(j.maxJitter)))
		time.Sleep(jitter)
	}
	runJob(ctx, j.inner)
}

// Jitter adds a random delay before job execution to prevent thundering herd.
// When many jobs are scheduled at the same time (e.g., @hourly), they would
// all execute simultaneously, causing database connection spikes, API rate
// limiting, and resource contention. Jitter spreads out the execution times.
//
// The delay is uniformly distributed in the range [0, maxJitter).
// A maxJitter of 0 or negative disables jitter (no delay).
//
// The returned wrapper implements JobWithContext, propagating the incoming
// context to the inner job if it also implements JobWithContext.
//
// Example:
//
//	// Add 0-30s random delay before each execution
//	cron.NewChain(cron.Jitter(30 * time.Second)).Then(myJob)
//
//	// Compose with other wrappers
//	cron.NewChain(
//	    cron.Recover(logger),
//	    cron.Jitter(30 * time.Second),
//	    cron.SkipIfStillRunning(logger),
//	).Then(myJob)
//
//	// Use via WithChain option
//	c.AddFunc("@hourly", syncData, cron.WithChain(cron.Jitter(30*time.Second)))
func Jitter(maxJitter time.Duration) JobWrapper {
	return func(j Job) Job {
		return &jitterJob{inner: j, maxJitter: maxJitter}
	}
}

// jitterLogJob implements Job and JobWithContext for the JitterWithLogger wrapper.
type jitterLogJob struct {
	inner     Job
	maxJitter time.Duration
	logger    Logger
}

// Run implements Job by delegating to RunWithContext with context.Background().
func (j *jitterLogJob) Run() {
	j.RunWithContext(context.Background())
}

// RunWithContext implements JobWithContext, propagating ctx to the inner job.
func (j *jitterLogJob) RunWithContext(ctx context.Context) {
	if j.maxJitter > 0 {
		// #nosec G404 -- math/rand is appropriate for jitter; cryptographic randomness not needed
		jitter := time.Duration(rand.Int64N(int64(j.maxJitter)))
		j.logger.Info("jitter", "delay", jitter, "max", j.maxJitter)
		time.Sleep(jitter)
	}
	runJob(ctx, j.inner)
}

// JitterWithLogger is like Jitter but logs the applied delay.
// This is useful for debugging and observability to verify jitter is working.
//
// The returned wrapper implements JobWithContext, propagating the incoming
// context to the inner job if it also implements JobWithContext.
//
// Example:
//
//	cron.NewChain(cron.JitterWithLogger(logger, 30 * time.Second)).Then(myJob)
func JitterWithLogger(logger Logger, maxJitter time.Duration) JobWrapper {
	return func(j Job) Job {
		return &jitterLogJob{inner: j, maxJitter: maxJitter, logger: logger}
	}
}

// maxConcurrentJob implements Job and JobWithContext for the MaxConcurrent wrapper.
type maxConcurrentJob struct {
	inner Job
	sem   chan struct{}
}

// Run implements Job by delegating to RunWithContext with context.Background().
func (m *maxConcurrentJob) Run() {
	m.RunWithContext(context.Background())
}

// RunWithContext implements JobWithContext, propagating ctx to the inner job.
func (m *maxConcurrentJob) RunWithContext(ctx context.Context) {
	m.sem <- struct{}{} // acquire slot (blocks if full)
	defer func() { <-m.sem }()
	runJob(ctx, m.inner)
}

// MaxConcurrent limits the total number of jobs that can run concurrently
// across all entries wrapped by this chain. When all slots are occupied,
// new job executions wait until a slot becomes available.
//
// This is useful when many jobs are scheduled at the same time (e.g., many
// @hourly jobs at minute 0) to prevent goroutine explosion and resource
// exhaustion (database connections, API rate limits, CPU).
//
// Unlike [SkipIfStillRunning] (which limits per-job), MaxConcurrent limits
// across all jobs sharing the same wrapper instance.
//
// A limit of 0 or negative panics — use no wrapper if you want no limit.
//
// The returned wrapper implements [JobWithContext], propagating the incoming
// context to the inner job if it also implements JobWithContext.
//
// Example:
//
//	// Limit all jobs to 10 concurrent executions
//	c := cron.New(cron.WithChain(
//	    cron.Recover(logger),
//	    cron.MaxConcurrent(10),
//	))
//
//	// Compose with jitter to prevent thundering herd AND limit concurrency
//	c := cron.New(cron.WithChain(
//	    cron.Recover(logger),
//	    cron.Jitter(30 * time.Second),
//	    cron.MaxConcurrent(10),
//	))
func MaxConcurrent(n int) JobWrapper {
	if n <= 0 {
		panic("cron: MaxConcurrent requires n > 0")
	}
	sem := make(chan struct{}, n)
	return func(j Job) Job {
		return &maxConcurrentJob{inner: j, sem: sem}
	}
}

// maxConcurrentSkipJob implements Job and JobWithContext for MaxConcurrentSkip.
type maxConcurrentSkipJob struct {
	inner  Job
	sem    chan struct{}
	logger Logger
}

// Run implements Job by delegating to RunWithContext with context.Background().
func (m *maxConcurrentSkipJob) Run() {
	m.RunWithContext(context.Background())
}

// RunWithContext implements JobWithContext, propagating ctx to the inner job.
func (m *maxConcurrentSkipJob) RunWithContext(ctx context.Context) {
	select {
	case m.sem <- struct{}{}: // try to acquire slot
		defer func() { <-m.sem }()
		runJob(ctx, m.inner)
	default:
		m.logger.Info("skip", "reason", "max concurrent reached")
	}
}

// MaxConcurrentSkip is like [MaxConcurrent] but skips execution instead of
// waiting when the concurrency limit is reached. This is useful when you
// prefer to drop executions rather than queue them up.
//
// Unlike [SkipIfStillRunning] (which limits per-job), MaxConcurrentSkip limits
// across all jobs sharing the same wrapper instance.
//
// The returned wrapper implements [JobWithContext], propagating the incoming
// context to the inner job if it also implements JobWithContext.
//
// Example:
//
//	c := cron.New(cron.WithChain(
//	    cron.Recover(logger),
//	    cron.MaxConcurrentSkip(logger, 5),
//	))
func MaxConcurrentSkip(logger Logger, n int) JobWrapper {
	if n <= 0 {
		panic("cron: MaxConcurrentSkip requires n > 0")
	}
	sem := make(chan struct{}, n)
	return func(j Job) Job {
		return &maxConcurrentSkipJob{inner: j, sem: sem, logger: logger}
	}
}
