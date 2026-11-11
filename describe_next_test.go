// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cron

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The times a description names (G1): a clock time, and the values after
// "minute(s)" and "second(s)" or before them ("30 minutes past").
var (
	namedClock   = regexp.MustCompile(`\b(midnight|noon|(\d+)(?::(\d\d))?(?::(\d\d))? ([ap])m)\b`)
	namedValues  = regexp.MustCompile(`\b(second|minute)s? (\d+(?:(?:, and |, … |, | and | thru )\d+)*)|\b(\d+) (second|minute)s?\b`)
	intervalNote = regexp.MustCompile(` \(every [^)]*\)`)
	digits       = regexp.MustCompile(`\d+`)
)

// scheduleZone returns the zone sch runs in, as a description names its
// times: the spec's zone, or the scheduler's.
func scheduleZone(sch Schedule) *time.Location {
	if s, ok := sch.(*SpecSchedule); ok && s.Location != nil {
		return s.Location
	}
	return time.UTC
}

// dayRuns walks Next() over the day of the first run from 2026 on, in the
// schedule's zone, and returns the hours, the minutes and the seconds that
// run, and every time of day as h*3600+m*60+s. A spec that never runs has
// none; Next() returns the zero time for it.
func dayRuns(sch Schedule) (hours, minutes, seconds, times map[int]bool) {
	hours, minutes, seconds, times = map[int]bool{}, map[int]bool{}, map[int]bool{}, map[int]bool{}
	loc := scheduleZone(sch)
	first := sch.Next(time.Date(2026, time.January, 1, 0, 0, 0, 0, loc).Add(-time.Second))
	start := time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, loc)
	for t := first; !t.IsZero() && t.Before(start.AddDate(0, 0, 1)); t = sch.Next(t) {
		hours[t.Hour()], minutes[t.Minute()], seconds[t.Second()] = true, true, true
		times[t.Hour()*3600+t.Minute()*60+t.Second()] = true
	}
	return hours, minutes, seconds, times
}

// checkNamedTimes reports every time desc names that sch does not run at: a
// clock time with seconds must be a run, one with minutes the minute of a
// run, and a bare one ("9 am", "noon") an hour with a run; a named minute or
// second must be the minute or second of a run. The check is one-sided: it
// does not find a run that the description leaves out.
func checkNamedTimes(desc string, sch Schedule) []string {
	hours, minutes, seconds, times := dayRuns(sch)
	desc = intervalNote.ReplaceAllString(desc, "") // "(every 7 minutes, …)" names an interval, not a value
	var bad []string
	for _, m := range namedClock.FindAllStringSubmatch(desc, -1) {
		if h, minute, sec := namedClockTime(m); !clockRuns(h, minute, sec, hours, times) {
			bad = append(bad, m[1])
		}
	}
	for _, m := range namedValues.FindAllStringSubmatch(desc, -1) {
		bad = append(bad, namedValueMisses(m, minutes, seconds)...)
	}
	return bad
}

// namedClockTime returns the hour of a clock time that namedClock matched,
// and its minute and second, -1 for each one not written.
func namedClockTime(m []string) (h, minute, sec int) {
	switch m[1] {
	case "midnight":
		return 0, -1, -1
	case "noon":
		return 12, -1, -1
	}
	h, _ = strconv.Atoi(m[2])
	h %= 12
	if m[5] == "p" {
		h += 12
	}
	minute, sec = -1, -1
	if m[3] != "" {
		minute, _ = strconv.Atoi(m[3])
	}
	if m[4] != "" {
		sec, _ = strconv.Atoi(m[4])
	}
	return h, minute, sec
}

// clockRuns reports whether a named clock time is a run: with seconds the
// time itself, with minutes any second of that minute, and a bare hour any
// time in that hour.
func clockRuns(h, minute, sec int, hours, times map[int]bool) bool {
	switch {
	case sec >= 0:
		return times[h*3600+minute*60+sec]
	case minute >= 0:
		for s := range 60 {
			if times[h*3600+minute*60+s] {
				return true
			}
		}
		return false
	}
	return hours[h]
}

// namedValueMisses returns "minute 5" or "second 5" for each value that
// namedValues matched and no run has.
func namedValueMisses(m []string, minutes, seconds map[int]bool) []string {
	unit, list := m[1], m[2]
	if unit == "" {
		unit, list = m[4], m[3]
	}
	runs := minutes
	if unit == "second" {
		runs = seconds
	}
	var bad []string
	for _, d := range digits.FindAllString(list, -1) {
		if v, _ := strconv.Atoi(d); !runs[v] {
			bad = append(bad, unit+" "+d)
		}
	}
	return bad
}

