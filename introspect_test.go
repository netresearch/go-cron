package cron

import (
	"testing"
	"time"
)

// TestNextN tests the NextN function for retrieving multiple next execution times.
func TestNextN(t *testing.T) {
	// Parse a schedule that runs every hour at minute 0
	schedule, err := ParseStandard("0 * * * *")
	if err != nil {
		t.Fatalf("ParseStandard failed: %v", err)
	}

	// Start time: 2024-06-15 10:30:00 UTC
	start := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name     string
		n        int
		wantLen  int
		wantHour []int // expected hours
	}{
		{
			name:     "next 3 hourly executions",
			n:        3,
			wantLen:  3,
			wantHour: []int{11, 12, 13},
		},
		{
			name:     "next 5 hourly executions",
			n:        5,
			wantLen:  5,
			wantHour: []int{11, 12, 13, 14, 15},
		},
		{
			name:    "next 0 returns empty",
			n:       0,
			wantLen: 0,
		},
		{
			name:    "negative n returns empty",
			n:       -1,
			wantLen: 0,
		},
		{
			name:     "next 1",
			n:        1,
			wantLen:  1,
			wantHour: []int{11},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			times := NextN(schedule, start, tt.n)
			if len(times) != tt.wantLen {
				t.Errorf("NextN(%d) returned %d times, want %d", tt.n, len(times), tt.wantLen)
			}

			for i, wantHour := range tt.wantHour {
				if i < len(times) {
					if times[i].Hour() != wantHour {
						t.Errorf("NextN(%d)[%d] hour = %d, want %d", tt.n, i, times[i].Hour(), wantHour)
					}
					if times[i].Minute() != 0 {
						t.Errorf("NextN(%d)[%d] minute = %d, want 0", tt.n, i, times[i].Minute())
					}
				}
			}

			// Verify times are in ascending order
			for i := 1; i < len(times); i++ {
				if !times[i].After(times[i-1]) {
					t.Errorf("NextN times not in ascending order: %v >= %v", times[i-1], times[i])
				}
			}
		})
	}
}

// TestNextNWithDailySchedule tests NextN with a daily schedule.
func TestNextNWithDailySchedule(t *testing.T) {
	schedule, err := ParseStandard("0 9 * * *") // Daily at 9am
	if err != nil {
		t.Fatalf("ParseStandard failed: %v", err)
	}

	start := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC) // After 9am

	times := NextN(schedule, start, 7)
	if len(times) != 7 {
		t.Fatalf("NextN(7) returned %d times, want 7", len(times))
	}

	// Verify we get 7 consecutive days
	for i, tm := range times {
		expectedDay := 16 + i // Starting from June 16
		if tm.Day() != expectedDay && tm.Month() == time.June {
			// Handle month rollover
			if tm.Month() != time.July || expectedDay <= 30 {
				t.Errorf("NextN[%d] day = %d, want %d", i, tm.Day(), expectedDay)
			}
		}
		if tm.Hour() != 9 {
			t.Errorf("NextN[%d] hour = %d, want 9", i, tm.Hour())
		}
	}
}

// TestBetween tests the Between function for executions in a time range.
func TestBetween(t *testing.T) {
	schedule, err := ParseStandard("0 * * * *") // Every hour
	if err != nil {
		t.Fatalf("ParseStandard failed: %v", err)
	}

	tests := []struct {
		name      string
		start     time.Time
		end       time.Time
		wantCount int
	}{
		{
			name:      "3 hours span",
			start:     time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC),
			end:       time.Date(2024, 6, 15, 13, 0, 0, 0, time.UTC),
			wantCount: 2, // 11:00, 12:00 (Next returns times AFTER start, end is exclusive)
		},
		{
			name:      "partial hour at start",
			start:     time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC),
			end:       time.Date(2024, 6, 15, 14, 0, 0, 0, time.UTC),
			wantCount: 3, // 11:00, 12:00, 13:00
		},
		{
			name:      "same time (no executions)",
			start:     time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC),
			end:       time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC),
			wantCount: 0,
		},
		{
			name:      "end before start (no executions)",
			start:     time.Date(2024, 6, 15, 14, 0, 0, 0, time.UTC),
			end:       time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC),
			wantCount: 0,
		},
		{
			name:      "24 hour span",
			start:     time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC),
			end:       time.Date(2024, 6, 16, 0, 0, 0, 0, time.UTC),
			wantCount: 23, // 01:00 through 23:00 (Next returns times AFTER start, end exclusive)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			times := Between(schedule, tt.start, tt.end)
			if len(times) != tt.wantCount {
				t.Errorf("Between() returned %d times, want %d", len(times), tt.wantCount)
			}

			// Verify all times are within range
			for _, tm := range times {
				if tm.Before(tt.start) || !tm.Before(tt.end) {
					t.Errorf("Time %v is outside range [%v, %v)", tm, tt.start, tt.end)
				}
			}

			// Verify times are in ascending order
			for i := 1; i < len(times); i++ {
				if !times[i].After(times[i-1]) {
					t.Errorf("Times not in ascending order at index %d", i)
				}
			}
		})
	}
}

