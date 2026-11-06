// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cron

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// withSeconds parses six-field specs, seconds first.
var withSeconds = WithParseOptions(Second | Minute | Hour | Dom | Month | Dow | Descriptor)

// withDowOrDom ORs the day fields; withYear adds the year field.
var (
	withDowOrDom = WithParseOptions(Minute | Hour | Dom | Month | Dow | DowOrDom)
	withYear     = WithParseOptions(Minute | Hour | Dom | Month | Dow | Year)
	withExt      = WithParseOptions(Minute | Hour | Dom | Month | Dow | Extended)
	withExtOr    = WithParseOptions(Minute | Hour | Dom | Month | Dow | Extended | DowOrDom)
	withHash     = WithParseOptions(Minute | Hour | Dom | Month | Dow | Hash)
	withSecHash  = WithParseOptions(Second | Minute | Hour | Dom | Month | Dow | Hash)
	withHashOr   = WithParseOptions(Minute | Hour | Dom | Month | Dow | Hash | DowOrDom)
	withAlign    = WithParseOptions(Minute | Hour | Dom | Month | Dow | Descriptor | AlignEvery)
	withRoundUp  = WithParseOptions(Minute | Hour | Dom | Month | Dow | Descriptor | EveryRoundUp)
)

// minEvery is the standard parser with a minimum @every interval of d, and
// EveryRoundUp if roundUp.
func minEvery(d time.Duration, roundUp bool) *Parser {
	o := Minute | Hour | Dom | Month | Dow | Descriptor
	if roundUp {
		o |= EveryRoundUp
	}
	p := NewParser(o).WithMinEveryInterval(d)
	return &p
}

