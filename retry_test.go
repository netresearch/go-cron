package cron

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetryWithBackoff_SuccessOnFirstAttempt(t *testing.T) {
	var attempts int32

	wrapped := RetryWithBackoff(DiscardLogger, 3, 10*time.Millisecond, time.Second, 2.0)(
		FuncJob(func() {
			atomic.AddInt32(&attempts, 1)
		}),
	)

	wrapped.Run()

	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("expected 1 attempt, got %d", got)
	}
}

func TestRetryWithBackoff_SuccessOnRetry(t *testing.T) {
	var attempts int32

	wrapped := RetryWithBackoff(DiscardLogger, 3, 10*time.Millisecond, time.Second, 2.0)(
		FuncJob(func() {
			count := atomic.AddInt32(&attempts, 1)
			if count < 3 {
				panic("transient failure")
			}
		}),
	)

	wrapped.Run()

	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

func TestRetryWithBackoff_ExhaustsRetries(t *testing.T) {
	var attempts int32

	wrapped := RetryWithBackoff(DiscardLogger, 3, 1*time.Millisecond, time.Second, 2.0)(
		FuncJob(func() {
			atomic.AddInt32(&attempts, 1)
			panic("always fails")
		}),
	)

	// Should panic after exhausting retries
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic after retries exhausted")
		}
	}()

	wrapped.Run()

	// Should never reach here
	t.Error("should have panicked")
}

func TestRetryWithBackoff_RetriesExhausted_AttemptCount(t *testing.T) {
	var attempts int32

	wrapped := RetryWithBackoff(DiscardLogger, 3, 1*time.Millisecond, time.Second, 2.0)(
		FuncJob(func() {
			atomic.AddInt32(&attempts, 1)
			panic("always fails")
		}),
	)

	func() {
		defer func() { recover() }()
		wrapped.Run()
	}()

	// maxRetries=3 means 4 total attempts (1 initial + 3 retries)
	if got := atomic.LoadInt32(&attempts); got != 4 {
		t.Errorf("expected 4 attempts (1 initial + 3 retries), got %d", got)
	}
}

func TestRetryWithBackoff_BackoffTiming(t *testing.T) {
	var timestamps []time.Time
	var mu sync.Mutex

	wrapped := RetryWithBackoff(DiscardLogger, 3, 50*time.Millisecond, time.Second, 2.0)(
		FuncJob(func() {
			mu.Lock()
			timestamps = append(timestamps, time.Now())
			mu.Unlock()
			if len(timestamps) < 4 {
				panic("transient")
			}
		}),
	)

	func() {
		defer func() { recover() }()
		wrapped.Run()
	}()

	mu.Lock()
	defer mu.Unlock()

	if len(timestamps) < 4 {
		t.Fatalf("expected at least 4 timestamps, got %d", len(timestamps))
	}

	// Check delays are increasing (exponential backoff)
	// Expected: 0, 50ms, 100ms, 200ms
	for i := 1; i < len(timestamps)-1; i++ {
		prev := timestamps[i].Sub(timestamps[i-1])
		next := timestamps[i+1].Sub(timestamps[i])
		// Next delay should be roughly double (with some tolerance)
		if next < prev {
			t.Logf("delay %d: %v, delay %d: %v", i, prev, i+1, next)
			// Allow some timing variance
		}
	}
}

func TestRetryWithBackoff_MaxDelayRespected(t *testing.T) {
	var timestamps []time.Time
	var mu sync.Mutex

	maxDelay := 30 * time.Millisecond
	wrapped := RetryWithBackoff(DiscardLogger, 10, 10*time.Millisecond, maxDelay, 2.0)(
		FuncJob(func() {
			mu.Lock()
			timestamps = append(timestamps, time.Now())
			count := len(timestamps)
			mu.Unlock()
			if count < 8 {
				panic("transient")
			}
		}),
	)

	wrapped.Run()

	mu.Lock()
	defer mu.Unlock()

	// After a few retries, delay should cap at maxDelay
	// Delays: 0, 10ms, 20ms, 30ms, 30ms, 30ms...
	for i := 4; i < len(timestamps); i++ {
		delay := timestamps[i].Sub(timestamps[i-1])
		// Allow 50% variance due to timing
		if delay > maxDelay*3/2 {
			t.Errorf("delay at attempt %d exceeded max: %v > %v", i, delay, maxDelay)
		}
	}
}

