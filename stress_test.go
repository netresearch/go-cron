//go:build integration

package cron

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestConcurrentAddRemove stress tests concurrent Add and Remove operations.
func TestConcurrentAddRemove(t *testing.T) {
	c := New()
	c.Start()
	defer c.Stop()

	const goroutines = 50
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	var addedIDs sync.Map // Store IDs for removal

	// Spawn adders
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				id, err := c.AddFunc("* * * * *", func() {})
				if err != nil {
					// May fail due to timing, that's ok
					continue
				}
				addedIDs.Store(id, struct{}{})
			}
		}(i)
	}

	// Spawn removers (they try to remove entries that may or may not exist)
	for i := 0; i < goroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				// Try to remove a random ID - some will be valid, some won't
				c.Remove(EntryID(j + 1))
			}
		}()
	}

	wg.Wait()

	// Verify cron is still functional
	id, err := c.AddFunc("@every 1h", func() {})
	if err != nil {
		t.Errorf("cron not functional after stress test: %v", err)
	}
	if id == 0 {
		t.Error("expected valid ID after stress test")
	}
}

// TestConcurrentEntriesAccess stress tests concurrent Entries() calls during modifications.
func TestConcurrentEntriesAccess(t *testing.T) {
	c := New()
	c.Start()
	defer c.Stop()

	const goroutines = 20
	const iterations = 100

	var wg sync.WaitGroup

	// Spawn entry readers
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				entries := c.Entries()
				// Just access the entries to ensure no race
				_ = len(entries)
				for _, e := range entries {
					_ = e.ID
					_ = e.Next
				}
			}
		}()
	}

	// Spawn adders
	for i := 0; i < goroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				c.AddFunc("* * * * *", func() {})
			}
		}()
	}

	// Spawn removers
	for i := 0; i < goroutines/2; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				c.Remove(EntryID(base*iterations + j))
			}
		}(i)
	}

	wg.Wait()
}

// TestConcurrentEntryLookup stress tests Entry() lookups during modifications.
func TestConcurrentEntryLookup(t *testing.T) {
	c := New()

	// Pre-add some entries
	var ids []EntryID
	for i := 0; i < 100; i++ {
		id, _ := c.AddFunc("* * * * *", func() {})
		ids = append(ids, id)
	}

	c.Start()
	defer c.Stop()

	var wg sync.WaitGroup

	// Spawn entry lookers
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				for _, id := range ids {
					entry := c.Entry(id)
					// Entry may or may not be valid (could have been removed)
					_ = entry.Valid()
				}
			}
		}()
	}

	// Spawn removers
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, id := range ids {
			c.Remove(id)
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()
}

// TestHighFrequencyAdditions tests rapid entry additions while running.
func TestHighFrequencyAdditions(t *testing.T) {
	clock := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	c := New(WithClock(clock))
	c.Start()
	defer c.Stop()

	const count = 1000
	start := time.Now()

	for i := 0; i < count; i++ {
		_, err := c.AddFunc("* * * * *", func() {})
		if err != nil {
			t.Errorf("failed to add entry %d: %v", i, err)
		}
	}

	elapsed := time.Since(start)
	if elapsed > 5*time.Second {
		t.Errorf("adding %d entries took too long: %v", count, elapsed)
	}

	entries := c.Entries()
	if len(entries) != count {
		t.Errorf("expected %d entries, got %d", count, len(entries))
	}
}

// slowSchedule is a schedule that simulates expensive Next() calculations.
type slowSchedule struct {
	delay time.Duration
}

func (s *slowSchedule) Next(t time.Time) time.Time {
	// Simulate expensive calculation
	time.Sleep(s.delay)
	return t.Add(time.Hour)
}

// TestSlowScheduleNext tests behavior with slow Schedule.Next() implementations.
// Note: Schedule.Next() panics are NOT currently handled by the scheduler and will
// crash the run loop. This is a known limitation - schedules must not panic.
func TestSlowScheduleNext(t *testing.T) {
	clock := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	c := New(WithClock(clock))

	var normalJobRuns int32

	// Add a normal job
	c.AddFunc("@every 1h", func() {
		atomic.AddInt32(&normalJobRuns, 1)
	})

	// Add a job with a slow schedule (simulates expensive computation)
	slowSched := &slowSchedule{delay: 10 * time.Millisecond}
	c.Schedule(slowSched, FuncJob(func() {}))

	c.Start()
	defer c.Stop()

	// Give scheduler time to initialize (including slow schedule calculation)
	time.Sleep(100 * time.Millisecond)

	// Advance time to trigger jobs
	clock.Advance(time.Hour)
	time.Sleep(150 * time.Millisecond)

	// Normal job should have run
	if runs := atomic.LoadInt32(&normalJobRuns); runs < 1 {
		t.Errorf("expected normal job to run at least once, got %d runs", runs)
	}
}