// describeGoldens are the binding descriptions (docs/DESCRIBE.md). literal
// marks the field-by-field fallback, which is outside the grammar. parser,
// when set, describes with Parser.DescribeSpec instead of the package function.
var describeGoldens = []struct {
	spec    string
	opts    []DescribeOption
	parser  *Parser
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
	{spec: "*/7 22-2 * * *", want: "at minutes 0, 7, 14, … 56 during the hours of midnight, 1 am, 2 am, 10 pm, and 11 pm (every 7 minutes, starting over each hour)"},
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
	{spec: "0,30 9,17 * * *", want: "at minutes 0 and 30 during the 9 am hour and the 5 pm hour"},
	{spec: "0 */6 * * *", want: "at the top of the hours of midnight, 6 am, noon, and 6 pm in every day"},

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
	{spec: "* */6 * * *", want: "every minute during the hours of midnight, 6 am, noon, and 6 pm in every day"},
	{spec: "* 9-17 * * *", want: "every minute from 9 am thru 5:59 pm every day"},
	{spec: "* 0-11 * * *", want: "every minute from midnight thru 11:59 am every day"},
	{spec: "* */2 * * *", want: "every minute every 2 hours starting with the midnight hour in every day"},
	{spec: "* 1/3 * * *", want: "every minute every 3 hours from the 1 am hour thru the 10 pm hour in every day"},
	{spec: "30 */6 * * *", want: "at 30 minutes past the hours of midnight, 6 am, noon, and 6 pm in every day"},
	{spec: "30 9-17 * * *", want: "at 30 minutes past every hour from 9 am thru 5 pm every day"},
	{spec: "30 0-11 * * *", want: "at 30 minutes past every hour from midnight thru 11 am every day"},
	{spec: "0,30 */6 * * *", want: "at minutes 0 and 30 during the hours of midnight, 6 am, noon, and 6 pm in every day"},
	{spec: "*/7 20-5 * * *", want: "from midnight thru 5:56 am and 8 pm thru 11:56 pm every day, at minutes 0, 7, 14, … 56 past each hour (every 7 minutes, starting over each hour)"},
	{spec: "*/15 */2 * * *", want: "at minutes 0, 15, 30, and 45 every 2 hours starting with the midnight hour in every day"},
	{spec: "0,30 1/3 * * *", want: "at minutes 0 and 30 every 3 hours from the 1 am hour thru the 10 pm hour in every day"},
	{spec: "5 * 9 * * *", opts: []DescribeOption{withSeconds}, want: "at 5 seconds within every minute during the 9 am hour"},
	{spec: "5 * 9-17 * * *", opts: []DescribeOption{withSeconds}, want: "at 5 seconds within every minute from 9 am thru 5:59 pm every day"},
	{spec: "5 0 */6 * * *", opts: []DescribeOption{withSeconds}, want: "at 5 seconds past the top of the hours of midnight, 6 am, noon, and 6 pm in every day"},
	{spec: "5 0 9-17 * * *", opts: []DescribeOption{withSeconds}, want: "at 5 seconds past the top of every hour from 9 am thru 5 pm every day"},
	{spec: "5 30 * * * *", opts: []DescribeOption{withSeconds}, want: "at 5 seconds within minute 30 in every hour"},
	{spec: "5 30 9-17 * * *", opts: []DescribeOption{withSeconds}, want: "at 5 seconds within minute 30 in every hour from 9 am thru 5 pm every day"},
	{spec: "5 0,30 9,17 * * *", opts: []DescribeOption{withSeconds}, want: "at 5 seconds within minutes 0 and 30 during the 9 am hour and the 5 pm hour"},
	{spec: "5 */15 */2 * * *", opts: []DescribeOption{withSeconds}, want: "at 5 seconds within minutes 0, 15, 30, and 45 every 2 hours starting with the midnight hour in every day"},
	{spec: "*/30 * * * * *", opts: []DescribeOption{withSeconds}, want: "at seconds 0 and 30 within a minute"},
	{spec: "*/30 0 * * * *", opts: []DescribeOption{withSeconds}, want: "at seconds 0 and 30 past the top of every hour"},
	{spec: "*/30 0,30 * * * *", opts: []DescribeOption{withSeconds}, want: "at seconds 0 and 30 within minutes 0 and 30 in every hour"},
	{spec: "*/30 30 9 * * *", opts: []DescribeOption{withSeconds}, want: "at seconds 0 and 30 within minute 30 during the 9 am hour"},
	{spec: "*/30 0,30 9-17 * * *", opts: []DescribeOption{withSeconds}, want: "from 9 am thru 5:30:30 pm every day, at seconds 0 and 30 within minutes 0 and 30 past each hour"},
	{spec: "* * */6 * * *", opts: []DescribeOption{withSeconds}, want: "every second during the hours of midnight, 6 am, noon, and 6 pm in every day"},
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
	{spec: "0 0 1,15 * *", want: "at midnight on the 1st and 15th of the month"},
	{spec: "0 0 1-7 * *", want: "at midnight on the 1st thru 7th of the month"},
	{spec: "0 0 11-13,22 * *", want: "at midnight on the 11th thru 13th and 22nd of the month"},
	{spec: "0 0 1-28/7 * *", want: "at midnight on the 1st, 8th, 15th, and 22nd of every month"},
	{spec: "0 0 1/10 * *", want: "at midnight on the 1st, 11th, 21st, and 31st of the month; the 31st runs only in months that have one"},
	{spec: "0 0 */3 * *", want: "at midnight on the 1st, 4th, 7th, 10th, 13th, 16th, 19th, 22nd, 25th, 28th, and 31st of the month; the 31st runs only in months that have one"},
	{spec: "0 0 */2 * *", want: "at midnight on the 1st, 3rd, 5th, … 31st of the month (every 2 days, starting over each month); the 29th and 31st run only in months that have them"},
	{spec: "0 0 29 * *", want: "at midnight on the 29th of the month; the 29th runs only in months that have one"},
	{spec: "0 0 30,31 * *", want: "at midnight on the 30th and 31st of the month; the 30th and 31st run only in months that have them"},
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
	{spec: "0 0 1 1-3 *", want: "at midnight on the 1st of the month in January thru March"},
	{spec: "0 0 1 */3 *", want: "at midnight on the 1st of the month in January, April, July, and October"},
	{spec: "0 0 1 JAN,JUN *", want: "at midnight on the 1st of the month in January and June"},
	{spec: "0 0 1 1 * 2026-2028", opts: []DescribeOption{withYear}, want: "at midnight on the 1st of the month in January in years 2026 thru 2028"},
	{spec: "0 0 1 1 * *", opts: []DescribeOption{withYear}, want: "at midnight on the 1st of the month in January"},
	{spec: "0 0 1 1 * 2027", opts: []DescribeOption{withYear}, want: "at midnight on the 1st of the month in January in year 2027"},
	{spec: "0 0 1 1 * 2030,2026", opts: []DescribeOption{withYear}, want: "at midnight on the 1st of the month in January in years 2026 and 2030"},
	{spec: "0 0 1 1 * 2026-2030/2", opts: []DescribeOption{withYear}, want: "at midnight on the 1st of the month in January in years 2026, 2028, and 2030"},
	{spec: "0 0 * *,3 *", want: "at midnight every day"},
	{spec: "0 0 *,1 * *", want: "at midnight every day"},
	{spec: "0 0 *,1 * 1", opts: []DescribeOption{withDowOrDom}, want: "at 12 am on Monday"},
	{spec: "0 0 1,* * 1", opts: []DescribeOption{WithParseOptions(Minute | Hour | Dom | Month | Dow | DowOrDom | StepWildcard)}, want: "at 12 am on Monday"},
	{spec: "0 9 */2 * MON", opts: []DescribeOption{WithParseOptions(Minute | Hour | Dom | Month | Dow | DowOrDom | StepWildcard)}, want: "at 9 am on the 1st, 3rd, 5th, … 31st of the month when it falls on Monday (every 2 days, starting over each month); the 29th and 31st run only in months that have them"},
	{spec: "0 0 1 * *,1", opts: []DescribeOption{withDowOrDom}, want: "at midnight on the 1st of the month"},
	{spec: "0 0 13 * ?,5", opts: []DescribeOption{withDowOrDom}, want: "at midnight on the 13th of the month"},
	{spec: "0 0 1 * */01", opts: []DescribeOption{withDowOrDom}, want: "at midnight on the 1st of the month"},
	{spec: "0 0 1-31 * 1", opts: []DescribeOption{withDowOrDom}, want: "at midnight every day"},
	{spec: "* 9-17 * * 1-5", want: "every minute from 9 am thru 5:59 pm on Monday thru Friday"},
	{spec: "30 9-17 * * 1-5", want: "at 30 minutes past every hour from 9 am thru 5 pm on Monday thru Friday"},
	{spec: "0 */6 * * 1", want: "at the top of the hours of 12 am, 6 am, noon, and 6 pm on Monday"},
	{spec: "*/15 */2 * * 1", want: "at minutes 0, 15, 30, and 45 every 2 hours starting with the 12 am hour on Monday"},
	{spec: "0 0 13 * 1-5", want: "at 12 am on the 13th of the month when it falls on Monday thru Friday"},
	{spec: "*/30 20-5 * * 1", want: "from 12 am thru 5:30 am and 8 pm thru 11:30 pm, at minutes 0 and 30 past each hour on Monday"},

	// Wrapped date ranges: days of the month low to high (T9); days of the
	// week and months unsplit, in week or year order (D4, D6)
	{spec: "0 0 25-5 * *", want: "at midnight on the 1st thru 5th and 25th thru 31st of the month; the 29th, 30th, and 31st run only in months that have them"},
	{spec: "0 0 25-5/2 * *", want: "at midnight on the 2nd, 4th, 25th, 27th, 29th, and 31st of the month; the 29th and 31st run only in months that have them"},
	{spec: "0 0 31-1 * *", want: "at midnight on the 1st and 31st of the month; the 31st runs only in months that have one"},
	{spec: "0 9 * * 5-1", want: "at 9 am on Friday thru Monday"},
	{spec: "0 9 * * 6-0", want: "at 9 am on Saturday and Sunday"},
	{spec: "0 0 1 11-2 *", want: "at midnight on the 1st of the month in November thru February"},
	{spec: "0 0 1 11-2/2 *", want: "at midnight on the 1st of the month in November and January"},
	{spec: "0 0 1 12-1 *", want: "at midnight on the 1st of the month in December and January"},

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
	{spec: "* 0 17-9 * * *", opts: []DescribeOption{withSeconds}, want: "every second for minute 0 in every hour from midnight thru 9 am and 5 pm thru 11 pm every day"},

	// T3 first: a field that covers its range is every value, however written
	{spec: "5 0-11,12-23 * * *", want: "at 5 minutes past the hour"},
	{spec: "0 0-11,12-23 * * *", want: "at the top of every hour"},
	{spec: "5 0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23 * * *", want: "at 5 minutes past the hour"},
	{spec: "5 1-0 * * *", want: "at 5 minutes past the hour"},

	// T3 first in the date fields
	{spec: "0 0 * * 0-6", want: "at midnight every day"},
	{spec: "0 9 * * 1-7", want: "at 9 am every day"},
	{spec: "0 9 * * 0-7", want: "at 9 am every day"},
	{spec: "0 0 * * 0,1,2,3,4,5,6", want: "at midnight every day"},
	{spec: "0 9 * * 1-0", want: "at 9 am every day"},
	{spec: "0 9 1-31 * 0-6", want: "at 9 am every day"},
	{spec: "0 0 15 * 0-6", want: "at midnight on the 15th of the month"},
	{spec: "0 0 15 * 0-6", opts: []DescribeOption{withDowOrDom}, want: "at midnight every day"},

	// G11: a day that none of the written months and years has
	{spec: "0 0 30 2 *", want: "never runs"},
	{spec: "0 0 31 4,6,9,11 *", want: "never runs"},
	{spec: "0 0 30,31 2 *", want: "never runs"},
	{spec: "0 0 29 2 * 2027", opts: []DescribeOption{withYear}, want: "never runs"},
	{spec: "0 0 30 2 1", want: "never runs"},
	{spec: "0 0 31W 4 *", opts: []DescribeOption{withExt}, want: "never runs"},
	{spec: "0 0 30W 2 *", opts: []DescribeOption{withExt}, want: "never runs"},
	{spec: "0 0 L-30 2 *", opts: []DescribeOption{withExt}, want: "never runs"},
	{spec: "0 0 L-29 2 *", opts: []DescribeOption{withExt}, want: "never runs"},
	{spec: "0 0 L-30 4,6 *", opts: []DescribeOption{withExt}, want: "never runs"},
	{spec: "0 0 1 * MON#2", opts: []DescribeOption{withExt}, want: "never runs"},
	{spec: "0 0 13 * FRI#3", opts: []DescribeOption{withExt}, want: "never runs"},
	{spec: "0 0 22 * MON#3", opts: []DescribeOption{withExt}, want: "never runs"},
	{spec: "0 0 L * MON#1", opts: []DescribeOption{withExt}, want: "never runs"},
	{spec: "0 0 * 2 MON#5 2027", opts: []DescribeOption{WithParseOptions(Minute | Hour | Dom | Month | Dow | Extended | Year)}, want: "never runs"},
	{spec: "0 0 H(1-7) * MON#2", opts: []DescribeOption{WithParseOptions(Minute | Hour | Dom | Month | Dow | Extended | Hash)}, want: "never runs"},
	{spec: "0 0 L * MON#4", opts: []DescribeOption{withExt}, want: "at 12 am on the last day of the month when it falls on the fourth Monday"},
	{spec: "0 0 * 2 MON#5", opts: []DescribeOption{withExt}, want: "at 12 am on the fifth Monday in February"},
	{spec: "0 0 29 2 * 2028", opts: []DescribeOption{withYear}, want: "at midnight on the 29th of the month in February in year 2028; the 29th runs only in months that have one"},
	{spec: "0 0 29 2 * 2027,2028", opts: []DescribeOption{withYear}, want: "at midnight on the 29th of the month in February in years 2027 and 2028; the 29th runs only in months that have one"},
	{spec: "0 0 30 2 1", opts: []DescribeOption{withDowOrDom}, want: "at 12 am on the 30th of the month or on any Monday in February; the 30th runs only in months that have one"},

	// D2: the values decide, step or list
	{spec: "0 0 1,8,15 * *", want: "at midnight on the 1st, 8th, and 15th of every month"},
	{spec: "0 0 1-28/14 * *", want: "at midnight on the 1st and 15th of the month"},
	// L, W and # (X1–X6)
	{spec: "0 0 L * *", opts: []DescribeOption{withExt}, want: "at midnight on the last day of the month"},
	{spec: "0 0 LW * *", opts: []DescribeOption{withExt}, want: "at midnight on the last weekday of the month"},
	{spec: "0 0 L-3 * *", opts: []DescribeOption{withExt}, want: "at midnight 3 days before the last day of the month"},
	{spec: "0 0 L-1 * *", opts: []DescribeOption{withExt}, want: "at midnight 1 day before the last day of the month"},
	{spec: "0 0 15W * *", opts: []DescribeOption{withExt}, want: "at midnight on the weekday nearest the 15th"},
	{spec: "0 0 31W * *", opts: []DescribeOption{withExt}, want: "at midnight on the weekday nearest the 31st; the 31st runs only in months that have one"},
	{spec: "0 0 1,15,L * *", opts: []DescribeOption{withExt}, want: "at midnight on the 1st, 15th, and last day of the month"},
	{spec: "0 0 L,1-7 * *", opts: []DescribeOption{withExt}, want: "at midnight on the 1st thru 7th and last day of the month"},
	{spec: "0 0 1,15,L-3 * *", opts: []DescribeOption{withExt}, want: "at midnight on the 1st and 15th and 3 days before the last day of the month"},
	{spec: "0 0 15W,L * *", opts: []DescribeOption{withExt}, want: "at midnight on the weekday nearest the 15th and the last day of the month"},
	{spec: "0 0 L,l * *", opts: []DescribeOption{withExt}, want: "at midnight on the last day of the month"},
	{spec: "0 0 L * 5", opts: []DescribeOption{withExt}, want: "at 12 am on the last day of the month when it falls on Friday"},
	{spec: "0 0 L-3 * 5", opts: []DescribeOption{withExt}, want: "at 12 am 3 days before the last day of the month when it falls on Friday"},
	{spec: "0 0 L * 5", opts: []DescribeOption{withExtOr}, want: "at 12 am on the last day of the month or on any Friday"},
	{spec: "0 0 L-27 * *", opts: []DescribeOption{withExt}, want: "at midnight 27 days before the last day of the month"},
	{spec: "0 0 L-28 * *", opts: []DescribeOption{withExt}, want: "at midnight 28 days before the last day of the month; it runs only in months of 29 days or more"},
	{spec: "0 0 L-29 * *", opts: []DescribeOption{withExt}, want: "at midnight 29 days before the last day of the month; it runs only in months of 30 days or more"},
	{spec: "0 0 L-30 * *", opts: []DescribeOption{withExt}, want: "at midnight 30 days before the last day of the month; it runs only in months of 31 days"},
	{spec: "0 0 L-30,l-+30 * *", opts: []DescribeOption{withExt}, want: "at midnight 30 days before the last day of the month; it runs only in months of 31 days"},
	{spec: "0 0 L-30 * 5", opts: []DescribeOption{withExt}, want: "at 12 am 30 days before the last day of the month when it falls on Friday; it runs only in months of 31 days"},
	{spec: "0 0 L-30 1-3 *", opts: []DescribeOption{withExt}, want: "at midnight 30 days before the last day of the month in January thru March; it runs only in months of 31 days"},
	{spec: "0 0 1,L-30 * *", opts: []DescribeOption{withExt}, want: "at midnight on the 1st and 30 days before the last day of the month; 30 days before the last day runs only in months of 31 days"},
	{spec: "0 0 L-28,29 * *", opts: []DescribeOption{withExt}, want: "at midnight on the 29th and 28 days before the last day of the month; the 29th runs only in months that have one; 28 days before the last day runs only in months of 29 days or more"},
	{spec: "0 0 L-29,L-30 * *", opts: []DescribeOption{withExt}, want: "at midnight 29 days before the last day and 30 days before the last day of the month; 29 days before the last day runs only in months of 30 days or more; 30 days before the last day runs only in months of 31 days"},
	{spec: "0 0 L-30 * 5", opts: []DescribeOption{withExtOr}, want: "at 12 am 30 days before the last day of the month or on any Friday; 30 days before the last day runs only in months of 31 days"},
	{spec: "0 0 * * FRI#3", opts: []DescribeOption{withExt}, want: "at 12 am on the third Friday"},
	{spec: "0 0 * * 5#L", opts: []DescribeOption{withExt}, want: "at 12 am on the last Friday"},
	{spec: "0 0 * * 7#1", opts: []DescribeOption{withExt}, want: "at 12 am on the first Sunday"},
	{spec: "0 0 * * MON,FRI#3", opts: []DescribeOption{withExt}, want: "at 12 am on Monday and the third Friday"},
	{spec: "0 0 * * FRI#3,5#3", opts: []DescribeOption{withExt}, want: "at 12 am on the third Friday"},
	{spec: "0 0 13 * FRI#2", opts: []DescribeOption{withExt}, want: "at 12 am on the 13th of the month when it falls on the second Friday"},
	{spec: "0 0 13 * MON,FRI#L", opts: []DescribeOption{withExtOr}, want: "at 12 am on the 13th of the month or on any Monday and any last Friday"},
	{spec: "0 0 LW,L * *", opts: []DescribeOption{withExt}, want: "at midnight on the last weekday and the last day of the month"},
	{spec: "0 0 1,LW * *", opts: []DescribeOption{withExt}, want: "at midnight on the 1st and the last weekday of the month"},
	{spec: "0 0 1,15W * *", opts: []DescribeOption{withExt}, want: "at midnight on the 1st and the weekday nearest the 15th of the month"},
	{spec: "0 0 1W,15W * *", opts: []DescribeOption{withExt}, want: "at midnight on the weekday nearest the 1st and the weekday nearest the 15th of the month"},
	{spec: "0 0 15W,L-3 * *", opts: []DescribeOption{withExt}, want: "at midnight on the weekday nearest the 15th and 3 days before the last day of the month"},
	{spec: "0 0 1,L,L-3 * *", opts: []DescribeOption{withExt}, want: "at midnight on the 1st and last day and 3 days before the last day of the month"},
	{spec: "0 0 L-3,L-1,L-2 * *", opts: []DescribeOption{withExt}, want: "at midnight 1 day before the last day, 2 days before the last day, and 3 days before the last day of the month"},
	{spec: "0 0 */2,L * *", opts: []DescribeOption{withExt}, want: "at midnight on the 1st, 3rd, 5th, … 31st and the last day of the month (every 2 days, starting over each month); the 29th and 31st run only in months that have them"},
	{spec: "0 0 15W * 5", opts: []DescribeOption{withExtOr}, want: "at 12 am on the weekday nearest the 15th or on any Friday"},
	{spec: "0 0 L-3 * FRI#3", opts: []DescribeOption{withExtOr}, want: "at 12 am 3 days before the last day of the month or on any third Friday"},
	{spec: "0 0 L-29,L-28,L-29 * *", opts: []DescribeOption{withExt}, want: "at midnight 28 days before the last day and 29 days before the last day of the month; 28 days before the last day runs only in months of 29 days or more; 29 days before the last day runs only in months of 30 days or more"},

	// H in the time fields (H1–H16)
	{spec: "H * * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute past the hour; the hash key fixes the minute"},
	{spec: "H(0-29) * * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute between minutes 0 and 29 past the hour; the hash key fixes the minute"},
	{spec: "H(50-10) * * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute past the hour, either between minutes 0 and 10 or between minutes 50 and 59; the hash key fixes the minute"},
	{spec: "H(0-59) * * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute past the hour; the hash key fixes the minute"},
	{spec: "0,H * * * *", opts: []DescribeOption{withHash}, want: "1 or 2 runs in every hour, at the top of the hour and a fixed minute past it; the hash key fixes the minute"},
	{spec: "H,H * * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute past the hour; the hash key fixes the minute"},
	{spec: "H 9 * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute during the 9 am hour every day; the hash key fixes the minute"},
	{spec: "0 H * * *", opts: []DescribeOption{withHash}, want: "at the top of a fixed hour every day; the hash key fixes the hour"},
	{spec: "H H * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute at a fixed hour every day; the hash key fixes the minute and the hour"},
	{spec: "0 H(9-17) * * *", opts: []DescribeOption{withHash}, want: "at the top of a fixed hour between the hours of 9 am and 5 pm every day; the hash key fixes the hour"},
	{spec: "H 9,17 * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute during the 9 am hour and the 5 pm hour every day; the hash key fixes the minute"},
	{spec: "H 9-17/2 * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute during the hours of 9 am, 11 am, 1 pm, 3 pm, and 5 pm every day; the hash key fixes the minute"},
	{spec: "0,30 H * * *", opts: []DescribeOption{withHash}, want: "at minutes 0 and 30 at a fixed hour every day; the hash key fixes the hour"},
	{spec: "0 9,H * * *", opts: []DescribeOption{withHash}, want: "1 or 2 runs in every day, at 9 am and the top of a fixed hour; the hash key fixes the hour"},
	{spec: "H 9 * * 1-5", opts: []DescribeOption{withHash}, want: "at a fixed minute during the 9 am hour on Monday thru Friday; the hash key fixes the minute"},
	{spec: "H 9 1 * *", opts: []DescribeOption{withHash}, want: "at a fixed minute during the 9 am hour on the 1st of the month; the hash key fixes the minute"},
	{spec: "H */2 * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute every 2 hours starting with the midnight hour in every day; the hash key fixes the minute"},
	{spec: "* H * * *", opts: []DescribeOption{withHash}, want: "every minute during a fixed hour; the hash key fixes the hour"},
	{spec: "* H/6 * * *", opts: []DescribeOption{withHash}, want: "every minute during 4 hours in every day, 6 hours apart, starting from a fixed hour; the hash key fixes the hour"},
	{spec: "H/15 * * * *", opts: []DescribeOption{withHash}, want: "4 runs in every hour, 15 minutes apart, starting from a fixed minute; the hash key fixes the minute"},
	{spec: "H/40 * * * *", opts: []DescribeOption{withHash}, want: "1 or 2 runs in every hour, 40 minutes apart, starting from a fixed minute; the hash key fixes the minute"},
	{spec: "H(0-29)/10 * * * *", opts: []DescribeOption{withHash}, want: "3 runs in every hour between minutes 0 and 29, 10 minutes apart, starting from a fixed minute; the hash key fixes the minute"},
	{spec: "H/15 9-17 * * *", opts: []DescribeOption{withHash}, want: "4 runs in every hour from 9 am thru 5 pm, 15 minutes apart, starting from a fixed minute; the hash key fixes the minute"},
	{spec: "0 H/6 * * *", opts: []DescribeOption{withHash}, want: "at the top of the hour, 4 runs in every day, 6 hours apart, starting from a fixed hour; the hash key fixes the hour"},
	{spec: "0 H/5 * * *", opts: []DescribeOption{withHash}, want: "at the top of the hour, 4 or 5 runs in every day, 5 hours apart, starting from a fixed hour; the hash key fixes the hour"},
	{spec: "0,H/15 * * * *", opts: []DescribeOption{withHash}, want: "4 or 5 runs in every hour, at the top of the hour and every 15 minutes starting from a fixed minute; the hash key fixes the minute"},
	{spec: "H,H(0-29) * * * *", opts: []DescribeOption{withHash}, want: "1 or 2 runs in every hour, at a fixed minute and a fixed minute between minutes 0 and 29; the hash key fixes both minutes"},
	{spec: "H * * * * *", opts: []DescribeOption{withSecHash}, want: "at a fixed second within a minute; the hash key fixes the second"},
	{spec: "H 0 9 * * *", opts: []DescribeOption{withSecHash}, want: "at a fixed second at 9 am every day; the hash key fixes the second"},
	{spec: "0 H 9 * * *", opts: []DescribeOption{withSecHash}, want: "at a fixed minute during the 9 am hour every day; the hash key fixes the minute"},
	{spec: "H H 9 * * *", opts: []DescribeOption{withSecHash}, want: "at a fixed second and a fixed minute during the 9 am hour every day; the hash key fixes the second and the minute"},
	{spec: "H/15 * * * * *", opts: []DescribeOption{withSecHash}, want: "4 runs in every minute, 15 seconds apart, starting from a fixed second; the hash key fixes the second"},
	{spec: "H/15 * 9 * * *", opts: []DescribeOption{withSecHash}, want: "4 runs in every minute during the 9 am hour, 15 seconds apart, starting from a fixed second; the hash key fixes the second"},
	{spec: "H/1 * * * *", opts: []DescribeOption{withHash}, want: "every minute"},
	{spec: "H(0-29)/1 * * * *", opts: []DescribeOption{withHash}, want: "at minutes 0 thru 29 past the hour"},
	{spec: "H/60 * * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute past the hour; the hash key fixes the minute"},
	{spec: "H/100 * * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute past the hour; the hash key fixes the minute"},
	{spec: "H(5-5) * * * *", opts: []DescribeOption{withHash}, want: "at 5 minutes past the hour"},
	{spec: "*,H * * * *", opts: []DescribeOption{withHash}, want: "every minute"},
	{spec: "H(0-29),H * * * *", opts: []DescribeOption{withHash}, want: "1 or 2 runs in every hour, at a fixed minute and a fixed minute between minutes 0 and 29; the hash key fixes both minutes"},
	{spec: "H 9-17 * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute past every hour from 9 am thru 5 pm every day; the hash key fixes the minute"},
	{spec: "H 20-5 * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute past every hour from midnight thru 5 am and 8 pm thru 11 pm every day; the hash key fixes the minute"},
	{spec: "H */6 * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute during the hours of midnight, 6 am, noon, and 6 pm in every day; the hash key fixes the minute"},
	{spec: "H 1/3 * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute every 3 hours from the 1 am hour thru the 10 pm hour in every day; the hash key fixes the minute"},
	{spec: "H(0-29) 9 * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute between minutes 0 and 29 during the 9 am hour every day; the hash key fixes the minute"},
	{spec: "30 H * * *", opts: []DescribeOption{withHash}, want: "at minute 30 at a fixed hour every day; the hash key fixes the hour"},
	{spec: "* H(9-17) * * *", opts: []DescribeOption{withHash}, want: "every minute during a fixed hour between the hours of 9 am and 5 pm; the hash key fixes the hour"},
	{spec: "*/7 H * * *", opts: []DescribeOption{withHash}, want: "at minutes 0, 7, 14, … 56 at a fixed hour every day (every 7 minutes, starting over each hour); the hash key fixes the hour"},
	{spec: "H(0-29) H(9-17) * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute between minutes 0 and 29 at a fixed hour between the hours of 9 am and 5 pm every day; the hash key fixes the minute and the hour"},
	{spec: "H/15 9,17 * * *", opts: []DescribeOption{withHash}, want: "4 runs in every hour during the 9 am hour and the 5 pm hour, 15 minutes apart, starting from a fixed minute; the hash key fixes the minute"},
	{spec: "H/15 */6 * * *", opts: []DescribeOption{withHash}, want: "4 runs in every hour during the hours of midnight, 6 am, noon, and 6 pm, 15 minutes apart, starting from a fixed minute; the hash key fixes the minute"},
	{spec: "H/15 20-5 * * *", opts: []DescribeOption{withHash}, want: "4 runs in every hour from midnight thru 5 am and 8 pm thru 11 pm, 15 minutes apart, starting from a fixed minute; the hash key fixes the minute"},
	{spec: "H(50-10)/5 * * * *", opts: []DescribeOption{withHash}, want: "4 or 5 runs in every hour between minutes 50 and 10, 5 minutes apart, starting from a fixed minute; the hash key fixes the minute"},
	{spec: "30 H/6 * * *", opts: []DescribeOption{withHash}, want: "at 30 minutes past the hour, 4 runs in every day, 6 hours apart, starting from a fixed hour; the hash key fixes the hour"},
	{spec: "* H/5 * * *", opts: []DescribeOption{withHash}, want: "every minute during 4 or 5 hours in every day, 5 hours apart, starting from a fixed hour; the hash key fixes the hour"},
	{spec: "H * 9 * * *", opts: []DescribeOption{withSecHash}, want: "at a fixed second within every minute during the 9 am hour; the hash key fixes the second"},
	{spec: "H * 9-17 * * *", opts: []DescribeOption{withSecHash}, want: "at a fixed second within every minute from 9 am thru 5:59 pm every day; the hash key fixes the second"},
	{spec: "H 30 9,17 * * *", opts: []DescribeOption{withSecHash}, want: "at a fixed second at 9:30 am and 5:30 pm every day; the hash key fixes the second"},
	{spec: "H(0-29) * * * * *", opts: []DescribeOption{withSecHash}, want: "at a fixed second between seconds 0 and 29 within a minute; the hash key fixes the second"},
	{spec: "H(0-29)/10 * * * * *", opts: []DescribeOption{withSecHash}, want: "3 runs in every minute between seconds 0 and 29, 10 seconds apart, starting from a fixed second; the hash key fixes the second"},
	{spec: "0 H/6 * * 1", opts: []DescribeOption{withHash}, want: "at the top of the hour, 4 runs in every day, 6 hours apart, starting from a fixed hour on Monday; the hash key fixes the hour"},
	{spec: "0 9,H * * 1", opts: []DescribeOption{withHash}, want: "1 or 2 runs in every day, at 9 am and the top of a fixed hour on Monday; the hash key fixes the hour"},

	// H in the date fields
	{spec: "0 0 H * *", opts: []DescribeOption{withHash}, want: "at midnight on a fixed day of the month; the hash key fixes the day"},
	{spec: "0 0 1,H * *", opts: []DescribeOption{withHash}, want: "at midnight on the 1st and a fixed day of the month; the hash key fixes the day"},
	{spec: "0 9 * * H", opts: []DescribeOption{withHash}, want: "at 9 am on a fixed day of the week; the hash key fixes the day"},
	{spec: "0 9 * * H(1-5)", opts: []DescribeOption{withHash}, want: "at 9 am on a fixed day of the week between Monday and Friday; the hash key fixes the day"},
	{spec: "0 0 1 H *", opts: []DescribeOption{withHash}, want: "at midnight on the 1st of the month in a fixed month; the hash key fixes the month"},
	{spec: "0 0 H(1-15) * *", opts: []DescribeOption{withHash}, want: "at midnight on a fixed day of the month between the 1st and 15th; the hash key fixes the day"},
	{spec: "0 0 H/10 * *", opts: []DescribeOption{withHash}, want: "at midnight on 2 to 4 days in every month, 10 days apart, starting from a fixed day; the hash key fixes the day"},
	{spec: "0 0 H/7 * *", opts: []DescribeOption{withHash}, want: "at midnight on 4 or 5 days in every month, 7 days apart, starting from a fixed day; the hash key fixes the day"},
	{spec: "0 0 H(1-15)/5 * *", opts: []DescribeOption{withHash}, want: "at midnight on 3 days in every month between the 1st and 15th, 5 days apart, starting from a fixed day; the hash key fixes the day"},
	{spec: "0 9 * * H/2", opts: []DescribeOption{withHash}, want: "at 9 am on 3 or 4 days in every week, 2 days apart, starting from a fixed day; the hash key fixes the day"},
	{spec: "0 9 * * H(1-5)/2", opts: []DescribeOption{withHash}, want: "at 9 am on 2 or 3 days in every week between Monday and Friday, 2 days apart, starting from a fixed day; the hash key fixes the day"},
	{spec: "0 9 * * H(5-7)", opts: []DescribeOption{withHash}, want: "at 9 am on a fixed day of the week between Friday and Sunday; the hash key fixes the day"},
	{spec: "0 9 * * H(0-6)", opts: []DescribeOption{withHash}, want: "at 9 am on a fixed day of the week; the hash key fixes the day"},
	{spec: "0 9 * * H/7", opts: []DescribeOption{withHash}, want: "at 9 am on a fixed day of the week; the hash key fixes the day"},
	{spec: "0 9 * * 1,H", opts: []DescribeOption{withHash}, want: "at 9 am on Monday and a fixed day of the week; the hash key fixes the day"},
	{spec: "0 0 1 H/3 *", opts: []DescribeOption{withHash}, want: "at midnight on the 1st of the month in 4 months in every year, 3 months apart, starting from a fixed month; the hash key fixes the month"},
	{spec: "0 0 1 H/5 *", opts: []DescribeOption{withHash}, want: "at midnight on the 1st of the month in 2 or 3 months in every year, 5 months apart, starting from a fixed month; the hash key fixes the month"},
	{spec: "0 0 1 H(1-6) *", opts: []DescribeOption{withHash}, want: "at midnight on the 1st of the month in a fixed month between January and June; the hash key fixes the month"},
	{spec: "0 0 1 H(1-6)/2 *", opts: []DescribeOption{withHash}, want: "at midnight on the 1st of the month in 3 months in every year between January and June, 2 months apart, starting from a fixed month; the hash key fixes the month"},
	{spec: "0 0 1 1,H *", opts: []DescribeOption{withHash}, want: "at midnight on the 1st of the month in January and a fixed month; the hash key fixes the month"},
	{spec: "0 0 H H *", opts: []DescribeOption{withHash}, want: "at midnight on a fixed day of the month in a fixed month; the hash key fixes the day and the month"},
	{spec: "H 9 H * *", opts: []DescribeOption{withHash}, want: "at a fixed minute during the 9 am hour on a fixed day of the month; the hash key fixes the minute and the day"},
	{spec: "0 0 H/1 * *", opts: []DescribeOption{withHash}, want: "at midnight every day"},
	{spec: "0 0 H * 1", opts: []DescribeOption{withHash}, want: "at 12 am on a fixed day of the month when it falls on Monday; the hash key fixes the day"},
	{spec: "0 0 1 * H", opts: []DescribeOption{withHash}, want: "at midnight on the 1st of the month when it falls on a fixed day of the week; the hash key fixes the day"},
	{spec: "0 0 H * 1", opts: []DescribeOption{withHashOr}, want: "at 12 am on a fixed day of the month or on any Monday; the hash key fixes the day"},
	{spec: "0 0 1 * H", opts: []DescribeOption{withHashOr}, want: "at midnight on the 1st of the month or on a fixed day of the week; the hash key fixes the day"},
	{spec: "0 0 H * H", opts: []DescribeOption{withHash}, want: "at midnight on a fixed day of the month when it falls on a fixed day of the week; the hash key fixes both days"},
	{spec: "0 9 H(1-15) * 1-5", opts: []DescribeOption{withHash}, want: "at 9 am on a fixed day of the month between the 1st and 15th when it falls on Monday thru Friday; the hash key fixes the day"},
	{spec: "0 0 H * 0-6", opts: []DescribeOption{withHash}, want: "at midnight on a fixed day of the month; the hash key fixes the day"},

	// Wrapped hashed ranges: a day of the month and minutes split low to
	// high, a part of one value read as that value; a day of the week and a
	// month read in week and year order (H3, H18, H19)
	{spec: "0 0 H(25-5) * *", opts: []DescribeOption{withHash}, want: "at midnight on a fixed day of the month between the 1st and 5th or the 25th and 31st; the hash key fixes the day"},
	{spec: "0 0 H(25-5)/7 * *", opts: []DescribeOption{withHash}, want: "at midnight on 0 to 2 days in every month between the 25th and 5th, 7 days apart, starting from a fixed day; the hash key fixes the day"},
	{spec: "0 9 * * H(5-1)", opts: []DescribeOption{withHash}, want: "at 9 am on a fixed day of the week between Friday and Monday; the hash key fixes the day"},
	{spec: "0 9 * * H(5-1)/2", opts: []DescribeOption{withHash}, want: "at 9 am on 2 days in every week between Friday and Monday, 2 days apart, starting from a fixed day; the hash key fixes the day"},
	{spec: "0 0 1 H(11-2) *", opts: []DescribeOption{withHash}, want: "at midnight on the 1st of the month in a fixed month between November and February; the hash key fixes the month"},
	{spec: "0 0 1 H(11-2)/2 *", opts: []DescribeOption{withHash}, want: "at midnight on the 1st of the month in 2 months in every year between November and February, 2 months apart, starting from a fixed month; the hash key fixes the month"},
	{spec: "H(59-5) * * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute past the hour, either between minutes 0 and 5 or minute 59; the hash key fixes the minute"},
	{spec: "H(1-0)/2 * * * *", opts: []DescribeOption{withHash}, want: "30 runs in every hour, 2 minutes apart, starting from a fixed minute; the hash key fixes the minute"},
	{spec: "0 9 * * H(1-0)", opts: []DescribeOption{withHash}, want: "at 9 am on a fixed day of the week; the hash key fixes the day"},
	{spec: "H(50-0) * * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute past the hour, either minute 0 or between minutes 50 and 59; the hash key fixes the minute"},
	{spec: "H(50-0) 9 * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute during the 9 am hour every day, either minute 0 or between minutes 50 and 59; the hash key fixes the minute"},
	{spec: "H(50-0) * H * *", opts: []DescribeOption{withHash}, want: "at a fixed minute, either minute 0 or between minutes 50 and 59, past the hour on a fixed day of the month; the hash key fixes the minute and the day"},
	{spec: "0 0 H(10-1) 2,4 *", opts: []DescribeOption{withHash}, want: "at midnight on a fixed day of the month in February and April, either the 1st or between the 10th and 31st; the hash key fixes the day"},
	{spec: "H(50-0) * 30 * *", opts: []DescribeOption{withHash}, want: "at a fixed minute past the hour on the 30th of the month, either minute 0 or between minutes 50 and 59; the 30th runs only in months that have one; the hash key fixes the minute"},
	{spec: "0 0 H(31-5) * *", opts: []DescribeOption{withHash}, want: "at midnight on a fixed day of the month, either between the 1st and 5th or on the 31st; the hash key fixes the day"},
	{spec: "0 0 H(10-1) * *", opts: []DescribeOption{withHash}, want: "at midnight on a fixed day of the month, either the 1st or between the 10th and 31st; the hash key fixes the day"},
	{spec: "0 9 * * H(6-1)", opts: []DescribeOption{withHash}, want: "at 9 am on a fixed day of the week between Saturday and Monday; the hash key fixes the day"},
	{spec: "0 9 * * H(6-0)", opts: []DescribeOption{withHash}, want: "at 9 am on a fixed day of the week between Saturday and Sunday; the hash key fixes the day"},
	{spec: "0 0 1 H(12-2) *", opts: []DescribeOption{withHash}, want: "at midnight on the 1st of the month in a fixed month between December and February; the hash key fixes the month"},

	// Descriptors: the cron equivalent's description (P2)
	{spec: "@yearly", want: "at midnight on the 1st of the month in January"},
	{spec: "@annually", want: "at midnight on the 1st of the month in January"},
	{spec: "@monthly", want: "at midnight on the 1st of the month"},
	{spec: "@weekly", want: "at 12 am on Sunday"},
	{spec: "@daily", want: "at midnight every day"},
	{spec: "@midnight", want: "at midnight every day"},
	{spec: "@hourly", want: "at the top of every hour"},
	{spec: "@triggered", want: "only when triggered"},
	{spec: "@manual", want: "only when triggered"},
	{spec: "@none", want: "only when triggered"},
	{spec: "CRON_TZ=Europe/Berlin @daily", want: "at midnight every day"},
	{spec: "@daily", opts: []DescribeOption{withSeconds}, want: "at midnight every day"},

	// Intervals: as they run, from their anchor (P3)
	{spec: "@every 1h30m", want: "every 1h30m from when scheduling started"},
	{spec: "@every 15m", opts: []DescribeOption{withAlign}, want: "every 15m from Unix epoch"},
	{spec: "@every_aligned 15m", want: "every 15m from Unix epoch"},
	{spec: "@every 500ms", opts: []DescribeOption{withRoundUp}, want: "every 1s from when scheduling started (written as 500ms, rounded up to the 1s minimum)"},
	{spec: "@every 30s", parser: minEvery(time.Minute, true), want: "every 1m from when scheduling started (written as 30s, rounded up to the 1m minimum)"},
	{spec: "@every 1.5s", want: "every 1s from when scheduling started (written as 1.5s, cut to whole seconds)"},
	{spec: "@every 01.s.1s", want: "every 1s from when scheduling started (written as 01.s.1s, cut to whole seconds)"},
	{spec: "@every 500ms", parser: minEvery(0, false), want: "every 500ms from when scheduling started"},
	{spec: "@every_start 15m", opts: []DescribeOption{withAlign}, want: "every 15m from when scheduling started"},
	{spec: "@every_aligned 1h", want: "every 1h from Unix epoch"},
	{spec: "@every 90m", want: "every 1h30m from when scheduling started"},
	{spec: "@every 1.7s", parser: minEvery(1500*time.Millisecond, false), want: "every 2s from when scheduling started (written as 1.7s, rounded up to whole seconds)"},
	{spec: "@every 500ms", parser: minEvery(1500*time.Millisecond, true), want: "every 2s from when scheduling started (written as 500ms, rounded up to the 1.5s minimum)"},
	{spec: "@every 0s", parser: minEvery(0, false), want: "every 1s from when scheduling started (written as 0s, rounded up to whole seconds)"},
	{spec: "@every -1h", parser: minEvery(0, false), want: "every 1s from when scheduling started (written as -1h, rounded up to whole seconds)"},

	// H21: several hashed items without a step
	{spec: "H,H(0-29) 9 * * *", opts: []DescribeOption{withHash}, want: "1 or 2 runs in every hour during the 9 am hour, at a fixed minute and a fixed minute between minutes 0 and 29; the hash key fixes both minutes"},
	{spec: "H,H(0-29) 9-17 * * *", opts: []DescribeOption{withHash}, want: "1 or 2 runs in every hour from 9 am thru 5 pm, at a fixed minute and a fixed minute between minutes 0 and 29; the hash key fixes both minutes"},
	{spec: "H(0-9),H(5-29) * * * *", opts: []DescribeOption{withHash}, want: "1 or 2 runs in every hour, at a fixed minute between minutes 0 and 9 and a fixed minute between minutes 5 and 29; the hash key fixes both minutes"},
	{spec: "H(0-9),H(20-29) * * * *", opts: []DescribeOption{withHash}, want: "2 runs in every hour, at a fixed minute between minutes 0 and 9 and a fixed minute between minutes 20 and 29; the hash key fixes both minutes"},
	{spec: "0 H,H(0-5) * * *", opts: []DescribeOption{withHash}, want: "1 or 2 runs in every day, at the top of a fixed hour and a fixed hour between the hours of midnight and 5 am; the hash key fixes both hours"},
	{spec: "0 H(20-23),H(1-5) * * *", opts: []DescribeOption{withHash}, want: "2 runs in every day, at the top of a fixed hour between the hours of 1 am and 5 am and a fixed hour between the hours of 8 pm and 11 pm; the hash key fixes both hours"},
	{spec: "H,H(0-29) * * * * *", opts: []DescribeOption{withSecHash}, want: "1 or 2 runs in every minute, at a fixed second and a fixed second between seconds 0 and 29; the hash key fixes both seconds"},
	// H22: a hashed hour step beside minutes
	{spec: "0,30 H/6 * * *", opts: []DescribeOption{withHash}, want: "at minutes 0 and 30 during 4 hours in every day, 6 hours apart, starting from a fixed hour; the hash key fixes the hour"},
	{spec: "*/15 H/6 * * *", opts: []DescribeOption{withHash}, want: "at minutes 0, 15, 30, and 45 during 4 hours in every day, 6 hours apart, starting from a fixed hour; the hash key fixes the hour"},
	{spec: "0-30 H/6 * * *", opts: []DescribeOption{withHash}, want: "at minutes 0 thru 30 during 4 hours in every day, 6 hours apart, starting from a fixed hour; the hash key fixes the hour"},
	{spec: "H H/6 * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute during 4 hours in every day, 6 hours apart, starting from a fixed hour; the hash key fixes the minute and the hour"},
	{spec: "H(0-29) H/6 * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute between minutes 0 and 29 during 4 hours in every day, 6 hours apart, starting from a fixed hour; the hash key fixes the minute and the hour"},
	// H23: a hashed minute step under an hour step past 6 values
	{spec: "H/15 */2 * * *", opts: []DescribeOption{withHash}, want: "4 runs in every hour, 15 minutes apart, starting from a fixed minute, every 2 hours starting with the midnight hour in every day; the hash key fixes the minute"},
	{spec: "H/7 */2 * * *", opts: []DescribeOption{withHash}, want: "8 or 9 runs in every hour, 7 minutes apart, starting from a fixed minute, every 2 hours starting with the midnight hour in every day; the hash key fixes the minute"},
	{spec: "H/40 */2 * * *", opts: []DescribeOption{withHash}, want: "1 or 2 runs in every hour, 40 minutes apart, starting from a fixed minute, every 2 hours starting with the midnight hour in every day; the hash key fixes the minute"},
	// H24: a hashed second beside a written minute or hour
	{spec: "H 0 * * * *", opts: []DescribeOption{withSecHash}, want: "at a fixed second past the top of every hour; the hash key fixes the second"},
	{spec: "H 5 * * * *", opts: []DescribeOption{withSecHash}, want: "at a fixed second within minute 5 in every hour; the hash key fixes the second"},
	{spec: "H 30 * * * *", opts: []DescribeOption{withSecHash}, want: "at a fixed second within minute 30 in every hour; the hash key fixes the second"},
	{spec: "H 0,30 * * * *", opts: []DescribeOption{withSecHash}, want: "at a fixed second within minutes 0 and 30 in every hour; the hash key fixes the second"},
	{spec: "H 30 9-17 * * *", opts: []DescribeOption{withSecHash}, want: "at a fixed second within minute 30 in every hour from 9 am thru 5 pm every day; the hash key fixes the second"},
	{spec: "H H * * * *", opts: []DescribeOption{withSecHash}, want: "at a fixed second within a fixed minute in every hour; the hash key fixes the second and the minute"},
	{spec: "H 0 H * * *", opts: []DescribeOption{withSecHash}, want: "at a fixed second at the top of a fixed hour every day; the hash key fixes the second and the hour"},
	{spec: "H/15 5 * * * *", opts: []DescribeOption{withSecHash}, want: "4 runs within minute 5 in every hour, 15 seconds apart, starting from a fixed second; the hash key fixes the second"},
	{spec: "H/15 0 * * * *", opts: []DescribeOption{withSecHash}, want: "4 runs past the top of every hour, 15 seconds apart, starting from a fixed second; the hash key fixes the second"},
	{spec: "H/15 * 9-17 * * *", opts: []DescribeOption{withSecHash}, want: "4 runs in every minute from 9 am thru 5:59 pm, 15 seconds apart, starting from a fixed second; the hash key fixes the second"},
	// Wrapped ranges in every field: seconds, and a hashed minute step on a wrapped hour step
	{spec: "50-10 * * * * *", opts: []DescribeOption{withSeconds}, want: "at seconds 0 thru 10 and 50 thru 59 within a minute"},
	{spec: "50-10/5 * * * * *", opts: []DescribeOption{withSeconds}, want: "at seconds 0, 5, 10, 50, and 55 within a minute"},
	{spec: "H(50-10) * * * * *", opts: []DescribeOption{withSecHash}, want: "at a fixed second within a minute, either between seconds 0 and 10 or between seconds 50 and 59; the hash key fixes the second"},
	{spec: "H(50-10)/15 * * * * *", opts: []DescribeOption{withSecHash}, want: "1 or 2 runs in every minute between seconds 50 and 10, 15 seconds apart, starting from a fixed second; the hash key fixes the second"},
	{spec: "H/15 20-10/2 * * *", opts: []DescribeOption{withHash}, want: "4 runs in every hour, 15 minutes apart, starting from a fixed minute, every 2 hours from the midnight hour thru the 10 am hour and the 8 pm hour thru the 10 pm hour in every day; the hash key fixes the minute"},

	// T9: a part of one hour under a hashed minute, x to x reads x
	{spec: "H 23-5 * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute past every hour from midnight thru 5 am and the 11 pm hour every day; the hash key fixes the minute"},
	{spec: "H 18-0 * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute past the midnight hour and every hour from 6 pm thru 11 pm every day; the hash key fixes the minute"},
	{spec: "H(0-29) 23-5 * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute between minutes 0 and 29 past every hour from midnight thru 5 am and the 11 pm hour every day; the hash key fixes the minute"},
	{spec: "H/15 23-5 * * *", opts: []DescribeOption{withHash}, want: "4 runs in every hour from midnight thru 5 am and during the 11 pm hour, 15 minutes apart, starting from a fixed minute; the hash key fixes the minute"},
	{spec: "H/15 18-0 * * *", opts: []DescribeOption{withHash}, want: "4 runs in every hour during the midnight hour and from 6 pm thru 11 pm, 15 minutes apart, starting from a fixed minute; the hash key fixes the minute"},
	{spec: "H H 23-5 * * *", opts: []DescribeOption{withSecHash}, want: "at a fixed second within a fixed minute in every hour from midnight thru 5 am and during the 11 pm hour every day; the hash key fixes the second and the minute"},
	{spec: "H H 7-0 * * *", opts: []DescribeOption{withSecHash}, want: "at a fixed second within a fixed minute during the midnight hour and in every hour from 7 am thru 11 pm every day; the hash key fixes the second and the minute"},

	{spec: "H,H(0-29) 2-0 * * *", opts: []DescribeOption{withHash}, want: "1 or 2 runs in every hour during the midnight hour and from 2 am thru 11 pm, at a fixed minute and a fixed minute between minutes 0 and 29; the hash key fixes both minutes"},
	{spec: "H,H(0-29) 23-5 * * *", opts: []DescribeOption{withHash}, want: "1 or 2 runs in every hour from midnight thru 5 am and during the 11 pm hour, at a fixed minute and a fixed minute between minutes 0 and 29; the hash key fixes both minutes"},

	// T3 and T11: a field that covers its range, beside H
	{spec: "0 9 * * 0-6,H", opts: []DescribeOption{withHash}, want: "at 9 am every day"},
	{spec: "0 9 * * 1-7,H", opts: []DescribeOption{withHash}, want: "at 9 am every day"},
	{spec: "0 9 * * SUN-SAT,H", opts: []DescribeOption{withHash}, want: "at 9 am every day"},
	{spec: "H 30 0-11,12-23 * * *", opts: []DescribeOption{withSecHash}, want: "at a fixed second within minute 30 in every hour; the hash key fixes the second"},

	// G11: the day the key picks decides, so H in a date field is not checked
	{spec: "0 0 H(30-31) 2 *", opts: []DescribeOption{withHash}, want: "never runs"},
	{spec: "0 0 H(30-31)/2 2 *", opts: []DescribeOption{withHash}, want: "never runs"},
	{spec: "0 0 31 H(4-4) *", opts: []DescribeOption{withHash}, want: "never runs"},
	{spec: "0 0 H(29-31) 2 * 2027", opts: []DescribeOption{withHash, withYear}, want: "never runs"},
	// G11: every H in a spec takes its value from one hash, so the fields move together
	{spec: "0 0 H(29-31) 1 H(1-3) 2028", opts: []DescribeOption{WithParseOptions(Minute | Hour | Dom | Month | Dow | Hash | Year)}, want: "never runs"},
	{spec: "0 0 H(30-31) H(2-3),H(2-2)/8191 H(5-6) 2028", opts: []DescribeOption{WithParseOptions(Minute | Hour | Dom | Month | Dow | Hash | Year)}, want: "never runs"},
	{spec: "0 0 L-3 H H(1-3) 2027", opts: []DescribeOption{WithParseOptions(Minute | Hour | Dom | Month | Dow | Extended | Hash | Year)}, want: "never runs"},
	{spec: "0 0 H(29-30) H(2-3) * 2027", opts: []DescribeOption{WithParseOptions(Minute | Hour | Dom | Month | Dow | Hash | Year)}, want: "at midnight on a fixed day of the month between the 29th and 30th in a fixed month between February and March in year 2027; the hash key fixes the day and the month"},
	{spec: "0 0 H(30-31) H(2-3) *", opts: []DescribeOption{withHash}, want: "at midnight on a fixed day of the month between the 30th and 31st in a fixed month between February and March; the hash key fixes the day and the month"},
	{spec: "0 0 H/10 4 *", opts: []DescribeOption{withHash}, want: "at midnight on 3 days in every month, 10 days apart, starting from a fixed day in April; the hash key fixes the day"},
	{spec: "0 0 H/10 2 *", opts: []DescribeOption{withHash}, want: "at midnight on 2 or 3 days in every month, 10 days apart, starting from a fixed day in February; the hash key fixes the day"},
	{spec: "0 0 H/15 1 *", opts: []DescribeOption{withHash}, want: "at midnight on 2 or 3 days in every month, 15 days apart, starting from a fixed day in January; the hash key fixes the day"},
	{spec: "0 0 H/30 2 *", opts: []DescribeOption{withHash}, want: "at midnight on 0 or 1 days in every month, 30 days apart, starting from a fixed day in February; the hash key fixes the day"},
	{spec: "0 0 H/30 4 *", opts: []DescribeOption{withHash}, want: "at midnight on 1 day in every month, 30 days apart, starting from a fixed day in April; the hash key fixes the day"},
	{spec: "0 0 H/2 2 *", opts: []DescribeOption{withHash}, want: "at midnight on 14 or 15 days in every month, 2 days apart, starting from a fixed day in February; the hash key fixes the day"},
	{spec: "0 0 H/10 2,4 *", opts: []DescribeOption{withHash}, want: "at midnight on 2 or 3 days in every month, 10 days apart, starting from a fixed day in February and April; the hash key fixes the day"},
	{spec: "0 0 H/28 2 * 2027", opts: []DescribeOption{withHash, withYear}, want: "at midnight on 1 day in every month, 28 days apart, starting from a fixed day in February in year 2027; the hash key fixes the day"},
	{spec: "0 0 H(28-30)/2 H * 2028", opts: []DescribeOption{withHash, withYear}, want: "at midnight on 1 or 2 days in every month between the 28th and 30th, 2 days apart, starting from a fixed day in a fixed month in year 2028; the hash key fixes the day and the month"},
	{spec: "0 0 H(28-30)/2 H * 2027", opts: []DescribeOption{withHash, withYear}, want: "at midnight on 0 to 2 days in every month between the 28th and 30th, 2 days apart, starting from a fixed day in a fixed month in year 2027; the hash key fixes the day and the month"},
	{spec: "0 0 H/10 H(4-6) *", opts: []DescribeOption{withHash}, want: "at midnight on 3 or 4 days in every month, 10 days apart, starting from a fixed day in a fixed month between April and June; the hash key fixes the day and the month"},
	{spec: "0 0 H/10 H(2-3) * 2028", opts: []DescribeOption{withHash, withYear}, want: "at midnight on 3 days in every month, 10 days apart, starting from a fixed day in a fixed month between February and March in year 2028; the hash key fixes the day and the month"},
	{spec: "0 0 H(29-31) 2 *", opts: []DescribeOption{withHash}, want: "at midnight on a fixed day of the month between the 29th and 31st in February; the hash key fixes the day"},
	{spec: "0 0 H(30-31) 2,4 *", opts: []DescribeOption{withHash}, want: "at midnight on a fixed day of the month between the 30th and 31st in February and April; the hash key fixes the day"},

	// H6: two hashed values of one unit merge in the hash clause
	{spec: "H 0 H * H", opts: []DescribeOption{withHash}, want: "at a fixed minute during the midnight hour on a fixed day of the month when it falls on a fixed day of the week; the hash key fixes the minute and both days"},
	{spec: "H,H(0-29) 0 H * *", opts: []DescribeOption{withHash}, want: "1 or 2 runs in every hour during the midnight hour, at a fixed minute and a fixed minute between minutes 0 and 29 on a fixed day of the month; the hash key fixes both minutes and the day"},
	{spec: "H H H * H", opts: []DescribeOption{withHash}, want: "at a fixed minute at a fixed hour on a fixed day of the month when it falls on a fixed day of the week; the hash key fixes the minute, the hour, and both days"},

	// D5, X4: the field as written decides the OR; a plain day takes "any"
	{spec: "0 0 1 * H,*/7", opts: []DescribeOption{WithParseOptions(Minute | Hour | Dom | Month | Dow | Hash | DowOrDom | StepWildcard)}, want: "at 12 am on the 1st of the month or on any Sunday and a fixed day of the week; the hash key fixes the day"},
	{spec: "0 0 1 * H,1", opts: []DescribeOption{withHashOr}, want: "at 12 am on the 1st of the month or on any Monday and a fixed day of the week; the hash key fixes the day"},
	{spec: "0 0 1 * H(1-5)", opts: []DescribeOption{withHashOr}, want: "at midnight on the 1st of the month or on a fixed day of the week between Monday and Friday; the hash key fixes the day"},

	// Limits: a form changes at a count, a range end or a month length, and
	// each row sits on one
	{spec: "0 0 */9 * *", want: "at midnight on the 1st, 10th, 19th, and 28th of every month"},
	{spec: "0 0 * * 6/1", opts: []DescribeOption{WithParseOptions(Minute | Hour | Dom | Month | Dow | DowStepToSaturday)}, want: "at 12 am on Saturday"},
	{spec: "*/10 * * * *", want: "at minutes 0, 10, 20, 30, 40, and 50 past the hour"},
	{spec: "* 0-5 * * *", want: "every minute during the hours of midnight, 1 am, 2 am, 3 am, 4 am, and 5 am"},
	{spec: "*/5 9-9 * * *", want: "at minutes 0, 5, 10, … 55 during the 9 am hour (every 5 minutes)"},
	{spec: "*/10 1 * * *", want: "at 1 am, 1:10 am, 1:20 am, 1:30 am, 1:40 am, and 1:50 am every day"},
	{spec: "0 0 H(1-31) * *", opts: []DescribeOption{withHash}, want: "at midnight on a fixed day of the month; the hash key fixes the day"},
	{spec: "0 0 * * H(2-2)", opts: []DescribeOption{withHash}, want: "at 12 am on Tuesday"},
	{spec: "0 0 H/3 12,H,H * 2032", opts: []DescribeOption{WithParseOptions(Minute | Hour | Dom | Month | Dow | Hash | Year)}, want: "at midnight on 10 or 11 days in every month, 3 days apart, starting from a fixed day in December and a fixed month in year 2032; the hash key fixes the day and the month"},
	{spec: "0 H(11-23),H(11-22) * * *", opts: []DescribeOption{withHash}, want: "1 or 2 runs in every day, at the top of a fixed hour between the hours of 11 am and 11 pm and a fixed hour between the hours of 11 am and 10 pm; the hash key fixes both hours"},
	{spec: "0 0 * H(12-10)/4 *", opts: []DescribeOption{withHash}, want: "at midnight every day in 2 or 3 months in every year between December and October, 4 months apart, starting from a fixed month; the hash key fixes the month"},
	{spec: "0 16,H * * *", opts: []DescribeOption{withHash}, want: "1 or 2 runs in every day, at 4 pm and the top of a fixed hour; the hash key fixes the hour"},
	{spec: "0 0 H/7 12 *", opts: []DescribeOption{withHash}, want: "at midnight on 4 or 5 days in every month, 7 days apart, starting from a fixed day in December; the hash key fixes the day"},
	{spec: "0 0 H(29-24)/7 12 *", opts: []DescribeOption{withHash}, want: "at midnight on 3 or 4 days in every month between the 29th and 24th, 7 days apart, starting from a fixed day in December; the hash key fixes the day"},
	{spec: "* */4 * * *", want: "every minute during the hours of midnight, 4 am, 8 am, noon, 4 pm, and 8 pm in every day"},
	{spec: "0 0 H/30 H/6 *", opts: []DescribeOption{withHash}, want: "at midnight on 1 or 2 days in every month, 30 days apart, starting from a fixed day in 2 months in every year, 6 months apart, starting from a fixed month; the hash key fixes the day and the month"},
	{spec: "0 0 H/10 H(4-6)/10 *", opts: []DescribeOption{withHash}, want: "at midnight on 3 days in every month, 10 days apart, starting from a fixed day in a fixed month between April and June; the hash key fixes the day and the month"},

	// Literal fallback: the whole spec renders field by field
	// H in the date fields without a form (H20)
	{spec: "0 0 H/10 * 1", opts: []DescribeOption{withHash}, want: "minute 0, hour 0, day of month H/10, month *, day of week 1", literal: true},
	{spec: "0 0 1 * H/2", opts: []DescribeOption{withHash}, want: "minute 0, hour 0, day of month 1, month *, day of week H/2", literal: true},
	{spec: "0 0 H * 0-6", opts: []DescribeOption{withHashOr}, want: "minute 0, hour 0, day of month H, month *, day of week 0-6", literal: true},
	{spec: "0 0 H(31-1) * *", opts: []DescribeOption{withHash}, want: "minute 0, hour 0, day of month H(31-1), month *, day of week *", literal: true},
	{spec: "0 0 1,H/10 * *", opts: []DescribeOption{withHash}, want: "minute 0, hour 0, day of month 1,H/10, month *, day of week *", literal: true},
	{spec: "0 0 1,15,H * *", opts: []DescribeOption{withHash}, want: "minute 0, hour 0, day of month 1,15,H, month *, day of week *", literal: true},
	{spec: "0 0 H,H(1-15) * *", opts: []DescribeOption{withHash}, want: "minute 0, hour 0, day of month H,H(1-15), month *, day of week *", literal: true},
	{spec: "0 0 L,H * *", opts: []DescribeOption{WithParseOptions(Minute | Hour | Dom | Month | Dow | Extended | Hash)}, want: "minute 0, hour 0, day of month L,H, month *, day of week *", literal: true},
	{spec: "0 0 * H/3,2 *", opts: []DescribeOption{withHash}, want: "minute 0, hour 0, day of month *, month H/3,2, day of week *", literal: true},
	// H in the time fields without a form (H17)
	{spec: "H(59-0) * * * *", opts: []DescribeOption{withHash}, want: "minute H(59-0), hour *, day of month *, month *, day of week *", literal: true},
	{spec: "H/15,H/20 * * * *", opts: []DescribeOption{withHash}, want: "minute H/15,H/20, hour *, day of month *, month *, day of week *", literal: true},
	{spec: "0 H(20-5) * * *", opts: []DescribeOption{withHash}, want: "minute 0, hour H(20-5), day of month *, month *, day of week *", literal: true},
	{spec: "5 H * * * *", opts: []DescribeOption{withSecHash}, want: "second 5, minute H, hour *, day of month *, month *, day of week *", literal: true},
	{spec: "H(0-29)/10 9 * * *", opts: []DescribeOption{withHash}, want: "minute H(0-29)/10, hour 9, day of month *, month *, day of week *", literal: true},
	{spec: "0,H 9 * * *", opts: []DescribeOption{withHash}, want: "minute 0,H, hour 9, day of month *, month *, day of week *", literal: true},
	{spec: "5,H * * * *", opts: []DescribeOption{withHash}, want: "minute 5,H, hour *, day of month *, month *, day of week *", literal: true},
	{spec: "30 9,H * * *", opts: []DescribeOption{withHash}, want: "minute 30, hour 9,H, day of month *, month *, day of week *", literal: true},
	{spec: "0 H(9-17)/2 * * *", opts: []DescribeOption{withHash}, want: "minute 0, hour H(9-17)/2, day of month *, month *, day of week *", literal: true},
	{spec: "H,5 0 * * * *", opts: []DescribeOption{withSecHash}, want: "second H,5, minute 0, hour *, day of month *, month *, day of week *", literal: true},
	{spec: "H,5 H * * *", opts: []DescribeOption{withHash}, want: "minute H,5, hour H, day of month *, month *, day of week *", literal: true},
	{spec: "H/15 * */2 * * *", opts: []DescribeOption{withSecHash}, want: "second H/15, minute *, hour */2, day of month *, month *, day of week *", literal: true},
	{spec: "H 30 H * * *", opts: []DescribeOption{withSecHash}, want: "second H, minute 30, hour H, day of month *, month *, day of week *", literal: true},
	{spec: "H H/15 * * * *", opts: []DescribeOption{withSecHash}, want: "second H, minute H/15, hour *, day of month *, month *, day of week *", literal: true},
	{spec: "H 5-10 9-17 * * *", opts: []DescribeOption{withSecHash}, want: "second H, minute 5-10, hour 9-17, day of month *, month *, day of week *", literal: true},
	{spec: "30 H(20-23),H(1-5) * * *", opts: []DescribeOption{withHash}, want: "minute 30, hour H(20-23),H(1-5), day of month *, month *, day of week *", literal: true},
	{spec: "0 H/6,9 * * *", opts: []DescribeOption{withHash}, want: "minute 0, hour H/6,9, day of month *, month *, day of week *", literal: true},
	{spec: "H(59-0) H/6 * * *", opts: []DescribeOption{withHash}, want: "minute H(59-0), hour H/6, day of month *, month *, day of week *", literal: true},
}

