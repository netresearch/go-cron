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

// TestFakeTimerZeroDurationNotInHeap verifies that immediate timers (d <= 0)
// are NOT added to the timer heap. This kills the mutation at clock.go:96
// where heapIndex = -1 could be changed to 0 or 1.
func TestFakeTimerZeroDurationNotInHeap(t *testing.T) {
	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	// Create an immediate timer (zero duration)
	timer := clock.NewTimer(0)

	// Timer should fire immediately
	select {
	case <-timer.C():
		// Expected
	default:
		t.Error("zero duration timer should fire immediately")
	}

	// Timer count should be 0 - immediate timers don't enter the heap
	if clock.TimerCount() != 0 {
		t.Errorf("TimerCount() = %d, want 0 for immediate timer", clock.TimerCount())
	}

	// Test negative duration as well
	timer2 := clock.NewTimer(-1 * time.Second)
	select {
	case <-timer2.C():
		// Expected - negative duration also fires immediately
	default:
		t.Error("negative duration timer should fire immediately")
	}

	if clock.TimerCount() != 0 {
		t.Errorf("TimerCount() = %d, want 0 for negative duration timer", clock.TimerCount())
	}
}

// TestFakeTimerResetZeroDuration verifies that Reset(0) fires the timer
// immediately. This kills the mutation at clock.go:234 where d <= 0
// could be changed to d < 0.
func TestFakeTimerResetZeroDuration(t *testing.T) {
	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	// Create a normal timer
	timer := clock.NewTimer(time.Hour)

	// Reset to zero duration
	timer.Reset(0)

	// Timer should fire immediately after Reset(0)
	select {
	case <-timer.C():
		// Expected
	default:
		t.Error("Reset(0) should fire timer immediately")
	}

	// Timer should not be in heap anymore
	if clock.TimerCount() != 0 {
		t.Errorf("After Reset(0), TimerCount() = %d, want 0", clock.TimerCount())
	}
}

// TestFakeTimerResetNegativeDuration verifies that Reset with negative duration
// also fires immediately. This tests the boundary at clock.go:234.
func TestFakeTimerResetNegativeDuration(t *testing.T) {
	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	timer := clock.NewTimer(time.Hour)
	timer.Reset(-1 * time.Second)

	select {
	case <-timer.C():
		// Expected
	default:
		t.Error("Reset(-1s) should fire timer immediately")
	}
}

// TestFakeTimerRemovalFromHeap tests that timers are properly removed from heap
// when they fire, ensuring heapIndex is correctly maintained.
// This helps kill mutations at clock.go:274.
func TestFakeTimerRemovalFromHeap(t *testing.T) {
	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	// Create multiple timers
	t1 := clock.NewTimer(1 * time.Minute)
	t2 := clock.NewTimer(2 * time.Minute)
	t3 := clock.NewTimer(3 * time.Minute)

	if clock.TimerCount() != 3 {
		t.Errorf("TimerCount() = %d, want 3", clock.TimerCount())
	}

	// Fire first timer
	clock.Advance(1 * time.Minute)
	<-t1.C()

	if clock.TimerCount() != 2 {
		t.Errorf("After first fire, TimerCount() = %d, want 2", clock.TimerCount())
	}

	// Stop second timer (should remove it from heap)
	t2.Stop()

	if clock.TimerCount() != 1 {
		t.Errorf("After stop, TimerCount() = %d, want 1", clock.TimerCount())
	}

	// Fire third timer
	clock.Advance(2 * time.Minute)
	<-t3.C()

	if clock.TimerCount() != 0 {
		t.Errorf("After all fired/stopped, TimerCount() = %d, want 0", clock.TimerCount())
	}
}

// TestImmediateTimerHeapIndexSentinel verifies that immediate timers (d<=0)
// have heapIndex=-1 and cannot be incorrectly found in the heap.
// This kills mutations at clock.go:96 where heapIndex: -1 could become 0 or 1.
func TestImmediateTimerHeapIndexSentinel(t *testing.T) {
	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	// Create a normal timer first (this will be at index 0)
	normalTimer := clock.NewTimer(1 * time.Hour)
	if clock.TimerCount() != 1 {
		t.Fatalf("TimerCount() = %d, want 1", clock.TimerCount())
	}

	// Create an immediate timer (d=0) - this should NOT be in heap
	immediateTimer := clock.NewTimer(0)
	<-immediateTimer.C() // Consume the immediate fire

	// Timer count should still be 1 (immediate timer never entered heap)
	if clock.TimerCount() != 1 {
		t.Errorf("After immediate timer, TimerCount() = %d, want 1", clock.TimerCount())
	}

	// Stopping the immediate timer should return false (not in heap)
	// If heapIndex were 0 instead of -1, this would try to stop the normalTimer!
	wasActive := immediateTimer.Stop()
	if wasActive {
		t.Error("Stop() on immediate timer should return false (not active)")
	}

	// Verify the normal timer is still in the heap and working
	if clock.TimerCount() != 1 {
		t.Errorf("After stopping immediate, TimerCount() = %d, want 1", clock.TimerCount())
	}

	// Stop the normal timer - should work
	wasActive = normalTimer.Stop()
	if !wasActive {
		t.Error("Stop() on normal timer should return true")
	}

	if clock.TimerCount() != 0 {
		t.Errorf("After stopping normal, TimerCount() = %d, want 0", clock.TimerCount())
	}
}

