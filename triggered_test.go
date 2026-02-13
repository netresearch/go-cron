package cron

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTriggeredSchedule_Next(t *testing.T) {
	s := TriggeredSchedule{}
	now := time.Now()
	if next := s.Next(now); !next.IsZero() {
		t.Errorf("Next() = %v, want zero time", next)
	}
}

func TestTriggeredSchedule_Prev(t *testing.T) {
	s := TriggeredSchedule{}
	now := time.Now()
	if prev := s.Prev(now); !prev.IsZero() {
		t.Errorf("Prev() = %v, want zero time", prev)
	}
}

func TestIsTriggered(t *testing.T) {
	if !IsTriggered(TriggeredSchedule{}) {
		t.Error("IsTriggered(TriggeredSchedule{}) = false, want true")
	}
	if IsTriggered(Every(time.Minute)) {
		t.Error("IsTriggered(ConstantDelaySchedule) = true, want false")
	}
}

func TestParsing_Triggered(t *testing.T) {
	for _, desc := range []string{"@triggered", "@manual", "@none"} {
		sched, err := secondParser.Parse(desc)
		if err != nil {
			t.Fatalf("Parse(%q) error: %v", desc, err)
		}
		if _, ok := sched.(TriggeredSchedule); !ok {
			t.Errorf("Parse(%q) = %T, want TriggeredSchedule", desc, sched)
		}
	}
}

func TestTriggeredEntry_NeverAutoRuns(t *testing.T) {
	start := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithParser(secondParser))
	defer c.Stop()

	var runs int32
	_, err := c.AddFunc("@triggered", func() { atomic.AddInt32(&runs, 1) }, WithName("never-auto"))
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	c.Start()
	clock.BlockUntil(1)

	// Advance time significantly — triggered entry must not fire
	clock.Advance(10 * time.Second)
	time.Sleep(20 * time.Millisecond)

	if n := atomic.LoadInt32(&runs); n != 0 {
		t.Errorf("expected 0 auto runs, got %d", n)
	}
}

func TestTriggerEntry(t *testing.T) {
	start := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithParser(secondParser))
	defer c.Stop()

	done := make(chan struct{}, 1)
	id, err := c.AddFunc("@triggered", func() { done <- struct{}{} })
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	c.Start()
	clock.BlockUntil(1)

	if err := c.TriggerEntry(id); err != nil {
		t.Fatalf("TriggerEntry failed: %v", err)
	}

	select {
	case <-done:
		// success
	case <-time.After(time.Second):
		t.Fatal("triggered job did not run within 1s")
	}
}

func TestTriggerEntryByName(t *testing.T) {
	start := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithParser(secondParser))
	defer c.Stop()

	done := make(chan struct{}, 1)
	_, err := c.AddFunc("@triggered", func() { done <- struct{}{} }, WithName("my-job"))
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	c.Start()
	clock.BlockUntil(1)

	if err := c.TriggerEntryByName("my-job"); err != nil {
		t.Fatalf("TriggerEntryByName failed: %v", err)
	}

	select {
	case <-done:
		// success
	case <-time.After(time.Second):
		t.Fatal("triggered job did not run within 1s")
	}
}

func TestTriggerEntry_NotFound(t *testing.T) {
	c := New()
	c.Start()
	defer c.Stop()

	err := c.TriggerEntry(EntryID(9999))
	if !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("expected ErrEntryNotFound, got %v", err)
	}
}

func TestTriggerEntryByName_NotFound(t *testing.T) {
	c := New()
	c.Start()
	defer c.Stop()

	err := c.TriggerEntryByName("nonexistent")
	if !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("expected ErrEntryNotFound, got %v", err)
	}
}

func TestTriggerEntry_Paused(t *testing.T) {
	start := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithParser(secondParser))
	defer c.Stop()

	id, err := c.AddFunc("@triggered", func() {}, WithPaused())
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	c.Start()
	clock.BlockUntil(1)

	err = c.TriggerEntry(id)
	if !errors.Is(err, ErrEntryPaused) {
		t.Errorf("expected ErrEntryPaused, got %v", err)
	}
}

