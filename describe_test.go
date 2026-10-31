// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cron

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// withSeconds parses six-field specs, seconds first.
var withSeconds = WithParseOptions(Second | Minute | Hour | Dom | Month | Dow | Descriptor)

// withDowOrDom ORs the day fields when both are restricted.
var withDowOrDom = WithParseOptions(Minute | Hour | Dom | Month | Dow | DowOrDom)

// withYear parses a sixth field, the year.
var withYear = WithParseOptions(Minute | Hour | Dom | Month | Dow | Year)

// describeGoldens are the binding descriptions (docs/DESCRIBE.md). literal
// marks the field-by-field fallback, which is outside the grammar.
var describeGoldens = []struct {
	spec    string
	opts    []DescribeOption
	want    string
	literal bool
}{
	// Times within the hour
	{spec: "0,15,30,45 * * * *", want: "at 0, 15, 30 and 45 minutes past the hour"},
	{spec: "*/15 * * * *", want: "at 0, 15, 30 and 45 minutes past the hour"},
	{spec: "10-40/10 * * * *", want: "at 10, 20, 30 and 40 minutes past the hour"},
	{spec: "*/7 * * * *", want: "at 0, 7, 14, … 56 minutes past the hour (every 7 minutes, starting over each hour)"},
	{spec: "*/15,7 * * * *", want: "at 0, 7, 15, 30 and 45 minutes past the hour"},
	{spec: "0-5 * * * *", want: "at 0 thru 5 minutes past the hour"},
	{spec: "5-6 * * * *", want: "at 5 and 6 minutes past the hour"},
	{spec: "5-5 * * * *", want: "at 5 minutes past the hour"},
	{spec: "5/1 * * * *", want: "at 5 thru 59 minutes past the hour"},
	{spec: "0 5/1 * * *", want: "at 0 minutes past hours 5 thru 23"},
	{spec: "50-10 * * * *", want: "at 50 thru 59 and 0 thru 10 minutes past the hour"},
	{spec: "58-1 * * * *", want: "at 58, 59, 0 and 1 minutes past the hour"},
	{spec: "50-10/2 * * * *", want: "at 50, 52, 54, 56, 58, 0, 2, 4, 6, 8 and 10 minutes past the hour"},
	{spec: "*/7 22-2 * * *", want: "at 0, 7, 14, … 56 minutes past hours 22, 23 and 0 thru 2 (every 7 minutes, starting over each hour)"},
	{spec: "0 0 */1 * *", want: "at 00:00 every day"},
	{spec: "5 * * * *", want: "at 5 minutes past the hour"},
	{spec: "1 * * * *", want: "at 1 minute past the hour"},
	{spec: "0 * * * *", want: "on the hour"},
	{spec: "*/60 * * * *", opts: []DescribeOption{WithParseOptions(Minute | Hour | Dom | Month | Dow | LenientSteps)}, want: "on the hour"},
	{spec: "* * * * *", want: "every minute"},
	{spec: "* 9 * * *", want: "every minute for hour 9"},

	// Lists of several items render their values sorted, once each
	{spec: "*/10,5 * * * *", want: "at 0, 5, 10, 20, 30, 40 and 50 minutes past the hour"},
	{spec: "0-10,*/15 * * * *", want: "at 0 thru 10, 15, 30 and 45 minutes past the hour"},
	{spec: "1-5,3-7 * * * *", want: "at 1 thru 7 minutes past the hour"},
	{spec: "1,1 * * * *", want: "at 1 minute past the hour"},
	{spec: "30,0 * * * *", want: "at 0 and 30 minutes past the hour"},
	{spec: "* 1,1 * * *", want: "every minute for hour 1"},
	{spec: "1,3,5,7,9,11,13,15 * * * *", want: "at 1, 3, 5, 7, 9, 11, 13 and 15 minutes past the hour"},

	// A field that covers its whole range is every value
	{spec: "*/1,5 * * * *", want: "every minute"},
	{spec: "0 0-23 * * *", want: "on the hour"},
	{spec: "0 */1,* * * *", want: "on the hour"},
	{spec: "* 0-23 * * *", want: "every minute"},
	{spec: "*/15 0-23 * * *", want: "at 0, 15, 30 and 45 minutes past the hour"},

	// Clock times
	{spec: "5/15 2 * * *", want: "at 02:05, 02:20, 02:35 and 02:50 every day"},
	{spec: "0 9-17 * * *", want: "at 09:00, 10:00, 11:00, 12:00, 13:00, 14:00, 15:00, 16:00 and 17:00 every day"},
	{spec: "0 22-2/2 * * *", want: "at 22:00, 00:00 and 02:00 every day"},
	{spec: "0 22-2/2 * * *", opts: []DescribeOption{WithClockForm(Clock24Words)}, want: "at 22:00, midnight and 02:00 every day"},
	{spec: "0 22-2/2 * * *", opts: []DescribeOption{WithClockForm(Clock12)}, want: "at 10 pm, midnight and 2 am every day"},
	{spec: "0 0 * * *", want: "at 00:00 every day"},
	{spec: "0 0 * * *", opts: []DescribeOption{WithClockForm(Clock24Words)}, want: "at midnight every day"},
	{spec: "0 0 * * *", opts: []DescribeOption{WithClockForm(Clock12)}, want: "at midnight every day"},
	{spec: "0 12 * * *", want: "at 12:00 every day"},
	{spec: "0 12 * * *", opts: []DescribeOption{WithClockForm(Clock24Words)}, want: "at noon every day"},
	{spec: "CRON_TZ=UTC 0 0 * * *", want: "at 00:00 every day"},
	{spec: "0 17,9 * * *", want: "at 09:00 and 17:00 every day"},
	{spec: "0 0,12,13 * * *", opts: []DescribeOption{WithClockForm(ClockForm(99))}, want: "at 00:00, 12:00 and 13:00 every day"},

	// Span form: several minutes over an hour range
	{spec: "*/7 9-17 * * *", want: "from 09:00 thru 17:56 every day, at 0, 7, 14, … 56 minutes past each hour (every 7 minutes, starting over each hour)"},
	{spec: "*/7 9-17 * * *", opts: []DescribeOption{WithClockForm(Clock12)}, want: "from 9 am thru 5:56 pm every day, at 0, 7, 14, … 56 minutes past each hour (every 7 minutes, starting over each hour)"},
	{spec: "0,30 9-17 * * *", want: "from 09:00 thru 17:30 every day, at 0 and 30 minutes past each hour"},
	{spec: "*/30 0-5 * * *", opts: []DescribeOption{WithClockForm(Clock24Words)}, want: "from midnight thru 05:30 every day, at 0 and 30 minutes past each hour"},

	// Hour lists and steps
	{spec: "30 */2 * * *", want: "at 30 minutes past hours 0, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20 and 22 in every day"},
	{spec: "0,30 9,17 * * *", want: "at 0 and 30 minutes past hours 9 and 17"},

	// Seconds
	{spec: "15 */4 * * * *", opts: []DescribeOption{withSeconds}, want: "at 15 seconds past minutes 0, 4, 8, … 56 in every hour (every 4 minutes)"},
	{spec: "*/20 30 * * * *", opts: []DescribeOption{withSeconds}, want: "at 0, 20 and 40 seconds past minute 30 in every hour"},
	{spec: "* * * * * *", opts: []DescribeOption{withSeconds}, want: "every second"},
	{spec: "* * 9 * * *", opts: []DescribeOption{withSeconds}, want: "every second for hour 9"},
	{spec: "* 0-59 9 * * *", opts: []DescribeOption{withSeconds}, want: "every second for hour 9"},
	{spec: "* 30 * * * *", opts: []DescribeOption{withSeconds}, want: "every second for minute 30 in every hour"},
	{spec: "5 * * * * *", opts: []DescribeOption{withSeconds}, want: "at 5 seconds past the minute"},
	{spec: "5 0 * * * *", opts: []DescribeOption{withSeconds}, want: "at 5 seconds past the hour"},
	{spec: "15 30 2 * * *", opts: []DescribeOption{withSeconds}, want: "at 02:30:15 every day"},
	{spec: "15 30 2 * * *", opts: []DescribeOption{withSeconds, WithClockForm(Clock12)}, want: "at 2:30:15 am every day"},

	// 12-hour details
	{spec: "30 0 * * *", want: "at 00:30 every day"},
	{spec: "30 0 * * *", opts: []DescribeOption{WithClockForm(Clock12)}, want: "at 12:30 am every day"},
	{spec: "30 12 * * *", opts: []DescribeOption{WithClockForm(Clock12)}, want: "at 12:30 pm every day"},
	{spec: "0 10,12,14 * * *", opts: []DescribeOption{WithClockForm(Clock12)}, want: "at 10 am, noon and 2 pm every day"},
	{spec: "0 10,12,14 * * *", opts: []DescribeOption{WithClockForm(Clock24Words)}, want: "at 10:00, noon and 14:00 every day"},

	// A named day: ascending clock times, no "midnight", no "every day"
	{spec: "0 0 * * 5", want: "at 00:00 on Friday"},
	{spec: "0 0 * * 5", opts: []DescribeOption{WithClockForm(Clock24Words)}, want: "at 00:00 on Friday"},
	{spec: "0 0 * * 5", opts: []DescribeOption{WithClockForm(Clock12)}, want: "at 12 am on Friday"},
	{spec: "0 22-2/2 * * 5", want: "at 00:00, 02:00 and 22:00 on Friday"},
	{spec: "0 22-2/2 * * 5", opts: []DescribeOption{WithClockForm(Clock12)}, want: "at 12 am, 2 am and 10 pm on Friday"},
	{spec: "0 9-17 * * 1-5", want: "at 09:00, 10:00, 11:00, 12:00, 13:00, 14:00, 15:00, 16:00 and 17:00 on Monday thru Friday"},
	{spec: "0 12 * * 1-5", opts: []DescribeOption{WithClockForm(Clock24Words)}, want: "at noon on Monday thru Friday"},
	{spec: "15 0 9 1 * *", opts: []DescribeOption{withSeconds}, want: "at 09:00:15 on the 1st of the month"},

	{spec: "30 */2 * * 1", want: "at 30 minutes past hours 0, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20 and 22 on Monday"},
	{spec: "*/7 9-17 * * 1-5", want: "from 09:00 thru 17:56, at 0, 7, 14, … 56 minutes past each hour on Monday thru Friday (every 7 minutes, starting over each hour)"},
	{spec: "* * * 1 *", want: "every minute in January"},
	{spec: "0 * * * 1", want: "on the hour on Monday"},
	{spec: "*/15 * 1 * *", want: "at 0, 15, 30 and 45 minutes past the hour on the 1st of the month"},

	// Days of the month
	{spec: "0 0 1,15 * *", want: "at 00:00 on the 1st and 15th of the month"},
	{spec: "0 0 1-7 * *", want: "at 00:00 on the 1st thru 7th of the month"},
	{spec: "0 0 11-13,22 * *", want: "at 00:00 on the 11th thru 13th and 22nd of the month"},
	{spec: "0 0 1-28/7 * *", want: "at 00:00 on the 1st, 8th, 15th and 22nd of every month"},
	{spec: "0 0 1/10 * *", want: "at 00:00 on the 1st, 11th, 21st and 31st of the month; the 31st runs only in months that have one"},
	{spec: "0 0 */3 * *", want: "at 00:00 on the 1st, 4th, 7th, 10th, 13th, 16th, 19th, 22nd, 25th, 28th and 31st of the month; the 31st runs only in months that have one"},
	{spec: "0 0 */2 * *", want: "at 00:00 on the 1st, 3rd, 5th, … 31st of the month (every 2 days, starting over each month); the 29th and 31st run only in months that have them"},
	{spec: "0 0 29 * *", want: "at 00:00 on the 29th of the month; the 29th runs only in months that have one"},
	{spec: "0 0 30,31 * *", want: "at 00:00 on the 30th and 31st of the month; the 30th and 31st run only in months that have them"},

	// Days of the week, from the parser's values
	{spec: "0 0 * * 1,3,5", want: "at 00:00 on Monday, Wednesday and Friday"},
	{spec: "0 0 * * MON-FRI", want: "at 00:00 on Monday thru Friday"},
	{spec: "0 0 * * 6-7", want: "at 00:00 on Saturday and Sunday"},
	{spec: "0 0 * * 0,7", want: "at 00:00 on Sunday"},
	{spec: "0 0 * * */2", want: "at 00:00 on Sunday, Tuesday, Thursday and Saturday"},
	{spec: "0 0 * * 5-1/2", want: "at 00:00 on Friday and Sunday"},
	{spec: "0 0 * * 1/2", want: "at 00:00 on Monday, Wednesday, Friday and Sunday"},
	{spec: "0 0 * * 1/2", opts: []DescribeOption{WithParseOptions(Minute | Hour | Dom | Month | Dow | DowStepToSaturday)}, want: "at 00:00 on Monday, Wednesday and Friday"},

	// Both day fields
	{spec: "0 0 13 * 5", want: "at 00:00 on the 13th of the month when it falls on Friday"},
	{spec: "0 0 1 * 1", opts: []DescribeOption{withDowOrDom}, want: "at 00:00 on the 1st of the month or on any Monday"},
	{spec: "0 0 1,15 * 1-5", opts: []DescribeOption{withDowOrDom}, want: "at 00:00 on the 1st and 15th of the month or on any Monday thru Friday"},

	// Months and years
	{spec: "0 0 * 1-3 *", want: "at 00:00 every day in January thru March"},
	{spec: "0 0 * 1-3 *", opts: []DescribeOption{WithClockForm(Clock24Words)}, want: "at midnight every day in January thru March"},
	{spec: "0 0 1 1-3 *", want: "at 00:00 on the 1st of the month in January thru March"},
	{spec: "0 0 1 */3 *", want: "at 00:00 on the 1st of the month in January, April, July and October"},
	{spec: "0 0 1 JAN,JUN *", want: "at 00:00 on the 1st of the month in January and June"},
	{spec: "0 0 1 1 * 2026-2028", opts: []DescribeOption{withYear}, want: "at 00:00 on the 1st of the month in January in years 2026 thru 2028"},
	{spec: "0 0 1 1 * *", opts: []DescribeOption{withYear}, want: "at 00:00 on the 1st of the month in January"},
	{spec: "0 0 1 1 * 2027", opts: []DescribeOption{withYear}, want: "at 00:00 on the 1st of the month in January in year 2027"},
	{spec: "0 0 1 1 * 2030,2026", opts: []DescribeOption{withYear}, want: "at 00:00 on the 1st of the month in January in years 2026 and 2030"},
	{spec: "0 0 1 1 * 2026-2030/2", opts: []DescribeOption{withYear}, want: "at 00:00 on the 1st of the month in January in years 2026, 2028 and 2030"},

	// The parser reads a range from "*" or "?" as the star alone
	{spec: "0 *-5 * * *", want: "on the hour"},
	{spec: "*-/15 * * * *", want: "at 0, 15, 30 and 45 minutes past the hour"},
	{spec: "0 0 *-3 * *", want: "at 00:00 every day"},
	{spec: "0 0 ?-3 * 1", want: "at 00:00 on Monday"},

	// A star anywhere in a list selects every value, and the day fields
	// then AND, as the parser runs them
	{spec: "0 0 * *,3 *", want: "at 00:00 every day"},
	{spec: "0 0 *,1 * *", want: "at 00:00 every day"},
	{spec: "0 0 *,1 * 1", opts: []DescribeOption{withDowOrDom}, want: "at 00:00 on Monday"},
	{spec: "0 0 1,* * 1", opts: []DescribeOption{WithParseOptions(Minute | Hour | Dom | Month | Dow | DowOrDom | StepWildcard)}, want: "at 00:00 on Monday"},
	{spec: "0 0 1 * *,1", opts: []DescribeOption{withDowOrDom}, want: "at 00:00 on the 1st of the month"},
	{spec: "0 0 13 * ?,5", opts: []DescribeOption{withDowOrDom}, want: "at 00:00 on the 13th of the month"},
	{spec: "0 0 1 * */01", opts: []DescribeOption{withDowOrDom}, want: "at 00:00 on the 1st of the month"},

	// Literal fallback: the whole spec renders field by field
	{spec: "0 0 L * *", opts: []DescribeOption{WithParseOptions(Minute | Hour | Dom | Month | Dow | DomL)}, want: "minute 0, hour 0, day of month L, month *, day of week *", literal: true},
	{spec: "0 0 * * 5#3", opts: []DescribeOption{WithParseOptions(Minute | Hour | Dom | Month | Dow | DowNth)}, want: "minute 0, hour 0, day of month *, month *, day of week 5#3", literal: true},
	{spec: "@daily", want: "@daily", literal: true},
	{spec: "* , * * *", want: "minute *, hour ,, day of month *, month *, day of week *", literal: true},
	{spec: ", * * * *", want: "minute ,, hour *, day of month *, month *, day of week *", literal: true},
	{spec: "H/15 * * * *", opts: []DescribeOption{WithParseOptions(Minute | Hour | Dom | Month | Dow | Hash)}, want: "minute H/15, hour *, day of month *, month *, day of week *", literal: true},
}

