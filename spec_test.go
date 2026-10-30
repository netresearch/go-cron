// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cron

import (
	"fmt"
	"slices"
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
		// Both must match (AND logic), consistent with all other cron fields.
		{"Sun Jul 15 00:00 2012", "* * 1,15 * Sun", true},  // DOM=15 matches, DOW=Sun matches
		{"Fri Jun 15 00:00 2012", "* * 1,15 * Sun", false}, // DOM=15 matches, but Friday != Sun
		{"Wed Aug 1 00:00 2012", "* * 1,15 * Sun", false},  // DOM=1 matches, but Wednesday != Sun
		{"Sun Jul 15 00:00 2012", "* * */10 * Sun", false}, // DOW=Sun matches, but 15 not in 1,11,21,31

		// Wildcard behavior unchanged: star means "any", so only the restricted field matters.
		{"Sun Jul 15 00:00 2012", "* * * * Mon", false},
		{"Mon Jul 9 00:00 2012", "* * 1,15 * *", false},
		{"Sun Jul 15 00:00 2012", "* * 1,15 * *", true},
		{"Sun Jul 15 00:00 2012", "* * */2 * Sun", true}, // */2 includes 15 (1,3,5,...,15,...), DOW=Sun
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
		{"Mon Jul 9 23:35 2012", "0 0 0 */5 Apr,Aug,Oct Mon", "Mon Aug 6 00:00 2012"}, // DOM AND DOW
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

		// 1am nightly job — Next() returns both occurrences (the scheduler
		// prevents duplicate execution via isDSTFallBackDuplicate; see ADR-016).
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

		// 1am nightly job — Next() returns both occurrences (the scheduler
		// prevents duplicate execution via isDSTFallBackDuplicate; see ADR-016).
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

// TestNormalizeDOW tests the NormalizeDOW function that maps bit 7 to bit 0.
func TestNormalizeDOW(t *testing.T) {
	tests := []struct {
		name     string
		input    uint64
		expected uint64
	}{
		{
			name:     "no bit 7 - unchanged",
			input:    0b01111111, // bits 0-6 set
			expected: 0b01111111,
		},
		{
			name:     "only bit 7 - becomes bit 0",
			input:    1 << 7,
			expected: 1,
		},
		{
			name:     "bit 7 and bit 0 both set - remains bit 0",
			input:    (1 << 7) | 1,
			expected: 1,
		},
		{
			name:     "bit 7 with other bits - normalized",
			input:    (1 << 7) | (1 << 3) | (1 << 5), // Sun(7), Wed, Fri
			expected: 1 | (1 << 3) | (1 << 5),        // Sun(0), Wed, Fri
		},
		{
			name:     "starBit preserved",
			input:    starBit | (1 << 7),
			expected: starBit | 1,
		},
		{
			name:     "zero input",
			input:    0,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeDOW(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeDOW(%b) = %b, want %b", tt.input, got, tt.expected)
			}
		})
	}
}

func TestPrev(t *testing.T) {
	runs := []struct {
		time, spec string
		expected   string
	}{
		// Simple cases - mirror of TestNext
		{"Mon Jul 9 15:05 2012", "0 0/15 * * * *", "Mon Jul 9 15:00 2012"},
		{"Mon Jul 9 15:01 2012", "0 0/15 * * * *", "Mon Jul 9 15:00 2012"},
		{"Mon Jul 9 15:00:01 2012", "0 0/15 * * * *", "Mon Jul 9 15:00 2012"},

		// Wrap around hours (backwards)
		{"Mon Jul 9 16:25 2012", "0 20-35/15 * * * *", "Mon Jul 9 16:20 2012"},
		{"Mon Jul 9 16:19 2012", "0 20-35/15 * * * *", "Mon Jul 9 15:35 2012"},

		// Wrap around days (backwards)
		{"Tue Jul 10 00:05 2012", "0 */15 * * * *", "Tue Jul 10 00:00 2012"},
		{"Tue Jul 10 00:00:01 2012", "0 */15 * * * *", "Tue Jul 10 00:00 2012"},
		{"Mon Jul 9 00:10 2012", "0 20-35/15 * * * *", "Sun Jul 8 23:35 2012"},

		// Every second
		{"Mon Jul 9 15:00:05 2012", "* * * * * *", "Mon Jul 9 15:00:04 2012"},

		// Specific seconds
		{"Mon Jul 9 15:00:35 2012", "0,30 * * * * *", "Mon Jul 9 15:00:30 2012"},

		// Hourly schedule
		{"Mon Jul 9 15:30 2012", "@hourly", "Mon Jul 9 15:00 2012"},
		{"Mon Jul 9 15:00:01 2012", "@hourly", "Mon Jul 9 15:00 2012"},

		// Daily schedule
		{"Mon Jul 9 12:00 2012", "@daily", "Mon Jul 9 00:00 2012"},
		{"Mon Jul 9 00:00:01 2012", "@daily", "Mon Jul 9 00:00 2012"},

		// Weekly schedule
		{"Sun Jul 15 12:00 2012", "@weekly", "Sun Jul 15 00:00 2012"},
		{"Sat Jul 14 23:59 2012", "@weekly", "Sun Jul 8 00:00 2012"},

		// Monthly schedule
		{"Sun Jul 15 12:00 2012", "@monthly", "Sun Jul 1 00:00 2012"},
		{"Sat Jun 30 23:59 2012", "@monthly", "Fri Jun 1 00:00 2012"},
	}

	for _, c := range runs {
		name := c.spec + "_from_" + c.time
		t.Run(name, func(t *testing.T) {
			sched, err := secondParser.Parse(c.spec)
			if err != nil {
				t.Fatal(err)
			}
			actual := sched.(ScheduleWithPrev).Prev(getTime(c.time))
			expected := getTime(c.expected)
			if !actual.Equal(expected) {
				t.Errorf("%s, \"%s\": (expected) %v != %v (actual)", c.time, c.spec, expected, actual)
			}
		})
	}
}