func TestTriggerEntry_NotRunning(t *testing.T) {
	c := New(WithParser(secondParser))

	id, err := c.AddFunc("@triggered", func() {})
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	// Scheduler not started — TriggerEntry should return ErrNotRunning
	err = c.TriggerEntry(id)
	if !errors.Is(err, ErrNotRunning) {
		t.Errorf("expected ErrNotRunning, got %v", err)
	}
}

func TestTriggerEntry_WithMiddleware(t *testing.T) {
	start := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(
		WithClock(clock),
		WithParser(secondParser),
		WithChain(SkipIfStillRunning(DiscardLogger)),
	)
	defer c.Stop()

	running := make(chan struct{})
	unblock := make(chan struct{})
	var runs int32

	id, err := c.AddFunc("@triggered", func() {
		atomic.AddInt32(&runs, 1)
		running <- struct{}{}
		<-unblock
	})
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	c.Start()
	clock.BlockUntil(1)

	// First trigger — should run
	if err := c.TriggerEntry(id); err != nil {
		t.Fatalf("first TriggerEntry failed: %v", err)
	}
	<-running // Wait for job to start

	// Second trigger — should be skipped by SkipIfStillRunning
	if err := c.TriggerEntry(id); err != nil {
		t.Fatalf("second TriggerEntry failed: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	close(unblock) // Let first job finish
	time.Sleep(20 * time.Millisecond)

	if n := atomic.LoadInt32(&runs); n != 1 {
		t.Errorf("expected 1 run (second skipped), got %d", n)
	}
}

func TestTriggerEntry_WithContext(t *testing.T) {
	start := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithParser(secondParser), WithChain())
	defer c.Stop()

	ctxCh := make(chan context.Context, 1)
	id, err := c.AddJob("@triggered", FuncJobWithContext(func(ctx context.Context) {
		ctxCh <- ctx
	}))
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	c.Start()
	clock.BlockUntil(1)

	if err := c.TriggerEntry(id); err != nil {
		t.Fatalf("TriggerEntry failed: %v", err)
	}

	select {
	case ctx := <-ctxCh:
		if ctx == nil {
			t.Error("received nil context")
		}
		// Verify the context is alive (not already canceled)
		select {
		case <-ctx.Done():
			t.Error("context already canceled")
		default:
			// good
		}
	case <-time.After(time.Second):
		t.Fatal("job did not run within 1s")
	}
}

func TestTriggerEntry_OnScheduledEntry(t *testing.T) {
	start := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithParser(secondParser))
	defer c.Stop()

	done := make(chan struct{}, 1)
	id, err := c.AddFunc("0 0 12 * * *", func() { done <- struct{}{} })
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	c.Start()
	clock.BlockUntil(1)

	// Trigger a scheduled entry before its schedule fires — should work
	if err := c.TriggerEntry(id); err != nil {
		t.Fatalf("TriggerEntry on scheduled entry failed: %v", err)
	}

	select {
	case <-done:
		// success
	case <-time.After(time.Second):
		t.Fatal("triggered scheduled job did not run within 1s")
	}
}

func TestWithRunImmediately_Triggered(t *testing.T) {
	start := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithParser(secondParser))
	defer c.Stop()

	var runs int32
	var once sync.Once
	done := make(chan struct{})
	_, err := c.AddFunc("@triggered", func() {
		atomic.AddInt32(&runs, 1)
		once.Do(func() { close(done) })
	}, WithRunImmediately())
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	c.Start()
	clock.BlockUntil(1)
	// WithRunImmediately schedules the first run at now, so advance to trigger it
	clock.Advance(0)

	select {
	case <-done:
		// Immediate run fired
	case <-time.After(time.Second):
		t.Fatal("immediate run did not fire within 1s")
	}

	if n := atomic.LoadInt32(&runs); n != 1 {
		t.Errorf("expected 1 immediate run, got %d", n)
	}

	// After the immediate run, the schedule returns zero time — no more auto runs
	clock.BlockUntil(1)
	clock.Advance(10 * time.Second)
	time.Sleep(20 * time.Millisecond)

	if n := atomic.LoadInt32(&runs); n != 1 {
		t.Errorf("expected still 1 run after advance, got %d", n)
	}
}

