package cron

import (
	"testing"
	"time"
)

func TestConstantDelayNext(t *testing.T) {
	tests := []struct {
		time     string
		delay    time.Duration
		expected string
	}{
		// Simple cases
		{"Mon Jul 9 14:45 2012", 15*time.Minute + 50*time.Nanosecond, "Mon Jul 9 15:00 2012"},
		{"Mon Jul 9 14:59 2012", 15 * time.Minute, "Mon Jul 9 15:14 2012"},
		{"Mon Jul 9 14:59:59 2012", 15 * time.Minute, "Mon Jul 9 15:14:59 2012"},

		// Wrap around hours
		{"Mon Jul 9 15:45 2012", 35 * time.Minute, "Mon Jul 9 16:20 2012"},

		// Wrap around days
		{"Mon Jul 9 23:46 2012", 14 * time.Minute, "Tue Jul 10 00:00 2012"},
		{"Mon Jul 9 23:45 2012", 35 * time.Minute, "Tue Jul 10 00:20 2012"},
		{"Mon Jul 9 23:35:51 2012", 44*time.Minute + 24*time.Second, "Tue Jul 10 00:20:15 2012"},
		{"Mon Jul 9 23:35:51 2012", 25*time.Hour + 44*time.Minute + 24*time.Second, "Thu Jul 11 01:20:15 2012"},

		// Wrap around months
		{"Mon Jul 9 23:35 2012", 91*24*time.Hour + 25*time.Minute, "Thu Oct 9 00:00 2012"},

		// Wrap around minute, hour, day, month, and year
		{"Mon Dec 31 23:59:45 2012", 15 * time.Second, "Tue Jan 1 00:00:00 2013"},

		// Round to nearest second on the delay
		{"Mon Jul 9 14:45 2012", 15*time.Minute + 50*time.Nanosecond, "Mon Jul 9 15:00 2012"},

		// Round up to 1 second if the duration is less.
		{"Mon Jul 9 14:45:00 2012", 15 * time.Millisecond, "Mon Jul 9 14:45:01 2012"},

		// Round to nearest second when calculating the next time.
		{"Mon Jul 9 14:45:00.005 2012", 15 * time.Minute, "Mon Jul 9 15:00 2012"},

		// Round to nearest second for both.
		{"Mon Jul 9 14:45:00.005 2012", 15*time.Minute + 50*time.Nanosecond, "Mon Jul 9 15:00 2012"},
	}

	for _, c := range tests {
		actual := Every(c.delay).Next(getTime(c.time))
		expected := getTime(c.expected)
		if actual != expected {
			t.Errorf("%s, \"%s\": (expected) %v != %v (actual)", c.time, c.delay, expected, actual)
		}
	}
}

func TestEveryWithMin(t *testing.T) {
	tests := []struct {
		name        string
		duration    time.Duration
		minInterval time.Duration
		wantDelay   time.Duration
	}{
		{
			name:        "duration above minimum - no change",
			duration:    5 * time.Second,
			minInterval: time.Second,
			wantDelay:   5 * time.Second,
		},
		{
			name:        "duration below minimum - rounds up",
			duration:    500 * time.Millisecond,
			minInterval: time.Second,
			wantDelay:   time.Second,
		},
		{
			name:        "zero minimum allows sub-second",
			duration:    100 * time.Millisecond,
			minInterval: 0,
			wantDelay:   100 * time.Millisecond,
		},
		{
			name:        "negative minimum allows sub-second",
			duration:    100 * time.Millisecond,
			minInterval: -time.Second,
			wantDelay:   100 * time.Millisecond,
		},
		{
			name:        "larger minimum enforced",
			duration:    30 * time.Second,
			minInterval: time.Minute,
			wantDelay:   time.Minute,
		},
		{
			name:        "duration equals minimum - no change",
			duration:    time.Minute,
			minInterval: time.Minute,
			wantDelay:   time.Minute,
		},
		{
			name:        "sub-second truncated when minimum >= 1s",
			duration:    5*time.Second + 500*time.Millisecond,
			minInterval: time.Second,
			wantDelay:   5 * time.Second,
		},
		{
			name:        "sub-second preserved when minimum allows",
			duration:    500 * time.Millisecond,
			minInterval: 100 * time.Millisecond,
			wantDelay:   500 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sched := EveryWithMin(tt.duration, tt.minInterval)
			if sched.Delay != tt.wantDelay {
				t.Errorf("EveryWithMin(%v, %v).Delay = %v, want %v",
					tt.duration, tt.minInterval, sched.Delay, tt.wantDelay)
			}
		})
	}
}

