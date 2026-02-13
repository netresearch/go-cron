package cron

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- RetryWithBackoff callback tests (#186) ---

func TestRetryWithBackoff_Callback_SuccessFirstAttempt(t *testing.T) {
	var events []RetryAttempt
	var mu sync.Mutex

	wrapped := RetryWithBackoff(DiscardLogger, 3, 10*time.Millisecond, time.Second, 2.0,
		WithRetryCallback(func(a RetryAttempt) {
			mu.Lock()
			events = append(events, a)
			mu.Unlock()
		}),
	)(FuncJob(func() {}))

	wrapped.Run()

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Attempt != 1 {
		t.Errorf("expected attempt 1, got %d", events[0].Attempt)
	}
	if events[0].Delay != 0 {
		t.Errorf("expected zero delay for first attempt, got %v", events[0].Delay)
	}
	if events[0].Err != nil {
		t.Errorf("expected nil error on success, got %v", events[0].Err)
	}
	if events[0].WillRetry {
		t.Error("expected WillRetry=false on success")
	}
}

func TestRetryWithBackoff_Callback_RetryThenSuccess(t *testing.T) {
	var events []RetryAttempt
	var mu sync.Mutex
	var attempts int32

	wrapped := RetryWithBackoff(DiscardLogger, 3, 10*time.Millisecond, time.Second, 2.0,
		WithRetryCallback(func(a RetryAttempt) {
			mu.Lock()
			events = append(events, a)
			mu.Unlock()
		}),
	)(FuncJob(func() {
		count := atomic.AddInt32(&attempts, 1)
		if count < 3 {
			panic("transient")
		}
	}))

	wrapped.Run()

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	// First attempt: panics, will retry
	if events[0].Attempt != 1 || events[0].Err == nil || !events[0].WillRetry {
		t.Errorf("event[0] unexpected: %+v", events[0])
	}
	if events[0].Delay != 0 {
		t.Errorf("expected zero delay for first attempt, got %v", events[0].Delay)
	}

	// Second attempt: panics, will retry
	if events[1].Attempt != 2 || events[1].Err == nil || !events[1].WillRetry {
		t.Errorf("event[1] unexpected: %+v", events[1])
	}
	if events[1].Delay == 0 {
		t.Error("expected non-zero delay for retry attempt")
	}

	// Third attempt: succeeds
	if events[2].Attempt != 3 || events[2].Err != nil || events[2].WillRetry {
		t.Errorf("event[2] unexpected: %+v", events[2])
	}
}

func TestRetryWithBackoff_Callback_Exhausted(t *testing.T) {
	var events []RetryAttempt
	var mu sync.Mutex

	wrapped := RetryWithBackoff(DiscardLogger, 2, 1*time.Millisecond, time.Second, 2.0,
		WithRetryCallback(func(a RetryAttempt) {
			mu.Lock()
			events = append(events, a)
			mu.Unlock()
		}),
	)(FuncJob(func() {
		panic("always fails")
	}))

	func() {
		defer func() { recover() }()
		wrapped.Run()
	}()

	mu.Lock()
	defer mu.Unlock()

	// maxRetries=2 means 3 total attempts
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	// First two: WillRetry=true
	if !events[0].WillRetry {
		t.Error("event[0] should have WillRetry=true")
	}
	if !events[1].WillRetry {
		t.Error("event[1] should have WillRetry=true")
	}

	// Last: WillRetry=false (exhausted)
	if events[2].WillRetry {
		t.Error("event[2] should have WillRetry=false (exhausted)")
	}
	if events[2].Err == nil {
		t.Error("event[2] should have error set")
	}
}

func TestRetryWithBackoff_NoCallback(t *testing.T) {
	// Verify RetryWithBackoff works without callback (backward compat)
	var attempts int32

	wrapped := RetryWithBackoff(DiscardLogger, 3, 10*time.Millisecond, time.Second, 2.0)(
		FuncJob(func() {
			count := atomic.AddInt32(&attempts, 1)
			if count < 2 {
				panic("transient")
			}
		}),
	)

	wrapped.Run()

	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Errorf("expected 2 attempts, got %d", got)
	}
}

// --- RetryOnError callback tests (#186) ---

