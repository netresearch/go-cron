// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT.

package cron

import (
	"fmt"
	mathbits "math/bits"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
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
	written string   // the field as written; the parser reads its first character (D5)
	plain   string   // the plain items, H/1 and H(n-n) among them, comma-separated
	fixed   []hashed // H items of one value, the whole range first
	steps   []hashed // H items with a step
}

// writtenOr returns the field as written when it has H items, otherwise field.
func (h hashField) writtenOr(field string) string {
	if h.written != "" {
		return h.written
	}
	return field
}

// splitHash splits field as the parser reads its H items (H10): H/1 is every
// value of its range, like */1, and a one-value range is that value, so both
// join the plain items; a step at or past the range size selects one value.
// A field whose plain items select every value drops its H items (T11).
func splitHash(field string, r bounds) hashField {
	h := hashField{written: field}
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
		// The whole range when it covers every value, wrapped or not
		// (H10): on the day of week's seven-day cycle, 0-6 and 1-0 too.
		clo, chi, cr := dowCycle(x.lo, x.hi, r)
		x.whole = cyclicRangeSize(clo, chi, cr) == cr.max-cr.min+1
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

// hashReach returns the bits of every value field's H items can pick under
// any key.
func hashReach(field string, r bounds) (bits uint64) {
	for it := range strings.SplitSeq(field, ",") {
		x, isHash := readHash(it, r)
		if !isHash {
			continue
		}
		v := x.lo
		for range cyclicRangeSize(x.lo, x.hi, r) {
			bits |= 1 << v
			v++
			if v > r.max {
				v = r.min
			}
		}
	}
	return bits
}

// maxHashWalk bounds the hash values anyPick walks, and with MaxSpecLength its
// cost. H over the date fields' whole ranges repeats within 5,208; a step far
// past its range, or many H items of different sizes, can pass the bound.
const maxHashWalk = 1 << 13

// hashPeriod returns how many hash values H item it takes to repeat, as the
// parser reads the hash: a step takes hash%step and, when that falls past the
// range, hash/step%size; a single value hash%size and, for a second Sunday in
// the day of the week, hash/size%(size-1).
func hashPeriod(it string, r bounds) uint64 {
	x, _ := readHash(it, r)
	if _, step, hasStep := strings.Cut(it, "/"); hasStep {
		n, _ := mustParseInt(step) // The spec parsed, so the step is valid.
		size := uint64(cyclicRangeSize(dowCycle(x.lo, x.hi, r)))
		switch {
		case uint64(n) <= size:
			return uint64(n)
		case uint64(n) > maxHashWalk:
			return maxHashWalk + 1
		}
		return uint64(n) * size
	}
	size := uint64(cyclicRangeSize(x.lo, x.hi, r))
	if isDowBounds(r) && size > 1 {
		return size * (size - 1)
	}
	return size
}

// hashDateField is one date field of a spec, split for the hash walk.
type hashDateField struct {
	bits  *uint64 // where the field's pick goes
	plain uint64  // the bits of its plain items
	r     bounds
	items []string // its H items
	field string
}

// hashDateFields splits the day of month, month and day of week in fields, with
// pick to take what each H picks.
func (p Parser) hashDateFields(pick, s *SpecSchedule, fields []string) []hashDateField {
	domPlain, domItems := splitItems(fields[3], hashItemSyntax)
	monthPlain, monthItems := splitItems(fields[4], hashItemSyntax)
	dowPlain, dowItems := splitItems(fields[5], hashItemSyntax)
	// The spec parsed, so the only error here is an empty list, which is no values.
	domBits, _, _ := p.parseDomField(strings.Join(domPlain, ","), false)
	monthBits, _ := getField(strings.Join(monthPlain, ","), p.fieldBounds(months))
	dowBits, _, _ := p.parseDowField(strings.Join(dowPlain, ","), false)
	return []hashDateField{
		{&pick.Dom, domBits | s.Dom&starBit, p.fieldBounds(dom), domItems, fields[3]},
		{&pick.Month, monthBits, p.fieldBounds(months), monthItems, fields[4]},
		{&pick.Dow, dowBits | s.Dow&starBit, p.dowBounds(), dowItems, fields[5]},
	}
}

// hashWalkPeriod returns how many hash values date's H items take to repeat
// together, or false past maxHashWalk.
func hashWalkPeriod(date []hashDateField) (uint64, bool) {
	period := uint64(1)
	for _, f := range date {
		for _, it := range f.items {
			n := hashPeriod(it, f.r)
			period = period / gcd(period, n) * n
			if period > maxHashWalk {
				return 0, false
			}
		}
	}
	return period, true
}

// at returns the field's bits under hash value h.
func (f hashDateField) at(h uint64) uint64 {
	all := f.plain
	for _, it := range f.items {
		bits, _ := hashExprBits(it, f.r, h) // The spec parsed.
		all |= bits
	}
	return all
}

// anyPick reports whether try holds for s with its date fields as some key
// picks them. Every H in a spec takes its value from one hash, so the picks
// move together (G11): this walks the hash values, not each H on its own. Past
// maxHashWalk it tries once with every value each H can pick.
func (p Parser) anyPick(s *SpecSchedule, fields []string, try func(*SpecSchedule) bool) bool {
	pick := *s
	date := p.hashDateFields(&pick, s, fields)
	period, ok := hashWalkPeriod(date)
	if !ok {
		for _, f := range date {
			*f.bits = f.plain | hashReach(f.field, f.r)
		}
		pick.Dow = NormalizeDOW(pick.Dow) // a reach ending in 7 is Sunday
		return try(&pick)
	}
	seen := make(map[[3]uint64]bool)
	for h := range period {
		for _, f := range date {
			*f.bits = f.at(h)
		}
		key := [3]uint64{pick.Dom, pick.Month, pick.Dow}
		if !seen[key] && try(&pick) {
			return true
		}
		seen[key] = true
	}
	return false
}

// hashShape sorts a field by its H items.
type hashShape int

const (
	hashNone      hashShape = iota // no H item selects by the hash
	hashFixed                      // one fixed value, nothing else
	hashFixedPlus                  // one fixed value of the whole range and one plain value (H15)
	hashTwoFixed                   // two fixed values, nothing else (H6, H21)
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
	case len(h.fixed) == 2 && len(f.vals) == 0:
		return hashTwoFixed
	}
	return hashOther
}

