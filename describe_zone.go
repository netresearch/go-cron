// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT.

package cron

import (
	"slices"
	"strconv"
	"strings"
	"time"
)

// describeNow gives the instant whose year the zone clauses are computed
// for. Tests replace it.
var describeNow = time.Now

// zoneShift is a change of a zone's offset from UTC: the instant and the
// offset from then on.
type zoneShift struct {
	at  time.Time
	off time.Duration
}

// zoneYear is a zone's offsets from UTC over one year of its own calendar:
// the offset on January 1 and each change after it.
type zoneYear struct {
	loc    *time.Location
	year   int
	base   time.Duration
	shifts []zoneShift
}

// maxZoneBounds limits the walk over a year's offset changes; no zone
// changes its offset monthly.
const maxZoneBounds = 12

// zoneYearOf walks loc's offset changes through year of its own calendar.
func zoneYearOf(loc *time.Location, year int) zoneYear {
	start := time.Date(year, time.January, 1, 0, 0, 0, 0, loc)
	return zoneYearOver(loc, year, start, start.AddDate(1, 0, 0))
}

// zoneYearOver walks loc's offset changes from start up to end, the instants
// another zone's year spans; year is that year. A bound that leaves the
// offset as it was is skipped: Go reports year-end bounds past a zone's
// stored transitions, and some not after the instant asked about, which
// zoneEndAfter steps past.
func zoneYearOver(loc *time.Location, year int, start, end time.Time) zoneYear {
	t := start.In(loc)
	z := zoneYear{loc: loc, year: year, base: offsetOf(t)}
	prev := z.base
	for range maxZoneBounds {
		bound := zoneEndAfter(t)
		if bound.IsZero() || !bound.Before(end) {
			break
		}
		if off := offsetOf(bound); off != prev {
			z.shifts = append(z.shifts, zoneShift{at: bound, off: off})
			prev = off
		}
		t = bound
	}
	return z
}

// offsetOf returns the offset from UTC in force at t in t's zone.
func offsetOf(t time.Time) time.Duration {
	_, sec := t.Zone()
	return time.Duration(sec) * time.Second
}

// seasonal reports whether the zone leaves its January offset once in the
// year and returns to it, as daylight saving time does.
func (z zoneYear) seasonal() bool {
	return len(z.shifts) == 2 && z.shifts[1].off == z.base
}

// tzAreas are the areas the tz database names its zones under. A name under
// another prefix, as "US/Eastern", is often one of several alike.
var tzAreas = []string{
	"Africa", "America", "Antarctica", "Arctic", "Asia",
	"Atlantic", "Australia", "Europe", "Indian", "Pacific",
}

// zoneName returns the name a description gives loc: for a name under
// one of the tz areas its last part with spaces, "New York"; any other name
// as written (Z1), "UTC", "Etc/GMT+5", "US/Eastern". generic is true for a zone
// with no name of its own, time.Local or an unnamed one, which reads "the
// scheduler's".
func zoneName(loc *time.Location) (name string, generic bool) {
	s := loc.String()
	if s == "Local" || s == "" {
		return "the scheduler's", true
	}
	area, _, _ := strings.Cut(s, "/")
	if !slices.Contains(tzAreas, area) {
		return s, false
	}
	return strings.ReplaceAll(s[strings.LastIndex(s, "/")+1:], "_", " "), false
}

// offsetWords names the size of an offset: "5 hours", "1 hour", "10 hours
// 30 minutes", "30 minutes".
func offsetWords(d time.Duration) string {
	d = d.Abs()
	count := func(n time.Duration, unit string) string {
		if n == 1 {
			return "1 " + unit
		}
		return strconv.Itoa(int(n)) + " " + unit + "s"
	}
	h, m := d/time.Hour, d%time.Hour/time.Minute
	switch {
	case m == 0:
		return count(h, "hour")
	case h == 0:
		return count(m, "minute")
	}
	return count(h, "hour") + " " + count(m, "minute")
}

// shiftWords names an offset from other: "5 hours behind UTC", "1 hour ahead
// of New York time", "the same as UTC".
func shiftWords(d time.Duration, other string) string {
	switch {
	case d == 0:
		return "the same as " + other
	case d < 0:
		return offsetWords(d) + " behind " + other
	}
	return offsetWords(d) + " ahead of " + other
}

// secondWords names the second value of a months form, after first was
// named in full: the size alone on the same side, "4 hours"; the side too
// when it differs, "1 hour ahead"; "the same as UTC" for none.
func secondWords(first, second time.Duration, other string) string {
	switch {
	case second == 0:
		return "the same as " + other
	case first != 0 && (first < 0) == (second < 0):
		return offsetWords(second)
	case second < 0:
		return offsetWords(second) + " behind"
	}
	return offsetWords(second) + " ahead"
}

