// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT.

package cron

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// hashItemSyntax is the H syntax: H or H(lo-hi), each with an optional step,
// the numbers as strconv.Atoi reads them.
var hashItemSyntax = regexp.MustCompile(`^H(?:\(\+?\d+-\+?\d+\))?(?:/\+?\d+)?$`)

// hashed is an H item that selects by the hash: one value of lo-hi (step 1),
// or a hashed start with a step through lo-hi.
type hashed struct {
	lo, hi, step uint
	whole        bool // lo-hi is the field's range
}

// hashField is a field split into its plain items and the H items that
// select by the hash, once each.
type hashField struct {
	plain string   // the plain items, H/1 and H(n-n) among them, comma-separated
	fixed []hashed // H items of one value, the whole range first
	steps []hashed // H items with a step
}

// splitHash splits field as the parser reads its H items (H10): H/1 is every
// value of its range, like */1, and a one-value range is that value, so both
// join the plain items; a step at or past the range size selects one value.
// A field whose plain items select every value drops its H items (T11).
func splitHash(field string, r bounds) hashField {
	var h hashField
	var plain []string
	for _, it := range strings.FieldsFunc(field, func(r rune) bool { return r == ',' }) {
		x, isHash := readHash(it, r)
		switch {
		case !isHash:
			plain = append(plain, it)
		case x.step == 0:
			plain = append(plain, fmt.Sprintf("%d-%d", x.lo, x.hi))
		case x.step > 1:
			if !slices.Contains(h.steps, x) {
				h.steps = append(h.steps, x)
			}
		case !slices.Contains(h.fixed, x) && x.whole:
			h.fixed = slices.Insert(h.fixed, 0, x) // the whole range first (H6)
		case !slices.Contains(h.fixed, x):
			h.fixed = append(h.fixed, x)
		}
	}
	h.plain = strings.Join(plain, ",")
	if isWildcard(h.plain) || len(plain) > 0 && newTimeField(h.plain, "", "", r).every() {
		h.fixed, h.steps = nil, nil
	}
	return h
}

// readHash reads an H item as the parser does; step 0 marks one that selects
// plain values, every value of lo-hi. isHash is false for other items.
func readHash(it string, r bounds) (x hashed, isHash bool) {
	if !strings.HasPrefix(it, "H") {
		return x, false
	}
	x = hashed{lo: r.min, hi: r.max, step: 1, whole: true}
	rng, step, hasStep := strings.Cut(it, "/")
	// The spec parsed, so the bounds and the step are valid.
	if inner, ok := strings.CutPrefix(rng, "H("); ok {
		lo, hi, _ := strings.Cut(strings.TrimSuffix(inner, ")"), "-")
		x.lo, _ = mustParseInt(lo)
		x.hi, _ = mustParseInt(hi)
		x.whole = x.lo == r.min && (x.hi == r.max || isDowBounds(r) && x.hi == 6)
	}
	if hasStep {
		x.step, _ = mustParseInt(step)
	}
	size := cyclicRangeSize(dowCycle(x.lo, x.hi, r))
	switch {
	case x.step == 1 && hasStep || size == 1:
		x.step = 0
	case x.step >= size:
		x.step = 1
	}
	return x, true
}

// hashShape sorts a field by its H items.
type hashShape int

const (
	hashNone      hashShape = iota // no H item selects by the hash
	hashFixed                      // one fixed value, nothing else
	hashFixedPlus                  // one fixed value of the whole range and one plain value (H15)
	hashTwoFixed                   // a fixed value of the whole range and one of a part (H6)
	hashStep                       // one hashed step, nothing else (H2)
	hashStepPlus                   // one hashed step of the whole range and plain 0 (H5)
	hashOther                      // no form: the spec renders field by field
)

// shape returns h's shape; f is its plain items.
func (h hashField) shape(f timeField) hashShape {
	zero := f.single() && f.vals[0] == 0
	switch {
	case len(h.fixed)+len(h.steps) == 0:
		return hashNone
	case len(h.steps) == 1 && len(h.fixed) == 0 && len(f.vals) == 0:
		return hashStep
	case len(h.steps) == 1 && len(h.fixed) == 0 && zero && h.steps[0].whole:
		return hashStepPlus
	case len(h.steps) > 0:
		return hashOther
	}
	return h.fixedShape(f)
}