// fixedPair names the two fixed values of h, the whole range first, then by
// their ranges, and counts their runs (H6, H21): "1 or 2 runs" when the two
// can fall on one value, "2 runs" when they cannot. Both values come from one
// hash, so the count enumerates the hash over the lengths of both ranges, as
// the parser picks them.
func (d *describer) fixedPair(h hashField, unit string, r bounds) (count, words string, ok bool) {
	a, b := h.fixed[0], h.fixed[1]
	if !a.whole && b.lo < a.lo {
		a, b = b, a
	}
	na, nb := uint64(cyclicRangeSize(a.lo, a.hi, r)), uint64(cyclicRangeSize(b.lo, b.hi, r))
	lo, hi := 2, 1
	for v := range na * nb / gcd(na, nb) {
		n := 1
		if getHashSingleBit(a.lo, a.hi, r, v) != getHashSingleBit(b.lo, b.hi, r, v) {
			n = 2
		}
		lo, hi = min(lo, n), max(hi, n)
	}
	aw, okA := d.fixed(unit, a, r)
	bw, okB := d.fixed(unit, b, r)
	return counted(lo, hi, "run"), aw + " and " + bw, okA && okB
}

// gcd is the greatest common divisor of a and b.
func gcd(a, b uint64) uint64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// between names the range of a hashed value or step: "between minutes 0 and
// 29", "between minutes 0 and 10 or 50 and 59", "between minutes 0 and 5 or
// 59" (H3, T9). A wrapped step reads from its start, where its walk starts:
// "between minutes 50 and 10". ok is false for a wrapped range of one value
// per part.
func between(unit string, x hashed, r bounds) (string, bool) {
	if x.step > 1 && x.lo > x.hi {
		return fmt.Sprintf("between %ss %d and %d", unit, x.lo, x.hi), true
	}
	parts, ok := rangeParts(x, r, number, "", " and ")
	return "between " + unit + "s " + parts, ok
}