// describeNoteGoldens are the binding notes (docs/DESCRIBE.md, N1–N8).
var describeNoteGoldens = []struct {
	spec string
	opts []DescribeOption
	want string
}{
	{spec: "H/10 */7 * * * *", opts: []DescribeOption{WithParseOptions(Second | Minute | Hour | Dom | Month | Dow | Hash)}, want: "at seconds h, h+10, h+20, h+30, h+40, and h+50 within minutes 0, 7, 14, … 56 in every hour (every 7 minutes, starting over each hour), where h is a fixed second from 0 to 9"},
	{spec: "H/10 */4 * * * *", opts: []DescribeOption{WithParseOptions(Second | Minute | Hour | Dom | Month | Dow | Hash)}, want: "at seconds h, h+10, h+20, h+30, h+40, and h+50 within minutes 0, 4, 8, … 56 in every hour (every 4 minutes), where h is a fixed second from 0 to 9"},
	{spec: "*/7 H/6 * * *", opts: []DescribeOption{withHash}, want: "at minutes 0, 7, 14, … 56 during a fixed hour between midnight and 5 am and every 6 hours after it (every 7 minutes, starting over each hour)"},
	{spec: "H/15 * * * *", opts: []DescribeOption{withHash}, want: "at minutes h, h+15, h+30, and h+45 past the hour, where h is a fixed minute from 0 to 14"},
	{spec: "H/5 * * * *", opts: []DescribeOption{withHash}, want: "at minutes h, h+5, h+10, … h+55 past the hour, where h is a fixed minute from 0 to 4"},
	{spec: "H/7 * * * *", opts: []DescribeOption{withHash}, want: "at minutes h, h+7, h+14, … h+56 past the hour, where h is a fixed minute from 0 to 6; h+56 runs only when h is below 4"},
	{spec: "H/40 * * * *", opts: []DescribeOption{withHash}, want: "at minutes h and h+40 past the hour, where h is a fixed minute from 0 to 39; h+40 runs only when h is below 20"},
	{spec: "H/15 9 * * *", opts: []DescribeOption{withHash}, want: "at minutes h, h+15, h+30, and h+45 during the 9 am hour, where h is a fixed minute from 0 to 14"},
	{spec: "H/15 9,17 * * *", opts: []DescribeOption{withHash}, want: "at minutes h, h+15, h+30, and h+45 during the 9 am hour and the 5 pm hour, where h is a fixed minute from 0 to 14"},
	{spec: "H/15 9,13,17 * * *", opts: []DescribeOption{withHash}, want: "at minutes h, h+15, h+30, and h+45 during the hours of 9 am, 1 pm, and 5 pm, where h is a fixed minute from 0 to 14"},
	{spec: "H/15 9-17 * * *", opts: []DescribeOption{withHash}, want: "at minutes h, h+15, h+30, and h+45 past every hour from 9 am thru 5 pm, where h is a fixed minute from 0 to 14"},
	{spec: "H/20 * * * * *", opts: []DescribeOption{withSecHash}, want: "at seconds h, h+20, and h+40 within a minute, where h is a fixed second from 0 to 19"},
	{spec: "0 H/6 * * *", opts: []DescribeOption{withHash}, want: "at the top of a fixed hour between midnight and 5 am and every 6 hours after it"},
	{spec: "0 H(9-17)/4 * * *", opts: []DescribeOption{withHash}, want: "at the top of a fixed hour between 9 am and noon and every 4 hours after it thru 5 pm"},
	{spec: "0 9 * * H/2", opts: []DescribeOption{withHash}, want: "on Sunday, Tuesday, Thursday, and Saturday; or Monday, Wednesday, and Friday, depending on the hash key"},
	{spec: "0 9 * * H/3", opts: []DescribeOption{withHash}, want: "on Sunday, Wednesday, and Saturday; Monday and Thursday; or Tuesday and Friday, depending on the hash key"},
	{spec: "0 9 * * H/4", opts: []DescribeOption{withHash}, want: "on Sunday and Thursday; Monday and Friday; Tuesday and Saturday; or Wednesday, depending on the hash key"},
	{spec: "0 9 * * H(1-5)/2", opts: []DescribeOption{withHash}, want: "on Monday, Wednesday, and Friday; or Tuesday and Thursday, depending on the hash key"},
	{spec: "0 9 * * H(5-1)/2", opts: []DescribeOption{withHash}, want: "on Friday and Sunday; or Saturday and Monday, depending on the hash key"},
	{spec: "H/15 23-5 * * *", opts: []DescribeOption{withHash}, want: "at minutes h, h+15, h+30, and h+45 past every hour from midnight thru 5 am and the 11 pm hour, where h is a fixed minute from 0 to 14"},
	{spec: "H/15 18-0 * * *", opts: []DescribeOption{withHash}, want: "at minutes h, h+15, h+30, and h+45 past the midnight hour and every hour from 6 pm thru 11 pm, where h is a fixed minute from 0 to 14"},
	// N4: a step that does not divide the whole range walks from its start
	{spec: "0 9 * * H(1-7)/2", opts: []DescribeOption{withHash}, want: "on Monday, Wednesday, Friday, and Sunday; or Tuesday, Thursday, and Saturday, depending on the hash key"},
	{spec: "0 9 * * H(1-7)/3", opts: []DescribeOption{withHash}, want: "on Monday, Thursday, and Sunday; Tuesday and Friday; or Wednesday and Saturday, depending on the hash key"},
	{spec: "0 9 * * H(5-4)/2", opts: []DescribeOption{withHash}, want: "on Friday, Sunday, Tuesday, and Thursday; or Saturday, Monday, and Wednesday, depending on the hash key"},
	{spec: "0 0 1 H(2-1)/5 *", opts: []DescribeOption{withHash}, want: "in February, July, and December; March, August, and January; April and September; May and October; or June and November, depending on the hash key"},
	{spec: "H(1-0)/15 * * * *", opts: []DescribeOption{withHash}, want: "at minutes h, h+15, h+30, and h+45 past the hour, where h is a fixed minute from 0 to 14"},
	{spec: "0 H(5-4)/7 * * *", opts: []DescribeOption{withHash}, want: "at the top of a fixed hour between 5 am and 11 am and every 7 hours after it thru 4 am"},
	{spec: "0 H(1-0)/5 * * *", opts: []DescribeOption{withHash}, want: "at the top of a fixed hour between 1 am and 5 am and every 5 hours after it thru midnight"},
	{spec: "H(5-4)/7 * * * *", opts: []DescribeOption{withHash}, want: ""},
	{spec: "0 0 1 H/2 *", opts: []DescribeOption{withHash}, want: "in January, March, May, July, September, and November; or February, April, June, August, October, and December, depending on the hash key"},
	{spec: "0 0 1 H/3 *", opts: []DescribeOption{withHash}, want: "in January, April, July, and October; February, May, August, and November; or March, June, September, and December, depending on the hash key"},
	{spec: "0 0 1 H/4 *", opts: []DescribeOption{withHash}, want: "in months h, h+4, and h+8, where h is a fixed month from 1 (January) to 4 (April)"},
	{spec: "0 0 1 H/5 *", opts: []DescribeOption{withHash}, want: "in months h, h+5, and h+10, where h is a fixed month from 1 (January) to 5 (May); h+10 runs only when h is below 3"},
	{spec: "0 0 1 H(11-2)/2 *", opts: []DescribeOption{withHash}, want: "in November and January; or December and February, depending on the hash key"},
	{spec: "0 0 H/2 * *", opts: []DescribeOption{withHash}, want: "on the 1st, 3rd, 5th, … 31st; or the 2nd, 4th, 6th, … 30th of every month, depending on the hash key"},
	{spec: "0 0 H/10 * *", opts: []DescribeOption{withHash}, want: "on days h, h+10, h+20, and h+30 of every month, where h is a fixed day from 1 to 10; h+30 runs only when h is below 2"},
	{spec: "0 0 H(1-28)/7 * *", opts: []DescribeOption{withHash}, want: "on days h, h+7, h+14, and h+21 of every month, where h is a fixed day from 1 to 7"},
	{spec: "0 0 H(25-5)/7 * *", opts: []DescribeOption{withHash}, want: "on the 25th and 1st; the 26th and 2nd; the 27th and 3rd; the 28th and 4th; the 29th and 5th; the 30th; or the 31st of every month, depending on the hash key; a short month skips the days it lacks and changes the last step"},
	{spec: "0 0 H * *", opts: []DescribeOption{withHash}, want: "on day h of every month, where h is a fixed day from 1 to 31; a month without day h is skipped"},
	{spec: "0 0 1,H * *", opts: []DescribeOption{withHash}, want: "on the 1st and day h of every month, where h is a fixed day from 1 to 31; a month without day h runs on the 1st only"},
	{spec: "0 0 28,H * *", opts: []DescribeOption{withHash}, want: "on the 28th and day h of every month, where h is a fixed day from 1 to 31; a month without day h runs on the 28th only"},
	{spec: "0 0 29,H * *", opts: []DescribeOption{withHash}, want: "on the 29th and day h of every month, where h is a fixed day from 1 to 31; a month without day h runs only on the 29th, if it has one"},
	{spec: "0 0 30,H * *", opts: []DescribeOption{withHash}, want: "on the 30th and day h of every month, where h is a fixed day from 1 to 31; a month without day h runs only on the 30th, if it has one"},
	{spec: "0 0 31,H * *", opts: []DescribeOption{withHash}, want: "on the 31st and day h of every month, where h is a fixed day from 1 to 31; a month without day h is skipped"},
	{spec: "0 0 30,H 4,6 *", opts: []DescribeOption{withHash}, want: "on the 30th and day h of the month in April and June, where h is a fixed day from 1 to 31; a month without day h runs on the 30th only"},
	{spec: "0 0 30,H 2,4 *", opts: []DescribeOption{withHash}, want: "on the 30th and day h of the month in February and April, where h is a fixed day from 1 to 31; a month without day h runs only on the 30th, if it has one"},
	{spec: "0 0 31,H 1,4 *", opts: []DescribeOption{withHash}, want: "on the 31st and day h of the month in January and April, where h is a fixed day from 1 to 31; a month without day h is skipped"},
	{spec: "0 0 H(1-15) * *", opts: []DescribeOption{withHash}, want: "on day h of every month, where h is a fixed day between 1 and 15"},
	{spec: "0 0 H(25-5) * *", opts: []DescribeOption{withHash}, want: "on day h of every month, where h is a fixed day between 1 and 5 or 25 and 31; a month without day h is skipped"},
	{spec: "0 0 H(10-1) * *", opts: []DescribeOption{withHash}, want: "on day h of every month, where h is a fixed day, either day 1 or between days 10 and 31; a month without day h is skipped"},
	{spec: "H/30 0 * * H/2", opts: []DescribeOption{withHash}, want: "at minutes h and h+30 during the midnight hour, where h is a fixed minute from 0 to 29, on Sunday, Tuesday, Thursday, and Saturday; or Monday, Wednesday, and Friday, depending on the hash key"},
	{spec: "0 0 H/10 H/4 *", opts: []DescribeOption{withHash}, want: "on days h, h+10, h+20, and h+30 of the month, where h is a fixed day from 1 to 10; h+30 runs only when h is below 2, in months k, k+4, and k+8, where k is a fixed month from 1 (January) to 4 (April)"},
	{spec: "0 0 H(31-5) * *", opts: []DescribeOption{withHash}, want: "on day h of every month, where h is a fixed day, either between days 1 and 5 or day 31; a month without day h is skipped"},
	// N8: the minutes of a hashed hour step; N2 in more hour contexts (R11,
	// R12, C1); N2, N4: a wrapped hashed range lists; N7 over a wrapped range
	{spec: "30 H/6 * * *", opts: []DescribeOption{withHash}, want: "at 30 minutes past a fixed hour between midnight and 5 am and every 6 hours after it"},
	{spec: "* H/6 * * *", opts: []DescribeOption{withHash}, want: "every minute during a fixed hour between midnight and 5 am and every 6 hours after it"},
	{spec: "0,30 H/6 * * *", opts: []DescribeOption{withHash}, want: "at minutes 0 and 30 during a fixed hour between midnight and 5 am and every 6 hours after it"},
	{spec: "*/15 H/6 * * *", opts: []DescribeOption{withHash}, want: "at minutes 0, 15, 30, and 45 during a fixed hour between midnight and 5 am and every 6 hours after it"},
	{spec: "0-30 H/6 * * *", opts: []DescribeOption{withHash}, want: "at minutes 0 thru 30 during a fixed hour between midnight and 5 am and every 6 hours after it"},
	{spec: "H H/6 * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute during a fixed hour between midnight and 5 am and every 6 hours after it"},
	{spec: "H(0-29) H/6 * * *", opts: []DescribeOption{withHash}, want: "at a fixed minute between minutes 0 and 29 during a fixed hour between midnight and 5 am and every 6 hours after it"},
	{spec: "H/15 */2 * * *", opts: []DescribeOption{withHash}, want: "at minutes h, h+15, h+30, and h+45 every 2 hours starting with the midnight hour, where h is a fixed minute from 0 to 14"},
	{spec: "H/7 */2 * * *", opts: []DescribeOption{withHash}, want: "at minutes h, h+7, h+14, … h+56 every 2 hours starting with the midnight hour, where h is a fixed minute from 0 to 6; h+56 runs only when h is below 4"},
	{spec: "H/40 */2 * * *", opts: []DescribeOption{withHash}, want: "at minutes h and h+40 every 2 hours starting with the midnight hour, where h is a fixed minute from 0 to 39; h+40 runs only when h is below 20"},
	{spec: "H/15 * 9 * * *", opts: []DescribeOption{withSecHash}, want: "at seconds h, h+15, h+30, and h+45 within every minute during the 9 am hour, where h is a fixed second from 0 to 14"},
	{spec: "H/15 * 9-17 * * *", opts: []DescribeOption{withSecHash}, want: "at seconds h, h+15, h+30, and h+45 within every minute from 9 am thru 5:59 pm, where h is a fixed second from 0 to 14"},
	{spec: "H/15 5 * * * *", opts: []DescribeOption{withSecHash}, want: "at seconds h, h+15, h+30, and h+45 within minute 5 in every hour, where h is a fixed second from 0 to 14"},
	{spec: "H/15 0 * * * *", opts: []DescribeOption{withSecHash}, want: "at seconds h, h+15, h+30, and h+45 past the top of every hour, where h is a fixed second from 0 to 14"},
	{spec: "H/15 20-10/2 * * *", opts: []DescribeOption{withHash}, want: "at minutes h, h+15, h+30, and h+45 every 2 hours from the midnight hour thru the 10 am hour and the 8 pm hour thru the 10 pm hour, where h is a fixed minute from 0 to 14"},
	{spec: "H/15 20-5 * * *", opts: []DescribeOption{withHash}, want: "at minutes h, h+15, h+30, and h+45 past every hour from midnight thru 5 am and 8 pm thru 11 pm, where h is a fixed minute from 0 to 14"},
	{spec: "0 0 H 1 *", opts: []DescribeOption{withHash}, want: "on day h of the month in January, where h is a fixed day from 1 to 31"},
	{spec: "0 0 H(29-31) 2 *", opts: []DescribeOption{withHash}, want: "on day h of the month in February, where h is a fixed day between 29 and 31; a month without day h is skipped"},
	{spec: "0 0 H(1-15) 2,4 *", opts: []DescribeOption{withHash}, want: "on day h of the month in February and April, where h is a fixed day between 1 and 15"},
	{spec: "0 0 H(1-30) 1-3 *", opts: []DescribeOption{withHash}, want: "on day h of the month in January thru March, where h is a fixed day between 1 and 30; a month without day h is skipped"},
	{spec: "0 0 H(1-15)/5 3 *", opts: []DescribeOption{withHash}, want: "on days h, h+5, and h+10 of every month in March, where h is a fixed day from 1 to 5"},
	{spec: "0 0 H/10 1 *", opts: []DescribeOption{withHash}, want: "on days h, h+10, h+20, and h+30 of every month in January, where h is a fixed day from 1 to 10; h+30 runs only when h is below 2"},
	{spec: "0 0 H/10 4 *", opts: []DescribeOption{withHash}, want: "on days h, h+10, h+20, and h+30 of every month in April, where h is a fixed day from 1 to 10; h+30 runs only in months that have one"},
	{spec: "0 0 H/10 2 *", opts: []DescribeOption{withHash}, want: "on days h, h+10, h+20, and h+30 of every month in February, where h is a fixed day from 1 to 10; h+20 and h+30 run only in months that have them"},
	// One hash picks the day and a hashed month, so a value no month it pairs
	// with has reads as if it were not there (N3).
	{spec: "0 0 H/10 H(2-3) * 2028", opts: []DescribeOption{withHash, withYear}, want: "on days h, h+10, and h+20 of the month, where h is a fixed day from 1 to 10"},
	{spec: "0 0 H/15 H(4-6) *", opts: []DescribeOption{withHash}, want: "on days h and h+15 of the month, where h is a fixed day from 1 to 15"},
	{spec: "0 0 H/30 H(2-3) *", opts: []DescribeOption{withHash}, want: "on day h of the month, where h is a fixed day from 1 to 30"},
	{spec: "0 0 H/10 H *", opts: []DescribeOption{withHash}, want: "on days h, h+10, h+20, and h+30 of the month, where h is a fixed day from 1 to 10; h+30 runs only when h is below 2"},
	{spec: "0 0 H(28-30)/2 H(2-3) * 2027", opts: []DescribeOption{withHash, withYear}, want: "on the 28th; or the 29th of the month, depending on the hash key"},
	{spec: "0 0 H(29-31)/2 H(2-3) * 2027", opts: []DescribeOption{withHash, withYear}, want: "on the 30th of the month"},
	{spec: "0 0 H/2 H(2-3) *", opts: []DescribeOption{withHash}, want: "on the 1st, 3rd, 5th, … 29th; or the 2nd, 4th, 6th, … 30th of the month, depending on the hash key"},
	{spec: "0 0 H/2 4 *", opts: []DescribeOption{withHash}, want: "on the 1st, 3rd, 5th, … 31st; or the 2nd, 4th, 6th, … 30th of every month in April, depending on the hash key; the 31st runs only in months that have one"},
	{spec: "0 0 H/2 2 *", opts: []DescribeOption{withHash}, want: "on the 1st, 3rd, 5th, … 31st; or the 2nd, 4th, 6th, … 30th of every month in February, depending on the hash key; the 29th, 30th, and 31st run only in months that have them"},
	{spec: "0 0 1,H 6 *", opts: []DescribeOption{withHash}, want: "on the 1st and day h of the month in June, where h is a fixed day from 1 to 31; a month without day h runs on the 1st only"},
	{spec: "0 0 1,H 1 *", opts: []DescribeOption{withHash}, want: "on the 1st and day h of the month in January, where h is a fixed day from 1 to 31"},
	{spec: "0 0 H(25-5)/3 1 *", opts: []DescribeOption{withHash}, want: "on the 25th, 28th, 31st, and 3rd; the 26th, 29th, 1st, and 4th; or the 27th, 30th, 2nd, and 5th of every month in January, depending on the hash key"},
	{spec: "0 0 H(25-5)/3 4 *", opts: []DescribeOption{withHash}, want: "on the 25th, 28th, 31st, and 3rd; the 26th, 29th, 1st, and 4th; or the 27th, 30th, 2nd, and 5th of every month in April, depending on the hash key; a short month skips the days it lacks and changes the last step"},
	{spec: "0 0 H H *", opts: []DescribeOption{withHash}, want: "on day h of the month, where h is a fixed day from 1 to 31; a month without day h is skipped"},
	{spec: "0 0 H 1,H *", opts: []DescribeOption{withHash}, want: "on day h of the month, where h is a fixed day from 1 to 31; a month without day h is skipped"},
	{spec: "0 0 H/10 H *", opts: []DescribeOption{withHash}, want: "on days h, h+10, h+20, and h+30 of the month, where h is a fixed day from 1 to 10; h+30 runs only when h is below 2"},
	{spec: "H(50-10)/5 * * * *", opts: []DescribeOption{withHash}, want: "at minutes 50, 55, 0, 5, and 10; 51, 56, 1, and 6; 52, 57, 2, and 7; 53, 58, 3, and 8; or 54, 59, 4, and 9 past the hour, depending on the hash key"},
	{spec: "H(50-10)/15 * * * * *", opts: []DescribeOption{withSecHash}, want: "at seconds 50 and 5; 51 and 6; 52 and 7; 53 and 8; 54 and 9; 55 and 10; 56; 57; 58; 59; 0; 1; 2; 3; or 4 within a minute, depending on the hash key"},
	{spec: "0 H(22-4)/2 * * *", opts: []DescribeOption{withHash}, want: "at the top of a fixed hour between 10 pm and 11 pm and every 2 hours after it thru 4 am"},
	{spec: "H/15 50-10 * * * *", opts: []DescribeOption{withSecHash}, want: "at seconds h, h+15, h+30, and h+45 within minutes 0 thru 10 and 50 thru 59 in every hour, where h is a fixed second from 0 to 14"},
	// Limits, as in describeGoldens
	{spec: "0 0 * H(3-2)/3 *", opts: []DescribeOption{withHash}, want: "in January, April, July, and October; February, May, August, and November; or March, June, September, and December, depending on the hash key"},
	{spec: "0 0 H/16 6 *", opts: []DescribeOption{withHash}, want: "on days h and h+16 of every month in June, where h is a fixed day from 1 to 16; h+16 runs only in months that have one"},
	{spec: "0 0 H/30 2,12 *", opts: []DescribeOption{withHash}, want: "on days h and h+30 of every month in February and December, where h is a fixed day from 1 to 30; h and h+30 run only in months that have them"},
	{spec: "0 0 H(5-2) 5 *", opts: []DescribeOption{withHash}, want: "on day h of the month in May, where h is a fixed day between 1 and 2 or 5 and 31"},
	{spec: "0 0 H/30 H *", opts: []DescribeOption{withHash}, want: "on days h and h+30 of the month, where h is a fixed day from 1 to 30; h+30 runs only when h is below 2"},
	{spec: "0 0 * H(9-5)/7 *", opts: []DescribeOption{withHash}, want: "in September and April; October and May; November; December; January; February; or March, depending on the hash key"},
	{spec: "0 0 H(4-26)/2 * *", opts: []DescribeOption{withHash}, want: "on the 4th, 6th, 8th, 10th, 12th, 14th, 16th, 18th, 20th, 22nd, 24th, and 26th; or the 5th, 7th, 9th, 11th, 13th, 15th, 17th, 19th, 21st, 23rd, and 25th of every month, depending on the hash key"},
	{spec: "0 0 H/7 H(11-12) *", opts: []DescribeOption{withHash}, want: "on days h, h+7, h+14, h+21, and h+28 of the month, where h is a fixed day from 1 to 7; h+28 runs only when h is below 4"},
	{spec: "0 0 H/10,H * H/3", opts: []DescribeOption{withHash}, want: ""},
}

