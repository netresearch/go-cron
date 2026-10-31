// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT.

package cron

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
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

// newDescribeConfig applies opts to a zero describeConfig.
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
// "minute 0, hour 0, day of month L, month *, day of week *". A descriptor
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
	if containsHashExpr(fields) || slices.ContainsFunc(fields, noValues) || (yearField != "" && noValues(yearField)) ||
		strings.ContainsAny(fields[3], "LlWw") || strings.ContainsAny(fields[5], "Ll#") {
		return p.describeLiteral(spec, fields, yearField), nil
	}
	day, short := p.describeDays(fields[3], fields[5])
	text, note := describeTime(fields, newDescribeConfig(opts).clock, day != "")
	parts := []string{text}
	for _, clause := range []string{day, describeMonths(fields[4]), describeYears(yearField)} {
		if clause != "" {
			parts = append(parts, clause)
		}
	}
	return strings.Join(parts, " ") + withNote(note) + short, nil
}

// describeDays describes the day of month and day of week, "" when neither
// is restricted. short names the days above the 28th that the day of month
// fires on: "; the 31st runs only in months that have one".
func (p Parser) describeDays(domField, dowField string) (day, short string) {
	domSet, dowSet := !isWildcard(domField), !isWildcard(dowField)
	var domText string
	if domSet {
		domText, short = describeDom(domField)
	}
	var dowWords []string
	if dowSet {
		w := newTimeField(dowField, "day", "week", p.dowBounds(), 12)
		w.word = func(v uint) string { return time.Weekday(v).String() }
		dowWords = w.runWords(w.vals)
	}
	switch {
	case domSet && dowSet && p.dowOrDom(domField, dowField):
		for i, w := range dowWords {
			dowWords[i] = "any " + w
		}
		return "on " + domText + " or on " + andList(dowWords), short
	case domSet && dowSet:
		return "on " + domText + " when it falls on " + andList(dowWords), short
	case domSet:
		return "on " + domText, short
	case dowSet:
		return "on " + andList(dowWords), ""
	}
	return "", ""
}

// describeDom describes the day of month, "the 1st and 15th of the month",
// and the clause naming its days above the 28th.
func describeDom(field string) (text, short string) {
	d := newTimeField(field, "day", "month", dom, 12)
	d.word = ordinal
	text, note := d.nums()
	var late []string
	for _, v := range slices.Sorted(slices.Values(d.vals)) {
		if v > 28 {
			late = append(late, ordinal(v))
		}
	}
	of := " of the month"
	if d.stepped() && len(late) == 0 {
		of = " of every month"
	}
	switch len(late) {
	case 0:
	case 1:
		short = "; the " + late[0] + " runs only in months that have one"
	default:
		short = "; the " + andList(late) + " run only in months that have them"
	}
	return "the " + text + of + withNote(note), short
}

// dowOrDom reports a spec whose restricted day fields are ORed, as the parser
// decides it: DowOrDom, unless StepWildcard keeps a field that starts with
// "*" or "?" unrestricted.
func (p Parser) dowOrDom(domField, dowField string) bool {
	return p.options&DowOrDom != 0 &&
		(p.options&StepWildcard == 0 || wildcardBit(domField)|wildcardBit(dowField) == 0)
}

// describeMonths describes the month field: "in January thru March".
func describeMonths(field string) string {
	if isWildcard(field) {
		return ""
	}
	m := newTimeField(field, "month", "year", months, 12)
	m.word = func(v uint) string { return time.Month(v).String() }
	text, _ := m.nums() // at most 12 values: always listed in full
	return "in " + text
}

// describeYears describes the year field: "in year 2027", "in years 2026
// thru 2028".
func describeYears(field string) string {
	if field == "" || isWildcard(field) {
		return ""
	}
	y := timeField{unit: "year", r: years, limit: 6}
	for _, expr := range strings.FieldsFunc(field, func(r rune) bool { return r == ',' }) {
		// The spec parsed, so the range and step are valid.
		vals, _ := getYearRange(expr)
		step := uint(1)
		if _, s, ok := strings.Cut(expr, "/"); ok {
			step, _ = mustParseInt(s)
		}
		y.items = append(y.items, timeItem{step: step})
		for _, v := range vals {
			y.vals = append(y.vals, uint(v)) // #nosec G115 -- years are positive
		}
	}
	if len(y.items) > 1 {
		slices.Sort(y.vals)
	}
	y.vals = uniq(y.vals)
	text, note := y.nums()
	if len(y.vals) == 1 {
		return "in year " + text
	}
	return "in years " + text + withNote(note)
}

// ordinal writes n as "1st", "2nd", "3rd", "4th", "11th", "21st".
func ordinal(n uint) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(int(n)) + suffix
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
	vals      []uint            // values once each: sorted for a list, else in firing order
	word      func(uint) string // names a value; nil writes the number
}

// w names the value v: "5", "5th", "Friday" or "May".
func (f timeField) w(v uint) string {
	if f.word == nil {
		return strconv.Itoa(int(v))
	}
	return f.word(v)
}

// newTimeField parses the field text, which has already parsed as a spec,
// into its items and the values they fire on, once each.
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
				if r.stepEnd != 0 && it.lo <= r.stepEnd {
					it.hi = r.stepEnd // DowStepToSaturday
				}
			}
		}
		f.items = append(f.items, it)
		f.vals = append(f.vals, it.fires(r)...)
	}
	if len(f.items) > 1 {
		slices.Sort(f.vals) // a list renders the union of its items
	}
	f.vals = uniq(f.vals)
	return f
}