func TestEveryWithMin_Next(t *testing.T) {
	// Test that sub-second intervals actually schedule correctly
	sched := EveryWithMin(100*time.Millisecond, 0)

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	next := sched.Next(baseTime)

	expected := baseTime.Add(100 * time.Millisecond)
	if !next.Equal(expected) {
		t.Errorf("Next(%v) = %v, want %v", baseTime, next, expected)
	}

	// Verify subsequent calls continue the pattern
	next2 := sched.Next(next)
	expected2 := next.Add(100 * time.Millisecond)
	if !next2.Equal(expected2) {
		t.Errorf("Next(%v) = %v, want %v", next, next2, expected2)
	}
}

func TestEveryBackwardCompatibility(t *testing.T) {
	// Ensure Every() still works exactly as before (rounds to 1 second)
	tests := []struct {
		duration  time.Duration
		wantDelay time.Duration
	}{
		{500 * time.Millisecond, time.Second},                   // Rounds up
		{time.Second, time.Second},                              // No change
		{5 * time.Second, 5 * time.Second},                      // No change
		{5*time.Second + 500*time.Millisecond, 5 * time.Second}, // Truncates sub-second
	}

	for _, tt := range tests {
		sched := Every(tt.duration)
		if sched.Delay != tt.wantDelay {
			t.Errorf("Every(%v).Delay = %v, want %v", tt.duration, sched.Delay, tt.wantDelay)
		}
	}
}

// TestEveryWithMinBoundaryConditions tests boundary conditions to kill mutations
// at constantdelay.go:40 where `minInterval > 0` and `duration < minInterval`
// could be mutated.
func TestEveryWithMinBoundaryConditions(t *testing.T) {
	t.Run("minInterval=0 boundary - no rounding applied", func(t *testing.T) {
		// This kills mutation at line 40:17 where `minInterval > 0` could become `>= 0`
		// If mutated to >=, minInterval=0 would wrongly trigger rounding
		sched := EveryWithMin(500*time.Millisecond, 0)
		if sched.Delay != 500*time.Millisecond {
			t.Errorf("EveryWithMin(500ms, 0).Delay = %v, want 500ms (no rounding)", sched.Delay)
		}
	})

	t.Run("minInterval=1ns boundary - rounding applied", func(t *testing.T) {
		// minInterval=1ns is > 0, so rounding should be applied
		sched := EveryWithMin(500*time.Millisecond, 1*time.Nanosecond)
		// 500ms < 1ns is false, so no minimum rounding
		// But truncation logic depends on whether minInterval >= time.Second
		if sched.Delay != 500*time.Millisecond {
			t.Errorf("EveryWithMin(500ms, 1ns).Delay = %v, want 500ms", sched.Delay)
		}
	})

	t.Run("duration=minInterval boundary - no rounding", func(t *testing.T) {
		// This kills mutation at line 40:33 where `duration < minInterval`
		// could become `duration <= minInterval`
		// If mutated to <=, duration == minInterval would wrongly round up
		sched := EveryWithMin(time.Second, time.Second)
		if sched.Delay != time.Second {
			t.Errorf("EveryWithMin(1s, 1s).Delay = %v, want 1s (exact match)", sched.Delay)
		}

		// Also test with different equal values
		sched2 := EveryWithMin(5*time.Second, 5*time.Second)
		if sched2.Delay != 5*time.Second {
			t.Errorf("EveryWithMin(5s, 5s).Delay = %v, want 5s", sched2.Delay)
		}
	})

	t.Run("duration just below minInterval - rounds up", func(t *testing.T) {
		// duration < minInterval should round up
		sched := EveryWithMin(time.Second-1, time.Second)
		if sched.Delay != time.Second {
			t.Errorf("EveryWithMin(999999999ns, 1s).Delay = %v, want 1s", sched.Delay)
		}
	})

	t.Run("duration just above minInterval - no rounding", func(t *testing.T) {
		// duration > minInterval should not round
		sched := EveryWithMin(time.Second+1, time.Second)
		// The +1 nanosecond gets truncated by sub-second truncation
		if sched.Delay != time.Second {
			t.Errorf("EveryWithMin(1s+1ns, 1s).Delay = %v, want 1s (truncated)", sched.Delay)
		}

		// More meaningful: duration clearly above
		sched2 := EveryWithMin(2*time.Second, time.Second)
		if sched2.Delay != 2*time.Second {
			t.Errorf("EveryWithMin(2s, 1s).Delay = %v, want 2s", sched2.Delay)
		}
	})
}