// monthsWords names the months, in loc, of the two instants a second value
// holds between: " from March to November".
func monthsWords(a, b time.Time, loc *time.Location) string {
	return " from " + a.In(loc).Month().String() + " to " + b.In(loc).Month().String()
}

// utcShift names a zone's offset from UTC over the year: "5 hours behind UTC",
// and for a zone that changes with the season "5 hours behind UTC, 4 hours
// from March to November". A zone that changes in another way gets its January
// offset, and the note names the rest of the year (Z2, Z4).
func utcShift(z zoneYear, name string) (clause, note string) {
	clause = shiftWords(z.base, "UTC")
	switch {
	case len(z.shifts) == 0:
		return clause, ""
	case z.seasonal():
		return clause + ", " + secondWords(z.base, z.shifts[0].off, "UTC") + monthsWords(z.shifts[0].at, z.shifts[1].at, z.loc), ""
	}
	return clause, windowNote(name, "UTC", zoneSpans(z, zoneYearOf(time.UTC, z.year)), z.base, z.loc, false)
}

// dateWords names t's date, "March 14", or its day alone after a date of
// the same month, "28".
func dateWords(t time.Time, sameMonth bool) string {
	if sameMonth {
		return strconv.Itoa(t.Day())
	}
	return t.Month().String() + " " + strconv.Itoa(t.Day())
}

// windowWords names a window by the dates of its first and last instant:
// "from March 14 to 28", "from October 31 to November 7", or "on September
// 30" for a window within one date.
func windowWords(from, last time.Time) string {
	if from.Month() == last.Month() && from.Day() == last.Day() {
		return "on " + dateWords(from, false)
	}
	return "from " + dateWords(from, false) + " to " + dateWords(last, from.Month() == last.Month())
}

// windowNote names the windows in which subject's zone is off the steady
// shift the description states, by the dates, in loc, of each window's
// first and last instant (Z4): "Berlin is 5 hours ahead of New York from
// March 14 to 28 and from October 31 to November 7", a list as G8 has it.
// Each shift gets one sentence. A window under a day is left out, although a
// run inside it is off the stated shift. tail says that the windows lie
// between two zones' clock changes (betweenChanges).
func windowNote(subject, object string, spans []zoneSpan, steady time.Duration, loc *time.Location, tail bool) string {
	var order []time.Duration
	windows := map[time.Duration][]string{}
	for _, s := range spans {
		if !s.named(steady) {
			continue
		}
		if windows[s.off] == nil {
			order = append(order, s.off)
		}
		windows[s.off] = append(windows[s.off], windowWords(s.from.In(loc), s.to.Add(-1).In(loc)))
	}
	if len(order) == 0 {
		return ""
	}
	sentences := make([]string, len(order))
	for i, off := range order {
		sentences[i] = subject + " is " + shiftWords(off, object) + " " + andList(windows[off])
	}
	note := strings.Join(sentences, "; ")
	if tail {
		note += ", between the two zones' clock changes"
	}
	return note
}

// utc reports whether the zone is on UTC all year, whatever its name.
func (z zoneYear) utc() bool {
	return z.base == 0 && len(z.shifts) == 0
}

// same reports whether o has z's offsets all year.
func (z zoneYear) same(o zoneYear) bool {
	return z.base == o.base && slices.EqualFunc(z.shifts, o.shifts, func(a, b zoneShift) bool {
		return a.at.Equal(b.at) && a.off == b.off
	})
}

// zoneSpan is a stretch of the year, from its start up to its end, in which
// a spec's zone keeps one offset from the scheduler's.
type zoneSpan struct {
	from, to time.Time
	off      time.Duration
}

// named reports whether a note names s as a window off the steady shift: it
// has another offset and lasts a day or more.
func (s zoneSpan) named(steady time.Duration) bool {
	return s.off != steady && s.to.Sub(s.from) >= 24*time.Hour
}

// changesAt reports whether the zone's offset changes at t.
func (z zoneYear) changesAt(t time.Time) bool {
	return slices.ContainsFunc(z.shifts, func(s zoneShift) bool { return s.at.Equal(t) })
}

// betweenChanges reports whether every window the note names starts at a
// clock change of one zone and ends at a clock change of the other, as
// Berlin's and New York's spring and autumn gaps do. A window from January 1
// or to December 31, or one between two changes of one zone, is not.
func betweenChanges(spans []zoneSpan, steady time.Duration, spec, sched zoneYear) bool {
	for _, s := range spans {
		fromTo := func(a, b zoneYear) bool { return a.changesAt(s.from) && b.changesAt(s.to) }
		if s.named(steady) && !fromTo(spec, sched) && !fromTo(sched, spec) {
			return false
		}
	}
	return true
}