func TestPrevWithTz(t *testing.T) {
	runs := []struct {
		time, spec string
		expected   string
	}{
		// Basic timezone test
		{"2016-01-03T14:15:03+0530", "14 14 * * *", "2016-01-03T14:14:00+0530"},
		{"2016-01-03T14:13:00+0530", "14 14 * * ?", "2016-01-02T14:14:00+0530"},

		// Previous day
		{"2016-01-03T14:00:00+0530", "14 14 * * *", "2016-01-02T14:14:00+0530"},
	}
	for _, c := range runs {
		name := c.spec + "_from_" + c.time
		t.Run(name, func(t *testing.T) {
			sched, err := ParseStandard(c.spec)
			if err != nil {
				t.Fatal(err)
			}
			actual := sched.(ScheduleWithPrev).Prev(getTimeTZ(c.time))
			expected := getTimeTZ(c.expected)
			if !actual.Equal(expected) {
				t.Errorf("%s, \"%s\": (expected) %v != %v (actual)", c.time, c.spec, expected, actual)
			}
		})
	}
}

func TestPrevUnsatisfiable(t *testing.T) {
	// Feb 30 doesn't exist - should return zero time
	sched, err := secondParser.Parse("0 0 0 30 Feb ?")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	result := sched.(ScheduleWithPrev).Prev(now)
	if !result.IsZero() {
		t.Errorf("Expected zero time for unsatisfiable schedule, got %v", result)
	}
}

func TestPrevAndNextSymmetry(t *testing.T) {
	// For any schedule, Next(Prev(t)) should return the same time as Prev(t)
	// when t is exactly on a scheduled time.
	specs := []string{
		"0 0 * * * *",      // Every hour
		"0 30 * * * *",     // Every hour at :30
		"0 0 */2 * * *",    // Every 2 hours
		"0 0 0 * * *",      // Daily
		"0 0 0 * * 0",      // Weekly on Sunday
		"0 0 0 1 * *",      // Monthly
		"*/15 * * * * *",   // Every 15 seconds
		"0 */10 * * * *",   // Every 10 minutes
		"0 0 9-17 * * 1-5", // Weekdays 9am-5pm
	}

	now := time.Date(2024, 6, 15, 12, 30, 45, 0, time.UTC)

	for _, spec := range specs {
		t.Run(spec, func(t *testing.T) {
			sched, err := secondParser.Parse(spec)
			if err != nil {
				t.Fatal(err)
			}

			// Get a scheduled time
			scheduledTime := sched.Next(now)
			if scheduledTime.IsZero() {
				t.Skip("No next time found")
			}

			// Prev from just after the scheduled time should return the scheduled time
			prevTime := sched.(ScheduleWithPrev).Prev(scheduledTime.Add(time.Second))
			if !prevTime.Equal(scheduledTime) {
				t.Errorf("Prev(Next(t)+1s) = %v, want %v", prevTime, scheduledTime)
			}

			// Next from just before the scheduled time should return the scheduled time
			nextTime := sched.Next(scheduledTime.Add(-time.Second))
			if !nextTime.Equal(scheduledTime) {
				t.Errorf("Next(scheduled-1s) = %v, want %v", nextTime, scheduledTime)
			}
		})
	}
}

