// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT.

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

	// DSTSkip disables ISC cron behavior for DST spring-forward transitions.
	// When false (default), a job scheduled in a spring-forward gap runs once
	// per gap, at its earliest skipped time shifted by the length of the gap
	// (2:30 runs at 3:30 after a one-hour gap). When true, that occurrence is
	// skipped, matching robfig/cron.
	DSTSkip bool
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
	min, max     uint
	names        map[string]uint
	wraparound   bool // whether this field supports wraparound ranges (e.g., 22-2 for hours)
	lenientSteps bool // whether a step at or above the range size is accepted (LenientSteps)
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
	seconds = bounds{0, 59, nil, true, false}              // cyclic: supports wraparound
	minutes = bounds{0, 59, nil, true, false}              // cyclic: supports wraparound
	hours   = bounds{0, 23, nil, true, false}              // cyclic: supports wraparound
	dom     = bounds{1, 31, nil, true, false}              // cyclic: supports wraparound (non-existent days skipped)
	years   = bounds{YearBase, YearMax, nil, false, false} // non-cyclic: no wraparound
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
	}, true, false} // cyclic: supports wraparound (e.g., NOV-FEB)
	dow = bounds{0, 7, map[string]uint{
		"sun": 0,
		"mon": 1,
		"tue": 2,
		"wed": 3,
		"thu": 4,
		"fri": 5,
		"sat": 6,
	}, true, false} // cyclic: supports wraparound (e.g., FRI-MON)
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
	var change time.Time // the next zone change, looked up once it matters
	looked := false
	for minute := t.Minute(); !fieldMatches(minute, minuteBits); {
		if !added {
			added = true
			t = t.Truncate(time.Minute)
		}
		if !looked {
			change, looked = zoneEndAfter(t), true
		}
		t = t.Add(1 * time.Minute)
		minute = t.Minute()
		// A new hour starts at minute 0, or at a zone change: 01:59 -> 02:30
		// across a 30-minute gap, 03:44 -> 02:45 in Pacific/Chatham.
		if minute == 0 || (!change.IsZero() && !t.Before(change)) {
			return t, added, true // wrap
		}
	}
	return t, added, false
}

// advanceHour advances time until the hour field matches the schedule bitmask.
// It returns the updated time, an 'added' flag indicating if time was modified,
// and a 'wrap' flag that is true if the day changed.
// When wrap is true, the caller must re-validate the date fields.
func advanceHour(t time.Time, hourBits uint64, added bool) (time.Time, bool, bool) {
	for !fieldMatches(t.Hour(), hourBits) {
		added = true
		day := t.Day()
		t = nextHourStart(t)
		// A new day can start at 01:00 when DST skips midnight.
		if t.Day() != day {
			return t, added, true // wrap
		}
	}
	return t, added, false
}

// zoneEndAfter returns the end of the zone period that contains t: the first
// zone change after t, or the zero time when none follows. For years past its
// stored transitions, ZoneBounds reports bounds at year ends, and for a time
// just after such a bound it returns an end that is not after the time (for
// example 2040-12-31 00:00 UTC); zoneEndAfter steps over it.
func zoneEndAfter(t time.Time) time.Time {
	probe := t
	for range 48 {
		_, end := probe.ZoneBounds()
		if end.IsZero() || end.After(t) {
			return end
		}
		probe = probe.Add(time.Hour)
	}
	return time.Time{}
}

// nextHourStart returns the start of the wall-clock hour after t's hour.
// When that hour starts in a DST spring-forward gap, it returns the end of
// the gap (02:30 for a gap from 02:00 to 02:30). When it starts twice because
// of a fall-back, it returns the first copy. Stepping by wall-clock hours
// keeps the result on the full hour after a 30- or 45-minute shift.
func nextHourStart(t time.Time) time.Time {
	want := (t.Hour() + 1) % 24
	// One real hour on from a full hour is the next full hour unless a
	// transition lies in between.
	if t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0 {
		if next := t.Add(time.Hour); next.Hour() == want && next.Minute() == 0 {
			return next
		}
	}
	next := nextFullHour(t, want)
	// A transition that changes the hour comes first: the end of a gap that
	// swallowed the full hour (02:00 -> 02:30), or a fall-back at a minute
	// other than 00 that starts an hour again (Chatham: 03:44:59 +1345 ->
	// 02:45 +1245).
	if end := zoneEndAfter(t); !end.IsZero() && end.Before(next) && end.Hour() != t.Hour() {
		return end
	}
	return next
}

