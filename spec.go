package cron

import "time"

// SpecSchedule specifies a duty cycle (to the second granularity), based on a
// traditional crontab specification. It is computed initially and stored as bit sets.
type SpecSchedule struct {
	Second, Minute, Hour, Dom, Month, Dow uint64

	// Year stores valid years using sparse storage for unlimited range.
	// nil means any year (wildcard). An empty map means no valid years.
	// Uses map[int]struct{} for O(1) lookup with minimal memory overhead.
	Year map[int]struct{}

	// Override location for this schedule.
	Location *time.Location

	// MaxSearchYears limits how many years into the future Next() will search
	// before giving up and returning zero time. This prevents infinite loops
	// for unsatisfiable schedules (e.g., Feb 30). Zero means use the default (5 years).
	MaxSearchYears int

	// DomConstraints holds dynamic day-of-month constraints that cannot be
	// pre-computed into bitmasks (L, L-n, LW, nW). These are evaluated at
	// match time because they depend on the specific month.
	DomConstraints []DomConstraint

	// DowConstraints holds nth-weekday-of-month constraints (e.g., FRI#3, MON#L).
	// These are evaluated at match time because they depend on which dates
	// fall on which weekdays in the specific month.
	DowConstraints []DowConstraint

	// DowOrDom enables legacy OR logic for day-of-week and day-of-month matching.
	// When false (default), both DOM and DOW must match (AND logic), consistent
	// with all other cron fields. When true, the schedule matches if either
	// DOM or DOW matches (OR logic), for robfig/cron compatibility.
	DowOrDom bool
}

// DomConstraintType identifies the type of day-of-month constraint.
type DomConstraintType uint8

const (
	// DomLast represents 'L' - the last day of the month.
	DomLast DomConstraintType = iota
	// DomLastOffset represents 'L-n' - n days before the last day of month.
	DomLastOffset
	// DomLastWeekday represents 'LW' - the last weekday (Mon-Fri) of month.
	DomLastWeekday
	// DomNearestWeekday represents 'nW' - the nearest weekday to day n.
	DomNearestWeekday
)

// DomConstraint represents a dynamic day-of-month constraint.
type DomConstraint struct {
	Type DomConstraintType
	N    int // For DomLastOffset: offset; for DomNearestWeekday: day number
}

// DowConstraint represents an nth-weekday-of-month constraint.
// For example, FRI#3 means "3rd Friday of the month".
type DowConstraint struct {
	Weekday int // 0-6 (Sunday=0, Saturday=6)
	N       int // 1-5 for nth occurrence, -1 for last occurrence
}

// bounds provides a range of acceptable values (plus a map of name to value).
type bounds struct {
	min, max   uint
	names      map[string]uint
	wraparound bool // whether this field supports wraparound ranges (e.g., 22-2 for hours)
}

// YearBase is the minimum valid year for the Year field.
// Set to 1 CE to allow any reasonable historical or future date.
const YearBase = 1

// YearMax is the maximum valid year for the Year field.
// With sparse map[int]struct{} storage, there is no technical limit.
// Using math.MaxInt32 (2147483647) ensures compatibility across platforms
// while being effectively unlimited for any practical scheduling use.
const YearMax = 1<<31 - 1 // 2147483647

// The bounds for each field.
// Fields with wraparound=true support ranges where start > end (e.g., 22-2 for hours).
var (
	seconds = bounds{0, 59, nil, true}              // cyclic: supports wraparound
	minutes = bounds{0, 59, nil, true}              // cyclic: supports wraparound
	hours   = bounds{0, 23, nil, true}              // cyclic: supports wraparound
	dom     = bounds{1, 31, nil, true}              // cyclic: supports wraparound (non-existent days skipped)
	years   = bounds{YearBase, YearMax, nil, false} // non-cyclic: no wraparound
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
	}, true} // cyclic: supports wraparound (e.g., NOV-FEB)
	dow = bounds{0, 7, map[string]uint{
		"sun": 0,
		"mon": 1,
		"tue": 2,
		"wed": 3,
		"thu": 4,
		"fri": 5,
		"sat": 6,
	}, true} // cyclic: supports wraparound (e.g., FRI-MON)
)

const (
	// starBit marks a field that was specified with a wildcard (*).
	// Using bit 63 (MSB of uint64) ensures it cannot conflict with any valid
	// schedule bit: seconds/minutes use bits 0-59, hours 0-23, days 1-31,
	// months 1-12, weekdays 0-6. All are well below bit 63.
	starBit = 1 << 63

	// dowBit7 represents Sunday specified as 7 (alternative to 0).
	// This bit is normalized to bit 0 after parsing.
	dowBit7 = 1 << 7

	// defaultSearchYears is the default limit for how many years into the future
	// Next() will search before giving up. This prevents infinite loops for
	// unsatisfiable schedules (e.g., Feb 30). Users can override this via
	// Parser.WithMaxSearchYears() or the WithMaxSearchYears() cron option.
	defaultSearchYears = 5
)

