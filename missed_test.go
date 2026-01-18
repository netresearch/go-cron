package cron

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMissedPolicyString(t *testing.T) {
	tests := []struct {
		policy MissedPolicy
		want   string
	}{
		{MissedSkip, "Skip"},
		{MissedRunOnce, "RunOnce"},
		{MissedRunAll, "RunAll"},
		{MissedPolicy(99), "Unknown"},
	}

	for _, tt := range tests {
		if got := tt.policy.String(); got != tt.want {
			t.Errorf("MissedPolicy(%d).String() = %q, want %q", tt.policy, got, tt.want)
		}
	}
}

func TestMissedSkipPolicy(t *testing.T) {
	// MissedSkip is the default - no catch-up should occur
	now := time.Date(2026, 1, 18, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(now)

	c := New(
		WithClock(clock),
		WithParser(secondParser),
	)

	var calls int64

	// Job was supposed to run at 9:00, but we're starting at 10:00
	// With MissedSkip (default), it should NOT run catch-up
	lastRun := time.Date(2026, 1, 18, 8, 0, 0, 0, time.UTC) // Last ran at 8:00
	_, err := c.AddFunc("0 0 9 * * *", func() {
		atomic.AddInt64(&calls, 1)
	},
		WithPrev(lastRun),
		WithMissedPolicy(MissedSkip),
	)
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}

	c.Start()
	defer c.Stop()

	// Give some time for any catch-up to occur (it shouldn't)
	time.Sleep(50 * time.Millisecond)

	if atomic.LoadInt64(&calls) != 0 {
		t.Errorf("MissedSkip should not run catch-up, got %d calls", calls)
	}
}

func TestMissedRunOncePolicy(t *testing.T) {
	// MissedRunOnce should run the job once for the most recent missed execution
	now := time.Date(2026, 1, 18, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(now)

	var calls int64
	var lastScheduledTime time.Time
	var mu sync.Mutex

	// Create single Cron instance with observability hooks
	c := New(
		WithClock(clock),
		WithParser(secondParser),
		WithObservability(ObservabilityHooks{
			OnJobStart: func(entryID EntryID, name string, scheduledTime time.Time) {
				mu.Lock()
				lastScheduledTime = scheduledTime
				mu.Unlock()
			},
		}),
	)

	// Job runs every hour at :00. Last ran at 7:00, now it's 10:00
	// Missed: 8:00, 9:00
	// With MissedRunOnce, should only run once for 9:00 (most recent)
	lastRun := time.Date(2026, 1, 18, 7, 0, 0, 0, time.UTC)
	_, err := c.AddFunc("0 0 * * * *", func() {
		atomic.AddInt64(&calls, 1)
	},
		WithPrev(lastRun),
		WithMissedPolicy(MissedRunOnce),
	)
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}

	c.Start()
	defer c.Stop()

	// Wait for catch-up to run
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt64(&calls) != 1 {
		t.Errorf("MissedRunOnce should run exactly once, got %d calls", calls)
	}

	mu.Lock()
	scheduled := lastScheduledTime
	mu.Unlock()

	// The scheduled time should be 9:00 (most recent missed)
	expected := time.Date(2026, 1, 18, 9, 0, 0, 0, time.UTC)
	if !scheduled.Equal(expected) {
		t.Errorf("MissedRunOnce scheduled time = %v, want %v", scheduled, expected)
	}
}