// TestEveryWithMinTruncationBoundaries tests the complex truncation condition
// at constantdelay.go:45 to kill mutations.
func TestEveryWithMinTruncationBoundaries(t *testing.T) {
	t.Run("minInterval >= time.Second - truncates sub-second", func(t *testing.T) {
		// When minInterval >= 1s, sub-second parts are truncated
		sched := EveryWithMin(5*time.Second+500*time.Millisecond, time.Second)
		if sched.Delay != 5*time.Second {
			t.Errorf("got %v, want 5s (sub-second truncated)", sched.Delay)
		}
	})

	t.Run("minInterval < time.Second and > 0 - preserves sub-second", func(t *testing.T) {
		// This kills mutations at line 45:47, 45:64
		// When 0 < minInterval < 1s, sub-second parts are preserved
		sched := EveryWithMin(500*time.Millisecond, 100*time.Millisecond)
		if sched.Delay != 500*time.Millisecond {
			t.Errorf("got %v, want 500ms (sub-second preserved)", sched.Delay)
		}

		// Also test with duration having sub-second parts
		sched2 := EveryWithMin(1*time.Second+500*time.Millisecond, 100*time.Millisecond)
		if sched2.Delay != 1*time.Second+500*time.Millisecond {
			t.Errorf("got %v, want 1.5s (sub-second preserved)", sched2.Delay)
		}
	})

	t.Run("minInterval <= 0 with duration >= 1s - truncates", func(t *testing.T) {
		// When minInterval <= 0 AND duration >= 1s, should truncate
		sched := EveryWithMin(5*time.Second+500*time.Millisecond, 0)
		if sched.Delay != 5*time.Second {
			t.Errorf("got %v, want 5s (truncated when duration >= 1s)", sched.Delay)
		}

		sched2 := EveryWithMin(5*time.Second+500*time.Millisecond, -time.Second)
		if sched2.Delay != 5*time.Second {
			t.Errorf("got %v, want 5s (truncated when minInterval < 0)", sched2.Delay)
		}
	})

	t.Run("minInterval <= 0 with duration < 1s - preserves", func(t *testing.T) {
		// When minInterval <= 0 AND duration < 1s, should preserve sub-second
		sched := EveryWithMin(500*time.Millisecond, 0)
		if sched.Delay != 500*time.Millisecond {
			t.Errorf("got %v, want 500ms (preserved sub-second)", sched.Delay)
		}
	})

	t.Run("minInterval = time.Second exactly - truncates", func(t *testing.T) {
		// Boundary: exactly 1 second should truncate sub-second parts
		sched := EveryWithMin(2*time.Second+100*time.Millisecond, time.Second)
		if sched.Delay != 2*time.Second {
			t.Errorf("got %v, want 2s (truncated at exact 1s boundary)", sched.Delay)
		}
	})

	t.Run("minInterval = time.Second-1 - preserves", func(t *testing.T) {
		// Just below 1 second - should preserve sub-second
		sched := EveryWithMin(2*time.Second+100*time.Millisecond, time.Second-1)
		if sched.Delay != 2*time.Second+100*time.Millisecond {
			t.Errorf("got %v, want 2.1s (preserved below 1s boundary)", sched.Delay)
		}
	})
}