func TestDescribeSpecNote(t *testing.T) {
	g := describeNoteGrammar()
	for _, tt := range describeNoteGoldens {
		if tt.want != "" && !g.MatchString(tt.want) { // N1: no note has no form
			t.Errorf("%q: note %q is outside the note grammar", tt.spec, tt.want)
		}
		got, err := DescribeSpecNote(tt.spec, tt.opts...)
		if err != nil {
			t.Errorf("DescribeSpecNote(%q): unexpected error %v", tt.spec, err)
			continue
		}
		if got != tt.want {
			t.Errorf("DescribeSpecNote(%q):\n got %q\nwant %q", tt.spec, got, tt.want)
		}
	}
}

// TestStepOutcomes binds the outcome sets the notes list to the parser:
// for each hashed step, the values stepOutcomes gives for each start are
// the sets the parser selects over many hash keys (N4).
func TestStepOutcomes(t *testing.T) {
	p := NewParser(Second | Minute | Hour | Dom | Month | Dow | Hash)
	for _, tt := range []struct {
		spec  string
		field int // seconds to day of week, as SpecSchedule
	}{
		{"0 H/15 * * * *", 1},
		{"0 H/7 * * * *", 1},
		{"0 H/40 * * * *", 1},
		{"0 H(0-29)/10 * * * *", 1},
		{"0 0 H/6 * * *", 2},
		{"0 0 H(9-17)/4 * * *", 2},
		{"0 0 0 H/2 * *", 3},
		{"0 0 0 H/10 * *", 3},
		{"0 0 0 H(1-28)/7 * *", 3},
		{"0 0 0 H(25-5)/7 * *", 3},
		{"0 0 0 1 H/2 *", 4},
		{"0 0 0 1 H/5 *", 4},
		{"0 0 0 1 H(11-2)/2 *", 4},
		{"0 0 9 * * H/2", 5},
		{"0 0 9 * * H/3", 5},
		{"0 0 9 * * H/4", 5},
		{"0 0 9 * * H(1-5)/2", 5},
		{"0 0 9 * * H(5-1)/2", 5},
		{"H(50-10)/15 * * * * *", 0},
		{"0 H(50-10)/5 * * * *", 1},
		{"0 0 H(22-4)/2 * * *", 2},
		{"0 0 9 * * H(1-7)/2", 5},
		{"0 0 9 * * H(1-7)/3", 5},
		{"0 0 9 * * H(5-4)/2", 5},
		{"0 0 9 * * H(1-0)/2", 5},
		{"0 H(1-0)/15 * * * *", 1},
		{"0 0 H(5-4)/7 * * *", 2},
		{"0 0 H(5-4)/6 * * *", 2},
		{"0 0 0 1 H(2-1)/5 *", 4},
	} {
		_, _, fields, _, _ := p.specFields(tt.spec)
		r := []bounds{seconds, minutes, hours, dom, months, p.dowBounds()}[tt.field]
		x := whole(splitHash(fields[tt.field], r).steps[0], r)
		want := map[string]bool{}
		for _, o := range stepOutcomes(x, r) {
			want[fmt.Sprint(slices.Sorted(slices.Values(o)))] = true
		}
		got := parserOutcomes(t, p, tt.spec, tt.field, r)
		if !maps.Equal(got, want) {
			t.Errorf("%s: parser outcomes %v, stepOutcomes %v", tt.spec, slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
		}
	}
}

// parserOutcomes returns the value sets field takes in spec over 1,000 keys.
func parserOutcomes(t *testing.T, p Parser, spec string, field int, r bounds) map[string]bool {
	t.Helper()
	got := map[string]bool{}
	for k := range 1000 {
		sch, err := p.WithHashKey(fmt.Sprint("key", k)).Parse(spec)
		if err != nil {
			t.Fatal(err)
		}
		ss := sch.(*SpecSchedule)
		bits := []uint64{ss.Second, ss.Minute, ss.Hour, ss.Dom, ss.Month, ss.Dow}[field]
		if field == 5 && bits&(1<<7) != 0 {
			bits = bits&^(1<<7) | 1 // Sunday is 0 and 7
		}
		var vals []uint
		for v := r.min; v <= r.max; v++ {
			if bits&(1<<v) != 0 {
				vals = append(vals, v)
			}
		}
		got[fmt.Sprint(vals)] = true
	}
	return got
}

// TestDayStepCountsWrittenMonths binds the count of a hashed day step to the
// parser: the fewest and most days in a written month, over keys and years.
func TestDayStepCountsWrittenMonths(t *testing.T) {
	p := MustNewParser(Minute | Hour | Dom | Month | Dow | Hash | Year)
	for _, spec := range dayStepSpecs() {
		sch, err := p.WithHashKey("key").Parse(spec)
		if err != nil {
			t.Fatal(err)
		}
		_, _, fields, _, _ := p.specFields(spec)
		d := p.dayDescriber(sch, fields)
		wantLo, wantHi := d.dayCount(splitHash(fields[3], dom).steps[0])
		lo, hi := keyedMonthRuns(t, p, spec)
		if lo != wantLo || hi != wantHi {
			t.Errorf("%s: parser runs %d to %d days in a month, count says %d to %d", spec, lo, hi, wantLo, wantHi)
		}
	}
}

// TestDayRunsPastTheWalk checks the count past maxHashWalk: the day and the
// month count each on its own, so it covers every length the month can pick.
func TestDayRunsPastTheWalk(t *testing.T) {
	p := MustNewParser(Minute | Hour | Dom | Month | Dow | Hash | Year)
	spec := "0 0 H/10 H/250,H/251 * 2027"
	sch, err := p.WithHashKey("key").Parse(spec)
	if err != nil {
		t.Fatal(err)
	}
	_, _, fields, _, _ := p.specFields(spec) //nolint:dogsled // only the fields matter
	s := sch.(*SpecSchedule)
	if _, ok := hashWalkPeriod(p.hashDateFields(s, s, fields)[:2]); ok {
		t.Fatalf("%s repeats within maxHashWalk", spec)
	}
	x := splitHash(fields[3], dom).steps[0]
	wantLo, wantHi := dayRange(x, []uint{28, 30, 31}) // the months of 2027
	d := p.dayDescriber(sch, fields)
	if lo, hi := d.dayCount(x); lo != wantLo || hi != wantHi {
		t.Errorf("%s counts %d to %d days; want %d to %d", spec, lo, hi, wantLo, wantHi)
	}
}

// TestDayRunsIgnoresDowPeriod checks that an H in the day of the week, which
// counts no days, does not push the walk past maxHashWalk.
func TestDayRunsIgnoresDowPeriod(t *testing.T) {
	p := MustNewParser(Minute | Hour | Dom | Month | Dow | Hash | Year)
	spec := "0 0 H/10 H(2-3) H/9999 2028"
	sch, err := p.WithHashKey("key").Parse(spec)
	if err != nil {
		t.Fatal(err)
	}
	_, _, fields, _, _ := p.specFields(spec) //nolint:dogsled // only the fields matter
	d := p.dayDescriber(sch, fields)
	if lo, hi := d.dayCount(splitHash(fields[3], dom).steps[0]); lo != 3 || hi != 3 {
		t.Errorf("%s counts %d to %d days; want 3", spec, lo, hi)
	}
}

// TestPairedDaysMatchKeys binds the last day a paired note names to the parser:
// the last day any key runs on in a month it picks.
func TestPairedDaysMatchKeys(t *testing.T) {
	p := MustNewParser(Minute | Hour | Dom | Month | Dow | Hash | Year)
	for _, spec := range dayStepSpecs() {
		sch, err := p.WithHashKey("key").Parse(spec)
		if err != nil {
			t.Fatal(err)
		}
		_, _, fields, _, _ := p.specFields(spec)
		d := p.dayDescriber(sch, fields)
		if d.dayPairs == nil {
			continue
		}
		if got, want := keyedLastDay(t, p, spec), d.lastPairedDay(); got != want {
			t.Errorf("%s: last day a key runs on is %d, the note names %d", spec, got, want)
		}
	}
}

// keyedLastDay returns the last day any of 500 keys runs on in 2027 and 2028.
func keyedLastDay(t *testing.T, p Parser, spec string) (last uint) {
	t.Helper()
	for k := range 500 {
		sch, err := p.WithHashKey(fmt.Sprint("key", k)).Parse(spec)
		if err != nil {
			t.Fatal(err)
		}
		for at := sch.Next(time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)); !at.IsZero() && at.Year() <= 2028; at = sch.Next(at) {
			last = max(last, uint(at.Day())) // #nosec G115 -- a day of the month
		}
	}
	return last
}