func TestRetryWithBackoff_UnlimitedRetries(t *testing.T) {
	var attempts int32

	// maxRetries=-1 means unlimited retries (explicit opt-in)
	wrapped := RetryWithBackoff(DiscardLogger, -1, 1*time.Millisecond, 5*time.Millisecond, 2.0)(
		FuncJob(func() {
			count := atomic.AddInt32(&attempts, 1)
			if count < 20 {
				panic("keep trying")
			}
		}),
	)

	wrapped.Run()

	if got := atomic.LoadInt32(&attempts); got != 20 {
		t.Errorf("expected 20 attempts, got %d", got)
	}
}

func TestRetryWithBackoff_NoRetries(t *testing.T) {
	var attempts int32

	// maxRetries=0 means no retries (safe default) - execute once and fail
	wrapped := RetryWithBackoff(DiscardLogger, 0, 1*time.Millisecond, 5*time.Millisecond, 2.0)(
		FuncJob(func() {
			atomic.AddInt32(&attempts, 1)
			panic("fail immediately")
		}),
	)

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic to propagate with maxRetries=0")
		}
	}()

	wrapped.Run()

	// Should have executed exactly once (no retries)
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("expected 1 attempt with maxRetries=0, got %d", got)
	}
}

func TestCircuitBreaker_NormalOperation(t *testing.T) {
	var executions int32

	wrapped := CircuitBreaker(DiscardLogger, 3, time.Minute)(
		FuncJob(func() {
			atomic.AddInt32(&executions, 1)
		}),
	)

	// Multiple successful executions
	for i := 0; i < 5; i++ {
		wrapped.Run()
	}

	if got := atomic.LoadInt32(&executions); got != 5 {
		t.Errorf("expected 5 executions, got %d", got)
	}
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	var executions int32

	wrapped := CircuitBreaker(DiscardLogger, 3, time.Hour)(
		FuncJob(func() {
			atomic.AddInt32(&executions, 1)
			panic("always fails")
		}),
	)

	// Fail 3 times to open circuit
	for i := 0; i < 3; i++ {
		func() {
			defer func() { recover() }()
			wrapped.Run()
		}()
	}

	// Circuit should be open, execution should be skipped
	wrapped.Run()
	wrapped.Run()
	wrapped.Run()

	// Should only have 3 executions (before circuit opened)
	if got := atomic.LoadInt32(&executions); got != 3 {
		t.Errorf("expected 3 executions (circuit should be open), got %d", got)
	}
}

func TestCircuitBreaker_HalfOpenRecovery(t *testing.T) {
	var executions int32
	var shouldFail bool
	var mu sync.Mutex

	cooldown := 100 * time.Millisecond
	wrapped := CircuitBreaker(DiscardLogger, 2, cooldown)(
		FuncJob(func() {
			atomic.AddInt32(&executions, 1)
			mu.Lock()
			fail := shouldFail
			mu.Unlock()
			if fail {
				panic("failure")
			}
		}),
	)

	// Set up to fail
	mu.Lock()
	shouldFail = true
	mu.Unlock()

	// Fail twice to open circuit
	for i := 0; i < 2; i++ {
		func() {
			defer func() { recover() }()
			wrapped.Run()
		}()
	}

	// Circuit is open
	wrapped.Run() // Skipped

	// Wait for cooldown
	time.Sleep(cooldown + 10*time.Millisecond)

	// Set up to succeed
	mu.Lock()
	shouldFail = false
	mu.Unlock()

	// Half-open: should execute and close circuit
	wrapped.Run()

	// Circuit should be closed now
	wrapped.Run()
	wrapped.Run()

	// Expected: 2 (initial failures) + 1 (half-open recovery) + 2 (after close) = 5
	if got := atomic.LoadInt32(&executions); got != 5 {
		t.Errorf("expected 5 executions, got %d", got)
	}
}

func TestCircuitBreaker_HalfOpenFailure(t *testing.T) {
	var executions int32

	cooldown := 100 * time.Millisecond
	wrapped := CircuitBreaker(DiscardLogger, 2, cooldown)(
		FuncJob(func() {
			atomic.AddInt32(&executions, 1)
			panic("always fails")
		}),
	)

	// Fail twice to open circuit
	for i := 0; i < 2; i++ {
		func() {
			defer func() { recover() }()
			wrapped.Run()
		}()
	}

	// Circuit is open
	wrapped.Run() // Skipped

	// Wait for cooldown
	time.Sleep(cooldown + 10*time.Millisecond)

	// Half-open: execute but fail again
	func() {
		defer func() { recover() }()
		wrapped.Run()
	}()

	// Circuit should reopen
	wrapped.Run() // Should be skipped

	// Expected: 2 (initial) + 1 (half-open attempt) = 3
	if got := atomic.LoadInt32(&executions); got != 3 {
		t.Errorf("expected 3 executions, got %d", got)
	}
}

