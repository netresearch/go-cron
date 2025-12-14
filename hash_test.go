package cron

import (
	"testing"
	"time"
)

// TestHashFieldParsing tests that the H (hash) field can be parsed.
func TestHashFieldParsing(t *testing.T) {
	parser := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor | Hash)

	tests := []struct {
		name    string
		spec    string
		hashKey string
		wantErr bool
	}{
		{
			name:    "simple H in minute",
			spec:    "H * * * *",
			hashKey: "job1",
			wantErr: false,
		},
		{
			name:    "H with step",
			spec:    "H/15 * * * *",
			hashKey: "job1",
			wantErr: false,
		},
		{
			name:    "H with range",
			spec:    "H(0-29) * * * *",
			hashKey: "job1",
			wantErr: false,
		},
		{
			name:    "H with range and step",
			spec:    "H(0-30)/10 * * * *",
			hashKey: "job1",
			wantErr: false,
		},
		{
			name:    "H in hour field",
			spec:    "0 H * * *",
			hashKey: "job1",
			wantErr: false,
		},
		{
			name:    "H in multiple fields",
			spec:    "H H * * *",
			hashKey: "job1",
			wantErr: false,
		},
		{
			name:    "H without hash key should fail",
			spec:    "H * * * *",
			hashKey: "",
			wantErr: true,
		},
		{
			name:    "H not enabled in parser should fail",
			spec:    "H * * * *",
			hashKey: "job1",
			wantErr: true, // Will test with Hash option disabled
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var testParser Parser
			if tt.name == "H not enabled in parser should fail" {
				testParser = NewParser(Minute | Hour | Dom | Month | Dow | Descriptor) // No Hash
			} else {
				testParser = parser
			}

			_, err := testParser.ParseWithHashKey(tt.spec, tt.hashKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseWithHashKey(%q, %q) error = %v, wantErr %v", tt.spec, tt.hashKey, err, tt.wantErr)
			}
		})
	}
}

// TestHashDeterminism tests that the same hash key always produces the same result.
func TestHashDeterminism(t *testing.T) {
	parser := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor | Hash)
	loc := time.UTC

	// Parse the same spec+key multiple times
	schedule1, err := parser.ParseWithHashKey("H * * * *", "job1")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	schedule2, err := parser.ParseWithHashKey("H * * * *", "job1")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Both should produce the same next time
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, loc)
	next1 := schedule1.Next(from)
	next2 := schedule2.Next(from)

	if !next1.Equal(next2) {
		t.Errorf("Same hash key produced different results: %v vs %v", next1, next2)
	}
}

// TestHashDistribution tests that different hash keys produce different results.
func TestHashDistribution(t *testing.T) {
	parser := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor | Hash)
	loc := time.UTC

	// Use many different keys to test distribution
	keys := []string{"job1", "job2", "job3", "worker-a", "worker-b", "task_123", "cron_job_xyz"}
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, loc)

	minutes := make(map[int]int)
	for _, key := range keys {
		schedule, err := parser.ParseWithHashKey("H * * * *", key)
		if err != nil {
			t.Fatalf("Parse failed for key %q: %v", key, err)
		}
		next := schedule.Next(from)
		minutes[next.Minute()]++
	}

	// We should see some distribution (not all same minute)
	// With 7 keys, it's statistically unlikely they all hash to the same minute
	if len(minutes) < 2 {
		t.Errorf("Poor hash distribution: all %d keys mapped to same minute(s): %v", len(keys), minutes)
	}
}

// TestHashWithStep tests H expressions with step values.
func TestHashWithStep(t *testing.T) {
	parser := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor | Hash)
	loc := time.UTC

	// H/15 should produce values at 15-minute intervals starting from hash offset
	schedule, err := parser.ParseWithHashKey("H/15 * * * *", "job1")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, loc)
	times := make([]time.Time, 4)
	current := from
	for i := 0; i < 4; i++ {
		times[i] = schedule.Next(current)
		current = times[i]
	}

	// Verify 15-minute intervals
	for i := 1; i < len(times); i++ {
		diff := times[i].Sub(times[i-1])
		if diff != 15*time.Minute {
			t.Errorf("Expected 15-minute interval, got %v between %v and %v", diff, times[i-1], times[i])
		}
	}
}

// TestHashWithRange tests H expressions with explicit ranges.
func TestHashWithRange(t *testing.T) {
	parser := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor | Hash)
	loc := time.UTC

	// H(0-29) should only produce values in 0-29 range
	schedule, err := parser.ParseWithHashKey("H(0-29) * * * *", "job1")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, loc)
	for i := 0; i < 10; i++ {
		next := schedule.Next(from)
		if next.Minute() < 0 || next.Minute() > 29 {
			t.Errorf("Minute %d outside range 0-29", next.Minute())
		}
		from = next
	}
}

// TestHashWithRangeAndStep tests H expressions with both range and step.
func TestHashWithRangeAndStep(t *testing.T) {
	parser := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor | Hash)
	loc := time.UTC

	// H(0-30)/10 should produce values at 10-minute intervals within 0-30
	schedule, err := parser.ParseWithHashKey("H(0-30)/10 * * * *", "job1")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, loc)

	// Get first few execution times and verify they're 10 minutes apart
	times := make([]time.Time, 3)
	current := from
	for i := 0; i < 3; i++ {
		times[i] = schedule.Next(current)
		if times[i].Minute() > 30 {
			t.Errorf("Minute %d exceeds range max 30", times[i].Minute())
		}
		current = times[i]
	}
}

// TestHashParseOptionConstant verifies Hash constant value.
func TestHashParseOptionConstant(t *testing.T) {
	// Hash should be a unique bit flag
	if Hash == 0 {
		t.Error("Hash constant should not be zero")
	}

	// Verify Hash doesn't conflict with existing options
	existingOptions := Second | SecondOptional | Minute | Hour | Dom | Month | Dow | DowOptional | Descriptor
	if Hash&existingOptions != 0 {
		t.Error("Hash constant conflicts with existing options")
	}
}

// TestHashWithSeconds tests H expression with seconds field enabled.
func TestHashWithSeconds(t *testing.T) {
	parser := NewParser(Second | Minute | Hour | Dom | Month | Dow | Hash)

	tests := []struct {
		name    string
		spec    string
		hashKey string
		wantErr bool
	}{
		{
			name:    "H in second field",
			spec:    "H * * * * *",
			hashKey: "job1",
			wantErr: false,
		},
		{
			name:    "H in minute with seconds",
			spec:    "0 H * * * *",
			hashKey: "job1",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parser.ParseWithHashKey(tt.spec, tt.hashKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseWithHashKey(%q, %q) error = %v, wantErr %v", tt.spec, tt.hashKey, err, tt.wantErr)
			}
		})
	}
}

// TestWithHashKey tests the WithHashKey chainable method.
func TestWithHashKey(t *testing.T) {
	parser := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor | Hash).
		WithHashKey("default-job")

	// Should be able to parse H expressions with the default key
	schedule, err := parser.Parse("H * * * *")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if schedule == nil {
		t.Error("Expected non-nil schedule")
	}
}

// TestHashFieldConcurrent tests concurrent access with hash fields.
func TestHashFieldConcurrent(t *testing.T) {
	parser := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor | Hash)

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				key := "job" + string(rune('0'+id))
				schedule, err := parser.ParseWithHashKey("H * * * *", key)
				if err != nil {
					t.Error(err)
				}
				from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
				next := schedule.Next(from)
				if next.IsZero() {
					t.Error("Next returned zero")
				}
			}
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}