// The dates a description names (G1): days of the month, days of the week
// and months, each a value or a range, and the years after "in year(s)".
var (
	namedOrdinals = regexp.MustCompile(`\b(\d+)(?:st|nd|rd|th)(?: thru (\d+)(?:st|nd|rd|th))?\b`)
	namedDays     = regexp.MustCompile(`\b(Sunday|Monday|Tuesday|Wednesday|Thursday|Friday|Saturday)(?: thru (Sunday|Monday|Tuesday|Wednesday|Thursday|Friday|Saturday))?\b`)
	namedMonths   = regexp.MustCompile(`\b(January|February|March|April|May|June|July|August|September|October|November|December)(?: thru (January|February|March|April|May|June|July|August|September|October|November|December))?\b`)
	namedYears    = regexp.MustCompile(`\bin years? (\d+(?:(?:, and |, … |, | and | thru )\d+)*)`)
	shortMonths   = regexp.MustCompile(`; the (.*) runs? only in months that have`)
	yearItem      = regexp.MustCompile(`(\d+)(?: thru (\d+))?`)
)

// dateNames maps the names of the days of the week and of the months to
// their values, as time.Weekday and time.Month spell them.
var dateNames = func() map[string]int {
	m := map[string]int{}
	for d := time.Sunday; d <= time.Saturday; d++ {
		m[d.String()] = int(d)
	}
	for mo := time.January; mo <= time.December; mo++ {
		m[mo.String()] = int(mo)
	}
	return m
}()

// runDates holds the days of the month, the months, the days of the week
// and the years that have a run.
type runDates struct{ doms, months, dows, years map[int]bool }

// dateRuns walks Next() from 2026 thru 2030, in the schedule's zone, one
// call per day with a run.
func dateRuns(sch Schedule) runDates {
	r := runDates{map[int]bool{}, map[int]bool{}, map[int]bool{}, map[int]bool{}}
	loc := scheduleZone(sch)
	end := time.Date(2031, time.January, 1, 0, 0, 0, 0, loc)
	t := sch.Next(time.Date(2026, time.January, 1, 0, 0, 0, 0, loc).Add(-time.Second))
	for ; !t.IsZero() && t.Before(end); t = sch.Next(time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, loc)) {
		r.doms[t.Day()], r.months[int(t.Month())], r.dows[int(t.Weekday())], r.years[t.Year()] = true, true, true, true
	}
	return r
}

// checkNamedDates reports every date desc names that has no run: each day
// of the month, day of the week, month and year, and each value of a range
// of them. It checks each field alone, through the spec with the other day
// field, or both, as "*" ("the 13th when it falls on Friday" needs runs on
// a 13th and on a Friday, not on a Friday the 13th), and a day that D3 says
// runs only in months that have it needs none. Like checkNamedTimes, it is
// one-sided.
func checkNamedDates(desc string, p Parser, spec string, sch Schedule) []string {
	if !namesDate(desc) {
		return nil // the walks below cost three five-year walks
	}
	doms := dateRuns(anyDay(p, spec, sch, false, true)).doms
	dows := dateRuns(anyDay(p, spec, sch, true, false)).dows
	both := dateRuns(anyDay(p, spec, sch, true, true))
	for _, m := range shortMonths.FindAllStringSubmatch(desc, -1) {
		for _, d := range digits.FindAllString(m[1], -1) {
			doms[dateValue(d)] = true
		}
	}
	bad := namedMisses(desc, namedOrdinals, doms, 1, 31)
	bad = append(bad, namedMisses(desc, namedDays, dows, 0, 6)...)
	bad = append(bad, namedMisses(desc, namedMonths, both.months, 1, 12)...)
	for _, m := range namedYears.FindAllStringSubmatch(desc, -1) {
		bad = append(bad, namedMisses(m[1], yearItem, both.years, 0, 9999)...)
	}
	return bad
}

// namesDate reports whether desc names a day, a month or a year.
func namesDate(desc string) bool {
	for _, re := range []*regexp.Regexp{namedOrdinals, namedDays, namedMonths, namedYears} {
		if re.MatchString(desc) {
			return true
		}
	}
	return false
}

