package cron

import (
	"sync"
	"testing"
	"time"
)

func TestObservabilityHooks_JobLifecycle(t *testing.T) {
	var mu sync.Mutex
	var events []string

	hooks := ObservabilityHooks{
		OnJobStart: func(entryID EntryID, name string, scheduledTime time.Time) {
			mu.Lock()
			events = append(events, "start")
			mu.Unlock()
		},
		OnJobComplete: func(entryID EntryID, name string, duration time.Duration, recovered any) {
			mu.Lock()
			events = append(events, "complete")
			mu.Unlock()
		},
		OnSchedule: func(entryID EntryID, name string, nextRun time.Time) {
			mu.Lock()
			events = append(events, "schedule")
			mu.Unlock()
		},
	}

	clock := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cron := New(
		WithClock(clock),
		WithObservability(hooks),
	)

	jobDone := make(chan struct{})
	cron.AddFunc("@every 1h", func() {
		close(jobDone)
	})

	cron.Start()
	defer cron.Stop()

	// Wait for initial scheduling
	time.Sleep(50 * time.Millisecond)

	// Advance time to trigger job
	clock.Advance(time.Hour)

	// Wait for job to complete
	select {
	case <-jobDone:
	case <-time.After(2 * time.Second):
		t.Fatal("job did not run")
	}

	// Give hooks time to complete
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Should have: schedule (initial), start, schedule (next), complete
	if len(events) < 3 {
		t.Errorf("expected at least 3 events, got %d: %v", len(events), events)
	}

	// Verify we got start and complete events
	var hasStart, hasComplete bool
	for _, e := range events {
		if e == "start" {
			hasStart = true
		}
		if e == "complete" {
			hasComplete = true
		}
	}
	if !hasStart {
		t.Error("missing start event")
	}
	if !hasComplete {
		t.Error("missing complete event")
	}
}

func TestObservabilityHooks_JobName(t *testing.T) {
	var capturedName string
	var mu sync.Mutex

	hooks := ObservabilityHooks{
		OnJobStart: func(entryID EntryID, name string, scheduledTime time.Time) {
			mu.Lock()
			capturedName = name
			mu.Unlock()
		},
	}

	clock := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cron := New(
		WithClock(clock),
		WithObservability(hooks),
	)

	// Add a named job
	cron.Schedule(Every(time.Hour), &testNamedJob{name: "my-test-job"})

	cron.Start()
	defer cron.Stop()

	// Wait for initial scheduling
	time.Sleep(50 * time.Millisecond)

	// Advance time to trigger job
	clock.Advance(time.Hour)

	// Wait for job to run
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if capturedName != "my-test-job" {
		t.Errorf("expected job name 'my-test-job', got '%s'", capturedName)
	}
}

// testNamedJob implements both Job and NamedJob interfaces.
type testNamedJob struct {
	name string
}

func (j *testNamedJob) Run() {}

func (j *testNamedJob) Name() string {
	return j.name
}

func TestObservabilityHooks_NilHooks(t *testing.T) {
	// Test that nil hooks don't cause panics
	clock := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cron := New(WithClock(clock))

	jobDone := make(chan struct{})
	cron.AddFunc("@every 1h", func() {
		close(jobDone)
	})

	cron.Start()
	defer cron.Stop()

	// Advance time to trigger job
	time.Sleep(50 * time.Millisecond)
	clock.Advance(time.Hour)

	// Wait for job to complete
	select {
	case <-jobDone:
		// Success - no panic with nil hooks
	case <-time.After(2 * time.Second):
		t.Fatal("job did not run")
	}
}

func TestObservabilityHooks_PartialHooks(t *testing.T) {
	var startCalled bool
	var mu sync.Mutex

	// Only set OnJobStart, leave others nil
	hooks := ObservabilityHooks{
		OnJobStart: func(entryID EntryID, name string, scheduledTime time.Time) {
			mu.Lock()
			startCalled = true
			mu.Unlock()
		},
		// OnJobComplete is nil
		// OnSchedule is nil
		// OnSkip is nil
	}

	clock := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cron := New(
		WithClock(clock),
		WithObservability(hooks),
	)

	jobDone := make(chan struct{})
	cron.AddFunc("@every 1h", func() {
		close(jobDone)
	})

	cron.Start()
	defer cron.Stop()

	time.Sleep(50 * time.Millisecond)
	clock.Advance(time.Hour)

	select {
	case <-jobDone:
	case <-time.After(2 * time.Second):
		t.Fatal("job did not run")
	}

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if !startCalled {
		t.Error("OnJobStart should have been called")
	}
}

