# Schedule Descriptions

`DescribeSpec` turns a cron spec into an English sentence for people to read
next to the spec, such as in a UI, a log line or a validation message:

| Spec | Description |
|---|---|
| `0,15,30,45 * * * *` | at minutes 0, 15, 30, and 45 past the hour |
| `5/15 2 * * *` | at 2:05 am, 2:20 am, 2:35 am, and 2:50 am every day |
| `0 22-2/2 * * *` | at 10 pm, midnight, and 2 am every day |
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
  description and a nil error.
  Specs outside the fields covered so far, and specs with `H`, get the
  literal form: each written field by name, as written ("minute 0, hour 9,
  day of month *, month *, day of week 1-5"). A descriptor such as `@daily`
  is returned as written.
- `H` needs no hash key: the description does not depend on it.
- The description is made from the spec text as the parser splits it, not
  from the parsed schedule, because the parsed bits have lost what was
  written (`*/15` and `0,15,30,45` parse the same, but only one has a step).
- Descriptions carry no time zone; callers show the location beside them.
- **Coverage:** the second, minute and hour fields.

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
| G7 | The note follows the time |
| G8 | No comma before a trailing clause such as "every day". A list of three or more takes the serial comma: "at minutes 0, 15, 30, and 45 past the hour" |
| G10 | A combination of fields the grammar has no form for renders field by field |

### Time fields

| ID | Rule |
|---|---|
| T1 | Values lead. Seconds and minutes list up to 6 values. Past that, evenly spaced values show the first 3, "…", the last, and a note: "(every 7 minutes, starting over each hour)". "starting over each …" appears when the gap across the boundary differs from the step. Hours follow R8, R9 and T4 |
| T2 | A list renders the union of its values, sorted and once each, with consecutive values as a range. A list that a step describes exactly renders as that step (`1,3,5,7,9,11,13,15` reads like `1-15/2`) |
| T3 | A field that covers its range is every value. After a leading "every …", such fields are left out |
| T4 | Clock times when the second is fixed and either one hour has up to 6 minutes, or one minute falls on hours written as a list (all values) or a range of up to 6 hours |
| T5 | Span form: several minutes over one hour range open with the first and last run of the day: "from 9 am thru 5:56 pm every day, at minutes 0, 7, 14, … 56 past each hour" |
| T6 | Hours read in the order the field fires: a wrapped range reads in wrap order ("at 10 pm, midnight, and 2 am") |
| J1 | "for" joins a value. "in every …" follows a step. Plain "every day" follows clock times, the span and an hour range |
| T9 | A range reads "thru". Two adjacent values name both ("5 and 6"). A wrapped range reads in two parts and a wrapped step lists in full. Minutes and seconds are sorted low to high: "at minutes 0 thru 10 and 50 thru 59 past the hour" |
| T11 | A range starting with `*` or `?`, and a list with a star item, read as the star |
| T12 | A time on the hour drops the minutes ("2 am"); "12:30 am", "12:30 pm"; never "12 pm" |

### Clock, hours and minutes

| ID | Rule |
|---|---|
| R1 | Times are on the 12-hour clock |
| R2 | 12:00 am is "midnight" and 12:00 pm is "noon" |
| R3 | A numbered hour is "the 9 am hour", "the midnight hour", "the 9 am and 5 pm hours" |
| R4 | Minute 0 alone is "the top of …": "at the top of every hour" |
| R5 | A single value keeps its unit after the number ("at 5 minutes past the hour"); a list puts the unit first ("at minutes 0 and 30 …") |
| R6 | Seconds fall "within" a minute. Minutes of the generic hour are "past the hour"; in a named hour they fall "during" it |
| R7 | A fixed second under a fixed minute and hour reads as a clock time ("at 2:30:15 am") |
| R8 | An hour step past 6 values reads as the step. It states the start, with the minute folded in ("every 2 hours starting at 12:30 am in every day"), and the end when the range isn't the whole day or the step doesn't divide 24 ("every 2 hours from 9:15 am thru 11:15 pm in every day") |
| R9 | An hour range past 6 values states its start and end: "at the top of every hour from 9 am thru 5 pm every day" |

## Grammar (EBNF)

The productions below are what `DescribeSpec` produces, in
[Lark](https://lark-parser.readthedocs.io/) notation. `describeGrammar` in
`describe_test.go` is the same grammar as a regular expression:
`TestDescribeSpecGrammar` checks every golden description against it, and
`FuzzDescribeSpec` checks the descriptions of generated specs. The literal
form (G10) and descriptors are outside the grammar.

What a context-free grammar can't check is kept by the goldens: the value
limits (T1, T4, R8, R9), the hour order (T6) and the values themselves (G1).

```ebnf
// "x" literal, [x] optional, (x)* repeat, a | b choice.
// Spaces between words are implied.

start: schedule

schedule: time [note]                                                     // G7

time: clocktime | toptime | perunit | span | lead | hourstep

clocktime: "at" list{clock} [everyday]                                    // T4, R7, J1

toptime: "at" "the" "top" "of" "every" "hour" [hourrange] [everyday]      // R4, R9, J1
       | "at" "the" "top" "of" hours "in" "every" "day"                   // R3, J1

perunit: "at" INT ("minute" | "minutes") "past" "the" "hour"              // R5
       | "at" "minutes" vlist minplace [note]                             // R5, R6
       | "at" INT ("second" | "seconds") "within" "a" "minute"            // R6
       | "at" INT ("second" | "seconds") "past" "the" "top" "of" "every" "hour"           // R4, R6
       | "at" INT ("second" | "seconds") "within" "minutes" vlist "in" "every" "hour"     // R6, J1
       | "at" "seconds" vlist "within" "minute" INT "in" "every" "hour"   // R5, R6, J1
minplace: "past" "the" "hour" | "during" hours                            // R6

span: "from" clock "thru" clock [everyday] "," "at" "minutes" vlist "past" "each" "hour"   // T5

lead: "every" ("minute" | "second") [leadjoin]                            // T3
leadjoin: "during" hours                                                  // R6
        | "for" "minute" INT "in" "every" "hour"                          // J1

hourstep: "every" INT "hours" ("starting" "at" clock | hourrange) [inevery]   // R8

hours: "the" clock "hour" | "the" list2{clock} "hours"                    // R3
hourrange: "from" clock "thru" clock                                      // R8, R9
everyday: "every" "day"                                                   // J1
inevery: "in" "every" "day"                                               // J1
clock: CLOCK12 | "midnight" | "noon"                                      // R1, R2, T12

note: "(" "every" (unit | INT units) ["," "starting" "over" "each" unit] ")"   // T1
unit: "second" | "minute" | "hour"
units: "seconds" | "minutes"

list{x}: x | x "and" x | x ("," x)+ "," "and" x                          // G8
list2{x}: x "and" x | x ("," x)+ "," "and" x
vlist: list{vitem} | INT "," INT "," INT "," "…" INT                      // T1, T9
vitem: INT ["thru" INT]

CLOCK12: /(1[0-2]|[1-9])(:[0-5][0-9]){1,2} [ap]m|(1[01]|[1-9]) [ap]m/     // T12
INT: /[0-9]+/
```

## How the wording was chosen

The wording is for people. It was tested with blind reader trials:

1. Each reader gets only descriptions, never the specs, plus a start time.
2. The reader lists the next runs. The start times sit near a boundary (the
   end of an hour, a day or a month), where a misreading shows.
3. Answers are scored against the runs go-cron actually computes for the
   spec.

The readers were AI assistants, with spot checks by people. The assistants
are a proxy for people: they show where a sentence's structure breaks (a
clause skipped, a parenthesis ignored, a rule misapplied), and their misses
are checked against everyday usage. A wording is chosen because people
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
