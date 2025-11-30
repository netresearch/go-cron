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
		{500 * time.Millisecond, time.Second},      // Rounds up
		{time.Second, time.Second},                 // No change
		{5 * time.Second, 5 * time.Second},         // No change
		{5*time.Second + 500*time.Millisecond, 5 * time.Second}, // Truncates sub-second
	}

	for _, tt := range tests {
		sched := Every(tt.duration)
		if sched.Delay != tt.wantDelay {
			t.Errorf("Every(%v).Delay = %v, want %v", tt.duration, sched.Delay, tt.wantDelay)
		}
	}
}
