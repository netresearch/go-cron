// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT.

package cron

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

type describeConfig struct {
	parse []ParseOption
}

// DescribeOption configures DescribeSpec.
type DescribeOption func(*describeConfig)

// WithParseOptions sets the parse options the package-level DescribeSpec
// parses with, as the options of AnalyzeSpec do; several calls combine.
// Parser.DescribeSpec ignores it and uses the parser's own settings.
func WithParseOptions(o ParseOption) DescribeOption {
	return func(c *describeConfig) {
		if len(c.parse) == 0 {
			c.parse = []ParseOption{0}
		}
		c.parse[0] |= o
	}
}

// newDescribeConfig applies opts to a zero describeConfig.
func newDescribeConfig(opts []DescribeOption) describeConfig {
	var c describeConfig
	for _, o := range opts {
		o(&c)
	}
	return c
}

// DescribeSpec returns an English description of spec, such as "at 2:05 am,
// 2:20 am, 2:35 am, and 2:50 am every day", or the parse error. It parses as
// AnalyzeSpec does: with the standard parser unless WithParseOptions is
// given. See Parser.DescribeSpec.
func DescribeSpec(spec string, opts ...DescribeOption) (string, error) {
	return getParserForOptions(newDescribeConfig(opts).parse).DescribeSpec(spec, opts...)
}

// DescribeSpec returns an English description of spec as p parses it, or the
// parse error. The description follows the grammar in docs/DESCRIBE.md and
// leaves out the time zone; callers show the location beside it. A valid spec
// always gets a description and a nil error. Specs outside the fields covered so far (see docs/DESCRIBE.md) get
// each written field by name, as written: "minute 0, hour 9, day of month *,
// month *, day of week 1-5". A descriptor such as "@daily" is returned as
// written. H needs no hash key here: the description does not depend on it.
func (p Parser) DescribeSpec(spec string, _ ...DescribeOption) (string, error) {
	if p.hashKey == "" {
		p.hashKey = "describe"
	}
	if _, err := p.Parse(spec); err != nil {
		return "", err
	}
	// The spec parsed, so the split cannot fail.
	_, descriptor, fields, yearField, _ := p.specFields(spec)
	if descriptor != "" {
		return descriptor, nil
	}
	if !containsHashExpr(fields) && isWildcard(fields[3]) && isWildcard(fields[4]) &&
		isWildcard(fields[5]) && (yearField == "" || isWildcard(yearField)) {
		return describeTime(fields), nil
	}
	return p.describeLiteral(spec, fields, yearField), nil
}

// isWildcard reports a field that selects every value and, like "*", keeps
// the parser's star bit ("*/1" included).
func isWildcard(field string) bool {
	switch field {
	case "*", "?", "*/1", "?/1":
		return true
	}
	return false
}

var literalNames = []string{seconds.name, minutes.name, hours.name, dom.name, months.name, dow.name}

// describeLiteral names each field spec wrote, as written. fields and
// yearField are spec's fields from specFields, which fill in the fields
// spec left out.
func (p Parser) describeLiteral(spec string, fields []string, yearField string) string {
	// The spec parsed, so neither step can fail.
	_, rest, _ := parseTimezone(spec)
	tokens := strings.Fields(rest)
	written, _, _ := p.handleYearExtraction(tokens)
	opts, _, _ := processOptionalFlags(p.options)
	optionalLeftOut := len(written) < countConfiguredFields(opts)

	parts := make([]string, 0, len(fields)+1)
	for i, place := range places {
		leftOut := optionalLeftOut && (place == Second && p.options&SecondOptional != 0 ||
			place == Dow && p.options&DowOptional != 0)
		if opts&place != 0 && !leftOut {
			parts = append(parts, literalNames[i]+" "+fields[i])
		}
	}
	if len(tokens) > len(written) {
		parts = append(parts, "year "+yearField)
	}
	return strings.Join(parts, ", ")
}

// Phrases the descriptions repeat.
const (
	thruSep          = " thru "
	everyDaySuffix   = " every day"
	inEveryDaySuffix = " in every day"
)

// valueLimit is how many second or minute values are listed before a step
// is cut short (T1).
const valueLimit = 6

// timeItem is one comma-separated element of a second, minute or hour field.
type timeItem struct {
	lo, hi, step uint
	star         bool // "*" or "?", alone or with a step
	ranged       bool // "lo-hi", alone or with a step
}

// timeField is a second, minute or hour field.
type timeField struct {
	unit      string // "second", "minute" or "hour"
	container string // the unit a step starts over in
	r         bounds
	items     []timeItem
	vals      []uint // values once each, sorted
}