func TestObservabilityHooks_PanicRecovery(t *testing.T) {
	var capturedRecover any
	var completeCalled bool
	var mu sync.Mutex

	hooks := ObservabilityHooks{
		OnJobComplete: func(entryID EntryID, name string, duration time.Duration, recovered any) {
			mu.Lock()
			capturedRecover = recovered
			completeCalled = true
			mu.Unlock()
		},
	}

	clock := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cron := New(
		WithClock(clock),
		WithObservability(hooks),
		WithChain(Recover(DiscardLogger)), // Use Recover to handle panic
	)

	cron.AddFunc("@every 1h", func() {
		panic("test panic")
	})

	cron.Start()
	defer cron.Stop()

	time.Sleep(50 * time.Millisecond)
	clock.Advance(time.Hour)

	// Wait for job to complete (including panic handling)
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// The Recover wrapper catches the panic before our hook sees it.
	// From the hook's perspective, the wrapped job completed normally
	// (the Recover wrapper handled the error internally).
	// This is the expected behavior: hooks see what the wrapped job returns.
	if !completeCalled {
		t.Error("OnJobComplete should have been called")
	}
	// recovered should be nil since Recover handled the panic
	if capturedRecover != nil {
		t.Errorf("expected nil recovered (Recover handled panic), got %v", capturedRecover)
	}
}

func TestObservabilityHooks_Duration(t *testing.T) {
	var capturedDuration time.Duration
	var mu sync.Mutex

	hooks := ObservabilityHooks{
		OnJobComplete: func(entryID EntryID, name string, duration time.Duration, recovered any) {
			mu.Lock()
			capturedDuration = duration
			mu.Unlock()
		},
	}

	clock := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cron := New(
		WithClock(clock),
		WithObservability(hooks),
	)

	cron.AddFunc("@every 1h", func() {
		// Simulate work by advancing fake clock
		clock.Advance(100 * time.Millisecond)
	})

	cron.Start()
	defer cron.Stop()

	time.Sleep(50 * time.Millisecond)
	clock.Advance(time.Hour)

	// Wait for job to complete
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Duration should be approximately 100ms (based on fake clock advancement in job)
	if capturedDuration != 100*time.Millisecond {
		t.Errorf("expected duration ~100ms, got %v", capturedDuration)
	}
}

func TestObservabilityHooks_EntryID(t *testing.T) {
	var capturedID EntryID
	var mu sync.Mutex

	hooks := ObservabilityHooks{
		OnJobStart: func(entryID EntryID, name string, scheduledTime time.Time) {
			mu.Lock()
			capturedID = entryID
			mu.Unlock()
		},
	}

	clock := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cron := New(
		WithClock(clock),
		WithObservability(hooks),
	)

	id, _ := cron.AddFunc("@every 1h", func() {})

	cron.Start()
	defer cron.Stop()

	time.Sleep(50 * time.Millisecond)
	clock.Advance(time.Hour)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if capturedID != id {
		t.Errorf("expected entry ID %d, got %d", id, capturedID)
	}
}

func TestGetJobName(t *testing.T) {
	// Test with NamedJob
	namedJob := &testNamedJob{name: "test-name"}
	if name := getJobName(namedJob); name != "test-name" {
		t.Errorf("expected 'test-name', got '%s'", name)
	}

	// Test with FuncJob (not named)
	funcJob := FuncJob(func() {})
	if name := getJobName(funcJob); name != "" {
		t.Errorf("expected empty string, got '%s'", name)
	}
}

func BenchmarkObservabilityHooks_Overhead(b *testing.B) {
	// Benchmark the overhead of observability hooks
	hooks := ObservabilityHooks{
		OnJobStart: func(entryID EntryID, name string, scheduledTime time.Time) {
			// Simulate lightweight metric recording
			_ = entryID
		},
		OnJobComplete: func(entryID EntryID, name string, duration time.Duration, recovered any) {
			// Simulate lightweight metric recording
			_ = duration
		},
		OnSchedule: func(entryID EntryID, name string, nextRun time.Time) {
			// Simulate lightweight metric recording
			_ = nextRun
		},
	}

	h := &hooks

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.callOnJobStart(EntryID(i), FuncJob(func() {}), time.Now())
		h.callOnJobComplete(EntryID(i), FuncJob(func() {}), time.Second, nil)
		h.callOnSchedule(EntryID(i), FuncJob(func() {}), time.Now())
	}
}

func BenchmarkObservabilityHooks_NilOverhead(b *testing.B) {
	// Benchmark the overhead when hooks are nil
	var h *ObservabilityHooks

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.callOnJobStart(EntryID(i), FuncJob(func() {}), time.Now())
		h.callOnJobComplete(EntryID(i), FuncJob(func() {}), time.Second, nil)
		h.callOnSchedule(EntryID(i), FuncJob(func() {}), time.Now())
	}
}
