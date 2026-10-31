// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT.

package cron

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// ClockForm selects how DescribeSpec writes clock times.
type ClockForm int

const (
	// Clock24 writes 24-hour times with hours 00-23, as RFC 3339 does:
	// "at 22:00, 00:00 and 02:00 every day". It is the default.
	Clock24 ClockForm = iota
	// Clock24Words writes 24-hour times with "midnight" and "noon":
	// "at 22:00, midnight and 02:00 every day".
	Clock24Words
	// Clock12 writes 12-hour times with "midnight" and "noon":
	// "at 10 pm, midnight and 2 am every day".
	Clock12
)

type describeConfig struct {
	clock ClockForm
	parse []ParseOption
}

// DescribeOption configures DescribeSpec.
type DescribeOption func(*describeConfig)

// WithClockForm sets how clock times are written. The default is Clock24,
// which an unknown form also gets.
func WithClockForm(f ClockForm) DescribeOption {
	if f < Clock24 || f > Clock12 {
		f = Clock24
	}
	return func(c *describeConfig) { c.clock = f }
}

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

func newDescribeConfig(opts []DescribeOption) describeConfig {
	var c describeConfig
	for _, o := range opts {
		o(&c)
	}
	return c
}

// DescribeSpec returns an English description of spec, such as "at 02:05,
// 02:20, 02:35 and 02:50 every day", or the parse error. It parses as
// AnalyzeSpec does: with the standard parser unless WithParseOptions is
// given. See Parser.DescribeSpec.
func DescribeSpec(spec string, opts ...DescribeOption) (string, error) {
	return getParserForOptions(newDescribeConfig(opts).parse).DescribeSpec(spec, opts...)
}

// DescribeSpec returns an English description of spec as p parses it, or the
// parse error. The description follows the grammar in docs/DESCRIBE.md and
// leaves out the time zone; callers show the location beside it. A valid spec
// always gets a description and a nil error. Specs outside the fields covered
// so far (see docs/DESCRIBE.md) get each written field by name, as written:
// "minute 0, hour 9, day of month *, month *, day of week 1-5". A descriptor
// such as "@daily" is returned as written. H needs no hash key here: the
// description does not depend on it.
func (p Parser) DescribeSpec(spec string, opts ...DescribeOption) (string, error) {
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
	if containsHashExpr(fields) || slices.ContainsFunc(fields[:3], noValues) ||
		!isWildcard(fields[3]) || !isWildcard(fields[4]) ||
		!isWildcard(fields[5]) || (yearField != "" && !isWildcard(yearField)) {
		return p.describeLiteral(spec, fields, yearField), nil
	}
	return describeTime(fields, newDescribeConfig(opts).clock), nil
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

// noValues reports a field written as commas only, which never fires.
func noValues(field string) bool {
	return strings.Trim(field, ",") == ""
}

var literalNames = []string{"second", "minute", "hour", "day of month", "month", "day of week"}

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
	limit     int // values listed before the list is cut short
	items     []timeItem
	vals      []uint // values once each: sorted for a list, else in firing order
}

