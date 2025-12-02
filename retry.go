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

func RetryWithBackoff(logger Logger, maxRetries int, initialDelay, maxDelay time.Duration, multiplier float64) JobWrapper {
	return func(j Job) Job {
		return FuncJob(func() {
			// maxRetries semantics:
			//   0  = no retries (1 execution total) - safe default
			//   >0 = retry up to N times (N+1 total attempts)
			//   -1 = unlimited retries (explicit opt-in)
			maxAttempts := maxRetries + 1 // maxRetries=3 means 4 total attempts (1 initial + 3 retries)
			if maxRetries < 0 {
				maxAttempts = 0 // unlimited (loop condition becomes: 0 == 0 || attempt <= 0 → always true)
			}

			var lastPanic any
			for attempt := 1; maxAttempts == 0 || attempt <= maxAttempts; attempt++ {
				if attempt > 1 {
					delay := calculateBackoffDelay(attempt, initialDelay, maxDelay, multiplier)
					// Extract original panic value for logging
					panicVal := lastPanic
					if pws, ok := lastPanic.(*PanicWithStack); ok {
						panicVal = pws.Value
					}
					logger.Info("retry", "attempt", attempt, "delay", delay, "last_panic", panicVal)
					time.Sleep(delay)
				}

				lastPanic = runWithRecovery(j)
				if lastPanic == nil {
					if attempt > 1 {
						logger.Info("retry succeeded", "attempt", attempt)
					}
					return // Success
				}
			}

			// All retries exhausted - log with stack trace if available
			panicVal := lastPanic
			var stack string
			if pws, ok := lastPanic.(*PanicWithStack); ok {
				panicVal = pws.Value
				stack = string(pws.Stack)
			}
			err, ok := panicVal.(error)
			if !ok {
				err = fmt.Errorf("%v", panicVal)
			}
			if stack != "" {
				logger.Error(err, "retry exhausted", "attempts", maxAttempts, "stack", stack)
			} else {
				logger.Error(err, "retry exhausted", "attempts", maxAttempts)
			}
			panic(lastPanic) // Re-panic for outer wrappers to handle
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
func CircuitBreaker(logger Logger, threshold int, cooldown time.Duration) JobWrapper {
	return func(j Job) Job {
		var (
			failures     int64      // atomic: current consecutive failure count
			lastFailNano int64      // atomic: unix nano of last failure
			mu           sync.Mutex // only for state transitions (not hot path)
		)

		return FuncJob(func() {
			// Fast path: check circuit state with atomics (no lock)
			currentFailures := int(atomic.LoadInt64(&failures))
			lastFail := atomic.LoadInt64(&lastFailNano)
			timeSinceLastFail := time.Since(time.Unix(0, lastFail))

			// Check if circuit is open
			if currentFailures >= threshold && timeSinceLastFail < cooldown {
				remaining := cooldown - timeSinceLastFail
				logger.Info("circuit breaker open",
					"failures", currentFailures,
					"cooldown_remaining", remaining.Round(time.Second))
				return // Skip execution
			}

			// Entering half-open state if recovering from open
			halfOpen := currentFailures >= threshold
			if halfOpen {
				logger.Info("circuit breaker half-open", "attempting_recovery", true)
			}

			// Execute with panic recovery using consolidated helper
			panicValue := safeExecute(j.Run)

			// State transition: use mutex to ensure consistency
			mu.Lock()
			if panicValue != nil {
				newFailures := atomic.AddInt64(&failures, 1)
				atomic.StoreInt64(&lastFailNano, time.Now().UnixNano())
				mu.Unlock()

				// Extract original value and stack for logging
				pVal := panicValue
				var stack string
				if pws, ok := panicValue.(*PanicWithStack); ok {
					pVal = pws.Value
					stack = string(pws.Stack)
				}
				err, ok := pVal.(error)
				if !ok {
					err = fmt.Errorf("%v", pVal)
				}

				if int(newFailures) == threshold {
					if stack != "" {
						logger.Error(err, "circuit breaker opened",
							"failures", newFailures, "cooldown", cooldown, "stack", stack)
					} else {
						logger.Error(err, "circuit breaker opened",
							"failures", newFailures, "cooldown", cooldown)
					}
				} else {
					if stack != "" {
						logger.Error(err, "circuit breaker recorded failure",
							"failures", newFailures, "threshold", threshold, "stack", stack)
					} else {
						logger.Error(err, "circuit breaker recorded failure",
							"failures", newFailures, "threshold", threshold)
					}
				}
				panic(panicValue) // Re-panic
			}

			// Success - reset failures atomically
			wasOpen := atomic.LoadInt64(&failures) >= int64(threshold)
			atomic.StoreInt64(&failures, 0)
			mu.Unlock()

			if wasOpen {
				logger.Info("circuit breaker closed", "recovered", true)
			}
		})
	}
}