// springForwardNY returns 2026-03-<day> <hour>:<minute> in New York. On
// 2026-03-08 New York clocks jump from 02:00 EST to 03:00 EDT.
func springForwardNY(t *testing.T, day, hour, minute int) time.Time {
	t.Helper()
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	return time.Date(2026, 3, day, hour, minute, 0, 0, ny)
}

// TestPrevDSTSpringForward checks that Prev returns the run that Next produces
// for a job in the hour skipped by DST spring-forward (ISC cron behavior).
func TestPrevDSTSpringForward(t *testing.T) {
	at := func(day, hour, minute int) time.Time { return springForwardNY(t, day, hour, minute) }
	runs := []struct {
		spec     string
		from     time.Time
		expected time.Time
	}{
		{"30 2 * * *", at(9, 0, 0), at(8, 3, 30)},
		{"30 2 * * *", at(8, 3, 31), at(8, 3, 30)},
		{"30 2 * * *", at(8, 3, 30), at(7, 2, 30)},
		{"30 2 * * *", at(8, 3, 10), at(7, 2, 30)},
		{"*/15 2 * * *", at(8, 4, 0), at(8, 3, 0)},
		{"0 2 * * *", at(8, 12, 0), at(8, 3, 0)},
		{"0 1,2 * * *", at(8, 3, 0), at(8, 1, 0)},
	}
	for _, c := range runs {
		t.Run(c.spec+"_from_"+c.from.String(), func(t *testing.T) {
			sched, err := ParseStandard(c.spec)
			if err != nil {
				t.Fatal(err)
			}
			actual := sched.(ScheduleWithPrev).Prev(c.from)
			if !actual.Equal(c.expected) {
				t.Errorf("Prev(%v) = %v, want %v", c.from, actual, c.expected)
			}
		})
	}
}

// TestPrevNextDSTSymmetry checks that Prev walked backwards over the
// spring-forward day yields exactly the runs that Next yields forwards, both
// with the default ISC behavior and with DSTSkip.
func TestPrevNextDSTSymmetry(t *testing.T) {
	start, end := springForwardNY(t, 7, 0, 0), springForwardNY(t, 9, 12, 0)
	gapStart, gapEnd := springForwardNY(t, 8, 1, 59), springForwardNY(t, 8, 4, 0)
	fields := Minute | Hour | Dom | Month | Dow
	specs := []struct {
		options ParseOption
		spec    string
		// skipGapRuns is the number of runs on 2026-03-08 between 01:59 and
		// 04:00 with DSTSkip, which drops the runs of the skipped 02:00 hour.
		skipGapRuns int
	}{
		{fields, "30 2 * * *", 0},
		{fields, "*/15 2 * * *", 0},
		{fields, "0 2 * * *", 0},
		{fields, "0 1,2 * * *", 0},
		{fields, "*/20 1,2 * * *", 0},
		{fields, "30 1-3 * * *", 1},
		{fields, "0 * * * *", 1},
		{Second | fields, "15 30 2 * * *", 0},
	}
	for _, c := range specs {
		for _, skip := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/DSTSkip=%t", c.spec, skip), func(t *testing.T) {
				options := c.options
				if skip {
					options |= DSTSkip
				}
				sched, err := NewParser(options).Parse(c.spec)
				if err != nil {
					t.Fatal(err)
				}
				forward, backward := runsBetween(sched, start, end)
				if !slices.EqualFunc(forward, backward, time.Time.Equal) {
					t.Errorf("Next and Prev disagree\nnext: %v\nprev: %v", forward, backward)
				}
				if !skip {
					return
				}
				n := 0
				for _, r := range forward {
					if r.After(gapStart) && r.Before(gapEnd) {
						n++
					}
				}
				if n != c.skipGapRuns {
					t.Errorf("got %d runs around the gap, want %d: %v", n, c.skipGapRuns, forward)
				}
			})
		}
	}
}

// runsBetween lists the runs strictly between start and end in ascending
// order, once collected with Next and once with Prev.
func runsBetween(sched Schedule, start, end time.Time) (forward, backward []time.Time) {
	for n := sched.Next(start); n.Before(end); n = sched.Next(n) {
		forward = append(forward, n)
	}
	for p := sched.(ScheduleWithPrev).Prev(end); p.After(start); p = sched.(ScheduleWithPrev).Prev(p) {
		backward = append(backward, p)
	}
	slices.Reverse(backward)
	return forward, backward
}