// anyDay returns the schedule of spec with its day of the month (dom), its
// day of the week (dow), or both as "*", or sch when it can't. It reads the
// fields as the parser fills them in, second thru day of the week, so
// optional fields don't move the day fields, and parses them all written.
func anyDay(p Parser, spec string, sch Schedule, dom, dow bool) Schedule {
	_, _, f, year, err := p.specFields(spec)
	if err != nil || f == nil { // a descriptor has no fields
		return sch
	}
	if dom {
		f[3] = "*"
	}
	if dow {
		f[5] = "*"
	}
	var zone string
	if w := strings.Fields(spec)[0]; strings.HasPrefix(w, "CRON_TZ=") || strings.HasPrefix(w, "TZ=") {
		zone = w + " "
	}
	q := p
	q.options = (q.options | Second | Minute | Hour | Dom | Month | Dow) &^ (SecondOptional | DowOptional)
	if s, err := q.Parse(strings.TrimSpace(zone + strings.Join(f, " ") + " " + year)); err == nil {
		return s
	}
	return sch
}

// namedMisses returns each item of re in s, a value in group 1 or a range
// over groups 1 and 2, that has a value with no run. A range whose end is
// below its start wraps past hi to lo, as "Friday thru Monday" does.
func namedMisses(s string, re *regexp.Regexp, runs map[int]bool, lo, hi int) []string {
	var bad []string
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		v, end := dateValue(m[1]), dateValue(m[1])
		if m[2] != "" {
			end = dateValue(m[2])
		}
		for ; runs[v] && v != end; v++ {
			if v == hi {
				v = lo - 1
			}
		}
		if !runs[v] {
			bad = append(bad, m[0])
		}
	}
	return bad
}

// dateValue returns the value of a day or month name, or of a number.
func dateValue(s string) int {
	if v, ok := dateNames[s]; ok {
		return v
	}
	v, _ := strconv.Atoi(s)
	return v
}

// randomField returns a field of one to three items over lo thru hi: a
// value, a range, plain or wrapped, or a step.
func randomField(r *rand.Rand, lo, hi int) string {
	if r.IntN(8) == 0 {
		return "*"
	}
	val := func() int { return lo + r.IntN(hi-lo+1) }
	items := make([]string, 1+r.IntN(3))
	for i := range items {
		switch r.IntN(5) {
		case 0, 1:
			items[i] = strconv.Itoa(val())
		case 2:
			items[i] = fmt.Sprintf("%d-%d", val(), val())
		case 3:
			items[i] = fmt.Sprintf("*/%d", 2+r.IntN((hi-lo+1)/2))
		default:
			items[i] = fmt.Sprintf("%d-%d/%d", val(), val(), 2+r.IntN(6))
		}
	}
	return strings.Join(items, ",")
}

// checkSpecNamesRuns reports an error for each time or date the description
// of spec names that Next() does not run at. It returns false for a spec
// that p does not take or that is described field by field, which names
// none.
func checkSpecNamesRuns(t *testing.T, p Parser, spec string) bool {
	t.Helper()
	sch, err := p.Parse(spec)
	if err != nil {
		return false
	}
	desc, err := p.DescribeSpec(spec)
	if err != nil {
		t.Errorf("DescribeSpec(%q): unexpected error %v", spec, err)
		return false
	}
	if literalForm.MatchString(desc) {
		return false
	}
	if bad := namesNotRun(desc, p, spec, sch); len(bad) > 0 {
		t.Errorf("DescribeSpec(%q) = %q: names %s, which Next() does not run at", spec, desc, strings.Join(bad, ", "))
	}
	return true
}

// namesNotRun returns each time and date desc names that Next() does not
// run at, and "never runs" when the spec runs. The times are read with both
// day fields as "*": the time fields run the same on every day, and a day
// the day fields allow may be years away.
func namesNotRun(desc string, p Parser, spec string, sch Schedule) []string {
	if desc == "never runs" {
		if sch.Next(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)).IsZero() {
			return nil
		}
		return []string{desc}
	}
	return append(checkNamedTimes(desc, anyDay(p, spec, sch, true, true)), checkNamedDates(desc, p, spec, sch)...)
}

// checkGoldensNameRuns runs checkSpecNamesRuns on each golden, with the
// parser its options give, and returns how many it checked.
func checkGoldensNameRuns(t *testing.T) int {
	t.Helper()
	checked := 0
	for _, tt := range describeGoldens {
		p, err := tryParserForOptions(newDescribeConfig(tt.opts).parse)
		if err != nil {
			t.Fatalf("parser for %q: %v", tt.spec, err)
		}
		if checkSpecNamesRuns(t, p, tt.spec) {
			checked++
		}
	}
	return checked
}