// dayStepSpecs lists hashed day steps whose count TestDayStepCountsWrittenMonths
// binds to the parser.
func dayStepSpecs() []string {
	specs := []string{
		"0 0 H/10 4 * *", "0 0 H/10 2 * *", "0 0 H/15 1 * *", "0 0 H/30 2 * *", "0 0 H/30 4 * *", "0 0 H/2 2 * *",
		"0 0 H/10 2,4 * *", "0 0 H(25-5)/7 2 * *", "0 0 H/28 2 * 2027", "0 0 H/10 * * *",
		"0 0 H(28-30)/2 H * 2028", "0 0 H(28-30)/2 H * 2027", "0 0 H/10 H(4-6) * *", "0 0 H/10 H(2-3) * 2028",
	}
	// One hash picks the day and the month, so the count follows their pairs.
	for _, d := range []string{"H/10", "H/7", "H/15", "H/28", "H/30", "H(28-30)/2", "H(25-5)/7"} {
		for _, m := range []string{"H", "H(2-3)", "H(4-6)", "H(11-2)", "H/3", "H/5", "2,H(4-6)"} {
			for _, y := range []string{"*", "2027", "2028"} {
				specs = append(specs, fmt.Sprintf("0 0 %s %s * %s", d, m, y))
			}
		}
	}
	return specs
}