// TestDayMatchesANDMode tests the default AND logic for DOM/DOW matching.
func TestDayMatchesANDMode(t *testing.T) {
	tests := []struct {
		name     string
		spec     string
		time     string
		expected bool
	}{
		// Both DOM and DOW must match (AND logic)
		{"both match - Sunday the 15th", "* * 15 * Sun", "Sun Jul 15 12:00 2012", true},
		{"only DOM matches", "* * 15 * Sun", "Fri Jun 15 12:00 2012", false},
		{"only DOW matches", "* * 15 * Sun", "Sun Jul 8 12:00 2012", false},
		{"neither matches", "* * 15 * Sun", "Sat Jul 14 12:00 2012", false},

		// Useful AND patterns
		{"last Friday of month", "* * 25-31 * Fri", "Fri Aug 31 12:00 2012", true},
		{"last Friday of month - not Friday", "* * 25-31 * Fri", "Sat Aug 25 12:00 2012", false},
		{"first Monday of month", "* * 1-7 * Mon", "Mon Aug 6 12:00 2012", true},
		{"first Monday of month - wrong week", "* * 1-7 * Mon", "Mon Aug 13 12:00 2012", false},

		// Wildcard cases (unchanged behavior - star means "any")
		{"DOM wildcard - DOW restricted", "* * * * Mon", "Mon Aug 6 12:00 2012", true},
		{"DOM wildcard - DOW restricted - wrong day", "* * * * Mon", "Tue Aug 7 12:00 2012", false},
		{"DOW wildcard - DOM restricted", "* * 15 * *", "Wed Aug 15 12:00 2012", true},
		{"DOW wildcard - DOM restricted - wrong day", "* * 15 * *", "Wed Aug 14 12:00 2012", false},
		{"both wildcards", "* * * * *", "Wed Aug 15 12:00 2012", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sched, err := ParseStandard(tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			tm := getTime(tc.time)
			// Next from 1 second before should return the time if it matches
			next := sched.Next(tm.Add(-time.Second))
			actual := next.Equal(tm)
			if actual != tc.expected {
				t.Errorf("spec %q at %s: got match=%v, want %v (next=%v)",
					tc.spec, tc.time, actual, tc.expected, next)
			}
		})
	}
}

// TestDayMatchesORMode tests the legacy OR logic for DOM/DOW matching.
func TestDayMatchesORMode(t *testing.T) {
	// Create parser with legacy OR mode
	parser := NewParser(Minute | Hour | Dom | Month | Dow | DowOrDom)

	tests := []struct {
		name     string
		spec     string
		time     string
		expected bool
	}{
		// Either DOM or DOW needs to match (OR logic)
		{"both match", "* * 15 * Sun", "Sun Jul 15 12:00 2012", true},
		{"only DOM matches", "* * 15 * Sun", "Fri Jun 15 12:00 2012", true},
		{"only DOW matches", "* * 15 * Sun", "Sun Jul 8 12:00 2012", true},
		{"neither matches", "* * 15 * Sun", "Sat Jul 14 12:00 2012", false},

		// Wildcard cases (unchanged - star means "any", both must match)
		{"DOM wildcard - DOW restricted", "* * * * Mon", "Mon Aug 6 12:00 2012", true},
		{"DOM wildcard - wrong day", "* * * * Mon", "Tue Aug 7 12:00 2012", false},
		{"DOW wildcard - DOM restricted", "* * 15 * *", "Wed Aug 15 12:00 2012", true},
		{"DOW wildcard - wrong day", "* * 15 * *", "Wed Aug 14 12:00 2012", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sched, err := parser.Parse(tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			tm := getTime(tc.time)
			next := sched.Next(tm.Add(-time.Second))
			actual := next.Equal(tm)
			if actual != tc.expected {
				t.Errorf("spec %q at %s: got match=%v, want %v (next=%v)",
					tc.spec, tc.time, actual, tc.expected, next)
			}
		})
	}
}

