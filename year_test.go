// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cron

import (
	"cmp"
	"maps"
	"runtime"
	"strings"
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
			spec:    "0 0 1 1 * 0",
			wantErr: true,
		},
		{
			name:    "year above maximum",
			spec:    "0 0 1 1 * 2147483648",
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
		{
			name:     "far future year - year 3000",
			spec:     "0 0 1 1 * 3000",
			from:     time.Date(2999, 6, 15, 10, 0, 0, 0, loc),
			wantYear: 3000,
		},
		{
			name:     "far future year range",
			spec:     "0 0 1 1 * 5000-5010/2",
			from:     time.Date(5000, 6, 15, 10, 0, 0, 0, loc),
			wantYear: 5002,
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
			name:     "prev in year range - middle of range",
			spec:     "0 0 1 1 * 2024-2026",
			from:     time.Date(2026, 1, 1, 0, 0, 0, 0, loc),
			wantYear: 2025,
		},
		{
			name:     "prev in year range - start of range",
			spec:     "0 0 1 1 * 2024-2026",
			from:     time.Date(2025, 1, 1, 0, 0, 0, 0, loc),
			wantYear: 2024,
		},
		{
			name:     "before year range returns zero",
			spec:     "0 0 1 1 * 2024-2026",
			from:     time.Date(2024, 1, 1, 0, 0, 0, 0, loc),
			wantZero: true,
		},
		{
			name:     "wildcard year works normally",
			spec:     "0 0 1 1 * *",
			from:     time.Date(2024, 6, 15, 10, 0, 0, 0, loc),
			wantYear: 2024,
		},
		{
			name:     "year list",
			spec:     "0 0 1 1 * 2024,2026,2028",
			from:     time.Date(2027, 6, 15, 10, 0, 0, 0, loc),
			wantYear: 2026,
		},
		{
			name:     "year step",
			spec:     "0 0 1 1 * 2020-2030/2",
			from:     time.Date(2027, 6, 15, 10, 0, 0, 0, loc),
			wantYear: 2026,
		},
		{
			name:     "far future year - year 3000",
			spec:     "0 0 1 1 * 3000",
			from:     time.Date(3001, 6, 15, 10, 0, 0, 0, loc),
			wantYear: 3000,
		},
		{
			name:     "far future year range",
			spec:     "0 0 1 1 * 5000-5010/2",
			from:     time.Date(5007, 6, 15, 10, 0, 0, 0, loc),
			wantYear: 5006,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schedule, err := parser.Parse(tt.spec)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.spec, err)
			}

			prev := schedule.(ScheduleWithPrev).Prev(tt.from)
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
		{"1", false},          // Minimum year (YearBase)
		{"100", false},        // 3-digit year
		{"1970", false},       // Unix epoch
		{"2000", false},       // Y2K
		{"2024", false},       // Current era
		{"9999", false},       // 4-digit max
		{"10000", false},      // 5-digit year - valid with sparse storage
		{"999999", false},     // 6-digit year
		{"2147483647", false}, // MaxInt32 (YearMax)
		{"0", true},           // Below minimum
		{"-1", true},          // Negative - invalid
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

// TestYearFieldLimit verifies that a year field can select maxYearsPerField
// years, counted over the whole list, and that the step counts.
func TestYearFieldLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		field string
		want  int // years stored; 0 means the limit error
	}{
		{"1-99999", 99999},
		{"1-100000", 100000},
		{"1-100001", 0},
		{"1-2000000", 0},
		{"1-200000/2", 100000},
		{"1-200001/2", 0},
		{"1-2147483647/100000", 21475},
		{"1-50000,50001-100000", 100000},
		{"1-50000,50001-100001", 0},
		{"1-60000,1-60000", 0},
		{"1-50000,1-50000,1-50000", 0},
		{"2147483647", 1},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			t.Parallel()
			got, err := FullParser().Parse("0 0 1 1 * " + tt.field)
			if tt.want != 0 {
				if err != nil {
					t.Fatal(err)
				}
				if n := len(got.(*SpecSchedule).Year); n != tt.want {
					t.Errorf("stored %d years, want %d", n, tt.want)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "selects more than 100000 years") {
				t.Errorf("err = %v, want the limit error", err)
			}
		})
	}
}

