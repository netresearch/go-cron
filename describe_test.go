// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cron

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// withSeconds parses six-field specs, seconds first.
var withSeconds = WithParseOptions(Second | Minute | Hour | Dom | Month | Dow | Descriptor)

// describeGoldens are the binding descriptions (docs/DESCRIBE.md). literal
// marks the field-by-field fallback, which is outside the grammar.
var describeGoldens = []struct {
	spec    string
	opts    []DescribeOption
	want    string
	literal bool
}{
	// Times within the hour
	{spec: "0,15,30,45 * * * *", want: "at minutes 0, 15, 30, and 45 past the hour"},
	{spec: "*/15 * * * *", want: "at minutes 0, 15, 30, and 45 past the hour"},
	{spec: "10-40/10 * * * *", want: "at minutes 10, 20, 30, and 40 past the hour"},
	{spec: "*/7 * * * *", want: "at minutes 0, 7, 14, … 56 past the hour (every 7 minutes, starting over each hour)"},
	{spec: "*/15,7 * * * *", want: "at minutes 0, 7, 15, 30, and 45 past the hour"},
	{spec: "0-5 * * * *", want: "at minutes 0 thru 5 past the hour"},
	{spec: "5-6 * * * *", want: "at minutes 5 and 6 past the hour"},
	{spec: "5-5 * * * *", want: "at 5 minutes past the hour"},
	{spec: "5/1 * * * *", want: "at minutes 5 thru 59 past the hour"},
	{spec: "0 5/1 * * *", want: "at the top of every hour from 5 am thru 11 pm every day"},
	{spec: "50-10 * * * *", want: "at minutes 0 thru 10 and 50 thru 59 past the hour"},
	{spec: "58-1 * * * *", want: "at minutes 0, 1, 58, and 59 past the hour"},
	{spec: "50-10/2 * * * *", want: "at minutes 0, 2, 4, 6, 8, 10, 50, 52, 54, 56, and 58 past the hour"},
	{spec: "*/7 22-2 * * *", want: "at minutes 0, 7, 14, … 56 during midnight, 1 am, 2 am, 10 pm, and 11 pm hours (every 7 minutes, starting over each hour)"},
	{spec: "0 0 */1 * *", want: "at midnight every day"},
	{spec: "5 * * * *", want: "at 5 minutes past the hour"},
	{spec: "1 * * * *", want: "at 1 minute past the hour"},
	{spec: "0 * * * *", want: "at the top of every hour"},
	{spec: "*/60 * * * *", opts: []DescribeOption{WithParseOptions(Minute | Hour | Dom | Month | Dow | LenientSteps)}, want: "at the top of every hour"},
	{spec: "* * * * *", want: "every minute"},
	{spec: "* 9 * * *", want: "every minute during the 9 am hour"},

	// Lists and whole ranges
	{spec: "*/10,5 * * * *", want: "at minutes 0, 5, 10, 20, 30, 40, and 50 past the hour"},
	{spec: "0-10,*/15 * * * *", want: "at minutes 0 thru 10, 15, 30, and 45 past the hour"},
	{spec: "1-5,3-7 * * * *", want: "at minutes 1 thru 7 past the hour"},
	{spec: "1,1 * * * *", want: "at 1 minute past the hour"},
	{spec: "30,0 * * * *", want: "at minutes 0 and 30 past the hour"},
	{spec: "* 1,1 * * *", want: "every minute during the 1 am hour"},
	{spec: "1,3,5,7,9,11,13,15 * * * *", want: "at minutes 1, 3, 5, … 15 past the hour (every 2 minutes, starting over each hour)"},
	{spec: "*/1,5 * * * *", want: "every minute"},
	{spec: "0 0-23 * * *", want: "at the top of every hour"},
	{spec: "0 */1,* * * *", want: "at the top of every hour"},
	{spec: "* 0-23 * * *", want: "every minute"},
	{spec: "*/15 0-23 * * *", want: "at minutes 0, 15, 30, and 45 past the hour"},

	// Clock times
	{spec: "5/15 2 * * *", want: "at 2:05 am, 2:20 am, 2:35 am, and 2:50 am every day"},
	{spec: "0 9-17 * * *", want: "at the top of every hour from 9 am thru 5 pm every day"},
	{spec: "0 22-2/2 * * *", want: "at midnight, 2 am, and 10 pm every day"},
	{spec: "0 0 * * *", want: "at midnight every day"},
	{spec: "0 12 * * *", want: "at noon every day"},
	{spec: "CRON_TZ=UTC 0 0 * * *", want: "at midnight every day"},
	{spec: "0 17,9 * * *", want: "at 9 am and 5 pm every day"},
	{spec: "0 0,12,13 * * *", want: "at midnight, noon, and 1 pm every day"},

	// Span form
	{spec: "*/7 9-17 * * *", want: "from 9 am thru 5:56 pm every day, at minutes 0, 7, 14, … 56 past each hour (every 7 minutes, starting over each hour)"},
	{spec: "0,30 9-17 * * *", want: "from 9 am thru 5:30 pm every day, at minutes 0 and 30 past each hour"},
	{spec: "*/30 0-5 * * *", want: "from midnight thru 5:30 am every day, at minutes 0 and 30 past each hour"},

	// Hour lists and steps
	{spec: "30 */2 * * *", want: "every 2 hours starting at 12:30 am in every day"},
	{spec: "15 9-23/2 * * *", want: "every 2 hours from 9:15 am thru 11:15 pm in every day"},
	{spec: "0,30 9,17 * * *", want: "at minutes 0 and 30 during the 9 am and 5 pm hours"},
	{spec: "0 */6 * * *", want: "at the top of midnight, 6 am, noon, and 6 pm hours in every day"},

	// Seconds
	{spec: "15 */4 * * * *", opts: []DescribeOption{withSeconds}, want: "at 15 seconds within minutes 0, 4, 8, … 56 in every hour (every 4 minutes)"},
	{spec: "*/20 30 * * * *", opts: []DescribeOption{withSeconds}, want: "at seconds 0, 20, and 40 within minute 30 in every hour"},
	{spec: "* * * * * *", opts: []DescribeOption{withSeconds}, want: "every second"},
	{spec: "* * 9 * * *", opts: []DescribeOption{withSeconds}, want: "every second during the 9 am hour"},
	{spec: "* 0-59 9 * * *", opts: []DescribeOption{withSeconds}, want: "every second during the 9 am hour"},
	{spec: "* 30 * * * *", opts: []DescribeOption{withSeconds}, want: "every second for minute 30 in every hour"},
	{spec: "5 * * * * *", opts: []DescribeOption{withSeconds}, want: "at 5 seconds within a minute"},
	{spec: "5 0 * * * *", opts: []DescribeOption{withSeconds}, want: "at 5 seconds past the top of every hour"},
	{spec: "15 30 2 * * *", opts: []DescribeOption{withSeconds}, want: "at 2:30:15 am every day"},

	// 12-hour details
	{spec: "30 0 * * *", want: "at 12:30 am every day"},
	{spec: "30 12 * * *", want: "at 12:30 pm every day"},
	{spec: "0 10,12,14 * * *", want: "at 10 am, noon, and 2 pm every day"},

	// Joins between fields: seconds, minutes, then hours
	{spec: "* */6 * * *", want: "every minute during midnight, 6 am, noon, and 6 pm hours in every day"},
	{spec: "* 9-17 * * *", want: "every minute from 9 am thru 5:59 pm every day"},
	{spec: "* 0-11 * * *", want: "every minute from midnight thru 11:59 am every day"},
	{spec: "* */2 * * *", want: "every minute every 2 hours starting with the midnight hour in every day"},
	{spec: "* 1/3 * * *", want: "every minute every 3 hours from the 1 am hour thru the 10 pm hour in every day"},
	{spec: "30 */6 * * *", want: "at 30 minutes past midnight, 6 am, noon, and 6 pm hours in every day"},
	{spec: "30 9-17 * * *", want: "at 30 minutes past every hour from 9 am thru 5 pm every day"},
	{spec: "30 0-11 * * *", want: "at 30 minutes past every hour from midnight thru 11 am every day"},
	{spec: "0,30 */6 * * *", want: "at minutes 0 and 30 during midnight, 6 am, noon, and 6 pm hours in every day"},
	{spec: "*/7 20-5 * * *", want: "from midnight thru 5:56 am and 8 pm thru 11:56 pm every day, at minutes 0, 7, 14, … 56 past each hour (every 7 minutes, starting over each hour)"},
	{spec: "*/15 */2 * * *", want: "at minutes 0, 15, 30, and 45 every 2 hours starting with the midnight hour in every day"},
	{spec: "0,30 1/3 * * *", want: "at minutes 0 and 30 every 3 hours from the 1 am hour thru the 10 pm hour in every day"},
	{spec: "5 * 9 * * *", opts: []DescribeOption{withSeconds}, want: "at 5 seconds within every minute during the 9 am hour"},
	{spec: "5 * 9-17 * * *", opts: []DescribeOption{withSeconds}, want: "at 5 seconds within every minute from 9 am thru 5:59 pm every day"},
	{spec: "5 0 */6 * * *", opts: []DescribeOption{withSeconds}, want: "at 5 seconds past the top of midnight, 6 am, noon, and 6 pm hours in every day"},
	{spec: "5 0 9-17 * * *", opts: []DescribeOption{withSeconds}, want: "at 5 seconds past the top of every hour from 9 am thru 5 pm every day"},
	{spec: "5 30 * * * *", opts: []DescribeOption{withSeconds}, want: "at 5 seconds within minute 30 in every hour"},
	{spec: "5 30 9-17 * * *", opts: []DescribeOption{withSeconds}, want: "at 5 seconds within minute 30 in every hour from 9 am thru 5 pm every day"},
	{spec: "5 0,30 9,17 * * *", opts: []DescribeOption{withSeconds}, want: "at 5 seconds within minutes 0 and 30 during the 9 am and 5 pm hours"},
	{spec: "5 */15 */2 * * *", opts: []DescribeOption{withSeconds}, want: "at 5 seconds within minutes 0, 15, 30, and 45 every 2 hours starting with the midnight hour in every day"},
	{spec: "*/30 * * * * *", opts: []DescribeOption{withSeconds}, want: "at seconds 0 and 30 within a minute"},
	{spec: "*/30 0 * * * *", opts: []DescribeOption{withSeconds}, want: "at seconds 0 and 30 past the top of every hour"},
	{spec: "*/30 0,30 * * * *", opts: []DescribeOption{withSeconds}, want: "at seconds 0 and 30 within minutes 0 and 30 in every hour"},
	{spec: "*/30 30 9 * * *", opts: []DescribeOption{withSeconds}, want: "at seconds 0 and 30 within minute 30 during the 9 am hour"},
	{spec: "*/30 0,30 9-17 * * *", opts: []DescribeOption{withSeconds}, want: "from 9 am thru 5:30:30 pm every day, at seconds 0 and 30 within minutes 0 and 30 past each hour"},
	{spec: "* * */6 * * *", opts: []DescribeOption{withSeconds}, want: "every second during midnight, 6 am, noon, and 6 pm hours in every day"},
	{spec: "* * 9-17 * * *", opts: []DescribeOption{withSeconds}, want: "every second from 9 am thru 5:59:59 pm every day"},
	{spec: "* 0,30 * * * *", opts: []DescribeOption{withSeconds}, want: "every second for minutes 0 and 30 in every hour"},
	{spec: "* 30 9 * * *", opts: []DescribeOption{withSeconds}, want: "every second for minute 30 during the 9 am hour"},
	{spec: "* 0 * * * *", opts: []DescribeOption{withSeconds}, want: "every second for minute 0 in every hour"},
	{spec: "* 30 9-17 * * *", opts: []DescribeOption{withSeconds}, want: "every second for minute 30 in every hour from 9 am thru 5 pm every day"},
	{spec: "*/30 0 9 * * *", opts: []DescribeOption{withSeconds}, want: "at seconds 0 and 30 past the top of the 9 am hour"},
	{spec: "*/30 0 */2 * * *", opts: []DescribeOption{withSeconds}, want: "at seconds 0 and 30 past the top of every 2 hours starting with the midnight hour in every day"},
	{spec: "*/7 */7 * * * *", opts: []DescribeOption{withSeconds}, want: "at seconds 0, 7, 14, … 56 within minutes 0, 7, 14, … 56 in every hour (every 7 seconds, starting over each minute; every 7 minutes, starting over each hour)"},

	// Wrapped hour spans: two parts in clock order (T9)
	{spec: "* 20-5 * * *", want: "every minute from midnight thru 5:59 am and 8 pm thru 11:59 pm every day"},
	{spec: "30 20-5 * * *", want: "at 30 minutes past every hour from midnight thru 5 am and 8 pm thru 11 pm every day"},
	{spec: "0 20-10/2 * * *", want: "every 2 hours from midnight thru 10 am and 8 pm thru 10 pm in every day"},
	{spec: "* 20-10/2 * * *", want: "every minute every 2 hours from the midnight hour thru the 10 am hour and the 8 pm hour thru the 10 pm hour in every day"},
	{spec: "0 22-10/2 * * *", want: "every 2 hours from midnight thru 10 am and at 10 pm in every day"},
	{spec: "0 4-0/2 * * *", want: "every 2 hours at midnight and from 4 am thru 10 pm in every day"},
	{spec: "* 22-10/2 * * *", want: "every minute every 2 hours from the midnight hour thru the 10 am hour and during the 10 pm hour in every day"},
	{spec: "* 4-0/2 * * *", want: "every minute every 2 hours during the midnight hour and from the 4 am hour thru the 10 pm hour in every day"},
	{spec: "30 23-5 * * *", want: "at 30 minutes past every hour from midnight thru 5 am and at 11:30 pm every day"},
	{spec: "30 18-0 * * *", want: "at 12:30 am and at 30 minutes past every hour from 6 pm thru 11 pm every day"},
	{spec: "0 5-0/3 * * *", want: "every 3 hours from 5 am thru 11 pm in every day"},
	{spec: "5 0 23-5 * * *", opts: []DescribeOption{withSeconds}, want: "at 5 seconds past the top of every hour from midnight thru 5 am and at 11:00:05 pm every day"},
	{spec: "5 0 18-0 * * *", opts: []DescribeOption{withSeconds}, want: "at 12:00:05 am and at 5 seconds past the top of every hour from 6 pm thru 11 pm every day"},

	// T9: a part of one hour under seconds, x to x reads x
	{spec: "5 30 23-5 * * *", opts: []DescribeOption{withSeconds}, want: "at 5 seconds within minute 30 in every hour from midnight thru 5 am and at 11:30:05 pm every day"},
	{spec: "5 30 18-0 * * *", opts: []DescribeOption{withSeconds}, want: "at 12:30:05 am and at 5 seconds within minute 30 in every hour from 6 pm thru 11 pm every day"},
	{spec: "*/30 0 18-0 * * *", opts: []DescribeOption{withSeconds}, want: "at seconds 0 and 30 past the top of the midnight hour and every hour from 6 pm thru 11 pm every day"},
	{spec: "*/30 0 23-5 * * *", opts: []DescribeOption{withSeconds}, want: "at seconds 0 and 30 past the top of every hour from midnight thru 5 am and the 11 pm hour every day"},
	{spec: "*/30 5 18-0 * * *", opts: []DescribeOption{withSeconds}, want: "at seconds 0 and 30 within minute 5 during the midnight hour and in every hour from 6 pm thru 11 pm every day"},
	{spec: "*/30 5 23-5 * * *", opts: []DescribeOption{withSeconds}, want: "at seconds 0 and 30 within minute 5 in every hour from midnight thru 5 am and during the 11 pm hour every day"},
	{spec: "* 59 6-0 * * *", opts: []DescribeOption{withSeconds}, want: "every second for minute 59 during the midnight hour and in every hour from 6 am thru 11 pm every day"},
	{spec: "* 0 23-5 * * *", opts: []DescribeOption{withSeconds}, want: "every second for minute 0 in every hour from midnight thru 5 am and during the 11 pm hour every day"},

	// T3 first: a field that covers its range is every value, however written
	{spec: "5 0-11,12-23 * * *", want: "at 5 minutes past the hour"},
	{spec: "0 0-11,12-23 * * *", want: "at the top of every hour"},
	{spec: "5 0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23 * * *", want: "at 5 minutes past the hour"},
	{spec: "5 1-0 * * *", want: "at 5 minutes past the hour"},

	// Literal fallback: the whole spec renders field by field
	{spec: "0 9 * * 1-5", want: "minute 0, hour 9, day of month *, month *, day of week 1-5", literal: true},
	{spec: "15 0 9 1 * *", opts: []DescribeOption{withSeconds}, want: "second 15, minute 0, hour 9, day of month 1, month *, day of week *", literal: true},
	{spec: "@daily", want: "@daily", literal: true},
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

// describeGrammar is the EBNF in docs/DESCRIBE.md as a regular expression,
// production by production. Every description outside the literal fallback
// must match it.
func describeGrammar() *regexp.Regexp {
	alt := func(s ...string) string { return "(?:" + strings.Join(s, "|") + ")" }
	opt := func(s string) string { return "(?:" + s + ")?" }
	// list{x} and list2{x}: the serial comma from three items.
	list2 := func(x string) string { return alt(x+" and "+x, x+"(?:, "+x+")+, and "+x) }
	list := func(x string) string { return alt(x, list2(x)) }

	num := `\d+`
	clock12 := alt(`(?:1[0-2]|[1-9])(?::[0-5]\d){1,2} [ap]m`, `(?:1[01]|[1-9]) [ap]m`)
	clock := alt(clock12, "midnight", "noon")
	vlist := alt(list(num+opt(" thru "+num)), num+", "+num+", "+num+", … "+num)
	unit := alt("second", "minute", "hour")
	units := alt("seconds", "minutes")
	// R3: no article before a list that starts with midnight
	hourtail := alt(" and "+clock, "(?:, "+clock+")+, and "+clock) + " hours"
	hours := alt("the "+clock+" hour", "the "+alt(clock12, "noon")+hourtail, "midnight"+hourtail)
	// T9: a wrapped range reads as two, in clock order
	hourrange := "from " + list(clock+" thru "+clock)
	everyday := " every day"
	inevery := " in every day"
	ival := "every " + alt(unit, num+" "+units) + opt(", starting over each "+unit)
	note := ` \(` + ival + "(?:; " + ival + `)*\)`
	hcrange := "the " + clock + " hour thru the " + clock + " hour"
	hone := "during the " + clock + " hour"
	hstep := "every " + num + " hours " + alt("starting with "+hours, "from "+list(hcrange),
		"from "+hcrange+" and "+hone, hone+" and from "+hcrange) + opt(inevery)

	// C1: what leads, with the minutes joined to the seconds
	secs := alt("at "+num+" seconds?", "at seconds "+vlist)
	inner := alt(
		secs+opt(" within "+alt("a minute", "every minute", "minute "+num, "minutes "+vlist)),
		"at minutes "+vlist,
		"every minute",
		"every second"+opt(" for "+alt("minute "+num, "minutes "+vlist)),
	)
	// R4, R5, R9, R10: one minute, any minute, on the hour forms
	top := alt("at the top of", "at "+num+" minutes? past", secs+" past the top of")
	crange := clock + " thru " + clock
	tophours := alt("the hour", "every hour"+opt(" "+hourrange)+opt(everyday), hours+opt(inevery), hstep,
		"every hour from "+crange+" and at "+clock+opt(everyday),
		"every hour from "+crange+" and the "+clock+" hour"+opt(everyday),
		"the "+clock+" hour and every hour from "+crange+opt(everyday))
	// R6, R9, R10, R11: the hours joined to what comes before
	hourjoin := alt("past the hour", "in every hour"+opt(" "+hourrange+opt(everyday)), "during "+hours+opt(inevery), hstep,
		"in every hour from "+crange+" and "+alt("at "+clock, hone)+opt(everyday),
		hone+" and in every hour from "+crange+opt(everyday))

	time := alt(
		// clocktime
		"at "+list(clock)+opt(everyday),
		// span
		hourrange+opt(everyday)+", "+inner+" past each hour",
		// hourstep
		"every "+num+" hours "+alt("starting at "+clock, hourrange,
			"from "+clock+" thru "+clock+" and at "+clock, "at "+clock+" and from "+clock+" thru "+clock)+opt(inevery),
		// parts
		inner+opt(" "+hourjoin),
		top+" "+tophours,
		"at "+clock+" and "+top+" every hour from "+crange+opt(everyday),
		"at "+clock+" and "+inner+" in every hour from "+crange+opt(everyday),
		"every "+alt("minute", "second")+" "+hourrange+opt(everyday),
		secs+" within every minute "+hourrange+opt(everyday),
	)
	return regexp.MustCompile("^" + time + opt(note) + "$")
}

func TestDescribeSpecGrammar(t *testing.T) {
	g := describeGrammar()
	for _, tt := range describeGoldens {
		if !tt.literal && !g.MatchString(tt.want) {
			t.Errorf("%q: description %q is outside the grammar", tt.spec, tt.want)
		}
	}
}

// TestDescribeGrammarRejects checks that the grammar refuses earlier
// wordings, so a description cannot drift back to them.
func TestDescribeGrammarRejects(t *testing.T) {
	g := describeGrammar()
	for _, s := range []string{
		"at 0, 15, 30 and 45 minutes past the hour",
		"at minutes 0, 15, 30 and 45 past the hour",
		"on the hour",
		"at 09:00 every day",
		"at 12 pm every day",
		"every minute for hour 9",
		"at 30 minutes past hours 0, 2, 4 and 6 in every day",
		"every minute during the midnight, 6 am, noon, and 6 pm hours in every day",
	} {
		if g.MatchString(s) {
			t.Errorf("grammar accepts %q", s)
		}
	}
}

func TestWithParseOptionsCombine(t *testing.T) {
	got, err := DescribeSpec("15 30 2 * * *",
		WithParseOptions(Second|Minute|Hour), WithParseOptions(Dom|Month|Dow))
	if err != nil {
		t.Fatal(err)
	}
	if want := "at 2:30:15 am every day"; got != want {
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
	if want := "at 2:30:15 am every day"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// YearOptional with no year written: the time-only description.
	got, err = FullParser().DescribeSpec("0 0 * * *")
	if err != nil {
		t.Fatal(err)
	}
	if want := "at midnight every day"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// SecondOptional: a second that was not written is not listed.
	got, err = FullParser().DescribeSpec("0 0 1 * *")
	if err != nil {
		t.Fatal(err)
	}
	if want := "minute 0, hour 0, day of month 1, month *, day of week *"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	got, err = FullParser().DescribeSpec("0 0 0 * * * 2027")
	if err != nil {
		t.Fatal(err)
	}
	if want := "second 0, minute 0, hour 0, day of month *, month *, day of week *, year 2027"; got != want {
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

// TestDescribeSpecStrictDaysNoKey checks that, with no hash key, a spec
// with H gets the description it gets without StrictDays, as the result would
// otherwise depend on the placeholder key, while a spec without H and a parser
// with a key keep the day check.
func TestDescribeSpecStrictDaysNoKey(t *testing.T) {
	strict := Minute | Hour | Dom | Month | Dow | Hash | StrictDays
	// "job-a" accepts the first spec; "job-a" and "job-b" both reject the second.
	for _, spec := range []string{"0 0 H(29-31) 2 *", "0 0 H(28-31) 2 *"} {
		want, err := DescribeSpec(spec, WithParseOptions(strict&^StrictDays))
		if err != nil {
			t.Fatal(err)
		}
		if got, err := DescribeSpec(spec, WithParseOptions(strict)); err != nil || got != want {
			t.Errorf("DescribeSpec(%q) with StrictDays = %q, %v; want %q, nil", spec, got, err, want)
		}
	}
	if _, err := DescribeSpec("0 0 30 2 *", WithParseOptions(strict)); err == nil {
		t.Error(`DescribeSpec("0 0 30 2 *") with StrictDays: want an error`)
	}
	if _, err := NewParser(strict).WithHashKey("job-a").DescribeSpec("0 0 H(28-31) 2 *"); err == nil {
		t.Error(`DescribeSpec("0 0 H(28-31) 2 *") with key "job-a" and StrictDays: want an error`)
	}
}

// literalForm matches the field-by-field fallback and bare descriptors.
var literalForm = regexp.MustCompile(`^(?:@|(?:second|minute|hour|day of month|month|day of week|year) )`)

// numberRange matches a range of numbers in a spec, such as a year range.
var numberRange = regexp.MustCompile(`(\d+)-(\d+)`)

// wideRange reports a range of more than 100 values. The parser lists every
// year of a year range, so a wide one spends the fuzz run in Parse; no other
// field has more than 60 values.
func wideRange(spec string) bool {
	for _, m := range numberRange.FindAllStringSubmatch(spec, -1) {
		lo, errLo := strconv.Atoi(m[1])
		hi, errHi := strconv.Atoi(m[2])
		if errLo != nil || errHi != nil || hi-lo > 100 {
			return true
		}
	}
	return false
}

// FuzzDescribeSpec checks that every spec that parses gets a description, and
// that it is in the grammar or the literal fallback. Specs with a range of
// more than 100 values (years) are skipped.
func FuzzDescribeSpec(f *testing.F) {
	for _, tt := range describeGoldens {
		f.Add(tt.spec)
	}
	f.Add("1,,2 * * * *")
	f.Add("0-59 0-23 * * *")

	g := describeGrammar()
	parsers := []Parser{
		NewParser(Minute | Hour | Dom | Month | Dow | Descriptor),
		NewParser(Second | Minute | Hour | Dom | Month | Dow | Descriptor),
		FullParser(),
	}
	f.Fuzz(func(t *testing.T, spec string) {
		if wideRange(spec) {
			t.Skip("range of more than 100 years")
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
		}
	})
}

// TestDescribeSpecRejected checks that a spec the parser rejects returns the
// parser's error, not a description.
func TestDescribeSpecRejected(t *testing.T) {
	for spec, want := range map[string]string{
		"0 *-5 * * *":   "cannot start a range",
		"*-/15 * * * *": "cannot start a range",
		"0 0 *-3 * *":   "cannot start a range",
		"0 0 ?-3 * 1":   "cannot start a range",
		"* , * * *":     "has no values",
		", * * * *":     "has no values",
		"0 0 , * 1":     "has no values",
		"0 0 , * ,":     "has no values",
		"0 0 1 , *":     "has no values",
		"0 0 1 * ,":     "has no values",
		"0 0 * * ,":     "has no values",
	} {
		if got, err := DescribeSpec(spec); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("DescribeSpec(%q) = %q, %v; want an error with %q", spec, got, err, want)
		}
	}
}