func TestMissedRunAllPolicy(t *testing.T) {
	// MissedRunAll should run the job for every missed execution
	now := time.Date(2026, 1, 18, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(now)

	var calls int64
	var scheduledTimes []time.Time
	var mu sync.Mutex

	c := New(
		WithClock(clock),
		WithParser(secondParser),
		WithObservability(ObservabilityHooks{
			OnJobStart: func(entryID EntryID, name string, scheduledTime time.Time) {
				mu.Lock()
				scheduledTimes = append(scheduledTimes, scheduledTime)
				mu.Unlock()
			},
		}),
	)

	// Job runs every hour at :00. Last ran at 7:00, now it's 10:00
	// Missed: 8:00, 9:00
	// With MissedRunAll, should run twice
	lastRun := time.Date(2026, 1, 18, 7, 0, 0, 0, time.UTC)
	_, err := c.AddFunc("0 0 * * * *", func() {
		atomic.AddInt64(&calls, 1)
	},
		WithPrev(lastRun),
		WithMissedPolicy(MissedRunAll),
	)
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}

	c.Start()
	defer c.Stop()

	// Wait for catch-up to run
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt64(&calls) != 2 {
		t.Errorf("MissedRunAll should run 2 times, got %d calls", calls)
	}

	mu.Lock()
	times := scheduledTimes
	mu.Unlock()

	// Should have scheduled times for 8:00 and 9:00 (order may vary due to goroutines)
	if len(times) != 2 {
		t.Fatalf("Expected 2 scheduled times, got %d", len(times))
	}

	expected1 := time.Date(2026, 1, 18, 8, 0, 0, 0, time.UTC)
	expected2 := time.Date(2026, 1, 18, 9, 0, 0, 0, time.UTC)

	// Check both times are present (order may vary)
	hasExpected1 := times[0].Equal(expected1) || times[1].Equal(expected1)
	hasExpected2 := times[0].Equal(expected2) || times[1].Equal(expected2)

	if !hasExpected1 {
		t.Errorf("Expected scheduled time %v not found in %v", expected1, times)
	}
	if !hasExpected2 {
		t.Errorf("Expected scheduled time %v not found in %v", expected2, times)
	}
}

func TestMissedGracePeriod(t *testing.T) {
	// With grace period, only recent missed executions should be caught up
	now := time.Date(2026, 1, 18, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(now)

	var calls int64
	var scheduledTimes []time.Time
	var mu sync.Mutex

	c := New(
		WithClock(clock),
		WithParser(secondParser),
		WithObservability(ObservabilityHooks{
			OnJobStart: func(entryID EntryID, name string, scheduledTime time.Time) {
				mu.Lock()
				scheduledTimes = append(scheduledTimes, scheduledTime)
				mu.Unlock()
			},
		}),
	)

	// Job runs every hour at :00. Last ran at 7:00, now it's 10:00
	// Missed: 8:00 (2h ago), 9:00 (1h ago)
	// With 90 minute grace period, only 9:00 should be caught up
	lastRun := time.Date(2026, 1, 18, 7, 0, 0, 0, time.UTC)
	_, err := c.AddFunc("0 0 * * * *", func() {
		atomic.AddInt64(&calls, 1)
	},
		WithPrev(lastRun),
		WithMissedPolicy(MissedRunAll),
		WithMissedGracePeriod(90*time.Minute),
	)
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}

	c.Start()
	defer c.Stop()

	// Wait for catch-up to run
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt64(&calls) != 1 {
		t.Errorf("With 90min grace period, should run 1 time, got %d calls", calls)
	}

	mu.Lock()
	times := scheduledTimes
	mu.Unlock()

	// Should only have 9:00
	if len(times) != 1 {
		t.Fatalf("Expected 1 scheduled time, got %d", len(times))
	}

	expected := time.Date(2026, 1, 18, 9, 0, 0, 0, time.UTC)
	if !times[0].Equal(expected) {
		t.Errorf("Scheduled time = %v, want %v", times[0], expected)
	}
}

func TestMissedNoPrevTime(t *testing.T) {
	// Without Prev time, no catch-up should occur even with MissedRunAll
	now := time.Date(2026, 1, 18, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(now)

	c := New(
		WithClock(clock),
		WithParser(secondParser),
	)

	var calls int64

	// No WithPrev() - cannot know what was missed
	_, err := c.AddFunc("0 0 * * * *", func() {
		atomic.AddInt64(&calls, 1)
	},
		WithMissedPolicy(MissedRunAll),
	)
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}

	c.Start()
	defer c.Stop()

	// Give some time for any catch-up to occur (it shouldn't)
	time.Sleep(50 * time.Millisecond)

	if atomic.LoadInt64(&calls) != 0 {
		t.Errorf("Without Prev time, no catch-up should occur, got %d calls", calls)
	}
}

func TestMissedMaxRunsLimit(t *testing.T) {
	// MissedRunAll should be capped at maxMissedRuns (100)
	now := time.Date(2026, 1, 18, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(now)

	var calls int64

	c := New(
		WithClock(clock),
		WithParser(secondParser),
	)

	// Job runs every minute. Last ran 200 minutes ago
	// Should only run 100 times (capped)
	lastRun := now.Add(-200 * time.Minute)
	_, err := c.AddFunc("0 * * * * *", func() {
		atomic.AddInt64(&calls, 1)
	},
		WithPrev(lastRun),
		WithMissedPolicy(MissedRunAll),
	)
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}

	c.Start()
	defer c.Stop()

	// Wait for catch-up to run
	time.Sleep(200 * time.Millisecond)

	count := atomic.LoadInt64(&calls)
	if count > maxMissedRuns {
		t.Errorf("MissedRunAll should be capped at %d, got %d calls", maxMissedRuns, count)
	}
	if count != maxMissedRuns {
		t.Errorf("Expected exactly %d calls, got %d", maxMissedRuns, count)
	}
}