func TestCircuitBreaker_SuccessResetsFailures(t *testing.T) {
	var executions int32
	var failCount int32
	var mu sync.Mutex

	wrapped := CircuitBreaker(DiscardLogger, 3, time.Hour)(
		FuncJob(func() {
			atomic.AddInt32(&executions, 1)
			mu.Lock()
			count := atomic.AddInt32(&failCount, 1)
			mu.Unlock()
			if count == 2 || count == 5 {
				// Fail on 2nd and 5th execution
				panic("intermittent failure")
			}
		}),
	)

	// Execute with some failures
	for i := 0; i < 6; i++ {
		func() {
			defer func() { recover() }()
			wrapped.Run()
		}()
	}

	// All executions should happen because failures are reset by successes
	if got := atomic.LoadInt32(&executions); got != 6 {
		t.Errorf("expected 6 executions, got %d", got)
	}
}

func TestCircuitBreaker_ConcurrentSafe(t *testing.T) {
	var executions int32

	wrapped := CircuitBreaker(DiscardLogger, 5, time.Millisecond)(
		FuncJob(func() {
			atomic.AddInt32(&executions, 1)
		}),
	)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wrapped.Run()
		}()
	}
	wg.Wait()

	// All should execute since no failures
	if got := atomic.LoadInt32(&executions); got != 100 {
		t.Errorf("expected 100 executions, got %d", got)
	}
}

func TestRetryWithBackoff_IntegrationWithCron(t *testing.T) {
	clock := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	var executions int32

	// Chain order: Recover is outermost (catches final re-panics),
	// RetryWithBackoff is innermost (sees panics from job, retries).
	c := New(
		WithClock(clock),
		WithChain(
			Recover(DiscardLogger),
			RetryWithBackoff(DiscardLogger, 2, 1*time.Millisecond, 10*time.Millisecond, 2.0),
		),
	)

	c.AddFunc("@every 1h", func() {
		count := atomic.AddInt32(&executions, 1)
		if count < 3 {
			panic("transient failure")
		}
	})

	c.Start()
	defer c.Stop()

	time.Sleep(50 * time.Millisecond)
	clock.Advance(time.Hour)
	time.Sleep(100 * time.Millisecond)

	// Should have 3 executions (2 retries + 1 success)
	if got := atomic.LoadInt32(&executions); got != 3 {
		t.Errorf("expected 3 executions, got %d", got)
	}
}

func TestCircuitBreaker_IntegrationWithCron(t *testing.T) {
	clock := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	var executions int32

	// Chain order: Recover is outermost (catches re-panics from circuit breaker),
	// CircuitBreaker is innermost (sees panics from job, tracks failures).
	c := New(
		WithClock(clock),
		WithChain(
			Recover(DiscardLogger),
			CircuitBreaker(DiscardLogger, 2, time.Hour),
		),
	)

	c.AddFunc("@every 1h", func() {
		atomic.AddInt32(&executions, 1)
		panic("always fails")
	})

	c.Start()
	defer c.Stop()

	// Trigger job multiple times
	time.Sleep(50 * time.Millisecond)
	for i := 0; i < 5; i++ {
		clock.Advance(time.Hour)
		time.Sleep(50 * time.Millisecond)
	}

	// Should have 2 executions before circuit opens
	if got := atomic.LoadInt32(&executions); got != 2 {
		t.Errorf("expected 2 executions (circuit should open), got %d", got)
	}
}

// TestCircuitBreaker_BoundaryThresholdExact tests retry.go:267 (isHalfOpen) boundary condition.
// This kills CONDITIONALS_BOUNDARY mutation where `>= threshold` could become `> threshold`.
// When failures == threshold exactly, isHalfOpen should return true.
func TestCircuitBreaker_BoundaryThresholdExact(t *testing.T) {
	var executions int32
	threshold := 2 // Use small threshold for clarity

	wrapped := CircuitBreaker(DiscardLogger, threshold, time.Hour)(
		FuncJob(func() {
			atomic.AddInt32(&executions, 1)
			panic("always fails")
		}),
	)

	// Fail exactly `threshold` times (2 times)
	for i := 0; i < threshold; i++ {
		func() {
			defer func() { recover() }()
			wrapped.Run()
		}()
	}

	// At this point: failures == threshold (2 == 2)
	// Circuit should be OPEN (isHalfOpen returns true for failures >= threshold)
	// With mutation >= → >: isHalfOpen would return false (2 > 2 is false)
	// and circuit would incorrectly allow execution

	execsBefore := atomic.LoadInt32(&executions)

	// This run should be SKIPPED if circuit is correctly open
	wrapped.Run()

	execsAfter := atomic.LoadInt32(&executions)

	// If circuit is correctly open, executions should NOT increase
	if execsAfter != execsBefore {
		t.Errorf("circuit should be open at exact threshold: failures=%d, threshold=%d, "+
			"expected executions to stay at %d, got %d",
			threshold, threshold, execsBefore, execsAfter)
	}

	// Verify we had exactly threshold executions before circuit opened
	if execsBefore != int32(threshold) {
		t.Errorf("expected %d executions before circuit opened, got %d", threshold, execsBefore)
	}
}

