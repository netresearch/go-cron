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
	namedClock  = regexp.MustCompile(`\b(midnight|noon|(\d+)(?::(\d\d))?(?::(\d\d))? ([ap])m)\b`)
	namedValues = regexp.MustCompile(`\b(second|minute)s? (\d+(?:(?:, and |, … |, | and | thru )\d+)*)|\b(\d+) (second|minute)s?\b`)
	trailingIvl = regexp.MustCompile(` \(every .*\)$`)
	digits      = regexp.MustCompile(`\d+`)
)

// dayRuns walks Next() over one day and returns the hours, the minutes and
// the seconds that run, and every time of day as h*3600+m*60+s.
func dayRuns(sch Schedule) (hours, minutes, seconds, times map[int]bool) {
	hours, minutes, seconds, times = map[int]bool{}, map[int]bool{}, map[int]bool{}, map[int]bool{}
	start := time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)
	for t := sch.Next(start.Add(-time.Second)); t.Before(start.AddDate(0, 0, 1)); t = sch.Next(t) {
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
	desc = trailingIvl.ReplaceAllString(desc, "") // "(every 7 minutes, …)" names an interval, not a value
	var bad []string
	for _, m := range namedClock.FindAllStringSubmatch(desc, -1) {
		h, minute, sec := 0, -1, -1
		switch m[1] {
		case "midnight":
		case "noon":
			h = 12
		default:
			h, _ = strconv.Atoi(m[2])
			h %= 12
			if m[5] == "p" {
				h += 12
			}
			if m[3] != "" {
				minute, _ = strconv.Atoi(m[3])
			}
			if m[4] != "" {
				sec, _ = strconv.Atoi(m[4])
			}
		}
		ok := hours[h]
		switch {
		case sec >= 0:
			ok = times[h*3600+minute*60+sec]
		case minute >= 0:
			ok = false
			for s := range 60 {
				ok = ok || times[h*3600+minute*60+s]
			}
		}
		if !ok {
			bad = append(bad, m[1])
		}
	}
	for _, m := range namedValues.FindAllStringSubmatch(desc, -1) {
		unit, list := m[1], m[2]
		if unit == "" {
			unit, list = m[4], m[3]
		}
		runs := minutes
		if unit == "second" {
			runs = seconds
		}
		for _, d := range digits.FindAllString(list, -1) {
			if v, _ := strconv.Atoi(d); !runs[v] {
				bad = append(bad, unit+" "+d)
			}
		}
	}
	return bad
}

// randomTimeField returns a field of one to three items over lo thru hi: a
// value, a range, plain or wrapped, or a step.
func randomTimeField(r *rand.Rand, lo, hi int) string {
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
			items[i] = fmt.Sprintf("*/%d", 2+r.IntN(hi/2))
		default:
			items[i] = fmt.Sprintf("%d-%d/%d", val(), val(), 2+r.IntN(6))
		}
	}
	return strings.Join(items, ",")
}

// TestDescribeSpecNamesRuns enforces G1 for the time fields: every clock
// time, minute and second that a description names is one the scheduler
// runs at, for the goldens and for random time fields.
func TestDescribeSpecNamesRuns(t *testing.T) {
	five := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor)
	six := NewParser(Second | Minute | Hour | Dom | Month | Dow | Descriptor)
	check := func(p Parser, spec string) bool {
		sch, err := p.Parse(spec)
		if err != nil {
			return false // not a spec this parser takes
		}
		desc, err := p.DescribeSpec(spec)
		if err != nil {
			t.Errorf("DescribeSpec(%q): unexpected error %v", spec, err)
			return false
		}
		if literalForm.MatchString(desc) {
			return false // field by field: it names no time
		}
		if bad := checkNamedTimes(desc, sch); len(bad) > 0 {
			t.Errorf("DescribeSpec(%q) = %q: names %s, which Next() does not run at", spec, desc, strings.Join(bad, ", "))
		}
		return true
	}

	checked := 0
	for _, tt := range describeGoldens {
		p := five
		if len(strings.Fields(strings.TrimPrefix(tt.spec, "CRON_TZ=UTC "))) == 6 {
			p = six
		}
		if check(p, tt.spec) {
			checked++
		}
	}
	if checked < 100 {
		t.Errorf("checked %d goldens, want at least 100", checked)
	}

	r := rand.New(rand.NewPCG(1, 2)) // fixed seed: the same specs on every run
	checked = 0
	for range 600 {
		spec := randomTimeField(r, 0, 59) + " " + randomTimeField(r, 0, 23) + " * * *"
		if check(five, spec) {
			checked++
		}
	}
	for range 100 {
		spec := strconv.Itoa(r.IntN(60)) + " " + randomTimeField(r, 0, 59) + " " + randomTimeField(r, 0, 23) + " * * *"
		if check(six, spec) {
			checked++
		}
	}
	if checked < 400 {
		t.Errorf("checked %d random specs, want at least 400", checked)
	}
}
