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
func DescribeSpecNote(spec string, opts ...DescribeOption) (string, error)
func (p Parser) DescribeSpecNote(spec string, opts ...DescribeOption) (string, error)

func WithParseOptions(o ParseOption) DescribeOption // package function only
```

- The package function parses as `AnalyzeSpec` does: the standard parser,
  or the options given with `WithParseOptions`. The `Parser` method uses the
  parser's own settings and ignores `WithParseOptions`.
- An invalid spec returns the parse error. A valid spec always gets a
  description and a nil error. A spec that never runs reads "never runs",
  within the limit in G11.
  Combinations of `H` that have no form (H17, H20) get the literal
  form: each written field by name, as written ("minute 0, hour 9, day
  of month H/10, month *, day of week 1-5"). A descriptor reads as its
  cron equivalent (P2), and an interval as it runs (P3). The `Parser`
  method uses the parser's minimum `@every` interval
  (`WithMinEveryInterval`); the package function uses the default, 1s.
- `H` needs no hash key: the description does not depend on it. With no
  key, `StrictDays` does not check a spec with `H` in the day of the month
  or the month when the value the key picks decides that check. When no
  key finds the day (`0 0 H(30-31) 2 *`), the parser's error returns.
- The description is made from the spec text as the parser splits it, not
  from the parsed schedule, because the parsed bits have lost what was
  written (`*/15` and `0,15,30,45` parse the same, but only one has a step).
- Descriptions carry no time zone; callers show the location beside them.
- `DescribeSpecNote` spells out the values the hash key picks, which the
  description leaves as "a fixed …": "at minutes h, h+15, h+30, and h+45
  past the hour, where h is a hashed minute from 0 to 14". It is "" unless
  a field has a hashed step or a hashed day of the month (N1–N8).
- **Coverage:** the second, minute and hour fields, and the day of month, day
  of week, month and year written as values, ranges, steps and lists; the
  `L` and `W` days of month (`DomL`, `DomW`) and the `#` days of week
  (`DowNth`, `DowLast`); `H` in the second, minute, hour, day of month, day
  of week and month fields (`Hash`); the descriptors `@yearly` thru
  `@hourly`, `@triggered`, `@manual` and `@none`, and the intervals
  `@every`, `@every_aligned` and `@every_start` (`Descriptor`).

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
| G11 | A spec that never runs reads "never runs": a day that none of the written months and years has (`0 0 30 2 *`, `0 0 29 2 * 2027`, `0 0 31W 4 *`, `0 0 L-30 2 *`), a day that cannot be the counted day of the week (`0 0 1 * MON#2`), and an H day that no key can find in them (`0 0 H(30-31) 2 *`). Every `H` in a spec takes its value from one hash, so the test covers the combinations the parser can produce: `0 0 H(29-31) 1 H(1-3) 2028` never runs, as none of its three pairs of a day and a day of the week occurs in January 2028. Past 8,192 hash values, each `H` is tested on its own. It reads the calendar, not `Next`, which looks 5 years ahead: `0 0 * 2 MON#5` is described, as it runs in 2044 |

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
| T9 | A range reads "thru". Two adjacent values name both ("5 and 6"). A wrapped range reads in two parts and a wrapped step lists in full, sorted low to high: "at minutes 0 thru 10 and 50 thru 59 past the hour", "on the 1st thru 5th and 25th thru 31st of the month". Days of the week and months are the exception (D4, D6). An hour range's two parts share one "from": "from midnight thru 5:56 am and 8 pm thru 11:56 pm every day". So do a wrapped hour step's, each from its first run to its last: "every 2 hours from midnight thru 10 am and 8 pm thru 10 pm in every day". A part of one run reads "at" its time: "every 2 hours from midnight thru 10 am and at 10 pm in every day"; with minutes in front, "during" its hour: "every minute every 2 hours from the midnight hour thru the 10 am hour and during the 10 pm hour in every day". With one minute, a part of one hour of a wrapped range reads "at" its run: "at 30 minutes past every hour from midnight thru 5 am and at 11:30 pm every day", "at 12:30 am and at 30 minutes past every hour from 6 pm thru 11 pm every day"; with one second leading, "at 5 seconds past the top of every hour from midnight thru 5 am and at 11:00:05 pm every day". Several seconds or every second read that part "during" its hour, or as the hour past the top: "at seconds 0 and 30 within minute 5 in every hour from midnight thru 5 am and during the 11 pm hour every day", "at seconds 0 and 30 past the top of the midnight hour and every hour from 6 pm thru 11 pm every day". A hashed minute reads the same: "at a fixed minute past every hour from midnight thru 5 am and the 11 pm hour every day", "4 runs in every hour during the midnight hour and from 6 pm thru 11 pm, 15 minutes apart, starting from a fixed minute". A named day changes nothing: "from 12 am thru 5:30 am and 8 pm thru 11:30 pm, at minutes 0 and 30 past each hour on Monday". A hashed step reads from its start instead (H3, N2, N4) |
| T11 | A list with a star item or an item that covers the range (`0-6,H`) reads as the star |
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
| D5 | Both day fields: "when it falls on" (AND, the default), "or on any" (`DowOrDom`). With `StepWildcard`, a field starting with `*` or `?` is unrestricted |
| D6 | Months are names. A wrapped range reads unsplit, in year order: "in November thru February"; two months name both, "in December and January"; a wrapped step lists in year order: "in November and January". Years name the unit ("in year 2027", "in years 2026 thru 2028") and list up to 6 values |
| D7 | A named day is Sunday thru Saturday: a day of the week that runs, alone, in a list or range, with "any", or as "the third Friday" or "the last Friday". A day of the week field that covers the week is every day, not a named day (T3). A fixed day is not a named day, nor a counted day of the week, a day of the month, `L`, `W`, `L-n` or a month: "at midnight on the 1st of the month", "at 12 am on the 13th of the month when it falls on Friday" |