func TestMissedNoMissedExecutions(t *testing.T) {
	// If Prev is recent and nothing was missed, no catch-up should occur
	now := time.Date(2026, 1, 18, 10, 30, 0, 0, time.UTC)
	clock := NewFakeClock(now)

	c := New(
		WithClock(clock),
		WithParser(secondParser),
	)

	var calls int64

	// Job runs every hour at :00. Last ran at 10:00, now it's 10:30
	// Nothing was missed (next run is 11:00)
	lastRun := time.Date(2026, 1, 18, 10, 0, 0, 0, time.UTC)
	_, err := c.AddFunc("0 0 * * * *", func() {
		atomic.AddInt64(&calls, 1)
	},
		WithPrev(lastRun),
		WithMissedPolicy(MissedRunAll),
	)
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}

	c.Start()
	defer c.Stop()

	// Give some time for any catch-up to occur (it shouldn't)
	time.Sleep(50 * time.Millisecond)

	if atomic.LoadInt64(&calls) != 0 {
		t.Errorf("No missed executions, should have 0 calls, got %d", calls)
	}
}

func TestMissedDynamicAdd(t *testing.T) {
	// Test that missed runs are processed when adding jobs dynamically
	now := time.Date(2026, 1, 18, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(now)

	c := New(
		WithClock(clock),
		WithParser(secondParser),
	)

	c.Start()
	defer c.Stop()

	var calls int64

	// Add job after scheduler is running
	// Job runs every hour, last ran at 8:00
	lastRun := time.Date(2026, 1, 18, 8, 0, 0, 0, time.UTC)
	_, err := c.AddFunc("0 0 * * * *", func() {
		atomic.AddInt64(&calls, 1)
	},
		WithPrev(lastRun),
		WithMissedPolicy(MissedRunOnce),
	)
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}

	// Wait for catch-up to run
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt64(&calls) != 1 {
		t.Errorf("Dynamic add should trigger catch-up, got %d calls", calls)
	}
}

func TestCalculateMissedRuns(t *testing.T) {
	now := time.Date(2026, 1, 18, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(now)

	c := New(
		WithClock(clock),
		WithParser(secondParser),
	)

	tests := []struct {
		name         string
		prev         time.Time
		policy       MissedPolicy
		gracePeriod  time.Duration
		wantCount    int
		wantSchedule string // cron expression
	}{
		{
			name:         "no prev time",
			prev:         time.Time{},
			policy:       MissedRunAll,
			wantCount:    0,
			wantSchedule: "0 0 * * * *",
		},
		{
			name:         "skip policy",
			prev:         now.Add(-2 * time.Hour),
			policy:       MissedSkip,
			wantCount:    0,
			wantSchedule: "0 0 * * * *",
		},
		{
			name:         "two missed hourly",
			prev:         now.Add(-3 * time.Hour),
			policy:       MissedRunAll,
			wantCount:    2,
			wantSchedule: "0 0 * * * *", // at :00:00
		},
		{
			name:         "grace period filters old",
			prev:         now.Add(-3 * time.Hour),
			policy:       MissedRunAll,
			gracePeriod:  90 * time.Minute,
			wantCount:    1,
			wantSchedule: "0 0 * * * *",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schedule, err := secondParser.Parse(tt.wantSchedule)
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			entry := &Entry{
				Schedule:          schedule,
				Prev:              tt.prev,
				MissedPolicy:      tt.policy,
				MissedGracePeriod: tt.gracePeriod,
			}

			missed := c.calculateMissedRuns(entry, now)
			if len(missed) != tt.wantCount {
				t.Errorf("calculateMissedRuns() returned %d runs, want %d", len(missed), tt.wantCount)
			}
		})
	}
}

func TestWithMissedPolicyOption(t *testing.T) {
	c := New(WithParser(secondParser))

	id, err := c.AddFunc("* * * * * *", func() {},
		WithMissedPolicy(MissedRunOnce),
	)
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}

	entry := c.Entry(id)
	if entry.MissedPolicy != MissedRunOnce {
		t.Errorf("MissedPolicy = %v, want MissedRunOnce", entry.MissedPolicy)
	}
}

