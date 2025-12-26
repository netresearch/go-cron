package cron

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

var secondParser = NewParser(Second | Minute | Hour | Dom | Month | DowOptional | Descriptor)

func TestRange(t *testing.T) {
	zero := uint64(0)
	ranges := []struct {
		expr     string
		min, max uint
		expected uint64
		err      string
	}{
		{"5", 0, 7, 1 << 5, ""},
		{"0", 0, 7, 1 << 0, ""},
		{"7", 0, 7, 1 << 7, ""},

		{"5-5", 0, 7, 1 << 5, ""},
		{"5-6", 0, 7, 1<<5 | 1<<6, ""},
		{"5-7", 0, 7, 1<<5 | 1<<6 | 1<<7, ""},

		{"5-6/2", 0, 7, zero, "step (2) must be less than range size (2)"},
		{"5-7/2", 0, 7, 1<<5 | 1<<7, ""},
		{"5-7/1", 0, 7, 1<<5 | 1<<6 | 1<<7, ""},

		{"*", 1, 3, 1<<1 | 1<<2 | 1<<3 | starBit, ""},
		{"*/2", 1, 3, 1<<1 | 1<<3, ""},

		{"5--5", 0, 0, zero, "too many hyphens"},
		{"jan-x", 0, 0, zero, "failed to parse int from"},
		{"2-x", 1, 5, zero, "failed to parse int from"},
		{"*/-12", 0, 0, zero, "negative number"},
		{"*//2", 0, 0, zero, "too many slashes"},
		{"1", 3, 5, zero, "below minimum"},
		{"6", 3, 5, zero, "above maximum"},
		{"5-3", 3, 5, zero, "beyond end of range"},
		{"*/0", 0, 0, zero, "must be a positive number"},

		// Step validation: step must be less than range size
		{"0-5/10", 0, 59, zero, "step (10) must be less than range size (6)"},
		{"0-5/6", 0, 59, zero, "step (6) must be less than range size (6)"},
		{"0-2/5", 0, 59, zero, "step (5) must be less than range size (3)"},
		{"*/60", 0, 59, zero, "step (60) must be less than range size (60)"},
		{"*/100", 0, 59, zero, "step (100) must be less than range size (60)"},
		// Valid step cases (step < rangeSize)
		{"0-10/5", 0, 59, 1<<0 | 1<<5 | 1<<10, ""},
		{"*/30", 0, 59, 1<<0 | 1<<30, ""},
		{"0-59/30", 0, 59, 1<<0 | 1<<30, ""},
		{"0-5/5", 0, 59, 1<<0 | 1<<5, ""},
	}

	for _, c := range ranges {
		actual, err := getRange(c.expr, bounds{c.min, c.max, nil})
		if len(c.err) != 0 && (err == nil || !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%s => expected %v, got %v", c.expr, c.err, err)
		}
		if len(c.err) == 0 && err != nil {
			t.Errorf("%s => unexpected error %v", c.expr, err)
		}
		if actual != c.expected {
			t.Errorf("%s => expected %d, got %d", c.expr, c.expected, actual)
		}
	}
}

func TestField(t *testing.T) {
	fields := []struct {
		expr     string
		min, max uint
		expected uint64
	}{
		{"5", 1, 7, 1 << 5},
		{"5,6", 1, 7, 1<<5 | 1<<6},
		{"5,6,7", 1, 7, 1<<5 | 1<<6 | 1<<7},
		{"1,5-7/2,3", 1, 7, 1<<1 | 1<<5 | 1<<7 | 1<<3},
	}

	for _, c := range fields {
		actual, _ := getField(c.expr, bounds{c.min, c.max, nil})
		if actual != c.expected {
			t.Errorf("%s => expected %d, got %d", c.expr, c.expected, actual)
		}
	}
}

func TestAll(t *testing.T) {
	allBits := []struct {
		r        bounds
		expected uint64
	}{
		{minutes, 0xfffffffffffffff}, // 0-59: 60 ones
		{hours, 0xffffff},            // 0-23: 24 ones
		{dom, 0xfffffffe},            // 1-31: 31 ones, 1 zero
		{months, 0x1ffe},             // 1-12: 12 ones, 1 zero
		{dow, 0xff},                  // 0-7: 8 ones (7 normalized to 0 by parser)
	}

	for _, c := range allBits {
		actual := all(c.r) // all() adds the starBit, so compensate for that..
		if c.expected|starBit != actual {
			t.Errorf("%d-%d/%d => expected %b, got %b",
				c.r.min, c.r.max, 1, c.expected|starBit, actual)
		}
	}
}

func TestBits(t *testing.T) {
	bits := []struct {
		min, max, step uint
		expected       uint64
	}{
		{0, 0, 1, 0x1},
		{1, 1, 1, 0x2},
		{1, 5, 2, 0x2a}, // 101010
		{1, 4, 2, 0xa},  // 1010
	}

	for _, c := range bits {
		actual := getBits(c.min, c.max, c.step)
		if c.expected != actual {
			t.Errorf("%d-%d/%d => expected %b, got %b",
				c.min, c.max, c.step, c.expected, actual)
		}
	}
}

func TestParseScheduleErrors(t *testing.T) {
	tests := []struct{ expr, err string }{
		{"* 5 j * * *", "failed to parse int from"},
		{"@every Xm", "failed to parse duration"},
		{"@unrecognized", "unrecognized descriptor"},
		{"* * * *", "expected 5 to 6 fields"},
		{"", "empty spec string"},
		// @every minimum duration validation (must be >= 1 second)
		{"@every 500ms", "@every duration must be at least 1s"},
		{"@every 100ms", "@every duration must be at least 1s"},
		{"@every 999ms", "@every duration must be at least 1s"},
		{"@every 1ns", "@every duration must be at least 1s"},
	}
	for _, c := range tests {
		actual, err := secondParser.Parse(c.expr)
		if err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%s => expected %v, got %v", c.expr, c.err, err)
		}
		if actual != nil {
			t.Errorf("expected nil schedule on error, got %v", actual)
		}
	}
}

func TestParseSpecLengthLimit(t *testing.T) {
	// Test that specs at exactly the limit work
	atLimit := strings.Repeat("*", MaxSpecLength-10) // Leave room for structure
	_, err := standardParser.Parse(atLimit)
	// Will fail validation but not due to length
	if err != nil && strings.Contains(err.Error(), "spec too long") {
		t.Errorf("spec at limit should not trigger length error")
	}

	// Test that specs over the limit are rejected
	overLimit := strings.Repeat("*", MaxSpecLength+1)
	_, err = standardParser.Parse(overLimit)
	if err == nil {
		t.Error("expected error for spec over length limit")
	}
	if err != nil && !strings.Contains(err.Error(), "spec too long") {
		t.Errorf("expected 'spec too long' error, got: %v", err)
	}

	// Test that normal specs work fine
	_, err = standardParser.Parse("* * * * *")
	if err != nil {
		t.Errorf("normal spec should work: %v", err)
	}
}

