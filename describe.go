// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT.

package cron

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
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
		if o != nil {
			o(&c)
		}
	}
	return c
}

// DescribeSpec returns an English description of spec, such as "at 2:05 am,
// 2:20 am, 2:35 am, and 2:50 am every day", or the parse error. It parses as
// AnalyzeSpec does: with the standard parser unless WithParseOptions is
// given. See Parser.DescribeSpec.
func DescribeSpec(spec string, opts ...DescribeOption) (string, error) {
	p, err := tryParserForOptions(newDescribeConfig(opts).parse)
	if err != nil {
		return "", err
	}
	return p.DescribeSpec(spec, opts...)
}

// DescribeSpecNote returns the note that spells out the values the hash key
// picks in spec's description, or "" when it has none, or the parse error. It
// parses as DescribeSpec does. See Parser.DescribeSpecNote.
func DescribeSpecNote(spec string, opts ...DescribeOption) (string, error) {
	p, err := tryParserForOptions(newDescribeConfig(opts).parse)
	if err != nil {
		return "", err
	}
	return p.DescribeSpecNote(spec, opts...)
}

// DescribeSpec returns an English description of spec as p parses it, or the
// parse error. The description follows the grammar in docs/DESCRIBE.md and
// leaves out the time zone; callers show the location beside it. A valid spec
// always gets a description and a nil error; one that never runs is "never
// runs", within the limit on H in G11 of docs/DESCRIBE.md. Combinations of H
// without a form (H17 and H20 in docs/DESCRIBE.md) get each written field by
// name, as written: "minute 0, hour 0, day of month H/10, month *, day of week
// 1". A descriptor reads as its cron equivalent
// ("@weekly" is "at 12 am on Sunday"), and "@triggered", "@manual" and "@none"
// read "only when triggered". An interval reads as it runs, from its anchor,
// with the parser's minimum: "every 1h30m from when scheduling started",
// "every 15m from Unix epoch". H needs no hash key here: the description does
// not depend on it. With no key, StrictDays does not check a spec with H in
// the day of the month or the month when the value the key picks decides that
// check; when no key finds the day, the parser's error returns.
func (p Parser) DescribeSpec(spec string, _ ...DescribeOption) (string, error) {
	desc, _, err := p.describe(spec)
	return desc, err
}

// DescribeSpecNote returns the note that spells out the values the hash
// key picks in spec's description, as p parses it: "at minutes h, h+15,
// h+30, and h+45 past the hour, where h is a fixed minute from 0 to 14".
// It is "" unless a field has a hashed step or a fixed day of the month. A
// description given field by field still gets its note. See docs/DESCRIBE.md.
func (p Parser) DescribeSpecNote(spec string, _ ...DescribeOption) (string, error) {
	_, note, err := p.describe(spec)
	return note, err
}

// withPlaceholderKey gives a parser with no hash key one, as the description
// does not depend on it. StrictDays is dropped when H is in the day of the
// month or the month, where the result would depend on that key; describe
// restores it when no key finds the day.
func (p Parser) withPlaceholderKey(spec string) Parser {
	if p.hashKey != "" {
		return p
	}
	p.hashKey = "describe"
	if len(spec) > MaxSpecLength { // Parse rejects it, so it need not be split.
		return p
	}
	// A spec that fails to split fails again in Parse.
	if _, _, fields, _, err := p.specFields(spec); err == nil && len(fields) > 4 && containsHashExpr(fields[3:5]) {
		p.options &^= StrictDays
	}
	return p
}

// describe returns the description of spec and its note (N1–N8).
func (p Parser) describe(spec string) (desc, note string, err error) {
	strict := p.options&StrictDays != 0
	p = p.withPlaceholderKey(spec)
	sch, err := p.Parse(spec)
	if err != nil {
		return "", "", err
	}
	// The spec parsed, so the split cannot fail.
	_, descriptor, fields, yearField, _ := p.specFields(spec)
	fields, text := p.expandDescriptor(descriptor, fields)
	if text != "" {
		return text, "", nil
	}
	if err := p.strictDaysError(spec, strict, sch, fields); err != nil {
		return "", "", err
	}
	if !allDescribable(fields) || !describable(yearField) {
		return p.describeLiteral(spec, fields, yearField), "", nil
	}
	if p.dayMissing(sch, fields) {
		return "never runs", "", nil
	}
	var h [3]hashField
	for i, r := range []bounds{seconds, minutes, hours} {
		h[i] = splitHash(fields[i], r)
	}
	hd, dates, ok := p.splitDates(fields)
	units := hashUnits(append(h[:], hd[:]...), []string{unitSecond, unitMinute, unitHour, unitDay, unitDay, unitMonth})
	d := p.dayDescriber(sch, fields)
	day, month, short, dok := p.describeDates(&d, hd, dates)
	if !ok || !dok {
		return p.describeLiteral(spec, fields, yearField), p.hashNote(&d, h, hd, dates), nil
	}
	d.named = day != ""
	var timeText string
	if !slices.ContainsFunc(h[:], hashField.hashes) {
		timeText = d.describeTime([]string{h[0].plain, h[1].plain, h[2].plain})
	} else if t, ok := d.describeHashTime(h); ok {
		timeText = t
	} else {
		return p.describeLiteral(spec, fields, yearField), p.hashNote(&d, h, hd, dates), nil
	}
	parts := []string{timeText}
	for _, clause := range []string{day, month, d.describeYears(yearField)} {
		if clause != "" {
			parts = append(parts, clause)
		}
	}
	desc = placeAlternatives(strings.Join(parts, " ")+d.noteText()+short) + hashKeyClause(units)
	return desc, p.hashNote(&d, h, hd, dates), nil
}

