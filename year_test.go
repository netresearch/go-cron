package cron

import (
	"testing"
	"time"
)

// TestYearFieldParsing tests that the year field can be parsed.
func TestYearFieldParsing(t *testing.T) {
	parser := NewParser(Minute | Hour | Dom | Month | Dow | Year)

	tests := []struct {
		name    string
		spec    string
		wantErr bool
	}{
		{
			name:    "specific year",
			spec:    "0 0 1 1 * 2025",
			wantErr: false,
		},
		{
			name:    "year range",
			spec:    "0 0 1 1 * 2024-2026",
			wantErr: false,
		},
		{
			name:    "year wildcard",
			spec:    "0 0 1 1 * *",
			wantErr: false,
		},
		{
			name:    "year list",
			spec:    "0 0 1 1 * 2024,2025,2026",
			wantErr: false,
		},
		{
			name:    "year step",
			spec:    "0 0 1 1 * 2020-2030/2",
			wantErr: false,
		},
		{
			name:    "year below minimum",
			spec:    "0 0 1 1 * 1969",
			wantErr: true,
		},
		{
			name:    "year above maximum",
			spec:    "0 0 1 1 * 2034",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parser.Parse(tt.spec)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse(%q) error = %v, wantErr %v", tt.spec, err, tt.wantErr)
			}
		})
	}
}

// TestYearFieldWithSeconds tests year field when seconds are also enabled.
func TestYearFieldWithSeconds(t *testing.T) {
	parser := NewParser(Second | Minute | Hour | Dom | Month | Dow | Year)

	tests := []struct {
		name    string
		spec    string
		wantErr bool
	}{
		{
			name:    "7 fields with specific year",
			spec:    "0 0 0 1 1 * 2025",
			wantErr: false,
		},
		{
			name:    "7 fields with year range",
			spec:    "30 15 10 15 6 * 2024-2026",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parser.Parse(tt.spec)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse(%q) error = %v, wantErr %v", tt.spec, err, tt.wantErr)
			}
		})
	}
}

// TestYearFieldScheduleNext tests that Next() respects year constraints.
func TestYearFieldScheduleNext(t *testing.T) {
	parser := NewParser(Minute | Hour | Dom | Month | Dow | Year)
	loc := time.UTC

	tests := []struct {
		name     string
		spec     string
		from     time.Time
		wantYear int
		wantZero bool // expect zero time (no valid time found)
	}{
		{
			name:     "next in specific year",
			spec:     "0 0 1 1 * 2025",
			from:     time.Date(2024, 6, 15, 10, 0, 0, 0, loc),
			wantYear: 2025,
		},
		{
			name:     "next in year range - start of range",
			spec:     "0 0 1 1 * 2024-2026",
			from:     time.Date(2023, 6, 15, 10, 0, 0, 0, loc),
			wantYear: 2024,
		},
		{
			name:     "next in year range - middle of range",
			spec:     "0 0 1 1 * 2024-2026",
			from:     time.Date(2024, 6, 15, 10, 0, 0, 0, loc),
			wantYear: 2025,
		},
		{
			name:     "next in year range - end of range",
			spec:     "0 0 1 1 * 2024-2026",
			from:     time.Date(2025, 6, 15, 10, 0, 0, 0, loc),
			wantYear: 2026,
		},
		{
			name:     "after year range returns zero",
			spec:     "0 0 1 1 * 2024-2026",
			from:     time.Date(2026, 6, 15, 10, 0, 0, 0, loc),
			wantZero: true,
		},
		{
			name:     "wildcard year works normally",
			spec:     "0 0 1 1 * *",
			from:     time.Date(2024, 6, 15, 10, 0, 0, 0, loc),
			wantYear: 2025,
		},
		{
			name:     "year list",
			spec:     "0 0 1 1 * 2024,2026,2028",
			from:     time.Date(2024, 6, 15, 10, 0, 0, 0, loc),
			wantYear: 2026,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schedule, err := parser.Parse(tt.spec)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.spec, err)
			}

			next := schedule.Next(tt.from)
			if tt.wantZero {
				if !next.IsZero() {
					t.Errorf("Next(%v) = %v, want zero time", tt.from, next)
				}
				return
			}

			if next.IsZero() {
				t.Errorf("Next(%v) returned zero time, want year %d", tt.from, tt.wantYear)
				return
			}

			if next.Year() != tt.wantYear {
				t.Errorf("Next(%v).Year() = %d, want %d", tt.from, next.Year(), tt.wantYear)
			}
		})
	}
}