// fixedShape returns the shape of h, whose H items are fixed values.
func (h hashField) fixedShape(f timeField) hashShape {
	switch {
	case len(h.fixed) == 1 && len(f.vals) == 0:
		return hashFixed
	case len(h.fixed) == 1 && f.single() && h.fixed[0].whole:
		return hashFixedPlus
	case len(h.fixed) == 2 && len(f.vals) == 0 && h.fixed[0].whole && !h.fixed[1].whole:
		return hashTwoFixed
	}
	return hashOther
}

// between names the range of a hashed value or step: "between minutes 0 and
// 29", "between minutes 0 and 10 or 50 and 59" (H3, T9).
func between(unit string, x hashed, r bounds) string {
	parts := timeItem{lo: x.lo, hi: x.hi}.parts(r)
	words := make([]string, len(parts))
	for i, p := range parts {
		words[i] = fmt.Sprintf("%d and %d", p[0], p[1])
	}
	return "between " + unit + "s " + strings.Join(words, " or ")
}

// fixed names a hashed value: "a fixed minute", "a fixed minute between
// minutes 0 and 29", "a fixed hour between the 9 am and 5 pm hours" (H1, H3).
// ok is false for a wrapped hour range, which has no form.
func (d *describer) fixed(unit string, x hashed, r bounds) (string, bool) {
	w := "a fixed " + unit
	switch {
	case x.whole:
		return w, true
	case unit != unitHour:
		return w + " " + between(unit, x, r), true
	case x.lo > x.hi:
		return "", false
	}
	return w + " between " + d.hourNames(timeField{vals: []uint{x.lo, x.hi}}), true
}

// runRange returns the fewest and most values a hashed step selects in its
// container, with the plain values beside it (H2, H15): each start the hash
// can pick is counted.
func runRange(x hashed, r bounds, plain []uint) (lo, hi int) {
	x.lo, x.hi, r = dowCycle(x.lo, x.hi, r)
	size, span := cyclicRangeSize(x.lo, x.hi, r), r.max-r.min+1
	lo = int(span) + len(plain)
	add := func(vals []uint) {
		n := len(uniq(append(slices.Clone(plain), vals...)))
		lo, hi = min(lo, n), max(hi, n)
	}
	for o := range x.step {
		if o < size && x.step > 1 {
			start := (x.lo+o-r.min)%span + r.min
			add(timeItem{lo: start, hi: x.hi, step: x.step}.runs(r))
			continue
		}
		// A fixed value, or a start past the range, which is drawn again:
		// one value of the range.
		for v := range size {
			add([]uint{(x.lo+v-r.min)%span + r.min})
		}
	}
	return lo, hi
}

// counted writes a count with its unit, or the counts the hashed start
// decides between: "4 runs", "1 or 2 runs", "4 or 5 hours" (H2); "2 to 4
// days" when the month's length decides too (H19).
func counted(lo, hi int, unit string) string {
	switch {
	case lo == hi:
		return count(uint(lo), unit) // #nosec G115 -- counts are small and positive
	case hi == lo+1:
		return fmt.Sprintf("%d or %d %ss", lo, hi, unit)
	}
	return fmt.Sprintf("%d to %d %ss", lo, hi, unit)
}

// runCount names the runs of a hashed step: "4 runs", "1 or 2 runs".
func runCount(x hashed, r bounds, plain []uint) string {
	lo, hi := runRange(x, r, plain)
	return counted(lo, hi, "run")
}

// stepTail closes a hashed step: ", 15 minutes apart, starting from a fixed
// minute" (H2).
func stepTail(x hashed, unit string) string {
	return fmt.Sprintf(", %d %ss apart, starting from a fixed %s", x.step, unit, unit)
}