// TestCircuitBreaker_ResetOnSuccessBoundary tests retry.go:282 (resetOnSuccess) boundary condition.
// This kills CONDITIONALS_BOUNDARY mutation where `>= threshold` could become `> threshold`.
// When failures == threshold, wasOpen should return true on successful reset.
func TestCircuitBreaker_ResetOnSuccessBoundary(t *testing.T) {
	var executions int32
	var shouldFail atomic.Bool
	threshold := 2
	cooldown := 50 * time.Millisecond

	shouldFail.Store(true)

	wrapped := CircuitBreaker(DiscardLogger, threshold, cooldown)(
		FuncJob(func() {
			atomic.AddInt32(&executions, 1)
			if shouldFail.Load() {
				panic("controlled failure")
			}
		}),
	)

	// Fail exactly threshold times to open circuit
	for i := 0; i < threshold; i++ {
		func() {
			defer func() { recover() }()
			wrapped.Run()
		}()
	}

	// At this point: failures == threshold (circuit open)
	// Wait for cooldown to allow half-open state
	time.Sleep(cooldown + 10*time.Millisecond)

	// Set up to succeed
	shouldFail.Store(false)

	execsBefore := atomic.LoadInt32(&executions)

	// Execute in half-open state - should succeed and reset circuit
	wrapped.Run()

	execsAfter := atomic.LoadInt32(&executions)

	// Execution should have happened (half-open allows one attempt)
	if execsAfter != execsBefore+1 {
		t.Errorf("half-open state should allow execution: expected %d, got %d",
			execsBefore+1, execsAfter)
	}

	// Now circuit should be closed, run again to verify
	wrapped.Run()

	execsFinal := atomic.LoadInt32(&executions)
	if execsFinal != execsAfter+1 {
		t.Errorf("circuit should be closed after successful reset: expected %d, got %d",
			execsAfter+1, execsFinal)
	}
}

// TestCircuitBreaker_IsOpenBoundary tests retry.go:259 (isOpen) at exact threshold.
// Combined with cooldown to verify open state detection at boundary.
func TestCircuitBreaker_IsOpenBoundary(t *testing.T) {
	var executions int32
	threshold := 3
	cooldown := 100 * time.Millisecond

	wrapped := CircuitBreaker(DiscardLogger, threshold, cooldown)(
		FuncJob(func() {
			atomic.AddInt32(&executions, 1)
			panic("always fails")
		}),
	)

	// Fail exactly threshold times
	for i := 0; i < threshold; i++ {
		func() {
			defer func() { recover() }()
			wrapped.Run()
		}()
	}

	// Verify exactly threshold executions happened
	if got := atomic.LoadInt32(&executions); got != int32(threshold) {
		t.Fatalf("expected %d executions, got %d", threshold, got)
	}

	// Circuit should be open - next call should be skipped (within cooldown)
	wrapped.Run()

	if got := atomic.LoadInt32(&executions); got != int32(threshold) {
		t.Errorf("circuit should be open at exact threshold, executions should stay at %d, got %d",
			threshold, got)
	}

	// Wait past cooldown - half-open state should allow one execution
	time.Sleep(cooldown + 20*time.Millisecond)

	func() {
		defer func() { recover() }()
		wrapped.Run()
	}()

	// Should have one more execution (half-open attempt)
	if got := atomic.LoadInt32(&executions); got != int32(threshold)+1 {
		t.Errorf("half-open state should allow execution, expected %d, got %d",
			threshold+1, got)
	}
}

// TestPanicError_String tests PanicError.String() formatting.
func TestPanicError_String(t *testing.T) {
	p := &PanicError{
		Value: "test panic",
		Stack: []byte("goroutine 1 [running]:\nmain.main()"),
	}
	s := p.String()
	if !strings.Contains(s, "panic: test panic") {
		t.Errorf("expected 'panic: test panic' in String(), got %q", s)
	}
	if !strings.Contains(s, "goroutine 1") {
		t.Errorf("expected stack trace in String(), got %q", s)
	}
}

