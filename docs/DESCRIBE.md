# Schedule Descriptions

`DescribeSpec` turns a cron spec into an English sentence for people to read
next to the spec, such as in a UI, a log line or a validation message:

| Spec | Description |
|---|---|
| `0,15,30,45 * * * *` | at 0, 15, 30 and 45 minutes past the hour |
| `5/15 2 * * *` | at 02:05, 02:20, 02:35 and 02:50 every day |
| `0 22-2/2 * * *` | at 22:00, 00:00 and 02:00 every day |
| `*/7 9-17 * * *` | from 09:00 thru 17:56 every day, at 0, 7, 14, … 56 minutes past each hour (every 7 minutes, starting over each hour) |
| `30 */2 * * *` | at 30 minutes past hours 0, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20 and 22 in every day |
| `0 9-17 * * 1-5` | at 09:00, 10:00, 11:00, 12:00, 13:00, 14:00, 15:00, 16:00 and 17:00 on Monday thru Friday |
| `0 0 1/10 * *` | at 00:00 on the 1st, 11th, 21st and 31st of the month; the 31st runs only in months that have one |
| `0 0 13 * 5` | at 00:00 on the 13th of the month when it falls on Friday |
| `0 0 1 1-3 *` | at 00:00 on the 1st of the month in January thru March |

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
  description and a nil error. A spec outside the coverage below, or one
  with `H`, gets the literal form: each written field by name, as written
  ("minute 0, hour 0, day of month L, month *, day of week *"). A
  descriptor such as `@daily` is returned as written.
- `H` needs no hash key: the description does not depend on it.
- The description is made from the spec text as the parser splits it, not
  from the parsed schedule, because the parsed bits have lost what was
  written (`*/15` and `0,15,30,45` parse the same, but only one has a step).
  The values each item fires on follow the parser's rules, including the
  options that change them (`DowStepToSaturday`, `DowOrDom`, `StepWildcard`).
- Descriptions carry no time zone; callers show the location beside them.
- **Coverage:** every field (second, minute, hour, day of month, month, day
  of week, year), written as values, ranges, steps and lists.

## Clock forms

Clock conventions differ by country, so the caller picks the form. Each form
carries the same schedule.

| Form | 12:00 | 00:00, no day named | 00:00, day named | `0 22-2/2 * * *` |
|---|---|---|---|---|
| `Clock24` (default) | 12:00 | 00:00 | 00:00 | at 22:00, 00:00 and 02:00 every day |
| `Clock24Words` | noon | midnight | 00:00 | at 22:00, midnight and 02:00 every day |
| `Clock12` | noon | midnight | 12 am | at 10 pm, midnight and 2 am every day |

