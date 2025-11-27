package cron

import (
	"sync"
	"testing"
	"time"
)

func TestRealClock(t *testing.T) {
	clock := RealClock{}

	// Test Now() returns approximate current time
	before := time.Now()
	now := clock.Now()
	after := time.Now()

	if now.Before(before) || now.After(after) {
		t.Errorf("Now() returned %v, expected between %v and %v", now, before, after)
	}

	// Test NewTimer() creates working timer
	timer := clock.NewTimer(10 * time.Millisecond)
	select {
	case <-timer.C():
		// Timer fired correctly
	case <-time.After(100 * time.Millisecond):
		t.Error("timer did not fire within expected time")
	}
}

func TestRealTimerStop(t *testing.T) {
	clock := RealClock{}
	timer := clock.NewTimer(50 * time.Millisecond)

	// Stop should return true for active timer
	if !timer.Stop() {
		t.Error("Stop() should return true for active timer")
	}

	// Verify timer doesn't fire
	select {
	case <-timer.C():
		t.Error("timer should not fire after Stop()")
	case <-time.After(100 * time.Millisecond):
		// Expected: timer did not fire
	}
}

func TestRealTimerReset(t *testing.T) {
	clock := RealClock{}
	timer := clock.NewTimer(100 * time.Millisecond)

	// Reset to shorter duration
	timer.Reset(10 * time.Millisecond)

	select {
	case <-timer.C():
		// Timer fired after reset
	case <-time.After(50 * time.Millisecond):
		t.Error("timer should have fired after reset to shorter duration")
	}
}

func TestFakeClockNow(t *testing.T) {
	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	if !clock.Now().Equal(start) {
		t.Errorf("Now() = %v, want %v", clock.Now(), start)
	}
}

func TestFakeClockSet(t *testing.T) {
	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	newTime := time.Date(2024, 6, 15, 18, 30, 0, 0, time.UTC)
	clock.Set(newTime)

	if !clock.Now().Equal(newTime) {
		t.Errorf("After Set(), Now() = %v, want %v", clock.Now(), newTime)
	}
}

func TestFakeClockAdvance(t *testing.T) {
	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	clock.Advance(time.Hour)
	expected := start.Add(time.Hour)

	if !clock.Now().Equal(expected) {
		t.Errorf("After Advance(1h), Now() = %v, want %v", clock.Now(), expected)
	}
}

func TestFakeTimerFires(t *testing.T) {
	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	timer := clock.NewTimer(time.Minute)

	// Timer should not fire immediately
	select {
	case <-timer.C():
		t.Error("timer fired before time advanced")
	default:
		// Expected
	}

	// Advance time to trigger the timer
	clock.Advance(time.Minute)

	select {
	case firedAt := <-timer.C():
		expected := start.Add(time.Minute)
		if !firedAt.Equal(expected) {
			t.Errorf("timer fired at %v, want %v", firedAt, expected)
		}
	default:
		t.Error("timer should have fired after advancing time")
	}
}

func TestFakeTimerZeroDuration(t *testing.T) {
	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	// Timer with zero duration should fire immediately
	timer := clock.NewTimer(0)

	select {
	case <-timer.C():
		// Expected immediate fire
	default:
		t.Error("timer with zero duration should fire immediately")
	}
}

func TestFakeTimerStop(t *testing.T) {
	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	timer := clock.NewTimer(time.Minute)

	// Stop should return true for active timer
	if !timer.Stop() {
		t.Error("Stop() should return true for active timer")
	}

	// Second stop should return false
	if timer.Stop() {
		t.Error("Stop() should return false for already stopped timer")
	}

	// Advance time - timer should not fire
	clock.Advance(2 * time.Minute)

	select {
	case <-timer.C():
		t.Error("stopped timer should not fire")
	default:
		// Expected
	}
}

func TestFakeTimerReset(t *testing.T) {
	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	timer := clock.NewTimer(time.Hour)

	// Reset to shorter duration
	wasActive := timer.Reset(time.Minute)
	if !wasActive {
		t.Error("Reset() should return true for active timer")
	}

	// Advance by original duration minus epsilon - should not fire yet
	clock.Advance(30 * time.Second)
	select {
	case <-timer.C():
		t.Error("timer should not fire before reset duration")
	default:
	}

	// Advance past reset duration - should fire
	clock.Advance(30 * time.Second)
	select {
	case <-timer.C():
		// Expected
	default:
		t.Error("timer should fire after reset duration")
	}
}