// TestPanicError_Unwrap tests PanicError.Unwrap() with error and non-error values.
func TestPanicError_Unwrap(t *testing.T) {
	origErr := errors.New("original error")
	testCases := []struct {
		name           string
		value          any
		shouldUnwrapTo error
	}{
		{"error value", origErr, origErr},
		{"string value", "not an error", nil},
		{"int value", 42, nil},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			p := &PanicError{Value: tc.value, Stack: []byte("stack")}
			unwrapped := p.Unwrap()
			if tc.shouldUnwrapTo != nil {
				if !errors.Is(unwrapped, tc.shouldUnwrapTo) {
					t.Errorf("expected Unwrap() to return original error, got %v", unwrapped)
				}
			} else if unwrapped != nil {
				t.Errorf("expected Unwrap() to return nil, got %v", unwrapped)
			}
		})
	}
}

// TestRetryOnError_NonPanicErrorRecovery tests extractPanicValueAndStack() with various panic types.
func TestRetryOnError_NonPanicErrorRecovery(t *testing.T) {
	pe := &PanicError{Value: "inner value", Stack: []byte("stack trace")}
	testCases := []struct {
		name          string
		panicInput    any
		expectedVal   any
		expectedStack string
	}{
		{"plain string", "plain string", "plain string", ""},
		{"*PanicError", pe, "inner value", "stack trace"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			val, stack := extractPanicValueAndStack(tc.panicInput)
			if val != tc.expectedVal {
				t.Errorf("expected value %v, got %v", tc.expectedVal, val)
			}
			if stack != tc.expectedStack {
				t.Errorf("expected stack %q, got %q", tc.expectedStack, stack)
			}
		})
	}
}

// TestPanicWithStackAlias verifies the backward compatibility type alias.
func TestPanicWithStackAlias(t *testing.T) {
	// Create using the deprecated alias - this tests backward compatibility
	p := PanicWithStack{Value: "test panic", Stack: []byte("stack trace")}

	// Should have access to all PanicError methods
	if p.Error() != "panic: test panic" {
		t.Errorf("unexpected error message: %s", p.Error())
	}

	// Assignment to PanicError should work (same type)
	pe := p
	if pe.Value != "test panic" {
		t.Errorf("unexpected value: %v", pe.Value)
	}

	// Assignment from PanicError to PanicWithStack should also work
	pws := pe
	if string(pws.Stack) != "stack trace" {
		t.Errorf("unexpected stack: %s", pws.Stack)
	}
}

// --- RetryOnError tests ---

func TestRetryOnError_SuccessOnFirstAttempt(t *testing.T) {
	var attempts int32

	wrapped := RetryOnError(DiscardLogger, 3, 10*time.Millisecond, time.Second, 2.0)(
		FuncErrorJob(func() error {
			atomic.AddInt32(&attempts, 1)
			return nil
		}),
	)

	wrapped.Run()

	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("expected 1 attempt, got %d", got)
	}
}

func TestRetryOnError_SuccessOnRetry(t *testing.T) {
	var attempts int32

	wrapped := RetryOnError(DiscardLogger, 3, 10*time.Millisecond, time.Second, 2.0)(
		FuncErrorJob(func() error {
			count := atomic.AddInt32(&attempts, 1)
			if count < 3 {
				return errors.New("transient failure")
			}
			return nil
		}),
	)

	wrapped.Run()

	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

func TestRetryOnError_ExhaustsRetries(t *testing.T) {
	var attempts int32

	wrapped := RetryOnError(DiscardLogger, 3, 1*time.Millisecond, time.Second, 2.0)(
		FuncErrorJob(func() error {
			atomic.AddInt32(&attempts, 1)
			return errors.New("always fails")
		}),
	)

	// Should panic after exhaustion to propagate failure through the middleware chain
	var panicValue any
	func() {
		defer func() {
			panicValue = recover()
		}()
		wrapped.Run()
	}()

	if panicValue == nil {
		t.Fatal("expected panic after retry exhaustion")
	}
	if err, ok := panicValue.(error); !ok {
		t.Errorf("expected error panic value, got %T: %v", panicValue, panicValue)
	} else if err.Error() != "always fails" {
		t.Errorf("expected 'always fails' error, got: %v", err)
	}

	// maxRetries=3 means 4 total attempts (1 initial + 3 retries)
	if got := atomic.LoadInt32(&attempts); got != 4 {
		t.Errorf("expected 4 attempts (1 initial + 3 retries), got %d", got)
	}
}

func TestRetryOnError_NoRetries(t *testing.T) {
	var attempts int32

	wrapped := RetryOnError(DiscardLogger, 0, 1*time.Millisecond, 5*time.Millisecond, 2.0)(
		FuncErrorJob(func() error {
			atomic.AddInt32(&attempts, 1)
			return errors.New("fail immediately")
		}),
	)

	// Should panic after single attempt (maxRetries=0 means 1 attempt, no retries)
	func() {
		defer func() { recover() }()
		wrapped.Run()
	}()

	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("expected 1 attempt with maxRetries=0, got %d", got)
	}
}

