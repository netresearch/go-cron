// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT.

package cron

import (
	"fmt"
	"strings"
	"time"
)

// noteVars are the variables of a note's values, taken in field order (N6).
var noteVars = []string{"h", "k", "j", "m", "n", "p"}

// dateLimit is how many days or months a note lists before it cuts the
// list short (T1).
const dateLimit = 12

// noter builds a note: the description's state and the variables taken.
type noter struct {
	d    *describer
	vars int
}

// next takes the next variable.
func (n *noter) next() string {
	v := noteVars[n.vars]
	n.vars++
	return v
}

// hashNote spells out the values the hash key picks, one clause per field
// with a hashed step or a hashed day of the month, in field order and
// joined with ", " (N1, N6). It is "" when there is none, and when a field
// has no form.
func (p Parser) hashNote(d *describer, h, hd [3]hashField, dates [3]string) string {
	n := noter{d: d}
	sec := newTimeField(h[0].plain, unitSecond, unitMinute, seconds)
	minute := newTimeField(h[1].plain, unitMinute, unitHour, minutes)
	hour := newTimeField(h[2].plain, unitHour, "day", hours)
	secPlace, secOK := n.secondPlace(minute, hour, h[1].hashes() || h[2].hashes())
	minPlace, minOK := n.minutePlace(hour, h[2])
	minOK = minOK && sec.single() && sec.vals[0] == 0 && !h[0].hashes()
	var clauses []string
	for _, c := range []func() (string, bool){
		func() (string, bool) { return n.stepNote(h[0], unitSecond, seconds, secPlace, secOK) },
		func() (string, bool) { return n.stepNote(h[1], unitMinute, minutes, minPlace, minOK) },
		func() (string, bool) { return n.hourNote(h[2], sec, minute, h[0], h[1]) },
		func() (string, bool) { return n.domNote(hd[0], newDomField(dates[0])) },
		func() (string, bool) { return n.outcomeNote(hd[1], p.dowBounds(), p.newDowField(dates[1])) },
		func() (string, bool) { return n.outcomeNote(hd[2], months, newMonthField(dates[2])) },
	} {
		text, ok := c()
		if !ok {
			return ""
		}
		if text != "" {
			clauses = append(clauses, text)
		}
	}
	return strings.Join(clauses, ", ")
}

// secondPlace is where a hashed second step falls, by the written minutes
// and hours (C1, R12): " within a minute", " within every minute during the
// 9 am hour", " within every minute from 9 am thru 5:59 pm", " within minute
// 5 in every hour", " past the top of every hour", " within minutes 0 thru
// 10 and 50 thru 59 in every hour". ok is false for other fields.
func (n *noter) secondPlace(minute, hour timeField, hashed bool) (string, bool) {
	switch {
	case hashed:
		return "", false
	case minute.every() && hour.every():
		return " within a minute", true
	case minute.every():
		switch hour.kind() {
		case namedHours, stepHours:
			return " within every minute during " + n.d.hourNames(hour), true
		case longRange:
			last := func(h uint) string { return n.d.clock(h, 59, 0) } // R12: the last run is minute 59
			return " within every minute " + fromThru(hour.items[0], n.d.onHour, last), true
		}
	case !hour.every():
	case minute.single() && minute.vals[0] == 0:
		return " past the top of every hour", true
	case minute.single():
		return fmt.Sprintf(" within minute %d in every hour", minute.vals[0]), true
	default:
		text, _ := minute.nums()
		return " within minutes " + text + " in every hour", true
	}
	return "", false
}

// minutePlace is where a hashed minute step falls, by the hours (R6, R9,
// R11): " past the hour", " during the 9 am hour", " past every hour from
// 9 am thru 5 pm", " every 2 hours starting with the midnight hour". ok is
// false for other hours, and for a step with a part of one run.
func (n *noter) minutePlace(hour timeField, hh hashField) (string, bool) {
	if hh.hashes() {
		return "", false
	}
	switch hour.kind() {
	case everyHour:
		return " past the hour", true
	case namedHours, stepHours:
		return " during " + n.d.hourNames(hour), true
	case longRange:
		return " past " + n.d.everyHourParts(hour.items[0]), true
	case longStep:
		for _, part := range stepParts(hour.items[0], hours) {
			if part[0] == part[1] && !wholeDay(hour.items[0]) {
				return "", false
			}
		}
		return " " + strings.TrimSuffix(n.d.stepClause(hour), inEveryDaySuffix), true
	}
	return "", false
}

// whole returns x over its field's range when it covers every value, so a
// wrapped range of every value counts from the range's start (H10). A step
// that does not divide the range keeps its written start: the parser walks
// from there, so the start decides the values (N4).
func whole(x hashed, r bounds) hashed {
	if !x.whole {
		return x
	}
	_, _, cr := dowCycle(r.min, r.max, r)
	if x.step > 1 && (cr.max-cr.min+1)%x.step != 0 {
		return x
	}
	x.lo, x.hi = cr.min, cr.max
	return x
}