func TestTriggeredEntry_Snapshot(t *testing.T) {
	c := New(WithParser(secondParser))

	_, err := c.AddFunc("@triggered", func() {}, WithName("snap-job"))
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}
	_, err = c.AddFunc("* * * * * *", func() {}, WithName("sched-job"))
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	entries := c.Entries()
	for _, e := range entries {
		switch e.Name {
		case "snap-job":
			if !e.Triggered {
				t.Error("snap-job: Triggered = false, want true")
			}
		case "sched-job":
			if e.Triggered {
				t.Error("sched-job: Triggered = true, want false")
			}
		}
	}
}

func TestTriggerEntry_SetsEntryPrev(t *testing.T) {
	start := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithParser(secondParser))
	defer c.Stop()

	done := make(chan struct{}, 1)
	id, err := c.AddFunc("@triggered", func() { done <- struct{}{} }, WithName("prev-test"))
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	c.Start()
	clock.BlockUntil(1)

	// Before trigger, Prev should be zero
	e := c.Entry(id)
	if !e.Prev.IsZero() {
		t.Errorf("Prev before trigger = %v, want zero", e.Prev)
	}

	if err := c.TriggerEntry(id); err != nil {
		t.Fatalf("TriggerEntry failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("job did not run")
	}

	// After trigger, Prev should be set to trigger time
	e = c.Entry(id)
	if e.Prev.IsZero() {
		t.Error("Prev after trigger is zero, want non-zero")
	}
}

func TestTriggerEntry_WithRunOnce(t *testing.T) {
	start := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithParser(secondParser))
	defer c.Stop()

	done := make(chan struct{}, 1)
	id, err := c.AddFunc("@triggered", func() { done <- struct{}{} }, WithRunOnce())
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	c.Start()
	clock.BlockUntil(1)

	if err := c.TriggerEntry(id); err != nil {
		t.Fatalf("TriggerEntry failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("job did not run")
	}

	// Entry should be removed after run-once trigger
	time.Sleep(20 * time.Millisecond)
	e := c.Entry(id)
	if e.Valid() {
		t.Error("entry still valid after run-once trigger, expected removal")
	}
}

func TestTriggerEntry_MultipleTriggers(t *testing.T) {
	start := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithParser(secondParser))
	defer c.Stop()

	var runs int32
	_, err := c.AddFunc("@triggered", func() { atomic.AddInt32(&runs, 1) }, WithName("multi"))
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	c.Start()
	clock.BlockUntil(1)

	for i := 0; i < 3; i++ {
		if err := c.TriggerEntryByName("multi"); err != nil {
			t.Fatalf("trigger %d failed: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	time.Sleep(20 * time.Millisecond)
	if n := atomic.LoadInt32(&runs); n != 3 {
		t.Errorf("expected 3 runs, got %d", n)
	}
}

func TestTriggerEntry_ObservabilityHooks(t *testing.T) {
	start := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	var startCount, completeCount int32
	hooks := ObservabilityHooks{
		OnJobStart: func(_ EntryID, _ string, _ time.Time) {
			atomic.AddInt32(&startCount, 1)
		},
		OnJobComplete: func(_ EntryID, _ string, _ time.Duration, _ any) {
			atomic.AddInt32(&completeCount, 1)
		},
	}

	c := New(WithClock(clock), WithParser(secondParser), WithObservability(hooks))
	defer c.Stop()

	done := make(chan struct{}, 1)
	id, err := c.AddFunc("@triggered", func() { done <- struct{}{} })
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	c.Start()
	clock.BlockUntil(1)

	if err := c.TriggerEntry(id); err != nil {
		t.Fatalf("TriggerEntry failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("job did not run")
	}

	time.Sleep(20 * time.Millisecond)
	if n := atomic.LoadInt32(&startCount); n != 1 {
		t.Errorf("OnJobStart called %d times, want 1", n)
	}
	if n := atomic.LoadInt32(&completeCount); n != 1 {
		t.Errorf("OnJobComplete called %d times, want 1", n)
	}
}