// TestConstantDelayNextBoundaries tests the Next() method boundaries
// to kill mutations at constantdelay.go:62 and :66.
func TestConstantDelayNextBoundaries(t *testing.T) {
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 500_000_000, time.UTC) // Has 500ms nanoseconds

	t.Run("Delay=0 returns t+1s fallback", func(t *testing.T) {
		// This kills mutation at line 62:20 where `Delay <= 0`
		// could become `Delay < 0` (missing zero case)
		sched := ConstantDelaySchedule{Delay: 0}
		next := sched.Next(baseTime)
		expected := baseTime.Add(time.Second)
		if !next.Equal(expected) {
			t.Errorf("Next() with Delay=0: got %v, want %v (1s fallback)", next, expected)
		}
	})

	t.Run("Delay=-1 returns t+1s fallback", func(t *testing.T) {
		// Negative delay should also trigger fallback
		sched := ConstantDelaySchedule{Delay: -1 * time.Nanosecond}
		next := sched.Next(baseTime)
		expected := baseTime.Add(time.Second)
		if !next.Equal(expected) {
			t.Errorf("Next() with Delay=-1ns: got %v, want %v", next, expected)
		}
	})

	t.Run("Delay=-1s returns t+1s fallback", func(t *testing.T) {
		sched := ConstantDelaySchedule{Delay: -1 * time.Second}
		next := sched.Next(baseTime)
		expected := baseTime.Add(time.Second)
		if !next.Equal(expected) {
			t.Errorf("Next() with Delay=-1s: got %v, want %v", next, expected)
		}
	})

	t.Run("Delay=1ns uses sub-second path", func(t *testing.T) {
		// Delay > 0 but < 1s should not trigger fallback and use sub-second path
		sched := ConstantDelaySchedule{Delay: 1 * time.Nanosecond}
		next := sched.Next(baseTime)
		expected := baseTime.Add(1 * time.Nanosecond)
		if !next.Equal(expected) {
			t.Errorf("Next() with Delay=1ns: got %v, want %v", next, expected)
		}
	})

	t.Run("Delay < 1s - no rounding", func(t *testing.T) {
		// This kills mutation at line 66:20 where `Delay < time.Second`
		// could become `Delay <= time.Second`
		sched := ConstantDelaySchedule{Delay: 500 * time.Millisecond}
		next := sched.Next(baseTime)
		// Sub-second: no rounding, just add delay
		expected := baseTime.Add(500 * time.Millisecond)
		if !next.Equal(expected) {
			t.Errorf("Next() with Delay=500ms: got %v, want %v (no rounding)", next, expected)
		}
	})

	t.Run("Delay = 1s exactly - rounds to second", func(t *testing.T) {
		// Exactly 1 second should round (removes nanoseconds from result)
		sched := ConstantDelaySchedule{Delay: time.Second}
		next := sched.Next(baseTime)
		// Should add 1s minus the nanosecond offset
		expected := time.Date(2024, 1, 1, 12, 0, 1, 0, time.UTC)
		if !next.Equal(expected) {
			t.Errorf("Next() with Delay=1s: got %v, want %v (rounded to second)", next, expected)
		}
	})

	t.Run("Delay = 1s-1ns - no rounding (sub-second path)", func(t *testing.T) {
		// Just below 1 second should use sub-second path (no rounding)
		sched := ConstantDelaySchedule{Delay: time.Second - 1}
		next := sched.Next(baseTime)
		expected := baseTime.Add(time.Second - 1)
		if !next.Equal(expected) {
			t.Errorf("Next() with Delay=999999999ns: got %v, want %v (no rounding)", next, expected)
		}
	})

	t.Run("Delay > 1s - rounds to second", func(t *testing.T) {
		sched := ConstantDelaySchedule{Delay: 5 * time.Second}
		next := sched.Next(baseTime)
		// Should add 5s and round to second boundary
		expected := time.Date(2024, 1, 1, 12, 0, 5, 0, time.UTC)
		if !next.Equal(expected) {
			t.Errorf("Next() with Delay=5s: got %v, want %v (rounded)", next, expected)
		}
	})
}