func TestDescribeSpec(t *testing.T) {
	for _, tt := range describeGoldens {
		got, err := DescribeSpec(tt.spec, tt.opts...)
		if err != nil {
			t.Errorf("DescribeSpec(%q): unexpected error %v", tt.spec, err)
			continue
		}
		if got != tt.want {
			t.Errorf("DescribeSpec(%q):\n got %q\nwant %q", tt.spec, got, tt.want)
		}
	}
}

// describeGrammar is the EBNF in docs/DESCRIBE.md, without the forms that
// render literally, as a regular expression. Every description outside the
// literal fallback must match it.
func describeGrammar() *regexp.Regexp {
	alt := func(s ...string) string { return "(?:" + strings.Join(s, "|") + ")" }
	andList := func(item string) string { return item + "(?:(?:, " + item + ")* and " + item + ")?" }

	clock24 := `\d\d:\d\d(?::\d\d)?`
	clock12 := `\d{1,2}(?::\d\d(?::\d\d)?)? [ap]m`
	clock := alt(clock24, clock12, "midnight", "noon")
	num := `\d+`
	list := andList(num + "(?: thru " + num + ")?")
	vlist := alt(list, num+", "+num+", "+num+", … "+num)
	unitSg := alt("second", "minute", "hour")
	unitPl := alt("seconds", "minutes", "hours")
	unit := alt(unitSg, unitPl) + " " + vlist
	every := alt("every "+unitSg, "every day")
	join := "(?: for " + unit + "| in " + every + ")*"
	tvals := vlist + " " + alt("seconds?", "minutes?")
	ival := "every " + alt(unitSg, num+" "+unitPl) +
		"(?:, starting over each " + alt("minute", "hour", "day") + ")?"
	note := ` \(` + ival + "(?:; " + ival + `)*\)`

	dayname := alt("Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday")
	monthname := alt("January", "February", "March", "April", "May", "June", "July",
		"August", "September", "October", "November", "December")
	ord := `\d+(?:st|nd|rd|th)`
	olist := andList(ord + "(?: thru " + ord + ")?")
	ovlist := alt(olist, ord+", "+ord+", "+ord+", … "+ord)
	dnote := ` \(every ` + alt("day", num+" days") + `(?:, starting over each month)?\)`
	dom := "the " + ovlist + " of " + alt("the", "every") + " month(?:" + dnote + ")?"
	dowItem := dayname + "(?: thru " + dayname + ")?"
	day := "on " + alt(dom+"(?: when it falls on "+andList(dowItem)+"| or on "+andList("any "+dowItem)+")?",
		andList(dowItem))
	month := "in " + andList(monthname+"(?: thru "+monthname+")?")
	year := alt("in year "+num, "in years "+vlist+`(?: \(every `+alt("year", num+" years")+`\))?`)
	short := "; the " + alt(ord+" runs only in months that have one",
		ord+"(?:, "+ord+")* and "+ord+" run only in months that have them")
	later := "(?: " + month + ")?(?: " + year + ")?"
	dated := "(?: " + day + ")?" + later

	clocks := "at " + andList(clock)
	span := "from " + clock + " thru " + clock
	each := ", at " + tvals + " past each hour"
	schedule := alt(
		clocks+" every day"+later,
		clocks+" "+day+later,
		span+" every day"+each+later,
		span+each+" "+day+later,
		"on the hour"+dated,
		"at "+tvals+" past "+alt("the "+alt("minute", "hour"), alt(unit, every)+join)+dated,
		"every "+unitSg+join+dated,
	)
	return regexp.MustCompile("^" + schedule + "(?:" + note + ")?(?:" + short + ")?$")
}

