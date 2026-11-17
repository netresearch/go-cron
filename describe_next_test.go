// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cron

import (
	"cmp"
	"fmt"
	"maps"
	"math/rand/v2"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// listSep joins the values of a list or a range: "0, 15, and 30", "5 and
// 6", "1 thru 5", "0, 7, 14, … 56".
const listSep = `(?:, and |, … |, | and | thru )`

// clockPattern is a clock time as a description writes it: "midnight", "noon",
// "9 am", "5:56 pm", "2:30:15 am".
const clockPattern = `(?:midnight|noon|\d+(?::\d\d){0,2} [ap]m)`

// The times a description names (G1): a clock time, the values after
// "minute(s)" and "second(s)" or before them ("30 minutes past"), and the
// hours of a clock range ("9 am thru 5 pm", "the 1 am hour thru the 4 am
// hour"), or under an hour step ("every 2 hours") the hours it picks from
// the range's first (T9).
var (
	namedClock   = regexp.MustCompile(`\b(midnight|noon|(\d+)(?::(\d\d))?(?::(\d\d))? ([ap])m)\b`)
	namedValues  = regexp.MustCompile(`\b(second|minute)s? (\d+(?:` + listSep + `\d+)*)|\b(\d+) (second|minute)s?\b`)
	clockRange   = regexp.MustCompile(`\b(` + clockPattern + `)(?: hour)? thru (?:the )?(` + clockPattern + `)`)
	hourStep     = regexp.MustCompile(`\bevery (\d+) hours\b`)
	intervalNote = regexp.MustCompile(` \(every [^)]*\)`)
	stepApart    = regexp.MustCompile(`\b\d+ (?:second|minute|hour|day|month)s? apart\b`)
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

// firstYear is the year the check starts in for a spec that allows any year.
const firstYear = 2026

// startYear returns the year the check starts in: the first year sch
// allows, or firstYear when it allows any. A spec for years before
// firstYear, or after a 30-year search from it, is read in its own years.
func startYear(sch Schedule) int {
	s, ok := sch.(*SpecSchedule)
	if !ok || s.Year == nil {
		return firstYear
	}
	start := YearMax
	for y := range s.Year {
		start = min(start, y)
	}
	return start
}

// yearStart returns the second before January 1 of year y in loc, so that
// Next() from it can return a run at the start of the year.
func yearStart(y int, loc *time.Location) time.Time {
	return time.Date(y, time.January, 1, 0, 0, 0, 0, loc).Add(-time.Second)
}

// dayRuns walks Next() over the day of the first run from startYear on, in
// the schedule's zone, and returns the hours, the minutes and the seconds
// that run, and every time of day as h*3600+m*60+s. A spec that never runs
// has none; Next() returns the zero time for it.
func dayRuns(sch Schedule) (hours, minutes, seconds, times map[int]bool) {
	hours, minutes, seconds, times = map[int]bool{}, map[int]bool{}, map[int]bool{}, map[int]bool{}
	loc := scheduleZone(sch)
	first := sch.Next(yearStart(startYear(sch), loc))
	if first.IsZero() {
		return hours, minutes, seconds, times
	}
	day := time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, loc)
	for _, t := range Between(sch, first.Add(-time.Second), day.AddDate(0, 0, 1)) {
		hours[t.Hour()], minutes[t.Minute()], seconds[t.Second()] = true, true, true
		times[t.Hour()*3600+t.Minute()*60+t.Second()] = true
	}
	return hours, minutes, seconds, times
}

// checkNamedTimes reports every time desc names that sch does not run at: a
// clock time with seconds must be a run, one with minutes the minute of a
// run, and a bare one ("9 am", "noon") an hour with a run; a named minute or
// second, and each value of a range of them, must be the minute or second of
// a run; each hour of a clock range must have a run, or under an hour step
// ("every 2 hours") each hour the step picks from its first. The check is
// one-sided: it does not find a run that the description leaves out.
func checkNamedTimes(desc string, sch Schedule) []string {
	hours, minutes, seconds, times := dayRuns(sch)
	desc = intervalNote.ReplaceAllString(desc, "") // "(every 7 minutes, …)" names an interval, not a value
	desc = stepApart.ReplaceAllString(desc, "")    // "15 seconds apart" names a step, not a value
	var bad []string
	for _, m := range namedClock.FindAllStringSubmatch(desc, -1) {
		if h, minute, sec := namedClockTime(m); !clockRuns(h, minute, sec, hours, times) {
			bad = append(bad, m[1])
		}
	}
	for _, m := range namedValues.FindAllStringSubmatch(desc, -1) {
		bad = append(bad, namedValueMisses(m, minutes, seconds)...)
	}
	step := 1
	if m := hourStep.FindStringSubmatch(desc); m != nil {
		step, _ = strconv.Atoi(m[1])
	}
	return append(bad, clockRangeMisses(desc, hours, step)...)
}

// clockRangeMisses returns each clock range in desc with an hour before its
// last that has no run, of every step-th hour from its first; the last hour
// is checked as a clock time.
func clockRangeMisses(desc string, hours map[int]bool, step int) []string {
	var bad []string
	for _, m := range clockRange.FindAllStringSubmatch(desc, -1) {
		from, _, _ := namedClockTime(namedClock.FindStringSubmatch(m[1]))
		to, _, _ := namedClockTime(namedClock.FindStringSubmatch(m[2]))
		for k := 0; k < (to-from+24)%24; k += step {
			if !hours[(from+k)%24] {
				bad = append(bad, m[0])
				break
			}
		}
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
// namedValues matched and no run has, and "minute 0 thru 10" for a range
// with a value that none has.
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
	for _, item := range namedMisses(list, valueItem, runs, 0, 59) {
		bad = append(bad, unit+" "+item)
	}
	return bad
}

// The dates a description names (G1): days of the month, days of the week
// and months, each a value or a range, and the years after "in year(s)".
var (
	namedOrdinals = regexp.MustCompile(`\b(\d+)(?:st|nd|rd|th)(?: thru (\d+)(?:st|nd|rd|th))?\b`)
	namedDays     = nameRange(time.Sunday, time.Saturday)
	namedMonths   = nameRange(time.January, time.December)
	namedYears    = regexp.MustCompile(`\bin years? (\d+(?:` + listSep + `\d+)*)`)
	shortMonths   = regexp.MustCompile(`; the (.*) runs? only in months that have`)
	valueItem     = regexp.MustCompile(`(\d+)(?: thru (\d+))?`)
)

// nameRange matches a name from first thru last, or a range of two of them,
// "Monday thru Friday", as time.Weekday or time.Month spells them.
func nameRange[T interface {
	time.Weekday | time.Month
	String() string
}](first, last T) *regexp.Regexp {
	var names []string
	for v := first; v <= last; v++ {
		names = append(names, v.String())
	}
	alt := strings.Join(names, "|")
	return regexp.MustCompile(`\b(` + alt + `)(?: thru (` + alt + `))?\b`)
}

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

// runDates holds the days of the month, the months and the days of the
// week that have a run.
type runDates struct{ doms, months, dows map[int]bool }

// dateRuns walks Next() over the five years from startYear, in the
// schedule's zone, one call per day with a run. A schedule with no run in
// those years, as one for the fifth Monday in February, is walked over the
// year of its first run instead.
func dateRuns(sch Schedule) runDates {
	r := runDates{map[int]bool{}, map[int]bool{}, map[int]bool{}}
	loc := scheduleZone(sch)
	start := startYear(sch)
	end := time.Date(start+5, time.January, 1, 0, 0, 0, 0, loc)
	t := sch.Next(yearStart(start, loc))
	if !t.IsZero() && !t.Before(end) {
		end = time.Date(t.Year()+1, time.January, 1, 0, 0, 0, 0, loc)
	}
	for ; !t.IsZero() && t.Before(end); t = sch.Next(time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, loc)) {
		r.doms[t.Day()], r.months[int(t.Month())], r.dows[int(t.Weekday())] = true, true, true
	}
	return r
}

// checkNamedDates reports every date desc names that has no run: each day
// of the month, day of the week, month and year, and each value of a range
// of them. It checks each field alone, through the spec with the other day
// field, or both, as "*" ("the 13th when it falls on Friday" needs runs on
// a 13th and on a Friday, not on a Friday the 13th), and a day that D3 says
// runs only in months that have it needs none, nor does a day in pool. Like
// checkNamedTimes, it is one-sided.
func checkNamedDates(desc string, sch Schedule, pool uint64) []string {
	if !namesDate(desc) {
		return nil // the walks below cost three five-year walks
	}
	doms := dateRuns(anyDay(sch, false, true)).doms
	dows := dateRuns(anyDay(sch, true, false)).dows
	bothSch := anyDay(sch, true, true)
	both := dateRuns(bothSch)
	for _, m := range shortMonths.FindAllStringSubmatch(desc, -1) {
		for _, d := range digits.FindAllString(m[1], -1) {
			doms[dateValue(d)] = true
		}
	}
	for d := 1; d <= 31; d++ {
		if pool&(1<<d) != 0 {
			doms[d] = true
		}
	}
	bad := namedMisses(desc, namedOrdinals, doms, 1, 31)
	bad = append(bad, namedMisses(desc, namedDays, dows, 0, 6)...)
	bad = append(bad, namedMisses(desc, namedMonths, both.months, 1, 12)...)
	for _, m := range namedYears.FindAllStringSubmatch(desc, -1) {
		bad = append(bad, yearMisses(bothSch, m[1])...)
	}
	return bad
}

// yearMisses returns each year and each range of years in list that the
// year field of sch does not allow in full. With both day fields as "*", sch
// runs in every year it allows, so the field answers for Next() however far
// off the year is, and a range costs one pass over the field.
func yearMisses(sch Schedule, list string) []string {
	s, ok := sch.(*SpecSchedule)
	if !ok || s.Year == nil {
		return nil // any year has a run
	}
	var bad []string
	for _, m := range valueItem.FindAllStringSubmatch(list, -1) {
		from, to := dateValue(m[1]), dateValue(m[1])
		if m[2] != "" {
			to = dateValue(m[2])
		}
		if from > to { // a range written backwards
			bad = append(bad, m[0])
			continue
		}
		allowed := 0
		for y := range s.Year {
			if y >= from && y <= to {
				allowed++
			}
		}
		if allowed != to-from+1 {
			bad = append(bad, m[0])
		}
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

// starDays holds the day fields as the parser sets them for "*".
var starDays = func() *SpecSchedule {
	s, err := NewParser(Minute | Hour | Dom | Month | Dow).Parse("* * * * *")
	if err != nil {
		panic(err)
	}
	return s.(*SpecSchedule)
}()

// anyDay returns a copy of sch with its day of the month (dom), its day of
// the week (dow), or both as "*", and with Next() searching 30 years ahead,
// a safeguard for a first run that dateRuns walks to past the parser's
// default span. It sets the fields on the copy, so no parser option can
// reject the result. A descriptor such as "@weekly" is a
// SpecSchedule too and gets the same; one that is not, as "@every 1h",
// comes back as it is.
func anyDay(sch Schedule, dom, dow bool) Schedule {
	s, ok := sch.(*SpecSchedule)
	if !ok {
		return sch
	}
	c := *s
	c.MaxSearchYears = 30
	if dom {
		c.Dom, c.DomConstraints = starDays.Dom, nil
	}
	if dow {
		c.Dow, c.DowConstraints = starDays.Dow, nil
	}
	return &c
}

// namedMisses returns each item of re in s, a value in group 1 or a range
// over groups 1 and 2, that has a value with no run. A range whose end is
// below its start wraps past hi to lo, as "Friday thru Monday" does. A
// value outside lo thru hi has no run.
func namedMisses(s string, re *regexp.Regexp, runs map[int]bool, lo, hi int) []string {
	var bad []string
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		v, end := dateValue(m[1]), dateValue(m[1])
		if m[2] != "" {
			end = dateValue(m[2])
		}
		if min(v, end) < lo || max(v, end) > hi { // no field has the value
			bad = append(bad, m[0])
			continue
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
// of spec, with opts, names that Next() does not run at. It returns false
// for a spec that p does not take or that is described field by field,
// which names none.
func checkSpecNamesRuns(t *testing.T, p Parser, spec string, opts ...DescribeOption) bool {
	t.Helper()
	p = withCheckKey(p)
	if _, err := p.Parse(spec); err != nil {
		return false
	}
	desc, err := p.DescribeSpec(spec, opts...)
	if err != nil {
		t.Errorf("DescribeSpec(%q): unexpected error %v", spec, err)
		return false
	}
	if literalForm.MatchString(desc) {
		return false
	}
	note, _ := p.DescribeSpecNote(spec, opts...)
	if bad, _ := namesNotRunSpec(p, spec, desc, note, newDescribeConfig(opts).sched); len(bad) > 0 { // spec parsed above
		t.Errorf("DescribeSpec(%q) = %q: names %s, which Next() does not run at", spec, desc, strings.Join(bad, ", "))
	}
	return true
}

// withCheckKey gives a parser with no hash key one, so a spec with H parses.
// The description does not depend on the key, and the check reads every
// value a key can pick (hashReachSchedule).
func withCheckKey(p Parser) Parser {
	if p.hashKey == "" {
		return p.WithHashKey("g1")
	}
	return p
}

// namesNotRunSpec returns namesNotRun for desc as a description of spec, as
// p parses it, or the parse error. A spec with H is read for every key: a
// value it names must be one some key runs at (hashReachSchedule), a day of
// the month an H item can pick needs no run in the months that lack it, and
// "never runs" must hold for every key, the picks moving together (G11). The
// zone clause is checked by zoneMisses, with note and the scheduler's zone
// sched, and taken out before the rest is read.
func namesNotRunSpec(p Parser, spec, desc, note string, sched *time.Location) ([]string, error) {
	p = withCheckKey(p)
	sch, err := p.Parse(spec)
	if err != nil {
		return nil, err
	}
	loc, descriptor, fields, _, _ := p.specFields(spec) // the year field is in sch
	if _, text := p.expandDescriptor(descriptor, fields); text != "" {
		loc = nil // "only when triggered" and the interval texts name no zone
	}
	desc, bad := zoneMisses(loc, sched, desc, note)
	s, ok := sch.(*SpecSchedule)
	if !ok || !containsHashExpr(fields) {
		return append(bad, namesNotRun(desc, sch, 0)...), nil
	}
	if desc == "never runs" {
		if p.anyPick(s, fields, func(pick *SpecSchedule) bool { return runsSomeYear(anyDay(pick, false, false)) }) {
			return append(bad, desc), nil
		}
		return bad, nil
	}
	return append(bad, namesNotRun(desc, hashReachSchedule(p, s, fields), hashReach(fields[3], p.fieldBounds(dom)))...), nil
}

// shiftPattern matches the shift a zone clause gives from object, "UTC" or
// "<name> time" (Z2, Z3): "5 hours behind UTC", "the same as
// UTC", and for a shift that changes with the season a second value and its
// months, "…, 4 hours from March to November", "…, 1 hour ahead from March to
// October".
func shiftPattern(object string) *regexp.Regexp {
	o := regexp.QuoteMeta(object)
	return regexp.MustCompile(`^(?:the same as ` + o + `|(.+?) (ahead of|behind) ` + o + `)(?:, (?:the same as ` + o + `|(.+?)(?: (ahead|behind))?) from (\w+) to (\w+))?$`)
}

var (
	sizePart    = regexp.MustCompile(`(\d+) (hour|minute)s?`)
	windowRange = regexp.MustCompile(`from (\w+) (\d+) to (?:(\w+) )?(\d+)|on (\w+) (\d+)`)
)

// zoneMisses checks the zone clause of desc for the written zone loc
// (Z1–Z3) and returns desc without the clause, and
// each shift that is not what Next() gives in loc, or "no <name> time" when
// desc does not name loc. A shift is read with the windows note names off it
// (Z4). sched is the scheduler's zone, nil when none is given. It
// reads nothing for no zone, TZ=Local and "never runs", which name no zone.
func zoneMisses(loc, sched *time.Location, desc, note string) (string, []string) {
	if loc == nil || loc == time.Local || desc == "never runs" {
		return desc, nil
	}
	name, _ := zoneName(loc)
	head := " " + name + " time, "
	if name == "UTC" {
		head = " UTC"
	}
	i := strings.Index(desc, head)
	if i < 0 {
		return desc, []string{"no " + strings.TrimSpace(strings.TrimSuffix(head, ", "))}
	}
	end := len(desc)
	if j := strings.Index(desc[i:], ";"); j >= 0 {
		end = i + j
	}
	utc, fromSched, _ := strings.Cut(desc[i+len(head):end], ", and ")
	if name == "UTC" { // UTC has no shift from UTC to give
		utc, fromSched = "", strings.TrimPrefix(desc[i+len(head):end], ", ")
	}
	desc = desc[:i] + desc[end:]
	var bad []string
	if name != "UTC" && !shiftRuns(utc, loc, time.UTC, "UTC", zoneWindows(note, name, "UTC")) {
		bad = append(bad, cmp.Or(utc, "no shift from UTC"))
	}
	if fromSched != "" && !schedShiftRuns(fromSched, loc, sched, name, note) {
		bad = append(bad, fromSched)
	}
	return desc, bad
}

// schedShiftRuns reports whether shift, the clause's shift from the
// scheduler's zone, is what Next() gives in loc against sched. A shift with
// no scheduler zone given is wrong.
func schedShiftRuns(shift string, loc, sched *time.Location, name, note string) bool {
	if sched == nil {
		return false
	}
	sname, generic := zoneName(sched)
	object := sname
	if generic {
		object = sname + " time"
	}
	return shiftRuns(shift, loc, sched, sname+" time", zoneWindows(note, name, object))
}

// shiftRuns reports whether shift, read with shiftPattern(object), is loc's
// offset from ref over the year the clause is made for, as hourly runs in
// loc walked with Next() find it. A shift with months must be that of a
// zone pair whose offset leaves its first value once and comes back: the
// second value and the months of the two changes. Any other shift is the
// stated value, and the windows the note names must be the stretches of a
// day or more in which the offset is another, by first and last date.
func shiftRuns(shift string, loc, ref *time.Location, object string, windows []zoneWindow) bool {
	m := shiftPattern(object).FindStringSubmatch(shift)
	if m == nil {
		return false
	}
	spans := cachedHourSpans(loc, ref, describeNow().In(loc).Year())
	if m[5] == "" {
		return windowsMatch(spans, sizeOf(m[1], m[2]), windows)
	}
	side := m[4]
	if side == "" {
		side = m[2] // the second value is on the first one's side
	}
	return len(spans) == 3 && spans[2].off == spans[0].off &&
		sizeOf(m[1], m[2]) == spans[0].off && sizeOf(m[3], side) == spans[1].off &&
		m[5] == spans[1].first.Month().String() && m[6] == spans[2].first.Month().String()
}

// zoneWindow is a window a note names, by its first and last dates, as
// month*100+day, and the offset in it.
type zoneWindow struct {
	from, to int
	off      time.Duration
}

// zoneWindows reads the windows note gives subject's zone off its shift from
// object (Z4): "Berlin is 5 hours ahead of New York from March 14 to
// 28 and from October 31 to November 7", "… on December 31".
func zoneWindows(note, subject, object string) []zoneWindow {
	sentence := regexp.MustCompile(`^` + regexp.QuoteMeta(subject) + ` is (?:the same as ` + regexp.QuoteMeta(object) +
		`|(.+?) (ahead of|behind) ` + regexp.QuoteMeta(object) + `) ((?:from|on) .+)$`)
	var windows []zoneWindow
	for _, s := range strings.Split(strings.TrimSuffix(note, ", between the two zones' clock changes"), "; ") {
		m := sentence.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		for _, r := range windowRange.FindAllStringSubmatch(m[3], -1) {
			w := zoneWindow{off: sizeOf(m[1], m[2])}
			switch {
			case r[5] != "": // on a date
				w.from = monthDay(r[5], r[6])
				w.to = w.from
			case r[3] != "":
				w.from, w.to = monthDay(r[1], r[2]), monthDay(r[3], r[4])
			default:
				w.from, w.to = monthDay(r[1], r[2]), monthDay(r[1], r[4])
			}
			windows = append(windows, w)
		}
	}
	return windows
}

// monthDay returns a date a note names as month*100+day, 0 for no month.
func monthDay(month, day string) int {
	d, _ := strconv.Atoi(day)
	for m := time.January; m <= time.December; m++ {
		if m.String() == month {
			return int(m)*100 + d
		}
	}
	return 0
}

// windowsMatch reports whether windows are the spans of a day or more whose
// offset is not stated, by the dates of their first and last runs.
func windowsMatch(spans []runSpan, stated time.Duration, windows []zoneWindow) bool {
	date := func(t time.Time) int { return int(t.Month())*100 + t.Day() }
	var runs []zoneWindow
	for _, s := range spans {
		if s.off != stated && s.next.Sub(s.first) >= 24*time.Hour {
			runs = append(runs, zoneWindow{date(s.first), date(s.last), s.off})
		}
	}
	byDate := func(a, b zoneWindow) int { return a.from - b.from }
	slices.SortFunc(runs, byDate)
	named := slices.SortedFunc(slices.Values(windows), byDate)
	return slices.Equal(runs, named)
}

// sizeOf reads an offset as a description gives it, "5 hours 30 minutes",
// on side "behind" or ahead; "" is no offset.
func sizeOf(s, side string) time.Duration {
	var d time.Duration
	for _, m := range sizePart.FindAllStringSubmatch(s, -1) {
		n, _ := strconv.Atoi(m[1])
		unit := time.Hour
		if m[2] == "minute" {
			unit = time.Minute
		}
		d += time.Duration(n) * unit
	}
	if side == "behind" {
		return -d
	}
	return d
}

// hourly is "0 * * * *" as the parser sets it.
var hourly = func() *SpecSchedule {
	s, err := NewParser(Minute | Hour | Dom | Month | Dow).Parse("0 * * * *")
	if err != nil {
		panic(err)
	}
	return s.(*SpecSchedule)
}()

// runSpan is a stretch of hourly runs in which a zone keeps one offset from
// another: its first run, at the change by the minute, its last, and the
// first run after it, or the end of the year.
type runSpan struct {
	first, last, next time.Time
	off               time.Duration
}

// everyMinute is "* * * * *" as the parser sets it.
var everyMinute = func() *SpecSchedule {
	s, err := NewParser(Minute | Hour | Dom | Month | Dow).Parse("* * * * *")
	if err != nil {
		panic(err)
	}
	return s.(*SpecSchedule)
}()

// spansCache keeps hourSpans by zone names and year, as the goldens and the
// random specs describe a few zones many times over.
var spansCache sync.Map

type spansKey struct {
	loc, ref string
	year     int
}

// cachedHourSpans returns hourSpans, kept for zones that tzdata names: a
// zone named "Local", or a fixed zone, can have other offsets under the
// same name.
func cachedHourSpans(loc, ref *time.Location, year int) []runSpan {
	named := func(l *time.Location) bool { return l == time.UTC || strings.Contains(l.String(), "/") }
	if !named(loc) || !named(ref) {
		return hourSpans(loc, ref, year)
	}
	k := spansKey{loc.String(), ref.String(), year}
	if s, ok := spansCache.Load(k); ok {
		return s.([]runSpan)
	}
	s := hourSpans(loc, ref, year)
	spansCache.Store(k, s)
	return s
}

// hourSpans walks Next() over an hourly run in loc through year and groups
// the runs by loc's offset from ref at them. A span starts at the change
// itself, found by the minute between two hourly runs (changeAt): ref's
// change can fall at 11:30 pm in loc, as Almaty's of 2024 does in Kolkata.
func hourSpans(loc, ref *time.Location, year int) []runSpan {
	h := *hourly
	h.Location = loc
	var spans []runSpan
	for t := h.Next(yearStart(year, loc)); t.Year() == year; t = h.Next(t) {
		off := offsetOf(t) - offsetOf(t.In(ref))
		n := len(spans)
		if n > 0 && spans[n-1].off == off {
			spans[n-1].last = t
			continue
		}
		if n > 0 {
			t = changeAt(loc, ref, spans[n-1].last, t, off)
			spans[n-1].next = t
		}
		spans = append(spans, runSpan{first: t, last: t, off: off})
	}
	if n := len(spans); n > 0 {
		spans[n-1].next = time.Date(year+1, time.January, 1, 0, 0, 0, 0, loc)
	}
	return spans
}

// changeAt returns the first run of every minute in loc after from, and
// before to, at which loc is off from ref, or to.
func changeAt(loc, ref *time.Location, from, to time.Time, off time.Duration) time.Time {
	m := *everyMinute
	m.Location = loc
	for t := m.Next(from); t.Before(to); t = m.Next(t) {
		if offsetOf(t)-offsetOf(t.In(ref)) == off {
			return t
		}
	}
	return to
}

// hashReachSchedule returns s with each field that has H items running at
// every value those items can pick under any key, and its other items as
// written.
func hashReachSchedule(p Parser, s *SpecSchedule, fields []string) *SpecSchedule {
	reach := *s
	for _, f := range p.hashDateFields(&reach, s, fields) {
		if len(f.items) > 0 {
			*f.bits = f.plain | hashReach(f.field, f.r)
		}
	}
	for i, f := range []struct {
		bits *uint64
		r    bounds
	}{{&reach.Second, p.fieldBounds(seconds)}, {&reach.Minute, p.fieldBounds(minutes)}, {&reach.Hour, p.fieldBounds(hours)}} {
		plain, items := splitItems(fields[i], hashItemSyntax)
		if len(items) > 0 {
			bits, _ := getField(strings.Join(plain, ","), f.r) // no plain items: no values
			*f.bits = bits | hashReach(fields[i], f.r)
		}
	}
	reach.Dow = NormalizeDOW(reach.Dow) // a reach ending in 7 is Sunday
	return &reach
}

// namesNotRun returns each time and date desc names that Next() does not
// run at, and "never runs" when the spec runs in a year it allows. The
// times are read with both day fields as "*": the time fields run the same
// on every day, and a day the day fields allow may be years away. pool is
// the days of the month that need no run (namesNotRunSpec).
func namesNotRun(desc string, sch Schedule, pool uint64) []string {
	if desc == "never runs" {
		if runsSomeYear(anyDay(sch, false, false)) {
			return []string{desc}
		}
		return nil
	}
	return append(checkNamedTimes(desc, anyDay(sch, true, true)), checkNamedDates(desc, sch, pool)...)
}

// yearKind returns which of the 14 calendars year y follows: the day of the
// week of January 1, plus 7 in a leap year. Years of one kind put the same
// dates on the same days of the week.
func yearKind(y int) int {
	k := int(time.Date(y, time.January, 1, 0, 0, 0, 0, time.UTC).Weekday())
	if daysIn(y, time.February) == 29 {
		k += 7
	}
	return k
}

// runsSomeYear reports whether sch runs in a year it allows, or, when it
// allows any, in a year from firstYear on. Whether a year has a run depends
// only on its yearKind, so it asks Next() from the first year of each kind:
// at most 14 calls, each from the start of its year, however far apart the
// years are. Any run Next() returns counts, in that year or a later one.
// NOTE: with DSTSkip, a run in an hour that daylight saving skips does not
// happen, so a year can lose runs that another year of its kind keeps, where
// the zone's transition dates differ (its rules changed, or tzdata lists
// each year's dates). Each search ends 30 years after the year it starts in
// (anyDay), so a wrong "never runs" can pass when every run is later.
func runsSomeYear(sch Schedule) bool {
	loc := scheduleZone(sch)
	for _, y := range firstOfEachKindList(sch) {
		if !sch.Next(yearStart(y, loc)).IsZero() {
			return true
		}
	}
	return false
}

// firstOfEachKindMap returns the first year of each yearKind that sch
// allows, by kind, from firstYear on when it allows any.
func firstOfEachKindMap(sch Schedule) map[int]int {
	first := map[int]int{}
	if s, ok := sch.(*SpecSchedule); ok && s.Year != nil {
		for y := range s.Year {
			if k := yearKind(y); first[k] == 0 || y < first[k] {
				first[k] = y
			}
		}
		return first
	}
	for y := firstYear; len(first) < 14; y++ {
		if k := yearKind(y); first[k] == 0 {
			first[k] = y
		}
	}
	return first
}

// firstOfEachKindList returns the years of firstOfEachKindMap, earliest
// first.
func firstOfEachKindList(sch Schedule) []int {
	return slices.Sorted(maps.Values(firstOfEachKindMap(sch)))
}

// checkGoldensNameRuns runs checkSpecNamesRuns on each golden, with the
// parser its options give, and returns how many it checked.
func checkGoldensNameRuns(t *testing.T) int {
	t.Helper()
	checked := 0
	old := describeNow
	defer func() { describeNow = old }()
	for _, tt := range describeGoldens {
		setDescribeYear(tt.year)
		p, err := tryParserForOptions(newDescribeConfig(tt.opts).parse)
		if err != nil {
			t.Fatalf("parser for %q: %v", tt.spec, err)
		}
		if checkSpecNamesRuns(t, p, tt.spec, tt.opts...) {
			checked++
		}
	}
	return checked
}

// TestDescribeSpecNamesRuns enforces G1: every clock time, minute, second,
// day of the month, day of the week, month and year that a description
// names is one the scheduler runs at, and the shift a zone clause gives is the
// zone's (zoneMisses), for the goldens with their parse options and for random
// specs. A golden with H is read for every key
// (namesNotRunSpec); only the goldens given field by field (G10) and the
// specs the parser rejects are not checked.
func TestDescribeSpecNamesRuns(t *testing.T) {
	if checked := checkGoldensNameRuns(t); checked < 524 {
		t.Errorf("checked %d goldens, want at least 524", checked)
	}

	r := rand.New(rand.NewPCG(1, 2)) // fixed seed: the same specs on every run
	field := func(lo, hi int) string { return randomField(r, lo, hi) }
	days := func() string {
		return fmt.Sprintf("%d %d %s %s %s", r.IntN(60), r.IntN(24), field(1, 31), field(1, 12), field(0, 6))
	}
	year := func() string { return randomYear(r) }
	checkRandomSpecs(t, []randomSpecs{
		{"time fields", NewParser(Minute | Hour | Dom | Month | Dow), 600, 500, func() string { return field(0, 59) + " " + field(0, 23) + " * * *" }},
		{"seconds", NewParser(Second | Minute | Hour | Dom | Month | Dow), 100, 80, func() string {
			return strconv.Itoa(r.IntN(60)) + " " + field(0, 59) + " " + field(0, 23) + " * * *"
		}},
		{"date fields", NewParser(Minute | Hour | Dom | Month | Dow), 400, 240, days},
		{"DowOrDom", NewParser(Minute | Hour | Dom | Month | Dow | DowOrDom), 200, 100, days},
		{"years", NewParser(Minute | Hour | Dom | Month | Dow | Year), 100, 60, func() string { return days() + " " + year() }},
		{"optional year", NewParser(Minute | Hour | Dom | Month | Dow | YearOptional), 200, 120, func() string {
			if r.IntN(2) == 0 {
				return days() // no year written
			}
			return days() + " " + year()
		}},
	})
}

// randomSpecs is n random specs from spec for p, of which at least least
// must name values the check reads.
type randomSpecs struct {
	name     string
	p        Parser
	n, least int
	spec     func() string
}

// checkRandomSpecs runs checkSpecNamesRuns on the specs of each case.
func checkRandomSpecs(t *testing.T, cases []randomSpecs) {
	t.Helper()
	for _, c := range cases {
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

// randomYear returns a year field: a year, a range, a step or a list. The
// year field takes no wrapped range.
func randomYear(r *rand.Rand) string {
	y := firstYear + r.IntN(5)
	switch r.IntN(8) {
	case 0:
		y = 1900 + r.IntN(firstYear-1900) // before firstYear
	case 1:
		y = firstYear + 31 + r.IntN(1000) // past a 30-year search from firstYear
	}
	switch r.IntN(4) {
	case 0:
		return strconv.Itoa(y)
	case 1:
		return fmt.Sprintf("%d-%d", y, y+r.IntN(5))
	case 2:
		return fmt.Sprintf("%d-%d/%d", y, y+2+r.IntN(20), 2+r.IntN(5))
	}
	return fmt.Sprintf("%d,%d", y, y+1+r.IntN(40)) // two years, up to 40 apart
}

// TestDescribeSpecNamesRunsCases checks specs the check once misread.
func TestDescribeSpecNamesRunsCases(t *testing.T) {
	five := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor)
	years := NewParser(Minute | Hour | Dom | Month | Dow | Year)
	for _, tt := range []struct {
		p    Parser
		spec string
	}{
		{years, "0 0 1 1 * 2035"},                    // a single year past the 5-year search: the check starts in it, not in 2026
		{years, "0 0 1 1 * 2025"},                    // a year before 2026
		{years, "0 0 1 1 * 2060"},                    // a year past a 30-year search from 2026
		{years, "0 0 1 1 * 2035,2037"},               // two years apart
		{years, "0 0 1 1 * 2027,2035"},               // a year after the five years walked
		{years, "0 0 1 1 * 2026-102025"},             // 100,000 years, the most a year field takes
		{years, "0 0 30 2 * 2100000000"},             // never runs, in a year near YearMax
		{five, "0 0 29 2 1"},                         // February 29 on a Monday: no run for years
		{five, "CRON_TZ=America/New_York 0 9 * * *"}, // 9 am in New York, 13:00 or 14:00 in UTC
		{five, "CRON_TZ=Asia/Tokyo 0 3 5 * *"},       // the 5th in Tokyo, the 4th in UTC
		{five, "3-59/7 * 31 * *"},                    // the note "(every 7 minutes, …)" before the D3 clause
		{NewParser(Minute | Hour | Dom | Month | Dow | SecondOptional), "0 0 0 29 2 1"},             // an optional second written
		{NewParser(Minute | Hour | Dom | Month | Dow | YearOptional), "* 5 2,12,24-5 4 0"},          // an optional year not written
		{NewParser(Minute | Hour | Dom | Month | Dow | Year | YearOptional), "* 5 2,12,24-5 4 0 *"}, // a year "*" written
		{NewParser(Minute | Hour | Dom | Month | Dow | Year | SecondOptional), "0 0 0 29 2 1"},      // six fields with Year: a second, no year
		{NewParser(Minute | Hour | Dom | Month | Dow | StrictDays | DowOrDom), "0 0 31 2 1"},        // valid while the day of the week stands in
	} {
		if !checkSpecNamesRuns(t, tt.p, tt.spec) {
			t.Errorf("%q was not checked", tt.spec)
		}
	}
}

// TestAnyDayMatchesParse checks that anyDay's copy is the schedule the
// parser builds, with a 30-year search, from the day fields written as "*",
// for options that keep state beside the day fields: constraints, hashes,
// zones, years, flags and descriptors.
func TestAnyDayMatchesParse(t *testing.T) {
	five := Minute | Hour | Dom | Month | Dow
	hash := NewParser(five | Hash).WithHashKey("job")
	desc := NewParser(five | Descriptor)
	for _, tt := range []struct {
		p          Parser
		spec, star string // star is spec with the starred day fields written as "*"
		dom, dow   bool
	}{
		{NewParser(five | DowOrDom), "0 9 13 * 5", "0 9 * * 5", true, false},
		{NewParser(five | DowOrDom), "0 9 13 * 5", "0 9 13 * *", false, true},
		{NewParser(five | Extended), "0 0 L * *", "0 0 * * *", true, false},
		{NewParser(five | Extended), "0 0 15W * 5#3", "0 0 * * *", true, true},
		{hash, "H H H * *", "H H * * *", true, false},
		{hash, "H H * * H", "H H * * *", false, true},
		{NewParser(five | DSTSkip), "CRON_TZ=America/New_York 30 2 * 3 0", "CRON_TZ=America/New_York 30 2 * 3 *", false, true},
		{NewParser(five | Year), "0 0 29 2 1 2028,2032", "0 0 * 2 * 2028,2032", true, true},
		{desc, "@weekly", "@daily", false, true},
		{desc, "@monthly", "@daily", true, false},
		{desc, "CRON_TZ=Asia/Tokyo @weekly", "CRON_TZ=Asia/Tokyo @daily", false, true},
	} {
		sch, err := tt.p.Parse(tt.spec)
		if err != nil {
			t.Errorf("Parse(%q): %v", tt.spec, err)
			continue
		}
		want, err := tt.p.WithMaxSearchYears(30).Parse(tt.star)
		if err != nil {
			t.Errorf("Parse(%q): %v", tt.star, err)
			continue
		}
		a := anyDay(sch, tt.dom, tt.dow)
		g, gok := a.(*SpecSchedule)
		wp, wok := want.(*SpecSchedule)
		if !gok || !wok {
			t.Errorf("anyDay(%q) = %T, Parse(%q) = %T: want two *SpecSchedule", tt.spec, a, tt.star, want)
			continue
		}
		got, w := *g, *wp
		// Each Parse loads its zone anew, with its own cache: compare names.
		if got.Location.String() != w.Location.String() {
			t.Errorf("anyDay(%q).Location = %v, want %v", tt.spec, got.Location, w.Location)
		}
		got.Location, w.Location = nil, nil
		if !reflect.DeepEqual(got, w) {
			t.Errorf("anyDay(%q, %v, %v) = %+v, want %+v, as %q parses", tt.spec, tt.dom, tt.dow, got, w, tt.star)
		}
	}
}

// TestZoneMissesSchedulerAndWindows checks that the zone check finds a wrong
// shift from the scheduler's zone and wrong or missing windows in the note.
func TestZoneMissesSchedulerAndWindows(t *testing.T) {
	pinDescribeYear(t)
	berlin, py := loadLocation(t, "Europe/Berlin"), loadLocation(t, "Asia/Pyongyang")
	utc, ny := loadLocation(t, "UTC"), loadLocation(t, "America/New_York")
	la, sydney := loadLocation(t, "America/Los_Angeles"), loadLocation(t, "Australia/Sydney")
	tonga, fakaofo := loadLocation(t, "Pacific/Tongatapu"), loadLocation(t, "Pacific/Fakaofo")
	kolkata, almaty := loadLocation(t, "Asia/Kolkata"), loadLocation(t, "Asia/Almaty")
	const kolkataAlmaty = " Kolkata time, 5 hours 30 minutes ahead of UTC, and 30 minutes behind Almaty time"
	const berlinUTC = " Berlin time, 1 hour ahead of UTC, 2 hours from March to October"
	const berlinNY = "Berlin is 5 hours ahead of New York from March 14 to 28 and from October 31 to November 7, between the two zones' clock changes"
	const sydneyNY = "Sydney is 16 hours ahead of New York from January 1 to March 14 and from November 7 to December 31; Sydney is 15 hours ahead of New York from March 14 to April 4 and from October 3 to November 7"
	for _, tt := range []struct {
		name       string
		loc, sched *time.Location
		year       int
		clause     string
		note, want string
	}{
		{"steady shift and its windows", berlin, ny, 0, berlinUTC + ", and 6 hours ahead of New York time", berlinNY, ""},
		{"wrong steady shift", berlin, ny, 0, berlinUTC + ", and 7 hours ahead of New York time", berlinNY, "7 hours ahead of New York time"},
		{"windows left out", berlin, ny, 0, berlinUTC + ", and 6 hours ahead of New York time", "", "6 hours ahead of New York time"},
		{"a window a week late", berlin, ny, 0, berlinUTC + ", and 6 hours ahead of New York time", strings.Replace(berlinNY, "March 14 to 28", "March 21 to April 4", 1), "6 hours ahead of New York time"},
		{"windows from January 1", sydney, ny, 0, " Sydney time, 11 hours ahead of UTC, 10 hours from April to October, and 14 hours ahead of New York time", sydneyNY, ""},
		{"no scheduler zone given", berlin, nil, 0, berlinUTC + ", and 6 hours ahead of New York time", berlinNY, "6 hours ahead of New York time"},
		{"months against the scheduler", utc, la, 0, " UTC, 8 hours ahead of Los Angeles time, 7 hours from March to November", "", ""},
		{"wrong months against the scheduler", utc, la, 0, " UTC, 8 hours ahead of Los Angeles time, 7 hours from April to November", "", "8 hours ahead of Los Angeles time, 7 hours from April to November"},
		{"an unnamed scheduler zone", utc, time.FixedZone("", -8*3600), 0, " UTC, 8 hours ahead of the scheduler's time", "", ""},
		{"one change, named in the note", py, nil, 2018, " Pyongyang time, 8 hours 30 minutes ahead of UTC", "Pyongyang is 9 hours ahead of UTC from May 5 to December 31", ""},
		{"one change, no note", py, nil, 2018, " Pyongyang time, 8 hours 30 minutes ahead of UTC", "", "8 hours 30 minutes ahead of UTC"},
		{"a window a day early", berlin, ny, 0, berlinUTC + ", and 6 hours ahead of New York time", strings.Replace(berlinNY, "March 14 to 28", "March 13 to 28", 1), "6 hours ahead of New York time"},
		{"a window a day late at its end", berlin, ny, 0, berlinUTC + ", and 6 hours ahead of New York time", strings.Replace(berlinNY, "March 14 to 28", "March 14 to 29", 1), "6 hours ahead of New York time"},
		{"a change after noon", tonga, berlin, 0, " Tongatapu time, 13 hours ahead of UTC, and 12 hours ahead of Berlin time, 11 hours from March to October", "", ""},
		{"a window within one date", fakaofo, nil, 2011, " Fakaofo time, 11 hours behind UTC", "Fakaofo is 13 hours ahead of UTC on December 31", ""},
		{"a window within one date, named a day late", fakaofo, nil, 2011, " Fakaofo time, 11 hours behind UTC", "Fakaofo is 13 hours ahead of UTC from December 31 to 31 and on December 30", "11 hours behind UTC"},
		{"no shift from UTC", berlin, nil, 0, " Berlin time, ", "", "no shift from UTC"},
		{"a change at 11:30 pm", kolkata, almaty, 2024, kolkataAlmaty, "Kolkata is 30 minutes ahead of Almaty from February 29 to December 31", ""},
		{"a change at 11:30 pm, named a day late", kolkata, almaty, 2024, kolkataAlmaty, "Kolkata is 30 minutes ahead of Almaty from March 1 to December 31", "30 minutes behind Almaty time"},
	} {
		setDescribeYear(tt.year)
		_, bad := zoneMisses(tt.loc, tt.sched, "at 9 am every day"+tt.clause, tt.note)
		if got := strings.Join(bad, ", "); got != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestNamesNotRun checks that the check finds what a wrong description
// names: each case pairs a spec with a description it does not run as.
func TestNamesNotRun(t *testing.T) {
	five := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor)
	years := NewParser(Minute | Hour | Dom | Month | Dow | Year)
	skip := NewParser(Minute | Hour | Dom | Month | Dow | Year | DSTSkip)
	hash := NewParser(Minute | Hour | Dom | Month | Dow | Hash)
	hashSec := NewParser(Second | Minute | Hour | Dom | Month | Dow | Hash)
	hashYears := NewParser(Minute | Hour | Dom | Month | Dow | Year | Hash)
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
		{years, "0 0 1 1 * 2035", "never runs", "never runs"},       // past the parser's default search span
		{years, "0 0 1 1 * 2060", "never runs", "never runs"},       // past a 30-year search from 2026
		{years, "0 0 1 1 * 2025", "never runs", "never runs"},       // before 2026
		{five, "0 0 29 2 1", "never runs", "never runs"},            // February 29 on a Monday: first in 2044
		{years, "0 0 29 2 1 2026-2060", "never runs", "never runs"}, // the same, in a year field
		// 2:30 am is skipped on April 30, 1972, but not in 2000, a year of the same kind
		{skip, "CRON_TZ=America/Chicago 30 2 30 4 * 1972,2000", "never runs", "never runs"},
		// 2028 runs too, but is more than 30 years after 1972: the check's limit
		{skip, "CRON_TZ=America/Chicago 30 2 30 4 * 1972,2028", "never runs", ""},
		{years, "0 0 1 1 * 2035,2037", "at midnight on the 1st of the month in January in years 2035 and 2036", "2036"},
		{years, "0 0 1 1 * 2026-2030", "at midnight on the 1st of the month in January in years 2026 thru 2031", "2026 thru 2031"},
		{years, "0 0 1 1 * 2035", "at midnight on the 1st of the month in January in year 1", "1"}, // the zero time is in year 1
		{years, "0 0 1 1 * 2026-2027", "at midnight on the 1st of the month in January in years 2027 thru 2026", "2027 thru 2026"},
		{five, "0 9,17 * * *", "at the top of every hour from 9 am thru 5 pm every day", "9 am thru 5 pm"},
		{five, "0 9,17 * * *", "every 2 hours from 9 am thru 5 pm in every day", "9 am thru 5 pm"}, // 11 am thru 3 pm: no runs
		{five, "0 9-17/2 * * *", "every 2 hours from 9 am thru 5 pm in every day", ""},
		{five, "0 0-10/2,20-22/2 * * *", "every 2 hours from midnight thru 10 am and 8 pm thru 10 pm in every day", ""},
		{five, "0,10 * * * *", "at minutes 0 thru 10 past the hour", "minute 0 thru 10"},
		{five, "* * * * *", "at minutes 0 thru 60 past the hour", "minute 0 thru 60"}, // an end past 59
		{five, "0 0 * * *", "at midnight on the 1st thru 32nd of the month", "1st thru 32nd"},
		// H: a value is named right when some key runs at it
		{hash, "0 H(9-17) * * *", "at the top of a fixed hour between the hours of 9 am and 6 pm every day; the hash key fixes the hour", "6 pm"},
		{hash, "H(0-29) * * * *", "at a fixed minute between minutes 0 and 30 past the hour; the hash key fixes the minute", "minute 30"},
		{hash, "0 0 H(1-15) * *", "at midnight on a fixed day of the month between the 1st and 16th; the hash key fixes the day", "16th"},
		{hash, "0 9 * * H(1-5)", "at 9 am on a fixed day of the week between Monday and Saturday; the hash key fixes the day", "Saturday"},
		{hash, "0 0 1 H(1-6) *", "at midnight on the 1st of the month in a fixed month between January and July; the hash key fixes the month", "July"},
		{hash, "0 0 H(28-31) 2 *", "never runs", "never runs"}, // a key that picks the 28th runs
		// right: the H day pool names days February lacks; the step is no second
		{hash, "0 0 H(29-31) 2 *", "at midnight on a fixed day of the month between the 29th and 31st in February; the hash key fixes the day", ""},
		{hashSec, "H(50-10)/15 * * * * *", "1 or 2 runs in every minute between seconds 50 and 10, 15 seconds apart, starting from a fixed second; the hash key fixes the second", ""},
		// right: no one key finds a day, though each H alone could (G11)
		{hashYears, "0 0 H(29-31) 1 H(1-3) 2028", "never runs", ""},
		// the zone clause: the shift and its months are read from Next() in the zone
		{five, "TZ=Asia/Kolkata 0 9 * * *", "at 9 am every day Kolkata time, 5 hours 30 minutes ahead of UTC", ""},
		{five, "TZ=Asia/Kolkata 0 9 * * *", "at 10 am every day Kolkata time, 5 hours 30 minutes ahead of UTC", "10 am"},
		{five, "TZ=Asia/Kolkata 0 9 * * *", "at 9 am every day Kolkata time, 5 hours ahead of UTC", "5 hours ahead of UTC"},
		{five, "TZ=Asia/Kolkata 0 9 * * *", "at 9 am every day Kolkata time, 5 hours 30 minutes behind UTC", "5 hours 30 minutes behind UTC"},
		{five, "CRON_TZ=Europe/Berlin 0 0 * * *", "at midnight every day Berlin time, 1 hour ahead of UTC, 2 hours from March to October", ""},
		{five, "CRON_TZ=Europe/Berlin 0 0 * * *", "at midnight every day Berlin time, 1 hour ahead of UTC, 2 hours from April to October", "1 hour ahead of UTC, 2 hours from April to October"},
		{five, "CRON_TZ=Europe/Berlin 0 0 * * *", "at midnight every day Berlin time, 1 hour ahead of UTC, 3 hours from March to October", "1 hour ahead of UTC, 3 hours from March to October"},
		{five, "CRON_TZ=Europe/Berlin 0 0 * * *", "at midnight every day Paris time, 1 hour ahead of UTC, 2 hours from March to October", "no Berlin time"},
		{five, "CRON_TZ=America/New_York 0 9 * * *", "at 9 am every day New York time, 5 hours behind UTC", "5 hours behind UTC"},
		{five, "CRON_TZ=Europe/London 0 12 * * *", "at noon every day London time, the same as UTC, 1 hour ahead from March to October", ""},
		{five, "CRON_TZ=Europe/London 0 12 * * *", "at noon every day London time, the same as UTC, 1 hour behind from March to October", "the same as UTC, 1 hour behind from March to October"},
		// the months are the zone's: Lord Howe's runs are all in January
		{years, "CRON_TZ=Australia/Lord_Howe 0 0 1 1 * 2027", "at midnight on the 1st of the month in January in year 2027 Lord Howe time, 11 hours ahead of UTC, 10 hours 30 minutes from April to October", ""},
		{five, "CRON_TZ=UTC 0 0 * * *", "at midnight every day UTC", ""},
		{five, "CRON_TZ=America/New_York 0 0 30 2 *", "never runs", ""},
	} {
		bad, err := namesNotRunSpec(tt.p, tt.spec, tt.desc, "", nil)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tt.spec, err)
		}
		if got := strings.Join(bad, ", "); got != tt.want {
			t.Errorf("namesNotRun(%q, %q) = %q, want %q", tt.desc, tt.spec, got, tt.want)
		}
	}
}
