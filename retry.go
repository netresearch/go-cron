package cron

import (
	"fmt"
	"math"
	"math/rand/v2"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// RetryAttempt contains metadata about a single retry attempt.
// This is passed to the callback configured via WithRetryCallback.
type RetryAttempt struct {
	Attempt   int           // 1-based attempt number (1 = first execution)
	Delay     time.Duration // Delay before this attempt (0 for first attempt)
	Err       any           // Panic value (RetryWithBackoff) or error (RetryOnError)
	WillRetry bool          // True if another attempt will follow
}

// RetryOption configures optional behavior for RetryWithBackoff and RetryOnError.
type RetryOption func(*retryConfig)

type retryConfig struct {
	callback func(RetryAttempt)
}

func applyRetryOptions(opts []RetryOption) retryConfig {
	var cfg retryConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// WithRetryCallback sets a callback invoked after each attempt (including
// the initial execution). This enables external monitoring and metrics
// collection for retry behavior.
//
// Example with Prometheus:
//
//	RetryWithBackoff(logger, 3, time.Second, time.Minute, 2.0,
//	    cron.WithRetryCallback(func(a cron.RetryAttempt) {
//	        retryCounter.WithLabelValues(fmt.Sprint(a.Attempt)).Inc()
//	        if !a.WillRetry && a.Err != nil {
//	            retryExhausted.Inc()
//	        }
//	    }),
//	)
func WithRetryCallback(fn func(RetryAttempt)) RetryOption {
	return func(c *retryConfig) {
		c.callback = fn
	}
}

// CircuitBreakerState represents the current state of a circuit breaker.
type CircuitBreakerState int

const (
	// CircuitClosed means the circuit is operating normally.
	// Failures increment the counter; threshold failures will open the circuit.
	CircuitClosed CircuitBreakerState = iota

	// CircuitOpen means the circuit has tripped. Executions are skipped
	// until the cooldown period expires.
	CircuitOpen

	// CircuitHalfOpen means the cooldown has expired and one probe execution
	// is allowed. Success closes the circuit; failure reopens it.
	CircuitHalfOpen
)

// String returns the human-readable name of the circuit breaker state.
func (s CircuitBreakerState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// CircuitBreakerEvent represents a state transition in the circuit breaker.
// This is passed to the callback configured via WithStateChangeCallback.
type CircuitBreakerEvent struct {
	OldState CircuitBreakerState // State before the transition
	NewState CircuitBreakerState // State after the transition
	Failures int64               // Current consecutive failure count
	Err      any                 // Panic value that caused the transition (nil on success)
}

// CircuitBreakerOption configures optional behavior for CircuitBreaker.
type CircuitBreakerOption func(*circuitBreakerConfig)

type circuitBreakerConfig struct {
	callback func(CircuitBreakerEvent)
}

func applyCircuitBreakerOptions(opts []CircuitBreakerOption) circuitBreakerConfig {
	var cfg circuitBreakerConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// WithStateChangeCallback sets a callback invoked on circuit breaker state
// transitions. This enables external monitoring and metrics collection.
//
// State transitions that trigger the callback:
//   - Closed → Open (threshold failures reached)
//   - Open → HalfOpen (cooldown expired, probe attempted)
//   - HalfOpen → Closed (probe succeeded)
//   - HalfOpen → Open (probe failed)
//
// The callback is invoked synchronously within the job execution goroutine.
// Keep callbacks fast to avoid delaying job execution.
//
// Example with Prometheus:
//
//	CircuitBreaker(logger, 5, 5*time.Minute,
//	    cron.WithStateChangeCallback(func(e cron.CircuitBreakerEvent) {
//	        circuitState.WithLabelValues(e.NewState.String()).Set(1)
//	        if e.NewState == cron.CircuitOpen {
//	            circuitTrips.Inc()
//	        }
//	    }),
//	)
func WithStateChangeCallback(fn func(CircuitBreakerEvent)) CircuitBreakerOption {
	return func(c *circuitBreakerConfig) {
		c.callback = fn
	}
}

// CircuitBreakerHandle provides read-only access to the internal state of a
// circuit breaker. Obtain one via CircuitBreakerWithHandle.
//
// All methods are safe for concurrent use.
type CircuitBreakerHandle struct {
	state     *circuitState
	threshold int
	cooldown  time.Duration
}

// State returns the current circuit breaker state.
func (h *CircuitBreakerHandle) State() CircuitBreakerState {
	if open, _ := h.state.isOpen(h.threshold, h.cooldown); open {
		return CircuitOpen
	}
	if h.state.isHalfOpen(h.threshold) {
		return CircuitHalfOpen
	}
	return CircuitClosed
}

// Failures returns the current consecutive failure count.
func (h *CircuitBreakerHandle) Failures() int64 {
	return atomic.LoadInt64(&h.state.failures)
}

// LastFailure returns the time of the last recorded failure.
// Returns the zero time if no failures have been recorded.
func (h *CircuitBreakerHandle) LastFailure() time.Time {
	nano := atomic.LoadInt64(&h.state.lastFailNano)
	if nano == 0 {
		return time.Time{}
	}
	return time.Unix(0, nano)
}

// CooldownEnds returns when the current cooldown period expires.
// Returns the zero time if the circuit is not open.
func (h *CircuitBreakerHandle) CooldownEnds() time.Time {
	nano := atomic.LoadInt64(&h.state.lastFailNano)
	failures := atomic.LoadInt64(&h.state.failures)
	if nano == 0 || failures < int64(h.threshold) {
		return time.Time{}
	}
	end := time.Unix(0, nano).Add(h.cooldown)
	if time.Now().After(end) {
		return time.Time{} // cooldown already expired
	}
	return end
}

// RetryWithBackoff wraps a job to retry on panic with exponential backoff.
// A job "fails" if it panics. The wrapper catches panics and retries.
//
// This wrapper is useful when jobs call external services that may have
// transient failures. Instead of waiting for the next scheduled run,
// the job is retried immediately with increasing delays.
//
// Parameters:
//   - logger: For logging retry attempts
//   - maxRetries: Maximum retry attempts:
//   - 0 = no retries (execute once, fail immediately on panic) - SAFE DEFAULT
//   - >0 = retry up to N times (N+1 total attempts)
//   - -1 = unlimited retries (use with caution - can cause resource exhaustion)
//   - initialDelay: First retry delay
//   - maxDelay: Maximum delay cap (prevents exponential explosion)
//   - multiplier: Delay multiplier per retry (typically 2.0)
//
// Example usage:
//
//	c := cron.New(
//	    cron.WithChain(
//	        cron.Recover(logger),   // Outermost: catches final re-panics
//	        cron.RetryWithBackoff(logger, 3, time.Second, time.Minute, 2.0),
//	    ),
//	)
//	c.AddFunc("@every 5m", func() {
//	    if err := callAPI(); err != nil {
//	        panic(err) // Will be retried up to 3 times
//	    }
//	})
//
// Retry behavior for maxRetries=3, initialDelay=1s, multiplier=2.0:
//
//	| Attempt | Delay | Action            |
//	|---------|-------|-------------------|
//	| 1       | 0     | Execute           |
//	| 2       | 1s    | Retry after delay |
//	| 3       | 2s    | Retry after delay |
//	| 4       | 4s    | Final retry       |
//	| -       | -     | Re-panic (fail)   |

// jitterFraction is the maximum percentage of delay to add/subtract as jitter.
// 0.1 means ±10% jitter, which helps prevent thundering herd when multiple
// jobs retry simultaneously.
const jitterFraction = 0.1

// calculateBackoffDelay returns the delay for a given retry attempt using exponential backoff
// with jitter. The base delay grows as: initialDelay * (multiplier ^ (attempt-2)), capped at
// maxDelay. Jitter of ±10% is applied to prevent thundering herd when multiple jobs retry
// simultaneously. Attempt 1 has no delay (first execution), attempt 2 uses initialDelay,
// and subsequent attempts grow exponentially.
func calculateBackoffDelay(attempt int, initialDelay, maxDelay time.Duration, multiplier float64) time.Duration {
	delay := min(time.Duration(float64(initialDelay)*math.Pow(multiplier, float64(attempt-2))), maxDelay)
	// Apply jitter: ±10% randomization to prevent thundering herd
	// #nosec G404 -- math/rand is appropriate for jitter; cryptographic randomness not needed
	jitter := time.Duration(float64(delay) * jitterFraction * (2*rand.Float64() - 1))
	return delay + jitter
}

// PanicError wraps a panic value with the stack trace at the point of panic.
// This allows re-panicking to preserve the original stack trace for debugging.
type PanicError struct {
	Value any    // The original panic value
	Stack []byte // Stack trace at point of panic
}

// PanicWithStack is a type alias for backward compatibility.
//
// Deprecated: Use PanicError instead. This alias will be removed in a future release.
//
//nolint:errname // This is intentionally a type alias for backward compat
type PanicWithStack = PanicError

// Error implements the error interface for PanicError.
func (p *PanicError) Error() string {
	return fmt.Sprintf("panic: %v", p.Value)
}

// String returns a detailed representation including the stack trace.
func (p *PanicError) String() string {
	return fmt.Sprintf("panic: %v\nstack:\n%s", p.Value, p.Stack)
}

// Unwrap returns the original panic value if it was an error.
func (p *PanicError) Unwrap() error {
	if err, ok := p.Value.(error); ok {
		return err
	}
	return nil
}

// safeExecute runs the given function and captures any panic value with stack trace.
// Returns nil if the function completes successfully, or a *PanicError otherwise.
// This is a generic helper for panic-safe execution used throughout the library.
//
// The returned *PanicError preserves the original stack trace, which is critical
// for debugging when panics are re-thrown by wrappers like RetryWithBackoff.
//
// Example usage:
//
//	if panicVal := safeExecute(func() { riskyOperation() }); panicVal != nil {
//	    log.Printf("operation panicked: %v", panicVal)
//	}
func safeExecute(fn func()) (panicValue any) {
	defer func() {
		if r := recover(); r != nil {
			const size = 64 << 10 // 64KB buffer for stack trace
			buf := make([]byte, size)
			buf = buf[:runtime.Stack(buf, false)]
			panicValue = &PanicError{Value: r, Stack: buf}
		}
	}()
	fn()
	return nil
}

// runWithRecovery executes the given job within a panic-recovering context.
// It returns the recovered panic value if the job panics, or nil if the job
// completes successfully. This allows the caller to handle panics as return
// values rather than unwinding the stack.
func runWithRecovery(j Job) any {
	return safeExecute(j.Run)
}

// extractPanicValueAndStack extracts the original panic value and stack from a PanicError.
// If the value is not a PanicError, returns the value directly with empty stack.
func extractPanicValueAndStack(p any) (value any, stack string) {
	if pws, ok := p.(*PanicError); ok {
		return pws.Value, string(pws.Stack)
	}
	return p, ""
}

// logErrorWithOptionalStack logs an error with optional stack trace.
func logErrorWithOptionalStack(logger Logger, err error, msg, stack string, keysAndValues ...any) {
	if stack != "" {
		keysAndValues = append(keysAndValues, "stack", stack)
	}
	logger.Error(err, msg, keysAndValues...)
}

// computeMaxAttempts converts maxRetries to maxAttempts.
// maxRetries: 0 = 1 attempt, >0 = N+1 attempts, -1 = unlimited (returns 0).
func computeMaxAttempts(maxRetries int) int {
	if maxRetries < 0 {
		return 0 // unlimited
	}
	return maxRetries + 1
}

func RetryWithBackoff(logger Logger, maxRetries int, initialDelay, maxDelay time.Duration, multiplier float64, opts ...RetryOption) JobWrapper {
	cfg := applyRetryOptions(opts)
	return func(j Job) Job {
		return FuncJob(func() {
			maxAttempts := computeMaxAttempts(maxRetries)
			var lastPanic any

			for attempt := 1; maxAttempts == 0 || attempt <= maxAttempts; attempt++ {
				var delay time.Duration
				if attempt > 1 {
					delay = calculateBackoffDelay(attempt, initialDelay, maxDelay, multiplier)
				}
				lastPanic = executeRetryAttempt(j, logger, attempt, lastPanic, initialDelay, maxDelay, multiplier)
				willRetry := lastPanic != nil && (maxAttempts == 0 || attempt < maxAttempts)
				if cfg.callback != nil {
					cfg.callback(RetryAttempt{
						Attempt:   attempt,
						Delay:     delay,
						Err:       lastPanic,
						WillRetry: willRetry,
					})
				}
				if lastPanic == nil {
					logRetrySuccess(logger, attempt)
					return
				}
			}

			logRetryExhausted(logger, lastPanic, maxAttempts)
			panic(lastPanic)
		})
	}
}

// executeRetryAttempt runs a single retry attempt with optional delay.
func executeRetryAttempt(j Job, logger Logger, attempt int, lastPanic any, initialDelay, maxDelay time.Duration, multiplier float64) any {
	if attempt > 1 {
		delay := calculateBackoffDelay(attempt, initialDelay, maxDelay, multiplier)
		panicVal, _ := extractPanicValueAndStack(lastPanic)
		logger.Info("retry", "attempt", attempt, "delay", delay, "last_panic", panicVal)
		time.Sleep(delay)
	}
	return runWithRecovery(j)
}

// logRetrySuccess logs success if this was a retry attempt.
func logRetrySuccess(logger Logger, attempt int) {
	if attempt > 1 {
		logger.Info("retry succeeded", "attempt", attempt)
	}
}

// logRetryExhausted logs when all retry attempts are exhausted.
func logRetryExhausted(logger Logger, lastPanic any, maxAttempts int) {
	panicVal, stack := extractPanicValueAndStack(lastPanic)
	err := toError(panicVal)
	logErrorWithOptionalStack(logger, err, "retry exhausted", stack, "attempts", maxAttempts)
}

// RetryOnError wraps an ErrorJob to retry on returned errors with exponential backoff.
// Unlike RetryWithBackoff which catches panics, this wrapper uses Go-idiomatic error
// returns for retry decisions. Jobs must implement the ErrorJob interface to benefit
// from this wrapper; regular Jobs that only implement Run() are passed through unchanged.
//
// Parameters:
//   - logger: For logging retry attempts
//   - maxRetries: Maximum retry attempts:
//   - 0 = no retries (execute once, log error if it fails) - SAFE DEFAULT
//   - >0 = retry up to N times (N+1 total attempts)
//   - -1 = unlimited retries (use with caution - can cause resource exhaustion)
//   - initialDelay: First retry delay
//   - maxDelay: Maximum delay cap (prevents exponential explosion)
//   - multiplier: Delay multiplier per retry (typically 2.0)
//
// When all retries are exhausted, the final error is logged and then panicked, propagating
// the failure through the middleware chain (e.g., CircuitBreaker, Recover). This is
// consistent with RetryWithBackoff and ensures downstream wrappers see the failure.
//
// Example usage:
//
//	c := cron.New(
//	    cron.WithChain(
//	        cron.Recover(logger),   // Outermost: catches panics from non-ErrorJob jobs
//	        cron.RetryOnError(logger, 3, time.Second, time.Minute, 2.0),
//	    ),
//	)
//	c.AddJob("@every 5m", cron.FuncErrorJob(func() error {
//	    return callAPI() // Returned errors trigger retry
//	}))
//
// Retry behavior for maxRetries=3, initialDelay=1s, multiplier=2.0:
//
//	| Attempt | Delay | Action             |
//	|---------|-------|--------------------|
//	| 1       | 0     | Execute            |
//	| 2       | 1s    | Retry after delay  |
//	| 3       | 2s    | Retry after delay  |
//	| 4       | 4s    | Final retry        |
//	| -       | -     | Log + panic (done) |
func RetryOnError(logger Logger, maxRetries int, initialDelay, maxDelay time.Duration, multiplier float64, opts ...RetryOption) JobWrapper {
	cfg := applyRetryOptions(opts)
	return func(j Job) Job {
		ej, ok := j.(ErrorJob)
		if !ok {
			// Job doesn't implement ErrorJob, pass through unchanged
			return j
		}

		return FuncJob(func() {
			maxAttempts := computeMaxAttempts(maxRetries)
			var lastErr error

			for attempt := 1; maxAttempts == 0 || attempt <= maxAttempts; attempt++ {
				var delay time.Duration
				if attempt > 1 {
					delay = calculateBackoffDelay(attempt, initialDelay, maxDelay, multiplier)
					logger.Info("retry", "attempt", attempt, "delay", delay, "last_error", lastErr)
					time.Sleep(delay)
				}

				lastErr = ej.RunE()
				willRetry := lastErr != nil && (maxAttempts == 0 || attempt < maxAttempts)
				if cfg.callback != nil {
					cfg.callback(RetryAttempt{
						Attempt:   attempt,
						Delay:     delay,
						Err:       lastErr,
						WillRetry: willRetry,
					})
				}
				if lastErr == nil {
					if attempt > 1 {
						logger.Info("retry succeeded", "attempt", attempt)
					}
					return
				}
			}

			logger.Error(lastErr, "retry exhausted", "attempts", maxAttempts)
			panic(lastErr)
		})
	}
}

// CircuitBreaker wraps a job to stop execution after consecutive failures.
// This prevents a failing job from continuously hammering an external service.
//
// The circuit breaker has three states:
//   - Closed: Normal execution. Failures increment counter.
//   - Open: Execution is skipped. Entered after threshold failures.
//   - Half-Open: After cooldown, one execution is attempted. Success closes circuit,
//     failure reopens it.
//
// A job "fails" if it panics. Successful execution resets the failure counter.
//
// Parameters:
//   - logger: For logging circuit state changes
//   - threshold: Number of consecutive failures to open circuit
//   - cooldown: Time to wait before attempting recovery (half-open state)
//
// Example usage:
//
//	c := cron.New(
//	    cron.WithChain(
//	        cron.Recover(logger), // Outermost: catches re-panics from circuit breaker
//	        cron.CircuitBreaker(logger, 5, 5*time.Minute),
//	    ),
//	)
//	c.AddFunc("@every 1m", func() {
//	    if err := callAPI(); err != nil {
//	        panic(err) // After 5 failures, circuit opens for 5 minutes
//	    }
//	})
//
// State transitions:
//
//	CLOSED --[threshold failures]--> OPEN --[cooldown expires]--> HALF-OPEN
//	   ^                                                              |
//	   |                                                              |
//	   +------------------[success]-----------------------------------+
//	                                                                  |
//	                      +--[failure]--------------------------------+
//	                      |
//	                      v
//	                    OPEN
//
// circuitState holds the shared state for a circuit breaker.
type circuitState struct {
	failures     int64      // atomic: current consecutive failure count
	lastFailNano int64      // atomic: unix nano of last failure
	mu           sync.Mutex // only for state transitions
}

// isOpen returns true if the circuit is open (in cooldown period).
func (s *circuitState) isOpen(threshold int, cooldown time.Duration) (bool, time.Duration) {
	currentFailures := int(atomic.LoadInt64(&s.failures))
	lastFail := atomic.LoadInt64(&s.lastFailNano)
	timeSinceLastFail := time.Since(time.Unix(0, lastFail))

	if currentFailures >= threshold && timeSinceLastFail < cooldown {
		return true, cooldown - timeSinceLastFail
	}
	return false, 0
}

// isHalfOpen returns true if the circuit is in half-open state (ready to attempt recovery).
func (s *circuitState) isHalfOpen(threshold int) bool {
	return int(atomic.LoadInt64(&s.failures)) >= threshold
}

// recordFailure increments the failure counter and returns the new count.
func (s *circuitState) recordFailure() int64 {
	s.mu.Lock()
	newFailures := atomic.AddInt64(&s.failures, 1)
	atomic.StoreInt64(&s.lastFailNano, time.Now().UnixNano())
	s.mu.Unlock()
	return newFailures
}

// resetOnSuccess resets the circuit if successful, returning true if it was previously open.
func (s *circuitState) resetOnSuccess(threshold int) bool {
	s.mu.Lock()
	wasOpen := atomic.LoadInt64(&s.failures) >= int64(threshold)
	atomic.StoreInt64(&s.failures, 0)
	s.mu.Unlock()
	return wasOpen
}

// logCircuitFailure logs a circuit breaker failure with appropriate message.
func logCircuitFailure(logger Logger, panicValue any, newFailures int64, threshold int, cooldown time.Duration) {
	panicVal, stack := extractPanicValueAndStack(panicValue)
	err := toError(panicVal)

	if int(newFailures) == threshold {
		logErrorWithOptionalStack(logger, err, "circuit breaker opened",
			stack, "failures", newFailures, "cooldown", cooldown)
	} else {
		logErrorWithOptionalStack(logger, err, "circuit breaker recorded failure",
			stack, "failures", newFailures, "threshold", threshold)
	}
}

func CircuitBreaker(logger Logger, threshold int, cooldown time.Duration, opts ...CircuitBreakerOption) JobWrapper {
	wrapper, _ := circuitBreakerImpl(logger, threshold, cooldown, opts)
	return wrapper
}

// CircuitBreakerWithHandle is like CircuitBreaker but also returns a handle
// for querying the circuit breaker's internal state. The handle is safe for
// concurrent use and can be used from health checks, dashboards, or metrics
// exporters.
//
// Example:
//
//	wrapper, handle := cron.CircuitBreakerWithHandle(logger, 5, 5*time.Minute)
//	c := cron.New(cron.WithChain(cron.Recover(logger), wrapper))
//
//	// In a health check endpoint:
//	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
//	    state := handle.State()
//	    if state == cron.CircuitOpen {
//	        http.Error(w, "circuit open", http.StatusServiceUnavailable)
//	        return
//	    }
//	    w.Write([]byte("ok"))
//	})
func CircuitBreakerWithHandle(logger Logger, threshold int, cooldown time.Duration, opts ...CircuitBreakerOption) (JobWrapper, *CircuitBreakerHandle) {
	return circuitBreakerImpl(logger, threshold, cooldown, opts)
}

// notifyFailureTransition emits a state-change callback when a failure causes
// a circuit state transition (Closed→Open or HalfOpen→Open).
func notifyFailureTransition(cb func(CircuitBreakerEvent), wasHalfOpen bool, newFailures int64, threshold int, panicValue any) {
	if cb == nil {
		return
	}
	if int(newFailures) == threshold {
		cb(CircuitBreakerEvent{OldState: CircuitClosed, NewState: CircuitOpen, Failures: newFailures, Err: panicValue})
	} else if wasHalfOpen {
		cb(CircuitBreakerEvent{OldState: CircuitHalfOpen, NewState: CircuitOpen, Failures: newFailures, Err: panicValue})
	}
}

// notifyRecovery emits a state-change callback when the circuit closes after recovery.
func notifyRecovery(cb func(CircuitBreakerEvent), wasHalfOpen bool) {
	if cb == nil {
		return
	}
	fromState := CircuitHalfOpen
	if !wasHalfOpen {
		fromState = CircuitClosed
	}
	cb(CircuitBreakerEvent{OldState: fromState, NewState: CircuitClosed, Failures: 0})
}

func circuitBreakerImpl(logger Logger, threshold int, cooldown time.Duration, opts []CircuitBreakerOption) (JobWrapper, *CircuitBreakerHandle) {
	cfg := applyCircuitBreakerOptions(opts)
	state := &circuitState{}
	handle := &CircuitBreakerHandle{state: state, threshold: threshold, cooldown: cooldown}

	wrapper := func(j Job) Job {
		return FuncJob(func() {
			// Check if circuit is open
			if open, remaining := state.isOpen(threshold, cooldown); open {
				logger.Info("circuit breaker open",
					"failures", atomic.LoadInt64(&state.failures),
					"cooldown_remaining", remaining.Round(time.Second))
				return
			}

			// Determine current state for event tracking
			wasHalfOpen := state.isHalfOpen(threshold)

			// Log half-open state and emit transition event
			if wasHalfOpen {
				logger.Info("circuit breaker half-open", "attempting_recovery", true)
				if cfg.callback != nil {
					cfg.callback(CircuitBreakerEvent{
						OldState: CircuitOpen, NewState: CircuitHalfOpen,
						Failures: atomic.LoadInt64(&state.failures),
					})
				}
			}

			// Execute job
			panicValue := safeExecute(j.Run)

			if panicValue != nil {
				newFailures := state.recordFailure()
				logCircuitFailure(logger, panicValue, newFailures, threshold, cooldown)
				notifyFailureTransition(cfg.callback, wasHalfOpen, newFailures, threshold, panicValue)
				panic(panicValue)
			}

			if state.resetOnSuccess(threshold) {
				logger.Info("circuit breaker closed", "recovered", true)
				notifyRecovery(cfg.callback, wasHalfOpen)
			}
		})
	}

	return wrapper, handle
}