func TestDescribeSpecGrammar(t *testing.T) {
	g := describeGrammar()
	for _, tt := range describeGoldens {
		if !tt.literal && !g.MatchString(tt.want) {
			t.Errorf("%q: description %q is outside the grammar", tt.spec, tt.want)
		}
	}
}

func TestWithParseOptionsCombine(t *testing.T) {
	got, err := DescribeSpec("15 30 2 * * *",
		WithParseOptions(Second|Minute|Hour), WithParseOptions(Dom|Month|Dow))
	if err != nil {
		t.Fatal(err)
	}
	if want := "at 02:30:15 every day"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDescribeSpecParseError(t *testing.T) {
	for _, spec := range []string{"", "61 * * * *", "* * *", "@every 1x"} {
		if got, err := DescribeSpec(spec); err == nil {
			t.Errorf("DescribeSpec(%q) = %q, want a parse error", spec, got)
		}
	}
}

func TestParserDescribeSpec(t *testing.T) {
	p := NewParser(Second | Minute | Hour | Dom | Month | Dow)
	// The parser's own settings apply; WithParseOptions is for the package function.
	got, err := p.DescribeSpec("15 30 2 * * *", WithParseOptions(Minute|Hour|Dom|Month|Dow))
	if err != nil {
		t.Fatal(err)
	}
	if want := "at 02:30:15 every day"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// YearOptional with no year written: the time-only description.
	got, err = FullParser().DescribeSpec("0 0 * * *")
	if err != nil {
		t.Fatal(err)
	}
	if want := "at 00:00 every day"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// YearOptional with a year written.
	got, err = FullParser().DescribeSpec("0 0 0 * * * 2027")
	if err != nil {
		t.Fatal(err)
	}
	if want := "at 00:00 every day in year 2027"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// SecondOptional: a second that was not written is not listed.
	got, err = FullParser().DescribeSpec("0 0 L * *")
	if err != nil {
		t.Fatal(err)
	}
	if want := "minute 0, hour 0, day of month L, month *, day of week *"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	got, err = FullParser().DescribeSpec("0 0 0 L * * 2027")
	if err != nil {
		t.Fatal(err)
	}
	if want := "second 0, minute 0, hour 0, day of month L, month *, day of week *, year 2027"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	got, err = NewParser(Minute | Hour | Dom | Month | Dow | Hash).WithHashKey("job").DescribeSpec("H/15 * * * *")
	if err != nil {
		t.Fatal(err)
	}
	if want := "minute H/15, hour *, day of month *, month *, day of week *"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// No hash key: the description does not depend on one.
	got, err = FullParser().DescribeSpec("H H * * *")
	if err != nil {
		t.Fatal(err)
	}
	if want := "minute H, hour H, day of month *, month *, day of week *"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// literalForm matches the field-by-field fallback and bare descriptors.
var literalForm = regexp.MustCompile(`^(?:@|(?:second|minute|hour|day of month|month|day of week|year) )`)

// longNumber matches a number of six or more digits.
var longNumber = regexp.MustCompile(`[0-9]{6}`)

// literalSyntax reports the syntax DescribeSpec renders literally: H in any
// field, L or W in the day of month, L or # in the day of week, and a field
// written as commas only. An item that starts with "*" or "?" is the star,
// whatever follows it, as the parser reads it. It is written apart from the
// describer's own check, so a spec that falls back for another reason fails
// FuzzDescribeSpec.
func literalSyntax(fields []string, yearField string) bool {
	if yearField != "" {
		fields = append(slices.Clone(fields), yearField)
	}
	for i, f := range fields {
		if strings.Trim(f, ",") == "" {
			return true
		}
		for item := range strings.SplitSeq(f, ",") {
			if strings.HasPrefix(item, "*") || strings.HasPrefix(item, "?") {
				continue
			}
			u := strings.ToUpper(item)
			if strings.HasPrefix(item, "H") ||
				(i == 3 && strings.ContainsAny(u, "LW")) || (i == 5 && strings.ContainsAny(u, "L#")) {
				return true
			}
		}
	}
	return false
}

// FuzzDescribeSpec checks that every spec that parses gets a description in
// the grammar or the literal fallback, and the literal fallback exactly for
// descriptors and literalSyntax.
func FuzzDescribeSpec(f *testing.F) {
	for _, tt := range describeGoldens {
		f.Add(tt.spec)
	}
	f.Add("5 , , * * *")
	f.Add("1,,2 * * * *")
	f.Add("0-59 0-23 * * *")
	f.Add("0 *- 1 1 0")
	f.Add("0 0 *-L 1 0")
	f.Add("+0 0 1 1 0")
	f.Add("0 0 1 1 0 22-99999")

	g := describeGrammar()
	parsers := []Parser{
		NewParser(Minute | Hour | Dom | Month | Dow | Descriptor),
		NewParser(Second | Minute | Hour | Dom | Month | Dow | Descriptor),
		FullParser(),
	}
	f.Fuzz(func(t *testing.T, spec string) {
		// The parser expands a year range year by year, so a range of
		// millions of years takes Parse itself minutes. Only a year can have
		// six digits.
		if longNumber.MatchString(spec) {
			return
		}
		for _, p := range parsers {
			if _, err := p.WithHashKey("fuzz").Parse(spec); err != nil {
				continue
			}
			got, err := p.DescribeSpec(spec)
			if err != nil {
				t.Fatalf("DescribeSpec(%q): unexpected error %v", spec, err)
			}
			if !g.MatchString(got) && !literalForm.MatchString(got) {
				t.Fatalf("DescribeSpec(%q) = %q, outside the grammar", spec, got)
			}
			_, descriptor, fields, yearField, _ := p.specFields(spec)
			if want := descriptor != "" || literalSyntax(fields, yearField); literalForm.MatchString(got) != want {
				t.Fatalf("DescribeSpec(%q) = %q: literal form %v, want %v", spec, got, !want, want)
			}
		}
	})
}