// number writes v as a number.
func number(v uint) string { return strconv.FormatUint(uint64(v), 10) }

// rangeParts writes a hashed range as its parts, low to high (T9), with the
// prefix before each part and the joiner between its ends: "0 and 10 or 50
// and 59", "the 1st and 5th or the 31st", "1 to 5 or 31"; a part of one value
// reads as that value. ok is false for a wrapped range whose parts are each
// one value (H17, H20).
func rangeParts(x hashed, r bounds, word func(uint) string, the, and string) (string, bool) {
	parts := timeItem{lo: x.lo, hi: x.hi}.parts(r)
	words := make([]string, len(parts))
	ones := 0
	for i, p := range parts {
		words[i] = the + word(p[0])
		if p[0] == p[1] {
			ones++
			continue
		}
		words[i] += and + word(p[1])
	}
	return strings.Join(words, " or "), ones < 2
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
		b, ok := between(unit, x, r)
		return w + " " + b, ok
	case x.lo > x.hi:
		return "", false
	}
	return w + betweenSep + d.hourNames(timeField{vals: []uint{x.lo, x.hi}}), true
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
	var words []string
	for i := 0; i < len(units); i++ {
		if i+1 < len(units) && units[i] == units[i+1] {
			words = append(words, "both "+units[i]+"s")
			i++
			continue
		}
		words = append(words, "the "+units[i])
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

// onesOnly reports a hashed second or minute range that wraps into parts of
// one value each, which has no form (H17).
func onesOnly(h [3]hashField) bool {
	for i, r := range []bounds{seconds, minutes} {
		for _, x := range append(slices.Clone(h[i].fixed), h[i].steps...) {
			if _, ok := between("", x, r); !ok {
				return true
			}
		}
	}
	return false
}

// describeHashTime describes second, minute and hour fields with H items
// (H1–H16, H21–H24). ok is false for the combinations without a form, which
// render field by field (G10).
func (d *describer) describeHashTime(h [3]hashField) (string, bool) {
	sec := newTimeField(h[0].plain, unitSecond, unitMinute, seconds)
	minute := newTimeField(h[1].plain, unitMinute, unitHour, minutes)
	hour := newTimeField(h[2].plain, unitHour, "day", hours)
	ss, ms, hs := h[0].shape(sec), h[1].shape(minute), h[2].shape(hour)
	switch {
	case onesOnly(h):
		return "", false
	case ss != hashNone:
		return d.hashSeconds(h, ss, minute, ms, hour, hs)
	case !sec.single() || sec.vals[0] != 0:
		return "", false
	case hs == hashNone:
		return d.hashMinute(h[1], minute, ms, hour)
	case ms == hashNone:
		return d.hashHour(h[2], hour, hs, minute)
	}
	return d.hashMinuteHour(h, ms, hs)
}

// hashSeconds describes H in the second field (H1, H2, H8, H12, H21, H24).
func (d *describer) hashSeconds(h [3]hashField, ss hashShape, minute timeField, ms hashShape, hour timeField, hs hashShape) (string, bool) {
	plain := ms == hashNone && hs == hashNone
	switch {
	case ss == hashFixed:
		return d.hashSecond(h[0].fixed[0], h[1], minute, ms, h[2], hour, hs)
	case ss == hashTwoFixed && plain && minute.every() && hour.every():
		runs, words, ok := d.fixedPair(h[0], unitSecond, seconds)
		return runs + " in every minute, at " + words, ok // H21
	case ss != hashStep || !plain:
		return "", false
	}
	x := h[0].steps[0]
	switch {
	case minute.every():
		place, ok := d.stepPlace(unitSecond, x, seconds, hour)
		return runCount(x, seconds, nil) + " in every minute" + place + stepTail(x, unitSecond), ok
	case minute.single() && x.whole:
		return d.joinSeconds(runCount(x, seconds, nil), nil, timeField{}, minute, hour) + stepTail(x, unitSecond), true // H24
	}
	return "", false
}

// hashMinuteHour describes H in both the minute and the hour field (H16,
// H22).
func (d *describer) hashMinuteHour(h [3]hashField, ms, hs hashShape) (string, bool) {
	switch {
	case ms == hashFixed && hs == hashFixed:
		mw, _ := d.fixed(unitMinute, h[1].fixed[0], minutes)
		hw, ok := d.fixed(unitHour, h[2].fixed[0], hours)
		return "at " + mw + " at " + hw + d.everyDay(), ok // H16
	case ms == hashFixed && hs == hashStep && h[2].steps[0].whole:
		x := h[2].steps[0]
		mw, ok := d.fixed(unitMinute, h[1].fixed[0], minutes)
		lo, hi := runRange(x, hours, nil)
		return "at " + mw + duringSep + counted(lo, hi, unitHour) + inEveryDaySuffix + stepTail(x, unitHour), ok // H22
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
		b, ok := between(unit, x, r)
		return " " + b, ok
	case !x.whole:
		return "", false
	case k == namedHours || k == stepHours:
		return duringSep + d.hourNames(hour), true
	case k == longRange && unit == unitMinute:
		it := hour.items[0]
		if p := it.parts(hours); len(p) == 2 { // T9, R11: a part of one hour
			switch {
			case p[1][0] == p[1][1]:
				return " from " + d.partRange(p[0]) + " and during the " + d.onHour(p[1][0]) + " hour", true
			case p[0][0] == p[0][1]:
				return " during the " + d.onHour(p[0][0]) + " hour and from " + d.partRange(p[1]), true
			}
		}
		return " " + fromThru(it, d.onHour, d.onHour), true
	case k == longRange:
		// R12: seconds run in every minute, so the last run is minute 59.
		last := func(h uint) string { return d.clock(h, 59, 0) }
		return " " + fromThru(hour.items[0], d.onHour, last), true // H24
	}
	return "", false
}

// hashSecond describes a fixed second (H1, H8, H12, H24): "at a fixed second
// within a minute", "at a fixed second at 9 am every day", "at a fixed
// second within minute 30 in every hour".
func (d *describer) hashSecond(x hashed, mh hashField, minute timeField, ms hashShape, hh hashField, hour timeField, hs hashShape) (string, bool) {
	sw, _ := d.fixed(unitSecond, x, seconds)
	lead := "at " + sw
	switch {
	case hs == hashFixed && ms == hashNone && minute.single() && minute.vals[0] == 0:
		hw, ok := d.fixed(unitHour, hh.fixed[0], hours)
		return lead + " at the top of " + hw + d.everyDay(), ok // H24, H16
	case hs != hashNone:
		return "", false
	case ms == hashFixed && hour.kind() == namedHours:
		mw, _ := d.fixed(unitMinute, mh.fixed[0], minutes)
		return lead + " and " + mw + duringSep + d.hourNames(hour) + d.everyDay(), true // H12
	case ms == hashFixed:
		mw, _ := d.fixed(unitMinute, mh.fixed[0], minutes)
		return d.joinHours(lead+" within "+mw, "in every hour", timeField{}, minute, hour), true // H24
	case ms != hashNone:
		return "", false
	}
	return d.secondPlain(lead, minute, hour)
}

// secondPlain joins plain minutes and hours to a fixed second (C1, H8,
// H24). ok is false for the span form (T5), whose runs are clock times.
func (d *describer) secondPlain(lead string, minute, hour timeField) (string, bool) {
	_, span := spanForm(minute, hour)
	switch {
	case minute.every() && hour.every():
		return lead + " within a minute", true
	case minute.every():
		zero := newTimeField("0", unitSecond, unitMinute, seconds)
		return d.joinHours(lead+" within every minute", "", zero, minute, hour), true // C1
	case minute.single() && hour.kind() == namedHours:
		clocks := make([]string, len(hour.vals))
		for i, hv := range hour.vals {
			clocks[i] = d.clock(hv, minute.vals[0], 0)
		}
		return lead + " at " + andList(clocks) + d.everyDay(), true // H8
	case !span:
		return d.joinSeconds(lead, nil, timeField{}, minute, hour), true // H24
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
			return lead + duringSep + d.hourNames(hour) + d.everyDay(), true // H7, H9
		case stepHours:
			return lead + duringSep + d.hourNames(hour) + d.inEveryDay(), true // R10
		case longStep:
			return lead + " " + d.stepClause(hour), true // R11
		}
		return lead + " past " + d.everyHourParts(hour.items[0]) + d.everyDay(), true // R9
	case hashStep:
		x := mh.steps[0]
		if hour.kind() == longStep && x.whole {
			return runCount(x, minutes, nil) + inEveryHourSuffix + stepTail(x, unitMinute) + ", " + d.stepClause(hour), true // H23
		}
		place, ok := d.stepPlace(unitMinute, x, minutes, hour)
		return runCount(x, minutes, nil) + inEveryHourSuffix + place + stepTail(x, unitMinute), ok
	case hashTwoFixed:
		place, ok := d.stepPlace(unitMinute, hashed{whole: true}, minutes, hour)
		runs, words, okWords := d.fixedPair(mh, unitMinute, minutes)
		return runs + inEveryHourSuffix + place + ", at " + words, ok && okWords // H6, H21
	}
	if !hour.every() {
		return "", false
	}
	switch {
	case ms == hashFixedPlus && minute.vals[0] == 0:
		return runCount(mh.fixed[0], minutes, minute.vals) + " in every hour, at the top of the hour and a fixed minute past it", true // H4, H15
	case ms == hashStepPlus:
		x := mh.steps[0]
		return runCount(x, minutes, minute.vals) + fmt.Sprintf(" in every hour, at the top of the hour and every %d minutes starting from a fixed minute", x.step), true // H5
	}
	return "", false
}