func TestParseSchedule(t *testing.T) {
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	entries := []struct {
		parser   Parser
		expr     string
		expected Schedule
	}{
		{secondParser, "0 5 * * * *", every5min(time.Local)},
		{standardParser, "5 * * * *", every5min(time.Local)},
		{secondParser, "CRON_TZ=UTC  0 5 * * * *", every5min(time.UTC)},
		{standardParser, "CRON_TZ=UTC  5 * * * *", every5min(time.UTC)},
		{secondParser, "CRON_TZ=Asia/Tokyo 0 5 * * * *", every5min(tokyo)},
		{secondParser, "@every 5m", ConstantDelaySchedule{5 * time.Minute}},
		{secondParser, "@every 1s", ConstantDelaySchedule{time.Second}},
		{secondParser, "@every 2s", ConstantDelaySchedule{2 * time.Second}},
		{secondParser, "@midnight", midnight(time.Local)},
		{secondParser, "TZ=UTC  @midnight", midnight(time.UTC)},
		{secondParser, "TZ=Asia/Tokyo @midnight", midnight(tokyo)},
		{secondParser, "@yearly", annual(time.Local)},
		{secondParser, "@annually", annual(time.Local)},
		{
			parser: secondParser,
			expr:   "* 5 * * * *",
			expected: &SpecSchedule{
				Second:   all(seconds),
				Minute:   1 << 5,
				Hour:     all(hours),
				Dom:      all(dom),
				Month:    all(months),
				Dow:      allDowNormalized(),
				Year:     nil, // nil = wildcard (any year)
				Location: time.Local,
			},
		},
	}

	for _, c := range entries {
		actual, err := c.parser.Parse(c.expr)
		if err != nil {
			t.Errorf("%s => unexpected error %v", c.expr, err)
		}
		if !reflect.DeepEqual(actual, c.expected) {
			t.Errorf("%s => expected %b, got %b", c.expr, c.expected, actual)
		}
	}
}

func TestOptionalSecondSchedule(t *testing.T) {
	parser := NewParser(SecondOptional | Minute | Hour | Dom | Month | Dow | Descriptor)
	entries := []struct {
		expr     string
		expected Schedule
	}{
		{"0 5 * * * *", every5min(time.Local)},
		{"5 5 * * * *", every5min5s(time.Local)},
		{"5 * * * *", every5min(time.Local)},
	}

	for _, c := range entries {
		actual, err := parser.Parse(c.expr)
		if err != nil {
			t.Errorf("%s => unexpected error %v", c.expr, err)
		}
		if !reflect.DeepEqual(actual, c.expected) {
			t.Errorf("%s => expected %b, got %b", c.expr, c.expected, actual)
		}
	}
}

func TestNormalizeFields(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		options  ParseOption
		expected []string
	}{
		{
			"AllFields_NoOptional",
			[]string{"0", "5", "*", "*", "*", "*"},
			Second | Minute | Hour | Dom | Month | Dow | Descriptor,
			[]string{"0", "5", "*", "*", "*", "*"},
		},
		{
			"AllFields_SecondOptional_Provided",
			[]string{"0", "5", "*", "*", "*", "*"},
			SecondOptional | Minute | Hour | Dom | Month | Dow | Descriptor,
			[]string{"0", "5", "*", "*", "*", "*"},
		},
		{
			"AllFields_SecondOptional_NotProvided",
			[]string{"5", "*", "*", "*", "*"},
			SecondOptional | Minute | Hour | Dom | Month | Dow | Descriptor,
			[]string{"0", "5", "*", "*", "*", "*"},
		},
		{
			"SubsetFields_NoOptional",
			[]string{"5", "15", "*"},
			Hour | Dom | Month,
			[]string{"0", "0", "5", "15", "*", "*"},
		},
		{
			"SubsetFields_DowOptional_Provided",
			[]string{"5", "15", "*", "4"},
			Hour | Dom | Month | DowOptional,
			[]string{"0", "0", "5", "15", "*", "4"},
		},
		{
			"SubsetFields_DowOptional_NotProvided",
			[]string{"5", "15", "*"},
			Hour | Dom | Month | DowOptional,
			[]string{"0", "0", "5", "15", "*", "*"},
		},
		{
			"SubsetFields_SecondOptional_NotProvided",
			[]string{"5", "15", "*"},
			SecondOptional | Hour | Dom | Month,
			[]string{"0", "0", "5", "15", "*", "*"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := normalizeFields(test.input, test.options)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(actual, test.expected) {
				t.Errorf("expected %v, got %v", test.expected, actual)
			}
		})
	}
}

func TestNormalizeFields_Errors(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		options ParseOption
		err     string
	}{
		{
			"TwoOptionals",
			[]string{"0", "5", "*", "*", "*", "*"},
			SecondOptional | Minute | Hour | Dom | Month | DowOptional,
			"",
		},
		{
			"TooManyFields",
			[]string{"0", "5", "*", "*"},
			SecondOptional | Minute | Hour,
			"",
		},
		{
			"NoFields",
			[]string{},
			SecondOptional | Minute | Hour,
			"",
		},
		{
			"TooFewFields",
			[]string{"*"},
			SecondOptional | Minute | Hour,
			"",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := normalizeFields(test.input, test.options)
			if err == nil {
				t.Errorf("expected an error, got none. results: %v", actual)
			}
			if !strings.Contains(err.Error(), test.err) {
				t.Errorf("expected error %q, got %q", test.err, err.Error())
			}
		})
	}
}

func TestStandardSpecSchedule(t *testing.T) {
	entries := []struct {
		expr     string
		expected Schedule
		err      string
	}{
		{
			expr:     "5 * * * *",
			expected: &SpecSchedule{1 << seconds.min, 1 << 5, all(hours), all(dom), all(months), allDowNormalized(), nil, time.Local, 0, nil, nil},
		},
		{
			expr:     "@every 5m",
			expected: ConstantDelaySchedule{time.Duration(5) * time.Minute},
		},
		{
			expr: "5 j * * *",
			err:  "failed to parse int from",
		},
		{
			expr: "* * * *",
			err:  "expected exactly 5 fields",
		},
	}

	for _, c := range entries {
		actual, err := ParseStandard(c.expr)
		if len(c.err) != 0 && (err == nil || !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%s => expected %v, got %v", c.expr, c.err, err)
		}
		if len(c.err) == 0 && err != nil {
			t.Errorf("%s => unexpected error %v", c.expr, err)
		}
		if !reflect.DeepEqual(actual, c.expected) {
			t.Errorf("%s => expected %b, got %b", c.expr, c.expected, actual)
		}
	}
}

func TestNoDescriptorParser(t *testing.T) {
	parser := NewParser(Minute | Hour)
	_, err := parser.Parse("@every 1m")
	if err == nil {
		t.Error("expected an error, got none")
	}
}

func TestNewParserPanicsWithNoFields(t *testing.T) {
	// Test that NewParser panics when called with no fields
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewParser(0) should panic with no fields configured")
		}
	}()
	NewParser(0)
}

func TestNewParserDescriptorOnlyValid(t *testing.T) {
	// Test that NewParser with only Descriptor is valid
	// (even though it only accepts @descriptors)
	parser := NewParser(Descriptor)
	_, err := parser.Parse("@daily")
	if err != nil {
		t.Errorf("expected @daily to parse with Descriptor-only parser, got: %v", err)
	}

	_, err = parser.Parse("* * * * *")
	if err == nil {
		t.Error("expected cron expression to fail with Descriptor-only parser")
	}
}