func TestWithMissedGracePeriodOption(t *testing.T) {
	c := New(WithParser(secondParser))

	id, err := c.AddFunc("* * * * * *", func() {},
		WithMissedGracePeriod(2*time.Hour),
	)
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}

	entry := c.Entry(id)
	if entry.MissedGracePeriod != 2*time.Hour {
		t.Errorf("MissedGracePeriod = %v, want 2h", entry.MissedGracePeriod)
	}
}

func TestMissedWithName(t *testing.T) {
	// Ensure WithName works correctly with missed policy
	// (Entry.Name is set, verifiable via Entry() method)
	now := time.Date(2026, 1, 18, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(now)

	var calls int64

	c := New(
		WithClock(clock),
		WithParser(secondParser),
	)

	// Last ran at 8:00, now it's 10:00 - missed the 9:00 run
	lastRun := time.Date(2026, 1, 18, 8, 0, 0, 0, time.UTC)
	id, err := c.AddFunc("0 0 * * * *", func() {
		atomic.AddInt64(&calls, 1)
	},
		WithName("hourly-report"),
		WithPrev(lastRun),
		WithMissedPolicy(MissedRunOnce),
	)
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}

	// Verify Entry.Name is set correctly
	entry := c.Entry(id)
	if entry.Name != "hourly-report" {
		t.Errorf("Entry.Name = %q, want 'hourly-report'", entry.Name)
	}

	c.Start()
	defer c.Stop()

	time.Sleep(100 * time.Millisecond)

	// Verify catch-up ran (missed 9:00)
	if atomic.LoadInt64(&calls) != 1 {
		t.Errorf("Expected 1 catch-up call, got %d", calls)
	}
}

func TestMissedGracePeriodZero(t *testing.T) {
	// Zero grace period means all missed runs are eligible (within safety limits)
	now := time.Date(2026, 1, 18, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(now)

	var calls int64

	c := New(
		WithClock(clock),
		WithParser(secondParser),
	)

	// Job runs every hour. Last ran 5 hours ago
	lastRun := now.Add(-5 * time.Hour)
	_, err := c.AddFunc("0 0 * * * *", func() {
		atomic.AddInt64(&calls, 1)
	},
		WithPrev(lastRun),
		WithMissedPolicy(MissedRunAll),
		WithMissedGracePeriod(0), // Zero means no limit
	)
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}

	c.Start()
	defer c.Stop()

	time.Sleep(100 * time.Millisecond)

	// Should have 4 missed runs (6:00, 7:00, 8:00, 9:00)
	if atomic.LoadInt64(&calls) != 4 {
		t.Errorf("Expected 4 catch-up runs, got %d", calls)
	}
}

func TestMissedPolicyValid(t *testing.T) {
	tests := []struct {
		policy MissedPolicy
		want   bool
	}{
		{MissedSkip, true},
		{MissedRunOnce, true},
		{MissedRunAll, true},
		{MissedPolicy(-1), false},
		{MissedPolicy(99), false},
	}

	for _, tt := range tests {
		if got := tt.policy.Valid(); got != tt.want {
			t.Errorf("MissedPolicy(%d).Valid() = %v, want %v", tt.policy, got, tt.want)
		}
	}
}

func TestMissedInvalidPolicySkipsCatchup(t *testing.T) {
	// Invalid MissedPolicy values should skip catch-up gracefully
	now := time.Date(2026, 1, 18, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(now)

	c := New(
		WithClock(clock),
		WithParser(secondParser),
	)

	var calls int64

	// Use invalid policy value
	lastRun := time.Date(2026, 1, 18, 8, 0, 0, 0, time.UTC)
	_, err := c.AddFunc("0 0 * * * *", func() {
		atomic.AddInt64(&calls, 1)
	},
		WithPrev(lastRun),
		WithMissedPolicy(MissedPolicy(99)), // Invalid policy
	)
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}

	c.Start()
	defer c.Stop()

	time.Sleep(50 * time.Millisecond)

	// Invalid policy should not trigger catch-up
	if atomic.LoadInt64(&calls) != 0 {
		t.Errorf("Invalid policy should not trigger catch-up, got %d calls", calls)
	}
}