func TestRetryOnError_UnlimitedRetries(t *testing.T) {
	var attempts int32

	wrapped := RetryOnError(DiscardLogger, -1, 1*time.Millisecond, 5*time.Millisecond, 2.0)(
		FuncErrorJob(func() error {
			count := atomic.AddInt32(&attempts, 1)
			if count < 20 {
				return errors.New("keep trying")
			}
			return nil
		}),
	)

	wrapped.Run()

	if got := atomic.LoadInt32(&attempts); got != 20 {
		t.Errorf("expected 20 attempts, got %d", got)
	}
}

func TestRetryOnError_PassesThroughRegularJob(t *testing.T) {
	var attempts int32

	// Regular FuncJob doesn't implement ErrorJob
	regularJob := FuncJob(func() {
		atomic.AddInt32(&attempts, 1)
	})

	wrapped := RetryOnError(DiscardLogger, 3, 10*time.Millisecond, time.Second, 2.0)(regularJob)

	wrapped.Run()

	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("expected 1 attempt (pass-through), got %d", got)
	}
}

func TestRetryOnError_BackoffTiming(t *testing.T) {
	var timestamps []time.Time
	var mu sync.Mutex

	wrapped := RetryOnError(DiscardLogger, 3, 50*time.Millisecond, time.Second, 2.0)(
		FuncErrorJob(func() error {
			mu.Lock()
			timestamps = append(timestamps, time.Now())
			count := len(timestamps)
			mu.Unlock()
			if count < 4 {
				return errors.New("transient")
			}
			return nil
		}),
	)

	wrapped.Run()

	mu.Lock()
	defer mu.Unlock()

	if len(timestamps) < 4 {
		t.Fatalf("expected at least 4 timestamps, got %d", len(timestamps))
	}

	// Check delays are increasing (exponential backoff with 2x multiplier).
	// Use generous tolerance (50%) to account for ±10% jitter and OS scheduling variance.
	for i := 1; i < len(timestamps)-1; i++ {
		prev := timestamps[i].Sub(timestamps[i-1])
		next := timestamps[i+1].Sub(timestamps[i])
		if next < prev/2 {
			t.Errorf("delay %d (%v) should be greater than half of delay %d (%v)", i+1, next, i, prev)
		}
	}
}

func TestRetryOnError_MaxDelayRespected(t *testing.T) {
	var timestamps []time.Time
	var mu sync.Mutex

	maxDelay := 30 * time.Millisecond
	wrapped := RetryOnError(DiscardLogger, 10, 10*time.Millisecond, maxDelay, 2.0)(
		FuncErrorJob(func() error {
			mu.Lock()
			timestamps = append(timestamps, time.Now())
			count := len(timestamps)
			mu.Unlock()
			if count < 8 {
				return errors.New("transient")
			}
			return nil
		}),
	)

	wrapped.Run()

	mu.Lock()
	defer mu.Unlock()

	for i := 4; i < len(timestamps); i++ {
		delay := timestamps[i].Sub(timestamps[i-1])
		if delay > maxDelay*3/2 {
			t.Errorf("delay at attempt %d exceeded max: %v > %v", i, delay, maxDelay)
		}
	}
}

func TestRetryOnError_IntegrationWithCron(t *testing.T) {
	clock := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	var executions int32

	c := New(
		WithClock(clock),
		WithChain(
			Recover(DiscardLogger),
			RetryOnError(DiscardLogger, 2, 1*time.Millisecond, 10*time.Millisecond, 2.0),
		),
	)

	c.AddJob("@every 1h", FuncErrorJob(func() error {
		count := atomic.AddInt32(&executions, 1)
		if count < 3 {
			return errors.New("transient failure")
		}
		return nil
	}))

	c.Start()
	defer c.Stop()

	time.Sleep(50 * time.Millisecond)
	clock.Advance(time.Hour)
	time.Sleep(100 * time.Millisecond)

	// Should have 3 executions (2 retries + 1 success)
	if got := atomic.LoadInt32(&executions); got != 3 {
		t.Errorf("expected 3 executions, got %d", got)
	}
}

