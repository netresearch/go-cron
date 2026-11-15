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
	base   time.Duration
	shifts []zoneShift
}

// maxZoneBounds limits the walk over a year's offset changes; no zone
// changes its offset monthly.
const maxZoneBounds = 12

// zoneYearOf walks loc's offset changes through year. A bound that leaves
// the offset as it was is skipped: Go reports year-end bounds past a zone's
// stored transitions, and some not after the instant asked about, which
// zoneEndAfter steps past.
func zoneYearOf(loc *time.Location, year int) zoneYear {
	t := time.Date(year, time.January, 1, 0, 0, 0, 0, loc)
	z := zoneYear{loc: loc, base: offsetOf(t)}
	prev := z.base
	for range maxZoneBounds {
		end := zoneEndAfter(t)
		if end.IsZero() || end.In(loc).Year() > year {
			break
		}
		if off := offsetOf(end); off != prev {
			z.shifts = append(z.shifts, zoneShift{at: end, off: off})
			prev = off
		}
		t = end
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
// as written, "UTC", "Etc/GMT+5", "US/Eastern".
func zoneName(loc *time.Location) string {
	s := loc.String()
	area, _, _ := strings.Cut(s, "/")
	if !slices.Contains(tzAreas, area) {
		return s
	}
	return strings.ReplaceAll(s[strings.LastIndex(s, "/")+1:], "_", " ")
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

// utcShift names a zone's offset from UTC over the year: "5 hours
// behind UTC", and for a zone that changes with the season "5 hours behind
// UTC, 4 hours from March to November".
func utcShift(z zoneYear) string {
	s := shiftWords(z.base, "UTC")
	if !z.seasonal() {
		return s
	}
	return s + ", " + secondWords(z.base, z.shifts[0].off, "UTC") + monthsWords(z.shifts[0].at, z.shifts[1].at, z.loc)
}

// zoneClause returns the clause that names the zone a spec wrote and its
// shift from UTC (zoneName, utcShift), with its leading space. It is "" for a
// spec with no zone and for TZ=Local, which reads as no zone does.
func zoneClause(spec *time.Location, now time.Time) string {
	if spec == nil || spec == time.Local {
		return ""
	}
	name := zoneName(spec)
	if name == "UTC" {
		return " UTC"
	}
	return " " + name + " time, " + utcShift(zoneYearOf(spec, now.In(spec).Year()))
}