func newTimeField(text, unit, container string, r bounds, limit int) timeField {
	f := timeField{unit: unit, container: container, r: r, limit: limit}
	for _, expr := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' }) {
		rangeAndStep := strings.Split(expr, "/")
		lowAndHigh := strings.Split(rangeAndStep[0], "-")
		// The spec parsed, so the bounds and the step are valid.
		lo, hi, extra, _ := parseRangeBounds(lowAndHigh, r)
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
	if len(f.items) > 1 {
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

func uniq(vals []uint) []uint {
	var out []uint
	for _, v := range vals {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

func (f timeField) stepped() bool {
	return slices.ContainsFunc(f.items, func(it timeItem) bool { return it.star || it.step > 1 })
}

// every reports a field that fires on every value ("*", "*/1", "0-59").
func (f timeField) every() bool {
	return len(f.vals) == int(f.r.max-f.r.min+1)
}

// nums renders the field's values ("0, 15, 30 and 45", "9 thru 17") and, for
// a step whose list is cut short, its interval note. Only a single step is
// cut short: its note restores the values left out.
func (f timeField) nums() (text, note string) {
	if !f.stepped() || len(f.items) > 1 {
		// A wrapped range reads as its two parts: "50 thru 59 and 0 thru 10".
		return andList(runWords(f.vals)), ""
	}
	words := make([]string, len(f.vals))
	for i, v := range f.vals {
		words[i] = strconv.Itoa(int(v))
	}
	// A wrapped step lists in full: its gap is not at the boundary, so the
	// "starting over" note cannot describe it.
	if len(words) <= f.limit || (len(f.items) == 1 && f.items[0].lo > f.items[0].hi) {
		return andList(words), ""
	}
	text = strings.Join(words[:3], ", ") + ", … " + words[len(words)-1]
	it := f.items[0]
	note = "every " + f.unit
	if it.step > 1 {
		note = fmt.Sprintf("every %d %ss", it.step, f.unit)
	}
	if first, last := f.vals[0], f.vals[len(f.vals)-1]; first+f.r.max-f.r.min+1-last != it.step {
		note += ", starting over each " + f.container
	}
	return text, note
}

// runWords names vals as runs of consecutive values: "0 thru 10, 15 and 30".
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

// phrase renders the field as "minutes 0, 4, 8, … 56" or "every minute".
func (f timeField) phrase() (text, note string) {
	if f.every() {
		return "every " + f.unit, ""
	}
	text, note = f.nums()
	return f.unitWord() + " " + text, note
}

func (f timeField) unitWord() string {
	if len(f.vals) == 1 {
		return f.unit
	}
	return f.unit + "s"
}

// describeTime describes the second, minute and hour fields of a spec whose
// date fields are all wildcards.
func describeTime(fields []string, form ClockForm) string {
	sec := newTimeField(fields[0], "second", "minute", seconds, 6)
	minute := newTimeField(fields[1], "minute", "hour", minutes, 6)
	hour := newTimeField(fields[2], "hour", "day", hours, 12)
	if len(sec.vals) == 1 {
		if out, ok := describeClocks(sec.vals[0], minute, hour, form); ok {
			return out
		}
	}
	return describePerUnit(sec, minute, hour)
}

// describeClocks describes a fixed second s as clock times, or as the span
// form for several minutes over one hour range. ok is false for other specs.
func describeClocks(s uint, minute, hour timeField, form ClockForm) (out string, ok bool) {
	// One hour with up to 6 minutes, or one minute over up to 12 hours
	// written as a list or range: clock times. Hour steps (*/2) keep
	// "past hours 0, 2, …".
	hourList := !slices.ContainsFunc(hour.items, func(it timeItem) bool { return it.star })
	if (len(hour.vals) == 1 && len(minute.vals) <= minute.limit) ||
		(len(minute.vals) == 1 && hourList && len(hour.vals) <= hour.limit) {
		clocks := make([]string, 0, len(hour.vals)*len(minute.vals))
		for _, h := range hour.vals {
			for _, m := range minute.vals {
				clocks = append(clocks, clockTime(h, m, s, form))
			}
		}
		return "at " + andList(clocks) + " every day", true
	}
	if isSpan(s, minute, hour) {
		it := hour.items[0]
		text, note := minute.nums()
		return "from " + clockTime(it.lo, slices.Min(minute.vals), 0, form) +
			" thru " + clockTime(it.hi, slices.Max(minute.vals), 0, form) +
			" every day, at " + text + " minutes past each hour" + withNote(note), true
	}
	return "", false
}

// isSpan reports several minutes over one hour range short of the whole
// day, which the span form describes: the first and last run of the day lead.
func isSpan(s uint, minute, hour timeField) bool {
	it := hour.items[0]
	return s == 0 && len(hour.items) == 1 && it.ranged && it.step == 1 && it.lo < it.hi &&
		!hour.every() && len(minute.vals) > 1 && !minute.every()
}

// describePerUnit describes the time fields finest first: "at 15 seconds past
// minutes 0, 4, 8, … 56 in every hour", "every minute for hour 9".
func describePerUnit(sec, minute, hour timeField) string {
	ps := []timeField{minute, hour}
	if len(sec.vals) != 1 || sec.vals[0] != 0 {
		ps = []timeField{sec, minute, hour}
	}
	// "every minute" already covers every hour.
	for n := len(ps); n > 1 && ps[n-1].every() && ps[n-2].every(); n-- {
		ps = ps[:n-1]
	}
	// "every second" already covers every minute: "every second for hour 9".
	for len(ps) > 1 && ps[0].every() && ps[1].every() {
		ps = slices.Delete(ps, 1, 2)
	}

	if isTheHour(ps) {
		return "on the hour"
	}
	var notes []string
	var out string
	if first := ps[0]; first.every() {
		out = joinPhrases(ps, &notes)
	} else {
		text, note := first.nums()
		notes = appendNote(notes, note)
		unit := first.unit + "s"
		if text == "1" {
			unit = first.unit
		}
		out = "at " + text + " " + unit + " past " + pastClause(ps[1:], &notes)
	}
	if last := ps[len(ps)-1]; last.unit == "hour" && last.stepped() && !last.every() {
		out += " in every day"
	}
	return out + withNote(strings.Join(notes, "; "))
}

// isTheHour reports minute 0 of every hour.
func isTheHour(fs []timeField) bool {
	return len(fs) == 2 && fs[0].unit == "minute" && len(fs[0].vals) == 1 && fs[0].vals[0] == 0 &&
		fs[1].every()
}

// pastClause names what leading values fall past: "the minute", "the hour",
// or the coarser fields joined with "for" or "in".
func pastClause(rest []timeField, notes *[]string) string {
	switch {
	case len(rest) == 1 && rest[0].every():
		return "the " + rest[0].unit
	case isTheHour(rest):
		return "the hour"
	}
	return joinPhrases(rest, notes)
}

// joinPhrases joins the fields' phrases and collects their notes: "for"
// before a value ("for hour 9"), "in" before a recurrence ("in every hour").
func joinPhrases(fs []timeField, notes *[]string) string {
	var out strings.Builder
	out.Grow(150) // the joined phrases fit without regrowing
	for i, f := range fs {
		text, note := f.phrase()
		*notes = appendNote(*notes, note)
		switch {
		case i == 0:
		case f.every():
			out.WriteString(" in ")
		default:
			out.WriteString(" for ")
		}
		out.WriteString(text)
	}
	return out.String()
}

func appendNote(notes []string, note string) []string {
	if note == "" {
		return notes
	}
	return append(notes, note)
}

func withNote(note string) string {
	if note == "" {
		return ""
	}
	return " (" + note + ")"
}

// clockTime writes h:m:s in the clock form. The word forms write 00:00 as
// "midnight" and 12:00 as "noon".
func clockTime(h, m, s uint, form ClockForm) string {
	switch {
	case form != Clock24 && h == 0 && m == 0 && s == 0:
		return "midnight"
	case form != Clock24 && h == 12 && m == 0 && s == 0:
		return "noon"
	case form == Clock12:
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
	t := fmt.Sprintf("%02d:%02d", h, m)
	if s != 0 {
		t += fmt.Sprintf(":%02d", s)
	}
	return t
}

func andList(words []string) string {
	if len(words) == 1 {
		return words[0]
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}