func TestTryNewParser(t *testing.T) {
	t.Run("returns ErrNoFields for empty options", func(t *testing.T) {
		_, err := TryNewParser(0)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, ErrNoFields) {
			t.Errorf("expected ErrNoFields, got: %v", err)
		}
	})

	t.Run("returns ErrMultipleOptionals for multiple optionals", func(t *testing.T) {
		_, err := TryNewParser(Minute | Hour | Dom | Month | SecondOptional | DowOptional)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, ErrMultipleOptionals) {
			t.Errorf("expected ErrMultipleOptionals, got: %v", err)
		}
	})

	t.Run("succeeds with valid options", func(t *testing.T) {
		parser, err := TryNewParser(Minute | Hour | Dom | Month | Dow)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Verify parser works
		_, err = parser.Parse("0 0 * * *")
		if err != nil {
			t.Errorf("parser should work, got: %v", err)
		}
	})

	t.Run("succeeds with Descriptor only", func(t *testing.T) {
		parser, err := TryNewParser(Descriptor)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Verify parser works
		_, err = parser.Parse("@daily")
		if err != nil {
			t.Errorf("parser should work with @daily, got: %v", err)
		}
	})

	t.Run("succeeds with single optional", func(t *testing.T) {
		parser, err := TryNewParser(Minute | Hour | Dom | Month | DowOptional)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Verify parser works with and without optional field
		_, err = parser.Parse("0 0 * *")
		if err != nil {
			t.Errorf("parser should work without optional dow, got: %v", err)
		}
		_, err = parser.Parse("0 0 * * 1")
		if err != nil {
			t.Errorf("parser should work with optional dow, got: %v", err)
		}
	})
}

func TestMustNewParser(t *testing.T) {
	t.Run("panics with invalid options", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("MustNewParser(0) should panic with no fields configured")
			}
		}()
		MustNewParser(0)
	})

	t.Run("succeeds with valid options", func(t *testing.T) {
		// Should not panic
		parser := MustNewParser(Minute | Hour | Dom | Month | Dow)
		_, err := parser.Parse("0 0 * * *")
		if err != nil {
			t.Errorf("parser should work, got: %v", err)
		}
	})
}

func TestParserWithCache(t *testing.T) {
	t.Run("returns same schedule for repeated parse", func(t *testing.T) {
		parser := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor).WithCache()

		sched1, err1 := parser.Parse("0 * * * *")
		if err1 != nil {
			t.Fatalf("first parse failed: %v", err1)
		}

		sched2, err2 := parser.Parse("0 * * * *")
		if err2 != nil {
			t.Fatalf("second parse failed: %v", err2)
		}

		// Both should return the exact same schedule pointer (cached)
		if sched1 != sched2 {
			t.Error("cached parser should return same schedule instance")
		}
	})

	t.Run("caches errors too", func(t *testing.T) {
		parser := NewParser(Minute | Hour | Dom | Month | Dow).WithCache()

		_, err1 := parser.Parse("invalid spec")
		if err1 == nil {
			t.Fatal("first parse should fail")
		}

		_, err2 := parser.Parse("invalid spec")
		if err2 == nil {
			t.Fatal("second parse should fail")
		}

		// Error messages should match (same cached error)
		if err1.Error() != err2.Error() {
			t.Errorf("cached error mismatch: %v vs %v", err1, err2)
		}
	})

	t.Run("different specs return different schedules", func(t *testing.T) {
		parser := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor).WithCache()

		sched1, _ := parser.Parse("0 * * * *")
		sched2, _ := parser.Parse("30 * * * *")

		if sched1 == sched2 {
			t.Error("different specs should return different schedules")
		}
	})

	t.Run("parser without cache returns new schedules", func(t *testing.T) {
		parser := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor)

		sched1, _ := parser.Parse("0 * * * *")
		sched2, _ := parser.Parse("0 * * * *")

		// Without cache, each parse creates a new schedule
		if sched1 == sched2 {
			t.Error("non-cached parser should return new schedule instances")
		}
	})

	t.Run("concurrent access is safe", func(t *testing.T) {
		parser := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor).WithCache()
		specs := []string{"0 * * * *", "30 * * * *", "@hourly", "@daily"}

		done := make(chan bool, 100)
		for i := 0; i < 100; i++ {
			go func(idx int) {
				_, _ = parser.Parse(specs[idx%len(specs)])
				done <- true
			}(i)
		}

		for i := 0; i < 100; i++ {
			<-done
		}
	})
}

// allDowNormalized returns all DOW bits after normalization (bit 7 mapped to bit 0).
// This matches what the parser produces for wildcard DOW fields.
func allDowNormalized() uint64 {
	return NormalizeDOW(all(dow))
}

func every5min(loc *time.Location) *SpecSchedule {
	return &SpecSchedule{1 << 0, 1 << 5, all(hours), all(dom), all(months), allDowNormalized(), nil, loc, 0, nil, nil}
}

func every5min5s(loc *time.Location) *SpecSchedule {
	return &SpecSchedule{1 << 5, 1 << 5, all(hours), all(dom), all(months), allDowNormalized(), nil, loc, 0, nil, nil}
}

func midnight(loc *time.Location) *SpecSchedule {
	return &SpecSchedule{1, 1, 1, all(dom), all(months), allDowNormalized(), nil, loc, 0, nil, nil}
}

func annual(loc *time.Location) *SpecSchedule {
	return &SpecSchedule{
		Second:   1 << seconds.min,
		Minute:   1 << minutes.min,
		Hour:     1 << hours.min,
		Dom:      1 << dom.min,
		Month:    1 << months.min,
		Dow:      allDowNormalized(),
		Year:     nil, // nil = wildcard (any year)
		Location: loc,
	}
}

// TestTimezoneParsingPanic tests fix for issues #554 and #555
// These bugs caused panics when parsing malformed timezone specs
func TestTimezoneParsingPanic(t *testing.T) {
	// Issue #554: TZ= without space causes slice bounds panic
	// Previously: strings.Index(spec, " ") returns -1, then spec[eq+1 : i] panics
	panicSpecs := []struct {
		name string
		spec string
		err  string
	}{
		{
			name: "TZ_without_space",
			spec: "TZ=0",
			err:  "missing fields after timezone",
		},
		{
			name: "TZ_equals_only",
			spec: "TZ=",
			err:  "missing fields after timezone",
		},
		{
			name: "CRON_TZ_without_space",
			spec: "CRON_TZ=UTC",
			err:  "missing fields after timezone",
		},
		{
			name: "TZ_with_only_spaces",
			spec: "TZ=UTC   ",
			err:  "missing fields after timezone",
		},
		{
			name: "TZ_valid_timezone_no_fields",
			spec: "TZ=America/New_York",
			err:  "missing fields after timezone",
		},
	}

	for _, tc := range panicSpecs {
		t.Run(tc.name, func(t *testing.T) {
			// This should NOT panic - it should return an error
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("ParseStandard(%q) panicked: %v", tc.spec, r)
				}
			}()

			_, err := ParseStandard(tc.spec)
			if err == nil {
				t.Errorf("ParseStandard(%q) expected error, got nil", tc.spec)
				return
			}
			if !strings.Contains(err.Error(), tc.err) {
				t.Errorf("ParseStandard(%q) error = %q, want error containing %q", tc.spec, err.Error(), tc.err)
			}
		})
	}
}

// TestTimezoneValidParsing ensures valid timezone specs still work
func TestTimezoneValidParsing(t *testing.T) {
	validSpecs := []string{
		"TZ=UTC * * * * *",
		"CRON_TZ=UTC * * * * *",
		"TZ=America/New_York 0 5 * * *",
		"CRON_TZ=Asia/Tokyo 30 4 * * *",
	}

	for _, spec := range validSpecs {
		t.Run(spec, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("ParseStandard(%q) panicked: %v", spec, r)
				}
			}()

			_, err := ParseStandard(spec)
			if err != nil {
				t.Errorf("ParseStandard(%q) unexpected error: %v", spec, err)
			}
		})
	}
}