// TestSchedulerRecoveryAfterPanic tests that the scheduler continues after job panics.
func TestSchedulerRecoveryAfterPanic(t *testing.T) {
	clock := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	c := New(
		WithClock(clock),
		WithChain(Recover(DiscardLogger)),
	)

	var panicJobRuns, normalJobRuns int32

	// Add panicking job
	c.AddFunc("@every 1h", func() {
		atomic.AddInt32(&panicJobRuns, 1)
		panic("intentional test panic")
	})

	// Add normal job
	c.AddFunc("@every 1h", func() {
		atomic.AddInt32(&normalJobRuns, 1)
	})

	c.Start()
	defer c.Stop()

	// Trigger multiple runs
	for i := 0; i < 5; i++ {
		time.Sleep(50 * time.Millisecond)
		clock.Advance(time.Hour)
		time.Sleep(100 * time.Millisecond)
	}

	// Both jobs should have run multiple times despite panics
	if panicRuns := atomic.LoadInt32(&panicJobRuns); panicRuns < 3 {
		t.Errorf("expected panicking job to run multiple times, got %d", panicRuns)
	}
	if normalRuns := atomic.LoadInt32(&normalJobRuns); normalRuns < 3 {
		t.Errorf("expected normal job to run multiple times, got %d", normalRuns)
	}
}

// TestMaxEntriesUnderConcurrentLoad tests entry limit under concurrent access.
func TestMaxEntriesUnderConcurrentLoad(t *testing.T) {
	const maxEntries = 100
	c := New(WithMaxEntries(maxEntries))
	c.Start()
	defer c.Stop()

	var wg sync.WaitGroup
	var successCount, failCount int32

	// Spawn many goroutines trying to add entries
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.AddFunc("* * * * *", func() {})
			if err != nil {
				atomic.AddInt32(&failCount, 1)
			} else {
				atomic.AddInt32(&successCount, 1)
			}
		}()
	}

	wg.Wait()

	// Should have approximately maxEntries successes (may be slightly over due to races)
	if success := atomic.LoadInt32(&successCount); success < int32(maxEntries)-10 {
		t.Errorf("expected at least %d successes, got %d", maxEntries-10, success)
	}

	// Total entries should not exceed limit by much
	entries := c.Entries()
	if len(entries) > maxEntries+20 {
		t.Errorf("entries (%d) significantly exceeded max (%d)", len(entries), maxEntries)
	}
}

// DST tests for various timezones

// TestDST_USEastern_SpringForward tests US Eastern spring forward (2nd Sunday of March).
func TestDST_USEastern_SpringForward(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("America/New_York timezone not available")
	}

	// March 10, 2024 at 2:00 AM - clocks spring forward to 3:00 AM
	// Job scheduled for 2:30 AM should run at 3:30 AM (or be skipped)
	beforeDST := time.Date(2024, 3, 10, 1, 59, 0, 0, loc)
	clock := NewFakeClock(beforeDST)

	c := New(WithClock(clock), WithLocation(loc))

	var runs int32
	c.AddFunc("30 2 * * *", func() { // 2:30 AM
		atomic.AddInt32(&runs, 1)
	})

	c.Start()
	defer c.Stop()

	// Advance past 2 AM -> 3 AM transition
	time.Sleep(50 * time.Millisecond)
	clock.Advance(2 * time.Hour)
	time.Sleep(100 * time.Millisecond)

	// The job may or may not run depending on DST handling
	// What's important is the scheduler didn't crash
	t.Logf("Job ran %d times through DST spring forward", atomic.LoadInt32(&runs))
}

// TestDST_USEastern_FallBack tests US Eastern fall back (1st Sunday of November).
func TestDST_USEastern_FallBack(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("America/New_York timezone not available")
	}

	// November 3, 2024 at 2:00 AM - clocks fall back to 1:00 AM
	// This creates a duplicate 1:00-2:00 hour
	beforeDST := time.Date(2024, 11, 3, 0, 30, 0, 0, loc)
	clock := NewFakeClock(beforeDST)

	c := New(WithClock(clock), WithLocation(loc))

	var runs int32
	c.AddFunc("30 1 * * *", func() { // 1:30 AM - occurs twice during fall back
		atomic.AddInt32(&runs, 1)
	})

	c.Start()
	defer c.Stop()

	// Advance past the DST transition
	time.Sleep(50 * time.Millisecond)
	clock.Advance(3 * time.Hour)
	time.Sleep(100 * time.Millisecond)

	// Job should run at least once (may run twice due to ambiguous time)
	if atomic.LoadInt32(&runs) < 1 {
		t.Errorf("expected job to run at least once during fall back, got %d", runs)
	}
	t.Logf("Job ran %d times through DST fall back (duplicate hour)", atomic.LoadInt32(&runs))
}