// TestBetweenWithLimit tests Between with a maximum limit.
func TestBetweenWithLimit(t *testing.T) {
	schedule, err := ParseStandard("* * * * *") // Every minute
	if err != nil {
		t.Fatalf("ParseStandard failed: %v", err)
	}

	start := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 6, 16, 0, 0, 0, 0, time.UTC) // 1440 minutes

	// Without limit, this would return 1440 times
	// With limit, it should cap at the limit
	times := BetweenWithLimit(schedule, start, end, 100)
	if len(times) != 100 {
		t.Errorf("BetweenWithLimit(100) returned %d times, want 100", len(times))
	}
}

// TestCount tests the Count function.
func TestCount(t *testing.T) {
	schedule, err := ParseStandard("0 * * * *") // Every hour
	if err != nil {
		t.Fatalf("ParseStandard failed: %v", err)
	}

	tests := []struct {
		name  string
		start time.Time
		end   time.Time
		want  int
	}{
		{
			name:  "3 hours",
			start: time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC),
			end:   time.Date(2024, 6, 15, 13, 0, 0, 0, time.UTC),
			want:  2, // 11:00, 12:00 (Next returns times AFTER start, end exclusive)
		},
		{
			name:  "24 hours",
			start: time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC),
			end:   time.Date(2024, 6, 16, 0, 0, 0, 0, time.UTC),
			want:  23, // 01:00 through 23:00 (Next returns times AFTER start, end exclusive)
		},
		{
			name:  "partial hour at start",
			start: time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC),
			end:   time.Date(2024, 6, 15, 14, 0, 0, 0, time.UTC),
			want:  3,
		},
		{
			name:  "end before start",
			start: time.Date(2024, 6, 15, 14, 0, 0, 0, time.UTC),
			end:   time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC),
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := Count(schedule, tt.start, tt.end)
			if count != tt.want {
				t.Errorf("Count() = %d, want %d", count, tt.want)
			}
		})
	}
}

// TestCountWithLimit tests the Count function with a limit.
func TestCountWithLimit(t *testing.T) {
	schedule, err := ParseStandard("* * * * *") // Every minute
	if err != nil {
		t.Fatalf("ParseStandard failed: %v", err)
	}

	start := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 6, 16, 0, 0, 0, 0, time.UTC) // 1440 minutes

	// Should stop counting at 1000 and return that we hit the limit
	count := CountWithLimit(schedule, start, end, 1000)
	if count != 1000 {
		t.Errorf("CountWithLimit(1000) = %d, want 1000", count)
	}
}

// TestMatches tests the Matches function.
func TestMatches(t *testing.T) {
	schedule, err := ParseStandard("0 9 * * MON-FRI") // 9am on weekdays
	if err != nil {
		t.Fatalf("ParseStandard failed: %v", err)
	}

	tests := []struct {
		name string
		t    time.Time
		want bool
	}{
		{
			name: "Monday 9am matches",
			t:    time.Date(2024, 6, 17, 9, 0, 0, 0, time.UTC), // Monday
			want: true,
		},
		{
			name: "Friday 9am matches",
			t:    time.Date(2024, 6, 21, 9, 0, 0, 0, time.UTC), // Friday
			want: true,
		},
		{
			name: "Saturday 9am does not match",
			t:    time.Date(2024, 6, 15, 9, 0, 0, 0, time.UTC), // Saturday
			want: false,
		},
		{
			name: "Monday 10am does not match",
			t:    time.Date(2024, 6, 17, 10, 0, 0, 0, time.UTC), // Monday but wrong hour
			want: false,
		},
		{
			name: "Monday 9:01am does not match",
			t:    time.Date(2024, 6, 17, 9, 1, 0, 0, time.UTC), // Wrong minute
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Matches(schedule, tt.t); got != tt.want {
				t.Errorf("Matches(%v) = %v, want %v", tt.t, got, tt.want)
			}
		})
	}
}

// TestMatchesHourly tests Matches with an hourly schedule.
func TestMatchesHourly(t *testing.T) {
	schedule, err := ParseStandard("0 * * * *") // Every hour at minute 0
	if err != nil {
		t.Fatalf("ParseStandard failed: %v", err)
	}

	tests := []struct {
		name string
		t    time.Time
		want bool
	}{
		{
			name: "exactly on the hour",
			t:    time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC),
			want: true,
		},
		{
			name: "one second after",
			t:    time.Date(2024, 6, 15, 10, 0, 1, 0, time.UTC),
			want: false,
		},
		{
			name: "one minute after",
			t:    time.Date(2024, 6, 15, 10, 1, 0, 0, time.UTC),
			want: false,
		},
		{
			name: "with nanoseconds (still matches)",
			t:    time.Date(2024, 6, 15, 10, 0, 0, 123456789, time.UTC),
			want: true, // Nanoseconds should be ignored for minute-level schedules
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Matches(schedule, tt.t); got != tt.want {
				t.Errorf("Matches(%v) = %v, want %v", tt.t, got, tt.want)
			}
		})
	}
}