// nextFullHour returns the first instant after t that reads want:00. When a
// spring-forward gap swallowed that time, it returns one real hour after t;
// nextHourStart then stops at the end of the gap, which comes first.
func nextFullHour(t time.Time, want int) time.Time {
	next := time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location())
	if next.Hour() == want && next.Minute() == 0 {
		// time.Date may pick either copy of a repeated wall time; take the
		// first one that lies after t.
		if first := earlierCopy(next); first.After(t) {
			return first
		}
		if next.After(t) {
			return next
		}
		if second := laterCopy(next); second.After(t) {
			return second
		}
	}
	return t.Add(time.Hour)
}

// prevHourEnd returns the last second (hh:59:59) of the wall-clock hour
// before t's hour. When that second lies in a DST spring-forward gap, it
// returns the last second before the gap. When it exists twice because of a
// fall-back, it returns the second copy, the later one.
func prevHourEnd(t time.Time) time.Time {
	prev := prevFullHourEnd(t, (t.Hour()+23)%24)
	// A transition that changes the hour comes first: the last second before
	// a gap that swallowed the full hour end, which can belong to an earlier
	// hour, or a fall-back at a minute other than 00 that ends an hour again
	// (Chatham: 03:44:59 +1345 before 02:45 +1245).
	if start, _ := t.ZoneBounds(); !start.IsZero() && !start.After(t) {
		if last := start.Add(-time.Second); last.After(prev) && last.Hour() != t.Hour() {
			return last
		}
	}
	return prev
}

// prevFullHourEnd returns the last instant before t that reads want:59:59.
// When a spring-forward gap swallowed that time, it returns one real hour
// before t; prevHourEnd then stops at the last second before the gap, which
// comes later.
func prevFullHourEnd(t time.Time, want int) time.Time {
	prev := time.Date(t.Year(), t.Month(), t.Day(), t.Hour()-1, 59, 59, 0, t.Location())
	if prev.Hour() == want && prev.Minute() == 59 {
		// time.Date may pick either copy of a repeated wall time; take the
		// last one that lies before t.
		if second := laterCopy(prev); second.Before(t) {
			return second
		}
		if prev.Before(t) {
			return prev
		}
		if first := earlierCopy(prev); first.Before(t) {
			return first
		}
	}
	return t.Add(-1 * time.Hour)
}

// earlierCopy returns the first copy of t's wall-clock time when t lies in
// the second copy of a DST fall-back period, and t otherwise.
func earlierCopy(t time.Time) time.Time {
	start, _ := t.ZoneBounds()
	if start.IsZero() {
		return t
	}
	_, before := start.Add(-time.Nanosecond).Zone()
	_, after := start.Zone()
	repeated := time.Duration(before-after) * time.Second
	if repeated <= 0 || !t.Before(start.Add(repeated)) {
		return t
	}
	return t.Add(-repeated)
}