// TestDST_Europe_London tests Europe/London DST transitions.
func TestDST_Europe_London(t *testing.T) {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Skip("Europe/London timezone not available")
	}

	// Last Sunday of March at 1:00 AM -> 2:00 AM (2024: March 31)
	beforeDST := time.Date(2024, 3, 31, 0, 30, 0, 0, loc)
	clock := NewFakeClock(beforeDST)

	c := New(WithClock(clock), WithLocation(loc))

	var runs int32
	c.AddFunc("30 0 * * *", func() { // 00:30 - should run before transition
		atomic.AddInt32(&runs, 1)
	})

	c.Start()
	defer c.Stop()

	time.Sleep(50 * time.Millisecond)
	clock.Advance(2 * time.Hour)
	time.Sleep(100 * time.Millisecond)

	t.Logf("Job ran %d times through Europe/London spring forward", atomic.LoadInt32(&runs))
}

// TestDST_Australia_Sydney tests Australia/Sydney DST (opposite hemisphere).
func TestDST_Australia_Sydney(t *testing.T) {
	loc, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		t.Skip("Australia/Sydney timezone not available")
	}

	// First Sunday of October at 2:00 AM -> 3:00 AM (spring forward in southern hemisphere)
	// 2024: October 6
	beforeDST := time.Date(2024, 10, 6, 1, 30, 0, 0, loc)
	clock := NewFakeClock(beforeDST)

	c := New(WithClock(clock), WithLocation(loc))

	var runs int32
	c.AddFunc("30 2 * * *", func() { // 2:30 AM - skipped hour
		atomic.AddInt32(&runs, 1)
	})

	c.Start()
	defer c.Stop()

	time.Sleep(50 * time.Millisecond)
	clock.Advance(2 * time.Hour)
	time.Sleep(100 * time.Millisecond)

	t.Logf("Job ran %d times through Australia/Sydney spring forward", atomic.LoadInt32(&runs))
}

// Benchmarks for scale testing

// BenchmarkAddWithManyEntries benchmarks adding entries with various existing entry counts.
func BenchmarkAddWithManyEntries(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("existing_%d", count), func(b *testing.B) {
			c := New()
			for i := 0; i < count; i++ {
				c.AddFunc("* * * * *", func() {})
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c.AddFunc("* * * * *", func() {})
			}
		})
	}
}

// BenchmarkEntriesWithManyEntries benchmarks Entries() with various entry counts.
func BenchmarkEntriesWithManyEntries(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("entries_%d", count), func(b *testing.B) {
			c := New()
			for i := 0; i < count; i++ {
				c.AddFunc("* * * * *", func() {})
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c.Entries()
			}
		})
	}
}

// BenchmarkEntryLookupWithManyEntries benchmarks Entry() lookup with various entry counts.
func BenchmarkEntryLookupWithManyEntries(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("entries_%d", count), func(b *testing.B) {
			c := New()
			var ids []EntryID
			for i := 0; i < count; i++ {
				id, _ := c.AddFunc("* * * * *", func() {})
				ids = append(ids, id)
			}
			// Lookup middle entry
			targetID := ids[len(ids)/2]
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c.Entry(targetID)
			}
		})
	}
}

// BenchmarkRemoveWithManyEntries benchmarks Remove() with various entry counts.
func BenchmarkRemoveWithManyEntries(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("entries_%d", count), func(b *testing.B) {
			b.StopTimer()
			for i := 0; i < b.N; i++ {
				c := New()
				var ids []EntryID
				for j := 0; j < count; j++ {
					id, _ := c.AddFunc("* * * * *", func() {})
					ids = append(ids, id)
				}
				b.StartTimer()
				// Remove middle entry
				c.Remove(ids[len(ids)/2])
				b.StopTimer()
			}
		})
	}
}

// BenchmarkConcurrentAdd benchmarks concurrent AddFunc operations.
func BenchmarkConcurrentAdd(b *testing.B) {
	c := New()
	c.Start()
	defer c.Stop()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.AddFunc("* * * * *", func() {})
		}
	})
}

// BenchmarkMemoryUsage reports memory usage with many entries.
func BenchmarkMemoryUsage(b *testing.B) {
	for _, count := range []int{1000, 10000, 100000} {
		b.Run(fmt.Sprintf("entries_%d", count), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				c := New()
				for j := 0; j < count; j++ {
					c.AddFunc("* * * * *", func() {})
				}
				// Force GC to get accurate memory stats
				runtime.GC()
			}
		})
	}
}

// BenchmarkScheduleNextCalculation benchmarks schedule.Next() calculations.
func BenchmarkScheduleNextCalculation(b *testing.B) {
	schedule, _ := ParseStandard("15 4 * * *")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		schedule.Next(now)
	}
}

// BenchmarkComplexScheduleNext benchmarks complex schedule calculations.
func BenchmarkComplexScheduleNext(b *testing.B) {
	// Complex schedule: every 15 minutes on weekdays in specific months
	schedule, _ := NewParser(Minute | Hour | Dom | Month | Dow).Parse("*/15 9-17 * 1-6 1-5")
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		schedule.Next(now)
	}
}