// keyedMonthRuns returns the fewest and most runs in one written month over
// 500 keys.
func keyedMonthRuns(t *testing.T, p Parser, spec string) (lo, hi int) {
	t.Helper()
	lo = 99
	for k := range 500 {
		keyed, err := p.WithHashKey(fmt.Sprint("key", k)).Parse(spec)
		if err != nil {
			t.Fatal(err)
		}
		keyLo, keyHi := writtenMonthRuns(keyed)
		lo, hi = min(lo, keyLo), max(hi, keyHi)
	}
	return lo, hi
}

// writtenMonthRuns returns the fewest and most runs in one written month of
// 2027 and 2028.
func writtenMonthRuns(sch Schedule) (lo, hi int) {
	s := sch.(*SpecSchedule)
	lo = 99
	for _, y := range []int{2027, 2028} {
		for m := time.January; m <= time.December; m++ {
			if s.Month&(1<<uint(m)) == 0 || len(s.Year) > 0 && !yearWritten(s, y) {
				continue
			}
			n := 0
			end := time.Date(y, m+1, 1, 0, 0, 0, 0, time.UTC)
			for at := sch.Next(time.Date(y, m, 1, 0, 0, 0, 0, time.UTC).Add(-time.Second)); !at.IsZero() && at.Before(end); at = sch.Next(at) {
				n++
			}
			lo, hi = min(lo, n), max(hi, n)
		}
	}
	return lo, hi
}