// hashKeyClause names the fields the hash key fixes: "; the hash key fixes
// the minute and the hour", "; the hash key fixes both minutes" (H1).
func hashKeyClause(units []string) string {
	if len(units) == 0 {
		return ""
	}
	if len(units) == 2 && units[0] == units[1] {
		return "; the hash key fixes both " + units[0] + "s"
	}
	words := make([]string, len(units))
	for i, u := range units {
		words[i] = "the " + u
	}
	return "; the hash key fixes " + andList(words)
}

// hashUnits names the field of each H item that selects by the hash, second
// first: "minute", "minute" for two hashed minutes.
func hashUnits(h []hashField, names []string) []string {
	var units []string
	for i, f := range h {
		for range len(f.fixed) + len(f.steps) {
			units = append(units, names[i])
		}
	}
	return units
}

// describeHashTime describes second, minute and hour fields with H items
// (H1–H16). ok is false for the combinations without a form, which render
// field by field (G10).
func (d *describer) describeHashTime(h [3]hashField) (string, bool) {
	sec := newTimeField(h[0].plain, unitSecond, unitMinute, seconds)
	minute := newTimeField(h[1].plain, unitMinute, unitHour, minutes)
	hour := newTimeField(h[2].plain, unitHour, "day", hours)
	ss, ms, hs := h[0].shape(sec), h[1].shape(minute), h[2].shape(hour)
	switch {
	case ss == hashFixed:
		return d.hashSecond(h[0].fixed[0], h[1], minute, ms, hour, hs)
	case ss == hashStep && ms == hashNone && hs == hashNone && minute.every():
		x := h[0].steps[0]
		place, ok := d.stepPlace(unitSecond, x, seconds, hour)
		return runCount(x, seconds, nil) + " in every minute" + place + stepTail(x, unitSecond), ok
	case ss != hashNone || !sec.single() || sec.vals[0] != 0:
		return "", false
	case hs == hashNone:
		return d.hashMinute(h[1], minute, ms, hour)
	case ms == hashNone:
		return d.hashHour(h[2], hour, hs, minute)
	case ms == hashFixed && hs == hashFixed:
		mw, _ := d.fixed(unitMinute, h[1].fixed[0], minutes)
		hw, ok := d.fixed(unitHour, h[2].fixed[0], hours)
		return "at " + mw + " at " + hw + d.everyDay(), ok // H16
	}
	return "", false
}

// stepPlace places a hashed step of seconds or minutes: its range, or the
// hours of every hour it runs in (H2, H3). ok is false for both at once, and
// for an hour step past 6 values.
func (d *describer) stepPlace(unit string, x hashed, r bounds, hour timeField) (string, bool) {
	k := hour.kind()
	switch {
	case k == everyHour && x.whole:
		return "", true
	case k == everyHour:
		return " " + between(unit, x, r), true
	case !x.whole:
		return "", false
	case k == namedHours || k == stepHours:
		return " during " + d.hourNames(hour), true
	case k == longRange && unit == unitMinute:
		return " " + fromThru(hour.items[0], d.onHour, d.onHour), true
	}
	return "", false
}

// hashSecond describes a fixed second (H1, H8, H12): "at a fixed second
// within a minute", "at a fixed second at 9 am every day".
func (d *describer) hashSecond(x hashed, mh hashField, minute timeField, ms hashShape, hour timeField, hs hashShape) (string, bool) {
	sw, _ := d.fixed(unitSecond, x, seconds)
	lead := "at " + sw
	switch {
	case hs != hashNone:
	case ms == hashFixed && hour.kind() == namedHours:
		mw, _ := d.fixed(unitMinute, mh.fixed[0], minutes)
		return lead + " and " + mw + " during " + d.hourNames(hour) + d.everyDay(), true // H12
	case ms != hashNone:
	case minute.every() && hour.every():
		return lead + " within a minute", true
	case minute.every():
		zero := newTimeField("0", unitSecond, unitMinute, seconds)
		return d.joinHours(lead+" within every minute", "", zero, minute, hour), true // C1
	case minute.single() && hour.named():
		clocks := make([]string, len(hour.vals))
		for i, hv := range hour.vals {
			clocks[i] = d.clock(hv, minute.vals[0], 0)
		}
		return lead + " at " + andList(clocks) + d.everyDay(), true // H8
	}
	return "", false
}