// TestStepWildcard verifies that StepWildcard keeps a stepped star in
// a day field unrestricted, so DowOrDom ANDs the two day fields as Vixie cron does.
func TestStepWildcard(t *testing.T) {
	vixie := NewParser(Minute | Hour | Dom | Month | Dow | DowOrDom | StepWildcard)
	legacy := NewParser(Minute | Hour | Dom | Month | Dow | DowOrDom)

	tests := []struct {
		name   string
		spec   string
		time   string
		vixie  bool
		legacy bool
	}{
		{"stepped DOM, odd Monday", "0 0 */2 * Mon", "Mon Aug 3 00:00 2026", true, true},
		{"stepped DOM, odd Wednesday", "0 0 */2 * Mon", "Wed Aug 5 00:00 2026", false, true},
		{"stepped DOM, even Monday", "0 0 */2 * Mon", "Mon Aug 10 00:00 2026", false, true},
		{"question mark alias", "0 0 ?/2 * Mon", "Wed Aug 5 00:00 2026", false, true},
		{"stepped DOW, first of month on Tuesday", "0 0 1 * */2", "Tue Sep 1 00:00 2026", true, true},
		{"stepped DOW, Tuesday that is not the first", "0 0 1 * */2", "Tue Aug 4 00:00 2026", false, true},
		{"both restricted still ORs", "0 0 15 * Sun", "Sun Aug 2 00:00 2026", true, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, p := range []struct {
				name   string
				parser Parser
				want   bool
			}{{"StepWildcard", vixie, tc.vixie}, {"DowOrDom only", legacy, tc.legacy}} {
				sched, err := p.parser.Parse(tc.spec)
				if err != nil {
					t.Fatal(err)
				}
				tm := getTime(tc.time)
				if got := sched.Next(tm.Add(-time.Second)).Equal(tm); got != p.want {
					t.Errorf("%s: spec %q at %s: got match=%v, want %v", p.name, tc.spec, tc.time, got, p.want)
				}
			}
		})
	}
}

// TestStepWildcardWithoutDowOrDom verifies that StepWildcard alone changes
// nothing: both day fields stay ANDed, and AnalyzeSpec still warns about it.
func TestStepWildcardWithoutDowOrDom(t *testing.T) {
	opts := Minute | Hour | Dom | Month | Dow
	spec := "0 0 */2 * MON"

	withOpt := AnalyzeSpec(spec, opts|StepWildcard)
	without := AnalyzeSpec(spec, opts)
	if !withOpt.Valid || !without.Valid {
		t.Fatalf("spec %q should be valid: %v / %v", spec, withOpt.Error, without.Error)
	}
	if len(without.Warnings) == 0 {
		t.Fatalf("AnalyzeSpec(%q) without StepWildcard returned no warning, want the AND-logic warning", spec)
	}
	if !slices.Equal(withOpt.Warnings, without.Warnings) {
		t.Errorf("StepWildcard without DowOrDom changed the warnings: got %q, want %q", withOpt.Warnings, without.Warnings)
	}

	sched, err := NewParser(opts | StepWildcard).Parse(spec)
	if err != nil {
		t.Fatal(err)
	}
	// Wed Aug 5 2026 is an odd day but no Monday: AND logic must not match it.
	tm := getTime("Wed Aug 5 00:00 2026")
	if sched.Next(tm.Add(-time.Second)).Equal(tm) {
		t.Errorf("StepWildcard without DowOrDom matched %s, want AND logic", tm)
	}
}

// TestDowOrDomParseOption verifies the DowOrDom ParseOption is correctly applied.
func TestDowOrDomParseOption(t *testing.T) {
	// Default parser (AND mode)
	defaultParser := NewParser(Minute | Hour | Dom | Month | Dow)
	andSched, err := defaultParser.Parse("0 0 15 * Fri")
	if err != nil {
		t.Fatal(err)
	}
	andSpec := andSched.(*SpecSchedule)
	if andSpec.DowOrDom {
		t.Error("Default parser should have DowOrDom=false")
	}

	// Parser with DowOrDom option (OR mode)
	orParser := NewParser(Minute | Hour | Dom | Month | Dow | DowOrDom)
	orSched, err := orParser.Parse("0 0 15 * Fri")
	if err != nil {
		t.Fatal(err)
	}
	orSpec := orSched.(*SpecSchedule)
	if !orSpec.DowOrDom {
		t.Error("Parser with DowOrDom option should have DowOrDom=true")
	}
}

// TestANDModeNextSchedule tests Next() with various AND mode scenarios.
func TestANDModeNextSchedule(t *testing.T) {
	tests := []struct {
		name     string
		spec     string
		from     string
		expected string
	}{
		{
			name:     "last Friday of month",
			spec:     "0 0 25-31 * Fri",
			from:     "Mon Aug 1 00:00 2012",
			expected: "Fri Aug 31 00:00 2012",
		},
		{
			name:     "first Monday of month",
			spec:     "0 0 1-7 * Mon",
			from:     "Mon Aug 1 00:00 2012",
			expected: "Mon Aug 6 00:00 2012",
		},
		{
			name:     "Friday the 13th",
			spec:     "0 0 13 * Fri",
			from:     "Mon Jul 9 00:00 2012",
			expected: "Fri Jul 13 00:00 2012",
		},
		{
			name:     "second Tuesday of month",
			spec:     "0 0 8-14 * Tue",
			from:     "Mon Aug 1 00:00 2012",
			expected: "Tue Aug 14 00:00 2012",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sched, err := ParseStandard(tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			from := getTime(tc.from)
			expected := getTime(tc.expected)
			actual := sched.Next(from)
			if !actual.Equal(expected) {
				t.Errorf("Next(%s) = %v, want %v", tc.from, actual, expected)
			}
		})
	}
}