// TestTimerCannotBeRemovedTwice verifies that after a timer is popped/stopped,
// its heapIndex is reset to -1 and it cannot be removed again.
// This kills mutations at clock.go:274 where t.heapIndex = -1 could become 0.
func TestTimerCannotBeRemovedTwice(t *testing.T) {
	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	// Add two timers
	timer1 := clock.NewTimer(1 * time.Minute)
	timer2 := clock.NewTimer(2 * time.Minute)

	if clock.TimerCount() != 2 {
		t.Fatalf("TimerCount() = %d, want 2", clock.TimerCount())
	}

	// Fire first timer (removes it from heap, sets heapIndex to -1)
	clock.Advance(1 * time.Minute)
	<-timer1.C()

	if clock.TimerCount() != 1 {
		t.Errorf("After first fire, TimerCount() = %d, want 1", clock.TimerCount())
	}

	// Try to stop the already-fired timer
	// If heapIndex were 0 instead of -1, this would remove timer2!
	wasActive := timer1.Stop()
	if wasActive {
		t.Error("Stop() on already-fired timer should return false")
	}

	// Verify timer2 is still in the heap
	if clock.TimerCount() != 1 {
		t.Errorf("After double-stop attempt, TimerCount() = %d, want 1", clock.TimerCount())
	}

	// Timer2 should still work
	clock.Advance(1 * time.Minute)
	select {
	case <-timer2.C():
		// Expected
	default:
		t.Error("timer2 should have fired")
	}
}

// TestRemoveTimerBoundaryCondition tests the idx >= len(*h) check in RemoveTimer.
// This kills the mutation at clock.go:284 where >= could become >.
func TestRemoveTimerBoundaryCondition(t *testing.T) {
	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	// Create exactly 2 timers
	t1 := clock.NewTimer(1 * time.Hour)
	t2 := clock.NewTimer(2 * time.Hour)

	if clock.TimerCount() != 2 {
		t.Fatalf("TimerCount() = %d, want 2", clock.TimerCount())
	}

	// Stop first timer - after this, only 1 timer remains (at index 0)
	t1.Stop()

	if clock.TimerCount() != 1 {
		t.Errorf("After stop, TimerCount() = %d, want 1", clock.TimerCount())
	}

	// The stopped timer now has a stale heapIndex that's >= current heap length
	// If the mutation (>= becomes >) were present, this could cause issues
	// Try to stop the already-stopped timer again
	wasActive := t1.Stop()
	if wasActive {
		t.Error("Second Stop() should return false")
	}

	// Heap should still be intact
	if clock.TimerCount() != 1 {
		t.Errorf("After second stop attempt, TimerCount() = %d, want 1", clock.TimerCount())
	}

	// Clean up
	t2.Stop()
	if clock.TimerCount() != 0 {
		t.Errorf("After all stops, TimerCount() = %d, want 0", clock.TimerCount())
	}
}

// TestMultipleImmediateTimersWithNormalTimers verifies heap integrity when
// mixing immediate timers (heapIndex=-1) with normal timers.
func TestMultipleImmediateTimersWithNormalTimers(t *testing.T) {
	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)

	// Create alternating immediate and normal timers
	immediate1 := clock.NewTimer(0)
	normal1 := clock.NewTimer(1 * time.Hour)
	immediate2 := clock.NewTimer(-1 * time.Second)
	normal2 := clock.NewTimer(2 * time.Hour)
	immediate3 := clock.NewTimer(0)

	// Consume immediate timers
	<-immediate1.C()
	<-immediate2.C()
	<-immediate3.C()

	// Only normal timers should be in heap
	if clock.TimerCount() != 2 {
		t.Errorf("TimerCount() = %d, want 2 (only normal timers)", clock.TimerCount())
	}

	// Stopping immediate timers should all return false and not affect heap
	for i, timer := range []Timer{immediate1, immediate2, immediate3} {
		if timer.Stop() {
			t.Errorf("Stop() on immediate timer %d should return false", i+1)
		}
	}

	// Heap should still have exactly 2 timers
	if clock.TimerCount() != 2 {
		t.Errorf("After stopping immediates, TimerCount() = %d, want 2", clock.TimerCount())
	}

	// Normal timers should still work
	normal1.Stop()
	if clock.TimerCount() != 1 {
		t.Errorf("After stopping normal1, TimerCount() = %d, want 1", clock.TimerCount())
	}

	normal2.Stop()
	if clock.TimerCount() != 0 {
		t.Errorf("After stopping normal2, TimerCount() = %d, want 0", clock.TimerCount())
	}
}