// zoneSpans splits spec's year, in its own zone, at each offset change of
// either zone. Neighbors with one offset are one span, as when both zones
// change at the same instant.
func zoneSpans(spec, sched zoneYear) []zoneSpan {
	start := time.Date(spec.year, time.January, 1, 0, 0, 0, 0, spec.loc)
	end := start.AddDate(1, 0, 0)
	cuts := []time.Time{start}
	for _, s := range slices.Concat(spec.shifts, sched.shifts) {
		if s.at.After(start) && s.at.Before(end) {
			cuts = append(cuts, s.at)
		}
	}
	slices.SortFunc(cuts, time.Time.Compare)
	var spans []zoneSpan
	for i, from := range cuts {
		to := end
		if i+1 < len(cuts) {
			to = cuts[i+1]
		}
		off := offsetOf(from.In(spec.loc)) - offsetOf(from.In(sched.loc))
		if n := len(spans); n > 0 && spans[n-1].off == off {
			spans[n-1].to = to
			continue
		}
		spans = append(spans, zoneSpan{from: from, to: to, off: off})
	}
	return spans
}

// steadyOf picks the offset the description states when it states one for
// the year: with two seasonal zones the one in force for the most time, the
// earlier on a tie; otherwise the offset at the start of the year.
func steadyOf(spans []zoneSpan, bothSeasonal bool) time.Duration {
	best := spans[0].off
	if !bothSeasonal {
		return best
	}
	total := map[time.Duration]time.Duration{}
	for _, s := range spans {
		total[s.off] += s.to.Sub(s.from)
	}
	for _, s := range spans {
		if total[s.off] > total[best] {
			best = s.off
		}
	}
	return best
}

// schedShift names a spec zone's shift from the scheduler's zone (Z3): one
// value when it holds all year; the months form when one zone changes with
// the season and the other does not, "8 hours ahead of Los Angeles time, 7
// hours from March to November"; otherwise the steady value, and the note
// names the windows off it (Z4). name is the spec zone's name; sname is the
// scheduler's, generic when it has none of its own.
func schedShift(spec, sched zoneYear, name, sname string, generic bool) (clause, note string) {
	other := sname + " time"
	spans := zoneSpans(spec, sched)
	moving := spec
	if len(spec.shifts) == 0 {
		moving = sched
	}
	oneMoves := len(spec.shifts) == 0 || len(sched.shifts) == 0
	switch {
	case len(spans) == 1:
		return shiftWords(spans[0].off, other), ""
	case oneMoves && moving.seasonal():
		return shiftWords(spans[0].off, other) + ", " + secondWords(spans[0].off, spans[1].off, other) +
			monthsWords(moving.shifts[0].at, moving.shifts[1].at, spec.loc), ""
	}
	steady := steadyOf(spans, spec.seasonal() && sched.seasonal())
	object := sname
	if generic {
		object = other
	}
	return shiftWords(steady, other), windowNote(name, object, spans, steady, spec.loc, betweenChanges(spans, steady, spec, sched))
}

// zoneClause returns the clause that names the zone a spec wrote, its shift
// from UTC and its shift from the scheduler's zone (Z1–Z3), with its leading
// space, and the note for the windows off those shifts (Z4). Both are "" for a
// spec with no zone and for TZ=Local, which reads as no zone does (Z6). The
// scheduler's shift is left out when sched is nil, as the scheduler may run in
// another process and zone than the caller (Z9), and when the scheduler runs on UTC, under any name, or has the spec zone's
// offsets all year.
func zoneClause(spec, sched *time.Location, now time.Time) (clause, note string) {
	if spec == nil || spec == time.Local {
		return "", ""
	}
	z := zoneYearOf(spec, now.In(spec).Year())
	name, _ := zoneName(spec)
	clause, join := " UTC", ", "
	if name != "UTC" {
		shift, n := utcShift(z, name)
		clause, note, join = " "+name+" time, "+shift, n, ", and "
	}
	if sched == nil {
		return clause, note
	}
	sname, generic := zoneName(sched)
	start := time.Date(z.year, time.January, 1, 0, 0, 0, 0, spec)
	if s := zoneYearOver(sched, z.year, start, start.AddDate(1, 0, 0)); !s.utc() && !s.same(z) {
		shift, n := schedShift(z, s, name, sname, generic)
		clause, note = clause+join+shift, joinNote(note, n)
	}
	return clause, note
}

// joinNote joins two notes, as the note for the hashed values and the note
// for the zone.
func joinNote(hash, zone string) string {
	if hash == "" || zone == "" {
		return hash + zone
	}
	return hash + "; " + zone
}