// TestANDModePrevSchedule tests Prev() with AND mode scenarios.
func TestANDModePrevSchedule(t *testing.T) {
	tests := []struct {
		name     string
		spec     string
		from     string
		expected string
	}{
		{
			name:     "last Friday of month - backwards",
			spec:     "0 0 25-31 * Fri",
			from:     "Sat Sep 1 00:00 2012",
			expected: "Fri Aug 31 00:00 2012",
		},
		{
			name:     "first Monday of month - backwards",
			spec:     "0 0 1-7 * Mon",
			from:     "Tue Aug 7 00:00 2012",
			expected: "Mon Aug 6 00:00 2012",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sched, err := ParseStandard(tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			from := getTime(tc.from)
			expected := getTime(tc.expected)
			actual := sched.(ScheduleWithPrev).Prev(from)
			if !actual.Equal(expected) {
				t.Errorf("Prev(%s) = %v, want %v", tc.from, actual, expected)
			}
		})
	}
}

// TestNormalizeDSTDayPrev tests the normalizeDSTDayPrev function.
func TestNormalizeDSTDayPrev(t *testing.T) {
	unchangedInput := time.Date(2024, 6, 15, 23, 59, 59, 0, time.UTC)
	testCases := []struct {
		name     string
		input    time.Time
		expected time.Time
	}{
		{
			"hour 23 unchanged",
			unchangedInput,
			unchangedInput,
		},
		{
			"hour not 23 adjusted",
			time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC),
			time.Date(2024, 6, 15, 23, 59, 59, 0, time.UTC),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := normalizeDSTDayPrev(tc.input)
			if !result.Equal(tc.expected) {
				t.Errorf("normalizeDSTDayPrev() = %v, want %v", result, tc.expected)
			}
		})
	}
}

// TestPrevWithLocalTimezone tests prepareTimeForPrevSchedule() with time.Local path.
func TestPrevWithLocalTimezone(t *testing.T) {
	// Create a schedule with time.Local location (matches daily at 09:30:00)
	sched := &SpecSchedule{
		Second:   1 << 0,
		Minute:   1 << 30,
		Hour:     1 << 9,
		Dom:      starBit | 0xFFFFFFFE,
		Month:    starBit | 0x1FFE,
		Dow:      starBit | 0x7F,
		Location: time.Local,
	}

	// Use a time in a named timezone (not Local)
	utcTime := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)
	prev := sched.Prev(utcTime)

	// When schedule location is time.Local, Prev uses the input time's location (UTC here).
	// So it finds the previous 09:30:00 UTC before 10:00:00 UTC → same day at 09:30 UTC.
	expected := time.Date(2024, 6, 15, 9, 30, 0, 0, time.UTC)
	if !prev.Equal(expected) {
		t.Errorf("Prev() with time.Local schedule = %v, want %v", prev, expected)
	}
}

func TestWeekdayOccurrence_Boundary(t *testing.T) {
	// Kills ARITHMETIC_BASE mutation at spec.go:155 where (Day()-1) → (Day()+1)
	// Day 7: (7-1)/7 + 1 = 0 + 1 = 1 (1st occurrence)
	// Mutant: (7+1)/7 + 1 = 1 + 1 = 2
	t7 := time.Date(2024, 1, 7, 12, 0, 0, 0, time.UTC) // Jan 7 = Sunday
	if got := weekdayOccurrence(t7); got != 1 {
		t.Errorf("weekdayOccurrence(day 7) = %d, want 1", got)
	}

	// Day 14: (14-1)/7 + 1 = 1 + 1 = 2
	// Mutant: (14+1)/7 + 1 = 2 + 1 = 3
	t14 := time.Date(2024, 1, 14, 12, 0, 0, 0, time.UTC)
	if got := weekdayOccurrence(t14); got != 2 {
		t.Errorf("weekdayOccurrence(day 14) = %d, want 2", got)
	}
}