// hashMinute describes H in the minute field under plain hours (H1–H7, H15).
func (d *describer) hashMinute(mh hashField, minute timeField, ms hashShape, hour timeField) (string, bool) {
	switch ms {
	case hashFixed:
		mw, _ := d.fixed(unitMinute, mh.fixed[0], minutes)
		lead := "at " + mw
		switch hour.kind() {
		case everyHour:
			return lead + " past the hour", true
		case namedHours:
			return lead + " during " + d.hourNames(hour) + d.everyDay(), true // H7, H9
		case stepHours:
			return lead + " during " + d.hourNames(hour) + d.inEveryDay(), true // R10
		case longStep:
			return lead + " " + d.stepClause(hour), true // R11
		}
		return lead + " past every hour " + fromThru(hour.items[0], d.onHour, d.onHour) + d.everyDay(), true // R9
	case hashStep:
		x := mh.steps[0]
		place, ok := d.stepPlace(unitMinute, x, minutes, hour)
		return runCount(x, minutes, nil) + " in every hour" + place + stepTail(x, unitMinute), ok
	}
	if !hour.every() {
		return "", false
	}
	switch {
	case ms == hashFixedPlus && minute.vals[0] == 0:
		return runCount(mh.fixed[0], minutes, minute.vals) + " in every hour, at the top of the hour and a fixed minute past it", true // H4, H15
	case ms == hashTwoFixed:
		part, _ := d.fixed(unitMinute, mh.fixed[1], minutes)
		return "1 or 2 runs in every hour, at a fixed minute and " + part, true // H6
	case ms == hashStepPlus:
		x := mh.steps[0]
		return runCount(x, minutes, minute.vals) + fmt.Sprintf(" in every hour, at the top of the hour and every %d minutes starting from a fixed minute", x.step), true // H5
	}
	return "", false
}

// hashHour describes H in the hour field under plain minutes (H1, H13, H14,
// H16).
func (d *describer) hashHour(hh hashField, hour timeField, hs hashShape, minute timeField) (string, bool) {
	switch hs {
	case hashFixed:
		hw, ok := d.fixed(unitHour, hh.fixed[0], hours)
		switch {
		case minute.every():
			return "every minute during " + hw, ok
		case minute.single() && minute.vals[0] == 0:
			return "at the top of " + hw + d.everyDay(), ok // R4
		case minute.single():
			return fmt.Sprintf("at minute %d at %s%s", minute.vals[0], hw, d.everyDay()), ok // H16
		}
		return "at minutes " + d.nums(minute) + " at " + hw + d.everyDay(), ok // H16
	case hashFixedPlus:
		if !minute.single() || minute.vals[0] != 0 {
			return "", false
		}
		return runCount(hh.fixed[0], hours, hour.vals) + " in every day, at " + d.clock(hour.vals[0], 0, 0) + " and the top of a fixed hour", true // H13, H15
	case hashStep:
		x := hh.steps[0]
		tail := runCount(x, hours, nil) + " in every day" + stepTail(x, unitHour)
		switch {
		case !x.whole:
		case minute.every():
			lo, hi := runRange(x, hours, nil)
			return "every minute during " + counted(lo, hi, unitHour) + " in every day" + stepTail(x, unitHour), true // H14
		case minute.single() && minute.vals[0] == 0:
			return "at the top of the hour, " + tail, true
		case minute.single():
			return "at " + count(minute.vals[0], unitMinute) + " past the hour, " + tail, true
		}
	}
	return "", false
}

// hashes reports a field with H items that select by the hash.
func (h hashField) hashes() bool {
	return len(h.fixed)+len(h.steps) > 0
}

// splitDates splits the date fields' H items, in the order of the hash
// clause: day of month, day of week, month (H1, G7). plain is each field
// without its H items, or as written when it has none. ok is false for H
// beside L, W or #, which has no form (H20).
func (p Parser) splitDates(fields []string) (h [3]hashField, plain [3]string, ok bool) {
	plain = [3]string{fields[3], fields[5], fields[4]}
	for i, r := range []bounds{dom, p.dowBounds(), months} {
		if !containsHashExpr(plain[i : i+1]) {
			continue
		}
		if !describable(plain[i], hashItemSyntax) {
			return h, plain, false
		}
		h[i] = splitHash(plain[i], r)
		plain[i] = h[i].plain
	}
	return h, plain, true
}