// TestTimezoneValidation tests security validation of timezone strings (issue #6)
func TestTimezoneValidation(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		wantErr string
	}{
		// Valid timezones should pass validation (may still fail LoadLocation)
		{"valid UTC", "TZ=UTC * * * * *", ""},
		{"valid America/New_York", "TZ=America/New_York * * * * *", ""},
		{"valid Etc/GMT+5", "TZ=Etc/GMT+5 * * * * *", ""},
		{"valid Europe/Isle_of_Man", "TZ=Europe/Isle_of_Man * * * * *", ""},
		{"valid with colon", "TZ=Etc/GMT+0:00 * * * * *", ""},

		// Invalid: path traversal attempts
		{"path traversal dots", "TZ=../../../etc/passwd * * * * *", "invalid character"},
		{"path traversal encoded", "TZ=%2e%2e%2f * * * * *", "invalid character"},

		// Invalid: special characters
		{"null byte", "TZ=UTC\x00evil * * * * *", "invalid character"},
		{"space in name", "TZ=America/New York * * * * *", "unknown time zone"}, // space splits, leaving "America/New"
		{"newline", "TZ=UTC\nevil * * * * *", "invalid character"},
		{"semicolon", "TZ=UTC;evil * * * * *", "invalid character"},
		{"backtick", "TZ=UTC`evil * * * * *", "invalid character"},
		{"dollar sign", "TZ=UTC$HOME * * * * *", "invalid character"},

		// Invalid: empty timezone
		{"empty timezone", "TZ= * * * * *", "empty timezone"},

		// Invalid: too long timezone (65+ chars)
		{"too long timezone", "TZ=" + strings.Repeat("A", 65) + " * * * * *", "too long"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseStandard(tt.spec)
			if tt.wantErr == "" {
				// Some valid format timezones may not exist on all systems
				// but they should pass validation (only fail LoadLocation)
				if err != nil && strings.Contains(err.Error(), "invalid") {
					t.Errorf("ParseStandard(%q) validation error: %v", tt.spec, err)
				}
			} else {
				if err == nil {
					t.Errorf("ParseStandard(%q) expected error containing %q, got nil", tt.spec, tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("ParseStandard(%q) error = %v, want error containing %q", tt.spec, err, tt.wantErr)
				}
			}
		})
	}
}

// TestWithMaxSearchYears tests the WithMaxSearchYears parser option
func TestParserWithMaxSearchYears(t *testing.T) {
	// Create a schedule for Feb 30 (impossible date - will never match)
	impossibleSpec := "0 0 30 2 *"

	// Test with default (5 years) - should return zero time
	defaultParser := NewParser(Minute | Hour | Dom | Month | Dow)
	sched, err := defaultParser.Parse(impossibleSpec)
	if err != nil {
		t.Fatalf("Parse(%q) unexpected error: %v", impossibleSpec, err)
	}

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	next := sched.Next(now)
	if !next.IsZero() {
		t.Errorf("Default parser: Next() for impossible schedule should return zero time, got %v", next)
	}

	// Test with custom search years (1 year) - should also return zero time
	shortParser := NewParser(Minute | Hour | Dom | Month | Dow).WithMaxSearchYears(1)
	sched, err = shortParser.Parse(impossibleSpec)
	if err != nil {
		t.Fatalf("Parse(%q) unexpected error: %v", impossibleSpec, err)
	}

	next = sched.Next(now)
	if !next.IsZero() {
		t.Errorf("Short parser: Next() for impossible schedule should return zero time, got %v", next)
	}

	// Test with longer search years (10 years) - still impossible
	longParser := NewParser(Minute | Hour | Dom | Month | Dow).WithMaxSearchYears(10)
	sched, err = longParser.Parse(impossibleSpec)
	if err != nil {
		t.Fatalf("Parse(%q) unexpected error: %v", impossibleSpec, err)
	}

	next = sched.Next(now)
	if !next.IsZero() {
		t.Errorf("Long parser: Next() for impossible schedule should return zero time, got %v", next)
	}

	// Test that zero value falls back to default (5)
	zeroParser := NewParser(Minute | Hour | Dom | Month | Dow).WithMaxSearchYears(0)
	sched, err = zeroParser.Parse(impossibleSpec)
	if err != nil {
		t.Fatalf("Parse(%q) unexpected error: %v", impossibleSpec, err)
	}

	next = sched.Next(now)
	if !next.IsZero() {
		t.Errorf("Zero parser: Next() for impossible schedule should return zero time, got %v", next)
	}

	// Verify MaxSearchYears is set on SpecSchedule
	// Zero is passed through (fallback happens in Next())
	// This is intentional to allow differentiation between "not set" and "explicitly set to default"
	specSched := sched.(*SpecSchedule)
	if specSched.MaxSearchYears != 0 {
		t.Errorf("Expected MaxSearchYears to be 0 (passed through), got %d", specSched.MaxSearchYears)
	}

	// Test with a valid schedule to verify it still works
	validSpec := "0 0 1 1 *" // Jan 1st at midnight
	longParser = NewParser(Minute | Hour | Dom | Month | Dow).WithMaxSearchYears(10)
	sched, err = longParser.Parse(validSpec)
	if err != nil {
		t.Fatalf("Parse(%q) unexpected error: %v", validSpec, err)
	}

	// Should find Jan 1st within 10 years
	next = sched.Next(now)
	if next.IsZero() {
		t.Error("Long parser: Next() for Jan 1st should find a valid time")
	}
	// Verify it's Jan 1st of 2025 (next occurrence after Jan 1, 2024)
	if next.Month() != time.January || next.Day() != 1 || next.Year() != 2025 {
		t.Errorf("Long parser: Expected Jan 1, 2025, got %v", next)
	}
}

// TestSundayAs7 tests that 7 is accepted as an alias for Sunday (0) in DOW field.
func TestSundayAs7(t *testing.T) {
	parser := NewParser(Minute | Hour | Dom | Month | Dow)

	tests := []struct {
		name string
		spec string
	}{
		{"Sunday as 0", "0 0 * * 0"},
		{"Sunday as 7", "0 0 * * 7"},
		{"Range ending with 7", "0 0 * * 5-7"},
		{"Range 0-7 (all week)", "0 0 * * 0-7"},
		{"List with 7", "0 0 * * 1,3,7"},
		{"Only 7", "0 0 * * 7"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sched, err := parser.Parse(tt.spec)
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.spec, err)
			}
			if sched == nil {
				t.Fatalf("Parse(%q) returned nil schedule", tt.spec)
			}
		})
	}

	// Verify Sunday as 0 and Sunday as 7 produce equivalent schedules
	sched0, _ := parser.Parse("0 0 * * 0")
	sched7, _ := parser.Parse("0 0 * * 7")

	spec0 := sched0.(*SpecSchedule)
	spec7 := sched7.(*SpecSchedule)

	if spec0.Dow != spec7.Dow {
		t.Errorf("Sunday as 0 and 7 should produce same DOW bits: 0=%b, 7=%b", spec0.Dow, spec7.Dow)
	}

	// Both should match Sunday
	saturday := time.Date(2024, 1, 6, 0, 0, 0, 0, time.UTC) // Jan 6, 2024 is Saturday

	next0 := sched0.Next(saturday)
	next7 := sched7.Next(saturday)

	if !next0.Equal(next7) {
		t.Errorf("Schedules should find same next Sunday: 0=%v, 7=%v", next0, next7)
	}
	if next0.Weekday() != time.Sunday {
		t.Errorf("Expected Sunday, got %v", next0.Weekday())
	}
}