// newTimeField parses the field text, which has already parsed as a spec,
// into its items and the values they fire on, once each, sorted: a wrapped
// range is shorthand for two ranges, so it reads in clock order (T6, T9).
func newTimeField(text, unit, container string, r bounds) timeField {
	f := timeField{unit: unit, container: container, r: r}
	for _, expr := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' }) {
		rangeAndStep := strings.Split(expr, "/")
		lowAndHigh := strings.Split(rangeAndStep[0], "-")
		// The spec parsed, so the bounds and the step are valid.
		lo, hi, extra, _ := parseRangeBounds(expr, lowAndHigh, r)
		it := timeItem{lo: lo, hi: hi, step: 1, star: extra != 0, ranged: len(lowAndHigh) == 2}
		if len(rangeAndStep) == 2 {
			it.step, _ = mustParseInt(rangeAndStep[1])
			if len(lowAndHigh) == 1 && !it.star {
				it.hi, it.ranged = r.max, true // "N/step" means "N-max/step"
			}
		}
		f.items = append(f.items, it)
		f.vals = append(f.vals, it.runs(r)...)
	}
	slices.Sort(f.vals)
	f.vals = uniq(f.vals)
	return f
}

// parts splits the item's range into the ranges it covers in clock order: a
// wrapped range is shorthand for two (T9).
func (it timeItem) parts(r bounds) [][2]uint {
	if it.lo <= it.hi {
		return [][2]uint{{it.lo, it.hi}}
	}
	return [][2]uint{{r.min, it.hi}, {it.lo, r.max}}
}

// fromThru names an hour range by its parts (T9): "from 9 am thru 5 pm",
// "from midnight thru 5 am and 8 pm thru 11 pm". start and end write a
// part's first and last hour.
func fromThru(it timeItem, start, end func(h uint) string) string {
	parts := it.parts(hours)
	words := make([]string, 0, len(parts))
	for _, p := range parts {
		words = append(words, start(p[0])+thruSep+end(p[1]))
	}
	return "from " + andList(words)
}

// onHour writes an hour as its clock time on the hour.
func onHour(h uint) string {
	return clockTime(h, 0, 0)
}

// runs lists the values an item fires on, in firing order.
func (it timeItem) runs(r bounds) []uint {
	end, size := it.hi, r.max-r.min+1
	if it.lo > it.hi {
		end += size
	}
	var out []uint
	for v := it.lo; v <= end; v += it.step {
		out = append(out, (v-r.min)%size+r.min)
	}
	return out
}