// NormalizeDOW normalizes the day-of-week bitmask by mapping bit 7 (Sunday as 7)
// to bit 0 (Sunday as 0). This allows both "0" and "7" to represent Sunday,
// matching the behavior of many cron implementations.
func NormalizeDOW(bits uint64) uint64 {
	if bits&dowBit7 != 0 {
		bits = (bits | 1) &^ dowBit7 // Set bit 0, clear bit 7
	}
	return bits
}

// lastDayOfMonth returns the last day of the month for the given time.
func lastDayOfMonth(t time.Time) int {
	// Go to first day of next month, then back one day
	return time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, t.Location()).Day()
}

// weekdayOccurrence returns which occurrence (1-5) of the weekday this date
// represents within its month. For example, if t is the 3rd Friday of the month,
// this returns 3.
func weekdayOccurrence(t time.Time) int {
	return (t.Day()-1)/7 + 1
}

// isLastOccurrence returns true if this is the last occurrence of the given
// weekday in the month. For example, if t is a Friday and there are no more
// Fridays in the month, this returns true.
func isLastOccurrence(t time.Time) bool {
	return t.Day()+7 > lastDayOfMonth(t)
}

// nearestWeekday returns the day of month that is the nearest weekday to the
// given target day. If the target is a weekday, returns the target.
// If Saturday, returns Friday (or Monday if that would go to previous month).
// If Sunday, returns Monday (or Friday if that would go to next month).
// If targetDay exceeds the month's length, returns -1 (no match).
// Use LW syntax instead if you want "last weekday of month" semantics.
func nearestWeekday(year int, month time.Month, targetDay int, loc *time.Location) int {
	// Calculate lastDay BEFORE creating time with targetDay to avoid Go's date normalization.
	// Go's time.Date(2024, Feb, 31) normalizes to March 2, corrupting the month context.
	lastDay := time.Date(year, month+1, 0, 12, 0, 0, 0, loc).Day()

	// If targetDay doesn't exist in this month, return -1 (no match).
	// For "last weekday" semantics, use LW instead of 31W.
	if targetDay > lastDay {
		return -1
	}

	t := time.Date(year, month, targetDay, 12, 0, 0, 0, loc)

	wd := t.Weekday()
	switch wd {
	case time.Saturday:
		// Try Friday first
		if targetDay > 1 {
			return targetDay - 1
		}
		// If target is 1st (Saturday), use Monday the 3rd
		return targetDay + 2
	case time.Sunday:
		// Try Monday first
		if targetDay < lastDay {
			return targetDay + 1
		}
		// If target is last day (Sunday), use Friday before
		return targetDay - 2
	default:
		return targetDay
	}
}

// lastWeekdayOfMonth returns the day of month that is the last weekday (Mon-Fri).
func lastWeekdayOfMonth(year int, month time.Month, loc *time.Location) int {
	lastDay := time.Date(year, month+1, 0, 12, 0, 0, 0, loc).Day()
	t := time.Date(year, month, lastDay, 12, 0, 0, 0, loc)
	wd := t.Weekday()
	switch wd {
	case time.Saturday:
		return lastDay - 1
	case time.Sunday:
		return lastDay - 2
	default:
		return lastDay
	}
}

// domConstraintMatches checks if any of the DOM constraints match the given time.
func domConstraintMatches(constraints []DomConstraint, t time.Time) bool {
	day := t.Day()
	for _, c := range constraints {
		switch c.Type {
		case DomLast:
			if day == lastDayOfMonth(t) {
				return true
			}
		case DomLastOffset:
			if day == lastDayOfMonth(t)-c.N {
				return true
			}
		case DomLastWeekday:
			if day == lastWeekdayOfMonth(t.Year(), t.Month(), t.Location()) {
				return true
			}
		case DomNearestWeekday:
			if day == nearestWeekday(t.Year(), t.Month(), c.N, t.Location()) {
				return true
			}
		}
	}
	return false
}

// dowConstraintMatches checks if any of the DOW constraints match the given time.
func dowConstraintMatches(constraints []DowConstraint, t time.Time) bool {
	wd := int(t.Weekday())
	occ := weekdayOccurrence(t)
	isLast := isLastOccurrence(t)

	for _, c := range constraints {
		if c.Weekday != wd {
			continue
		}
		switch c.N {
		case -1:
			// Last occurrence
			if isLast {
				return true
			}
		case occ:
			return true
		}
	}
	return false
}