// TestSundayAs7Range tests that ranges involving 7 work correctly.
func TestSundayAs7Range(t *testing.T) {
	parser := NewParser(Minute | Hour | Dom | Month | Dow)

	// "5-7" should match Fri, Sat, Sun
	sched, err := parser.Parse("0 0 * * 5-7")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	spec := sched.(*SpecSchedule)

	// Should have bits for Fri(5), Sat(6), Sun(0)
	expectedBits := uint64(1<<5 | 1<<6 | 1) // Fri, Sat, Sun(normalized to 0)
	// Also need to account for starBit if using wildcard in other fields
	actualDowBits := spec.Dow &^ starBit // Remove starBit for comparison

	if actualDowBits != expectedBits {
		t.Errorf("5-7 should set bits for Fri,Sat,Sun(0): got %b, want %b", actualDowBits, expectedBits)
	}

	// Test actual scheduling
	thursday := time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC) // Thursday
	next := sched.Next(thursday)
	if next.Weekday() != time.Friday {
		t.Errorf("Next after Thursday should be Friday, got %v", next.Weekday())
	}
}

// =============================================================================
// Extended Cron Syntax Tests (#n, #L, L, L-n, nW, LW)
// =============================================================================

func TestExtendedDowNth(t *testing.T) {
	// Parser with DowNth enabled
	parser := NewParser(Minute | Hour | Dom | Month | Dow | DowNth)

	tests := []struct {
		name    string
		expr    string
		wantErr bool
		checkFn func(*testing.T, *SpecSchedule)
	}{
		{
			name: "FRI#3 - third Friday",
			expr: "0 0 * * FRI#3",
			checkFn: func(t *testing.T, s *SpecSchedule) {
				if len(s.DowConstraints) != 1 {
					t.Fatalf("expected 1 constraint, got %d", len(s.DowConstraints))
				}
				c := s.DowConstraints[0]
				if c.Weekday != 5 || c.N != 3 {
					t.Errorf("expected FRI(5)#3, got weekday=%d n=%d", c.Weekday, c.N)
				}
			},
		},
		{
			name: "5#2 - second Friday (numeric)",
			expr: "0 0 * * 5#2",
			checkFn: func(t *testing.T, s *SpecSchedule) {
				if len(s.DowConstraints) != 1 {
					t.Fatalf("expected 1 constraint, got %d", len(s.DowConstraints))
				}
				c := s.DowConstraints[0]
				if c.Weekday != 5 || c.N != 2 {
					t.Errorf("expected 5#2, got weekday=%d n=%d", c.Weekday, c.N)
				}
			},
		},
		{
			name: "MON#1 - first Monday",
			expr: "0 0 * * MON#1",
			checkFn: func(t *testing.T, s *SpecSchedule) {
				c := s.DowConstraints[0]
				if c.Weekday != 1 || c.N != 1 {
					t.Errorf("expected MON(1)#1, got weekday=%d n=%d", c.Weekday, c.N)
				}
			},
		},
		{
			name:    "FRI#6 - invalid (max is 5)",
			expr:    "0 0 * * FRI#6",
			wantErr: true,
		},
		{
			name:    "FRI#0 - invalid (min is 1)",
			expr:    "0 0 * * FRI#0",
			wantErr: true,
		},
		{
			name:    "FRI# - missing occurrence",
			expr:    "0 0 * * FRI#",
			wantErr: true,
		},
		{
			name:    "#3 - missing weekday",
			expr:    "0 0 * * #3",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sched, err := parser.Parse(tc.expr)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			spec := sched.(*SpecSchedule)
			if tc.checkFn != nil {
				tc.checkFn(t, spec)
			}
		})
	}
}

func TestExtendedDowLast(t *testing.T) {
	// Parser with DowLast enabled
	parser := NewParser(Minute | Hour | Dom | Month | Dow | DowLast)

	tests := []struct {
		name    string
		expr    string
		wantErr bool
		checkFn func(*testing.T, *SpecSchedule)
	}{
		{
			name: "FRI#L - last Friday",
			expr: "0 0 * * FRI#L",
			checkFn: func(t *testing.T, s *SpecSchedule) {
				if len(s.DowConstraints) != 1 {
					t.Fatalf("expected 1 constraint, got %d", len(s.DowConstraints))
				}
				c := s.DowConstraints[0]
				if c.Weekday != 5 || c.N != -1 {
					t.Errorf("expected FRI(5)#L(-1), got weekday=%d n=%d", c.Weekday, c.N)
				}
			},
		},
		{
			name: "0#L - last Sunday (numeric)",
			expr: "0 0 * * 0#L",
			checkFn: func(t *testing.T, s *SpecSchedule) {
				c := s.DowConstraints[0]
				if c.Weekday != 0 || c.N != -1 {
					t.Errorf("expected 0#L, got weekday=%d n=%d", c.Weekday, c.N)
				}
			},
		},
		{
			name: "fri#l - lowercase",
			expr: "0 0 * * fri#l",
			checkFn: func(t *testing.T, s *SpecSchedule) {
				c := s.DowConstraints[0]
				if c.Weekday != 5 || c.N != -1 {
					t.Errorf("expected fri#l, got weekday=%d n=%d", c.Weekday, c.N)
				}
			},
		},
		{
			name: "7#L - last Sunday (Sunday as 7, normalized to 0)",
			expr: "0 0 * * 7#L",
			checkFn: func(t *testing.T, s *SpecSchedule) {
				c := s.DowConstraints[0]
				// Weekday 7 should be normalized to 0 (Sunday)
				if c.Weekday != 0 || c.N != -1 {
					t.Errorf("expected 7#L normalized to weekday=0, got weekday=%d n=%d", c.Weekday, c.N)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sched, err := parser.Parse(tc.expr)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			spec := sched.(*SpecSchedule)
			if tc.checkFn != nil {
				tc.checkFn(t, spec)
			}
		})
	}
}

func TestExtendedDowCombined(t *testing.T) {
	// Parser with both DowNth and DowLast enabled
	parser := NewParser(Minute | Hour | Dom | Month | Dow | DowNth | DowLast)

	// Test combined expression: MON,FRI#3,SUN#L
	sched, err := parser.Parse("0 0 * * MON,FRI#3,SUN#L")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	spec := sched.(*SpecSchedule)

	// Should have Monday in bitmask
	if spec.Dow&(1<<1) == 0 {
		t.Error("Monday should be in bitmask")
	}

	// Should have 2 constraints
	if len(spec.DowConstraints) != 2 {
		t.Fatalf("expected 2 constraints, got %d", len(spec.DowConstraints))
	}

	// Find FRI#3 and SUN#L
	var hasFri3, hasSunL bool
	for _, c := range spec.DowConstraints {
		if c.Weekday == 5 && c.N == 3 {
			hasFri3 = true
		}
		if c.Weekday == 0 && c.N == -1 {
			hasSunL = true
		}
	}
	if !hasFri3 {
		t.Error("missing FRI#3 constraint")
	}
	if !hasSunL {
		t.Error("missing SUN#L constraint")
	}
}

func TestExtendedDomL(t *testing.T) {
	// Parser with DomL enabled
	parser := NewParser(Minute | Hour | Dom | Month | Dow | DomL)

	tests := []struct {
		name    string
		expr    string
		wantErr bool
		checkFn func(*testing.T, *SpecSchedule)
	}{
		{
			name: "L - last day of month",
			expr: "0 0 L * *",
			checkFn: func(t *testing.T, s *SpecSchedule) {
				if len(s.DomConstraints) != 1 {
					t.Fatalf("expected 1 constraint, got %d", len(s.DomConstraints))
				}
				c := s.DomConstraints[0]
				if c.Type != DomLast {
					t.Errorf("expected DomLast type, got %d", c.Type)
				}
			},
		},
		{
			name: "L-3 - third from last day",
			expr: "0 0 L-3 * *",
			checkFn: func(t *testing.T, s *SpecSchedule) {
				if len(s.DomConstraints) != 1 {
					t.Fatalf("expected 1 constraint, got %d", len(s.DomConstraints))
				}
				c := s.DomConstraints[0]
				if c.Type != DomLastOffset || c.N != 3 {
					t.Errorf("expected DomLastOffset with N=3, got type=%d n=%d", c.Type, c.N)
				}
			},
		},
		{
			name: "l-1 - lowercase",
			expr: "0 0 l-1 * *",
			checkFn: func(t *testing.T, s *SpecSchedule) {
				c := s.DomConstraints[0]
				if c.Type != DomLastOffset || c.N != 1 {
					t.Errorf("expected DomLastOffset with N=1, got type=%d n=%d", c.Type, c.N)
				}
			},
		},
		{
			name:    "L-0 - invalid offset",
			expr:    "0 0 L-0 * *",
			wantErr: true,
		},
		{
			name:    "L-31 - offset too large",
			expr:    "0 0 L-31 * *",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sched, err := parser.Parse(tc.expr)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			spec := sched.(*SpecSchedule)
			if tc.checkFn != nil {
				tc.checkFn(t, spec)
			}
		})
	}
}