// uniq drops repeated values, keeping the first of each in order.
func uniq(vals []uint) []uint {
	var out []uint
	for _, v := range vals {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// every reports a field that fires on every value ("*", "*/1", "0-59").
func (f timeField) every() bool {
	return len(f.vals) == int(f.r.max-f.r.min+1)
}

// single reports a field with one value.
func (f timeField) single() bool {
	return len(f.vals) == 1
}

// star reports a field with a "*" or "?" item.
func (f timeField) star() bool {
	return slices.ContainsFunc(f.items, func(it timeItem) bool { return it.star })
}

// span reports one hour range item with no step, as "9-17" or "5/1".
func (f timeField) span() (timeItem, bool) {
	if len(f.items) != 1 {
		return timeItem{}, false
	}
	it := f.items[0]
	return it, it.ranged && !it.star && it.step == 1
}

// gap returns the even gap between sorted values, when there are at least
// three and the gap is more than 1: the step that describes them (T2).
func (f timeField) gap() (uint, bool) {
	if len(f.vals) < 3 {
		return 0, false
	}
	d := f.vals[1] - f.vals[0]
	for i := 2; i < len(f.vals); i++ {
		if f.vals[i]-f.vals[i-1] != d {
			return 0, false
		}
	}
	return d, d > 1
}

// nums renders second or minute values ("0, 15, 30, and 45", "0 thru 10")
// and, for evenly spaced values past the limit, the first 3, "…", the last
// and an interval note (T1, T2). Other values are listed in full.
func (f timeField) nums() (text, note string) {
	d, even := f.gap()
	if !even || len(f.vals) <= valueLimit {
		return andList(runWords(f.vals)), ""
	}
	first, last := f.vals[0], f.vals[len(f.vals)-1]
	text = fmt.Sprintf("%d, %d, %d, … %d", first, f.vals[1], f.vals[2], last)
	note = fmt.Sprintf("every %d %ss", d, f.unit)
	if first+f.r.max-f.r.min+1-last != d {
		note += ", starting over each " + f.container
	}
	return text, note
}

// runWords names vals as runs of consecutive values: "0 thru 10", "15", "30".
func runWords(vals []uint) []string {
	var words []string
	for i := 0; i < len(vals); {
		j := i
		for j+1 < len(vals) && vals[j+1] == vals[j]+1 {
			j++
		}
		words = append(words, rangeWords(vals[i], vals[j])...)
		i = j + 1
	}
	return words
}

// rangeWords names a range: "5", "5 and 6" when adjacent, or "5 thru 9".
func rangeWords(lo, hi uint) []string {
	l, h := strconv.Itoa(int(lo)), strconv.Itoa(int(hi))
	switch hi {
	case lo:
		return []string{l}
	case lo + 1:
		return []string{l, h}
	}
	return []string{l + thruSep + h}
}

// named reports hours that read by name ("the 9 am and 5 pm hours"): one
// hour, hours written as a list, or a range of up to 6 hours (R3, T4).
func (f timeField) named() bool {
	return f.single() || !f.star() && (len(f.items) > 1 || len(f.vals) <= valueLimit)
}

// hourNames names the hours: "the 9 am hour", "the 9 am and 5 pm hours", and
// with no article before a list that starts with midnight, "midnight, 6 am,
// noon, and 6 pm hours" (R3).
func (f timeField) hourNames() string {
	clocks := make([]string, len(f.vals))
	for i, h := range f.vals {
		clocks[i] = onHour(h)
	}
	switch {
	case len(clocks) == 1:
		return "the " + clocks[0] + " hour"
	case clocks[0] == "midnight":
		return andList(clocks) + " hours"
	}
	return "the " + andList(clocks) + " hours"
}

// count writes a number with its unit, singular for 1: "1 minute", "5 seconds".
func count(n uint, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// hourKind sorts an hour field by how it joins what comes before it.
type hourKind int

const (
	everyHour  hourKind = iota // every hour
	namedHours                 // one hour, a list, or a range or range step of up to 6 (R3)
	stepHours                  // a star step of up to 6 values, or a list with one (R10)
	longRange                  // one range past 6 values (R9, R12)
	longStep                   // one step past 6 values (R8, R11)
)

// kind returns the hour field's kind.
func (f timeField) kind() hourKind {
	switch {
	case f.every():
		return everyHour
	case f.named():
		return namedHours
	case len(f.items) == 1 && f.items[0].step > 1 && len(f.vals) > valueLimit:
		return longStep
	case f.star():
		return stepHours
	}
	return longRange
}

// describeTime describes the second, minute and hour fields of a spec whose
// date fields are all wildcards: what leads, the minutes joined to it, then
// the hours (C1).
func describeTime(fields []string) string {
	sec := newTimeField(fields[0], "second", "minute", seconds)
	minute := newTimeField(fields[1], "minute", "hour", minutes)
	hour := newTimeField(fields[2], "hour", "day", hours)
	if sec.single() {
		if out, ok := clockTimes(sec.vals[0], minute, hour); ok {
			return out
		}
		if out, ok := hourStep(sec.vals[0], minute, hour); ok {
			return out
		}
	}
	var n notes
	out := n.describe(sec, minute, hour)
	if len(n) == 0 {
		return out
	}
	return out + " (" + strings.Join(n, "; ") + ")"
}

// notes collects the interval notes of the fields a description cuts short.
type notes []string

// nums renders f's values and keeps its note.
func (n *notes) nums(f timeField) string {
	text, note := f.nums()
	if note != "" {
		*n = append(*n, note)
	}
	return text
}

// values names minutes after "within" or "for": "minute 30", "minutes 0 and 30".
func (n *notes) values(f timeField) string {
	if f.single() {
		return fmt.Sprintf("%s %d", f.unit, f.vals[0])
	}
	return f.unit + "s " + n.nums(f)
}

// describe describes the fields by what leads: minutes at second 0, or the
// seconds.
func (n *notes) describe(sec, minute, hour timeField) string {
	switch {
	case !sec.single() || sec.vals[0] != 0:
		return n.bySeconds(sec, minute, hour)
	case minute.single():
		m := minute.vals[0]
		return onTheHour(minuteAt(m), hour, func(h uint) string { return clockTime(h, m, 0) })
	case minute.every():
		return joinHours("every minute", "", sec, minute, hour)
	}
	return joinHours("at minutes "+n.nums(minute), "past the hour", sec, minute, hour)
}

// bySeconds describes fields led by seconds other than 0 (C1): the minutes
// join with "within", or with "for" after "every second".
func (n *notes) bySeconds(sec, minute, hour timeField) string {
	if sec.every() {
		if minute.every() {
			return joinHours("every second", "", sec, minute, hour)
		}
		return joinHours("every second for "+n.values(minute), "in every hour", sec, minute, hour)
	}
	secs := "at " + count(sec.vals[0], "second")
	if !sec.single() {
		secs = "at seconds " + n.nums(sec)
	}
	switch {
	case minute.every() && hour.every():
		return secs + " within a minute"
	case minute.every():
		return joinHours(secs+" within every minute", "", sec, minute, hour)
	case minute.single() && minute.vals[0] == 0:
		var at func(h uint) string
		if sec.single() {
			at = func(h uint) string { return clockTime(h, 0, sec.vals[0]) }
		}
		return onTheHour(secs+" past the top of", hour, at)
	}
	return joinHours(secs+" within "+n.values(minute), "in every hour", sec, minute, hour)
}

// minuteAt opens one minute on the hour forms: "at the top of" (R4), "at 30
// minutes past" (R5).
func minuteAt(m uint) string {
	if m == 0 {
		return "at the top of"
	}
	return "at " + count(m, "minute") + " past"
}

// onTheHour joins the hours to one minute, any minute, as lead opens it
// (R4, R5, R9, R10): "at the top of every hour", "at 30 minutes past every
// hour from 9 am thru 5 pm every day".
//
// at writes the run in hour h when the lead fixes one minute and one
// second; a part of one hour of a wrapped range then reads "at" that run
// (R9, T9): "from midnight thru 5 am and at 11:30 pm". Otherwise at is nil.
func onTheHour(lead string, hour timeField, at func(h uint) string) string {
	switch hour.kind() {
	case everyHour:
		if strings.HasSuffix(lead, " past") {
			return lead + " the hour"
		}
		return lead + " every hour"
	case namedHours:
		return lead + " " + hour.hourNames()
	case stepHours:
		return lead + " " + hour.hourNames() + inEveryDaySuffix
	case longStep:
		return lead + " " + stepClause(hour)
	}
	it := hour.items[0]
	if p := it.parts(hours); at != nil && len(p) == 2 {
		switch {
		case p[1][0] == p[1][1]:
			return lead + " every hour from " + onHour(p[0][0]) + thruSep + onHour(p[0][1]) + " and at " + at(p[1][0]) + everyDaySuffix
		case p[0][0] == p[0][1]:
			return "at " + at(p[0][0]) + " and " + lead + " every hour from " + onHour(p[1][0]) + thruSep + onHour(p[1][1]) + everyDaySuffix
		}
	}
	return lead + " every hour " + fromThru(it, onHour, onHour) + everyDaySuffix
}

// joinHours joins the hours to inner, the seconds and minutes (R6, R9–R12,
// T5). every is the join for every hour.
func joinHours(inner, every string, sec, minute, hour timeField) string {
	if it, ok := hour.span(); ok && !hour.every() && len(minute.vals) > 1 && !minute.every() &&
		(it.lo < it.hi || hour.kind() == longRange) {
		// T5: the first and last run of the day lead.
		first := func(h uint) string { return clockTime(h, minute.vals[0], sec.vals[0]) }
		last := func(h uint) string {
			return clockTime(h, minute.vals[len(minute.vals)-1], sec.vals[len(sec.vals)-1])
		}
		return fromThru(it, first, last) + " every day, " + inner + " past each hour"
	}
	switch hour.kind() {
	case everyHour:
		if every == "" {
			return inner
		}
		return inner + " " + every
	case namedHours:
		return inner + " during " + hour.hourNames()
	case stepHours:
		return inner + " during " + hour.hourNames() + inEveryDaySuffix
	case longStep:
		return inner + " " + stepClause(hour)
	}
	it := hour.items[0]
	if minute.every() {
		// R12: the first and last run.
		var s uint
		if sec.every() {
			s = 59
		}
		last := func(h uint) string { return clockTime(h, 59, s) }
		return inner + " " + fromThru(it, onHour, last) + everyDaySuffix
	}
	return inner + " in every hour " + fromThru(it, onHour, onHour) + everyDaySuffix // R9
}

// wholeDay reports an hour step over the whole day that divides 24 (R8, R11).
func wholeDay(it timeItem) bool {
	return it.lo == hours.min && it.hi == hours.max && 24%it.step == 0
}

// stepClause names an hour step past 6 values by its hours (R11): "every 2
// hours starting with the midnight hour in every day".
func stepClause(hour timeField) string {
	it := hour.items[0]
	out := fmt.Sprintf("every %d hours ", it.step)
	if wholeDay(it) {
		return out + "starting with the " + onHour(it.lo) + " hour in every day"
	}
	// T9: the parts, the first range after "from"; a part of one run is
	// "during" its hour.
	parts := stepParts(it, hour.r)
	words := make([]string, 0, len(parts))
	for i, p := range parts {
		if p[0] == p[1] {
			words = append(words, "during the "+onHour(p[0])+" hour")
			continue
		}
		w := "the " + onHour(p[0]) + " hour thru the " + onHour(p[1]) + " hour"
		if i == 0 || parts[0][0] == parts[0][1] {
			w = "from " + w
		}
		words = append(words, w)
	}
	return out + andList(words) + inEveryDaySuffix
}

// stepParts returns the first and last run of an hour step. A wrapped step
// splits at midnight into its parts in clock order, each from its first run
// to its last (T9).
func stepParts(it timeItem, r bounds) [][2]uint {
	runs := it.runs(r)
	if it.lo <= it.hi {
		return [][2]uint{{runs[0], runs[len(runs)-1]}}
	}
	var am, pm []uint
	for _, v := range runs {
		if v < it.lo {
			am = append(am, v)
		} else {
			pm = append(pm, v)
		}
	}
	parts := make([][2]uint, 0, 2)
	for _, half := range [][]uint{am, pm} {
		if len(half) > 0 {
			parts = append(parts, [2]uint{half[0], half[len(half)-1]})
		}
	}
	return parts
}

// partWords names the parts of an hour step (R8, T9): "from 9 am thru 5 pm",
// "from midnight thru 10 am and 8 pm thru 10 pm", and a part of one run at
// its time: "from midnight thru 10 am and at 10 pm", "at midnight and from 4
// am thru 10 pm". at writes an hour.
func partWords(parts [][2]uint, at func(h uint) string) string {
	if len(parts) == 2 && parts[0][0] == parts[0][1] {
		return "at " + at(parts[0][0]) + " and from " + at(parts[1][0]) + thruSep + at(parts[1][1])
	}
	words := make([]string, 0, len(parts))
	for _, p := range parts {
		if p[0] == p[1] {
			words = append(words, "at "+at(p[0]))
			continue
		}
		words = append(words, at(p[0])+thruSep+at(p[1]))
	}
	return "from " + andList(words)
}

// clockTimes describes a fixed second as clock times: one hour with up to 6
// minutes, or one minute on named hours (T4, R7).
func clockTimes(s uint, minute, hour timeField) (string, bool) {
	oneHour := hour.single() && len(minute.vals) <= valueLimit
	oneMinute := minute.single() && hour.named()
	if !oneHour && !oneMinute {
		return "", false
	}
	clocks := make([]string, 0, len(hour.vals)*len(minute.vals))
	for _, h := range hour.vals {
		for _, m := range minute.vals {
			clocks = append(clocks, clockTime(h, m, s))
		}
	}
	return "at " + andList(clocks) + everyDaySuffix, true
}

// hourStep describes one minute on an hour step past 6 values (R8): the
// start, with the minute folded in, and the end unless the step covers the
// whole day and divides 24.
func hourStep(s uint, minute, hour timeField) (string, bool) {
	if !minute.single() || hour.kind() != longStep {
		return "", false
	}
	it, m := hour.items[0], minute.vals[0]
	out := fmt.Sprintf("every %d hours ", it.step)
	if wholeDay(it) {
		out += "starting at " + clockTime(it.lo, m, s)
	} else {
		at := func(h uint) string { return clockTime(h, m, s) }
		out += partWords(stepParts(it, hour.r), at)
	}
	return out + inEveryDaySuffix, true
}

// clockTime writes h:m:s on the 12-hour clock: "midnight", "noon", "2 am",
// "12:30 am", "2:30:15 pm" (R1, R2, T12).
func clockTime(h, m, s uint) string {
	switch {
	case m == 0 && s == 0 && h == 0:
		return "midnight"
	case m == 0 && s == 0 && h == 12:
		return "noon"
	}
	t, suffix := strconv.Itoa(int((h+11)%12+1)), " am"
	if h >= 12 {
		suffix = " pm"
	}
	if m != 0 || s != 0 {
		t += fmt.Sprintf(":%02d", m)
	}
	if s != 0 {
		t += fmt.Sprintf(":%02d", s)
	}
	return t + suffix
}

// andList joins words as "a", "a and b", or "a, b, and c" (G8).
func andList(words []string) string {
	switch len(words) {
	case 1:
		return words[0]
	case 2:
		return words[0] + " and " + words[1]
	}
	return strings.Join(words[:len(words)-1], ", ") + ", and " + words[len(words)-1]
}
