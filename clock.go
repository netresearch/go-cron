package cron

import (
	"container/heap"
	"sync"
	"time"
)

// Clock provides time-related operations that can be mocked for testing.
// This interface allows deterministic testing of scheduled jobs by controlling
// time advancement and timer firing.
type Clock interface {
	Now() time.Time
	NewTimer(d time.Duration) Timer
}

// Timer represents a single event timer, similar to time.Timer.
// It provides the same core operations needed for scheduling.
type Timer interface {
	// C returns the channel on which the timer fires.
	C() <-chan time.Time
	// Stop prevents the Timer from firing. Returns true if the call stops
	// the timer, false if the timer has already expired or been stopped.
	Stop() bool
	// Reset changes the timer to expire after duration d.
	// Returns true if the timer had been active, false if it had expired or been stopped.
	Reset(d time.Duration) bool
}

// RealClock implements Clock using the standard time package.
// This is the default clock used in production.
type RealClock struct{}

// Now returns the current time.
func (RealClock) Now() time.Time {
	return time.Now()
}

// NewTimer creates a new Timer that will send the current time
// on its channel after at least duration d.
func (RealClock) NewTimer(d time.Duration) Timer {
	return &realTimer{timer: time.NewTimer(d)}
}

// realTimer wraps time.Timer to implement the Timer interface.
type realTimer struct {
	timer *time.Timer
}

func (r *realTimer) C() <-chan time.Time {
	return r.timer.C
}

func (r *realTimer) Stop() bool {
	return r.timer.Stop()
}

func (r *realTimer) Reset(d time.Duration) bool {
	return r.timer.Reset(d)
}

// FakeClock provides a controllable clock for testing.
// It allows advancing time manually and fires timers deterministically.
type FakeClock struct {
	mu      sync.Mutex
	now     time.Time
	timers  timerHeap
	waiters []chan struct{}
}

// NewFakeClock creates a new FakeClock initialized to the given time.
func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{
		now:    t,
		timers: make(timerHeap, 0),
	}
}

// Now returns the fake clock's current time.
func (f *FakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// NewTimer creates a fake timer that fires when the clock advances past its deadline.
func (f *FakeClock) NewTimer(d time.Duration) Timer {
	f.mu.Lock()
	defer f.mu.Unlock()

	t := &fakeTimer{
		clock:     f,
		deadline:  f.now.Add(d),
		ch:        make(chan time.Time, 1),
		stopped:   false,
		heapIndex: -1, // -1 means not in heap; immediate timers (d<=0) never enter heap
	}

	if d <= 0 {
		// Fire immediately for non-positive duration
		t.ch <- f.now
	} else {
		heap.Push(&f.timers, t)
		f.notifyWaiters()
	}

	return t
}

// Set sets the fake clock to the specified time.
// If the new time is after the current time, fires any timers
// whose deadlines fall between the old and new times.
func (f *FakeClock) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.now = t
	f.fireExpiredTimers()
}

// Advance moves the fake clock forward by the specified duration
// and fires any timers whose deadlines have passed.
func (f *FakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.now = f.now.Add(d)
	f.fireExpiredTimers()
}

// BlockUntil blocks until at least n timers are waiting on the clock.
// This is useful for synchronizing tests with timer creation.
func (f *FakeClock) BlockUntil(n int) {
	f.mu.Lock()

	if len(f.timers) >= n {
		f.mu.Unlock()
		return
	}

	waiter := make(chan struct{})
	f.waiters = append(f.waiters, waiter)
	f.mu.Unlock()

	for {
		<-waiter
		f.mu.Lock()
		if len(f.timers) >= n {
			f.mu.Unlock()
			return
		}
		waiter = make(chan struct{})
		f.waiters = append(f.waiters, waiter)
		f.mu.Unlock()
	}
}

// TimerCount returns the number of active timers.
// Useful for test assertions.
func (f *FakeClock) TimerCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.timers)
}

// fireExpiredTimers fires all timers whose deadline has passed.
// Must be called with f.mu held.
func (f *FakeClock) fireExpiredTimers() {
	for len(f.timers) > 0 && !f.timers[0].deadline.After(f.now) {
		t := heap.Pop(&f.timers).(*fakeTimer) //nolint:forcetypeassert,errcheck // heap.Interface contract guarantees type
		if !t.stopped {
			select {
			case t.ch <- f.now:
			default:
				// Channel full, timer already fired
			}
		}
	}
}

// notifyWaiters wakes up any goroutines waiting in BlockUntil.
// Must be called with f.mu held.
func (f *FakeClock) notifyWaiters() {
	for _, w := range f.waiters {
		select {
		case w <- struct{}{}:
		default:
		}
	}
	f.waiters = nil
}

// removeTimer removes a timer from the heap using O(log n) indexed removal.
// Must be called with f.mu held.
func (f *FakeClock) removeTimer(t *fakeTimer) bool {
	return f.timers.RemoveTimer(t)
}

// fakeTimer implements Timer for use with FakeClock.
type fakeTimer struct {
	clock     *FakeClock
	deadline  time.Time
	ch        chan time.Time
	stopped   bool
	heapIndex int // position in timerHeap, -1 if not in heap
}

func (t *fakeTimer) C() <-chan time.Time {
	return t.ch
}

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()

	if t.stopped {
		return false
	}

	t.stopped = true
	wasActive := t.clock.removeTimer(t)
	return wasActive
}

func (t *fakeTimer) Reset(d time.Duration) bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()

	wasActive := !t.stopped && t.clock.removeTimer(t)

	t.stopped = false
	t.deadline = t.clock.now.Add(d)

	if d <= 0 {
		select {
		case t.ch <- t.clock.now:
		default:
		}
	} else {
		heap.Push(&t.clock.timers, t)
		t.clock.notifyWaiters()
	}

	return wasActive
}

// timerHeap implements heap.Interface for fakeTimer priority queue.
// Timers are ordered by deadline (earliest first).
type timerHeap []*fakeTimer

func (h timerHeap) Len() int           { return len(h) }
func (h timerHeap) Less(i, j int) bool { return h[i].deadline.Before(h[j].deadline) }

// Swap swaps elements i and j and updates their heap indices.
func (h timerHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].heapIndex = i
	h[j].heapIndex = j
}

// Push adds a timer to the heap and sets its heap index.
func (h *timerHeap) Push(x any) {
	t := x.(*fakeTimer) //nolint:forcetypeassert,errcheck // heap.Interface contract guarantees type
	t.heapIndex = len(*h)
	*h = append(*h, t)
}

// Pop removes and returns the last element, marking it as no longer in the heap.
func (h *timerHeap) Pop() any {
	old := *h
	n := len(old)
	t := old[n-1]
	old[n-1] = nil   // avoid memory leak
	t.heapIndex = -1 // mark as removed
	*h = old[0 : n-1]
	return t
}

// RemoveTimer removes a timer from the heap using its stored heapIndex.
// Returns true if the timer was found and removed.
// This is O(log n) compared to O(n) linear search.
func (h *timerHeap) RemoveTimer(t *fakeTimer) bool {
	idx := t.heapIndex
	if idx < 0 || idx >= len(*h) || (*h)[idx] != t {
		return false
	}
	heap.Remove(h, idx)
	return true
}
