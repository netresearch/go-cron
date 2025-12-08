package cron

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestActivation(t *testing.T) {
	tests := []struct {
		time, spec string
		expected   bool
	}{
		// Every fifteen minutes.
		{"Mon Jul 9 15:00 2012", "0/15 * * * *", true},
		{"Mon Jul 9 15:45 2012", "0/15 * * * *", true},
		{"Mon Jul 9 15:40 2012", "0/15 * * * *", false},

		// Every fifteen minutes, starting at 5 minutes.
		{"Mon Jul 9 15:05 2012", "5/15 * * * *", true},
		{"Mon Jul 9 15:20 2012", "5/15 * * * *", true},
		{"Mon Jul 9 15:50 2012", "5/15 * * * *", true},

		// Named months
		{"Sun Jul 15 15:00 2012", "0/15 * * Jul *", true},
		{"Sun Jul 15 15:00 2012", "0/15 * * Jun *", false},

		// Everything set.
		{"Sun Jul 15 08:30 2012", "30 08 ? Jul Sun", true},
		{"Sun Jul 15 08:30 2012", "30 08 15 Jul ?", true},
		{"Mon Jul 16 08:30 2012", "30 08 ? Jul Sun", false},
		{"Mon Jul 16 08:30 2012", "30 08 15 Jul ?", false},

		// Predefined schedules
		{"Mon Jul 9 15:00 2012", "@hourly", true},
		{"Mon Jul 9 15:04 2012", "@hourly", false},
		{"Mon Jul 9 15:00 2012", "@daily", false},
		{"Mon Jul 9 00:00 2012", "@daily", true},
		{"Mon Jul 9 00:00 2012", "@weekly", false},
		{"Sun Jul 8 00:00 2012", "@weekly", true},
		{"Sun Jul 8 01:00 2012", "@weekly", false},
		{"Sun Jul 8 00:00 2012", "@monthly", false},
		{"Sun Jul 1 00:00 2012", "@monthly", true},

		// Test interaction of DOW and DOM.
		// If both are restricted, then only one needs to match.
		{"Sun Jul 15 00:00 2012", "* * 1,15 * Sun", true},
		{"Fri Jun 15 00:00 2012", "* * 1,15 * Sun", true},
		{"Wed Aug 1 00:00 2012", "* * 1,15 * Sun", true},
		{"Sun Jul 15 00:00 2012", "* * */10 * Sun", true}, // verifies #70

		// However, if one has a star, then both need to match.
		{"Sun Jul 15 00:00 2012", "* * * * Mon", false},
		{"Mon Jul 9 00:00 2012", "* * 1,15 * *", false},
		{"Sun Jul 15 00:00 2012", "* * 1,15 * *", true},
		{"Sun Jul 15 00:00 2012", "* * */2 * Sun", true},
	}

	for _, test := range tests {
		name := test.spec + "_at_" + test.time
		t.Run(name, func(t *testing.T) {
			sched, err := ParseStandard(test.spec)
			if err != nil {
				t.Fatal(err)
			}
			actual := sched.Next(getTime(test.time).Add(-1 * time.Second))
			expected := getTime(test.time)
			if test.expected && expected != actual || !test.expected && expected.Equal(actual) {
				t.Errorf("Fail evaluating %s on %s: (expected) %s != %s (actual)",
					test.spec, test.time, expected, actual)
			}
		})
	}
}