// startsElsewhere reports a step over the whole range kept at a written
// start other than the field's first value (whole).
func startsElsewhere(x hashed, r bounds) bool {
	return x.whole && x.lo != r.min
}

// oneStep returns f's one hashed step, alone in its field. ok is false when
// the field has other items beside it, which have no note form.
func oneStep(f hashField) (x hashed, ok bool) {
	if len(f.steps) != 1 || len(f.fixed) > 0 || f.plain != "" {
		return x, false
	}
	return f.steps[0], true
}

// stepNote is the clause of a hashed step in the second or minute field
// (N2, N3): "at minutes h, h+15, h+30, and h+45 past the hour, where h is a
// hashed minute from 0 to 14". place is where the values fall, and placeOK
// whether there is a form for it.
func (n *noter) stepNote(f hashField, unit string, r bounds, place string, placeOK bool) (string, bool) {
	if len(f.steps) == 0 {
		return "", true
	}
	x, ok := oneStep(f)
	x = whole(x, r)
	if !ok || !placeOK || startsElsewhere(x, r) {
		return "", false // N4: no list that long, and no plain form is true
	}
	if x.lo > x.hi {
		return "at " + unit + "s " + timeOutcomes(x, r) + place + ", depending on the hash", true
	}
	v := n.next()
	vals, top, tail := stepValues(v, x, valueLimit)
	where := fmt.Sprintf(", where %s is a hashed %s from %d to %d", v, unit, x.lo, top)
	return "at " + unit + "s " + vals + place + where + tail, true
}

// hourNote is the clause of a hashed hour step (N7, N8): the minutes as the
// description names them, then the fixed hour's range and the step: "at the
// top of a fixed hour between midnight and 5 am and every 6 hours after it",
// with "thru" the range's last hour when the range is not the whole day. A
// wrapped range reads in clock order from its start: "between 10 pm and 11
// pm and every 2 hours after it thru 4 am".
func (n *noter) hourNote(f hashField, sec, minute timeField, hs, hm hashField) (string, bool) {
	if len(f.steps) == 0 {
		return "", true
	}
	x, ok := oneStep(f)
	x = whole(x, hours)
	lead, leadOK := hourLead(minute, hm)
	if !ok || !leadOK || hs.hashes() || !sec.single() || sec.vals[0] != 0 {
		return "", false
	}
	span := hours.max - hours.min + 1
	top := (x.lo + min(x.step, cyclicRangeSize(x.lo, x.hi, hours)) - 1) % span
	text := fmt.Sprintf("%s a fixed hour between %s and %s and every %d hours after it",
		lead, n.d.onHour(x.lo), n.d.onHour(top), x.step)
	if !x.whole || startsElsewhere(x, hours) {
		text += " thru " + n.d.onHour(x.hi)
	}
	return text, true
}

// hourLead names the minutes before a hashed hour step as the description
// does (R4, R5, H14, H22, N8): "at the top of", "at 30 minutes past", "every
// minute during", "at minutes 0 and 30 during", "at a fixed minute between
// minutes 0 and 29 during". ok is false for a hashed minute step, or a
// hashed minute beside other items or over a wrapped range.
func hourLead(minute timeField, hm hashField) (string, bool) {
	switch {
	case len(hm.steps) > 0:
		return "", false
	case len(hm.fixed) > 0:
		x := hm.fixed[0]
		if len(hm.fixed) > 1 || hm.plain != "" || x.lo > x.hi {
			return "", false
		}
		if x.whole {
			return "at a fixed minute during", true
		}
		return fmt.Sprintf("at a fixed minute between minutes %d and %d during", x.lo, x.hi), true
	case minute.every():
		return "every minute during", true
	case minute.single():
		return minuteAt(minute.vals[0]), true
	}
	text, _ := minute.nums()
	return "at minutes " + text + " during", true
}

// stepValues writes the values of a hashed step with the variable v (N2,
// N3), cut short past limit (T1), and a tail for the last value when only
// some starts have it: "h and h+40", "; h+40 runs only when h is below 20".
// top is the largest start.
func stepValues(v string, x hashed, limit int) (vals string, top uint, tail string) {
	top = x.lo + min(x.step, x.hi-x.lo+1) - 1
	sure, all := (x.hi-top)/x.step+1, (x.hi-x.lo)/x.step+1
	terms := make([]string, all)
	for k := range all {
		terms[k] = v
		if k > 0 {
			terms[k] = fmt.Sprintf("%s+%d", v, k*x.step)
		}
	}
	vals = andList(terms)
	if int(all) > limit {
		vals = strings.Join(terms[:3], ", ") + ", … " + terms[all-1]
	}
	if all > sure {
		k := sure * x.step
		tail = fmt.Sprintf("; %s+%d runs only when %s is below %d", v, k, v, x.hi-k+1)
	}
	return vals, top, tail
}