// laterCopy returns the second copy of t's wall-clock time when t lies in
// the first copy of a DST fall-back period, and t otherwise.
func laterCopy(t time.Time) time.Time {
	_, end := t.ZoneBounds()
	if end.IsZero() {
		return t
	}
	_, before := t.Zone()
	_, after := end.Zone()
	repeated := time.Duration(before-after) * time.Second
	if repeated <= 0 || t.Before(end.Add(-repeated)) {
		return t
	}
	return t.Add(repeated)
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

// nextDay returns the start of the day after t's day.
func nextDay(t time.Time) time.Time {
	day := t.Day()
	// Handle DST causing midnight to not exist.
	t = normalizeDSTDay(t.AddDate(0, 0, 1))
	// A zone can skip a whole day (Pacific/Apia on 2011-12-30); AddDate then
	// makes no progress.
	if t.Day() == day {
		t = normalizeDSTDay(t.Add(24 * time.Hour))
	}
	return t
}

// prevDay returns the end of the day before t's day.
func prevDay(t time.Time) time.Time {
	day := t.Day()
	t = normalizeDSTDayPrev(t.AddDate(0, 0, -1))
	// A zone can skip a whole day; AddDate then makes no progress.
	if t.Day() == day {
		t = normalizeDSTDayPrev(t.Add(-24 * time.Hour))
	}
	return t
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

// fieldMatches checks if a time component value matches the schedule bitmask.
// It returns true if the bit at position 'value' is set in 'bits'.
// For example, fieldMatches(5, bits) checks if bit 5 (representing minute 5,
// hour 5, etc.) is set in the schedule.
func fieldMatches(value int, bits uint64) bool {
	// #nosec G115 -- time components are bounded and safe for uint
	return 1<<uint(value)&bits != 0
}

// matchesWall reports whether every field of s matches the wall-clock time
// that t shows, ignoring t's location.
func (s *SpecSchedule) matchesWall(t time.Time) bool {
	return yearMatches(t.Year(), s.Year) &&
		fieldMatches(int(t.Month()), s.Month) &&
		dayMatches(s, t) &&
		fieldMatches(t.Hour(), s.Hour) &&
		fieldMatches(t.Minute(), s.Minute) &&
		fieldMatches(t.Second(), s.Second)
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
//
// A time skipped by a DST spring-forward gap runs once per gap, shifted by the
// length of the gap (ISC cron behavior, ADR-016), unless DSTSkip is set.
func (s *SpecSchedule) Next(t time.Time) time.Time {
	next := s.nextExisting(t)
	if s.DSTSkip {
		return next
	}
	if run, ok := s.nextGapRun(t, next); ok {
		return run.In(t.Location())
	}
	return next
}

// nextExisting returns the next time after t whose wall clock matches the
// schedule; times skipped by a DST spring-forward gap never match.
func (s *SpecSchedule) nextExisting(t time.Time) time.Time {
	// General approach: For each field (Month, Day, Hour, Minute, Second),
	// check if it matches. If not, increment until it matches.
	// Wrap-around resets to verify previous fields.

	t, loc, origLocation := prepareTimeForSchedule(t, s.Location)
	added := false // indicates whether a field has been incremented

	// If no time is found within the search limit, return zero.
	// Use configured MaxSearchYears if set, otherwise use default.
	yearLimit := t.Year() + s.searchSpan()

	// Each pass that wraps a field around starts again from the year.
	for t.Year() <= yearLimit {
		var step searchStep
		t, added, step = s.advanceDate(t, loc, added, yearLimit)
		if step == stepExhausted {
			return time.Time{}
		}
		if step == stepWrapped {
			continue
		}
		var wrap bool
		if t, added, wrap = s.advanceTime(t, added); !wrap {
			return t.In(origLocation)
		}
	}
	return time.Time{}
}

// searchStep is the outcome of moving t to the next or previous matching
// date.
type searchStep int

const (
	stepFound     searchStep = iota // t is on a matching date
	stepWrapped                     // a field wrapped around; search again
	stepExhausted                   // the search limit was passed
)

// advanceDate moves t forward to the first matching year, month and day,
// resetting the time of day when it moves.
func (s *SpecSchedule) advanceDate(t time.Time, loc *time.Location, added bool, yearLimit int) (time.Time, bool, searchStep) {
	// Check year constraint first (if Year field is not wildcard).
	// Advance to next valid year if current year doesn't match.
	for !yearMatches(t.Year(), s.Year) {
		if !added {
			added = true
			t = time.Date(t.Year(), 1, 1, 0, 0, 0, 0, loc)
		}
		t = t.AddDate(1, 0, 0)
		if t.Year() > yearLimit {
			return t, added, stepExhausted
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
			return t, added, stepWrapped
		}
	}

	return s.advanceDay(t, loc, added)
}

// advanceDay moves t forward to the first matching day of its month.
//
// NOTE: This causes issues for daylight savings regimes where midnight does
// not exist.  For example: Sao Paulo has DST that transforms midnight on
// 11/3 into 1am. Handle that by noticing when the Hour ends up != 0.
func (s *SpecSchedule) advanceDay(t time.Time, loc *time.Location, added bool) (time.Time, bool, searchStep) {
	for !dayMatches(s, t) {
		if !added {
			added = true
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
		}
		t = nextDay(t)

		if t.Day() == 1 {
			return t, added, stepWrapped
		}
	}
	return t, added, stepFound
}

// advanceTime moves t forward to the first matching hour, minute and second
// of its day. wrap reports that a field wrapped into the next day; added
// carries over to the next pass, as it did with the former goto.
func (s *SpecSchedule) advanceTime(t time.Time, added bool) (_ time.Time, _, wrap bool) {
	if t, added, wrap = advanceHour(t, s.Hour, added); wrap {
		return t, added, true
	}
	if t, added, wrap = advanceMinute(t, s.Minute, added); wrap {
		return t, added, true
	}
	t, _, wrap = advanceSecond(t, s.Second, added)
	return t, added, wrap
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
		cur, hour := t.Minute(), t.Hour()
		if !added {
			added = true
			// Truncate to beginning of current minute, then go back 1 second
			t = t.Truncate(time.Minute)
			t = t.Add(-1 * time.Second)
		} else {
			t = t.Add(-1 * time.Minute)
		}
		// A 45-minute DST gap moves from 03:45 to 02:44, not to minute 59.
		if t.Minute() > cur || t.Hour() != hour {
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

// retreatHour retreats time until the hour field matches the schedule bitmask.
// It returns the updated time, an 'added' flag indicating if time was modified,
// and a 'wrap' flag that is true if the hour rolled past 0 to 23.
// When wrap is true, the caller must decrement the day and re-validate.
func retreatHour(t time.Time, hourBits uint64, added bool) (_ time.Time, _, wrap bool) {
	for !fieldMatches(t.Hour(), hourBits) {
		cur := t.Hour()
		added = true
		t = prevHourEnd(t)
		if t.Hour() > cur {
			return t, added, true
		}
	}
	return t, added, false
}

// Prev returns the previous time this schedule was activated, earlier than the given time.
// If no time can be found to satisfy the schedule, returns the zero time.
// It reports the runs for DST spring-forward gaps that Next reports.
func (s *SpecSchedule) Prev(t time.Time) time.Time {
	prev := s.prevExisting(t)
	if s.DSTSkip {
		return prev
	}
	if run, ok := s.prevGapRun(t, prev); ok {
		return run.In(t.Location())
	}
	return prev
}

// prevExisting returns the latest time before t whose wall clock matches the
// schedule; times skipped by a DST spring-forward gap never match.
func (s *SpecSchedule) prevExisting(t time.Time) time.Time {
	// General approach: For each field (Month, Day, Hour, Minute, Second),
	// check if it matches. If not, decrement until it matches.
	// Wrap-around resets to verify previous fields.

	t, loc, origLocation := prepareTimeForPrevSchedule(t, s.Location)
	added := false // indicates whether a field has been decremented

	// If no time is found within the search limit, return zero.
	// Use configured MaxSearchYears if set, otherwise use default.
	yearLimit := t.Year() - s.searchSpan()

	// Each pass that wraps a field around starts again from the year.
	for t.Year() >= yearLimit {
		var step searchStep
		t, added, step = s.retreatDate(t, loc, added, yearLimit)
		if step == stepExhausted {
			return time.Time{}
		}
		if step == stepWrapped {
			continue
		}
		var wrap bool
		if t, added, wrap = s.retreatTime(t, added); !wrap {
			return t.In(origLocation)
		}
	}
	return time.Time{}
}

// retreatDate moves t back to the last matching year, month and day, setting
// the time of day to its end when it moves.
func (s *SpecSchedule) retreatDate(t time.Time, loc *time.Location, added bool, yearLimit int) (time.Time, bool, searchStep) {
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
			return t, added, stepExhausted
		}
	}

	// Find the last applicable month.
	// If it's this month, then do nothing.
	for !fieldMatches(int(t.Month()), s.Month) {
		cur := t.Month()
		added = true
		// Go to the first day of the month, then back 1 second to the end of
		// the previous month. When midnight lies in a repeated fall-back
		// period, time.Date may pick its second copy, and one second before
		// that is still this month (Asia/Gaza on 2004-10-01); start from the
		// first copy.
		t = earlierCopy(time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc))
		t = t.Add(-1 * time.Second)
		// Check for wrap to previous year
		if t.Month() > cur {
			return t, added, stepWrapped
		}
	}

	return s.retreatDay(t, loc, added)
}

// retreatDay moves t back to the last matching day of its month.
func (s *SpecSchedule) retreatDay(t time.Time, loc *time.Location, added bool) (time.Time, bool, searchStep) {
	for !dayMatches(s, t) {
		cur := t.Day()
		if !added {
			added = true
			// Set to end of current day
			t = time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, loc)
		}
		t = prevDay(t)

		if t.Day() > cur {
			return t, added, stepWrapped
		}
	}
	return t, added, stepFound
}

// retreatTime moves t back to the last matching hour, minute and second of
// its day. wrap reports that a field wrapped into the previous day; added
// carries over to the next pass, as it did with the former goto.
func (s *SpecSchedule) retreatTime(t time.Time, added bool) (_ time.Time, _, wrap bool) {
	if t, added, wrap = retreatHour(t, s.Hour, added); wrap {
		return t, added, true
	}
	if t, added, wrap = retreatMinute(t, s.Minute, added); wrap {
		return t, added, true
	}
	t, _, wrap = retreatSecond(t, s.Second, added)
	return t, added, wrap
}

// scheduleLocation returns the location whose wall clock the schedule matches
// for a call with t, as prepareTimeForSchedule does.
func (s *SpecSchedule) scheduleLocation(t time.Time) *time.Location {
	if s.Location == time.Local {
		return t.Location()
	}
	return s.Location
}

// searchSpan returns how far Next and Prev search for a run.
func (s *SpecSchedule) searchSpan() int {
	if s.MaxSearchYears > 0 {
		return s.MaxSearchYears
	}
	return defaultSearchYears
}

// nextGapRun returns the earliest DST gap run after t and before next. A zero
// next means that no time after t matches within the search limit.
func (s *SpecSchedule) nextGapRun(t, next time.Time) (time.Time, bool) {
	loc := s.scheduleLocation(t)
	limit := next
	if limit.IsZero() {
		// nextExisting searches to the end of year t.Year()+searchSpan.
		limit = time.Date(t.In(loc).Year()+s.searchSpan()+1, 1, 1, 0, 0, 0, 0, loc)
	}
	// The run for the gap that opened the zone in effect at t can still lie
	// after t (for 01:59:59 -> 03:00, a 02:30 job runs at 03:30).
	tr, _ := t.In(loc).ZoneBounds()
	if tr.IsZero() || tr.After(t) || !t.Before(tr.Add(gapLength(tr, loc))) {
		// A gap run lies before the end of its gap; past it, only later
		// transitions can hold a run.
		tr = zoneEndAfter(t.In(loc))
	}
	for !tr.IsZero() && tr.Before(limit) {
		if run, ok := s.gapRun(tr, loc); ok && run.After(t) && run.Before(limit) {
			return run, true
		}
		tr = zoneEndAfter(tr.In(loc))
	}
	return time.Time{}, false
}

// prevGapRun returns the latest DST gap run before t and after prev. A zero
// prev means that no time before t matches within the search limit.
func (s *SpecSchedule) prevGapRun(t, prev time.Time) (time.Time, bool) {
	loc := s.scheduleLocation(t)
	limit := prev
	if limit.IsZero() {
		// prevExisting searches back to the start of year t.Year()-searchSpan.
		limit = time.Date(t.In(loc).Year()-s.searchSpan(), 1, 1, 0, 0, 0, 0, loc).Add(-time.Nanosecond)
	}
	before := t.Truncate(time.Second) // Prev ignores fractions of a second
	tr, _ := t.In(loc).ZoneBounds()
	for !tr.IsZero() {
		if run, ok := s.gapRun(tr, loc); ok && run.Before(before) && run.After(limit) {
			return run, true
		}
		if !tr.After(limit) {
			break
		}
		tr, _ = tr.Add(-time.Nanosecond).In(loc).ZoneBounds()
	}
	return time.Time{}, false
}

// gapLength returns how far the zone transition tr moves the clock forward:
// the length of a spring-forward gap, or zero or less for any other
// transition.
func gapLength(tr time.Time, loc *time.Location) time.Duration {
	_, before := tr.Add(-time.Nanosecond).In(loc).Zone()
	_, after := tr.In(loc).Zone()
	return time.Duration(after-before) * time.Second
}

// gapRun returns the run for the DST spring-forward gap that opens at the
// zone transition tr: the earliest scheduled wall time that the gap skips,
// shifted by the length of the gap. All other times in the gap merge into
// this run, so the schedule runs once per gap (ISC cron behavior, ADR-016).
// It reports false when tr does not move the clock forward or the gap skips
// no scheduled time.
func (s *SpecSchedule) gapRun(tr time.Time, loc *time.Location) (time.Time, bool) {
	gap := gapLength(tr, loc)
	if gap <= 0 {
		return time.Time{}, false
	}
	// Walk the skipped wall times as UTC times with the same clock reading.
	_, before := tr.Add(-time.Nanosecond).In(loc).Zone()
	start := tr.UTC().Add(time.Duration(before) * time.Second)
	end := start.Add(gap)
	for w := start; w.Before(end); w = w.Truncate(time.Minute).Add(time.Minute) {
		if !yearMatches(w.Year(), s.Year) || !fieldMatches(int(w.Month()), s.Month) ||
			!dayMatches(s, w) || !fieldMatches(w.Hour(), s.Hour) || !fieldMatches(w.Minute(), s.Minute) {
			continue
		}
		for sec := w; sec.Minute() == w.Minute() && sec.Before(end); sec = sec.Add(time.Second) {
			if fieldMatches(sec.Second(), s.Second) {
				return tr.Add(sec.Sub(start)), true
			}
		}
	}
	return time.Time{}, false
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