// TestYearRangeLimit verifies that a single range is rejected before its years
// are generated. getYearField would catch it afterwards, at the cost this
// limit exists to prevent.
//
// It is not parallel, because it reads the allocation count of the process. A
// count of allocations would not do: the years come from one large make.
func TestYearRangeLimit(t *testing.T) {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	got, err := getYearRange("1-2000000")
	runtime.ReadMemStats(&after)

	if err == nil || got != nil {
		t.Errorf("got %d years, err %v, want the limit error", len(got), err)
	}
	if n := after.TotalAlloc - before.TotalAlloc; n > 1<<20 {
		t.Errorf("allocated %d bytes before the rejection", n)
	}
}

// TestYearLimitErrors verifies that a range outside the year bounds reports the
// bounds, also when it is wide, and that the limit error names the whole field
// when no single item exceeds the limit.
func TestYearLimitErrors(t *testing.T) {
	t.Parallel()
	tests := []struct{ field, want string }{
		{"0-2000000", "below minimum"},
		{"1-3000000000", "above maximum"},
		{"1-2000000,2030", `selects more than 100000 years: "1-2000000"`},
		{"1-2000000/2,2030", `selects more than 100000 years: "1-2000000/2"`},
		{"1-100001,2030", `selects more than 100000 years: "1-100001"`},
		{"1-100000,2030", `selects more than 100000 years: "1-100000,2030"`},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			t.Parallel()
			_, err := FullParser().Parse("0 0 1 1 * " + tt.field)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
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
	for range 10 {
		go func() {
			for range 100 {
				from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
				next := schedule.Next(from)
				if next.IsZero() {
					t.Error("Next returned zero")
				}
				prev := schedule.(ScheduleWithPrev).Prev(from.Add(24 * time.Hour))
				if prev.IsZero() {
					t.Error("Prev returned zero")
				}
			}
			done <- true
		}()
	}

	for range 10 {
		<-done
	}
}

// TestSecondOptionalWithYear tests that SecondOptional and Year can be used together.
// This combination requires special handling because of field count ambiguity.
func TestSecondOptionalWithYear(t *testing.T) {
	// Parser with both SecondOptional and Year enabled
	parser := NewParser(SecondOptional | Minute | Hour | Dom | Month | Dow | Year)
	loc := time.UTC

	tests := []struct {
		name       string
		spec       string
		wantErr    bool
		errContain string
		from       time.Time
		wantYear   int
		wantSecond int
	}{
		{
			// 7 fields: second minute hour dom month dow year
			name:       "7 fields with explicit year",
			spec:       "30 15 10 1 1 * 2025",
			wantErr:    false,
			from:       time.Date(2024, 12, 1, 0, 0, 0, 0, loc),
			wantYear:   2025,
			wantSecond: 30,
		},
		{
			// 6 fields with explicit year: minute hour dom month dow year
			name:       "6 fields with year - no seconds",
			spec:       "15 10 1 1 * 2025",
			wantErr:    false,
			from:       time.Date(2024, 12, 1, 0, 0, 0, 0, loc),
			wantYear:   2025,
			wantSecond: 0, // Default when seconds omitted
		},
		{
			// 7 fields with wildcard year
			name:       "7 fields with wildcard year",
			spec:       "30 15 10 1 1 * *",
			wantErr:    false,
			from:       time.Date(2024, 12, 1, 0, 0, 0, 0, loc),
			wantYear:   2025,
			wantSecond: 30,
		},
		{
			// 6 fields ending with wildcard - NEW BEHAVIOR: prefer seconds
			// "15 10 1 1 * *" is now [sec=15 min=10 hour=1 dom=1 month=* dow=*] year=any
			name:       "6 fields with wildcard - prefers seconds",
			spec:       "15 10 1 1 * *",
			wantErr:    false,
			from:       time.Date(2024, 12, 1, 2, 0, 0, 0, loc), // After 01:10:15
			wantYear:   2025,                                    // Next is Jan 1, 2025
			wantSecond: 15,                                      // Now treated as seconds!
		},
		{
			// 6 fields ending with number < 100 - treated as dow, not year
			// "30 15 10 1 1 5" is [sec=30 min=15 hour=10 dom=1 month=1 dow=5] year=any
			// With AND logic: Jan 1 must be a Friday. Jan 1, 2027 is the next Friday-Jan-1.
			name:       "6 fields with dow - prefers seconds",
			spec:       "30 15 10 1 1 5",
			wantErr:    false,
			from:       time.Date(2024, 12, 1, 0, 0, 0, 0, loc),
			wantYear:   2027,
			wantSecond: 30,
		},
		{
			// 7 fields with year range - unambiguous
			name:       "7 fields with year range",
			spec:       "0 0 0 1 1 * 2024-2026",
			wantErr:    false,
			from:       time.Date(2023, 6, 1, 0, 0, 0, 0, loc),
			wantYear:   2024,
			wantSecond: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schedule, err := parser.Parse(tt.spec)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Parse(%q) expected error containing %q, got nil", tt.spec, tt.errContain)
					return
				}
				if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("Parse(%q) error = %v, want error containing %q", tt.spec, err, tt.errContain)
				}
				return
			}

			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.spec, err)
			}

			next := schedule.Next(tt.from)
			if next.IsZero() {
				t.Errorf("Next(%v) returned zero time", tt.from)
				return
			}

			if next.Year() != tt.wantYear {
				t.Errorf("Next(%v).Year() = %d, want %d", tt.from, next.Year(), tt.wantYear)
			}

			if next.Second() != tt.wantSecond {
				t.Errorf("Next(%v).Second() = %d, want %d", tt.from, next.Second(), tt.wantSecond)
			}
		})
	}
}