// TestMatchesWithSeconds tests Matches with a seconds-enabled schedule.
func TestMatchesWithSeconds(t *testing.T) {
	parser := NewParser(Second | Minute | Hour | Dom | Month | Dow)
	schedule, err := parser.Parse("30 0 * * * *") // Every hour at minute 0, second 30
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	tests := []struct {
		name string
		t    time.Time
		want bool
	}{
		{
			name: "exact match with seconds",
			t:    time.Date(2024, 6, 15, 10, 0, 30, 0, time.UTC),
			want: true,
		},
		{
			name: "wrong second",
			t:    time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC),
			want: false,
		},
		{
			name: "wrong second by one",
			t:    time.Date(2024, 6, 15, 10, 0, 31, 0, time.UTC),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Matches(schedule, tt.t); got != tt.want {
				t.Errorf("Matches(%v) = %v, want %v", tt.t, got, tt.want)
			}
		})
	}
}

// TestNextNWithEverySchedule tests NextN with @every schedules.
func TestNextNWithEverySchedule(t *testing.T) {
	schedule, err := ParseStandard("@every 15m")
	if err != nil {
		t.Fatalf("ParseStandard failed: %v", err)
	}

	start := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)
	times := NextN(schedule, start, 4)

	if len(times) != 4 {
		t.Fatalf("NextN(4) returned %d times, want 4", len(times))
	}

	// Verify 15-minute intervals
	for i := 1; i < len(times); i++ {
		diff := times[i].Sub(times[i-1])
		if diff != 15*time.Minute {
			t.Errorf("Interval between times[%d] and times[%d] = %v, want 15m", i-1, i, diff)
		}
	}
}

// TestBetweenWithWeeklySchedule tests Between with a weekly schedule.
func TestBetweenWithWeeklySchedule(t *testing.T) {
	schedule, err := ParseStandard("0 9 * * MON") // Every Monday at 9am
	if err != nil {
		t.Fatalf("ParseStandard failed: %v", err)
	}

	// June 2024: Mondays are 3, 10, 17, 24
	start := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC)

	times := Between(schedule, start, end)

	// Should have 4 Mondays in June 2024
	if len(times) != 4 {
		t.Errorf("Between() returned %d times, want 4", len(times))
	}

	expectedDays := []int{3, 10, 17, 24}
	for i, tm := range times {
		if tm.Day() != expectedDays[i] {
			t.Errorf("times[%d].Day() = %d, want %d", i, tm.Day(), expectedDays[i])
		}
		if tm.Weekday() != time.Monday {
			t.Errorf("times[%d].Weekday() = %v, want Monday", i, tm.Weekday())
		}
	}
}

// TestIntrospectionWithTimezone tests introspection functions with timezone-aware schedules.
func TestIntrospectionWithTimezone(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("Failed to load timezone: %v", err)
	}

	schedule, err := ParseStandard("TZ=America/New_York 0 9 * * *")
	if err != nil {
		t.Fatalf("ParseStandard failed: %v", err)
	}

	// Start in UTC, but schedule is in New York time
	startUTC := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC) // 8am in NY (EDT)

	times := NextN(schedule, startUTC, 3)
	if len(times) != 3 {
		t.Fatalf("NextN(3) returned %d times, want 3", len(times))
	}

	// First execution should be today at 9am NY time = 1pm UTC (during EDT)
	// Verify times are at 9am in New York
	for i, tm := range times {
		nyTime := tm.In(loc)
		if nyTime.Hour() != 9 {
			t.Errorf("times[%d] in NY = %v, want 9am", i, nyTime)
		}
	}
}

// TestIntrospectionNilSchedule tests handling of nil schedules.
func TestIntrospectionNilSchedule(t *testing.T) {
	// These should not panic and return reasonable defaults
	times := NextN(nil, time.Now(), 5)
	if times != nil {
		t.Errorf("NextN(nil) = %v, want nil", times)
	}

	times = Between(nil, time.Now(), time.Now().Add(time.Hour))
	if times != nil {
		t.Errorf("Between(nil) = %v, want nil", times)
	}

	count := Count(nil, time.Now(), time.Now().Add(time.Hour))
	if count != 0 {
		t.Errorf("Count(nil) = %d, want 0", count)
	}

	if Matches(nil, time.Now()) {
		t.Error("Matches(nil) = true, want false")
	}
}

// TestIntrospectionConcurrent tests thread safety of introspection functions.
func TestIntrospectionConcurrent(t *testing.T) {
	schedule, err := ParseStandard("0 * * * *")
	if err != nil {
		t.Fatalf("ParseStandard failed: %v", err)
	}

	start := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)
	end := time.Date(2024, 6, 16, 10, 0, 0, 0, time.UTC)

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = NextN(schedule, start, 10)
				_ = Between(schedule, start, end)
				_ = Count(schedule, start, end)
				_ = Matches(schedule, start)
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}