func yearWritten(s *SpecSchedule, y int) bool {
	_, ok := s.Year[y]
	return ok
}

// neverRunsSpecs lists the day forms TestNeverRunsMatchesParser sweeps.
func neverRunsSpecs() []string {
	var specs []string
	for m := 1; m <= 12; m++ {
		for d := 1; d <= 31; d++ {
			specs = append(specs, fmt.Sprintf("0 0 %dW %d *", d, m), fmt.Sprintf("0 0 L-%d %d *", d-1, m))
		}
		for n := 1; n <= 5; n++ {
			specs = append(specs, fmt.Sprintf("0 0 * %d MON#%d", m, n))
		}
	}
	for d := 1; d <= 31; d++ {
		for n := 1; n <= 5; n++ {
			specs = append(specs, fmt.Sprintf("0 0 %d * FRI#%d", d, n))
		}
	}
	return specs
}

// TestNeverRunsMatchesParser binds "never runs" to Next for the day forms the
// parser cannot reject: no start in a run of years finds a run.
func TestNeverRunsMatchesParser(t *testing.T) {
	p := MustNewParser(Minute | Hour | Dom | Month | Dow | Extended)
	for _, spec := range neverRunsSpecs() {
		sch, err := p.Parse(spec)
		if err != nil {
			continue
		}
		runs := false
		for y := 2001; y <= 2029 && !runs; y += 4 { // Next looks 5 years ahead
			runs = !sch.Next(time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)).IsZero()
		}
		got, err := p.DescribeSpec(spec)
		if err != nil {
			t.Fatal(err)
		}
		if (got == "never runs") == runs {
			t.Errorf("%s reads %q; a run within the years: %v", spec, got, runs)
		}
	}
}

// hashedDateSpecs returns specs with H in the date fields, whose picks move
// together under the one hash.
func hashedDateSpecs() []string {
	var specs []string
	for _, d := range []string{"H(29-31)", "H(30-31)", "H(30-31)/2", "H(30-31)/3", "31", "L-3", "*/2"} {
		for _, m := range []string{"H(1-3)", "H(2-3)", "H(4-5)", "H(2-4)/2", "H", "2"} {
			for _, w := range []string{"*", "H(1-3)", "H(1-2)", "MON#5", "*/2"} {
				specs = append(specs, fmt.Sprintf("0 0 %s %s %s", d, m, w), fmt.Sprintf("0 0 %s %s %s 2027", d, m, w), fmt.Sprintf("0 0 %s %s %s 2028", d, m, w))
			}
		}
	}
	return specs
}

// hashedDateParsers are the options that change how the date fields combine.
var hashedDateParsers = []ParseOption{
	Minute | Hour | Dom | Month | Dow | Extended | Hash | YearOptional,
	Minute | Hour | Dom | Month | Dow | Extended | Hash | YearOptional | DowOrDom,
	Minute | Hour | Dom | Month | Dow | Extended | Hash | YearOptional | DowOrDom | StepWildcard,
}

// hashedDateKeys returns keys that reach every set of picks of
// hashedDateSpecs, whose H items all repeat within 12 hash values.
func hashedDateKeys(t *testing.T) []string {
	t.Helper()
	var keys []string
	seen := map[uint64]bool{}
	for k := 0; k < 60; k++ {
		keys = append(keys, fmt.Sprintf("key-%d", k))
		seen[computeHash(keys[k])%12] = true
	}
	if len(seen) != 12 {
		t.Fatalf("the keys reach %d of the 12 hash values", len(seen))
	}
	return keys
}

// anyKeyRuns reports whether spec finds a run under any of keys.
func anyKeyRuns(t *testing.T, p Parser, spec string, keys []string) bool {
	t.Helper()
	for _, key := range keys {
		sch, err := p.ParseWithHashKey(spec, key)
		if err != nil {
			t.Fatal(err)
		}
		for y := 2001; y <= 2029; y += 4 { // Next looks 5 years ahead
			if !sch.Next(time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)).IsZero() {
				return true
			}
		}
	}
	return false
}

// TestNeverRunsMatchesKeys binds "never runs" to Next for H in the date
// fields: no key finds a run.
func TestNeverRunsMatchesKeys(t *testing.T) {
	keys := hashedDateKeys(t)
	for _, opts := range hashedDateParsers {
		p := MustNewParser(opts)
		for _, spec := range hashedDateSpecs() {
			runs := anyKeyRuns(t, p, spec, keys)
			got, err := p.DescribeSpec(spec)
			if err != nil {
				t.Fatal(err)
			}
			if (got == "never runs") == runs {
				t.Errorf("options %d: %s reads %q; a key finds a run: %v", opts, spec, got, runs)
			}
		}
	}
}

// TestStrictDaysMatchesKeys binds the parser's error to the keys: with no key,
// DescribeSpec returns it only when StrictDays rejects every key.
func TestStrictDaysMatchesKeys(t *testing.T) {
	keys := hashedDateKeys(t)
	for _, opts := range hashedDateParsers {
		p := MustNewParser(opts | StrictDays)
		for _, spec := range hashedDateSpecs() {
			rejected := true
			for _, key := range keys {
				if _, err := p.ParseWithHashKey(spec, key); err == nil {
					rejected = false
					break
				}
			}
			if _, err := p.DescribeSpec(spec); (err != nil) != rejected {
				t.Errorf("options %d: %s: error %v; StrictDays rejects every key: %v", opts, spec, err, rejected)
			}
		}
	}
}

// TestHashPeriod checks that an H item picks the same bits every period hash
// values: the walk behind "never runs" sees all it can pick.
func TestHashPeriod(t *testing.T) {
	for _, tt := range []struct {
		r     bounds
		items []string
	}{
		{dom, []string{"H", "H(29-31)", "H(30-31)/2", "H(30-31)/3", "H/10", "H/40", "H(28-3)", "H(5-5)"}},
		{months, []string{"H", "H(1-3)", "H(2-4)/2", "H/5", "H/1"}},
		{dow, []string{"H", "H(0-7)", "H(1-3)", "H(5-7)", "H(5-1)", "H(7-7)", "H/2", "H/9", "H(0-7)/3", "H(6-1)/2"}},
	} {
		for _, it := range tt.items {
			period := hashPeriod(it, tt.r)
			for h := uint64(0); h < 2000; h++ {
				a, err := hashExprBits(it, tt.r, h)
				if err != nil {
					t.Fatal(err)
				}
				if b, _ := hashExprBits(it, tt.r, h+period); a != b {
					t.Errorf("%s: hash %d picks %b, hash %d picks %b", it, h, a, h+period, b)
					break
				}
			}
		}
	}
}

// TestHashPeriodStep checks that a step inside its range repeats with the
// step: only a step past the range reads the rest of the hash.
func TestHashPeriodStep(t *testing.T) {
	for _, tt := range []struct {
		item string
		r    bounds
		want uint64
	}{
		{"H(1-31)/30", dom, 30},
		{"H/10", dom, 10},
		{"H(30-31)/2", dom, 2},
		{"H(30-31)/3", dom, 6},
		{"H(6-1)/2", dow, 2},
		{"H", dom, 31},
		{"H", dow, 56},
	} {
		if got := hashPeriod(tt.item, tt.r); got != tt.want {
			t.Errorf("hashPeriod(%q) = %d, want %d", tt.item, got, tt.want)
		}
	}
}

// TestNeverRunsPastTheWalk checks specs whose H items repeat only past
// maxHashWalk: each H is tested on its own, so these read as running, the last
// two only if a Sunday written as 7 counts: 2026-03-01 is a Sunday.
// TestDescribeSpecTooLongStaysCheap checks that a spec over MaxSpecLength is
// rejected before it is split: its allocations must not follow its length.
func TestDescribeSpecTooLongStaysCheap(t *testing.T) {
	spec := strings.Repeat("* ", MaxSpecLength)
	if _, err := DescribeSpec(spec); err == nil {
		t.Fatalf("DescribeSpec of %d bytes: want an error", len(spec))
	}
	if allocs := testing.AllocsPerRun(5, func() { _, _ = DescribeSpec(spec) }); allocs > 100 {
		t.Errorf("DescribeSpec of %d bytes: %.0f allocations", len(spec), allocs)
	}
}

func TestNeverRunsPastTheWalk(t *testing.T) {
	for _, spec := range []string{
		"0 0 H(29-31),H(29-31)/251,H(29-31)/257 1 H(1-3) 2028",
		"0 0 1,H(1-2)/70000 3 H(5-7) 2026",
		"0 0 1,H(1-2)/70000 3 H(5-7)/2 2026",
	} {
		got, err := DescribeSpec(spec, WithParseOptions(Minute|Hour|Dom|Month|Dow|Hash|Year))
		if err != nil {
			t.Fatal(err)
		}
		if got == "never runs" {
			t.Errorf("%s reads %q", spec, got)
		}
	}
}