// fires lists the values an item fires on, in firing order, as the parser
// selects them. A day-of-week step walks the seven distinct days (dowCycle),
// and Sunday reads as 0 whether written 0 or 7.
func (it timeItem) fires(r bounds) []uint {
	if !isDowBounds(r) {
		return it.runs(r)
	}
	lo, hi, cr := it.lo, it.hi, r
	if it.step > 1 && lo > hi {
		lo, hi, cr = dowCycle(lo, hi, r)
	}
	vals := timeItem{lo: lo, hi: hi, step: it.step}.runs(cr)
	for i := range vals {
		vals[i] %= 7
	}
	return vals
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

// stepped reports a field with a wildcard or a step greater than 1.
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
		return andList(f.runWords(f.vals)), ""
	}
	words := make([]string, len(f.vals))
	for i, v := range f.vals {
		words[i] = f.w(v)
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
	if first, last := f.vals[0], f.vals[len(f.vals)-1]; f.container != "" && first+f.r.max-f.r.min+1-last != it.step {
		note += ", starting over each " + f.container
	}
	return text, note
}

// runWords names vals as runs of consecutive values: "0 thru 10, 15 and 30".
func (f timeField) runWords(vals []uint) []string {
	var words []string
	for i := 0; i < len(vals); {
		j := i
		for j+1 < len(vals) && vals[j+1] == vals[j]+1 {
			j++
		}
		words = append(words, f.rangeWords(vals[i], vals[j])...)
		i = j + 1
	}
	return words
}

// rangeWords names a range: "5", "5 and 6" when adjacent, or "5 thru 9".
func (f timeField) rangeWords(lo, hi uint) []string {
	l, h := f.w(lo), f.w(hi)
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

// unitWord returns the unit, plural unless the field has one value.
func (f timeField) unitWord() string {
	if len(f.vals) == 1 {
		return f.unit
	}
	return f.unit + "s"
}

// describeTime describes the second, minute and hour fields. named is a
// description with a day clause: clock times then run in ascending order,
// 00:00 is not "midnight", and "every day" drops. The note follows the date
// clauses.
func describeTime(fields []string, form ClockForm, named bool) (text, note string) {
	sec := newTimeField(fields[0], "second", "minute", seconds, 6)
	minute := newTimeField(fields[1], "minute", "hour", minutes, 6)
	hour := newTimeField(fields[2], "hour", "day", hours, 12)
	if len(sec.vals) == 1 {
		if text, note, ok := describeClocks(sec.vals[0], minute, hour, form, named); ok {
			return text, note
		}
	}
	return describePerUnit(sec, minute, hour, named)
}

// describeClocks describes a fixed second s as clock times, or as the span
// form for several minutes over one hour range. ok is false for other specs.
func describeClocks(s uint, minute, hour timeField, form ClockForm, named bool) (text, note string, ok bool) {
	daily := " every day"
	if named {
		daily = ""
	}
	// One hour with up to 6 minutes, or one minute over up to 12 hours
	// written as a list or range: clock times. Hour steps (*/2) keep
	// "past hours 0, 2, …".
	hourList := !slices.ContainsFunc(hour.items, func(it timeItem) bool { return it.star })
	if (len(hour.vals) == 1 && len(minute.vals) <= minute.limit) ||
		(len(minute.vals) == 1 && hourList && len(hour.vals) <= hour.limit) {
		hs, ms := hour.vals, minute.vals
		if named { // ascending within the named day
			hs, ms = slices.Sorted(slices.Values(hs)), slices.Sorted(slices.Values(ms))
		}
		clocks := make([]string, 0, len(hs)*len(ms))
		for _, h := range hs {
			for _, m := range ms {
				clocks = append(clocks, clockTime(h, m, s, form, named))
			}
		}
		return "at " + andList(clocks) + daily, "", true
	}
	if isSpan(s, minute, hour) {
		it := hour.items[0]
		text, note := minute.nums()
		return "from " + clockTime(it.lo, slices.Min(minute.vals), 0, form, named) +
			" thru " + clockTime(it.hi, slices.Max(minute.vals), 0, form, named) +
			daily + ", at " + text + " minutes past each hour", note, true
	}
	return "", "", false
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
func describePerUnit(sec, minute, hour timeField, named bool) (text, note string) {
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
		return "on the hour", ""
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
	if !named && steppedHours(ps[len(ps)-1]) {
		out += " in every day"
	}
	return out, strings.Join(notes, "; ")
}

// steppedHours reports an hour field that steps short of every hour.
func steppedHours(f timeField) bool {
	return f.unit == "hour" && f.stepped() && !f.every()
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

// appendNote appends note to notes unless it is empty.
func appendNote(notes []string, note string) []string {
	if note == "" {
		return notes
	}
	return append(notes, note)
}

// withNote returns note in parentheses after a space, or "" for no note.
func withNote(note string) string {
	if note == "" {
		return ""
	}
	return " (" + note + ")"
}

// clockTime writes h:m:s in the clock form. The word forms write 00:00 as
// "midnight", unless a day is named (named), and 12:00 as "noon".
func clockTime(h, m, s uint, form ClockForm, named bool) string {
	switch {
	case form != Clock24 && !named && h == 0 && m == 0 && s == 0:
		return "midnight"
	case form != Clock24 && h == 12 && m == 0 && s == 0:
		return "noon"
	case form == Clock12:
		return clock12(h, m, s)
	}
	t := fmt.Sprintf("%02d:%02d", h, m)
	if s != 0 {
		t += fmt.Sprintf(":%02d", s)
	}
	return t
}

// clock12 writes h:m:s as a 12-hour time: "2 am", "5:56 pm", "2:30:15 am".
func clock12(h, m, s uint) string {
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

// andList joins words as "a, b and c".
func andList(words []string) string {
	if len(words) == 1 {
		return words[0]
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}