// descriptorFields holds the fields, second to day of week, that each
// calendar descriptor runs as (parseDescriptor).
var descriptorFields = map[string][]string{
	descriptorYearly:   {"0", "0", "0", "1", "1", "*"},
	descriptorAnnually: {"0", "0", "0", "1", "1", "*"},
	descriptorMonthly:  {"0", "0", "0", "1", "*", "*"},
	descriptorWeekly:   {"0", "0", "0", "*", "*", "0"},
	descriptorDaily:    {"0", "0", "0", "*", "*", "*"},
	descriptorMidnight: {"0", "0", "0", "*", "*", "*"},
	descriptorHourly:   {"0", "0", "*", "*", "*", "*"},
}

// expandDescriptor returns the fields a calendar descriptor runs as, or the
// whole description of any other descriptor: "only when triggered" for a
// trigger, the interval for "@every". Without a descriptor it returns fields.
func (p Parser) expandDescriptor(descriptor string, fields []string) (expanded []string, text string) {
	switch descriptor {
	case "":
		return fields, ""
	case descriptorTriggered, descriptorManual, descriptorNone:
		return nil, "only when triggered"
	}
	if f, ok := descriptorFields[descriptor]; ok {
		return f, ""
	}
	return nil, p.describeInterval(descriptor)
}

// describeInterval describes a parsed "@every" descriptor as it runs (P3):
// the interval after the parser's minimum and truncation, from the start or
// the Unix epoch, and how the written duration changed.
func (p Parser) describeInterval(descriptor string) string {
	sch, _ := p.intervalSchedule(descriptor) // the spec parsed
	from := "when scheduling started"
	var every time.Duration
	switch s := sch.(type) {
	case ConstantDelaySchedule:
		every = s.Delay
	case AlignedSchedule:
		every, from = s.Interval, "Unix epoch"
	}
	if every <= 0 {
		every, from = time.Second, "when scheduling started" // Next adds 1s
	}
	_, writtenText, _ := strings.Cut(descriptor, " ")
	written, _ := time.ParseDuration(writtenText)
	text := "every " + durationText(every) + " from " + from
	var how string
	switch {
	case every == written:
		return text
	case p.minEveryInterval > 0 && written < p.minEveryInterval:
		how = "rounded up to the " + durationText(p.minEveryInterval) + " minimum"
	case every > written:
		how = "rounded up to whole seconds"
	default:
		how = "cut to whole seconds"
	}
	return text + " (written as " + writtenText + ", " + how + ")"
}