func TestNearestWeekday_LastDayCalculation(t *testing.T) {
	// Kills ARITHMETIC_BASE mutation at spec.go:174 where (month+1) → (month-1)
	// For February 2024 (leap year, 29 days), targetDay=30 should return -1.
	// Mutant uses month-1 to calculate lastDay, getting December's 31 days,
	// which would NOT return -1 for targetDay=30.
	got := nearestWeekday(2024, time.February, 30)
	if got != -1 {
		t.Errorf("nearestWeekday(2024, Feb, 30) = %d, want -1 (day exceeds month)", got)
	}
}

func TestLastWeekdayOfMonth_Saturday(t *testing.T) {
	// Kills ARITHMETIC_BASE mutation at spec.go:212 where (lastDay - 1) → (lastDay + 1)
	// August 2024 ends on Saturday (Aug 31). Last weekday = Friday Aug 30.
	// Mutant: lastDay + 1 = 32 (wrong).
	got := lastWeekdayOfMonth(2024, time.August)
	if got != 30 {
		t.Errorf("lastWeekdayOfMonth(2024, August) = %d, want 30 (Friday)", got)
	}
}

// TestLastDaySkippedMidnight pins the month length as a calendar fact
// (issue #462). Atlantic/Azores springs forward at 00:00 on 2024-03-31, the
// last day of March; time.Date resolves that midnight to 23:00 on the 30th,
// and a month length taken from it was one day short. LW and nW took the
// month length from noon in the zone, which fails where a zone skips the
// whole last day. Asia/Beirut skips the same midnight with a positive offset
// and was never affected.
func TestLastDaySkippedMidnight(t *testing.T) {
	p := NewParser(Second | Minute | Hour | Dom | Month | Dow | Year | DomL | DomW | DowLast | DowNth)
	tests := []struct {
		spec string
		want time.Time // UTC
	}{
		{"TZ=Atlantic/Azores 0 0 12 L 3 * *", time.Date(2024, 3, 31, 12, 0, 0, 0, time.UTC)},
		{"TZ=Atlantic/Azores 0 0 12 * 3 0#L *", time.Date(2024, 3, 31, 12, 0, 0, 0, time.UTC)},
		{"TZ=Atlantic/Azores 0 0 12 L-1 3 * *", time.Date(2024, 3, 30, 13, 0, 0, 0, time.UTC)},
		// Control: the last Saturday stays the 30th.
		{"TZ=Atlantic/Azores 0 0 12 * 3 6#L *", time.Date(2024, 3, 30, 13, 0, 0, 0, time.UTC)},
		// The skipped midnight itself runs at the end of the gap (ADR-016).
		{"TZ=Atlantic/Azores 0 0 0 L 3 * *", time.Date(2024, 3, 31, 1, 0, 0, 0, time.UTC)},
		{"TZ=Atlantic/Azores 0 0 12 L 3 * 2019", time.Date(2019, 3, 31, 12, 0, 0, 0, time.UTC)},
		{"TZ=Atlantic/Azores 0 0 12 L 3 * 2030", time.Date(2030, 3, 31, 12, 0, 0, 0, time.UTC)},
		{"TZ=America/Scoresbysund 0 0 12 L 3 * 2019", time.Date(2019, 3, 31, 12, 0, 0, 0, time.UTC)},
		// Pacific/Kiritimati skips all of 1994-12-31 (-10 -> +14); the month
		// still has 31 days, so LW is Friday the 30th and 15W exists.
		{"TZ=Pacific/Kiritimati 0 0 12 LW 12 * 1994", time.Date(1994, 12, 30, 22, 0, 0, 0, time.UTC)},
		{"TZ=Pacific/Kiritimati 0 0 12 15W 12 * 1994", time.Date(1994, 12, 15, 22, 0, 0, 0, time.UTC)},
		// Control: positive offset, unchanged.
		{"TZ=Asia/Beirut 0 0 12 L 3 * *", time.Date(2024, 3, 31, 9, 0, 0, 0, time.UTC)},
		{"TZ=Asia/Beirut 0 0 12 * 3 0#L *", time.Date(2024, 3, 31, 9, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			s, err := p.Parse(tt.spec)
			if err != nil {
				t.Fatal(err)
			}
			got := s.Next(time.Date(tt.want.Year(), 1, 10, 0, 0, 0, 0, time.UTC))
			if !got.Equal(tt.want) {
				t.Errorf("Next = %s, want %s", got.UTC(), tt.want)
			}
			// Prev from just after the run finds the same run.
			if prev := s.(*SpecSchedule).Prev(tt.want.Add(time.Minute)); !prev.Equal(tt.want) {
				t.Errorf("Prev = %s, want %s", prev.UTC(), tt.want)
			}
		})
	}
}

