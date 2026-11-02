// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cron

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// withSeconds parses six-field specs, seconds first.
var withSeconds = WithParseOptions(Second | Minute | Hour | Dom | Month | Dow | Descriptor)

// withDowOrDom ORs the day fields; withYear adds the year field.
var (
	withDowOrDom = WithParseOptions(Minute | Hour | Dom | Month | Dow | DowOrDom)
	withYear     = WithParseOptions(Minute | Hour | Dom | Month | Dow | Year)
)

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

	// Stars in ranges: the range reads as the star
	{spec: "0 *-5 * * *", want: "at the top of every hour"},
	{spec: "*-/15 * * * *", want: "at minutes 0, 15, 30, and 45 past the hour"},

	// A field of only commas selects no values
	{spec: "* , * * *", want: "never runs"},
	{spec: ", * * * *", want: "never runs"},

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

	// Date fields
	{spec: "0 0 * * 5", want: "at 12 am on Friday"},
	{spec: "0 22-2/2 * * 5", want: "at 12 am, 2 am, and 10 pm on Friday"},
	{spec: "0 9-17 * * 1-5", want: "at the top of every hour from 9 am thru 5 pm on Monday thru Friday"},
	{spec: "0 12 * * 1-5", want: "at noon on Monday thru Friday"},
	{spec: "15 0 9 1 * *", opts: []DescribeOption{withSeconds}, want: "at 9:00:15 am on the 1st of the month"},
	{spec: "30 */2 * * 1", want: "every 2 hours starting at 12:30 am on Monday"},
	{spec: "*/7 9-17 * * 1-5", want: "from 9 am thru 5:56 pm, at minutes 0, 7, 14, … 56 past each hour on Monday thru Friday (every 7 minutes, starting over each hour)"},
	{spec: "* * * 1 *", want: "every minute in January"},
	{spec: "0 * * * 1", want: "at the top of every hour on Monday"},
	{spec: "*/15 * 1 * *", want: "at minutes 0, 15, 30, and 45 past the hour on the 1st of the month"},
	{spec: "0 0 1,15 * *", want: "at 12 am on the 1st and 15th of the month"},
	{spec: "0 0 1-7 * *", want: "at 12 am on the 1st thru 7th of the month"},
	{spec: "0 0 11-13,22 * *", want: "at 12 am on the 11th thru 13th and 22nd of the month"},
	{spec: "0 0 1-28/7 * *", want: "at 12 am on the 1st, 8th, 15th, and 22nd of every month"},
	{spec: "0 0 1/10 * *", want: "at 12 am on the 1st, 11th, 21st, and 31st of the month; the 31st runs only in months that have one"},
	{spec: "0 0 */3 * *", want: "at 12 am on the 1st, 4th, 7th, 10th, 13th, 16th, 19th, 22nd, 25th, 28th, and 31st of the month; the 31st runs only in months that have one"},
	{spec: "0 0 */2 * *", want: "at 12 am on the 1st, 3rd, 5th, … 31st of the month (every 2 days, starting over each month); the 29th and 31st run only in months that have them"},
	{spec: "0 0 29 * *", want: "at 12 am on the 29th of the month; the 29th runs only in months that have one"},
	{spec: "0 0 30,31 * *", want: "at 12 am on the 30th and 31st of the month; the 30th and 31st run only in months that have them"},
	{spec: "0 0 * * 1,3,5", want: "at 12 am on Monday, Wednesday, and Friday"},
	{spec: "0 0 * * MON-FRI", want: "at 12 am on Monday thru Friday"},
	{spec: "0 0 * * 6-7", want: "at 12 am on Saturday and Sunday"},
	{spec: "0 0 * * 0,7", want: "at 12 am on Sunday"},
	{spec: "0 0 * * */2", want: "at 12 am on Sunday, Tuesday, Thursday, and Saturday"},
	{spec: "0 0 * * 5-1/2", want: "at 12 am on Friday and Sunday"},
	{spec: "0 0 * * 1/2", want: "at 12 am on Monday, Wednesday, Friday, and Sunday"},
	{spec: "0 0 * * 1/2", opts: []DescribeOption{WithParseOptions(Minute | Hour | Dom | Month | Dow | DowStepToSaturday)}, want: "at 12 am on Monday, Wednesday, and Friday"},
	{spec: "0 0 13 * 5", want: "at 12 am on the 13th of the month when it falls on Friday"},
	{spec: "0 0 1 * 1", opts: []DescribeOption{withDowOrDom}, want: "at 12 am on the 1st of the month or on any Monday"},
	{spec: "0 0 1,15 * 1-5", opts: []DescribeOption{withDowOrDom}, want: "at 12 am on the 1st and 15th of the month or on any Monday thru Friday"},
	{spec: "0 0 * 1-3 *", want: "at midnight every day in January thru March"},
	{spec: "0 0 1 1-3 *", want: "at 12 am on the 1st of the month in January thru March"},
	{spec: "0 0 1 */3 *", want: "at 12 am on the 1st of the month in January, April, July, and October"},
	{spec: "0 0 1 JAN,JUN *", want: "at 12 am on the 1st of the month in January and June"},
	{spec: "0 0 1 1 * 2026-2028", opts: []DescribeOption{withYear}, want: "at 12 am on the 1st of the month in January in years 2026 thru 2028"},
	{spec: "0 0 1 1 * *", opts: []DescribeOption{withYear}, want: "at 12 am on the 1st of the month in January"},
	{spec: "0 0 1 1 * 2027", opts: []DescribeOption{withYear}, want: "at 12 am on the 1st of the month in January in year 2027"},
	{spec: "0 0 1 1 * 2030,2026", opts: []DescribeOption{withYear}, want: "at 12 am on the 1st of the month in January in years 2026 and 2030"},
	{spec: "0 0 1 1 * 2026-2030/2", opts: []DescribeOption{withYear}, want: "at 12 am on the 1st of the month in January in years 2026, 2028, and 2030"},
	{spec: "0 0 *-3 * *", want: "at midnight every day"},
	{spec: "0 0 ?-3 * 1", want: "at 12 am on Monday"},
	{spec: "0 0 * *,3 *", want: "at midnight every day"},
	{spec: "0 0 *,1 * *", want: "at midnight every day"},
	{spec: "0 0 *,1 * 1", opts: []DescribeOption{withDowOrDom}, want: "at 12 am on Monday"},
	{spec: "0 0 1,* * 1", opts: []DescribeOption{WithParseOptions(Minute | Hour | Dom | Month | Dow | DowOrDom | StepWildcard)}, want: "at 12 am on Monday"},
	{spec: "0 0 1 * *,1", opts: []DescribeOption{withDowOrDom}, want: "at 12 am on the 1st of the month"},
	{spec: "0 0 13 * ?,5", opts: []DescribeOption{withDowOrDom}, want: "at 12 am on the 13th of the month"},
	{spec: "0 0 1 * */01", opts: []DescribeOption{withDowOrDom}, want: "at 12 am on the 1st of the month"},
	{spec: "0 0 , * 1", opts: []DescribeOption{withDowOrDom}, want: "at 12 am on Monday"},
	{spec: "0 0 1 * ,", opts: []DescribeOption{withDowOrDom}, want: "at 12 am on the 1st of the month"},
	{spec: "0 0 , * 1", want: "never runs"},
	{spec: "0 0 * * ,", want: "never runs"},
	{spec: "0 0 1 , *", want: "never runs"},
	{spec: "0 0 1 1 * ,", opts: []DescribeOption{withYear}, want: "never runs"},
	{spec: "* 9-17 * * 1-5", want: "every minute from 9 am thru 5:59 pm on Monday thru Friday"},
	{spec: "30 9-17 * * 1-5", want: "at 30 minutes past every hour from 9 am thru 5 pm on Monday thru Friday"},
	{spec: "0 */6 * * 1", want: "at the top of the 12 am, 6 am, noon, and 6 pm hours on Monday"},
	{spec: "*/15 */2 * * 1", want: "at minutes 0, 15, 30, and 45 every 2 hours starting with the 12 am hour on Monday"},
	{spec: "0 0 13 * 1-5", want: "at 12 am on the 13th of the month when it falls on Monday thru Friday"},
	{spec: "*/30 20-5 * * 1", want: "from 12 am thru 5:30 am and 8 pm thru 11:30 pm, at minutes 0 and 30 past each hour on Monday"},

	// Wrapped hour spans: two parts in clock order (T9)
	{spec: "* 20-5 * * *", want: "every minute from midnight thru 5:59 am and 8 pm thru 11:59 pm every day"},
	{spec: "30 20-5 * * *", want: "at 30 minutes past every hour from midnight thru 5 am and 8 pm thru 11 pm every day"},
	{spec: "0 20-10/2 * * *", want: "every 2 hours from midnight thru 10 am and 8 pm thru 10 pm in every day"},
	{spec: "* 20-10/2 * * *", want: "every minute every 2 hours from the midnight hour thru the 10 am hour and the 8 pm hour thru the 10 pm hour in every day"},

	// Literal fallback: the whole spec renders field by field
	{spec: "0 0 L * *", opts: []DescribeOption{WithParseOptions(Minute | Hour | Dom | Month | Dow | DomL)}, want: "minute 0, hour 0, day of month L, month *, day of week *", literal: true},
	{spec: "0 0 * * 5#3", opts: []DescribeOption{WithParseOptions(Minute | Hour | Dom | Month | Dow | DowNth)}, want: "minute 0, hour 0, day of month *, month *, day of week 5#3", literal: true},
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
	clock12 := alt(`(?:1[0-2]|[1-9])(?::[0-5]\d){1,2} [ap]m`, `(?:1[01]|[1-9]) [ap]m`, "12 am")
	clock := alt(clock12, "midnight", "noon")
	vlist := alt(list(num+opt(" thru "+num)), num+", "+num+", "+num+", … "+num)
	unit := alt("second", "minute", "hour", "day", "month", "year")
	units := alt("seconds", "minutes", "days", "months", "years")
	// R3: no article before a list that starts with midnight
	hourtail := alt(" and "+clock, "(?:, "+clock+")+, and "+clock) + " hours"
	hours := alt("the "+clock+" hour", "the "+alt(clock12, "noon")+hourtail, "midnight"+hourtail)
	// T9: a wrapped range reads as two, in clock order
	hourrange := "from " + list(clock+" thru "+clock)
	everyday := " every day"
	inevery := " in every day"
	ival := "every " + alt(unit, num+" "+units) + opt(", starting over each "+unit)
	note := ` \(` + ival + "(?:; " + ival + `)*\)`
	hstep := "every " + num + " hours " + alt("starting with "+hours, "from "+list("the "+clock+" hour thru the "+clock+" hour")) + opt(inevery)

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
	tophours := alt("the hour", "every hour"+opt(" "+hourrange)+opt(everyday), hours+opt(inevery), hstep)
	// R6, R9, R10, R11: the hours joined to what comes before
	hourjoin := alt("past the hour", "in every hour"+opt(" "+hourrange+opt(everyday)), "during "+hours+opt(inevery), hstep)

	time := alt(
		// clocktime
		"at "+list(clock)+opt(everyday),
		// span
		hourrange+opt(everyday)+", "+inner+" past each hour",
		// hourstep
		"every "+num+" hours "+alt("starting at "+clock, hourrange)+opt(inevery),
		// parts
		inner+opt(" "+hourjoin),
		top+" "+tophours,
		"every "+alt("minute", "second")+" "+hourrange+opt(everyday),
		secs+" within every minute "+hourrange+opt(everyday),
	)
	// D2, D4–D6: the date clauses; D3: the short-month clause
	ord := `\d+(?:st|nd|rd|th)`
	dayname := alt("Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday")
	monthname := alt("January", "February", "March", "April", "May", "June", "July",
		"August", "September", "October", "November", "December")
	olist := alt(list(ord+opt(" thru "+ord)), ord+", "+ord+", "+ord+", … "+ord)
	dom := "the " + olist + " of " + alt("the", "every") + " month"
	dowitem := dayname + opt(" thru "+dayname)
	dow := list(dowitem)
	andor := alt(" when it falls on "+dow, " or on "+list("any "+dowitem))
	day := alt("on "+dom+opt(andor), "on "+dow)
	month := "in " + list(monthname+opt(" thru "+monthname))
	year := alt("in year "+num, "in years "+vlist)
	short := "; the " + list(ord) + " " + alt("runs", "run") + " only in months that have " + alt("one", "them")

	schedule := time + opt(" "+day) + opt(" "+month) + opt(" "+year) + opt(note) + "(?:" + short + ")*"
	return regexp.MustCompile("^" + alt(schedule, "never runs") + "$")
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

// literalSyntax reports the syntax DescribeSpec renders literally: H in any
// field, L or W in the day of month, L or # in the day of week. An item that
// starts with "*" or "?" is the star, whatever follows it, as the parser
// reads it. It is written apart from the describer's own check, so a spec
// that falls back for another reason fails FuzzDescribeSpec.
func literalSyntax(fields []string, yearField string) bool {
	if yearField != "" {
		fields = append(slices.Clone(fields), yearField)
	}
	for i, f := range fields {
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
// descriptors and literalSyntax. Specs with a range of more than 100 values
// (years) are skipped.
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
	f.Add("0 0 1 1 0 22-99")

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
			_, descriptor, fields, yearField, _ := p.specFields(spec)
			if want := descriptor != "" || literalSyntax(fields, yearField); literalForm.MatchString(got) != want {
				t.Fatalf("DescribeSpec(%q) = %q: literal form %v, want %v", spec, got, !want, want)
			}
		}
	})
}