func TestRetryOnError_Callback_SuccessFirstAttempt(t *testing.T) {
	var events []RetryAttempt
	var mu sync.Mutex

	wrapped := RetryOnError(DiscardLogger, 3, 10*time.Millisecond, time.Second, 2.0,
		WithRetryCallback(func(a RetryAttempt) {
			mu.Lock()
			events = append(events, a)
			mu.Unlock()
		}),
	)(FuncErrorJob(func() error {
		return nil
	}))

	wrapped.Run()

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Err != nil {
		t.Errorf("expected nil error, got %v", events[0].Err)
	}
	if events[0].WillRetry {
		t.Error("expected WillRetry=false")
	}
}

func TestRetryOnError_Callback_RetryThenSuccess(t *testing.T) {
	var events []RetryAttempt
	var mu sync.Mutex
	var attempts int32

	wrapped := RetryOnError(DiscardLogger, 3, 10*time.Millisecond, time.Second, 2.0,
		WithRetryCallback(func(a RetryAttempt) {
			mu.Lock()
			events = append(events, a)
			mu.Unlock()
		}),
	)(FuncErrorJob(func() error {
		count := atomic.AddInt32(&attempts, 1)
		if count < 3 {
			return errors.New("transient")
		}
		return nil
	}))

	wrapped.Run()

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	// First: error, will retry
	if events[0].Err == nil || !events[0].WillRetry {
		t.Errorf("event[0] unexpected: %+v", events[0])
	}

	// Second: error, will retry
	if events[1].Err == nil || !events[1].WillRetry {
		t.Errorf("event[1] unexpected: %+v", events[1])
	}

	// Third: success
	if events[2].Err != nil || events[2].WillRetry {
		t.Errorf("event[2] unexpected: %+v", events[2])
	}
}

func TestRetryOnError_Callback_Exhausted(t *testing.T) {
	var events []RetryAttempt
	var mu sync.Mutex

	wrapped := RetryOnError(DiscardLogger, 2, 1*time.Millisecond, time.Second, 2.0,
		WithRetryCallback(func(a RetryAttempt) {
			mu.Lock()
			events = append(events, a)
			mu.Unlock()
		}),
	)(FuncErrorJob(func() error {
		return errors.New("always fails")
	}))

	func() {
		defer func() { recover() }()
		wrapped.Run()
	}()

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	// Last event should have WillRetry=false
	if events[2].WillRetry {
		t.Error("last event should have WillRetry=false")
	}
	if events[2].Err == nil {
		t.Error("last event should have error set")
	}
}

func TestRetryOnError_Callback_ErrorType(t *testing.T) {
	// Verify Err field contains the actual error value
	var capturedErr any
	expectedErr := errors.New("specific error")

	wrapped := RetryOnError(DiscardLogger, 0, 1*time.Millisecond, time.Second, 2.0,
		WithRetryCallback(func(a RetryAttempt) {
			capturedErr = a.Err
		}),
	)(FuncErrorJob(func() error {
		return expectedErr
	}))

	func() {
		defer func() { recover() }()
		wrapped.Run()
	}()

	if err, ok := capturedErr.(error); !ok || !errors.Is(err, expectedErr) {
		t.Errorf("expected %v, got %v", expectedErr, capturedErr)
	}
}

// --- CircuitBreaker state change callback tests (#185) ---

func TestCircuitBreaker_StateChangeCallback_ClosedToOpen(t *testing.T) {
	var events []CircuitBreakerEvent
	var mu sync.Mutex

	wrapped := CircuitBreaker(DiscardLogger, 3, time.Hour,
		WithStateChangeCallback(func(e CircuitBreakerEvent) {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
		}),
	)(FuncJob(func() {
		panic("always fails")
	}))

	for i := 0; i < 3; i++ {
		func() {
			defer func() { recover() }()
			wrapped.Run()
		}()
	}

	mu.Lock()
	defer mu.Unlock()

	// Should have one Closed → Open transition
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %+v", len(events), events)
	}
	if events[0].OldState != CircuitClosed || events[0].NewState != CircuitOpen {
		t.Errorf("expected Closed→Open, got %s→%s", events[0].OldState, events[0].NewState)
	}
	if events[0].Failures != 3 {
		t.Errorf("expected 3 failures, got %d", events[0].Failures)
	}
	if events[0].Err == nil {
		t.Error("expected error to be set")
	}
}