func TestExtendedDomW(t *testing.T) {
	// Parser with DomW enabled
	parser := NewParser(Minute | Hour | Dom | Month | Dow | DomW)

	tests := []struct {
		name    string
		expr    string
		wantErr bool
		checkFn func(*testing.T, *SpecSchedule)
	}{
		{
			name: "15W - nearest weekday to 15th",
			expr: "0 0 15W * *",
			checkFn: func(t *testing.T, s *SpecSchedule) {
				if len(s.DomConstraints) != 1 {
					t.Fatalf("expected 1 constraint, got %d", len(s.DomConstraints))
				}
				c := s.DomConstraints[0]
				if c.Type != DomNearestWeekday || c.N != 15 {
					t.Errorf("expected DomNearestWeekday with N=15, got type=%d n=%d", c.Type, c.N)
				}
			},
		},
		{
			name: "1w - lowercase",
			expr: "0 0 1w * *",
			checkFn: func(t *testing.T, s *SpecSchedule) {
				c := s.DomConstraints[0]
				if c.Type != DomNearestWeekday || c.N != 1 {
					t.Errorf("expected DomNearestWeekday with N=1, got type=%d n=%d", c.Type, c.N)
				}
			},
		},
		{
			name: "LW - last weekday of month",
			expr: "0 0 LW * *",
			checkFn: func(t *testing.T, s *SpecSchedule) {
				if len(s.DomConstraints) != 1 {
					t.Fatalf("expected 1 constraint, got %d", len(s.DomConstraints))
				}
				c := s.DomConstraints[0]
				if c.Type != DomLastWeekday {
					t.Errorf("expected DomLastWeekday type, got %d", c.Type)
				}
			},
		},
		{
			name:    "0W - invalid day",
			expr:    "0 0 0W * *",
			wantErr: true,
		},
		{
			name:    "32W - day too large",
			expr:    "0 0 32W * *",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sched, err := parser.Parse(tc.expr)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			spec := sched.(*SpecSchedule)
			if tc.checkFn != nil {
				tc.checkFn(t, spec)
			}
		})
	}
}

func TestExtendedDomCombined(t *testing.T) {
	// Parser with both DomL and DomW enabled
	parser := NewParser(Minute | Hour | Dom | Month | Dow | DomL | DomW)

	// Test combined expression: 1,15,L
	sched, err := parser.Parse("0 0 1,15,L * *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	spec := sched.(*SpecSchedule)

	// Should have 1 and 15 in bitmask
	if spec.Dom&(1<<1) == 0 {
		t.Error("day 1 should be in bitmask")
	}
	if spec.Dom&(1<<15) == 0 {
		t.Error("day 15 should be in bitmask")
	}

	// Should have L constraint
	if len(spec.DomConstraints) != 1 {
		t.Fatalf("expected 1 constraint, got %d", len(spec.DomConstraints))
	}
	if spec.DomConstraints[0].Type != DomLast {
		t.Error("expected DomLast constraint")
	}
}

func TestExtendedFlag(t *testing.T) {
	// Parser with Extended flag (enables all extended syntax)
	parser := NewParser(Minute | Hour | Dom | Month | Dow | Extended)

	// Should accept all extended syntax
	tests := []string{
		"0 0 * * FRI#3", // DowNth
		"0 0 * * MON#L", // DowLast
		"0 0 L * *",     // DomL
		"0 0 L-5 * *",   // DomL
		"0 0 15W * *",   // DomW
		"0 0 LW * *",    // DomW
	}

	for _, expr := range tests {
		t.Run(expr, func(t *testing.T) {
			_, err := parser.Parse(expr)
			if err != nil {
				t.Errorf("Extended parser should accept %q, got error: %v", expr, err)
			}
		})
	}
}

func TestExtendedSyntaxDisabledByDefault(t *testing.T) {
	// Standard parser without extended flags
	parser := NewParser(Minute | Hour | Dom | Month | Dow)

	// Should reject extended syntax
	tests := []struct {
		expr string
		desc string
	}{
		{"0 0 * * FRI#3", "DowNth"},
		{"0 0 * * FRI#L", "DowLast"},
		{"0 0 L * *", "DomL"},
		{"0 0 15W * *", "DomW"},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			_, err := parser.Parse(tc.expr)
			if err == nil {
				t.Errorf("standard parser should reject %q (%s)", tc.expr, tc.desc)
			}
		})
	}
}

func TestExtendedDowScheduling(t *testing.T) {
	// Test actual scheduling with DOW constraints
	parser := NewParser(Minute | Hour | Dom | Month | Dow | DowNth | DowLast)

	// FRI#3 - third Friday of month
	sched, err := parser.Parse("0 12 * * FRI#3")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Start from Jan 1, 2024 (Monday)
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	next := sched.Next(start)

	// Third Friday of Jan 2024 is Jan 19
	expected := time.Date(2024, 1, 19, 12, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("FRI#3: expected %v, got %v", expected, next)
	}

	// Next occurrence should be Feb 16, 2024 (third Friday of Feb)
	next = sched.Next(next)
	expected = time.Date(2024, 2, 16, 12, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("FRI#3 next: expected %v, got %v", expected, next)
	}
}