func TestNext(t *testing.T) {
	runs := []struct {
		time, spec string
		expected   string
	}{
		// Simple cases
		{"Mon Jul 9 14:45 2012", "0 0/15 * * * *", "Mon Jul 9 15:00 2012"},
		{"Mon Jul 9 14:59 2012", "0 0/15 * * * *", "Mon Jul 9 15:00 2012"},
		{"Mon Jul 9 14:59:59 2012", "0 0/15 * * * *", "Mon Jul 9 15:00 2012"},

		// Wrap around hours
		{"Mon Jul 9 15:45 2012", "0 20-35/15 * * * *", "Mon Jul 9 16:20 2012"},

		// Wrap around days
		{"Mon Jul 9 23:46 2012", "0 */15 * * * *", "Tue Jul 10 00:00 2012"},
		{"Mon Jul 9 23:45 2012", "0 20-35/15 * * * *", "Tue Jul 10 00:20 2012"},
		{"Mon Jul 9 23:35:51 2012", "15/35 20-35/15 * * * *", "Tue Jul 10 00:20:15 2012"},
		{"Mon Jul 9 23:35:51 2012", "15/35 20-35/15 1/2 * * *", "Tue Jul 10 01:20:15 2012"},
		{"Mon Jul 9 23:35:51 2012", "15/35 20-35/15 10-12 * * *", "Tue Jul 10 10:20:15 2012"},

		{"Mon Jul 9 23:35:51 2012", "15/35 20-35/15 1/2 */2 * *", "Thu Jul 11 01:20:15 2012"},
		{"Mon Jul 9 23:35:51 2012", "15/35 20-35/15 * 9-20 * *", "Wed Jul 10 00:20:15 2012"},
		{"Mon Jul 9 23:35:51 2012", "15/35 20-35/15 * 9-20 Jul *", "Wed Jul 10 00:20:15 2012"},

		// Wrap around months
		{"Mon Jul 9 23:35 2012", "0 0 0 9 Apr-Oct ?", "Thu Aug 9 00:00 2012"},
		{"Mon Jul 9 23:35 2012", "0 0 0 */5 Apr,Aug,Oct Mon", "Tue Aug 1 00:00 2012"},
		{"Mon Jul 9 23:35 2012", "0 0 0 */5 Oct Mon", "Mon Oct 1 00:00 2012"},

		// Wrap around years
		{"Mon Jul 9 23:35 2012", "0 0 0 * Feb Mon", "Mon Feb 4 00:00 2013"},
		{"Mon Jul 9 23:35 2012", "0 0 0 * Feb Mon/2", "Fri Feb 1 00:00 2013"},

		// Wrap around minute, hour, day, month, and year
		{"Mon Dec 31 23:59:45 2012", "0 * * * * *", "Tue Jan 1 00:00:00 2013"},

		// Leap year
		{"Mon Jul 9 23:35 2012", "0 0 0 29 Feb ?", "Mon Feb 29 00:00 2016"},

		// Daylight savings time 2am EST (-5) -> 3am EDT (-4)
		// ISC cron behavior: Jobs in skipped DST hour run immediately at 3am
		{"2012-03-11T00:00:00-0500", "TZ=America/New_York 0 30 2 11 Mar ?", "2012-03-11T03:30:00-0400"},

		// hourly job
		{"2012-03-11T00:00:00-0500", "TZ=America/New_York 0 0 * * * ?", "2012-03-11T01:00:00-0500"},
		{"2012-03-11T01:00:00-0500", "TZ=America/New_York 0 0 * * * ?", "2012-03-11T03:00:00-0400"},
		{"2012-03-11T03:00:00-0400", "TZ=America/New_York 0 0 * * * ?", "2012-03-11T04:00:00-0400"},
		{"2012-03-11T04:00:00-0400", "TZ=America/New_York 0 0 * * * ?", "2012-03-11T05:00:00-0400"},

		// hourly job using CRON_TZ
		{"2012-03-11T00:00:00-0500", "CRON_TZ=America/New_York 0 0 * * * ?", "2012-03-11T01:00:00-0500"},
		{"2012-03-11T01:00:00-0500", "CRON_TZ=America/New_York 0 0 * * * ?", "2012-03-11T03:00:00-0400"},
		{"2012-03-11T03:00:00-0400", "CRON_TZ=America/New_York 0 0 * * * ?", "2012-03-11T04:00:00-0400"},
		{"2012-03-11T04:00:00-0400", "CRON_TZ=America/New_York 0 0 * * * ?", "2012-03-11T05:00:00-0400"},

		// 1am nightly job
		{"2012-03-11T00:00:00-0500", "TZ=America/New_York 0 0 1 * * ?", "2012-03-11T01:00:00-0500"},
		{"2012-03-11T01:00:00-0500", "TZ=America/New_York 0 0 1 * * ?", "2012-03-12T01:00:00-0400"},

		// 2am nightly job - ISC cron behavior: runs at 3am when DST skips 2am
		{"2012-03-11T00:00:00-0500", "TZ=America/New_York 0 0 2 * * ?", "2012-03-11T03:00:00-0400"},

		// Daylight savings time 2am EDT (-4) => 1am EST (-5)
		{"2012-11-04T00:00:00-0400", "TZ=America/New_York 0 30 2 04 Nov ?", "2012-11-04T02:30:00-0500"},
		{"2012-11-04T01:45:00-0400", "TZ=America/New_York 0 30 1 04 Nov ?", "2012-11-04T01:30:00-0500"},

		// hourly job
		{"2012-11-04T00:00:00-0400", "TZ=America/New_York 0 0 * * * ?", "2012-11-04T01:00:00-0400"},
		{"2012-11-04T01:00:00-0400", "TZ=America/New_York 0 0 * * * ?", "2012-11-04T01:00:00-0500"},
		{"2012-11-04T01:00:00-0500", "TZ=America/New_York 0 0 * * * ?", "2012-11-04T02:00:00-0500"},

		// 1am nightly job (runs twice)
		{"2012-11-04T00:00:00-0400", "TZ=America/New_York 0 0 1 * * ?", "2012-11-04T01:00:00-0400"},
		{"2012-11-04T01:00:00-0400", "TZ=America/New_York 0 0 1 * * ?", "2012-11-04T01:00:00-0500"},
		{"2012-11-04T01:00:00-0500", "TZ=America/New_York 0 0 1 * * ?", "2012-11-05T01:00:00-0500"},

		// 2am nightly job
		{"2012-11-04T00:00:00-0400", "TZ=America/New_York 0 0 2 * * ?", "2012-11-04T02:00:00-0500"},
		{"2012-11-04T02:00:00-0500", "TZ=America/New_York 0 0 2 * * ?", "2012-11-05T02:00:00-0500"},

		// 3am nightly job
		{"2012-11-04T00:00:00-0400", "TZ=America/New_York 0 0 3 * * ?", "2012-11-04T03:00:00-0500"},
		{"2012-11-04T03:00:00-0500", "TZ=America/New_York 0 0 3 * * ?", "2012-11-05T03:00:00-0500"},

		// hourly job
		{"TZ=America/New_York 2012-11-04T00:00:00-0400", "0 0 * * * ?", "2012-11-04T01:00:00-0400"},
		{"TZ=America/New_York 2012-11-04T01:00:00-0400", "0 0 * * * ?", "2012-11-04T01:00:00-0500"},
		{"TZ=America/New_York 2012-11-04T01:00:00-0500", "0 0 * * * ?", "2012-11-04T02:00:00-0500"},

		// 1am nightly job (runs twice)
		{"TZ=America/New_York 2012-11-04T00:00:00-0400", "0 0 1 * * ?", "2012-11-04T01:00:00-0400"},
		{"TZ=America/New_York 2012-11-04T01:00:00-0400", "0 0 1 * * ?", "2012-11-04T01:00:00-0500"},
		{"TZ=America/New_York 2012-11-04T01:00:00-0500", "0 0 1 * * ?", "2012-11-05T01:00:00-0500"},

		// 2am nightly job
		{"TZ=America/New_York 2012-11-04T00:00:00-0400", "0 0 2 * * ?", "2012-11-04T02:00:00-0500"},
		{"TZ=America/New_York 2012-11-04T02:00:00-0500", "0 0 2 * * ?", "2012-11-05T02:00:00-0500"},

		// 3am nightly job
		{"TZ=America/New_York 2012-11-04T00:00:00-0400", "0 0 3 * * ?", "2012-11-04T03:00:00-0500"},
		{"TZ=America/New_York 2012-11-04T03:00:00-0500", "0 0 3 * * ?", "2012-11-05T03:00:00-0500"},

		// Unsatisfiable
		{"Mon Jul 9 23:35 2012", "0 0 0 30 Feb ?", ""},
		{"Mon Jul 9 23:35 2012", "0 0 0 31 Apr ?", ""},

		// Monthly job
		{"TZ=America/New_York 2012-11-04T00:00:00-0400", "0 0 3 3 * ?", "2012-12-03T03:00:00-0500"},

		// Test the scenario of DST resulting in midnight not being a valid time.
		// https://github.com/robfig/cron/issues/157
		{"2018-10-17T05:00:00-0400", "TZ=America/Sao_Paulo 0 0 9 10 * ?", "2018-11-10T06:00:00-0500"},
		{"2018-02-14T05:00:00-0500", "TZ=America/Sao_Paulo 0 0 9 22 * ?", "2018-02-22T07:00:00-0500"},
	}

	for _, c := range runs {
		name := c.spec + "_from_" + c.time
		t.Run(name, func(t *testing.T) {
			sched, err := secondParser.Parse(c.spec)
			if err != nil {
				t.Fatal(err)
			}
			actual := sched.Next(getTime(c.time))
			expected := getTime(c.expected)
			if !actual.Equal(expected) {
				t.Errorf("%s, \"%s\": (expected) %v != %v (actual)", c.time, c.spec, expected, actual)
			}
		})
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name string
		spec string
	}{
		{"invalid_expression", "xyz"},
		{"second_out_of_range", "60 0 * * *"},
		{"minute_out_of_range", "0 60 * * *"},
		{"invalid_day_of_week", "0 0 * * XYZ"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseStandard(tc.spec)
			if err == nil {
				t.Errorf("expected an error parsing: %s", tc.spec)
			}
		})
	}
}

func getTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}

	location := time.Local
	if strings.HasPrefix(value, "TZ=") {
		parts := strings.Fields(value)
		loc, err := time.LoadLocation(parts[0][len("TZ="):])
		if err != nil {
			panic("could not parse location:" + err.Error())
		}
		location = loc
		value = parts[1]
	}

	layouts := []string{
		"Mon Jan 2 15:04 2006",
		"Mon Jan 2 15:04:05 2006",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, value, location); err == nil {
			return t
		}
	}
	if t, err := time.ParseInLocation("2006-01-02T15:04:05-0700", value, location); err == nil {
		return t
	}
	panic("could not parse time value " + value)
}

func TestNextWithTz(t *testing.T) {
	runs := []struct {
		time, spec string
		expected   string
	}{
		// Failing tests
		{"2016-01-03T13:09:03+0530", "14 14 * * *", "2016-01-03T14:14:00+0530"},
		{"2016-01-03T04:09:03+0530", "14 14 * * ?", "2016-01-03T14:14:00+0530"},

		// Passing tests
		{"2016-01-03T14:09:03+0530", "14 14 * * *", "2016-01-03T14:14:00+0530"},
		{"2016-01-03T14:00:00+0530", "14 14 * * ?", "2016-01-03T14:14:00+0530"},
	}
	for _, c := range runs {
		name := c.spec + "_from_" + c.time
		t.Run(name, func(t *testing.T) {
			sched, err := ParseStandard(c.spec)
			if err != nil {
				t.Fatal(err)
			}
			actual := sched.Next(getTimeTZ(c.time))
			expected := getTimeTZ(c.expected)
			if !actual.Equal(expected) {
				t.Errorf("%s, \"%s\": (expected) %v != %v (actual)", c.time, c.spec, expected, actual)
			}
		})
	}
}