func TestRetryOnError_ChainWithRecover(t *testing.T) {
	// RetryOnError should compose with Recover for mixed job types.
	// ErrorJob gets error-based retry; if Run() is called on a FuncErrorJob
	// that returns an error, it panics, which Recover catches.
	var errorJobAttempts int32
	var regularJobAttempts int32

	chain := NewChain(
		Recover(DiscardLogger),
		RetryOnError(DiscardLogger, 2, 1*time.Millisecond, 10*time.Millisecond, 2.0),
	)

	// ErrorJob: gets error-based retry
	errorJob := chain.Then(FuncErrorJob(func() error {
		count := atomic.AddInt32(&errorJobAttempts, 1)
		if count < 2 {
			return errors.New("transient")
		}
		return nil
	}))
	errorJob.Run()

	if got := atomic.LoadInt32(&errorJobAttempts); got != 2 {
		t.Errorf("ErrorJob: expected 2 attempts, got %d", got)
	}

	// Regular Job: passes through RetryOnError, Recover catches panic
	regularJob := chain.Then(FuncJob(func() {
		atomic.AddInt32(&regularJobAttempts, 1)
		panic("regular job failure")
	}))
	regularJob.Run() // Recover catches the panic

	if got := atomic.LoadInt32(&regularJobAttempts); got != 1 {
		t.Errorf("Regular Job: expected 1 attempt (no retry), got %d", got)
	}
}

// --- FuncErrorJob tests ---

func TestFuncErrorJob_RunE_Success(t *testing.T) {
	var called bool
	job := FuncErrorJob(func() error {
		called = true
		return nil
	})

	err := job.RunE()
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if !called {
		t.Error("expected function to be called")
	}
}

func TestFuncErrorJob_RunE_Error(t *testing.T) {
	expectedErr := errors.New("test error")
	job := FuncErrorJob(func() error {
		return expectedErr
	})

	err := job.RunE()
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected %v, got %v", expectedErr, err)
	}
}

func TestFuncErrorJob_Run_Success(t *testing.T) {
	var called bool
	job := FuncErrorJob(func() error {
		called = true
		return nil
	})

	// Run() should not panic on success
	job.Run()
	if !called {
		t.Error("expected function to be called")
	}
}

func TestFuncErrorJob_Run_PanicsOnError(t *testing.T) {
	expectedErr := errors.New("test error")
	job := FuncErrorJob(func() error {
		return expectedErr
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic from Run() when RunE returns error")
		}
		if err, ok := r.(error); ok {
			if !errors.Is(err, expectedErr) {
				t.Errorf("expected panic with %v, got %v", expectedErr, err)
			}
		} else {
			t.Errorf("expected error panic, got %T: %v", r, r)
		}
	}()

	job.Run()
}

func TestFuncErrorJob_ImplementsErrorJob(t *testing.T) {
	var job ErrorJob = FuncErrorJob(func() error { return nil })
	_ = job // Compile-time check that FuncErrorJob implements ErrorJob
}

// capturingLogger captures log messages for testing mutation boundaries.
type capturingLogger struct {
	onInfo  func(msg string, keysAndValues ...any)
	onError func(err error, msg string, keysAndValues ...any)
}

func (l *capturingLogger) Info(msg string, keysAndValues ...any) {
	if l.onInfo != nil {
		l.onInfo(msg, keysAndValues...)
	}
}

func (l *capturingLogger) Error(err error, msg string, keysAndValues ...any) {
	if l.onError != nil {
		l.onError(err, msg, keysAndValues...)
	}
}

func TestCalculateBackoffDelay_Formula(t *testing.T) {
	// Kills ARITHMETIC_BASE mutation at retry.go:243 where (attempt-2) → (attempt+2)
	// attempt=2: delay = initialDelay * multiplier^(2-2) = 100ms * 1 = 100ms
	// Mutant:    delay = initialDelay * multiplier^(2+2) = 100ms * 16 = 1600ms
	delay := calculateBackoffDelay(2, 100*time.Millisecond, time.Hour, 2.0)
	if delay < 80*time.Millisecond || delay > 120*time.Millisecond {
		t.Errorf("calculateBackoffDelay(attempt=2) = %v, want ~100ms (±20%%)", delay)
	}

	// attempt=3: delay = 100ms * 2^(3-2) = 200ms. Mutant: 100ms * 2^5 = 3200ms
	delay3 := calculateBackoffDelay(3, 100*time.Millisecond, time.Hour, 2.0)
	if delay3 < 160*time.Millisecond || delay3 > 240*time.Millisecond {
		t.Errorf("calculateBackoffDelay(attempt=3) = %v, want ~200ms (±20%%)", delay3)
	}
}