func TestExtendedDowLastScheduling(t *testing.T) {
	parser := NewParser(Minute | Hour | Dom | Month | Dow | DowLast)

	// FRI#L - last Friday of month
	sched, err := parser.Parse("0 12 * * FRI#L")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Start from Jan 1, 2024
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	next := sched.Next(start)

	// Last Friday of Jan 2024 is Jan 26
	expected := time.Date(2024, 1, 26, 12, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("FRI#L: expected %v, got %v", expected, next)
	}

	// Next occurrence should be Feb 23, 2024 (last Friday of Feb)
	next = sched.Next(next)
	expected = time.Date(2024, 2, 23, 12, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("FRI#L next: expected %v, got %v", expected, next)
	}
}

func TestExtendedDomLastScheduling(t *testing.T) {
	parser := NewParser(Minute | Hour | Dom | Month | Dow | DomL)

	// L - last day of month
	sched, err := parser.Parse("0 12 L * *")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Start from Jan 15, 2024
	start := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	next := sched.Next(start)

	// Last day of Jan 2024 is Jan 31
	expected := time.Date(2024, 1, 31, 12, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("L: expected %v, got %v", expected, next)
	}

	// Next occurrence should be Feb 29, 2024 (leap year)
	next = sched.Next(next)
	expected = time.Date(2024, 2, 29, 12, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("L next (Feb leap): expected %v, got %v", expected, next)
	}
}

func TestExtendedDomLastOffsetScheduling(t *testing.T) {
	parser := NewParser(Minute | Hour | Dom | Month | Dow | DomL)

	// L-3 - third from last day
	sched, err := parser.Parse("0 12 L-3 * *")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Start from Jan 15, 2024
	start := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	next := sched.Next(start)

	// Third from last of Jan (31 days) is Jan 28
	expected := time.Date(2024, 1, 28, 12, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("L-3: expected %v, got %v", expected, next)
	}

	// Next occurrence: Feb 2024 has 29 days (leap year), so L-3 = Feb 26
	next = sched.Next(next)
	expected = time.Date(2024, 2, 26, 12, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("L-3 next (Feb leap): expected %v, got %v", expected, next)
	}
}

func TestExtendedDomNearestWeekdayScheduling(t *testing.T) {
	parser := NewParser(Minute | Hour | Dom | Month | Dow | DomW)

	tests := []struct {
		name     string
		expr     string
		start    time.Time
		expected time.Time
	}{
		{
			// 15W when 15th is Monday (weekday) - should be 15th
			name:     "15W on weekday",
			expr:     "0 12 15W * *",
			start:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), // Jan 2024, 15th is Monday
			expected: time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
		},
		{
			// 15W when 15th is Saturday - should be Friday 14th
			name:     "15W on Saturday",
			expr:     "0 12 15W * *",
			start:    time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC), // June 2024, 15th is Saturday
			expected: time.Date(2024, 6, 14, 12, 0, 0, 0, time.UTC),
		},
		{
			// 15W when 15th is Sunday - should be Monday 16th
			name:     "15W on Sunday",
			expr:     "0 12 15W * *",
			start:    time.Date(2024, 9, 1, 0, 0, 0, 0, time.UTC), // Sep 2024, 15th is Sunday
			expected: time.Date(2024, 9, 16, 12, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sched, err := parser.Parse(tc.expr)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			next := sched.Next(tc.start)
			if !next.Equal(tc.expected) {
				t.Errorf("%s: expected %v, got %v", tc.name, tc.expected, next)
			}
		})
	}
}

func TestExtendedDomLastWeekdayScheduling(t *testing.T) {
	parser := NewParser(Minute | Hour | Dom | Month | Dow | DomW)

	// LW - last weekday of month
	sched, err := parser.Parse("0 12 LW * *")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	tests := []struct {
		name     string
		start    time.Time
		expected time.Time
	}{
		{
			// Jan 2024 ends on Wednesday (31st)
			name:     "Jan 2024 ends on Wednesday",
			start:    time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			expected: time.Date(2024, 1, 31, 12, 0, 0, 0, time.UTC),
		},
		{
			// Feb 2024 ends on Thursday (29th - leap year)
			name:     "Feb 2024 ends on Thursday",
			start:    time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
			expected: time.Date(2024, 2, 29, 12, 0, 0, 0, time.UTC),
		},
		{
			// March 2024 ends on Sunday (31st), last weekday is Friday 29th
			name:     "March 2024 ends on Sunday",
			start:    time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
			expected: time.Date(2024, 3, 29, 12, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			next := sched.Next(tc.start)
			if !next.Equal(tc.expected) {
				t.Errorf("%s: expected %v, got %v (weekday: %v)", tc.name, tc.expected, next, next.Weekday())
			}
		})
	}
}

func TestExtendedEdgeCases(t *testing.T) {
	t.Run("31W in February - skip month (no day 31)", func(t *testing.T) {
		// 31W in February: Feb only has 28/29 days, no day 31 exists
		// Behavior: skip February, run in next month that has a 31st
		// Use LW instead if you want "last weekday of every month"
		parser := NewParser(Minute | Hour | Dom | Month | Dow | DomW)
		sched, err := parser.Parse("0 12 31W * *")
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Start in February 2024 - should skip to March
		start := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
		next := sched.Next(start)

		// Feb has no 31st → skip to March
		// March 31, 2024 is Sunday → nearest weekday is Friday March 29
		// (Monday would be April 1, which crosses month boundary, so Friday is chosen)
		expected := time.Date(2024, 3, 29, 12, 0, 0, 0, time.UTC)
		if !next.Equal(expected) {
			t.Errorf("31W from Feb: expected %v (March 29), got %v", expected, next)
		}
	})

	t.Run("L-30 in February - large offset produces no match", func(t *testing.T) {
		// L-30 means "30 days before last day of month"
		// In February (28 days): 28 - 30 = -2 (invalid, no match)
		// Schedule should skip to March
		parser := NewParser(Minute | Hour | Dom | Month | Dow | DomL)
		sched, err := parser.Parse("0 12 L-30 * *")
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Start in February 2024
		start := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
		next := sched.Next(start)

		// Feb has 29 days in 2024: L-30 = 29-30 = -1 (no match)
		// March has 31 days: L-30 = 31-30 = 1 (March 1st)
		expected := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
		if !next.Equal(expected) {
			t.Errorf("L-30 from Feb: expected %v (March 1), got %v", expected, next)
		}
	})

	t.Run("FRI#5 in month with only 4 Fridays", func(t *testing.T) {
		// February 2024 has only 4 Fridays (2nd, 9th, 16th, 23rd)
		// FRI#5 should not match in February, should skip to a month with 5 Fridays
		parser := NewParser(Minute | Hour | Dom | Month | Dow | DowNth)
		sched, err := parser.Parse("0 12 * * FRI#5")
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Start in February 2024
		start := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
		next := sched.Next(start)

		// Feb 2024 has 4 Fridays, March 2024 has 5 Fridays (1st, 8th, 15th, 22nd, 29th)
		// 5th Friday of March 2024 is March 29th
		expected := time.Date(2024, 3, 29, 12, 0, 0, 0, time.UTC)
		if !next.Equal(expected) {
			t.Errorf("FRI#5 from Feb 2024: expected %v (5th Friday of March), got %v", expected, next)
		}
	})

	t.Run("L-27 boundary - minimum offset that works in all months", func(t *testing.T) {
		// L-27 in February (28 days): 28-27 = 1 (valid)
		// This is the largest offset guaranteed to work in all months
		parser := NewParser(Minute | Hour | Dom | Month | Dow | DomL)
		sched, err := parser.Parse("0 12 L-27 * *")
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Non-leap year February has 28 days: L-27 = day 1
		start := time.Date(2023, 2, 1, 0, 0, 0, 0, time.UTC)
		next := sched.Next(start)

		expected := time.Date(2023, 2, 1, 12, 0, 0, 0, time.UTC)
		if !next.Equal(expected) {
			t.Errorf("L-27 in Feb 2023: expected %v, got %v", expected, next)
		}
	})

	t.Run("L-28 in non-leap February - no match", func(t *testing.T) {
		// L-28 in non-leap February (28 days): 28-28 = 0 (invalid day)
		// Should skip to March
		parser := NewParser(Minute | Hour | Dom | Month | Dow | DomL)
		sched, err := parser.Parse("0 12 L-28 * *")
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Non-leap year February 2023 has 28 days: L-28 = 0 (no match)
		start := time.Date(2023, 2, 1, 0, 0, 0, 0, time.UTC)
		next := sched.Next(start)

		// March has 31 days: L-28 = 3 (March 3rd)
		expected := time.Date(2023, 3, 3, 12, 0, 0, 0, time.UTC)
		if !next.Equal(expected) {
			t.Errorf("L-28 from Feb 2023: expected %v (March 3), got %v", expected, next)
		}
	})
}