The default follows [RFC 3339](https://www.rfc-editor.org/rfc/rfc3339): hours
00–23, as in cron fields and log timestamps. In the 12-hour form, a time on
the hour drops the minutes ("2 am"), and 12:30 keeps the number ("12:30 am",
"12:30 pm"). "12 pm" is never used; 12:00 is "noon". A day is named when
the description has a day clause ("on Friday", "on the 1st of the month").
"midnight" is not used then: "at midnight on Friday", read on a Friday, can
mean the night that ends Friday, while "00:00 on Friday" and "12 am on
Friday" read as its start.

## Rendering rules

The grammar below lists every form a description can take. These rules say
which form a spec gets.

- **Values lead.** A step renders as the values it fires on: `*/15` → "0,
  15, 30 and 45". A step lists up to 6 values for seconds, minutes and
  years, 12 for hours, days and months. A longer step shows the first 3, "…" and the last, with a note
  giving the interval: "(every 7 minutes, starting over each hour)".
  "starting over each …" appears when the gap across the boundary differs
  from the step.
- **Lists.** A list of several items renders the union of their values,
  sorted and once each, with consecutive values as a range: `0-10,*/15` →
  "0 thru 10, 15, 30 and 45". It is never cut short, since no note could
  give the values left out.
- **Every value.** A field that covers its whole range reads as every
  value: `0-59` is "every minute", and `0 0-23 * * *` is "on the hour".
  After a leading "every …", such fields are left out: `* * 9 * * *` is
  "every second for hour 9".
- **Clock times** are used when the second is fixed and either one hour has
  up to 6 minutes, or one minute falls on up to 12 hours written as a list
  or range. Hour steps (`*/2`) keep "past hours 0, 2, …".
- **Span form.** Several minutes over one hour range open with the first
  and last run of the day: "from 09:00 thru 17:56 every day, at … past each
  hour". Readers took "hours 9 thru 17" to end at 17:00, so the span puts
  the last run first.
- **Clock order** follows the hour field, so a wrapped range reads in wrap
  order: "at 22:00, 00:00 and 02:00 every day". With a day named, the times
  run in ascending order within the day: "at 00:00, 02:00 and 22:00 on
  Friday".
- **for / in.** "for" joins a value ("every minute for hour 9") and "in"
  a recurrence ("every second for minute 30 in every hour", "at 30
  minutes past hours 0, 2, … 22 in every day").
- **Every day** follows clock times and the span when no day is named:
  "at 00:00 every day in January thru March". Times within the hour ("past
  the hour", "on the hour") do not get it. A day clause takes its place,
  and that of "in every day" after hour steps: "at 30 minutes past hours
  0, 2, … 22 on Monday".
- **Date clauses** follow the time in the order day, month, year: "at 00:00
  on the 1st of the month in January in year 2027". With the span form the
  day follows "past each hour": "from 09:00 thru 17:56, at 0, 7, 14, … 56
  minutes past each hour on Monday thru Friday". The time's note follows
  the date clauses.
- **Days of the month** are ordinals: "on the 1st thru 7th of the month". A
  step whose days all exist in every month says "of every month" ("the
  1st, 8th, 15th and 22nd of every month"); otherwise "of the month".
- **Short months.** When the day of month fires on days above the 28th,
  the description ends by naming them: "; the 31st runs only in months
  that have one", "; the 29th and 31st run only in months that have
  them", whether the list shows them or "…" stands for them.
- **Days of the week** are names, with Sunday once whether written 0 or 7:
  `0,7` is "Sunday", `6-7` is "Saturday and Sunday". A step walks the
  seven days as the parser does: `5-1/2` is "Friday and Sunday", and `1/2`
  is "Monday, Wednesday, Friday and Sunday" by default, "Monday, Wednesday
  and Friday" with `DowStepToSaturday`.
- **Both day fields.** When both are restricted, a run needs both by
  default: "on the 13th of the month when it falls on Friday". With
  `DowOrDom` either one is enough: "on the 1st of the month or on any
  Monday". With `StepWildcard` as well, a field that starts with `*` or `?`
  counts as unrestricted, so the fields are ANDed, as the parser does.
- **Months** are names: "in January thru March", "in January, April, July
  and October". **Years** name the unit: "in year 2027", "in years 2026
  thru 2028".
- **Commas.** No comma before a trailing clause: "every day", a day, a
  month or a year. A comma stays after the opening span, between list
  items, and inside parentheses. A trailing clause applies to everything
  before it: "at 12 am, 2 am and 10 pm on Friday".
- A range reads "thru" ("0 thru 5"); a range of two adjacent values names
  both ("5 and 6"). A wrapped range reads as its two parts ("50 thru 59 and
  0 thru 10"), and a wrapped step lists its values in full. Minute 0 of
  every hour is "on the hour".
- A range that starts with `*` or `?` reads as the star alone, as the
  parser runs it: `*-5` is `*`, `*-/15` is `*/15`.
- A list with a star item reads as the star: the star selects every
  value, so `*,5` is `*`. In the day fields it also keeps the AND under
  `DowOrDom`, as the parser does: `0 0 *,1 * 1` is "at 00:00 on Monday".

## Grammar (EBNF)

The productions below are what `DescribeSpec` produces.
`TestDescribeSpecGrammar` checks every golden description against them, and
`FuzzDescribeSpec` checks the descriptions of generated specs.

```ebnf
schedule  = time , [ " " , day ] , [ " " , month ] , [ " " , year ] , [ note ] , [ short ]
          | literal ;                                 (* field by field, as written *)

time      = "at " , clocks , [ " every day" ]         (* "every day" when no day is named *)
          | "on the hour"
          | "from " , clock , " thru " , clock , [ " every day" ] , ", " , each
          | perunit
          | lead , { join } ;
each      = "at " , tvals , " past each hour" ;
clocks    = clock , [ { ", " , clock } , " and " , clock ] ;
clock     = clock24 | "midnight" | "noon" | clock12 ;   (* one form per call *)
clock24   = hh , ":" , mm , [ ":" , ss ] ;
clock12   = h12 , [ ":" , mm , [ ":" , ss ] ] , ( " am" | " pm" ) ;
perunit   = "at " , tvals , " past " , ( "the " , ( "minute" | "hour" ) | ( unit | every ) , { join } ) ;
tvals     = vlist , " " , ( "second" | "seconds" | "minute" | "minutes" ) ;
lead      = "every " , unit-sg ;
unit      = ( unit-sg | unit-pl ) , " " , vlist ;
every     = "every " , unit-sg | "every day" ;
join      = " for " , unit | " in " , every ;

day       = "on " , dom , [ " when it falls on " , dow    (* both restricted; default *)
                          | " or on " , dow-any ]       (* both restricted; DowOrDom *)
          | "on " , dow ;
dom       = "the " , ovlist , ( " of the month" | " of every month" ) , [ dnote ] ;
dnote     = " (every " , ( "day" | int , " days" ) , [ ", starting over each month" ] , ")" ;
dow       = dow-item , [ { ", " , dow-item } , " and " , dow-item ] ;
dow-any   = "any " , dow-item , [ { ", any " , dow-item } , " and any " , dow-item ] ;
dow-item  = dayname , [ " thru " , dayname ] ;
month     = "in " , m-item , [ { ", " , m-item } , " and " , m-item ] ;
m-item    = monthname , [ " thru " , monthname ] ;
year      = "in year " , int
          | "in years " , vlist , [ " (every " , ( "year" | int , " years" ) , ")" ] ;
short     = "; the " , ord , " runs only in months that have one"
          | "; the " , ord , { ", " , ord } , " and " , ord , " run only in months that have them" ;

note      = " (" , ival , { "; " , ival } , ")" ;
ival      = "every " , ( unit-sg | int , " " , unit-pl ) ,
            [ ", starting over each " , ( "minute" | "hour" | "day" ) ] ;
list      = item , [ { ", " , item } , " and " , item ] ;
item      = int , [ " thru " , int ] ;
vlist     = list | int , ", " , int , ", " , int , ", … " , int ;
olist     = ord-item , [ { ", " , ord-item } , " and " , ord-item ] ;
ord-item  = ord , [ " thru " , ord ] ;
ovlist    = olist | ord , ", " , ord , ", " , ord , ", … " , ord ;
ord       = int , ( "st" | "nd" | "rd" | "th" ) ;
dayname   = "Sunday" | "Monday" | "Tuesday" | "Wednesday" | "Thursday" | "Friday" | "Saturday" ;
monthname = "January" | "February" | "March" | "April" | "May" | "June" | "July"
          | "August" | "September" | "October" | "November" | "December" ;
unit-sg   = "second" | "minute" | "hour" ;
unit-pl   = "seconds" | "minutes" | "hours" ;
literal   = field , { ", " , field } ;
field     = ( "second" | "minute" | "hour" | "day of month" | "month"
            | "day of week" | "year" ) , " " , text ;  (* the field as written *)
```

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
5. A form the grammar does not cover renders literally. `literalSyntax` in
   `describe_test.go` names the syntax that does, written apart from the
   describer's own check, and `FuzzDescribeSpec` fails when a spec renders
   literally without it or is described with it. When a change covers a
   form, take it out of `literalSyntax`; a fuzz failure then shows any spec
   still left out.