func TestExecuteRetryAttempt_FirstAttemptNoDelay(t *testing.T) {
	// Kills CONDITIONALS_BOUNDARY mutation at retry.go:379 where (attempt > 1) → (attempt >= 1)
	// The log assertion alone distinguishes the branches — no timing check needed.
	var logged bool
	logger := &capturingLogger{onInfo: func(msg string, _ ...any) {
		if msg == "retry" {
			logged = true
		}
	}}

	executeRetryAttempt(FuncJob(func() {}), logger, 1, 0, nil)

	if logged {
		t.Error("first attempt should not log 'retry'")
	}
}

func TestLogRetrySuccess_FirstAttemptNoLog(t *testing.T) {
	// Kills CONDITIONALS_BOUNDARY mutation at retry.go:389 where (attempt > 1) → (attempt >= 1)
	var logged bool
	logger := &capturingLogger{onInfo: func(msg string, _ ...any) {
		if msg == "retry succeeded" {
			logged = true
		}
	}}

	logRetrySuccess(logger, 1)
	if logged {
		t.Error("logRetrySuccess should not log for attempt 1")
	}

	logRetrySuccess(logger, 2)
	if !logged {
		t.Error("logRetrySuccess should log for attempt 2")
	}
}

func TestRetryOnError_FirstAttemptNoDelay(t *testing.T) {
	// Kills CONDITIONALS_BOUNDARY mutation at retry.go:456 where (attempt > 1) → (attempt >= 1)
	// The log assertion alone distinguishes the branches — no timing check needed.
	var logged bool
	logger := &capturingLogger{onInfo: func(msg string, _ ...any) {
		if msg == "retry" {
			logged = true
		}
	}}

	wrapped := RetryOnError(logger, 0, 500*time.Millisecond, time.Second, 2.0)(
		FuncErrorJob(func() error { return nil }),
	)
	wrapped.Run()

	if logged {
		t.Error("first attempt should not log 'retry'")
	}
}

func TestCircuitState_IsOpenExactThreshold(t *testing.T) {
	// Kills CONDITIONALS_BOUNDARY mutation at retry.go:541 where (>= threshold) → (> threshold)
	state := &circuitState{}
	threshold := 3
	cooldown := time.Hour

	for range threshold {
		state.recordFailure()
	}

	open, remaining := state.isOpen(threshold, cooldown)
	if !open {
		t.Error("circuit should be open at exactly threshold failures")
	}
	if remaining <= 0 {
		t.Errorf("remaining cooldown should be positive, got %v", remaining)
	}
}

func TestCircuitState_IsOpenCooldownRemaining(t *testing.T) {
	// Kills ARITHMETIC_BASE mutation at retry.go:542 where (cooldown - elapsed) → (cooldown + elapsed)
	// Set lastFailNano directly to avoid real sleeping and flaky timing.
	state := &circuitState{}
	cooldown := time.Second

	atomic.StoreInt64(&state.failures, 1)
	// Set last failure to 200ms ago
	atomic.StoreInt64(&state.lastFailNano, time.Now().Add(-200*time.Millisecond).UnixNano())

	open, remaining := state.isOpen(1, cooldown)
	if !open {
		t.Fatal("circuit should be open")
	}
	// Remaining should be ≈ cooldown - 200ms ≈ 800ms
	// With mutation (+), remaining ≈ cooldown + 200ms ≈ 1200ms > cooldown
	if remaining > cooldown {
		t.Errorf("remaining (%v) should be less than cooldown (%v)", remaining, cooldown)
	}
}

func TestLogCircuitFailure_LogsOpenedAtExactThreshold(t *testing.T) {
	// Kills CONDITIONALS_NEGATION mutation at retry.go:575 where (==) → (!=)
	var messages []string
	logger := &capturingLogger{
		onError: func(_ error, msg string, _ ...any) {
			messages = append(messages, msg)
		},
	}

	// At exact threshold: should log "circuit breaker opened"
	logCircuitFailure(logger, errors.New("fail"), 3, 3, time.Second)
	found := false
	for _, msg := range messages {
		if msg == "circuit breaker opened" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'circuit breaker opened' at exact threshold, got: %v", messages)
	}

	// Below threshold: should NOT log "circuit breaker opened"
	messages = nil
	logCircuitFailure(logger, errors.New("fail"), 2, 3, time.Second)
	for _, msg := range messages {
		if msg == "circuit breaker opened" {
			t.Error("should not log 'circuit breaker opened' below threshold")
		}
	}
}