// durationText is d as Go writes it, without zero minutes and seconds after
// a larger unit: "1h30m", "1h", "1m", "500ms".
func durationText(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// describer builds one description: whether it names a day, and the notes
// of the fields it cuts short.
type describer struct {
	named     bool              // a day clause follows the time (J1)
	dayName   bool              // the day clause names a day of the week (R2, D7)
	notes     []string          // the time fields' notes
	dateNotes []string          // the date fields' notes, after the time's (G7)
	monthDays []uint            // the month lengths a hashed day step can run in (H19)
	dayPairs  map[uint64]uint64 // a hashed day step's days and a hashed month's lengths (H19)
}

// dayMissing reports a schedule whose days the calendar never has (G11): a
// day none of the written months and years has ("0 0 30 2 *", "0 0 29 2 *
// 2027", "0 0 31W 4 *"), or a day that cannot be the counted day of the week
// ("0 0 1 * MON#2"). With H in a date field it asks this of every set of values
// a key can pick, up to maxHashWalk: "0 0 H(30-31) 2 *" never runs for any key,
// while "0 0 H(29-31) 2 *" runs for some. It reads the calendar, not Next, which looks only some
// years ahead: "0 0 * 2 MON#5" next runs in 2044.
func (p Parser) dayMissing(sch Schedule, fields []string) bool {
	s, ok := sch.(*SpecSchedule)
	if !ok || len(s.Year)+len(s.DomConstraints)+len(s.DowConstraints) == 0 {
		return p.noKeyFindsDay(sch, fields)
	}
	years := calendarYears(s.Year)
	return !p.anyPick(s, fields, func(pick *SpecSchedule) bool { return dayInYears(pick, years) })
}

// dayInYears reports whether s matches a day in any of years.
func dayInYears(s *SpecSchedule, years []int) bool {
	for _, y := range years {
		for t := time.Date(y, time.January, 1, 0, 0, 0, 0, time.UTC); t.Year() == y; t = t.AddDate(0, 0, 1) {
			if s.Month&(1<<uint(t.Month())) != 0 && dayMatches(s, t) { // #nosec G115 -- a month, 1 to 12
				return true
			}
		}
	}
	return false
}

// calendarYears returns one year of each kind among the written years, or
// among 28 years when none is written, which hold all 14 kinds: a year's days
// depend only on its first day of the week and whether it is a leap year.
func calendarYears(written map[int]struct{}) []int {
	if len(written) == 0 {
		written = make(map[int]struct{}, 28)
		for y := 2001; y <= 2028; y++ {
			written[y] = struct{}{}
		}
	}
	var years []int
	seen := map[[2]int]bool{}
	for y := range written {
		kind := [2]int{int(time.Date(y, time.January, 1, 0, 0, 0, 0, time.UTC).Weekday()), time.Date(y, time.March, 0, 0, 0, 0, 0, time.UTC).Day()}
		if !seen[kind] {
			seen[kind] = true
			years = append(years, y)
		}
	}
	return years
}

// strictDaysError returns the parser's error when strict was dropped for the
// placeholder key and no key finds the day: every key picks a missing day, so
// the parser rejects any of them.
func (p Parser) strictDaysError(spec string, strict bool, sch Schedule, fields []string) error {
	if !strict || p.options&StrictDays != 0 || !p.noKeyFindsDay(sch, fields) {
		return nil
	}
	p.options |= StrictDays
	_, err := p.Parse(spec)
	return err
}

// noKeyFindsDay reports a day of the month that none of the written months
// has, whatever values a key picks; the parser's check, without the year.
func (p Parser) noKeyFindsDay(sch Schedule, fields []string) bool {
	s, ok := sch.(*SpecSchedule)
	if !ok {
		return false
	}
	return !p.anyPick(s, fields, func(pick *SpecSchedule) bool {
		return !dayNeverMatches(pick.Dom, pick.Month, pick.Dow, pick.DomConstraints, p.options&DowOrDom != 0)
	})
}

// describeDays describes the day of month and day of week, "" when neither
// is restricted (D2, D4, D5, X1–X4), with each field's H items in hd and hw
// (H18). short names the days above the 28th that the day of month fires on:
// "; the 31st runs only in months that have one" (D3, X5), then the days
// before the last day that short months lack (X6). ok is false for H without
// a form (H20).
func (p Parser) describeDays(d *describer, domField, dowField string, hd, hw hashField) (day, short string, ok bool) {
	domPlain, domSpecial := splitItems(domField, domSpecialItem)
	dowPlain, dowNth := splitItems(dowField, dowNthItem)
	md := newDomField(strings.Join(domPlain, ","))
	wd := p.newDowField(strings.Join(dowPlain, ","))
	domSet, dowSet := restricted(domField, hd), restricted(dowField, hw)
	or := domSet && dowSet && p.dowOrDom(hd.writtenOr(domField), hw.writtenOr(dowField))
	domAll, dowAll := md.every(), wd.every()
	if or && (domAll || dowAll) {
		return "", "", !hd.hashes() && !hw.hashes() // T3: one side of the OR is every day
	}
	// T3 comes first: a field that covers its range drops out on its own.
	domSet = domSet && !domAll
	dowSet = dowSet && !dowAll

	ok = true
	var domText string
	switch {
	case hd.hashes():
		domText, short, ok = hashDom(hd, md, d.dayCount, !dowSet)
	case domSet:
		domText, short = d.describeDom(md, domSpecial, !or)
	}
	var days, nth []string
	if dowSet {
		days, nth = wd.runWords(wd.vals), nthDays(dowNth)
	}
	d.dayName = len(days)+len(nth) > 0 // a fixed day is not a named day (D7)
	dowWords := dowItems(days, nth, or)
	if hw.hashes() {
		w, wok := p.hashDow(hw, wd, !domSet, or)
		dowWords, ok = []string{w}, ok && wok
	}
	return dayClause(domText, dowWords, or), short, ok
}

// newDomField is the day of month: ordinals, up to 12 listed (D2).
func newDomField(text string) timeField {
	md := newTimeField(text, unitDay, unitMonth, dom)
	md.word, md.limit = ordinal, 12
	return md
}

// newDowField is the day of week: names, 7 read as Sunday (D4).
func (p Parser) newDowField(text string) timeField {
	wd := newTimeField(text, unitDay, "week", p.dowBounds())
	wd.word = func(v uint) string { return time.Weekday(v % 7).String() }
	return wd
}

// newMonthField is the month: names, up to 12 listed (D6).
func newMonthField(text string) timeField {
	m := newTimeField(text, unitMonth, "year", months)
	m.word, m.limit = func(v uint) string { return time.Month(v).String() }, 12
	return m
}

// restricted reports a day field that names days: H items, or plain items
// that are not the star.
func restricted(field string, h hashField) bool {
	return h.hashes() || !isWildcard(field)
}

// dayClause joins the day of month, a clause with its own "on" or none
// (X2), and the days of the week, either of which may be missing: "when it
// falls on" (D5 AND) or "or on any" (D5 OR).
func dayClause(domText string, dowWords []string, or bool) string {
	switch {
	case or:
		return domText + " or on " + andList(dowWords)
	case domText != "" && dowWords != nil:
		return domText + " when it falls on " + andList(dowWords)
	case domText != "":
		return domText
	case dowWords != nil:
		return "on " + andList(dowWords)
	}
	return ""
}

// dowItems names the days of the week for their place in the clause:
// "Monday and the third Friday", or each with "any" when ORed (X4).
func dowItems(days, nth []string, or bool) []string {
	out := make([]string, 0, len(days)+len(nth))
	for _, w := range days {
		if or {
			w = "any " + w
		}
		out = append(out, w)
	}
	for _, w := range nth {
		if or {
			w = "any " + w
		} else {
			w = "the " + w
		}
		out = append(out, w)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// domSpecialItem is the day-of-month syntax of DomL and DomW: L, LW, L-n and
// nW. dowNthItem is the day-of-week syntax of DowNth and DowLast: a day with
// "#" and its occurrence, or "#L" for the last. Numbers read as strconv.Atoi
// reads them, as the parser does, so "-0" is Sunday.
var (
	domSpecialItem = regexp.MustCompile(`^(?i:L|LW|L-\+?\d+|\+?\d+W)$`)
	dowNthItem     = regexp.MustCompile(`^(?:[+-]?\d+|[A-Za-z]{3})#(?:\+?\d+|[Ll])$`)
)

// splitItems splits field into its plain items and those that special
// matches, each in written order.
func splitItems(field string, special *regexp.Regexp) (plain, matched []string) {
	for _, it := range strings.FieldsFunc(field, func(r rune) bool { return r == ',' }) {
		if special.MatchString(it) {
			matched = append(matched, it)
		} else {
			plain = append(plain, it)
		}
	}
	return plain, matched
}

var nthNames = []string{"", "first", "second", "third", "fourth", "fifth"}

// nthDays names "#" items, "third Friday" or "last Friday", once each, in
// written order (X1).
func nthDays(items []string) []string {
	var out []string
	for _, it := range items {
		day, n, _ := strings.Cut(it, "#")
		// The spec parsed, so the day and the occurrence are valid.
		v, _ := parseIntOrName(day, dow.names)
		word := "last"
		if !strings.EqualFold(n, "L") {
			i, _ := mustParseInt(n)
			word = nthNames[i]
		}
		if w := word + " " + time.Weekday(v%7).String(); !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}

// domPart is one element of a day-of-month list and the day it sorts by.
type domPart struct {
	key    uint
	text   string
	before bool // "N days before the last day" (X2)
}

// describeDom describes the day of month as a clause: "on the 1st and 15th
// of the month" (D2), "on the last day of the month", "3 days before the
// last day of the month" (X1–X3); and the short-month clauses (D3, X5, X6).
// whole is false when the day of week is ORed with it.
func (d *describer) describeDom(md timeField, special []string, whole bool) (text, short string) {
	if len(special) == 0 {
		text = d.dateNums(md)
		of := ofTheMonth
		if _, even := md.gap(); even && !slices.ContainsFunc(md.vals, func(v uint) bool { return v > 28 }) {
			of = " of every month"
		}
		return "on the " + text + of, lateClause(md.vals)
	}
	parts, lacking, late := d.domParts(md, special)
	words := make([]string, len(parts))
	for i, p := range parts {
		words[i] = p.text
	}
	switch {
	case len(parts) == 1 && strings.HasPrefix(parts[0].text, "the weekday nearest"):
		text = "on " + parts[0].text
	case parts[0].before:
		text = andList(words) + ofTheMonth
	default:
		text = "on " + andList(words) + ofTheMonth
	}
	clauses := []string{lateClause(late)}
	for _, p := range lacking {
		subject := p.text
		if whole && len(parts) == 1 {
			subject = "it"
		}
		clauses = append(clauses, "; "+subject+" runs only in "+monthsOf(p.key))
	}
	return text, strings.Join(clauses, "")
}

// domParts lists the day-of-month items in the order of the days they name,
// once each (X3), the L-n items short months lack, keyed by the fewest days
// they need (X6), and the days the items name, for the short-month clause
// (D3, X5).
func (d *describer) domParts(md timeField, special []string) (parts, lacking []domPart, late []uint) {
	late = slices.Clone(md.vals)
	var last bool
	for _, it := range special {
		u := strings.ToUpper(it)
		// The spec parsed, so the numbers are valid.
		switch {
		case u == "L":
			last = true
		case u == "LW":
			parts = append(parts, domPart{key: 32, text: "the last weekday"})
		case strings.HasPrefix(u, "L-"):
			n, _ := mustParseInt(u[2:])
			text := count(n, unitDay) + " before the last day"
			parts = append(parts, domPart{key: 33 + n, text: text, before: true})
			if n >= 28 {
				lacking = append(lacking, domPart{key: n + 1, text: text})
			}
		default:
			n, _ := mustParseInt(strings.TrimSuffix(u, "W"))
			parts = append(parts, domPart{key: n, text: "the weekday nearest the " + ordinal(n)})
			late = append(late, n)
		}
	}
	// X3: the ordinals, with the last day, share one "the".
	if len(md.vals) > 0 {
		words := md.runWords(md.vals)
		if t, note := md.nums(); note != "" {
			// T1: cut short, so the last day keeps its own "the".
			words = []string{t}
			d.dateNotes = append(d.dateNotes, note)
		} else if last {
			words, last = append(words, "last day"), false
		}
		parts = append(parts, domPart{key: slices.Min(md.vals), text: "the " + andList(words)})
	}
	if last {
		parts = append(parts, domPart{key: 32, text: "the last day"})
	}
	// Sorted, a repeated item sits next to itself.
	byKey := func(a, b domPart) int { return cmp.Compare(a.key, b.key) }
	sameText := func(a, b domPart) bool { return a.text == b.text }
	slices.SortStableFunc(parts, byKey)
	slices.SortStableFunc(lacking, byKey)
	return slices.CompactFunc(parts, sameText), slices.CompactFunc(lacking, sameText), late
}

// lateClause names the days above the 28th: "; the 31st runs only in months
// that have one" (D3, X5).
func lateClause(days []uint) string {
	return lateClauseAfter(days, 28)
}

// lateClauseAfter names the days past shortest, the most days every month in
// question has (D3, N5).
func lateClauseAfter(days []uint, shortest uint) string {
	var late []string
	for _, v := range slices.Compact(slices.Sorted(slices.Values(days))) {
		if v > shortest {
			late = append(late, ordinal(v))
		}
	}
	switch len(late) {
	case 0:
		return ""
	case 1:
		return "; the " + late[0] + " runs only in months that have one"
	}
	return "; the " + andList(late) + " run only in months that have them"
}

// monthsOf names the months of at least days days: "months of 29 days or
// more", "months of 31 days" (X6).
func monthsOf(days uint) string {
	if days == 31 {
		return "months of 31 days"
	}
	return fmt.Sprintf("months of %d days or more", days)
}

// dowOrDom reports a spec whose restricted day fields are ORed, as the parser
// decides it: DowOrDom, unless StepWildcard finds a field that starts with
// "*" or "?", which keeps both fields ANDed.
func (p Parser) dowOrDom(domField, dowField string) bool {
	return p.options&DowOrDom != 0 &&
		(p.options&StepWildcard == 0 || wildcardBit(domField)|wildcardBit(dowField) == 0)
}

// describeMonths describes the month field: "in January thru March" (D6).
func (d *describer) describeMonths(field string) string {
	m := newMonthField(field)
	if isWildcard(field) || m.every() {
		return ""
	}
	return "in " + d.dateNums(m)
}

// describeYears describes the year field: "in year 2027", "in years 2026
// thru 2028" (D6).
func (d *describer) describeYears(field string) string {
	if field == "" || isWildcard(field) {
		return ""
	}
	y := timeField{unit: "year", r: years}
	for _, expr := range strings.FieldsFunc(field, func(r rune) bool { return r == ',' }) {
		// The spec parsed, so the range and step are valid.
		vals, _ := getYearRange(expr)
		y.items = append(y.items, timeItem{})
		for _, v := range vals {
			y.vals = append(y.vals, uint(v)) // #nosec G115 -- years are positive
		}
	}
	slices.Sort(y.vals)
	y.vals = slices.Compact(y.vals)
	if y.single() {
		return fmt.Sprintf("in year %d", y.vals[0])
	}
	return "in years " + d.dateNums(y)
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
// the parser's star bit: one of its items is the star ("*/1" included).
// The star selects every value, so the other items add nothing ("*,5" is
// "*"), and with the bit set the day fields AND under DowOrDom.
func isWildcard(field string) bool {
	for item := range strings.SplitSeq(field, ",") {
		lo, step, hasStep := strings.Cut(item, "/")
		n, err := strconv.Atoi(step)
		if (lo == "*" || lo == "?") && (!hasStep || err == nil && n == 1) {
			return true
		}
	}
	return false
}

// describableItem is the item syntax the description covers: "*", "?", a
// number (with an optional "+", as strconv.Atoi reads it) or a three-letter
// name, or a range of them, each with an optional step. Any other item renders the spec field by field (G10).
var describableItem = regexp.MustCompile(`^(?:[*?]|(?:\+?[0-9]+|[A-Za-z]{3})(?:-(?:\+?[0-9]+|[A-Za-z]{3}))?)(?:/\+?[0-9]+)?$`)

// describable reports a field whose items all have the covered syntax, or
// one of the field's own (special): H, L and W in the day of month, # in the
// day of week.
func describable(field string, special ...*regexp.Regexp) bool {
	items := strings.FieldsFunc(field, func(r rune) bool { return r == ',' })
	return !slices.ContainsFunc(items, func(it string) bool {
		return !describableItem.MatchString(it) && !slices.ContainsFunc(special, func(re *regexp.Regexp) bool {
			return re.MatchString(it)
		})
	})
}

// allDescribable reports fields, second to day of week, that are all
// describable.
func allDescribable(fields []string) bool {
	for i, f := range fields {
		special := []*regexp.Regexp{hashItemSyntax}
		switch i {
		case 3:
			special = append(special, domSpecialItem)
		case 5:
			special = append(special, dowNthItem)
		}
		if !describable(f, special...) {
			return false
		}
	}
	return true
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
	unitSecond        = "second"
	unitMinute        = "minute"
	unitDay           = "day"
	unitHour          = "hour"
	unitMonth         = "month"
	thruSep           = " thru "
	everyDaySuffix    = " every day"
	inEveryDaySuffix  = " in every day"
	inEveryHourSuffix = " in every hour"
	duringSep         = " during "
	betweenSep        = " between "
	everyHourFrom     = " every hour from "
	inEveryHourFrom   = " in every hour from "
)

// valueLimit is how many values are listed before a step is cut short,
// unless a field sets its own limit (T1).
const valueLimit = 6

// timeItem is one comma-separated element of a second, minute or hour field.
type timeItem struct {
	lo, hi, step uint
	star         bool // "*" or "?", alone or with a step
	ranged       bool // "lo-hi", alone or with a step
}

// timeField is a field of a spec: its items and the values they fire on.
type timeField struct {
	unit      string // "second", "minute", "hour", "day", "month" or "year"
	container string // the unit a step starts over in; "" for none
	r         bounds
	items     []timeItem
	vals      []uint            // values once each: sorted, except one item of a day of the week or month in firing order
	word      func(uint) string // names a value; nil writes the number
	limit     int               // values listed before a step is cut short; 0 for valueLimit
}

// w names the value v: "5", "5th", "Friday" or "May".
func (f timeField) w(v uint) string {
	if f.word == nil {
		return strconv.Itoa(int(v))
	}
	return f.word(v)
}

// newTimeField parses the field text, which has already parsed as a spec,
// into its items and the values they fire on, once each. Seconds, minutes,
// hours and days of the month sort: a wrapped range is shorthand for two
// ranges, so it reads low to high (T6, T9). A day of the week or a month
// keeps the firing order of a single item: a wrapped range reads in week or
// year order (D4, D6).
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
				if r.stepEnd != 0 && it.lo <= r.stepEnd {
					it.hi = r.stepEnd // DowStepToSaturday
				}
			}
		}
		f.items = append(f.items, it)
		f.vals = append(f.vals, it.fires(r)...)
	}
	if len(f.items) > 1 || !f.cyclic() {
		slices.Sort(f.vals) // a list renders the union of its items
	}
	f.vals = uniq(f.vals)
	return f
}

// fires lists the values an item fires on, in firing order, as the parser
// selects them. A day-of-week step walks the seven distinct days (dowCycle),
// and Sunday reads as 0 whether written 0 or 7 (D4).
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

// onHour writes an hour as its clock time on the hour; midnight is "12 am"
// on a named day (R2).
func (d *describer) onHour(h uint) string {
	return d.clock(h, 0, 0)
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
	if isDowBounds(f.r) { // 7 is Sunday: seven days, not eight values
		var days uint
		for _, v := range f.vals {
			days |= 1 << (v % 7)
		}
		return days == 1<<7-1
	}
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

// nums renders the values ("0, 15, 30, and 45", "0 thru 10", "the 1st and
// 15th") and, for evenly spaced values past the limit, the first 3, "…",
// the last and an interval note (T1, T2). Other values are listed in full.
func (f timeField) nums() (text, note string) {
	limit := f.limit
	if limit == 0 {
		limit = valueLimit
	}
	d, even := f.gap()
	if !even || len(f.vals) <= limit {
		return andList(f.runWords(f.vals)), ""
	}
	first, last := f.vals[0], f.vals[len(f.vals)-1]
	text = f.w(first) + ", " + f.w(f.vals[1]) + ", " + f.w(f.vals[2]) + ", … " + f.w(last)
	note = fmt.Sprintf("every %d %ss", d, f.unit)
	if f.container != "" && first+f.r.max-f.r.min+1-last != d {
		note += ", starting over each " + f.container
	}
	return text, note
}

// cyclic reports a day of the week or a month, whose values read in week or
// year order (D4, D6).
func (f timeField) cyclic() bool {
	return isDowBounds(f.r) || f.unit == "month"
}

// next returns the value after v: Saturday is followed by Sunday, and
// December by January (D4, D6).
func (f timeField) next(v uint) uint {
	switch {
	case isDowBounds(f.r):
		return (v + 1) % 7
	case f.cyclic() && v == f.r.max: // a month
		return f.r.min
	}
	return v + 1
}

// runWords names vals as runs of consecutive values: "0 thru 10", "15", "30",
// "Friday thru Monday".
func (f timeField) runWords(vals []uint) []string {
	var words []string
	for i := 0; i < len(vals); {
		j := i
		for j+1 < len(vals) && vals[j+1] == f.next(vals[j]) {
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
	case f.next(lo):
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
// noon, and 6 pm hours" (R3). On a named day midnight is "12 am" and keeps
// the article.
func (d *describer) hourNames(f timeField) string {
	clocks := make([]string, len(f.vals))
	for i, h := range f.vals {
		clocks[i] = d.onHour(h)
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

// noteText returns the notes in one pair of parentheses, joined with "; ".
func (d *describer) noteText() string {
	all := slices.Concat(d.notes, d.dateNotes)
	if len(all) == 0 {
		return ""
	}
	return " (" + strings.Join(all, "; ") + ")"
}

// nums renders f's values and keeps its note.
func (d *describer) nums(f timeField) string {
	text, note := f.nums()
	if note != "" {
		d.notes = append(d.notes, note)
	}
	return text
}

// dateNums renders a date field's values and keeps its note.
func (d *describer) dateNums(f timeField) string {
	text, note := f.nums()
	if note != "" {
		d.dateNotes = append(d.dateNotes, note)
	}
	return text
}

// values names minutes after "within" or "for": "minute 30", "minutes 0 and 30".
func (d *describer) values(f timeField) string {
	if f.single() {
		return fmt.Sprintf("%s %d", f.unit, f.vals[0])
	}
	return f.unit + "s " + d.nums(f)
}

// everyDay is " every day", or "" when a day clause takes its place (J1).
func (d *describer) everyDay() string {
	if d.named {
		return ""
	}
	return everyDaySuffix
}

// inEveryDay is " in every day", or "" when a day clause takes its place (J1).
func (d *describer) inEveryDay() string {
	if d.named {
		return ""
	}
	return inEveryDaySuffix
}

// clock writes a clock time; midnight is "12 am" on a named day, Sunday
// thru Saturday (R2, D7).
func (d *describer) clock(h, m, s uint) string {
	if d.dayName && h == 0 && m == 0 && s == 0 {
		return "12 am"
	}
	return clockTime(h, m, s)
}

// describeTime describes the second, minute and hour fields: what leads, the
// minutes joined to it, then the hours (C1).
func (d *describer) describeTime(fields []string) string {
	sec := newTimeField(fields[0], unitSecond, unitMinute, seconds)
	minute := newTimeField(fields[1], unitMinute, unitHour, minutes)
	hour := newTimeField(fields[2], unitHour, unitDay, hours)
	if sec.single() {
		if out, ok := d.clockTimes(sec.vals[0], minute, hour); ok {
			return out
		}
		if out, ok := d.hourStep(sec.vals[0], minute, hour); ok {
			return out
		}
	}
	switch {
	case !sec.single() || sec.vals[0] != 0:
		return d.bySeconds(sec, minute, hour)
	case minute.single():
		m := minute.vals[0]
		return d.onTheHour(minuteAt(m), hour, func(h uint) string { return d.clock(h, m, 0) })
	case minute.every():
		return d.joinHours("every minute", "", sec, minute, hour)
	}
	return d.joinHours("at minutes "+d.nums(minute), "past the hour", sec, minute, hour)
}

// bySeconds describes fields led by seconds other than 0 (C1): the minutes
// join with "within", or with "for" after "every second".
func (d *describer) bySeconds(sec, minute, hour timeField) string {
	if sec.every() {
		if minute.every() {
			return d.joinHours("every second", "", sec, minute, hour)
		}
		return d.joinHours("every second for "+d.values(minute), "in every hour", sec, minute, hour)
	}
	secs := "at " + count(sec.vals[0], unitSecond)
	var at func(h uint) string
	if sec.single() {
		at = func(h uint) string { return d.clock(h, 0, sec.vals[0]) }
	} else {
		secs = "at seconds " + d.nums(sec)
	}
	return d.joinSeconds(secs, at, sec, minute, hour)
}

// joinSeconds joins the minutes and hours to lead, which names the seconds
// (C1): "within a minute", "within minute 30 in every hour", "past the top
// of every hour". at writes the run in hour h for one second at minute 0, as
// onTheHour takes it; otherwise it is nil.
func (d *describer) joinSeconds(lead string, at func(h uint) string, sec, minute, hour timeField) string {
	switch {
	case minute.every() && hour.every():
		return lead + " within a minute"
	case minute.every():
		return d.joinHours(lead+" within every minute", "", sec, minute, hour)
	case minute.single() && minute.vals[0] == 0:
		return d.onTheHour(lead+" past the top of", hour, at)
	}
	return d.joinHours(lead+" within "+d.values(minute), "in every hour", sec, minute, hour)
}

// minuteAt opens one minute on the hour forms: "at the top of" (R4), "at 30
// minutes past" (R5).
func minuteAt(m uint) string {
	if m == 0 {
		return "at the top of"
	}
	return "at " + count(m, unitMinute) + " past"
}

// onTheHour joins the hours to one minute, any minute, as lead opens it
// (R4, R5, R9, R10): "at the top of every hour", "at 30 minutes past every
// hour from 9 am thru 5 pm every day".
//
// at writes the run in hour h when the lead fixes one minute and one
// second; a part of one hour of a wrapped range then reads "at" that run
// (R9, T9): "from midnight thru 5 am and at 11:30 pm". Otherwise at is nil,
// and that part reads as its hour: "from 6 pm thru 11 pm and the 5 am hour".
func (d *describer) onTheHour(lead string, hour timeField, at func(h uint) string) string {
	switch hour.kind() {
	case everyHour:
		if strings.HasSuffix(lead, " past") {
			return lead + " the hour"
		}
		return lead + " every hour"
	case namedHours:
		return lead + " " + d.hourNames(hour)
	case stepHours:
		return lead + " " + d.hourNames(hour) + d.inEveryDay()
	case longStep:
		return lead + " " + d.stepClause(hour)
	}
	it := hour.items[0]
	if p := it.parts(hours); len(p) == 2 {
		switch {
		case p[1][0] == p[1][1] && at != nil:
			return lead + everyHourFrom + d.partRange(p[0]) + " and at " + at(p[1][0]) + d.everyDay()
		case p[0][0] == p[0][1] && at != nil:
			return "at " + at(p[0][0]) + " and " + lead + everyHourFrom + d.partRange(p[1]) + d.everyDay()
		}
	}
	return lead + " " + d.everyHourParts(it) + d.everyDay()
}

// everyHourParts names a long hour range: "every hour from 9 am thru 5 pm";
// a part of one hour of a wrapped range reads as that hour (T9): "every hour
// from midnight thru 5 am and the 11 pm hour", "the midnight hour and every
// hour from 6 pm thru 11 pm".
func (d *describer) everyHourParts(it timeItem) string {
	if p := it.parts(hours); len(p) == 2 {
		switch {
		case p[1][0] == p[1][1]:
			return "every hour from " + d.partRange(p[0]) + " and the " + d.onHour(p[1][0]) + " hour"
		case p[0][0] == p[0][1]:
			return "the " + d.onHour(p[0][0]) + " hour and every hour from " + d.partRange(p[1])
		}
	}
	return "every hour " + fromThru(it, d.onHour, d.onHour)
}

// joinHours joins the hours to inner, the seconds and minutes (R6, R9–R12,
// T5). every is the join for every hour.
func (d *describer) joinHours(inner, every string, sec, minute, hour timeField) string {
	if it, ok := spanForm(minute, hour); ok {
		// T5: the first and last run of the day lead.
		first := func(h uint) string { return d.clock(h, minute.vals[0], sec.vals[0]) }
		last := func(h uint) string {
			return d.clock(h, minute.vals[len(minute.vals)-1], sec.vals[len(sec.vals)-1])
		}
		return fromThru(it, first, last) + d.everyDay() + ", " + inner + " past each hour"
	}
	switch hour.kind() {
	case everyHour:
		if every == "" {
			return inner
		}
		return inner + " " + every
	case namedHours:
		return inner + duringSep + d.hourNames(hour)
	case stepHours:
		return inner + duringSep + d.hourNames(hour) + d.inEveryDay()
	case longStep:
		return inner + " " + d.stepClause(hour)
	}
	it := hour.items[0]
	if minute.every() {
		// R12: the first and last run.
		var s uint
		if sec.every() {
			s = 59
		}
		last := func(h uint) string { return d.clock(h, 59, s) }
		return inner + " " + fromThru(it, d.onHour, last) + d.everyDay()
	}
	if out, ok := d.wrappedPart(inner, sec, minute, it); ok {
		return out
	}
	return inner + " in every hour " + fromThru(it, d.onHour, d.onHour) + d.everyDay() // R9
}

// wrappedPart joins one minute on a wrapped hour range with a part of one
// hour: the part reads "at" its run under one second, otherwise "during" its
// hour (T9, R11). ok is false when neither part is one hour.
func (d *describer) wrappedPart(inner string, sec, minute timeField, it timeItem) (string, bool) {
	p := it.parts(hours)
	if len(p) != 2 {
		return "", false
	}
	switch {
	case p[1][0] == p[1][1] && sec.single(): // a hashed minute has no sec
		return inner + inEveryHourFrom + d.partRange(p[0]) + " and at " + d.clock(p[1][0], minute.vals[0], sec.vals[0]) + d.everyDay(), true
	case p[0][0] == p[0][1] && sec.single():
		return "at " + d.clock(p[0][0], minute.vals[0], sec.vals[0]) + " and " + inner + inEveryHourFrom + d.partRange(p[1]) + d.everyDay(), true
	case p[1][0] == p[1][1]:
		return inner + inEveryHourFrom + d.partRange(p[0]) + " and during the " + d.onHour(p[1][0]) + " hour" + d.everyDay(), true
	case p[0][0] == p[0][1]:
		return inner + " during the " + d.onHour(p[0][0]) + " hour and in every hour from " + d.partRange(p[1]) + d.everyDay(), true
	}
	return "", false
}

// partRange names a part of an hour range: "midnight thru 5 am".
func (d *describer) partRange(p [2]uint) string {
	return d.onHour(p[0]) + thruSep + d.onHour(p[1])
}

// spanForm reports the span form (T5), whose first and last runs need the
// minutes and seconds as clock times: several minutes on an hour range.
func spanForm(minute, hour timeField) (timeItem, bool) {
	it, ok := hour.span()
	return it, ok && !hour.every() && len(minute.vals) > 1 && !minute.every() &&
		(it.lo < it.hi || hour.kind() == longRange)
}

// wholeDay reports an hour step over the whole day that divides 24 (R8, R11).
func wholeDay(it timeItem) bool {
	return it.lo == hours.min && it.hi == hours.max && 24%it.step == 0
}

// stepClause names an hour step past 6 values by its hours (R11): "every 2
// hours starting with the midnight hour in every day".
func (d *describer) stepClause(hour timeField) string {
	it := hour.items[0]
	out := fmt.Sprintf("every %d hours ", it.step)
	if wholeDay(it) {
		return out + "starting with the " + d.onHour(it.lo) + " hour" + d.inEveryDay()
	}
	// T9: the parts, the first range after "from"; a part of one run is
	// "during" its hour.
	parts := stepParts(it, hour.r)
	words := make([]string, 0, len(parts))
	for i, p := range parts {
		if p[0] == p[1] {
			words = append(words, "during the "+d.onHour(p[0])+" hour")
			continue
		}
		w := "the " + d.onHour(p[0]) + " hour thru the " + d.onHour(p[1]) + " hour"
		if i == 0 || parts[0][0] == parts[0][1] {
			w = "from " + w
		}
		words = append(words, w)
	}
	return out + andList(words) + d.inEveryDay()
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
func (d *describer) clockTimes(s uint, minute, hour timeField) (string, bool) {
	oneHour := hour.single() && len(minute.vals) <= valueLimit
	oneMinute := minute.single() && hour.kind() == namedHours // T3: every hour is not a list
	if !oneHour && !oneMinute {
		return "", false
	}
	clocks := make([]string, 0, len(hour.vals)*len(minute.vals))
	for _, h := range hour.vals {
		for _, m := range minute.vals {
			clocks = append(clocks, d.clock(h, m, s))
		}
	}
	return "at " + andList(clocks) + d.everyDay(), true
}

// hourStep describes one minute on an hour step past 6 values (R8): the
// start, with the minute folded in, and the end unless the step covers the
// whole day and divides 24.
func (d *describer) hourStep(s uint, minute, hour timeField) (string, bool) {
	if !minute.single() || hour.kind() != longStep {
		return "", false
	}
	it, m := hour.items[0], minute.vals[0]
	out := fmt.Sprintf("every %d hours ", it.step)
	if wholeDay(it) {
		out += "starting at " + d.clock(it.lo, m, s)
	} else {
		at := func(h uint) string { return d.clock(h, m, s) }
		out += partWords(stepParts(it, hour.r), at)
	}
	return out + d.inEveryDay(), true
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