// hashHour describes H in the hour field under plain minutes (H1, H13, H14,
// H16, H21, H22).
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
	case hashTwoFixed:
		if !minute.single() || minute.vals[0] != 0 {
			return "", false
		}
		runs, words, ok := d.fixedPair(hh, unitHour, hours)
		return runs + " in every day, at the top of " + words, ok // H21
	case hashStep:
		return d.hashHourStep(hh.steps[0], minute)
	}
	return "", false
}

// hashHourStep describes a hashed hour step under plain minutes (H2, H14, H22).
// ok is false for a ranged step, which has no form (H17).
func (d *describer) hashHourStep(x hashed, minute timeField) (string, bool) {
	tail := runCount(x, hours, nil) + inEveryDaySuffix + stepTail(x, unitHour)
	lo, hi := runRange(x, hours, nil)
	during := duringSep + counted(lo, hi, unitHour) + inEveryDaySuffix + stepTail(x, unitHour)
	switch {
	case !x.whole:
		return "", false
	case minute.every():
		return "every minute" + during, true // H14
	case minute.single() && minute.vals[0] == 0:
		return "at the top of the hour, " + tail, true
	case minute.single():
		return "at " + count(minute.vals[0], unitMinute) + " past the hour, " + tail, true
	}
	return "at minutes " + d.nums(minute) + during, true // H22
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
func hashDom(h hashField, md timeField, runs func(hashed) (int, int), alone bool) (text, short string, ok bool) {
	text, ok = hashDay(h, md, "a fixed day of the month", "the ", runs, alone)
	return "on " + text, lateClause(md.vals), ok
}

// hashDow describes a hashed day of week, counted on the parser's seven-day
// cycle (H18, H19).
func (p Parser) hashDow(h hashField, wd timeField, alone, or bool) (string, bool) {
	if or && len(wd.vals) > 0 { // X4: a plain day beside the hashed one takes "any"
		word := wd.w
		wd.word = func(v uint) string { return "any " + word(v) }
	}
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
// " between Monday and Friday", " between the 1st and 15th" (H3, H18). A
// wrapped fixed day of the month splits low to high: " between the 1st and
// 5th or the 25th and 31st"; a wrapped step of days, week or year reads in its
// order: " between the 25th and 5th", " between Friday and Monday", " between
// November and February". ok is false for a wrapped day of the month of one
// value per part (H20).
func dateBetween(x hashed, f timeField, the string) (string, bool) {
	switch {
	case x.whole:
		return "", true
	case x.lo > x.hi && f.container == unitMonth && x.step <= 1:
		parts, ok := rangeParts(x, f.r, f.w, the, " and ")
		return betweenSep + parts, ok
	}
	return betweenSep + the + f.w(x.lo) + " and " + f.w(x.hi), true
}

// monthDays lists the lengths of the months a hashed day step can run in
// (H19): those of the written months and years, with hashedMonths the months a
// hashed month can pick.
func monthDays(sch Schedule, hashedMonths uint64) []uint {
	s, ok := sch.(*SpecSchedule)
	if !ok {
		return []uint{28, 29, 30, 31}
	}
	var days []uint
	for m := uint(time.January); m <= uint(time.December); m++ {
		if (s.Month|hashedMonths)&(1<<m) != 0 {
			days = append(days, monthLengths(s, m)...)
		}
	}
	slices.Sort(days)
	return slices.Compact(days)
}

// monthLengths lists the lengths of month m. February is both 28 and 29 days
// unless the years decide.
func monthLengths(s *SpecSchedule, m uint) []uint {
	switch {
	case m != uint(time.February):
		return []uint{maxDaysInMonth[m]}
	case len(s.Year) == 0:
		return []uint{28, 29}
	}
	var days []uint
	for y := range s.Year {
		days = append(days, uint(time.Date(y, time.March, 0, 0, 0, 0, 0, time.UTC).Day())) // #nosec G115 -- a day of the month
	}
	return days
}

// dayRuns returns the count of a hashed day step's days in a month (H19). One
// hash picks the day and a hashed month (G11), so it counts the pairs they can
// make, by walking the hash values. Past maxHashWalk it counts each on its own.
func (p Parser) dayRuns(sch Schedule, fields []string) func(hashed) (int, int) {
	lengths := monthDays(sch, hashReach(fields[4], p.fieldBounds(months)))
	alone := func(x hashed) (int, int) { return dayRange(x, lengths) }
	s, ok := sch.(*SpecSchedule)
	if !ok || !splitHash(fields[4], p.fieldBounds(months)).hashes() {
		return alone
	}
	return func(x hashed) (int, int) {
		pick := *s
		date := p.hashDateFields(&pick, s, fields)
		period, ok := hashWalkPeriod(date)
		if !ok {
			return alone(x)
		}
		return walkDayRange(s, date[0], date[1], period)
	}
}

// walkDayRange returns the fewest and most days the hashed day runs on in a
// month the hashed month picks, over period hash values.
func walkDayRange(s *SpecSchedule, day, month hashDateField, period uint64) (lo, hi int) {
	lo = int(dom.max)
	for h := range period {
		days, picked := day.at(h), month.at(h)
		for m := uint(time.January); m <= uint(time.December); m++ {
			if picked&(1<<m) == 0 {
				continue
			}
			for _, n := range monthLengths(s, m) {
				c := mathbits.OnesCount64(days & (1<<(n+1) - 2)) // the days 1 to n
				lo, hi = min(lo, c), max(hi, c)
			}
		}
	}
	return lo, hi
}

// dayRange returns the fewest and most days a hashed day-of-month step runs
// on in a month, over each start the hash can pick and each of the month
// lengths (H19): H/10 runs on 9, 19 and 29, or on 2 days in February.
func dayRange(x hashed, lengths []uint) (lo, hi int) {
	size, span := cyclicRangeSize(x.lo, x.hi, dom), dom.max-dom.min+1
	lo = int(dom.max)
	count := func(vals []uint) {
		for _, days := range lengths {
			n := 0
			for _, v := range vals {
				if v <= days {
					n++
				}
			}
			lo, hi = min(lo, n), max(hi, n)
		}
	}
	// Each start the hash can pick, as runRange; a wrapped range steps on
	// past the 31st into the 1st, whatever the month's length.
	for o := range x.step {
		if o < size {
			start := (x.lo+o-dom.min)%span + dom.min
			count(timeItem{lo: start, hi: x.hi, step: x.step}.runs(dom))
			continue
		}
		for v := range size {
			count([]uint{(x.lo+v-dom.min)%span + dom.min})
		}
	}
	return lo, hi
}