// TestDescribeSpecNamesRuns enforces G1: every clock time, minute, second,
// day of the month, day of the week, month and year that a description
// names is one the scheduler runs at, for the goldens with their parse
// options and for random specs.
func TestDescribeSpecNamesRuns(t *testing.T) {
	if checked := checkGoldensNameRuns(t); checked < 180 {
		t.Errorf("checked %d goldens, want at least 180", checked)
	}

	r := rand.New(rand.NewPCG(1, 2)) // fixed seed: the same specs on every run
	field := func(lo, hi int) string { return randomField(r, lo, hi) }
	days := func() string {
		return fmt.Sprintf("%d %d %s %s %s", r.IntN(60), r.IntN(24), field(1, 31), field(1, 12), field(0, 6))
	}
	year := func() string { // a year or a range: the year field takes no wrapped range
		y := 2026 + r.IntN(5)
		if r.IntN(2) == 0 {
			return strconv.Itoa(y)
		}
		return fmt.Sprintf("%d-%d", y, y+r.IntN(2031-y))
	}
	for _, c := range []struct {
		name     string
		p        Parser
		n, least int
		spec     func() string
	}{
		{"time fields", NewParser(Minute | Hour | Dom | Month | Dow), 600, 500, func() string { return field(0, 59) + " " + field(0, 23) + " * * *" }},
		{"seconds", NewParser(Second | Minute | Hour | Dom | Month | Dow), 100, 80, func() string {
			return strconv.Itoa(r.IntN(60)) + " " + field(0, 59) + " " + field(0, 23) + " * * *"
		}},
		{"date fields", NewParser(Minute | Hour | Dom | Month | Dow), 400, 240, days},
		{"DowOrDom", NewParser(Minute | Hour | Dom | Month | Dow | DowOrDom), 200, 100, days},
		{"years", NewParser(Minute | Hour | Dom | Month | Dow | Year), 100, 60, func() string { return days() + " " + year() }},
	} {
		checked := 0
		for range c.n {
			if checkSpecNamesRuns(t, c.p, c.spec()) {
				checked++
			}
		}
		if checked < c.least {
			t.Errorf("%s: checked %d random specs, want at least %d", c.name, checked, c.least)
		}
	}
}

// TestDescribeSpecNamesRunsCases checks specs the check once misread.
func TestDescribeSpecNamesRunsCases(t *testing.T) {
	five := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor)
	for _, tt := range []struct {
		p    Parser
		spec string
	}{
		{five, "0 0 29 2 1"},                         // February 29 on a Monday: no run for years
		{five, "CRON_TZ=America/New_York 0 9 * * *"}, // 9 am in New York, 13:00 or 14:00 in UTC
		{five, "CRON_TZ=Asia/Tokyo 0 3 5 * *"},       // the 5th in Tokyo, the 4th in UTC
		{five, "3-59/7 * 31 * *"},                    // the note "(every 7 minutes, …)" before the D3 clause
		{NewParser(Minute | Hour | Dom | Month | Dow | SecondOptional), "0 0 0 29 2 1"}, // an optional second written
	} {
		if !checkSpecNamesRuns(t, tt.p, tt.spec) {
			t.Errorf("%q was not checked", tt.spec)
		}
	}
}

// TestNamesNotRun checks that the check finds what a wrong description
// names: each case pairs a spec with a description it does not run as.
func TestNamesNotRun(t *testing.T) {
	five := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor)
	years := NewParser(Minute | Hour | Dom | Month | Dow | Year)
	for _, tt := range []struct {
		p          Parser
		spec, desc string
		want       string
	}{
		{five, "0 9 * * *", "at 10 am every day", "10 am"},
		{five, "30 9 * * *", "at 9:45 am every day", "9:45 am"},
		{five, "0,30 * * * *", "at minutes 0 and 15 past the hour", "minute 15"},
		{five, "0 0 1 * *", "at midnight on the 2nd of the month", "2nd"},
		{five, "0 0 * * 1-5", "at 12 am on Monday thru Saturday", "Monday thru Saturday"},
		{five, "0 0 1 3 *", "at midnight on the 1st of the month in April", "April"},
		{years, "0 0 1 1 * 2027", "at midnight on the 1st of the month in January in year 2028", "2028"},
		{five, "0 0 1 1 *", "never runs", "never runs"},
	} {
		sch, err := tt.p.Parse(tt.spec)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tt.spec, err)
		}
		if got := strings.Join(namesNotRun(tt.desc, tt.p, tt.spec, sch), ", "); got != tt.want {
			t.Errorf("namesNotRun(%q, %q) = %q, want %q", tt.desc, tt.spec, got, tt.want)
		}
	}
}
