// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cron

import (
	"regexp"
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
	{spec: "* 9 * * *", want: "every minute of hour 9"},

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

	// Span form: several minutes over an hour range
	{spec: "*/7 9-17 * * *", want: "from 09:00 thru 17:56 every day, at 0, 7, 14, … 56 minutes past each hour (every 7 minutes, starting over each hour)"},
	{spec: "*/7 9-17 * * *", opts: []DescribeOption{WithClockForm(Clock12)}, want: "from 9 am thru 5:56 pm every day, at 0, 7, 14, … 56 minutes past each hour (every 7 minutes, starting over each hour)"},
	{spec: "0,30 9-17 * * *", want: "from 09:00 thru 17:30 every day, at 0 and 30 minutes past each hour"},
	{spec: "*/30 0-5 * * *", opts: []DescribeOption{WithClockForm(Clock24Words)}, want: "from midnight thru 05:30 every day, at 0 and 30 minutes past each hour"},

	// Hour lists and steps
	{spec: "30 */2 * * *", want: "at 30 minutes past hours 0, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20 and 22 of every day"},
	{spec: "0,30 9,17 * * *", want: "at 0 and 30 minutes past hours 9 and 17"},

	// Seconds
	{spec: "15 */4 * * * *", opts: []DescribeOption{withSeconds}, want: "at 15 seconds past minutes 0, 4, 8, … 56 of every hour (every 4 minutes)"},
	{spec: "*/20 30 * * * *", opts: []DescribeOption{withSeconds}, want: "at 0, 20 and 40 seconds past minute 30 of every hour"},
	{spec: "* * * * * *", opts: []DescribeOption{withSeconds}, want: "every second"},
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

	// Not yet described: the whole spec renders literally
	{spec: "0 9 * * 1-5", want: "minute 0, hour 9, day of month *, month *, day of week 1-5", literal: true},
	{spec: "15 0 9 1 * *", opts: []DescribeOption{withSeconds}, want: "second 15, minute 0, hour 9, day of month 1, month *, day of week *", literal: true},
	{spec: "@daily", want: "@daily", literal: true},
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

// describeGrammar is the time part of the EBNF in docs/DESCRIBE.md as a
// regular expression. Every description outside the literal fallback must
// match it.
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
	unit := alt(alt(unitSg, unitPl)+" "+vlist, "every "+unitSg, "every day")
	tvals := vlist + " " + alt("seconds?", "minutes?")
	ival := "every " + alt(unitSg, num+" "+unitPl) +
		"(?:, starting over each " + alt("minute", "hour", "day") + ")?"
	note := ` \(` + ival + "(?:; " + ival + `)*\)`
	time := alt(
		"at "+andList(clock)+" every day",
		"on the hour",
		"from "+clock+" thru "+clock+" every day, at "+tvals+" past each hour(?:"+note+")?",
		"at "+tvals+" past "+alt("the "+alt("minute", "hour"), unit+"(?: of "+unit+")*")+"(?:"+note+")?",
		"every "+unitSg+"(?: of "+unit+")*(?:"+note+")?",
	)
	return regexp.MustCompile("^" + time + "$")
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
}