func TestDescribeSpec(t *testing.T) {
	for _, tt := range describeGoldens {
		describe := DescribeSpec
		if tt.parser != nil {
			describe = tt.parser.DescribeSpec
		}
		got, err := describe(tt.spec, tt.opts...)
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
	desc, _ := describeGrammars()
	return desc
}

// describeNoteGrammar is the note EBNF in docs/DESCRIBE.md as a regular
// expression. Every DescribeSpecNote text other than "" must match it.
func describeNoteGrammar() *regexp.Regexp {
	_, note := describeGrammars()
	return note
}

// describeGrammars returns the description and note grammars, which share
// their clock, hour and name productions.
func describeGrammars() (descRe, noteRe *regexp.Regexp) {
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
	units := alt("seconds", "minutes", "hours", "days", "months", "years")
	// R3: no article before a list that starts with midnight
	hourtail := "(?:, " + clock + ")+, and " + clock
	hours := alt("the "+clock+" hour"+opt(" and the "+clock+" hour"), "the hours of "+clock+hourtail)
	// H3: the ends of a fixed hour's range
	hourspan := "the hours of " + clock + " and " + clock
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

	// H1, H3: a value the hash key fixes
	// H3, T9: a wrapped range in two parts, a part of one value as that value
	nrange := num + " and " + num
	nspan := "between " + units + " " + nrange
	nalts := ", either " + alt(unit+" "+num+" or "+nspan, nspan+" or "+unit+" "+num, nspan+" or "+nspan)
	fixed := "a fixed " + unit + opt(alt(" between "+alt(units+" "+nrange, hourspan), nalts+opt(",")))

	// C1: what leads, with the minutes joined to the seconds
	secs := alt("at "+num+" seconds?", "at seconds "+vlist, "at "+fixed)
	inner := alt(
		secs+opt(" within "+alt("a minute", "every minute", "minute "+num, "minutes "+vlist, fixed)), // H24
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
	cnt := alt(num, num+" or "+num)
	apart := ", " + num + " " + units + " apart, starting from " + fixed

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
		// H in the time fields
		"at "+fixed+" past every hour "+hourrange+opt(everyday),                             // R9
		"at "+fixed+" past every hour from "+crange+" and the "+clock+" hour"+opt(everyday), // T9
		"at "+fixed+" past the "+clock+" hour and every hour from "+crange+opt(everyday),    // T9
		"at "+fixed+" during "+hours+opt(alt(everyday, inevery)),                            // H7, R10
		"at "+fixed+" "+hstep, // R11
		"at "+alt(fixed, "minute "+num, "minutes "+vlist)+" at "+fixed+opt(everyday), // H16
		"at the top of "+fixed+opt(everyday),                                         // R4
		"every minute during "+fixed,                                                 // R6
		"at "+fixed+" at "+list(clock)+opt(everyday),                                 // H8
		"at "+fixed+" and "+fixed+" during "+hours+opt(everyday),                     // H12
		"at "+fixed+" at the top of "+fixed+opt(everyday),                            // H24
		// H2, H14, H15: the count forms
		opt(alt("at the top of the hour", "at "+num+" minutes? past the hour")+", ")+cnt+" runs? in every "+unit+
			opt(" "+alt("between "+units+" "+nrange, hourrange, "during "+hours, "from "+crange+" and "+hone, hone+" and from "+crange))+apart,
		alt("every minute", "at minutes "+vlist, "at "+fixed)+" during "+cnt+" hours in every day"+apart, // H14, H22
		cnt+" runs in every "+unit+opt(" "+alt(hourrange, "during "+hours, "from "+crange+" and "+hone, hone+" and from "+crange))+", at "+alt( // H6, H15, H21
			"the top of the hour and "+fixed+" past it", fixed+" and "+fixed,
			"the top of the hour and every "+num+" "+units+" starting from "+fixed, clock+" and the top of "+fixed,
			"the top of "+fixed+" and "+fixed),
		cnt+" runs? in every "+unit+apart+", "+hstep, // H23
		cnt+" runs? "+alt("within "+alt("minute "+num, "minutes "+vlist)+opt(" "+hourjoin), // H24
			"past the top of "+tophours)+apart,
	)
	// D2, D4–D6: the date clauses; D3: the short-month clause
	ord := `\d+(?:st|nd|rd|th)`
	dayname := alt("Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday")
	monthname := alt("January", "February", "March", "April", "May", "June", "July",
		"August", "September", "October", "November", "December")
	nth := alt("first", "second", "third", "fourth", "fifth", "last")
	olist := alt(list(alt(ord+opt(" thru "+ord), "last day")), ord+", "+ord+", "+ord+", … "+ord)
	// X1–X3: the ordinals share one "the"; other kinds keep their own words,
	// and a list that starts with a day before the last day takes no "on".
	before := num + " days? before the last day"
	domel := alt("the "+olist, "the last weekday", "the weekday nearest the "+ord)
	anyel := alt(domel, before)
	dom := alt(
		"on the "+olist+" of "+alt("the", "every")+" month",
		"on the weekday nearest the "+ord,
		"on "+alt(domel, domel+" and "+anyel, domel+"(?:, "+anyel+")+, and "+anyel)+" of the month",
		list(before)+" of the month",
	)
	dayrange := dayname + opt(" thru "+dayname)
	dow := list(alt(dayrange, "the "+nth+" "+dayname))
	// H18, H19: a day or month the hash key fixes
	hcnt := alt(num, num+" or "+num, num+" to "+num)
	hapart := func(u string) string { return ", " + num + " " + u + "s apart, starting from a fixed " + u }
	hform := func(noun, plain, unit, in, lo, hi string) string {
		b := opt(" between " + lo + " and " + hi)
		return alt(noun+b, plain+" and "+noun, hcnt+" "+unit+"s? in every "+in+b+hapart(unit))
	}
	// H18: a wrapped day of the month in two parts, low to high
	domhi := ord + opt(" or the "+ord+" and "+ord)
	domlo := "the " + ord
	// H3: a part of one value stands apart
	domspan := "between the " + ord + " and " + ord
	domalts := ", either " + alt("the "+ord+" or "+domspan, domspan+" or on the "+ord)
	domone := "a fixed day of the month" + domalts + opt(",")
	hday := alt(domone, hform("a fixed day of the month", "the "+ord, "day", "month", domlo, domhi),
		hform("a fixed day of the week", dayname, "day", "week", dayname, dayname))
	hmonth := hform("a fixed month", monthname, "month", "year", monthname, monthname)
	// H18, D5: a fixed day joins the other day field
	hdomfix := alt(domone, "a fixed day of the month"+opt(" between "+domlo+" and "+domhi), "the "+ord+" and a fixed day of the month")
	hdowfix := alt("a fixed day of the week"+opt(" between "+dayname+" and "+dayname), dayname+" and a fixed day of the week")
	hdowany := alt("a fixed day of the week"+opt(" between "+dayname+" and "+dayname), "any "+dayname+" and a fixed day of the week") // X4
	andor := alt(" when it falls on "+alt(dow, hdowfix), " or on "+alt(list("any "+alt(dayrange, nth+" "+dayname)), hdowany))
	day := alt(alt(dom, "on "+hdomfix)+opt(andor), "on "+dow, "on "+hday)
	month := "in " + alt(list(monthname+opt(" thru "+monthname)), hmonth)
	year := alt("in year "+num, "in years "+vlist)
	short := "; " + alt(
		"the "+list(ord)+" "+alt("runs", "run")+" only in months that have "+alt("one", "them"),
		alt("it", before)+" runs only in months of "+num+" days"+opt(" or more"),
	)

	hashkey := "; the hash key fixes " + list(alt("the "+unit, "both "+units)) // H1, H6
	schedule := time + opt(" "+day) + opt(" "+month) + opt(" "+year) + opt(note) + opt(alt(nalts, domalts)) + "(?:" + short + ")*" + opt(hashkey)
	// P3: an interval as it runs, from its anchor
	dur := `[-+]?(?:(?:(?:\d+\.?\d*|\.\d+)(?:ns|us|µs|μs|ms|s|m|h))+|0)` // Go's duration: "1.s", ".5m", "0"
	interval := "every " + dur + " from " + alt("when scheduling started", "Unix epoch") +
		opt(` \(written as `+dur+", "+alt("rounded up to the "+dur+" minimum", "rounded up to whole seconds", "cut to whole seconds")+`\)`)
	descRe = regexp.MustCompile("^" + alt(schedule, "never runs", "only when triggered", interval) + "$")

	// N1–N8: the note, one clause per hashed field
	v := `[hkjmnp]`
	vterm := v + `(?:\+\d+)?`
	vals := alt(vterm+" and "+vterm, vterm+"(?:, "+vterm+")+, and "+vterm, vterm+", "+vterm+", "+vterm+", … "+vterm)
	bound := num + opt(` \(`+monthname+`\)`)
	where := ", where " + v + " is a fixed " + alt("second", "minute", "day", "month") + alt(" from "+bound+" to "+bound,
		" between "+bound+" and "+bound+opt(" or "+bound+" and "+bound),
		", either day "+bound+" or between days "+bound+" and "+bound, ", either between days "+bound+" and "+bound+" or day "+bound)
	tail := "; " + vterm + " runs only when " + v + " is below " + num
	nhstep := "every " + num + " hours " + alt("starting with "+hours, "from "+list(hcrange)) // R11
	tplace := alt(" past the hour", " during "+hours, " past every hour "+hourrange,
		" past every hour from "+clock+" thru "+clock+" and the "+clock+" hour", " past the "+clock+" hour and every hour from "+clock+" thru "+clock, // T9
		" within a minute",
		" within every minute "+alt("during "+hours, hourrange),                                                   // C1, R12
		" within "+alt("minute "+num, "minutes "+vlist)+" in every hour"+opt(note), " past the top of every hour", // C1
		" "+nhstep)
	toutcomes := list(num) + "(?:; " + list(num) + ")*; or " + list(num) // N2, N4: a wrapped range lists
	hlead := alt("at the top of", "at "+num+" minutes? past", "every minute during", "at minutes "+vlist+" during",
		"at a fixed minute"+opt(" between minutes "+num+" and "+num)+" during") // N8
	ntime := alt("at "+alt("seconds", "minutes")+" "+vals+tplace+where+opt(tail),
		"at "+alt("seconds", "minutes")+" "+toutcomes+tplace+", depending on the hash key",
		hlead+" a fixed hour between "+clock+" and "+clock+" and every "+num+" hours after it"+opt(" thru "+clock)+opt(note)) // N7, N8
	outcome := alt(list(dayname), list(monthname), "the "+ord+opt(" and "+ord), "the "+ord+"(?:, "+ord+")+, and "+ord, "the "+ord+", "+ord+", "+ord+", … "+ord)
	outcomes := outcome + "(?:; " + outcome + ")*; or " + outcome
	inMonths := opt(" in " + list(monthname+opt(" thru "+monthname))) // N5: a written month
	ofDays := alt(" of every month"+inMonths, " of the month")
	shortTail := alt("; "+vterm+" runs only in months that have one", "; "+list2(vterm)+" run only in months that have them") // N3
	shortDays := alt("; a short month skips the days it lacks and changes the last step",
		"; the "+ord+" runs only in months that have one", "; the "+alt(ord+" and "+ord, ord+"(?:, "+ord+")+, and "+ord)+" run only in months that have them") // N3, N4
	nday := alt("on days "+vals+ofDays+where+opt(alt(tail, shortTail)),
		"on "+opt("the "+ord+" and ")+"day "+v+alt(" of every month", " of the month"+inMonths)+where+opt("; a month without day "+v+alt(" is skipped", alt(" runs on the "+ord+" only", " runs only on the "+ord+", if it has one"))),
		"on "+outcomes+opt(ofDays)+", depending on the hash key"+opt(shortDays),
		"on "+outcome+ofDays) // N3: one outcome left
	nmonth := alt("in months "+vals+where+opt(tail), "in "+outcomes+", depending on the hash key")
	nclause := alt(ntime, nday, nmonth)
	noteRe = regexp.MustCompile("^" + nclause + "(?:, " + nclause + ")*$")
	return descRe, noteRe
}

func TestDescribeSpecGrammar(t *testing.T) {
	g := describeGrammar()
	for _, tt := range describeGoldens {
		if !tt.literal && !g.MatchString(tt.want) {
			t.Errorf("%q: description %q is outside the grammar", tt.spec, tt.want)
		}
	}
}

// TestDescribeNoteGrammarRejectsHashed checks that a note names a value the
// hash key picks as a description does (H1): "fixed", not "hashed".
func TestDescribeNoteGrammarRejectsHashed(t *testing.T) {
	const s = "at minutes h and h+30 past the hour, where h is a hashed minute from 0 to 29"
	if describeNoteGrammar().MatchString(s) {
		t.Errorf("note grammar accepts %q", s)
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
		"every minute during midnight, 6 am, noon, and 6 pm hours in every day",
		"at minutes 0 and 30 during the 9 am and 5 pm hours",
		"at minutes 0 and 30 during the hours of 9 am and 5 pm",
	} {
		if g.MatchString(s) {
			t.Errorf("grammar accepts %q", s)
		}
	}
}

// TestDescribableGate checks that each day field takes only its own special
// syntax: "#" belongs to the day of week, "L" and "W" to the day of month.
func TestDescribableGate(t *testing.T) {
	if allDescribable([]string{"0", "0", "0", "5#3", "*", "*"}) {
		t.Error(`"#" accepted in the day of month`)
	}
	if allDescribable([]string{"0", "0", "0", "*", "*", "L"}) {
		t.Error(`"L" accepted in the day of week`)
	}
	if !allDescribable([]string{"0", "0", "0", "L", "*", "5#3"}) {
		t.Error("the day fields' own syntax refused")
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
	got, err = FullParser().DescribeSpec("0 0 H/10 * 1")
	if err != nil {
		t.Fatal(err)
	}
	if want := "minute 0, hour 0, day of month H/10, month *, day of week 1"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	got, err = FullParser().DescribeSpec("0 0 0 H/10 * 1 2027")
	if err != nil {
		t.Fatal(err)
	}
	if want := "second 0, minute 0, hour 0, day of month H/10, month *, day of week 1, year 2027"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	got, err = NewParser(Minute | Hour | Dom | Month | Dow | Hash).WithHashKey("job").DescribeSpec("H/15 * * * *")
	if err != nil {
		t.Fatal(err)
	}
	if want := "4 runs in every hour, 15 minutes apart, starting from a fixed minute; the hash key fixes the minute"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// No hash key: the description does not depend on one.
	got, err = FullParser().DescribeSpec("H H * * *")
	if err != nil {
		t.Fatal(err)
	}
	if want := "at a fixed minute at a fixed hour every day; the hash key fixes the minute and the hour"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestDescribeSpecBadOptions checks that invalid parse options and a nil
// option return an error or are skipped, not a panic.
func TestDescribeSpecBadOptions(t *testing.T) {
	if _, err := DescribeSpec("0 0 * * *", WithParseOptions(SecondOptional|DowOptional|Minute|Hour|Dom|Month)); err == nil {
		t.Error("two optional fields: want an error")
	}
	if got, err := DescribeSpec("0 0 * * *", nil); err != nil || got != "at midnight every day" {
		t.Errorf("nil option: got %q, %v", got, err)
	}
	if _, err := DescribeSpecNote("0 0 * * *", WithParseOptions(SecondOptional|DowOptional|Minute|Hour|Dom|Month)); err == nil {
		t.Error("DescribeSpecNote, two optional fields: want an error")
	}
}

// wantStrictError checks that each spec returns an error under options.
func wantStrictError(t *testing.T, options ParseOption, specs ...string) {
	t.Helper()
	for _, spec := range specs {
		if _, err := DescribeSpec(spec, WithParseOptions(options)); err == nil {
			t.Errorf("DescribeSpec(%q) with StrictDays: want an error", spec)
		}
	}
}

// TestDescribeSpecStrictDaysNoKey checks that, with no hash key, a spec with
// H in the day of the month or the month gets the description it gets without
// StrictDays, as the result would otherwise depend on the placeholder key,
// while any other spec and a parser with a key keep the day check.
func TestDescribeSpecStrictDaysNoKey(t *testing.T) {
	strict := Minute | Hour | Dom | Month | Dow | Hash | StrictDays
	// "job-a" accepts the first spec; "job-a" and "job-b" both reject the second.
	for _, spec := range []string{"0 0 H(29-31) 2 *", "0 0 H(28-31) 2 *", "0 0 30 H *"} {
		want, err := DescribeSpec(spec, WithParseOptions(strict&^StrictDays))
		if err != nil {
			t.Fatal(err)
		}
		if got, err := DescribeSpec(spec, WithParseOptions(strict)); err != nil || got != want {
			t.Errorf("DescribeSpec(%q) with StrictDays = %q, %v; want %q, nil", spec, got, err, want)
		}
	}
	// No key finds the day, so the parser rejects every key, as it rejects
	// "0 0 30 2 *". The year is outside its check, as "0 0 29 2 * 2027" is.
	wantStrictError(t, strict, "0 0 H(30-31) 2 *", "0 0 H(30-31)/2 2 *", "0 0 31 H(4-4) *")
	if got, err := DescribeSpec("0 0 H(29-31) 2 * 2027", WithParseOptions(strict|Year)); err != nil || got != "never runs" {
		t.Errorf(`DescribeSpec("0 0 H(29-31) 2 * 2027") with StrictDays = %q, %v; want "never runs", nil`, got, err)
	}
	// H outside the day of the month and the month cannot change the day
	// check: every key rejects these, so no key rejects them too.
	wantStrictError(t, strict, "H 0 30 2 *", "0 H 30 2 *", "0 0 30 2 H", "0 0 30 2 *")
	if _, err := NewParser(strict).WithHashKey("job-a").DescribeSpec("0 0 H(28-31) 2 *"); err == nil {
		t.Error(`DescribeSpec("0 0 H(28-31) 2 *") with key "job-a" and StrictDays: want an error`)
	}
}

// literalForm matches the field-by-field fallback.
var literalForm = regexp.MustCompile(`^(?:second|minute|hour|day of month|month|day of week|year) `)

// numberRange matches a range of numbers in a spec, such as a year range.
// The parser takes a bound with a plus sign ("1-+2000").
var numberRange = regexp.MustCompile(`(\d+)-\+?(\d+)`)

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

// repeatsEnd reports a range whose two ends read the same: "11 pm thru 11 pm".
func repeatsEnd(text string) bool {
	parts := strings.Split(text, " thru ")
	for i := 1; i < len(parts); i++ {
		before, after := strings.Fields(parts[i-1]), strings.Fields(parts[i])
		for n := 1; n <= min(len(before), len(after)); n++ {
			if slices.Equal(before[len(before)-n:], after[:n]) {
				return true
			}
		}
	}
	return false
}

func TestRepeatsEnd(t *testing.T) {
	for text, want := range map[string]bool{
		"from midnight thru 5 am and 11 pm thru 11 pm every day":                    true,
		"every minute from the 6 am hour thru the 6 am hour":                        true,
		"at minutes 5 thru 5 past the hour":                                         true,
		"from midnight thru 5 am and 11 pm thru 11:59 pm every day":                 false,
		"every 2 hours from the midnight hour thru the 10 am hour":                  false,
		"at the top of a fixed hour between 5 am and 11 am and every 7 hours after": false,
	} {
		if got := repeatsEnd(text); got != want {
			t.Errorf("repeatsEnd(%q) = %v, want %v", text, got, want)
		}
	}
}

// FuzzDescribeSpec checks that every spec that parses gets a description in
// the grammar or the literal fallback, and the literal fallback only with H,
// which has forms for some combinations (G10). Specs with a range of more than 100 values (years) are
// skipped.
func FuzzDescribeSpec(f *testing.F) {
	for _, tt := range describeGoldens {
		f.Add(tt.spec)
	}
	f.Add("1,,2 * * * *")
	f.Add("0-59 0-23 * * *")
	f.Add("+0 0 1 1 0")
	f.Add("0 0 1 1 0 22-99")
	f.Add("0 13 * * *")

	g, ng := describeGrammars()
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
			checkDescribed(t, p, spec, g, ng)
		}
	})
}

// checkDescribed checks what p says about spec, if p parses it: a nil error,
// a description in the grammar or the literal form, and a note in its grammar.
func checkDescribed(t *testing.T, p Parser, spec string, g, ng *regexp.Regexp) {
	t.Helper()
	if _, err := p.WithHashKey("fuzz").Parse(spec); err != nil {
		return
	}
	got, err := p.DescribeSpec(spec)
	if err != nil {
		t.Fatalf("DescribeSpec(%q): unexpected error %v", spec, err)
	}
	if !g.MatchString(got) && !literalForm.MatchString(got) {
		t.Fatalf("DescribeSpec(%q) = %q, outside the grammar", spec, got)
	}
	if repeatsEnd(got) {
		t.Fatalf("DescribeSpec(%q) = %q: a range with one value (T9)", spec, got)
	}
	_, _, fields, _, _ := p.specFields(spec) //nolint:dogsled // only the fields matter here
	if literalForm.MatchString(got) && !containsHashExpr(fields) {
		t.Fatalf("DescribeSpec(%q) = %q: literal form without H", spec, got)
	}
	if note, _ := p.DescribeSpecNote(spec); note != "" && !ng.MatchString(note) {
		t.Fatalf("DescribeSpecNote(%q) = %q, outside the note grammar", spec, note)
	}
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