func getTimeTZ(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	t, err := time.Parse("Mon Jan 2 15:04 2006", value)
	if err != nil {
		t, err = time.Parse("Mon Jan 2 15:04:05 2006", value)
		if err != nil {
			t, err = time.Parse("2006-01-02T15:04:05-0700", value)
			if err != nil {
				panic(err)
			}
		}
	}

	return t
}

// https://github.com/robfig/cron/issues/144
func TestSlash0NoHang(t *testing.T) {
	schedule := "TZ=America/New_York 15/0 * * * *"
	_, err := ParseStandard(schedule)
	if err == nil {
		t.Error("expected an error on 0 increment")
	}
}

// TestNormalizeDSTDay_Hour12Boundary tests spec.go:128 boundary condition.
// This kills CONDITIONALS_BOUNDARY mutation where `> 12` could become `>= 12`.
// Hour 12 should be adjusted backward to midnight, not forward.
func TestNormalizeDSTDay_Hour12Boundary(t *testing.T) {
	// Test times in a neutral timezone to isolate the function behavior
	loc := time.UTC

	tests := []struct {
		name     string
		input    time.Time
		expected time.Time
	}{
		{
			name:     "hour 0 unchanged",
			input:    time.Date(2024, 3, 10, 0, 0, 0, 0, loc),
			expected: time.Date(2024, 3, 10, 0, 0, 0, 0, loc),
		},
		{
			name:     "hour 1 adjusted to midnight",
			input:    time.Date(2024, 3, 10, 1, 0, 0, 0, loc),
			expected: time.Date(2024, 3, 10, 0, 0, 0, 0, loc),
		},
		{
			name:     "hour 11 adjusted to midnight",
			input:    time.Date(2024, 3, 10, 11, 0, 0, 0, loc),
			expected: time.Date(2024, 3, 10, 0, 0, 0, 0, loc),
		},
		{
			// CRITICAL: This tests the boundary at hour == 12
			// Original: hour > 12 is false, so we go to else: t.Add(-12h) = midnight
			// Mutant:   hour >= 12 is true, so we go to if: t.Add(12h) = noon next day (WRONG)
			name:     "hour 12 BOUNDARY - must adjust backward to midnight",
			input:    time.Date(2024, 3, 10, 12, 0, 0, 0, loc),
			expected: time.Date(2024, 3, 10, 0, 0, 0, 0, loc),
		},
		{
			name:     "hour 13 adjusted forward to midnight",
			input:    time.Date(2024, 3, 10, 13, 0, 0, 0, loc),
			expected: time.Date(2024, 3, 11, 0, 0, 0, 0, loc),
		},
		{
			name:     "hour 23 adjusted forward to midnight",
			input:    time.Date(2024, 3, 10, 23, 0, 0, 0, loc),
			expected: time.Date(2024, 3, 11, 0, 0, 0, 0, loc),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeDSTDay(tt.input)
			if !got.Equal(tt.expected) {
				t.Errorf("normalizeDSTDay(%v) = %v, want %v", tt.input, got, tt.expected)
			}
			// Additional check: result hour should always be 0 (midnight)
			if got.Hour() != 0 {
				t.Errorf("normalizeDSTDay(%v) resulted in hour %d, want 0 (midnight)",
					tt.input, got.Hour())
			}
		})
	}
}

