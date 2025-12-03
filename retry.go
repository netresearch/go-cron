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
	delay := time.Duration(float64(initialDelay) * math.Pow(multiplier, float64(attempt-2)))
	if delay > maxDelay {
		delay = maxDelay
	}
	// Apply jitter: ±10% randomization to prevent thundering herd
	// #nosec G404 -- math/rand is appropriate for jitter; cryptographic randomness not needed
	jitter := time.Duration(float64(delay) * jitterFraction * (2*rand.Float64() - 1))
	return delay + jitter
}

// PanicWithStack wraps a panic value with the stack trace at the point of panic.
// This allows re-panicking to preserve the original stack trace for debugging.
type PanicWithStack struct {
	Value any    // The original panic value
	Stack []byte // Stack trace at point of panic
}

// Error implements the error interface for PanicWithStack.
func (p *PanicWithStack) Error() string {
	return fmt.Sprintf("panic: %v", p.Value)
}

// String returns a detailed representation including the stack trace.
func (p *PanicWithStack) String() string {
	return fmt.Sprintf("panic: %v\nstack:\n%s", p.Value, p.Stack)
}

// Unwrap returns the original panic value if it was an error.
func (p *PanicWithStack) Unwrap() error {
	if err, ok := p.Value.(error); ok {
		return err
	}
	return nil
}

// safeExecute runs the given function and captures any panic value with stack trace.
// Returns nil if the function completes successfully, or a *PanicWithStack otherwise.
// This is a generic helper for panic-safe execution used throughout the library.
//
// The returned *PanicWithStack preserves the original stack trace, which is critical
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
			panicValue = &PanicWithStack{Value: r, Stack: buf}
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

// extractPanicValueAndStack extracts the original panic value and stack from a PanicWithStack.
// If the value is not a PanicWithStack, returns the value directly with empty stack.
func extractPanicValueAndStack(p any) (value any, stack string) {
	if pws, ok := p.(*PanicWithStack); ok {
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

func RetryWithBackoff(logger Logger, maxRetries int, initialDelay, maxDelay time.Duration, multiplier float64) JobWrapper {
	return func(j Job) Job {
		return FuncJob(func() {
			maxAttempts := computeMaxAttempts(maxRetries)
			var lastPanic any

			for attempt := 1; maxAttempts == 0 || attempt <= maxAttempts; attempt++ {
				lastPanic = executeRetryAttempt(j, logger, attempt, lastPanic, initialDelay, maxDelay, multiplier)
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

func CircuitBreaker(logger Logger, threshold int, cooldown time.Duration) JobWrapper {
	return func(j Job) Job {
		state := &circuitState{}

		return FuncJob(func() {
			// Check if circuit is open
			if open, remaining := state.isOpen(threshold, cooldown); open {
				logger.Info("circuit breaker open",
					"failures", atomic.LoadInt64(&state.failures),
					"cooldown_remaining", remaining.Round(time.Second))
				return
			}

			// Log half-open state
			if state.isHalfOpen(threshold) {
				logger.Info("circuit breaker half-open", "attempting_recovery", true)
			}

			// Execute job
			panicValue := safeExecute(j.Run)

			// Handle failure
			if panicValue != nil {
				newFailures := state.recordFailure()
				logCircuitFailure(logger, panicValue, newFailures, threshold, cooldown)
				panic(panicValue)
			}

			// Handle success
			if state.resetOnSuccess(threshold) {
				logger.Info("circuit breaker closed", "recovered", true)
			}
		})
	}
}
