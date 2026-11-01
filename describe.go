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
	_, descriptor, fields, yearField, err := p.specFields(spec)
	if err != nil {
		return "", err
	}
	if descriptor != "" {
		return descriptor, nil
	}
	if !containsHashExpr(fields) && isWildcard(fields[3]) && isWildcard(fields[4]) &&
		isWildcard(fields[5]) && (yearField == "" || isWildcard(yearField)) {
		if out, ok := describeTime(fields); ok {
			return out, nil
		}
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
	vals      []uint // values once each: sorted, except one hour item in firing order
}

// newTimeField parses the field text, which has already parsed as a spec,
// into its items and the values they fire on, once each. Seconds and
// minutes sort (T9); hours keep the firing order of a single item (T6).
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
	if len(f.items) > 1 || unit != "hour" {
		slices.Sort(f.vals) // a list renders the union of its items
	}
	f.vals = uniq(f.vals)
	return f
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
	return []string{l + " thru " + h}
}

// named reports hours that read by name ("the 9 am and 5 pm hours"): one
// hour, hours written as a list, or a range of up to 6 hours (R3, T4).
func (f timeField) named() bool {
	return f.single() || !f.star() && (len(f.items) > 1 || len(f.vals) <= valueLimit)
}

// hourNames names the hours: "the 9 am hour", "the 9 am and 5 pm hours" (R3).
func (f timeField) hourNames() string {
	clocks := make([]string, len(f.vals))
	for i, h := range f.vals {
		clocks[i] = clockTime(h, 0, 0)
	}
	if len(clocks) == 1 {
		return "the " + clocks[0] + " hour"
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

// describeTime describes the second, minute and hour fields of a spec whose
// date fields are all wildcards. ok is false for a combination the grammar
// has no form for.
func describeTime(fields []string) (out string, ok bool) {
	sec := newTimeField(fields[0], "second", "minute", seconds)
	minute := newTimeField(fields[1], "minute", "hour", minutes)
	hour := newTimeField(fields[2], "hour", "day", hours)
	switch {
	case sec.every():
		return everySecond(minute, hour)
	case !sec.single():
		// R5, R6: several seconds within one minute of every hour.
		if minute.single() && minute.vals[0] != 0 && hour.every() {
			text, note := sec.nums()
			return fmt.Sprintf("at seconds %s within minute %d in every hour", text, minute.vals[0]) + withNote(note), true
		}
		return "", false
	}
	s := sec.vals[0]
	if out, ok := clockTimes(s, minute, hour); ok {
		return out, true
	}
	if out, ok := hourStep(s, minute, hour); ok {
		return out, true
	}
	if s != 0 {
		return fixedSecond(s, minute, hour)
	}
	return byMinute(minute, hour)
}

// everySecond describes a spec that runs every second of its minutes (T3, R6, J1).
func everySecond(minute, hour timeField) (string, bool) {
	switch {
	case minute.every() && hour.every():
		return "every second", true
	case minute.every() && hour.named():
		return "every second during " + hour.hourNames(), true
	case minute.single() && minute.vals[0] != 0 && hour.every():
		return fmt.Sprintf("every second for minute %d in every hour", minute.vals[0]), true
	}
	return "", false
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
	return "at " + andList(clocks) + " every day", true
}

// hourStep describes one minute on an hour step past 6 values (R8): the
// start, with the minute folded in, and the end unless the step covers the
// whole day and divides 24.
func hourStep(s uint, minute, hour timeField) (string, bool) {
	if !minute.single() || len(hour.items) != 1 || hour.items[0].step < 2 || len(hour.vals) <= valueLimit {
		return "", false
	}
	it, m := hour.items[0], minute.vals[0]
	out := fmt.Sprintf("every %d hours ", it.step)
	if it.lo == hours.min && it.hi == hours.max && 24%it.step == 0 {
		out += "starting at " + clockTime(hour.vals[0], m, s)
	} else {
		out += "from " + clockTime(hour.vals[0], m, s) + " thru " + clockTime(hour.vals[len(hour.vals)-1], m, s)
	}
	return out + " in every day", true
}

// fixedSecond describes one second other than 0 in every hour (R4, R5, R6).
func fixedSecond(s uint, minute, hour timeField) (string, bool) {
	if !hour.every() {
		return "", false
	}
	at := "at " + count(s, "second")
	switch {
	case minute.every():
		return at + " within a minute", true
	case minute.single() && minute.vals[0] == 0:
		return at + " past the top of every hour", true
	case !minute.single():
		text, note := minute.nums()
		return at + " within minutes " + text + " in every hour" + withNote(note), true
	}
	return "", false
}

// byMinute describes second 0 by its minutes and hours.
func byMinute(minute, hour timeField) (string, bool) {
	switch {
	case minute.every():
		// T3, R6
		if hour.every() {
			return "every minute", true
		}
		if hour.named() {
			return "every minute during " + hour.hourNames(), true
		}
	case minute.single():
		return oneMinute(minute.vals[0], hour)
	default:
		text, note := minute.nums()
		if it, ok := hour.span(); hour.every() {
			return "at minutes " + text + " past the hour" + withNote(note), true
		} else if ok && it.lo < it.hi {
			// T5: the first and last run of the day lead.
			return "from " + clockTime(it.lo, minute.vals[0], 0) + " thru " +
				clockTime(it.hi, minute.vals[len(minute.vals)-1], 0) +
				" every day, at minutes " + text + " past each hour" + withNote(note), true
		} else if hour.named() {
			return "at minutes " + text + " during " + hour.hourNames() + withNote(note), true
		}
	}
	return "", false
}

// oneMinute describes one minute at second 0 on hours that are not clock
// times: every hour (R4, R5), an hour step of up to 6 values (R3, R4), or an
// hour range past 6 values (R9).
func oneMinute(m uint, hour timeField) (string, bool) {
	switch {
	case hour.every() && m == 0:
		return "at the top of every hour", true
	case hour.every():
		return "at " + count(m, "minute") + " past the hour", true
	case m != 0:
		return "", false
	case len(hour.items) == 1 && hour.items[0].star:
		return "at the top of " + hour.hourNames() + " in every day", true
	}
	if it, ok := hour.span(); ok {
		return "at the top of every hour from " + clockTime(it.lo, 0, 0) + " thru " + clockTime(it.hi, 0, 0) + " every day", true
	}
	return "", false
}

// withNote returns note in parentheses after a space, or "" for no note.
func withNote(note string) string {
	if note == "" {
		return ""
	}
	return " (" + note + ")"
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