func TestFullParser(t *testing.T) {
	parser := FullParser()

	t.Run("standard 5-field cron", func(t *testing.T) {
		sched, err := parser.Parse("30 14 * * *")
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		next := sched.Next(start)

		expected := time.Date(2025, 1, 1, 14, 30, 0, 0, time.UTC)
		if !next.Equal(expected) {
			t.Errorf("5-field cron: expected %v, got %v", expected, next)
		}
	})

	t.Run("6-field cron with seconds", func(t *testing.T) {
		sched, err := parser.Parse("15 30 14 * * *")
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		next := sched.Next(start)

		expected := time.Date(2025, 1, 1, 14, 30, 15, 0, time.UTC)
		if !next.Equal(expected) {
			t.Errorf("6-field cron with seconds: expected %v, got %v", expected, next)
		}
	})

	t.Run("6-field cron with year", func(t *testing.T) {
		sched, err := parser.Parse("30 14 25 12 * 2025")
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		next := sched.Next(start)

		expected := time.Date(2025, 12, 25, 14, 30, 0, 0, time.UTC)
		if !next.Equal(expected) {
			t.Errorf("6-field cron with year: expected %v, got %v", expected, next)
		}
	})

	t.Run("7-field cron with seconds and year", func(t *testing.T) {
		sched, err := parser.Parse("0 0 0 1 1 * 2030")
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		next := sched.Next(start)

		expected := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
		if !next.Equal(expected) {
			t.Errorf("7-field cron: expected %v, got %v", expected, next)
		}
	})

	t.Run("descriptors", func(t *testing.T) {
		testCases := []struct {
			desc string
		}{
			{"@yearly"},
			{"@annually"},
			{"@monthly"},
			{"@weekly"},
			{"@daily"},
			{"@midnight"},
			{"@hourly"},
			{"@every 1h"},
			{"@every 30m"},
		}

		for _, tc := range testCases {
			t.Run(tc.desc, func(t *testing.T) {
				_, err := parser.Parse(tc.desc)
				if err != nil {
					t.Errorf("descriptor %s: unexpected error: %v", tc.desc, err)
				}
			})
		}
	})

	t.Run("hash expressions", func(t *testing.T) {
		// Hash expressions require a hash key for deterministic scheduling
		parserWithHash := parser.WithHashKey("test-job")
		sched, err := parserWithHash.Parse("H H * * *")
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		if sched == nil {
			t.Error("hash expression: schedule is nil")
		}
	})

	t.Run("extended DOW syntax - nth weekday", func(t *testing.T) {
		// FRI#3 = third Friday
		sched, err := parser.Parse("0 12 * * FRI#3")
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Jan 2025: Third Friday is Jan 17
		start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		next := sched.Next(start)

		expected := time.Date(2025, 1, 17, 12, 0, 0, 0, time.UTC)
		if !next.Equal(expected) {
			t.Errorf("FRI#3: expected %v, got %v", expected, next)
		}
	})

	t.Run("extended DOW syntax - last weekday", func(t *testing.T) {
		// FRI#L = last Friday
		sched, err := parser.Parse("0 12 * * FRI#L")
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Jan 2025: Last Friday is Jan 31
		start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		next := sched.Next(start)

		expected := time.Date(2025, 1, 31, 12, 0, 0, 0, time.UTC)
		if !next.Equal(expected) {
			t.Errorf("FRI#L: expected %v, got %v", expected, next)
		}
	})

	t.Run("extended DOM syntax - last day", func(t *testing.T) {
		sched, err := parser.Parse("0 12 L * *")
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Jan 2025: Last day is Jan 31
		start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		next := sched.Next(start)

		expected := time.Date(2025, 1, 31, 12, 0, 0, 0, time.UTC)
		if !next.Equal(expected) {
			t.Errorf("L: expected %v, got %v", expected, next)
		}
	})

	t.Run("extended DOM syntax - nearest weekday", func(t *testing.T) {
		// 15W = nearest weekday to the 15th
		sched, err := parser.Parse("0 12 15W * *")
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Jan 2025: 15th is Wednesday, so it matches directly
		start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		next := sched.Next(start)

		expected := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
		if !next.Equal(expected) {
			t.Errorf("15W: expected %v, got %v", expected, next)
		}
	})

	t.Run("year range", func(t *testing.T) {
		sched, err := parser.Parse("0 0 1 1 * 2025-2027")
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		next := sched.Next(start)
		expected := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		if !next.Equal(expected) {
			t.Errorf("year range (first): expected %v, got %v", expected, next)
		}

		// Next occurrence after 2025
		next = sched.Next(next)
		expected = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		if !next.Equal(expected) {
			t.Errorf("year range (second): expected %v, got %v", expected, next)
		}
	})

	t.Run("specific date and time - Christmas 2025", func(t *testing.T) {
		sched, err := parser.Parse("0 30 14 25 12 * 2025")
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		next := sched.Next(start)

		expected := time.Date(2025, 12, 25, 14, 30, 0, 0, time.UTC)
		if !next.Equal(expected) {
			t.Errorf("Christmas 2025: expected %v, got %v", expected, next)
		}
	})

	t.Run("combined extended features", func(t *testing.T) {
		// At noon on the last Friday of each month in 2025
		sched, err := parser.Parse("0 12 * * FRI#L 2025")
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		next := sched.Next(start)

		// Last Friday of January 2025 is Jan 31
		expected := time.Date(2025, 1, 31, 12, 0, 0, 0, time.UTC)
		if !next.Equal(expected) {
			t.Errorf("last Friday 2025: expected %v, got %v", expected, next)
		}
	})
}

func TestFullParserReturnsConsistentInstance(t *testing.T) {
	// FullParser should return the same pre-configured instance
	p1 := FullParser()
	p2 := FullParser()

	// Parse the same expression with both
	spec := "30 14 * * *"
	s1, err1 := p1.Parse(spec)
	s2, err2 := p2.Parse(spec)

	if err1 != nil || err2 != nil {
		t.Fatalf("parse errors: %v, %v", err1, err2)
	}

	// Both should produce equivalent schedules
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	n1 := s1.Next(start)
	n2 := s2.Next(start)

	if !n1.Equal(n2) {
		t.Errorf("FullParser instances produce different results: %v vs %v", n1, n2)
	}
}