### L, W and #

| ID | Rule |
|---|---|
| X1 | "the last day of the month", "the last weekday of the month", "the weekday nearest the 15th", "the third Friday", "the last Friday" |
| X2 | `L-n`: "N days before the last day of the month", with no "on" |
| X3 | A mixed list follows D2: the ordinals and the last day share one "the" ("the 1st, 15th, and last day of the month"). An item of another kind keeps its own words ("the weekday nearest the 15th and the last day of the month"). Items read in the order of the days they name, with the days before the last day at the end, closest first. A list starting with one takes no "on" |
| X4 | Both day fields join as D5; an ORed item takes "any" ("any Monday and any last Friday"), beside a hashed day too ("any Monday and a fixed day of the week"). "a fixed" itself takes none |
| X5 | A nearest weekday above the 28th gets the short-month clause (D3): "; the 31st runs only in months that have one" |
| X6 | `L-n` with n ≥ 28 runs only in months of more than n days. As the whole day clause, not ORed: "; it runs only in months of 31 days". In a list or ORed, the item is named: "; 30 days before the last day runs only in months of 31 days" |

### H (hash)

| ID | Rule |
|---|---|
| H1 | A value the hash key picks is "fixed", and the hash clause closes the description after a semicolon: "at a fixed minute past the hour; the hash key fixes the minute" |
| H2 | A hashed step states its count: "4 runs in every hour, 15 minutes apart, starting from a fixed minute"; "1 or 2 runs" when the hashed start decides the count |
| H3 | A range: "a fixed minute between minutes 0 and 29"; a wrapped range in two parts, sorted (T9): "between minutes 0 and 10 or 50 and 59", a part of one value as that value: "between minutes 0 and 5 or 59"; a wrapped step from its start, where its walk starts: "between minutes 50 and 10"; hours by name: "a fixed hour between the 9 am and 5 pm hours" |
| H4 | `0,H` follows R4: "at the top of the hour and a fixed minute past it" |
| H5 | `0,H/15`: the plain value first: "at the top of the hour and every 15 minutes starting from a fixed minute" |
| H6 | `H,H(0-29)`: "at a fixed minute and a fixed minute between minutes 0 and 29; the hash key fixes both minutes". Two hashed values of one unit merge in that clause wherever they stand: "the minute and both days", "both minutes and the day" |
| H7 | A fixed minute in named hours takes "during" (R6): "at a fixed minute during the 9 am hour every day" |
| H8 | A fixed second under one minute and named hours is set clock times: "at a fixed second at 9 am every day"; with every minute, C1: "at a fixed second within every minute during the 9 am hour" |
| H9 | "every day" follows set times |
| H10 | `H/1` is every value of its range; a step at or past the range size is one fixed value; `H(5-5)` is 5; `*,H` is every value; `H(0-59)` drops "between", as does a wrapped range of every value (`H(1-0)`) |
| H12 | A fixed second and a fixed minute in named hours: "at a fixed second and a fixed minute during the 9 am hour every day" |
| H13 | `0 9,H`: "at 9 am and the top of a fixed hour" (with the count, H15) |
| H14 | Every minute in a hashed hour step: "every minute during 4 hours in every day, 6 hours apart, starting from a fixed hour" |
| H15 | A plain value beside a hashed one in a field states the count, as the two can coincide: "1 or 2 runs in every hour, at the top of the hour and a fixed minute past it" |
| H16 | Minutes under a fixed hour join with "at": "at minutes 0 and 30 at a fixed hour every day", "at minute 30 at a fixed hour every day"; minute 0 keeps R4: "at the top of a fixed hour every day" |
| H17 | Other combinations of H in the time fields render field by field (G10): H with a plain second other than 0, several hashed steps in a field, a hashed step beside a fixed value or with a fixed hour, a wrapped fixed hour range, a wrapped range whose parts are each one value (`H(59-0)`), a ranged hour step, a ranged step under a written minute or restricted hours, a fixed value beside a plain one other than H15's, two hashed values outside the H21 forms, a hashed second step beside several minutes or under an hour step past 6 values, a hashed second in the span form (T5), and a hashed second beside a hashed minute or hour other than H12's and H24's |
| H18 | A hashed day or month: "on a fixed day of the month", "on a fixed day of the week", "in a fixed month"; a range by its values: "between the 1st and 15th", "between Monday and Friday", "between January and June". A wrapped day of the month splits low to high: "between the 1st and 5th or the 25th and 31st", "between the 1st and 5th or the 31st"; a wrapped week or year reads in its order: "between Friday and Monday", "between November and February"; one plain value first: "on the 1st and a fixed day of the month". Beside a restricted other day field a fixed day joins as D5: "at 12 am on a fixed day of the month when it falls on Monday", "on the 1st of the month or on a fixed day of the week" |
| H19 | A hashed date step counts its days or months, as H14: "on 3 or 4 days in every week, 2 days apart, starting from a fixed day", "in 4 months in every year, 3 months apart, starting from a fixed month". In the day of month the count covers the lengths of the written months and years, a hashed month counting every month it can pick, or every month length when none is written: "on 2 to 4 days in every month, 10 days apart, starting from a fixed day". One hash picks a hashed day and month, so the count covers the pairs they can make; past 8,192 hash values, each is counted on its own |
| H20 | Other combinations in the date fields render field by field (G10): a hashed day step beside a restricted other day field, `H` beside `L`, `W` or `#`, a hashed day ORed with a day field that is every day, a wrapped day of the month of one value per part (`H(31-1)`), several hashed items, a hashed step beside a value, or more than one value beside `H` |
| H21 | Two hashed values in one field, and nothing else, read as H6, each in its own words, the whole range first, then low to high. The count (H2) is "1 or 2 runs" when they can fall on one value, "2 runs" when they cannot: "2 runs in every hour, at a fixed minute between minutes 0 and 9 and a fixed minute between minutes 20 and 29". Minutes take the hours as H2 does: "1 or 2 runs in every hour during the 9 am hour, at a fixed minute and a fixed minute between minutes 0 and 29". Hours take minute 0: "1 or 2 runs in every day, at the top of a fixed hour and a fixed hour between midnight and 5 am hours". Seconds take every minute of every hour |
| H22 | Minutes other than one plain value on a hashed hour step read as H14: "at minutes 0 and 30 during 4 hours in every day, 6 hours apart, starting from a fixed hour", "at a fixed minute during 4 hours in every day, 6 hours apart, starting from a fixed hour" |
| H23 | A hashed minute step under an hour step past 6 values: the step as H2, then the hours as R11: "4 runs in every hour, 15 minutes apart, starting from a fixed minute, every 2 hours starting with the midnight hour in every day" |
| H24 | A hashed second takes C1's joins in place of a value: "at a fixed second past the top of every hour", "at a fixed second within minute 30 in every hour", "at a fixed second within a fixed minute in every hour"; at minute 0 of a fixed hour, "at a fixed second at the top of a fixed hour every day". A hashed second step under one minute keeps H2's count: "4 runs within minute 5 in every hour, 15 seconds apart, starting from a fixed second", "4 runs past the top of every hour, 15 seconds apart, starting from a fixed second"; under an hour range it names the last run (R12): "4 runs in every minute from 9 am thru 5:59 pm, 15 seconds apart, starting from a fixed second" |