// TestYearFieldSchedulePrev tests that Prev() respects year constraints.
func TestYearFieldSchedulePrev(t *testing.T) {
	parser := NewParser(Minute | Hour | Dom | Month | Dow | Year)
	loc := time.UTC

	tests := []struct {
		name     string
		spec     string
		from     time.Time
		wantYear int
		wantZero bool
	}{
		{
			name:     "prev in specific year",
			spec:     "0 0 1 1 * 2024",
			from:     time.Date(2025, 6, 15, 10, 0, 0, 0, loc),
			wantYear: 2024,
		},
		{
			name:     "prev in year range - end of range",
			spec:     "0 0 1 1 * 2024-2026",
			from:     time.Date(2027, 6, 15, 10, 0, 0, 0, loc),
			wantYear: 2026,
		},
		{
			name:     "before year range returns zero",
			spec:     "0 0 1 1 * 2024-2026",
			from:     time.Date(2024, 1, 1, 0, 0, 0, 0, loc),
			wantZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schedule, err := parser.Parse(tt.spec)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.spec, err)
			}

			prev := schedule.Prev(tt.from)
			if tt.wantZero {
				if !prev.IsZero() {
					t.Errorf("Prev(%v) = %v, want zero time", tt.from, prev)
				}
				return
			}

			if prev.IsZero() {
				t.Errorf("Prev(%v) returned zero time, want year %d", tt.from, tt.wantYear)
				return
			}

			if prev.Year() != tt.wantYear {
				t.Errorf("Prev(%v).Year() = %d, want %d", tt.from, prev.Year(), tt.wantYear)
			}
		})
	}
}

// TestYearFieldWithSevenFields tests 7-field parsing.
func TestYearFieldWithSevenFields(t *testing.T) {
	// Standard parser with seconds and year (7 fields total)
	parser := NewParser(Second | Minute | Hour | Dom | Month | Dow | Year)
	loc := time.UTC

	// Parse "at second 30, minute 15, hour 10 on Jan 1, any weekday, 2025"
	schedule, err := parser.Parse("30 15 10 1 1 * 2025")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Find next from Dec 2024
	from := time.Date(2024, 12, 15, 10, 0, 0, 0, loc)
	next := schedule.Next(from)

	if next.IsZero() {
		t.Fatal("Next returned zero time")
	}

	// Should be Jan 1, 2025 at 10:15:30
	expected := time.Date(2025, 1, 1, 10, 15, 30, 0, loc)
	if !next.Equal(expected) {
		t.Errorf("Next() = %v, want %v", next, expected)
	}
}

// TestYearParseOptionConstant verifies Year constant value.
func TestYearParseOptionConstant(t *testing.T) {
	// Year should be a unique bit flag
	if Year == 0 {
		t.Error("Year constant should not be zero")
	}

	// Verify Year doesn't conflict with existing options
	existingOptions := Second | SecondOptional | Minute | Hour | Dom | Month | Dow | DowOptional | Descriptor
	if Year&existingOptions != 0 {
		t.Error("Year constant conflicts with existing options")
	}
}

// TestYearFieldDefaultBehavior tests that without Year option, 6 fields work as before.
func TestYearFieldDefaultBehavior(t *testing.T) {
	// Standard 5-field parser (no seconds, no year)
	parser := NewParser(Minute | Hour | Dom | Month | Dow)

	// Should parse 5 fields normally
	schedule, err := parser.Parse("0 0 1 1 *")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Verify it schedules correctly for any year
	from := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)
	next := schedule.Next(from)
	if next.IsZero() {
		t.Fatal("Next returned zero time")
	}

	// Should be Jan 1, 2025 (next occurrence)
	if next.Year() != 2025 || next.Month() != 1 || next.Day() != 1 {
		t.Errorf("Next() = %v, want Jan 1, 2025", next)
	}
}

// TestYearBounds verifies the year field accepts valid years.
func TestYearBounds(t *testing.T) {
	parser := NewParser(Minute | Hour | Dom | Month | Dow | Year)

	tests := []struct {
		year    string
		wantErr bool
	}{
		{"1970", false}, // Unix epoch - minimum
		{"2000", false}, // Y2K
		{"2024", false}, // Current era
		{"2033", false}, // Maximum year (1970 + 63)
		{"1969", true},  // Below minimum
		{"2034", true},  // Above maximum
		{"0", true},     // Invalid
		{"-1", true},    // Invalid
		{"10000", true}, // Way above maximum
	}

	for _, tt := range tests {
		t.Run(tt.year, func(t *testing.T) {
			spec := "0 0 1 1 * " + tt.year
			_, err := parser.Parse(spec)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse(%q) error = %v, wantErr %v", spec, err, tt.wantErr)
			}
		})
	}
}

// TestYearFieldConcurrent tests concurrent access with year field.
func TestYearFieldConcurrent(t *testing.T) {
	parser := NewParser(Minute | Hour | Dom | Month | Dow | Year)

	schedule, err := parser.Parse("0 0 1 * * 2024-2030")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
				next := schedule.Next(from)
				if next.IsZero() {
					t.Error("Next returned zero")
				}
				prev := schedule.Prev(from.Add(24 * time.Hour))
				if prev.IsZero() {
					t.Error("Prev returned zero")
				}
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}