func TestMissedRunOnceSkipsForRunOnceJobs(t *testing.T) {
	// Run-once jobs should skip catch-up to avoid duplicate runs
	now := time.Date(2026, 1, 18, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(now)

	c := New(
		WithClock(clock),
		WithParser(secondParser),
	)

	var calls int64

	// Job with both WithRunOnce and WithMissedPolicy
	lastRun := time.Date(2026, 1, 18, 8, 0, 0, 0, time.UTC)
	_, err := c.AddFunc("0 0 * * * *", func() {
		atomic.AddInt64(&calls, 1)
	},
		WithPrev(lastRun),
		WithMissedPolicy(MissedRunOnce),
		WithRunOnce(),
	)
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}

	c.Start()
	defer c.Stop()

	time.Sleep(50 * time.Millisecond)

	// Run-once jobs should not trigger catch-up
	if atomic.LoadInt64(&calls) != 0 {
		t.Errorf("Run-once jobs should skip catch-up, got %d calls", calls)
	}
}

func TestMissedUpdatesPrevAfterCatchup(t *testing.T) {
	// Entry.Prev should be updated after catch-up to prevent duplicate catch-ups
	now := time.Date(2026, 1, 18, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(now)

	c := New(
		WithClock(clock),
		WithParser(secondParser),
	)

	var calls int64

	// Job runs every hour, last ran at 7:00
	// Missed: 8:00, 9:00
	lastRun := time.Date(2026, 1, 18, 7, 0, 0, 0, time.UTC)
	id, err := c.AddFunc("0 0 * * * *", func() {
		atomic.AddInt64(&calls, 1)
	},
		WithPrev(lastRun),
		WithMissedPolicy(MissedRunOnce),
	)
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}

	c.Start()
	defer c.Stop()

	time.Sleep(100 * time.Millisecond)

	// Should have run once for catch-up
	if atomic.LoadInt64(&calls) != 1 {
		t.Errorf("Expected 1 catch-up call, got %d", calls)
	}

	// Entry.Prev should be updated to the most recent caught-up time (9:00)
	entry := c.Entry(id)
	expectedPrev := time.Date(2026, 1, 18, 9, 0, 0, 0, time.UTC)
	if !entry.Prev.Equal(expectedPrev) {
		t.Errorf("Entry.Prev = %v, want %v", entry.Prev, expectedPrev)
	}
}

func TestMissedRunAllUpdatesPrevAfterCatchup(t *testing.T) {
	// Entry.Prev should be updated to the most recent catch-up time for MissedRunAll
	now := time.Date(2026, 1, 18, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(now)

	c := New(
		WithClock(clock),
		WithParser(secondParser),
	)

	var calls int64

	// Job runs every hour, last ran at 7:00
	lastRun := time.Date(2026, 1, 18, 7, 0, 0, 0, time.UTC)
	id, err := c.AddFunc("0 0 * * * *", func() {
		atomic.AddInt64(&calls, 1)
	},
		WithPrev(lastRun),
		WithMissedPolicy(MissedRunAll),
	)
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}

	c.Start()
	defer c.Stop()

	time.Sleep(100 * time.Millisecond)

	// Should have run twice for catch-up (8:00, 9:00)
	if atomic.LoadInt64(&calls) != 2 {
		t.Errorf("Expected 2 catch-up calls, got %d", calls)
	}

	// Entry.Prev should be updated to the most recent caught-up time (9:00)
	entry := c.Entry(id)
	expectedPrev := time.Date(2026, 1, 18, 9, 0, 0, 0, time.UTC)
	if !entry.Prev.Equal(expectedPrev) {
		t.Errorf("Entry.Prev = %v, want %v", entry.Prev, expectedPrev)
	}
}

func TestHandleMissedRunsWithInvalidPolicy(t *testing.T) {
	// Test the defensive default case in handleMissedRuns
	// This case is normally unreachable because calculateMissedRuns validates,
	// but we test it directly to ensure defensive code coverage
	c := New()

	var calls int64
	entry := &Entry{
		ID:           1,
		MissedPolicy: MissedPolicy(99), // Invalid policy
		Job:          FuncJob(func() { atomic.AddInt64(&calls, 1) }),
		WrappedJob:   FuncJob(func() { atomic.AddInt64(&calls, 1) }),
	}

	missed := []time.Time{time.Now()}

	// Should not panic, should log and return without executing job
	c.handleMissedRuns(entry, missed)

	// Job should NOT have been executed for invalid policy
	if atomic.LoadInt64(&calls) != 0 {
		t.Errorf("Expected 0 calls for invalid policy, got %d", calls)
	}
}