// advanceMinute advances time until the minute field matches the schedule bitmask.
// It returns the updated time, an 'added' flag indicating if time was modified,
// and a 'wrap' flag that is true if the minute rolled past 59 to 0.
// When wrap is true, the caller must increment the hour and re-validate.
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

// advanceSecond advances time until the second field matches the schedule bitmask.
// It returns the updated time, an 'added' flag indicating if time was modified,
// and a 'wrap' flag that is true if the second rolled past 59 to 0.
// When wrap is true, the caller must increment the minute and re-validate.
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

// fieldMatches checks if a time component value matches the schedule bitmask.
// It returns true if the bit at position 'value' is set in 'bits'.
// For example, fieldMatches(5, bits) checks if bit 5 (representing minute 5,
// hour 5, etc.) is set in the schedule.
func fieldMatches(value int, bits uint64) bool {
	// #nosec G115 -- time components are bounded and safe for uint
	return 1<<uint(value)&bits != 0
}

// yearMatches checks if a year matches the year set.
// A nil yearSet means any year (wildcard).
// An empty yearSet means no year matches.
func yearMatches(year int, yearSet map[int]struct{}) bool {
	if yearSet == nil {
		return true // nil = wildcard (any year)
	}
	_, ok := yearSet[year]
	return ok
}