### Descriptors

| ID | Rule |
|---|---|
| P2 | A descriptor reads as its cron equivalent: `@weekly` as `0 0 * * 0`, "at 12 am on Sunday"; `@hourly` as `0 * * * *`, "at the top of every hour". `@triggered`, `@manual` and `@none` read "only when triggered" |
| P3 | An interval reads as it runs, after the parser's minimum and the cut to whole seconds, from its anchor: "every 1h30m from when scheduling started" (`@every`, `@every_start`), "every 15m from Unix epoch" (`@every_aligned`, and `@every` under `AlignEvery`). A written duration that changed is named in a note: "(written as 500ms, rounded up to the 1s minimum)" (`EveryRoundUp`), "(written as 1.5s, cut to whole seconds)", "(written as 1.7s, rounded up to whole seconds)" (a fractional minimum). `90m` reads "1h30m" |

### Notes (`DescribeSpecNote`)

| ID | Rule |
|---|---|
| N1 | The note is "" unless a field has a hashed step or a hashed day of the month (`H`, `H(1-15)`, `1,H`); a plain hashed second, minute, hour, day of the week or month needs none. A description rendered field by field still gets its note. A field whose hashed items have no note form leaves the whole note "" |
| N2 | A hashed step in the second or minute field reads as its values with a variable, unit first (R5), in the hours of the description (R6, R9, R11, R12, C1), or of the written fields when the description is field by field: "at minutes h, h+15, h+30, and h+45 past the hour, where h is a hashed minute from 0 to 14"; "at seconds h, h+15, h+30, and h+45 within minutes 0 thru 10 and 50 thru 59 in every hour, …". Minutes cut short carry their note (T1): "within minutes 0, 7, 14, … 56 in every hour (every 7 minutes, starting over each hour), where h is …". A wrapped hashed range lists its outcomes as N4 does: "at minutes 50, 55, 0, 5, and 10; 51, 56, 1, and 6; …; or 54, 59, 4, and 9 past the hour, depending on the hash" |
| N3 | The values run to the last one any h has; past the T1 limit, the first 3, "…", the last ("h, h+5, h+10, … h+55"). When only some h have the last value, a tail says which: "at minutes h and h+40 past the hour, where h is a hashed minute from 0 to 39; h+40 runs only when h is below 20". A hashed day past the shortest written month keeps its values, and a tail in the form of D3 names the ones some month lacks: "on days h, h+10, h+20, and h+30 of every month in April, …; h+30 runs only in months that have one"; "…, depending on the hash; the 31st runs only in months that have one" A hashed day beside a hashed month stops at the last day the months its hash picks with it have, as one hash picks both; past 8,192 hash values it keeps its values: "on days h, h+10, and h+20 of the month, where h is a hashed day from 1 to 10" for `0 0 H/10 H(2-3) * 2028`. An outcome no pair leaves a day in drops out, and a lone outcome names no hash: "on the 30th of the month" |
| N4 | The day of the week always lists its outcomes; the day of the month and the month list up to 3, and a wrapped range, otherwise the variable form. Outcomes are ordered by h: "on Sunday, Tuesday, Thursday, and Saturday; or Monday, Wednesday, and Friday, depending on the hash". Each outcome reads in the order it runs ("on Friday and Sunday; or Saturday and Monday"). A wrapped day of the month ends "; a short month skips the days it lacks and changes the last step", as the last step across a short month's end can be cut. A whole range written from another start (`H(1-7)/2`, `H(5-4)/7`) walks from that start, as the parser does: when the step does not divide the range, a day of the week or month lists, an hour reads "at the top of a fixed hour between 5 am and 11 am and every 7 hours after it thru 4 am", and a second or minute has no note; when it divides, the plain form stays |
| N5 | A hashed day of the month: "on day h of every month, where h is a hashed day from 1 to 31; a month without day h is skipped"; beside a plain value "on the 1st and day h of every month, …; a month without day h runs on the 1st only". The tail only when h can pass the 28th, or the shortest written month. A written month is named as in the description (D2): "on day h of the month in January, …", "on days h, h+5, and h+10 of every month in March, …". A hashed month is not named: "on day h of the month, …" |
| N6 | Each clause carries its own explanation; clauses in field order, joined with ", "; the variables h, k, j, m, n, p in turn. Midnight reads as in the description (R2, D7) |
| N7 | A hashed hour step names the fixed hour's range and the step: "at the top of a fixed hour between midnight and 5 am and every 6 hours after it"; "thru" the range's last hour when the range is not the whole day; over a wrapped range, in clock order from its start: "at the top of a fixed hour between 10 pm and 11 pm and every 2 hours after it thru 4 am" |
| N8 | A hashed hour step's note opens with the minutes as its description names them (R4, R5, H14, H22), then N7's hours: "at minutes 0 and 30 during a fixed hour between midnight and 5 am and every 6 hours after it", "at 30 minutes past a fixed hour …", "every minute during a fixed hour …", "at a fixed minute during a fixed hour …" |

