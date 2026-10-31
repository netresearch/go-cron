# Schedule Descriptions

`DescribeSpec` turns a cron spec into an English sentence for people to read
next to the spec, such as in a UI, a log line or a validation message:

| Spec | Description |
|---|---|
| `0,15,30,45 * * * *` | at 0, 15, 30 and 45 minutes past the hour |
| `5/15 2 * * *` | at 02:05, 02:20, 02:35 and 02:50 every day |
| `0 22-2/2 * * *` | at 22:00, 00:00 and 02:00 every day |
| `*/7 9-17 * * *` | from 09:00 thru 17:56 every day, at 0, 7, 14, … 56 minutes past each hour (every 7 minutes, starting over each hour) |
| `30 */2 * * *` | at 30 minutes past hours 0, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20 and 22 of every day |

This file covers the API, the rules that pick the wording, the grammar every
description follows, and how the wording was tested and is extended.

## API

```go
func DescribeSpec(spec string, opts ...DescribeOption) (string, error)
func (p Parser) DescribeSpec(spec string, opts ...DescribeOption) (string, error)

func WithClockForm(f ClockForm) DescribeOption      // Clock24 (default), Clock24Words, Clock12
func WithParseOptions(o ParseOption) DescribeOption // package function only
```

- The package function parses as `AnalyzeSpec` does: the standard parser,
  or the options given with `WithParseOptions`. The `Parser` method uses the
  parser's own settings and ignores `WithParseOptions`.
- An invalid spec returns the parse error. A valid spec always gets a
  description and a nil error.
- The description is made from the spec text as the parser splits it, not
  from the parsed schedule, because the parsed bits have lost what was
  written (`*/15` and `0,15,30,45` parse the same, but only one has a step).
- Descriptions carry no time zone; callers show the location beside them.
- **Coverage so far:** the second, minute and hour fields. A spec whose day
  of month, month, day of week or year is restricted, and a spec with `H`,
  render literally, field by field: `0 9 * * 1-5` → "minute 0, hour 9, day
  of month *, month *, day of week 1-5". A descriptor renders as written
  (`@daily`). Later changes describe those fields and shrink the fallback.

## Clock forms

Clock conventions differ by country, so the caller picks the form. Each form
carries the same schedule.

| Form | 12:00 | 00:00 | `0 22-2/2 * * *` |
|---|---|---|---|
| `Clock24` (default) | 12:00 | 00:00 | at 22:00, 00:00 and 02:00 every day |
| `Clock24Words` | noon | midnight | at 22:00, midnight and 02:00 every day |
| `Clock12` | noon | midnight | at 10 pm, midnight and 2 am every day |