// Next returns the next time this schedule is activated, greater than the given time.
// If no time can be found to satisfy the schedule, returns the zero time.
func (s *SpecSchedule) Next(t time.Time) time.Time {
	// General approach: For each field (Month, Day, Hour, Minute, Second),
	// check if it matches. If not, increment until it matches.
	// Wrap-around resets to verify previous fields.

	t, loc, origLocation := prepareTimeForSchedule(t, s.Location)
	added := false // indicates whether a field has been incremented

	// If no time is found within the search limit, return zero.
	// Use configured MaxSearchYears if set, otherwise use default.
	searchYears := s.MaxSearchYears
	if searchYears <= 0 {
		searchYears = defaultSearchYears
	}
	yearLimit := t.Year() + searchYears

WRAP:
	if t.Year() > yearLimit {
		return time.Time{}
	}

	// Check year constraint first (if Year field is not wildcard).
	// Advance to next valid year if current year doesn't match.
	for !yearMatches(t.Year(), s.Year) {
		if !added {
			added = true
			t = time.Date(t.Year(), 1, 1, 0, 0, 0, 0, loc)
		}
		t = t.AddDate(1, 0, 0)
		if t.Year() > yearLimit {
			return time.Time{}
		}
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
//
// By default, both DOM and DOW must match (AND logic), consistent with how
// all other cron fields work. This enables useful patterns like:
//   - "0 0 25-31 * FRI" = last Friday of month
//   - "0 0 1-7 * MON" = first Monday of month
//
// When DowOrDom is true (legacy mode), the behavior matches robfig/cron:
// OR logic when both fields are restricted, AND when either is wildcard.
func dayMatches(s *SpecSchedule, t time.Time) bool {
	// #nosec G115 -- Day() returns 1-31, Weekday() returns 0-6, safe for uint
	var (
		domMatch = 1<<uint(t.Day())&s.Dom > 0
		dowMatch = 1<<uint(t.Weekday())&s.Dow > 0
	)

	// Check dynamic constraints (L, W, #n patterns)
	if len(s.DomConstraints) > 0 && domConstraintMatches(s.DomConstraints, t) {
		domMatch = true
	}
	if len(s.DowConstraints) > 0 && dowConstraintMatches(s.DowConstraints, t) {
		dowMatch = true
	}

	// Legacy OR mode (robfig/cron compatibility): OR when both fields are
	// restricted, AND when either is wildcard
	if s.DowOrDom {
		if s.Dom&starBit > 0 || s.Dow&starBit > 0 {
			return domMatch && dowMatch
		}
		return domMatch || dowMatch
	}

	// Default AND mode: both must match (consistent with other fields)
	return domMatch && dowMatch
}

// prepareTimeForPrevSchedule converts time to schedule timezone and prepares for backwards matching.
// Returns the prepared time, effective location, and original location for final conversion.
func prepareTimeForPrevSchedule(t time.Time, schedLoc *time.Location) (prepared time.Time, loc, origLocation *time.Location) {
	origLocation = t.Location()
	loc = schedLoc
	if loc == time.Local {
		loc = t.Location()
	}
	if schedLoc != time.Local {
		t = t.In(schedLoc)
	}
	// Start at the latest possible time before t (the previous second).
	prepared = t.Add(-1*time.Second - time.Duration(t.Nanosecond())*time.Nanosecond)
	return
}

// retreatMinute retreats time until the minute field matches the schedule bitmask.
// It returns the updated time, an 'added' flag indicating if time was modified,
// and a 'wrap' flag that is true if the minute rolled past 0 to 59.
// When wrap is true, the caller must decrement the hour and re-validate.
func retreatMinute(t time.Time, minuteBits uint64, added bool) (time.Time, bool, bool) {
	for !fieldMatches(t.Minute(), minuteBits) {
		cur := t.Minute()
		if !added {
			added = true
			// Truncate to beginning of current minute, then go back 1 second
			t = t.Truncate(time.Minute)
			t = t.Add(-1 * time.Second)
		} else {
			t = t.Add(-1 * time.Minute)
		}
		if t.Minute() > cur {
			return t, added, true // wrap
		}
	}
	return t, added, false
}

// retreatSecond retreats time until the second field matches the schedule bitmask.
// It returns the updated time, an 'added' flag indicating if time was modified,
// and a 'wrap' flag that is true if the second rolled past 0 to 59.
// When wrap is true, the caller must decrement the minute and re-validate.
func retreatSecond(t time.Time, secondBits uint64, added bool) (time.Time, bool, bool) {
	for !fieldMatches(t.Second(), secondBits) {
		cur := t.Second()
		if !added {
			added = true
			t = t.Truncate(time.Second)
		}
		t = t.Add(-1 * time.Second)
		if t.Second() > cur {
			return t, added, true // wrap
		}
	}
	return t, added, false
}

// Prev returns the previous time this schedule was activated, earlier than the given time.
// If no time can be found to satisfy the schedule, returns the zero time.
func (s *SpecSchedule) Prev(t time.Time) time.Time {
	// General approach: For each field (Month, Day, Hour, Minute, Second),
	// check if it matches. If not, decrement until it matches.
	// Wrap-around resets to verify previous fields.

	t, loc, origLocation := prepareTimeForPrevSchedule(t, s.Location)
	added := false // indicates whether a field has been decremented

	// If no time is found within the search limit, return zero.
	// Use configured MaxSearchYears if set, otherwise use default.
	searchYears := s.MaxSearchYears
	if searchYears <= 0 {
		searchYears = defaultSearchYears
	}
	yearLimit := t.Year() - searchYears

WRAP:
	if t.Year() < yearLimit {
		return time.Time{}
	}

	// Check year constraint first (if Year field is not wildcard).
	// Retreat to previous valid year if current year doesn't match.
	for !yearMatches(t.Year(), s.Year) {
		if !added {
			added = true
			// Set to end of current year (Dec 31, 23:59:59)
			t = time.Date(t.Year(), 12, 31, 23, 59, 59, 0, loc)
		}
		t = t.AddDate(-1, 0, 0)
		if t.Year() < yearLimit {
			return time.Time{}
		}
	}

	// Find the last applicable month.
	// If it's this month, then do nothing.
	for !fieldMatches(int(t.Month()), s.Month) {
		cur := t.Month()
		if !added {
			added = true
			// Set to start of month, then go back 1 second to end of previous month
			t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc)
			t = t.Add(-1 * time.Second)
		} else {
			// Go to the first day of current month, then back 1 second
			t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc)
			t = t.Add(-1 * time.Second)
		}
		// Check for wrap to previous year
		if t.Month() > cur {
			goto WRAP
		}
	}

	// Now get a day in that month (going backwards).
	for !dayMatches(s, t) {
		cur := t.Day()
		if !added {
			added = true
			// Set to end of current day
			t = time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, loc)
		}
		t = t.AddDate(0, 0, -1)
		// Handle DST causing issues
		t = normalizeDSTDayPrev(t)

		if t.Day() > cur {
			goto WRAP
		}
	}

	// Find matching hour (going backwards)
	for !fieldMatches(t.Hour(), s.Hour) {
		cur := t.Hour()
		if !added {
			added = true
			// Set to end of current hour
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 59, 59, 0, loc)
		} else {
			t = t.Add(-1 * time.Hour)
		}

		if t.Hour() > cur {
			goto WRAP
		}
	}

	var wrap bool
	t, added, wrap = retreatMinute(t, s.Minute, added)
	if wrap {
		goto WRAP
	}

	t, _, wrap = retreatSecond(t, s.Second, added)
	if wrap {
		goto WRAP
	}

	return t.In(origLocation)
}

// normalizeDSTDayPrev adjusts time when DST causes issues going backwards.
// Similar to normalizeDSTDay but for backwards traversal.
func normalizeDSTDayPrev(t time.Time) time.Time {
	// Ensure we're at the end of the day
	if t.Hour() == 23 {
		return t
	}
	// If we're at a weird hour due to DST, adjust to end of day
	return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, t.Location())
}