func TestCircuitBreaker_StateChangeCallback_HalfOpenToOpen(t *testing.T) {
	var events []CircuitBreakerEvent
	var mu sync.Mutex

	cooldown := 50 * time.Millisecond
	wrapped := CircuitBreaker(DiscardLogger, 2, cooldown,
		WithStateChangeCallback(func(e CircuitBreakerEvent) {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
		}),
	)(FuncJob(func() {
		panic("always fails")
	}))

	// Open circuit
	for i := 0; i < 2; i++ {
		func() {
			defer func() { recover() }()
			wrapped.Run()
		}()
	}

	// Wait for cooldown
	time.Sleep(cooldown + 10*time.Millisecond)

	// Half-open probe — should fail and reopen
	func() {
		defer func() { recover() }()
		wrapped.Run()
	}()

	mu.Lock()
	defer mu.Unlock()

	// Events: Closed→Open, Open→HalfOpen, HalfOpen→Open
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(events), events)
	}

	if events[1].OldState != CircuitOpen || events[1].NewState != CircuitHalfOpen {
		t.Errorf("event[1]: expected Open→HalfOpen, got %s→%s", events[1].OldState, events[1].NewState)
	}
	if events[2].OldState != CircuitHalfOpen || events[2].NewState != CircuitOpen {
		t.Errorf("event[2]: expected HalfOpen→Open, got %s→%s", events[2].OldState, events[2].NewState)
	}
}

func TestCircuitBreaker_StateChangeCallback_HalfOpenToClosed(t *testing.T) {
	var events []CircuitBreakerEvent
	var mu sync.Mutex
	var shouldFail atomic.Bool

	cooldown := 50 * time.Millisecond
	shouldFail.Store(true)

	wrapped := CircuitBreaker(DiscardLogger, 2, cooldown,
		WithStateChangeCallback(func(e CircuitBreakerEvent) {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
		}),
	)(FuncJob(func() {
		if shouldFail.Load() {
			panic("failure")
		}
	}))

	// Open circuit
	for i := 0; i < 2; i++ {
		func() {
			defer func() { recover() }()
			wrapped.Run()
		}()
	}

	// Wait for cooldown
	time.Sleep(cooldown + 10*time.Millisecond)

	// Set to succeed
	shouldFail.Store(false)

	// Half-open probe — should succeed and close
	wrapped.Run()

	mu.Lock()
	defer mu.Unlock()

	// Events: Closed→Open, Open→HalfOpen, HalfOpen→Closed
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(events), events)
	}

	if events[2].OldState != CircuitHalfOpen || events[2].NewState != CircuitClosed {
		t.Errorf("event[2]: expected HalfOpen→Closed, got %s→%s", events[2].OldState, events[2].NewState)
	}
	if events[2].Failures != 0 {
		t.Errorf("expected 0 failures after close, got %d", events[2].Failures)
	}
}

func TestCircuitBreaker_NoCallback(t *testing.T) {
	// Verify CircuitBreaker works without callback (backward compat)
	var executions int32

	wrapped := CircuitBreaker(DiscardLogger, 3, time.Hour)(
		FuncJob(func() {
			atomic.AddInt32(&executions, 1)
		}),
	)

	for i := 0; i < 5; i++ {
		wrapped.Run()
	}

	if got := atomic.LoadInt32(&executions); got != 5 {
		t.Errorf("expected 5 executions, got %d", got)
	}
}

// --- CircuitBreakerWithHandle tests (#185) ---

func TestCircuitBreakerHandle_InitialState(t *testing.T) {
	_, handle := CircuitBreakerWithHandle(DiscardLogger, 3, time.Minute)

	if handle.State() != CircuitClosed {
		t.Errorf("expected closed, got %s", handle.State())
	}
	if handle.Failures() != 0 {
		t.Errorf("expected 0 failures, got %d", handle.Failures())
	}
	if !handle.LastFailure().IsZero() {
		t.Errorf("expected zero time, got %v", handle.LastFailure())
	}
	if !handle.CooldownEnds().IsZero() {
		t.Errorf("expected zero time, got %v", handle.CooldownEnds())
	}
}

func TestCircuitBreakerHandle_Open(t *testing.T) {
	wrapper, handle := CircuitBreakerWithHandle(DiscardLogger, 2, time.Hour)

	wrapped := wrapper(FuncJob(func() {
		panic("fail")
	}))

	// Fail twice to open
	for i := 0; i < 2; i++ {
		func() {
			defer func() { recover() }()
			wrapped.Run()
		}()
	}

	if handle.State() != CircuitOpen {
		t.Errorf("expected open, got %s", handle.State())
	}
	if handle.Failures() != 2 {
		t.Errorf("expected 2 failures, got %d", handle.Failures())
	}
	if handle.LastFailure().IsZero() {
		t.Error("expected non-zero last failure time")
	}
	if handle.CooldownEnds().IsZero() {
		t.Error("expected non-zero cooldown end time")
	}
	if handle.CooldownEnds().Before(time.Now()) {
		t.Error("cooldown should be in the future")
	}
}