The default follows [RFC 3339](https://www.rfc-editor.org/rfc/rfc3339): hours
00–23, as in cron fields and log timestamps. In the 12-hour form, a time on
the hour drops the minutes ("2 am"), and 12:30 keeps the number ("12:30 am",
"12:30 pm"). "12 pm" is never used; 12:00 is "noon".

## Rendering rules

The grammar below lists every form a description can take. These rules say
which form a spec gets.

- **Values lead.** A step renders as the values it fires on: `*/15` → "0,
  15, 30 and 45". Seconds and minutes list up to 6 values, hours up to 12.
  A longer list shows the first 3, "…" and the last, with a note giving the
  interval: "(every 7 minutes, starting over each hour)". "starting over
  each …" appears when the gap across the boundary differs from the step.
  A list that mixes steps and values renders the union of its values.
- **Clock times** are used when the second is fixed and either one hour has
  up to 6 minutes, or one minute falls on up to 12 hours written as a list
  or range. Hour steps (`*/2`) keep "past hours 0, 2, …".
- **Span form.** Several minutes over one hour range open with the first
  and last run of the day: "from 09:00 thru 17:56 every day, at … past each
  hour". Readers took "hours 9 thru 17" to end at 17:00, so the span puts
  the last run first.
- **Clock order** follows the hour field, so a wrapped range reads in wrap
  order: "at 22:00, 00:00 and 02:00".
- **Every day** follows clock times and the span when no day is named.
  Times within the hour ("past the hour", "on the hour") do not get it.
- **Commas.** No comma before a trailing clause such as "every day". A comma
  stays after the opening span, between list items, and inside parentheses.
- A range reads "thru" ("0 thru 5"); a range of two adjacent values names
  both ("5 and 6"). A wrapped range reads as its two parts ("50 thru 59 and
  0 thru 10"), and a wrapped step lists its values in full. Minute 0 of
  every hour is "on the hour".

## Grammar (EBNF)

The productions below are what `DescribeSpec` produces today.
`TestDescribeSpecGrammar` checks every golden description against them.

```ebnf
schedule  = time
          | literal ;                                 (* not yet covered *)

time      = "at " , clocks , " every day"
          | "on the hour"
          | "from " , clock , " thru " , clock , " every day" , ", " , each , [ note ]
          | perunit , [ note ]
          | lead , { " of " , unit } , [ note ] ;
each      = "at " , tvals , " past each hour" ;
clocks    = clock , [ { ", " , clock } , " and " , clock ] ;
clock     = clock24 | "midnight" | "noon" | clock12 ;   (* one form per call *)
clock24   = hh , ":" , mm , [ ":" , ss ] ;
clock12   = h12 , [ ":" , mm , [ ":" , ss ] ] , ( " am" | " pm" ) ;
perunit   = "at " , tvals , " past " , ( "the " , ( "minute" | "hour" ) | unit , { " of " , unit } ) ;
tvals     = vlist , " " , ( "second" | "seconds" | "minute" | "minutes" ) ;
lead      = "every " , unit-sg ;
unit      = ( unit-sg | unit-pl ) , " " , vlist | "every " , unit-sg | "every day" ;

note      = " (" , ival , { "; " , ival } , ")" ;
ival      = "every " , ( unit-sg | int , " " , unit-pl ) ,
            [ ", starting over each " , ( "minute" | "hour" | "day" ) ] ;
list      = item , [ { ", " , item } , " and " , item ] ;
item      = int , [ " thru " , int ] ;
vlist     = list | int , ", " , int , ", " , int , ", … " , int ;
unit-sg   = "second" | "minute" | "hour" ;
unit-pl   = "seconds" | "minutes" | "hours" ;
literal   = field , { ", " , field } ;
field     = ( "second" | "minute" | "hour" | "day of month" | "month"
            | "day of week" | "year" ) , " " , text ;  (* the field as written *)
```

The full grammar, with days, months, years, `H`, `L`, `W`, `#` and intervals,
is planned in issue #205; each later change adds its productions here.

## How the wording was chosen

The wording is for people. It was tested with blind reader trials:

1. Each reader gets only descriptions, never the specs, plus a start time.
2. The reader lists the next runs. The start times sit near a boundary (the
   end of an hour, a day or a month), where a misreading shows.
3. Answers are scored against the runs go-cron actually computes for the
   spec.

The readers were AI assistants, with spot checks by people. The assistants
are a proxy for people: they show where a sentence's structure breaks (a clause skipped, a parenthesis
ignored, a rule misapplied), and their misses are checked against everyday
usage. A wording is chosen because people would say it and the trials show
it carries the schedule, not for a score. Examples from the trials:

- Values-first steps ("at 0, 7, 14, … 56 minutes past the hour") replaced
  words-only steps ("every 7 minutes past the hour"), which readers took as a
  steady rhythm or as one run at :07.
- Hour ranges became clock times, and the span form leads, because readers
  dropped the runs in the last hour of "hours 9 thru 17".
- "every day" was added after clock times: without it, readers noted that
  nothing said daily.

## Extending the wording

When a parser option or a new field form changes what a spec means:

1. Add golden rows to `describeGoldens` in `describe_test.go`, one per new
   form, with the description you want.
2. Add the production to the EBNF above and to `describeGrammar` in the same
   test file. One grammar covers every option set: a description must mean
   one schedule whichever options produced it.
3. For new wording, run a reader trial as described above before settling
   it, with start times that cross the boundary the wording is about.
4. Keep the rule that decides between forms in "Rendering rules".