func TestConstantDelayPrev(t *testing.T) {
	tests := []struct {
		time     string
		delay    time.Duration
		expected string
	}{
		// Simple cases - mirror of Next tests but going backwards
		{"Mon Jul 9 15:00 2012", 15 * time.Minute, "Mon Jul 9 14:45 2012"},
		{"Mon Jul 9 15:14 2012", 15 * time.Minute, "Mon Jul 9 14:59 2012"},
		{"Mon Jul 9 15:14:59 2012", 15 * time.Minute, "Mon Jul 9 14:59:59 2012"},

		// Wrap around hours backwards
		{"Mon Jul 9 16:20 2012", 35 * time.Minute, "Mon Jul 9 15:45 2012"},

		// Wrap around days backwards
		{"Tue Jul 10 00:00 2012", 14 * time.Minute, "Mon Jul 9 23:46 2012"},
		{"Tue Jul 10 00:20 2012", 35 * time.Minute, "Mon Jul 9 23:45 2012"},
	}

	for _, c := range tests {
		actual := Every(c.delay).Prev(getTime(c.time))
		expected := getTime(c.expected)
		if actual != expected {
			t.Errorf("%s, \"%s\": (expected) %v != %v (actual)", c.time, c.delay, expected, actual)
		}
	}
}

// TestConstantDelayPrevBoundaries tests the Prev() method boundaries
// to kill mutations similar to Next() boundaries.
func TestConstantDelayPrevBoundaries(t *testing.T) {
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 500_000_000, time.UTC) // Has 500ms nanoseconds

	t.Run("Delay=0 returns t-1s fallback", func(t *testing.T) {
		sched := ConstantDelaySchedule{Delay: 0}
		prev := sched.Prev(baseTime)
		expected := baseTime.Add(-time.Second)
		if !prev.Equal(expected) {
			t.Errorf("Prev() with Delay=0: got %v, want %v (1s fallback)", prev, expected)
		}
	})

	t.Run("Delay=-1 returns t-1s fallback", func(t *testing.T) {
		sched := ConstantDelaySchedule{Delay: -1 * time.Nanosecond}
		prev := sched.Prev(baseTime)
		expected := baseTime.Add(-time.Second)
		if !prev.Equal(expected) {
			t.Errorf("Prev() with Delay=-1ns: got %v, want %v", prev, expected)
		}
	})

	t.Run("Delay < 1s - no rounding", func(t *testing.T) {
		sched := ConstantDelaySchedule{Delay: 500 * time.Millisecond}
		prev := sched.Prev(baseTime)
		expected := baseTime.Add(-500 * time.Millisecond)
		if !prev.Equal(expected) {
			t.Errorf("Prev() with Delay=500ms: got %v, want %v (no rounding)", prev, expected)
		}
	})

	t.Run("Delay = 1s exactly - rounds to second", func(t *testing.T) {
		sched := ConstantDelaySchedule{Delay: time.Second}
		prev := sched.Prev(baseTime)
		// Subtracts 1s and adds back the nanosecond offset
		// -1s + 500ms nanoseconds = 12:00:00.5 - 1s + 0.5s = 12:00:00
		expected := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		if !prev.Equal(expected) {
			t.Errorf("Prev() with Delay=1s: got %v, want %v (rounded to second)", prev, expected)
		}
	})

	t.Run("Delay > 1s - rounds to second", func(t *testing.T) {
		sched := ConstantDelaySchedule{Delay: 5 * time.Second}
		prev := sched.Prev(baseTime)
		// -5s + 500ms nanoseconds = 12:00:00.5 - 5s + 0.5s = 11:55:01
		expected := time.Date(2024, 1, 1, 11, 59, 56, 0, time.UTC)
		if !prev.Equal(expected) {
			t.Errorf("Prev() with Delay=5s: got %v, want %v (rounded)", prev, expected)
		}
	})
}

func TestConstantDelayPrevAndNextSymmetry(t *testing.T) {
	delays := []time.Duration{
		time.Second,
		5 * time.Second,
		time.Minute,
		15 * time.Minute,
		time.Hour,
	}

	baseTime := time.Date(2024, 6, 15, 12, 30, 0, 0, time.UTC)

	for _, delay := range delays {
		t.Run(delay.String(), func(t *testing.T) {
			sched := Every(delay)

			// Next then Prev should return close to original
			nextTime := sched.Next(baseTime)
			prevFromNext := sched.Prev(nextTime)

			// For ConstantDelaySchedule, the difference should equal the delay
			diff := nextTime.Sub(prevFromNext)
			if diff != delay {
				t.Errorf("Next() - Prev(Next()) = %v, want %v", diff, delay)
			}
		})
	}
}
