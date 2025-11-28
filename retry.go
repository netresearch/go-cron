package cron

import (
	"fmt"
	"math"
	"sync"
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
//   - maxRetries: Maximum retry attempts (0 = unlimited retries)
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

// calculateBackoffDelay returns the delay for a given retry attempt using exponential backoff.
func calculateBackoffDelay(attempt int, initialDelay, maxDelay time.Duration, multiplier float64) time.Duration {
	delay := time.Duration(float64(initialDelay) * math.Pow(multiplier, float64(attempt-2)))
	if delay > maxDelay {
		return maxDelay
	}
	return delay
}

// runWithRecovery executes a job and returns any panic value, or nil on success.
func runWithRecovery(j Job) (panicValue any) {
	defer func() {
		panicValue = recover()
	}()
	j.Run()
	return nil
}

func RetryWithBackoff(logger Logger, maxRetries int, initialDelay, maxDelay time.Duration, multiplier float64) JobWrapper {
	return func(j Job) Job {
		return FuncJob(func() {
			maxAttempts := maxRetries + 1 // maxRetries=3 means 4 total attempts (1 initial + 3 retries)
			if maxRetries == 0 {
				maxAttempts = 0 // unlimited
			}

			var lastPanic any
			for attempt := 1; maxAttempts == 0 || attempt <= maxAttempts; attempt++ {
				if attempt > 1 {
					delay := calculateBackoffDelay(attempt, initialDelay, maxDelay, multiplier)
					logger.Info("retry", "attempt", attempt, "delay", delay, "last_panic", lastPanic)
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

			// All retries exhausted
			logger.Error(fmt.Errorf("%v", lastPanic), "retry exhausted", "attempts", maxAttempts)
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
			failures   int
			lastFailAt time.Time
			mu         sync.Mutex
		)

		return FuncJob(func() {
			mu.Lock()
			currentFailures := failures
			timeSinceLastFail := time.Since(lastFailAt)

			// Check if circuit is open
			if currentFailures >= threshold && timeSinceLastFail < cooldown {
				mu.Unlock()
				remaining := cooldown - timeSinceLastFail
				logger.Info("circuit breaker open",
					"failures", currentFailures,
					"cooldown_remaining", remaining.Round(time.Second))
				return // Skip execution
			}

			// Entering half-open state if recovering from open
			halfOpen := currentFailures >= threshold
			mu.Unlock()

			if halfOpen {
				logger.Info("circuit breaker half-open", "attempting_recovery", true)
			}

			// Execute with panic recovery
			var panicked bool
			var panicValue any
			func() {
				defer func() {
					if r := recover(); r != nil {
						panicked = true
						panicValue = r
					}
				}()
				j.Run()
			}()

			mu.Lock()
			if panicked {
				failures++
				lastFailAt = time.Now()
				currentFailures = failures
				mu.Unlock()

				if currentFailures == threshold {
					logger.Error(fmt.Errorf("%v", panicValue), "circuit breaker opened",
						"failures", currentFailures, "cooldown", cooldown)
				} else {
					logger.Error(fmt.Errorf("%v", panicValue), "circuit breaker recorded failure",
						"failures", currentFailures, "threshold", threshold)
				}
				panic(panicValue) // Re-panic
			}

			// Success - reset failures
			wasOpen := failures >= threshold
			failures = 0
			mu.Unlock()

			if wasOpen {
				logger.Info("circuit breaker closed", "recovered", true)
			}
		})
	}
}
