package cron

import "time"

// SpecSchedule specifies a duty cycle (to the second granularity), based on a
// traditional crontab specification. It is computed initially and stored as bit sets.
type SpecSchedule struct {
	Second, Minute, Hour, Dom, Month, Dow uint64

	// Override location for this schedule.
	Location *time.Location
}

// bounds provides a range of acceptable values (plus a map of name to value).
type bounds struct {
	min, max uint
	names    map[string]uint
}

// The bounds for each field.
var (
	seconds = bounds{0, 59, nil}
	minutes = bounds{0, 59, nil}
	hours   = bounds{0, 23, nil}
	dom     = bounds{1, 31, nil}
	months  = bounds{1, 12, map[string]uint{
		"jan": 1,
		"feb": 2,
		"mar": 3,
		"apr": 4,
		"may": 5,
		"jun": 6,
		"jul": 7,
		"aug": 8,
		"sep": 9,
		"oct": 10,
		"nov": 11,
		"dec": 12,
	}}
	dow = bounds{0, 6, map[string]uint{
		"sun": 0,
		"mon": 1,
		"tue": 2,
		"wed": 3,
		"thu": 4,
		"fri": 5,
		"sat": 6,
	}}
)

const (
	// Set the top bit if a star was included in the expression.
	starBit = 1 << 63
)

// advanceMinute advances time until minute matches, returns new time and wrap flag.
func advanceMinute(t time.Time, minuteBits uint64, added bool) (time.Time, bool, bool) {
	for !fieldMatches(t.Minute(), minuteBits) {
		if !added {
			added = true
			t = t.Truncate(time.Minute)
		}
		t = t.Add(1 * time.Minute)
		if t.Minute() == 0 {
			return t, added, true // wrap
		}
	}
	return t, added, false
}

// advanceSecond advances time until second matches, returns new time and wrap flag.
func advanceSecond(t time.Time, secondBits uint64, added bool) (time.Time, bool, bool) {
	for !fieldMatches(t.Second(), secondBits) {
		if !added {
			added = true
			t = t.Truncate(time.Second)
		}
		t = t.Add(1 * time.Second)
		if t.Second() == 0 {
			return t, added, true // wrap
		}
	}
	return t, added, false
}

// prepareTimeForSchedule converts time to schedule timezone and prepares for matching.
// Returns the prepared time, effective location, and original location for final conversion.
func prepareTimeForSchedule(t time.Time, schedLoc *time.Location) (prepared time.Time, loc, origLocation *time.Location) {
	origLocation = t.Location()
	loc = schedLoc
	if loc == time.Local {
		loc = t.Location()
	}
	if schedLoc != time.Local {
		t = t.In(schedLoc)
	}
	// Start at the earliest possible time (the upcoming second).
	prepared = t.Add(1*time.Second - time.Duration(t.Nanosecond())*time.Nanosecond)
	return
}

// normalizeDSTDay adjusts time when DST causes midnight to not exist.
// For example, Sao Paulo DST transforms midnight on 11/3 into 1am.
func normalizeDSTDay(t time.Time) time.Time {
	if t.Hour() == 0 {
		return t
	}
	if t.Hour() > 12 {
		return t.Add(time.Duration(24-t.Hour()) * time.Hour)
	}
	return t.Add(time.Duration(-t.Hour()) * time.Hour)
}

// checkHourDSTSkip handles ISC cron behavior for DST spring-forward.
// If time was adjusted one hour forward due to DST, jobs that would have
// run in the skipped interval will run immediately.
func checkHourDSTSkip(prev, curr time.Time, hourBits uint64) bool {
	if curr.Hour()-prev.Hour() != 2 {
		return false
	}
	// #nosec G115 -- Hour()-1 bounded 1-22 in DST context
	return 1<<uint(curr.Hour()-1)&hourBits > 0
}

// fieldMatches checks if a time component value matches the schedule bits.
func fieldMatches(value int, bits uint64) bool {
	// #nosec G115 -- time components are bounded and safe for uint
	return 1<<uint(value)&bits != 0
}

// Next returns the next time this schedule is activated, greater than the given time.
// If no time can be found to satisfy the schedule, returns the zero time.
func (s *SpecSchedule) Next(t time.Time) time.Time {
	// General approach: For each field (Month, Day, Hour, Minute, Second),
	// check if it matches. If not, increment until it matches.
	// Wrap-around resets to verify previous fields.

	t, loc, origLocation := prepareTimeForSchedule(t, s.Location)
	added := false // indicates whether a field has been incremented

	// If no time is found within five years, return zero.
	yearLimit := t.Year() + 5

WRAP:
	if t.Year() > yearLimit {
		return time.Time{}
	}

	// Find the first applicable month.
	// If it's this month, then do nothing.
	for !fieldMatches(int(t.Month()), s.Month) {
		// If we have to add a month, reset the other parts to 0.
		if !added {
			added = true
			// Otherwise, set the date at the beginning (since the current time is irrelevant).
			t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc)
		}
		t = t.AddDate(0, 1, 0)

		// Wrapped around.
		if t.Month() == time.January {
			goto WRAP
		}
	}

	// Now get a day in that month.
	//
	// NOTE: This causes issues for daylight savings regimes where midnight does
	// not exist.  For example: Sao Paulo has DST that transforms midnight on
	// 11/3 into 1am. Handle that by noticing when the Hour ends up != 0.
	for !dayMatches(s, t) {
		if !added {
			added = true
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
		}
		t = t.AddDate(0, 0, 1)
		// Handle DST causing midnight to not exist.
		t = normalizeDSTDay(t)

		if t.Day() == 1 {
			goto WRAP
		}
	}

	for !fieldMatches(t.Hour(), s.Hour) {
		if !added {
			added = true
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, loc)
		}
		prev := t
		t = t.Add(1 * time.Hour)
		// ISC cron behavior for DST spring-forward.
		if checkHourDSTSkip(prev, t, s.Hour) {
			break
		}

		if t.Hour() == 0 {
			goto WRAP
		}
	}

	var wrap bool
	t, added, wrap = advanceMinute(t, s.Minute, added)
	if wrap {
		goto WRAP
	}

	t, _, wrap = advanceSecond(t, s.Second, added)
	if wrap {
		goto WRAP
	}

	return t.In(origLocation)
}

// dayMatches returns true if the schedule's day-of-week and day-of-month
// restrictions are satisfied by the given time.
func dayMatches(s *SpecSchedule, t time.Time) bool {
	// #nosec G115 -- Day() returns 1-31, Weekday() returns 0-6, safe for uint
	var (
		domMatch = 1<<uint(t.Day())&s.Dom > 0
		dowMatch = 1<<uint(t.Weekday())&s.Dow > 0
	)
	if s.Dom&starBit > 0 || s.Dow&starBit > 0 {
		return domMatch && dowMatch
	}
	return domMatch || dowMatch
}
