# Schedule Descriptions

`DescribeSpec` turns a cron spec into an English sentence for people to read
next to the spec, such as in a UI, a log line or a validation message:

| Spec | Description |
|---|---|
| `0,15,30,45 * * * *` | at minutes 0, 15, 30, and 45 past the hour |
| `5/15 2 * * *` | at 2:05 am, 2:20 am, 2:35 am, and 2:50 am every day |
| `0 22-2/2 * * *` | at midnight, 2 am, and 10 pm every day |
| `*/7 9-17 * * *` | from 9 am thru 5:56 pm every day, at minutes 0, 7, 14, … 56 past each hour (every 7 minutes, starting over each hour) |
| `30 */2 * * *` | every 2 hours starting at 12:30 am in every day |

This file covers the API, the rules that pick the wording, the grammar every
description follows, and how the wording was tested and is extended.

## API

```go
func DescribeSpec(spec string, opts ...DescribeOption) (string, error)
func (p Parser) DescribeSpec(spec string, opts ...DescribeOption) (string, error)

func WithParseOptions(o ParseOption) DescribeOption // package function only
```

- The package function parses as `AnalyzeSpec` does: the standard parser,
  or the options given with `WithParseOptions`. The `Parser` method uses the
  parser's own settings and ignores `WithParseOptions`.