// TestNormalizeDSTDay_Arithmetic tests spec.go:131 arithmetic operations.
// This kills ARITHMETIC_BASE mutations in the hour adjustment calculations.
func TestNormalizeDSTDay_Arithmetic(t *testing.T) {
	loc := time.UTC

	// Test a range of hours to ensure arithmetic is correct
	for hour := 1; hour <= 23; hour++ {
		t.Run(fmt.Sprintf("hour_%d", hour), func(t *testing.T) {
			input := time.Date(2024, 6, 15, hour, 30, 45, 123, loc)
			got := normalizeDSTDay(input)

			// Result should always be at midnight
			if got.Hour() != 0 {
				t.Errorf("hour %d: got hour %d, want 0", hour, got.Hour())
			}

			// Verify the date is correct based on which branch was taken
			if hour > 12 {
				// Should advance to next day's midnight
				expectedDate := time.Date(2024, 6, 16, 0, 0, 0, 0, loc)
				if got.Year() != expectedDate.Year() || got.Month() != expectedDate.Month() ||
					got.Day() != expectedDate.Day() {
					t.Errorf("hour %d: expected date %v, got %v", hour, expectedDate, got)
				}
			} else {
				// Should go back to same day's midnight
				expectedDate := time.Date(2024, 6, 15, 0, 0, 0, 0, loc)
				if got.Year() != expectedDate.Year() || got.Month() != expectedDate.Month() ||
					got.Day() != expectedDate.Day() {
					t.Errorf("hour %d: expected date %v, got %v", hour, expectedDate, got)
				}
			}
		})
	}
}

// TestCheckHourDSTSkip_Boundary tests spec.go:142 for DST skip detection.
// This ensures the hour bit check correctly identifies skipped hours.
func TestCheckHourDSTSkip_Boundary(t *testing.T) {
	tests := []struct {
		name     string
		prev     time.Time
		curr     time.Time
		hourBits uint64
		expected bool
	}{
		{
			name:     "no DST skip (1 hour diff)",
			prev:     time.Date(2024, 3, 10, 1, 0, 0, 0, time.UTC),
			curr:     time.Date(2024, 3, 10, 2, 0, 0, 0, time.UTC),
			hourBits: 0xFFFFFF, // all hours
			expected: false,
		},
		{
			name:     "DST skip (2 hour diff) - skipped hour in schedule",
			prev:     time.Date(2024, 3, 10, 1, 0, 0, 0, time.UTC),
			curr:     time.Date(2024, 3, 10, 3, 0, 0, 0, time.UTC),
			hourBits: 1 << 2, // hour 2 scheduled (the skipped hour)
			expected: true,
		},
		{
			name:     "DST skip (2 hour diff) - skipped hour NOT in schedule",
			prev:     time.Date(2024, 3, 10, 1, 0, 0, 0, time.UTC),
			curr:     time.Date(2024, 3, 10, 3, 0, 0, 0, time.UTC),
			hourBits: 1 << 5, // hour 5 scheduled (not the skipped hour)
			expected: false,
		},
		{
			name:     "3 hour diff - not a DST skip",
			prev:     time.Date(2024, 3, 10, 1, 0, 0, 0, time.UTC),
			curr:     time.Date(2024, 3, 10, 4, 0, 0, 0, time.UTC),
			hourBits: 0xFFFFFF,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkHourDSTSkip(tt.prev, tt.curr, tt.hourBits)
			if got != tt.expected {
				t.Errorf("checkHourDSTSkip(%v, %v, %x) = %v, want %v",
					tt.prev, tt.curr, tt.hourBits, got, tt.expected)
			}
		})
	}
}