// domNote is the clause of a hashed day of the month (N4, N5): its step as
// the other date fields', or a fixed day: "on day h of every month, where h
// is a hashed day from 1 to 31; a month without day h is skipped".
func (n *noter) domNote(f hashField, plain timeField) (string, bool) {
	switch f.shape(plain) {
	case hashNone:
		return "", true
	case hashStep:
		return n.outcomeNote(f, dom, plain)
	case hashFixed:
		x := f.fixed[0]
		v := n.next()
		from, ok := rangeParts(whole(x, dom), dom, number, "", " to ")
		if !ok {
			return "", false
		}
		text := fmt.Sprintf("on day %s of every month, where %s is a hashed day from %s", v, v, from)
		if x.lo > x.hi || x.hi > 28 {
			text += fmt.Sprintf("; a month without day %s is skipped", v)
		}
		return text, true
	case hashFixedPlus:
		first := ordinal(plain.vals[0])
		v := n.next()
		return fmt.Sprintf("on the %s and day %s of every month, where %s is a hashed day from 1 to 31; a month without day %s runs on the %s only",
			first, v, v, v, first), true
	}
	return "", false
}

// outcomeNote is the clause of a hashed step in a date field (N4): the
// day of the week always lists its outcomes; the day of the month and the
// month list up to 3, and a wrapped range, otherwise the variable form. A
// field without a hashed step has no clause.
func (n *noter) outcomeNote(f hashField, r bounds, plain timeField) (string, bool) {
	if len(f.steps) == 0 {
		return "", true
	}
	x, ok := oneStep(f)
	if !ok {
		return "", false
	}
	x = whole(x, r)
	outs := stepOutcomes(x, r)
	in, of := "on ", ""
	switch {
	case plain.container == unitMonth:
		of = " of every month"
	case plain.unit == unitMonth:
		in = "in "
	}
	if isDowBounds(r) || x.lo > x.hi || len(outs) <= 3 {
		words := make([]string, len(outs))
		for i, o := range outs {
			words[i] = outcomeWords(o, plain)
		}
		last := len(words) - 1
		text := in + strings.Join(words[:last], "; ") + "; or " + words[last] + of + ", depending on the hash"
		if x.lo > x.hi && plain.container == unitMonth {
			text += "; a short month skips the days it lacks and changes the last step"
		}
		return text, true
	}
	v := n.next()
	vals, top, tail := stepValues(v, x, dateLimit)
	unit, bound := plain.unit, number
	if unit == unitMonth {
		bound = func(m uint) string { return fmt.Sprintf("%d (%s)", m, time.Month(m)) } // #nosec G115 -- a month, 1 to 12
	}
	return fmt.Sprintf("%s%ss %s%s, where %s is a hashed %s from %s to %s%s",
		in, unit, vals, of, v, unit, bound(x.lo), bound(top), tail), true
}

// timeOutcomes lists the values a wrapped hashed step in the second or
// minute field selects for each start, ordered by the start, each in the
// order the parser walks them (N2, N4): "50, 55, 0, 5, and 10; 51, 56, 1,
// and 6; …; or 54, 59, 4, and 9".
func timeOutcomes(x hashed, r bounds) string {
	outs := stepOutcomes(x, r)
	words := make([]string, len(outs))
	for i, o := range outs {
		nums := make([]string, len(o))
		for j, v := range o {
			nums[j] = number(v)
		}
		words[i] = andList(nums)
	}
	last := len(words) - 1
	return strings.Join(words[:last], "; ") + "; or " + words[last]
}

// stepOutcomes returns the values a hashed step selects for each start the
// hash can pick, ordered by the start, each in the order the parser walks
// the field (the day of week on its seven-day cycle).
func stepOutcomes(x hashed, r bounds) [][]uint {
	lo, hi, cr := dowCycle(x.lo, x.hi, r)
	size, span := cyclicRangeSize(lo, hi, cr), cr.max-cr.min+1
	out := make([][]uint, 0, min(x.step, size))
	for o := range min(x.step, size) {
		start := (lo+o-cr.min)%span + cr.min
		out = append(out, timeItem{lo: start, hi: hi, step: x.step}.runs(cr))
	}
	return out
}

// outcomeWords names one outcome in the order it runs: days of the month
// with one "the", cut short past dateLimit (T1): "the 1st, 3rd, 5th, … 31st";
// days of the week and months by name.
func outcomeWords(vals []uint, plain timeField) string {
	if plain.container != unitMonth {
		words := make([]string, len(vals))
		for i, v := range vals {
			words[i] = plain.w(v)
		}
		return andList(words)
	}
	words := make([]string, len(vals))
	for i, v := range vals {
		words[i] = ordinal(v)
	}
	if len(words) > dateLimit {
		return "the " + strings.Join(words[:3], ", ") + ", … " + words[len(words)-1]
	}
	return "the " + andList(words)
}