func TestRetreatMinute_SubtractsNotAdds(t *testing.T) {
	// Kills ARITHMETIC_BASE mutation at spec.go:532 where (-1*time.Second) → (+1*time.Second)
	// retreatMinute should go to the END of the previous minute (second=59),
	// not the START of the next second (second=1).
	input := time.Date(2024, 1, 1, 12, 31, 0, 0, time.UTC)
	minuteBits := uint64(1 << 30) // match minute 30

	result, added, _ := retreatMinute(input, minuteBits, false)

	if !added {
		t.Error("expected added=true after retreating")
	}
	// Correct: truncate to 12:31:00, subtract 1s → 12:30:59
	// Mutation: truncate to 12:31:00, add 1s → 12:31:01, then -1min → 12:30:01
	if result.Second() != 59 {
		t.Errorf("retreatMinute should land at second 59, got second %d", result.Second())
	}
	if result.Minute() != 30 {
		t.Errorf("retreatMinute should find minute 30, got minute %d", result.Minute())
	}
}

// TestIsDSTFallBackDuplicate tests the isDSTFallBackDuplicate function
// that prevents duplicate job execution during DST fall-back transitions
// (GitHub issue #349).
func TestIsDSTFallBackDuplicate(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("America/New_York timezone not available")
	}

	// Nov 1, 2026: 2:00 AM EDT → 1:00 AM EST (fall-back)
	// 01:30 EDT = 05:30 UTC (first occurrence)
	// 01:30 EST = 06:30 UTC (second occurrence)

	tests := []struct {
		name string
		prev time.Time
		next time.Time
		want bool
	}{
		{
			name: "same wall-clock, different offset (fall-back) — duplicate",
			prev: time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC), // 01:30 EDT
			next: time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC), // 01:30 EST
			want: true,
		},
		{
			name: "different wall-clock times — not duplicate",
			prev: time.Date(2026, 11, 1, 5, 45, 0, 0, time.UTC), // 01:45 EDT
			next: time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC), // 01:30 EST
			want: false,
		},
		{
			name: "same wall-clock, same offset, different day — not duplicate",
			prev: time.Date(2026, 11, 3, 5, 0, 0, 0, time.UTC), // 00:00 EST Nov 3
			next: time.Date(2026, 11, 4, 5, 0, 0, 0, time.UTC), // 00:00 EST Nov 4
			want: false,
		},
		{
			name: "zero prev — not duplicate",
			prev: time.Time{},
			next: time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC),
			want: false,
		},
		{
			name: "hourly at :00, fall-back — duplicate",
			prev: time.Date(2026, 11, 1, 5, 0, 0, 0, time.UTC), // 01:00 EDT
			next: time.Date(2026, 11, 1, 6, 0, 0, 0, time.UTC), // 01:00 EST
			want: true,
		},
		{
			name: "spring-forward — must NOT trigger (offset increases)",
			prev: time.Date(2026, 3, 8, 6, 0, 0, 0, time.UTC), // 01:00 EST
			next: time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC), // 03:00 EDT (2am skipped)
			want: false,
		},
		{
			name: "zero next — not duplicate",
			prev: time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC),
			next: time.Time{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isDSTFallBackDuplicate(tt.prev, tt.next, loc)
			if got != tt.want {
				t.Errorf("isDSTFallBackDuplicate(%v, %v) = %v, want %v",
					tt.prev.In(loc), tt.next.In(loc), got, tt.want)
			}
		})
	}

	// Nil location guard
	t.Run("nil location — not duplicate", func(t *testing.T) {
		prev := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)
		next := time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC)
		if isDSTFallBackDuplicate(prev, next, nil) {
			t.Error("expected false for nil location")
		}
	})

	// Also test with Europe/London (last Sunday of October)
	t.Run("Europe/London fall-back", func(t *testing.T) {
		londonLoc, err := time.LoadLocation("Europe/London")
		if err != nil {
			t.Skip("Europe/London timezone not available")
		}
		// Oct 25, 2026: 2:00 BST → 1:00 GMT (fall-back)
		// 01:30 BST = 00:30 UTC, 01:30 GMT = 01:30 UTC
		prev := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC) // 01:30 BST
		next := time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC) // 01:30 GMT
		if !isDSTFallBackDuplicate(prev, next, londonLoc) {
			t.Errorf("expected duplicate for Europe/London fall-back: prev=%v, next=%v",
				prev.In(londonLoc), next.In(londonLoc))
		}
	})
}