// TestSpecFieldsYearWritten checks that specFields returns no year field
// for a spec that wrote none, and a written one as written, "*" and "?"
// included: a caller that writes the fields back must not add a year the
// spec left out. It also checks the years Parse reads for each: any year
// for one not written or written as "*" or "?".
func TestSpecFieldsYearWritten(t *testing.T) {
	five := Minute | Hour | Dom | Month | Dow
	for _, tt := range []struct {
		opts ParseOption
		spec string
		want string
	}{
		{five | YearOptional, "0 0 * * *", ""},
		{five | YearOptional, "0 0 * * * 2030", "2030"},
		{five | SecondOptional | YearOptional, "0 0 * * *", ""},
		{five | SecondOptional | YearOptional, "0 0 0 * * * 2030", "2030"},
		{five | DowOptional | YearOptional, "0 0 * *", ""},
		{five | Year, "0 0 * * * *", "*"},
		{five | Year, "0 0 * * * ?", "?"},
		{five | Year | YearOptional, "0 0 * * * *", "*"},
		{five | Year | SecondOptional, "0 0 0 * * *", ""}, // six fields: a second, not a year
		{five | Year | SecondOptional, "0 0 0 * * * 2030", "2030"},
		{five, "0 0 * * *", ""},
	} {
		p := NewParser(tt.opts)
		_, _, _, got, err := p.specFields(tt.spec)
		if err != nil {
			t.Errorf("specFields(%q): %v", tt.spec, err)
			continue
		}
		if got != tt.want {
			t.Errorf("specFields(%q) year = %q, want %q", tt.spec, got, tt.want)
		}
		want, _ := getYearField(cmp.Or(tt.want, "*")) // nil: any year
		sch, err := p.Parse(tt.spec)
		s, ok := sch.(*SpecSchedule)
		if err != nil || !ok {
			t.Errorf("Parse(%q) = %T, %v", tt.spec, sch, err)
			continue
		}
		if (s.Year == nil) != (want == nil) || !maps.Equal(s.Year, want) {
			t.Errorf("Parse(%q).Year = %v, want %v", tt.spec, s.Year, want)
		}
	}
}