// describeDates describes the day of month, day of week and month, the
// fields in that order with their H items in h (H18, H19), and the
// short-month clause. ok is false for the combinations without a form (H20).
func (p Parser) describeDates(d *describer, h [3]hashField, fields [3]string) (day, month, short string, ok bool) {
	day, short, ok = p.describeDays(d, fields[0], fields[1], h[0], h[1])
	if !h[2].hashes() {
		return day, d.describeMonths(fields[2]), short, ok
	}
	runs := func(x hashed) (int, int) { return runRange(x, months, nil) }
	month, mok := hashDate(h[2], newMonthField(fields[2]), "a fixed month", "", runs)
	return day, "in " + month, short, ok && mok
}

// hashDay describes a day field with H items as hashDate does. alone is
// false beside a restricted other day field, where a fixed day joins as D5
// does and a step has no form: its count would not count the runs (H20).
func hashDay(h hashField, f timeField, noun, the string, runs func(hashed) (int, int), alone bool) (string, bool) {
	if !alone && len(h.steps) > 0 {
		return "", false
	}
	return hashDate(h, f, noun, the, runs)
}

// hashDom describes a hashed day of month as a day clause, with the
// short-month clause of its plain days (H18, H19, D3).
func hashDom(h hashField, md timeField, alone bool) (text, short string, ok bool) {
	text, ok = hashDay(h, md, "a fixed day of the month", "the ", dayRange, alone)
	return "on " + text, lateClause(md.vals), ok
}

// hashDow describes a hashed day of week, counted on the parser's seven-day
// cycle (H18, H19).
func (p Parser) hashDow(h hashField, wd timeField, alone bool) (string, bool) {
	runs := func(x hashed) (int, int) { return runRange(x, p.dowBounds(), nil) }
	return hashDay(h, wd, "a fixed day of the week", "", runs, alone)
}

// hashDate describes a date field with H items, f its plain items: "a fixed
// day of the month between the 1st and 15th", "the 1st and a fixed day of
// the month" (H18), "2 to 4 days in every month, 10 days apart, starting
// from a fixed day" (H19). noun names the fixed value, the is the article
// before ordinals (D2), and runs counts a step's values in its container.
func hashDate(h hashField, f timeField, noun, the string, runs func(hashed) (int, int)) (string, bool) {
	switch h.shape(f) {
	case hashFixed:
		b, ok := dateBetween(h.fixed[0], f, the)
		return noun + b, ok
	case hashFixedPlus:
		return the + f.w(f.vals[0]) + " and " + noun, true
	case hashStep:
		x := h.steps[0]
		b, ok := dateBetween(x, f, the)
		lo, hi := runs(x)
		return counted(lo, hi, f.unit) + " in every " + f.container + b + stepTail(x, f.unit), ok
	}
	return "", false
}

// dateBetween names the range of a hashed date value or step by its values:
// " between Monday and Friday", " between the 1st and 15th" (H3, H18). ok is
// false for a wrapped range, which has no form (H20).
func dateBetween(x hashed, f timeField, the string) (string, bool) {
	switch {
	case x.whole:
		return "", true
	case x.lo > x.hi:
		return "", false
	}
	return " between " + the + f.w(x.lo) + " and " + f.w(x.hi), true
}

// dayRange returns the fewest and most days a hashed day-of-month step runs
// on in a month, over each start the hash can pick and each month length
// (H19): H/10 runs on 9, 19 and 29, or on 2 days in February.
func dayRange(x hashed) (lo, hi int) {
	lo = int(dom.max)
	for o := range x.step {
		for days := dom.max - 3; days <= dom.max; days++ {
			n := 0
			for v := x.lo + o; v <= min(x.hi, days); v += x.step {
				n++
			}
			lo, hi = min(lo, n), max(hi, n)
		}
	}
	return lo, hi
}