func TestCircuitBreakerHandle_HalfOpen(t *testing.T) {
	cooldown := 50 * time.Millisecond
	wrapper, handle := CircuitBreakerWithHandle(DiscardLogger, 2, cooldown)

	wrapped := wrapper(FuncJob(func() {
		panic("fail")
	}))

	// Open circuit
	for i := 0; i < 2; i++ {
		func() {
			defer func() { recover() }()
			wrapped.Run()
		}()
	}

	// Wait for cooldown
	time.Sleep(cooldown + 10*time.Millisecond)

	// Should be half-open: failures >= threshold but cooldown expired
	if handle.State() != CircuitHalfOpen {
		t.Errorf("expected half-open, got %s", handle.State())
	}
	if !handle.CooldownEnds().IsZero() {
		t.Errorf("expected zero cooldown end (expired), got %v", handle.CooldownEnds())
	}
}

func TestCircuitBreakerHandle_Recovery(t *testing.T) {
	cooldown := 50 * time.Millisecond
	var shouldFail atomic.Bool
	shouldFail.Store(true)

	wrapper, handle := CircuitBreakerWithHandle(DiscardLogger, 2, cooldown)

	wrapped := wrapper(FuncJob(func() {
		if shouldFail.Load() {
			panic("fail")
		}
	}))

	// Open circuit
	for i := 0; i < 2; i++ {
		func() {
			defer func() { recover() }()
			wrapped.Run()
		}()
	}

	time.Sleep(cooldown + 10*time.Millisecond)

	// Recover
	shouldFail.Store(false)
	wrapped.Run()

	if handle.State() != CircuitClosed {
		t.Errorf("expected closed after recovery, got %s", handle.State())
	}
	if handle.Failures() != 0 {
		t.Errorf("expected 0 failures after recovery, got %d", handle.Failures())
	}
}

func TestCircuitBreakerHandle_WithCallback(t *testing.T) {
	// Verify handle and callback work together
	var events []CircuitBreakerEvent
	var mu sync.Mutex

	wrapper, handle := CircuitBreakerWithHandle(DiscardLogger, 2, time.Hour,
		WithStateChangeCallback(func(e CircuitBreakerEvent) {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
		}),
	)

	wrapped := wrapper(FuncJob(func() {
		panic("fail")
	}))

	for i := 0; i < 2; i++ {
		func() {
			defer func() { recover() }()
			wrapped.Run()
		}()
	}

	if handle.State() != CircuitOpen {
		t.Errorf("expected open, got %s", handle.State())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].NewState != CircuitOpen {
		t.Errorf("expected Open event, got %s", events[0].NewState)
	}
}

// --- CircuitBreakerState.String() tests ---

func TestCircuitBreakerState_String(t *testing.T) {
	tests := []struct {
		state    CircuitBreakerState
		expected string
	}{
		{CircuitClosed, "closed"},
		{CircuitOpen, "open"},
		{CircuitHalfOpen, "half-open"},
		{CircuitBreakerState(99), "unknown(99)"},
	}

	for _, tc := range tests {
		if got := tc.state.String(); got != tc.expected {
			t.Errorf("CircuitBreakerState(%d).String() = %q, want %q", tc.state, got, tc.expected)
		}
	}
}

// --- RetryWithBackoff callback with unlimited retries ---

func TestRetryWithBackoff_Callback_UnlimitedRetries(t *testing.T) {
	var eventCount int32

	wrapped := RetryWithBackoff(DiscardLogger, -1, 1*time.Millisecond, 5*time.Millisecond, 2.0,
		WithRetryCallback(func(a RetryAttempt) {
			atomic.AddInt32(&eventCount, 1)
			// With unlimited retries and a failing job, WillRetry should be true
			// until the job eventually succeeds
			if a.Err != nil && !a.WillRetry {
				panic("unlimited retries should always have WillRetry=true on failure")
			}
		}),
	)(FuncJob(func() {
		count := atomic.LoadInt32(&eventCount)
		if count < 5 {
			panic("keep trying")
		}
	}))

	wrapped.Run()

	if got := atomic.LoadInt32(&eventCount); got < 5 {
		t.Errorf("expected at least 5 events, got %d", got)
	}
}