## Grammar (EBNF)

The productions below are what `DescribeSpec` produces, in
[Lark](https://lark-parser.readthedocs.io/) notation. `describeGrammar` in
`describe_test.go` is the same grammar as a regular expression:
`TestDescribeSpecGrammar` checks every golden description against it, and
`FuzzDescribeSpec` checks the descriptions of generated specs. The literal
form (G10) is outside the grammar.

What a context-free grammar can't check is kept by the goldens: the value
limits (T1, T4, R8–R12), the hour order (T6) and the values themselves (G1).

```ebnf
// "x" literal, [x] optional, (x)* repeat, a | b choice.
// Spaces between words are implied.

start: schedule
     | "never" "runs"                                                     // G11
     | "only" "when" "triggered"                                          // P2
     | interval                                                           // P3

schedule: time [day] [month] [year] [note] short* [hashkey]               // G7, H1

time: clocktime | span | hourstep | parts | hashed | counted

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
secs: "at" INT ("second" | "seconds") | "at" "seconds" vlist | "at" fixed  // C1, R5, H1
minjoin: "within" ("a" "minute" | "every" "minute" | "minute" INT | "minutes" vlist | fixed)   // C1, R6, H24
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

// H1–H16: values the hash key fixes
hashed: "at" fixed "past" "every" "hour" hourrange [everyday]             // R9
      | "at" fixed "past" "every" "hour" "from" crange "and" "the" clock "hour" [everyday]   // T9: a last part of one hour
      | "at" fixed "past" "the" clock "hour" "and" "every" "hour" "from" crange [everyday]   // T9: a first part of one hour
      | "at" fixed "during" hours [everyday | inevery]                    // H7, R10
      | "at" fixed hstep                                                  // R11
      | "at" (fixed | "minute" INT | "minutes" vlist) "at" fixed [everyday]   // H16
      | "at" "the" "top" "of" fixed [everyday]                            // R4, H16
      | "every" "minute" "during" fixed                                   // R6
      | "at" fixed "at" list{clock} [everyday]                            // H8
      | "at" fixed "and" fixed "during" hours [everyday]                  // H12
      | "at" fixed "at" "the" "top" "of" fixed [everyday]                 // H24, H16
counted: [toplead ","] count "in" "every" unit [cplace] apart             // H2
       | ("every" "minute" | "at" "minutes" vlist | "at" fixed) "during" cnt "hours" "in" "every" "day" apart   // H14, H22
       | count "in" "every" unit [cplace] "," "at" overlap   // H6, H15, H21
       | count "in" "every" unit apart "," hstep                        // H23
       | count ("within" ("minute" INT | "minutes" vlist) [hourjoin] | "past" "the" "top" "of" tophours) apart   // H24
toplead: "at" "the" "top" "of" "the" "hour" | "at" INT ("minute" | "minutes") "past" "the" "hour"
cnt: INT | INT "or" INT
count: cnt ("run" | "runs")
cplace: between | hourrange | "during" hours | "from" crange "and" "during" "the" clock "hour" | "during" "the" clock "hour" "and" "from" crange
apart: "," INT units "apart" "," "starting" "from" fixed
overlap: "the" "top" "of" "the" "hour" "and" fixed "past" "it"             // H4
       | fixed "and" fixed                                                // H6
       | "the" "top" "of" "the" "hour" "and" "every" INT units "starting" "from" fixed   // H5
       | clock "and" "the" "top" "of" fixed                               // H13
       | "the" "top" "of" fixed "and" fixed                               // H21
fixed: "a" "fixed" unit [between | "between" hours]                       // H1, H3
between: "between" units (INT "and" INT ["or" INT ["and" INT]] | INT "or" INT "and" INT)   // H3, T9
hashkey: ";" "the" "hash" "key" "fixes" list{keyunit}                      // H1, H6
keyunit: "the" unit | "both" units

hours: "the" clock "hour" | "the" (CLOCK12 | "noon") hourtail | "midnight" hourtail   // R3
hourtail: ("and" clock | ("," clock)+ "," "and" clock) "hours"            // R3: no article before a list that starts with midnight
hourrange: "from" list{crange}                                            // R8, R9, R12, T9
crange: clock "thru" clock                                                // T9: a wrapped range is two, in clock order
everyday: "every" "day"                                                   // J1
inevery: "in" "every" "day"                                               // J1
clock: CLOCK12 | "midnight" | "noon"                                      // R1, R2, T12

// D1–D7: the date clauses
day: "on" (dom | hdomfix) [andor] | before [andor] | "on" dow | "on" hday   // D5, D7, X2, H18
andor: "when" "it" "falls" "on" (dow | hdowfix)                           // D5 AND, H18
     | "or" "on" (list{anyitem} | hdowany)                                // D5 OR, X4, H18
anyitem: "any" (DAYNAME ["thru" DAYNAME] | NTH DAYNAME)
dom: "the" olist "of" ("the" | "every") "month"                           // D2
   | "the" "weekday" "nearest" "the" ORD                                  // X1
   | domlist "of" "the" "month"                                           // X1, X3
domlist: domel | domel "and" anyel | domel ("," anyel)+ "," "and" anyel   // G8
domel: "the" olist | "the" "last" "weekday" | "the" "weekday" "nearest" "the" ORD
anyel: domel | beforeitem
olist: list{oitem} | ORD "," ORD "," ORD "," "…" ORD
oitem: ORD ["thru" ORD] | "last" "day"                                    // X1, X3
before: list{beforeitem} "of" "the" "month"                               // X2
beforeitem: INT ("day" | "days") "before" "the" "last" "day"
dow: list{dowitem}                                                        // D4
dowitem: DAYNAME ["thru" DAYNAME] | "the" NTH DAYNAME                     // X1
month: "in" list{mitem} | "in" hmonth                                     // D6, H18
mitem: MONTHNAME ["thru" MONTHNAME]

// H18, H19: a day or month the hash key fixes
hday: hdomfix | hdowfix                                                   // H18
    | hcnt ("day" | "days") "in" "every" "month" [hdomrange] dapart        // H19
    | hcnt ("day" | "days") "in" "every" "week" [hdowrange] dapart         // H19
hmonth: "a" "fixed" "month" [hmonthrange]
      | MONTHNAME "and" "a" "fixed" "month"
      | hcnt ("month" | "months") "in" "every" "year" [hmonthrange] "," INT "months" "apart" "," "starting" "from" "a" "fixed" "month"
hdomfix: "a" "fixed" "day" "of" "the" "month" [hdomrange] | "the" ORD "and" "a" "fixed" "day" "of" "the" "month"   // H18, D5
hdowfix: "a" "fixed" "day" "of" "the" "week" [hdowrange] | DAYNAME "and" "a" "fixed" "day" "of" "the" "week"        // H18, D5
hdowany: "a" "fixed" "day" "of" "the" "week" [hdowrange] | "any" DAYNAME "and" "a" "fixed" "day" "of" "the" "week"   // H18, X4
hdomrange: "between" "the" ORD ["or" "the" ORD] "and" ORD ["or" "the" ORD ["and" ORD]]   // H18, D2, T9
hdowrange: "between" DAYNAME "and" DAYNAME
hmonthrange: "between" MONTHNAME "and" MONTHNAME
hcnt: INT | INT "or" INT | INT "to" INT                                   // H19
dapart: "," INT "days" "apart" "," "starting" "from" "a" "fixed" "day"
year: "in" "year" INT | "in" "years" vlist                                // D6
short: ";" "the" list{ORD} ("runs" | "run") "only" "in" "months" "that" "have" ("one" | "them")   // D3, X5
     | ";" ("it" | beforeitem) "runs" "only" "in" "months" "of" INT "days" ["or" "more"]           // X6

interval: "every" DURATION ("from" "when" "scheduling" "started"         // P3
                           | "from" "Unix" "epoch") [ivnote]
ivnote: "(" "written" "as" DURATION "," ivhow ")"                        // P3
ivhow: "rounded" "up" "to" ("the" DURATION "minimum" | "whole" "seconds")
     | "cut" "to" "whole" "seconds"

note: "(" ival (";" ival)* ")"                                            // T1
ival: "every" (unit | INT units) ["," "starting" "over" "each" unit]
unit: "second" | "minute" | "hour" | "day" | "month" | "year"
units: "seconds" | "minutes" | "hours" | "days" | "months" | "years"

list{x}: x | x "and" x | x ("," x)+ "," "and" x                          // G8
vlist: list{vitem} | INT "," INT "," INT "," "…" INT                      // T1, T9
vitem: INT ["thru" INT]

CLOCK12: /(1[0-2]|[1-9])(:[0-5][0-9]){1,2} [ap]m|(1[01]|[1-9]) [ap]m|12 am/   // T12, R2
INT: /[0-9]+/
DURATION: /[-+]?((([0-9]+\.?[0-9]*|\.[0-9]+)(ns|us|µs|μs|ms|s|m|h))+|0)/   // P3: Go's duration
ORD: /[0-9]+(st|nd|rd|th)/
NTH: "first" | "second" | "third" | "fourth" | "fifth" | "last"
DAYNAME: "Sunday" | "Monday" | "Tuesday" | "Wednesday" | "Thursday" | "Friday" | "Saturday"
MONTHNAME: "January" | "February" | "March" | "April" | "May" | "June" | "July"
         | "August" | "September" | "October" | "November" | "December"

// ---------------- notes (DescribeSpecNote) ----------------
// A note is its own text, not a description: it starts at notetext.
notetext: nclause ("," nclause)*                                            // N6

nclause: ntime | nday | nmonth

// N2, N3: a hashed step in a time field, with a variable; N7, N8: hour steps
ntime: "at" ("seconds" | "minutes") vals tplace where [tail]
     | "at" ("seconds" | "minutes") toutcomes tplace "," "depending" "on" "the" "hash"   // N2, N4: a wrapped range lists
     | hlead "a" "fixed" "hour" "between" clock "and" clock "and" "every" INT "hours" "after" "it" ["thru" clock] [note]   // N7, N8, T1
tplace: "past" "the" "hour" | "during" hours | "past" "every" "hour" hourrange | "within" "a" "minute"   // R6, R9, C1
      | "past" "every" "hour" "from" clock "thru" clock "and" "the" clock "hour"   // T9: a last part of one hour
      | "past" "the" clock "hour" "and" "every" "hour" "from" clock "thru" clock   // T9: a first part of one hour
      | "within" "every" "minute" ("during" hours | hourrange)             // C1, R12
      | "within" ("minute" INT | "minutes" vlist) "in" "every" "hour" [note]   // C1, T1
      | "past" "the" "top" "of" "every" "hour"                              // C1, R4
      | nhstep                                                              // R11
nhstep: "every" INT "hours" ("starting" "with" hours | "from" list{hcrange})   // R11, T9
// N8: the minutes of a hashed hour step, as the description names them
hlead: "at" "the" "top" "of"                                                // R4
     | "at" INT ("minute" | "minutes") "past"                              // R5
     | "every" "minute" "during"                                            // H14
     | "at" "minutes" vlist "during"                                        // H22
     | "at" "a" "fixed" "minute" ["between" "minutes" INT "and" INT] "during"   // H22, H3
toutcomes: list{INT} (";" list{INT})* ";" "or" list{INT}                    // N2, N4: values as they run, ordered by h

// N4, N5: date fields
nday: "on" "days" vals ofdays where [tail | shorttail]
    | "on" ["the" ORD "and"] "day" VAR ("of" "every" "month" | "of" "the" "month" [inmonths]) where [daytail]
    | "on" outcomes [ofdays] "," "depending" "on" "the" "hash" [shortdays]
    | "on" outcome ofdays                                                  // N3: one outcome left
shorttail: ";" vterm "runs" "only" "in" "months" "that" "have" "one"        // N3
         | ";" list{vterm} "run" "only" "in" "months" "that" "have" "them"
shortdays: ";" "a" "short" "month" "skips" "the" "days" "it" "lacks" "and" "changes" "the" "last" "step"   // N4
         | ";" "the" ORD "runs" "only" "in" "months" "that" "have" "one"    // N3
         | ";" "the" list{ORD} "run" "only" "in" "months" "that" "have" "them"
ofdays: "of" "every" "month" [inmonths] | "of" "the" "month"                // N5
inmonths: "in" list{mrun}                                                   // N5: a written month
mrun: MONTHNAME ["thru" MONTHNAME]
nmonth: "in" "months" vals where [tail]
      | "in" outcomes "," "depending" "on" "the" "hash"

// values with a variable (G8, T1, N3)
vals: vterm "and" vterm | vterm ("," vterm)+ "," "and" vterm
    | vterm "," vterm "," vterm "," "…" vterm
vterm: VAR ["+" INT]
where: "," "where" VAR "is" "a" "hashed" ("second" | "minute" | "day" | "month") "from" bound ["to" bound] ["or" bound ["to" bound]]
bound: INT ["(" MONTHNAME ")"]
tail: ";" vterm "runs" "only" "when" VAR "is" "below" INT                   // N3
daytail: ";" "a" "month" "without" "day" VAR ("is" "skipped" | "runs" "on" "the" ORD "only")   // N5

// N4: outcomes, ordered by h, each in the order it runs
outcomes: outcome (";" outcome)* ";" "or" outcome
outcome: list{DAYNAME} | list{MONTHNAME}
       | "the" ORD ["and" ORD] | "the" ORD ("," ORD)+ "," "and" ORD
       | "the" ORD "," ORD "," ORD "," "…" ORD                             // T1
VAR: /[hkjmnp]/                                                          // N6
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