func TestFakeClockMultipleTimers(t *testing.T) {
	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	timer1 := clock.NewTimer(time.Minute)
	timer2 := clock.NewTimer(2 * time.Minute)
	timer3 := clock.NewTimer(30 * time.Second)

	// Advance to fire first timer (30s)
	clock.Advance(30 * time.Second)

	select {
	case <-timer3.C():
		// Expected
	default:
		t.Error("timer3 should have fired")
	}

	// Other timers should not have fired
	select {
	case <-timer1.C():
		t.Error("timer1 should not have fired yet")
	default:
	}
	select {
	case <-timer2.C():
		t.Error("timer2 should not have fired yet")
	default:
	}

	// Advance to fire second timer (30s more = 1min total)
	clock.Advance(30 * time.Second)

	select {
	case <-timer1.C():
		// Expected
	default:
		t.Error("timer1 should have fired")
	}

	// timer2 still should not have fired
	select {
	case <-timer2.C():
		t.Error("timer2 should not have fired yet")
	default:
	}

	// Advance to fire third timer
	clock.Advance(time.Minute)

	select {
	case <-timer2.C():
		// Expected
	default:
		t.Error("timer2 should have fired")
	}
}

func TestFakeClockBlockUntil(t *testing.T) {
	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	// BlockUntil with 0 timers should return immediately
	done := make(chan struct{})
	go func() {
		clock.BlockUntil(0)
		close(done)
	}()

	select {
	case <-done:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("BlockUntil(0) should return immediately")
	}

	// BlockUntil with 1 timer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		clock.BlockUntil(1)
		wg.Done()
	}()

	// Create a timer
	clock.NewTimer(time.Hour)

	// Wait should complete
	waitDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("BlockUntil(1) should return after timer is created")
	}
}

func TestFakeClockTimerCount(t *testing.T) {
	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	if clock.TimerCount() != 0 {
		t.Errorf("TimerCount() = %d, want 0", clock.TimerCount())
	}

	t1 := clock.NewTimer(time.Minute)
	if clock.TimerCount() != 1 {
		t.Errorf("TimerCount() = %d, want 1", clock.TimerCount())
	}

	clock.NewTimer(time.Hour)
	if clock.TimerCount() != 2 {
		t.Errorf("TimerCount() = %d, want 2", clock.TimerCount())
	}

	t1.Stop()
	if clock.TimerCount() != 1 {
		t.Errorf("After Stop(), TimerCount() = %d, want 1", clock.TimerCount())
	}

	clock.Advance(time.Hour)
	if clock.TimerCount() != 0 {
		t.Errorf("After firing all, TimerCount() = %d, want 0", clock.TimerCount())
	}
}

func TestClockFuncAdapter(t *testing.T) {
	fixedTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := ClockFunc(func() time.Time { return fixedTime })

	if !clock.Now().Equal(fixedTime) {
		t.Errorf("ClockFunc Now() = %v, want %v", clock.Now(), fixedTime)
	}

	// ClockFunc should create real timers
	timer := clock.NewTimer(10 * time.Millisecond)
	select {
	case <-timer.C():
		// Timer fired
	case <-time.After(100 * time.Millisecond):
		t.Error("ClockFunc timer should work like real timer")
	}
}

func TestFakeClockConcurrency(t *testing.T) {
	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	var wg sync.WaitGroup
	timerCount := 100

	// Create many timers concurrently
	for i := 0; i < timerCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			timer := clock.NewTimer(time.Duration(i+1) * time.Second)
			// Randomly stop some timers
			if i%3 == 0 {
				timer.Stop()
			}
		}(i)
	}

	wg.Wait()

	// Advance time to fire all remaining timers
	clock.Advance(time.Duration(timerCount+1) * time.Second)

	// All timers should have been processed without panics
	if clock.TimerCount() != 0 {
		t.Errorf("After advancing, TimerCount() = %d, want 0", clock.TimerCount())
	}
}