- An invalid spec returns the parse error. A valid spec always gets a
  description and a nil error. A spec that never runs reads "never runs".
  Specs outside the fields covered so far, and specs with `H`, get the
  literal form: each written field by name, as written ("minute 0, hour 9,
  day of month *, month *, day of week 1-5"). A descriptor such as `@daily`
  is returned as written.
- `H` needs no hash key: the description does not depend on it. With no
  key, `StrictDays` does not check a spec with `H` in the day of the month
  or the month, as the value the key picks decides that check.
- The description is made from the spec text as the parser splits it, not
  from the parsed schedule, because the parsed bits have lost what was
  written (`*/15` and `0,15,30,45` parse the same, but only one has a step).
- Descriptions carry no time zone; callers show the location beside them.
- **Coverage:** the second, minute and hour fields, and the day of month, day
  of week, month and year written as values, ranges, steps and lists.

## Rules

The grammar below lists every form a description can take. These rules say
which form a spec gets. Each has an ID, which the grammar cites.

### General

| ID | Rule |
|---|---|
| G1 | A description says what the scheduler runs: each golden in `describe_test.go` was checked against the runs `Next()` computes |
| G2 | A parser oddity is described as it runs |
| G3 | The description is built from the spec text as the parser splits it, and its values follow the parser's options |
| G6 | Wording is for people |
| G7 | Clause order: time, day, month, year, then the note, then the short-month clause |
| G8 | No comma before a trailing clause such as "every day". A list of three or more takes the serial comma: "at minutes 0, 15, 30, and 45 past the hour" |
| G10 | A combination of fields the grammar has no form for renders field by field |
| G11 | A spec that never runs reads "never runs": a day that none of the written months and years has (`0 0 30 2 *`, `0 0 29 2 * 2027`) |

### Time fields

| ID | Rule |
|---|---|
| T1 | Values lead. Seconds and minutes list up to 6 values. Past that, evenly spaced values show the first 3, "…", the last, and a note: "(every 7 minutes, starting over each hour)". Two fields cut short share one parenthesis, joined with "; ". "starting over each …" appears when the gap across the boundary differs from the step. Hours follow R8, R9 and T4 |
| T2 | A list renders the union of its values, sorted and once each, with consecutive values as a range. A list that a step describes exactly renders as that step (`1,3,5,7,9,11,13,15` reads like `1-15/2`) |
| T3 | A field that covers its range is every value. After a leading "every …", such fields are left out. T3 comes first: however the field is written (a range, a list, `1-7`, a wrapped range), it reads as the star before T4 and D7 apply |
| T4 | Clock times when the second is fixed and either one hour has up to 6 minutes, or one minute falls on hours written as a list (all values; a list of every hour is T3) or a range of up to 6 hours |
| T5 | Span form: several minutes over one hour range open with the first and last run of the day: "from 9 am thru 5:56 pm every day, at minutes 0, 7, 14, … 56 past each hour" |
| T6 | Hours read in clock order: a wrapped range is shorthand for two ranges ("at midnight, 2 am, and 10 pm every day"; on a named day "at 12 am, 2 am, and 10 pm on Friday") |
| J1 | "for" joins a value. "in every …" follows a step. Plain "every day" follows clock times, the span and an hour range. A day clause takes the place of "every day" and "in every day" |
| T9 | A range reads "thru". Two adjacent values name both ("5 and 6"). A wrapped range reads in two parts and a wrapped step lists in full, sorted low to high: "at minutes 0 thru 10 and 50 thru 59 past the hour", "on the 1st thru 5th and 25th thru 31st of the month". Days of the week and months are the exception (D4, D6). An hour range's two parts share one "from": "from midnight thru 5:56 am and 8 pm thru 11:56 pm every day". So do a wrapped hour step's, each from its first run to its last: "every 2 hours from midnight thru 10 am and 8 pm thru 10 pm in every day". A part of one run reads "at" its time: "every 2 hours from midnight thru 10 am and at 10 pm in every day"; with minutes in front, "during" its hour: "every minute every 2 hours from the midnight hour thru the 10 am hour and during the 10 pm hour in every day". With one minute, a part of one hour of a wrapped range reads "at" its run: "at 30 minutes past every hour from midnight thru 5 am and at 11:30 pm every day", "at 12:30 am and at 30 minutes past every hour from 6 pm thru 11 pm every day"; with one second leading, "at 5 seconds past the top of every hour from midnight thru 5 am and at 11:00:05 pm every day". Several seconds or every second read that part "during" its hour, or as the hour past the top: "at seconds 0 and 30 within minute 5 in every hour from midnight thru 5 am and during the 11 pm hour every day", "at seconds 0 and 30 past the top of the midnight hour and every hour from 6 pm thru 11 pm every day". A named day changes nothing: "from 12 am thru 5:30 am and 8 pm thru 11:30 pm, at minutes 0 and 30 past each hour on Monday" |
| T11 | A list with a star item reads as the star |
| T12 | A time on the hour drops the minutes ("2 am"); "12:30 am", "12:30 pm"; never "12 pm" |

### Clock, hours and minutes

| ID | Rule |
|---|---|
| R1 | Times are on the 12-hour clock |
| R2 | 12:00 am is "midnight", or "12 am" on a named day; 12:00 pm is "noon" |
| R3 | A numbered hour is "the 9 am hour", "the midnight hour", "the 9 am and 5 pm hours". A list that starts with midnight takes no article: "midnight, 6 am, noon, and 6 pm hours" |
| R4 | Minute 0 alone is "the top of …": "at the top of every hour" |
| R5 | A single value keeps its unit after the number ("at 5 minutes past the hour"); a list puts the unit first ("at minutes 0 and 30 …") |
| R6 | Seconds fall "within" a minute. Minutes of the generic hour are "past the hour"; in a named hour they fall "during" it |
| R7 | A fixed second under a fixed minute and hour reads as a clock time ("at 2:30:15 am") |
| R8 | An hour step past 6 values reads as the step. It states the start, with the minute folded in ("every 2 hours starting at 12:30 am in every day"), and the end when the range isn't the whole day or the step doesn't divide 24 ("every 2 hours from 9:15 am thru 11:15 pm in every day"). A wrapped step reads in two parts (T9) |
| R9 | One minute on an hour range past 6 values states the range's start and end: "at the top of every hour from 9 am thru 5 pm every day", "at 30 minutes past every hour from 9 am thru 5 pm every day". A wrapped range reads in two parts (T9) |
| R10 | An hour step of up to 6 values (`*/6`) names its hours, followed by "in every day": "at the top of midnight, 6 am, noon, and 6 pm hours in every day", "at minutes 0 and 30 during … hours in every day" |
| R11 | Several minutes, or every minute, on an hour step past 6 values: the minutes, then the step: "at minutes 0, 15, 30, and 45 every 2 hours starting with the midnight hour in every day", "every minute every 3 hours from the 1 am hour thru the 10 pm hour in every day". A wrapped step reads in two parts (T9) |
| R12 | Every minute or every second on an hour range past 6 values states the first and last run: "every minute from 9 am thru 5:59 pm every day". Several minutes on a wrapped range past 6 values take the span form. A wrapped range reads in two parts (T9) |
| C1 | Seconds other than 0 lead: "every second", "at 5 seconds", "at seconds 0 and 30". The minutes join with "within" ("within a minute", "within every minute", "within minute 30", "within minutes 0 and 30"), minute 0 with "past the top of", and after "every second" with "for" ("for minute 30"). The hours then join as for minutes: "in every hour", "during the 9 am hour", R9–R12 |

### Date fields

| ID | Rule |
|---|---|
| D1 | Date clauses follow the time: day, month, year. With the span form, the day follows "past each hour" |
| D2 | Days of the month are ordinals, with one "the" before them: "the 1st, 5th, 7th, and 10th of the month". They list up to 12 values. Three or more evenly spaced days that are not consecutive and all exist in every month say "of every month", written as a step or a list (T2); otherwise "of the month". The same days read the same |
| D3 | Short months: "; the 31st runs only in months that have one", "; the 29th and 31st run only in months that have them" |
| D4 | Days of the week are names, from the values the parser selects. Sunday appears once. A step walks the seven days as the parser does (`DowStepToSaturday` included). A wrapped range reads unsplit, in week order: "on Friday thru Monday"; two days name both, "on Saturday and Sunday"; a wrapped step lists in week order: "on Friday and Sunday" |
| D5 | Both day fields: "when it falls on" (AND, the default), "or on any" (`DowOrDom`). With `StepWildcard`, a field starting with `*` or `?` keeps AND logic under `DowOrDom`, and its values still restrict the days. `0 9 */2 * MON` runs on Mondays that fall on odd days |
| D6 | Months are names. A wrapped range reads unsplit, in year order: "in November thru February"; two months name both, "in December and January"; a wrapped step lists in year order: "in November and January". Years name the unit ("in year 2027", "in years 2026 thru 2028") and list up to 6 values |
| D7 | A named day is Sunday thru Saturday: a day of the week in the day clause, alone, in a list or range, or with "any". A day of the week field that covers the week is every day, not a named day (T3). A day of the month or a month is not a named day: "at midnight on the 1st of the month", "at 12 am on the 13th of the month when it falls on Friday" |

## Grammar (EBNF)

The productions below are what `DescribeSpec` produces, in
[Lark](https://lark-parser.readthedocs.io/) notation. `describeGrammar` in
`describe_test.go` is the same grammar as a regular expression:
`TestDescribeSpecGrammar` checks every golden description against it, and
`FuzzDescribeSpec` checks the descriptions of generated specs. The literal
form (G10) and descriptors are outside the grammar.

What a context-free grammar can't check is kept by the goldens: the value
limits (T1, T4, R8–R12), the hour order (T6) and the values themselves (G1).

```ebnf
// "x" literal, [x] optional, (x)* repeat, a | b choice.
// Spaces between words are implied.

start: schedule
     | "never" "runs"                                                     // G11

schedule: time [day] [month] [year] [note] short*                         // G7

time: clocktime | span | hourstep | parts

// T4, R1, R2, R7, J1: set clock times
clocktime: "at" list{clock} [everyday]

// T5, R12: span form, the first and last run lead
span: "from" list{crange} [everyday] "," inner "past" "each" "hour"

// R8: one minute on an hour step past 6 values, the minute folded in
hourstep: "every" INT "hours" ("starting" "at" clock | hourrange | oneparts) [inevery]
oneparts: "from" crange "and" "at" clock | "at" clock "and" "from" crange   // R8, T9: a part of one run

// C1: seconds, minutes, then hours, finest first
parts: inner [hourjoin]
     | top tophours                                                       // R4, R9, R10
     | "at" clock "and" top "every" "hour" "from" crange [everyday]      // R9, T9: a first part of one hour
     | "at" clock "and" inner "in" "every" "hour" "from" crange [everyday]   // C1, T9: a first part of one hour
     | "every" ("minute" | "second") hourrange [everyday]                 // R12
     | secs "within" "every" "minute" hourrange [everyday]                // C1, R12

// C1, R5, T3: what leads, with the minutes joined to the seconds
inner: secs [minjoin]
     | "at" "minutes" vlist                                               // R5
     | "every" "minute"                                                   // T3
     | "every" "second" [forjoin]                                         // T3, J1
secs: "at" INT ("second" | "seconds") | "at" "seconds" vlist              // C1, R5
minjoin: "within" ("a" "minute" | "every" "minute" | "minute" INT | "minutes" vlist)   // C1, R6
forjoin: "for" ("minute" INT | "minutes" vlist)                           // C1, J1

// R4, R5, R9, R10: one minute, any minute, on the hour forms
top: "at" "the" "top" "of"                                                // R4
   | "at" INT ("minute" | "minutes") "past"                               // R5
   | secs "past" "the" "top" "of"                                         // C1, R4
tophours: "the" "hour"                                                    // R6
        | "every" "hour" [hourrange] [everyday]                           // R9, J1
        | "every" "hour" "from" crange "and" "at" clock [everyday]         // R9, T9: a last part of one hour
        | "every" "hour" "from" crange "and" "the" clock "hour" [everyday] // T9: a last part, several seconds
        | "the" clock "hour" "and" "every" "hour" "from" crange [everyday] // T9: a first part, several seconds
        | hours [inevery]                                                 // R3, R10, J1
        | hstep                                                           // R11

// R6, R9, R10, R11: the hours joined to what comes before
hourjoin: "past" "the" "hour"                                             // R6
        | "in" "every" "hour" [hourrange [everyday]]                      // J1, R9
        | "during" hours [inevery]                                        // R6, R10
        | hstep                                                           // R11
        | "in" "every" "hour" "from" crange "and" ("at" clock | "during" "the" clock "hour") [everyday]   // C1, T9: a last part of one hour
        | "during" "the" clock "hour" "and" "in" "every" "hour" "from" crange [everyday]   // C1, T9: a first part of one hour
hstep: "every" INT "hours" ("starting" "with" hours | "from" list{hcrange} | hone) [inevery]   // R11
hone: "from" hcrange "and" "during" "the" clock "hour" | "during" "the" clock "hour" "and" "from" hcrange   // R11, T9: a part of one run
hcrange: "the" clock "hour" "thru" "the" clock "hour"                    // R11, T9: a wrapped step is two, in clock order

hours: "the" clock "hour" | "the" (CLOCK12 | "noon") hourtail | "midnight" hourtail   // R3
hourtail: ("and" clock | ("," clock)+ "," "and" clock) "hours"            // R3: no article before a list that starts with midnight
hourrange: "from" list{crange}                                            // R8, R9, R12, T9
crange: clock "thru" clock                                                // T9: a wrapped range is two, in clock order
everyday: "every" "day"                                                   // J1
inevery: "in" "every" "day"                                               // J1
clock: CLOCK12 | "midnight" | "noon"                                      // R1, R2, T12

// D1–D7: the date clauses
day: "on" dom [andor] | "on" dow                                          // D5, D7
andor: "when" "it" "falls" "on" dow                                       // D5 AND
     | "or" "on" list{anyitem}                                            // D5 OR
anyitem: "any" dowitem
dom: "the" olist "of" ("the" | "every") "month"                           // D2
olist: list{oitem} | ORD "," ORD "," ORD "," "…" ORD
oitem: ORD ["thru" ORD]
dow: list{dowitem}                                                        // D4
dowitem: DAYNAME ["thru" DAYNAME]
month: "in" list{mitem}                                                   // D6
mitem: MONTHNAME ["thru" MONTHNAME]
year: "in" "year" INT | "in" "years" vlist                                // D6
short: ";" "the" list{ORD} ("runs" | "run") "only" "in" "months" "that" "have" ("one" | "them")   // D3

note: "(" ival (";" ival)* ")"                                            // T1
ival: "every" (unit | INT units) ["," "starting" "over" "each" unit]
unit: "second" | "minute" | "hour" | "day" | "month" | "year"
units: "seconds" | "minutes" | "days" | "months" | "years"

list{x}: x | x "and" x | x ("," x)+ "," "and" x                          // G8
vlist: list{vitem} | INT "," INT "," INT "," "…" INT                      // T1, T9
vitem: INT ["thru" INT]

CLOCK12: /(1[0-2]|[1-9])(:[0-5][0-9]){1,2} [ap]m|(1[01]|[1-9]) [ap]m|12 am/   // T12, R2
INT: /[0-9]+/
ORD: /[0-9]+(st|nd|rd|th)/
DAYNAME: "Sunday" | "Monday" | "Tuesday" | "Wednesday" | "Thursday" | "Friday" | "Saturday"
MONTHNAME: "January" | "February" | "March" | "April" | "May" | "June" | "July"
         | "August" | "September" | "October" | "November" | "December"
```

## How the wording was chosen

The wording is for people. It was tested with blind reader trials:

1. Each reader gets only descriptions, never the specs, plus a start time.
2. The reader lists the next runs. The start times sit near a boundary (the
   end of an hour, a day or a month), where a misreading shows.
3. Answers are scored against the runs go-cron actually computes for the
   spec.

Blind reader trials are a proxy for people: they show where a sentence's
structure breaks (a clause skipped, a parenthesis ignored, a rule
misapplied), and their misses are checked against everyday usage. A wording is chosen because people
would say it and the trials show it carries the schedule, not for a score.
Examples from the trials:

- Values-first steps ("at minutes 0, 7, 14, … 56 past the hour") replaced
  words-only steps ("every 7 minutes past the hour"), which readers took as
  a steady rhythm or as one run at :07.
- The span form leads with the first and last run, because readers dropped
  the runs in the last hour of "hours 9 thru 17".
- "every day" follows clock times: without it, readers noted that nothing
  said daily.

## Extending the wording

When a parser option or a new field form changes what a spec means:

1. Add golden rows to `describeGoldens` in `describe_test.go`, one per new
   form, with the description you want, and check each against the runs
   `Next()` computes.
2. Add the production to the EBNF above and to `describeGrammar` in the same
   test file, citing the rule it carries. One grammar covers every option
   set: a description must mean one schedule whichever options produced it.
3. For new wording, run a reader trial as described above before settling
   it, with start times that cross the boundary the wording is about.
4. Add the rule that decides between forms to "Rules".
