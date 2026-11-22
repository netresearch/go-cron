# Moving Cron Specs to go-cron

This guide is for moving cron specs from another scheduler to go-cron. For
each source scheduler it says how to write its specs for go-cron and which
parser options reproduce its behavior. For moving Go code from robfig/cron,
see [MIGRATION.md](MIGRATION.md).

Many specs parse in go-cron but run on other days, with no error. The most
common case is day-of-week numbers: in Quartz, `6L` is the last Friday, while
go-cron's `6#L` is the last Saturday. Other specs fail to parse and need a known
rewrite (`FRIL` becomes `FRI#L`). Each section therefore starts with what runs
differently without an error, then what fails to parse, then the scheduler's
rules part by part, then a rewrite table.

## How to read this guide

- **Parser setup.** Each section names the parser options its specs assume.
  Pass them to `NewParser` and the parser to `WithParser`:

  ```go
  parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.DowOrDom)
  c := cron.New(cron.WithParser(parser))
  ```

  A table row that needs an option beyond the section's setup names it.
- **Search window.** `Next()` searches through the fifth calendar year after
  the year it starts in. A spec whose runs are further apart is added with no
  error, but once its next run is out of reach, `Next()` returns the zero time
  and the entry does not run again: without `DowOrDom`, `0 9 13 2 FRI` (Friday
  13 February) runs in 2026 and never in 2032. Give the parser a window as
  long as the longest gap between runs. A fixed date on a fixed day of the
  week can be 11 years apart, so this spec needs
  `cron.WithParser(parser.WithMaxSearchYears(11))`.
- **"Runs differently without an error"** tables have four columns: the
  spec; what the source runs; what go-cron runs with the spec as written,
  under only the options it needs to parse (`Second`, `Year`, `Extended`,
  `Descriptor`, `Hash`), or for a source that is not a spec string, its
  literal transcription; and what to write for go-cron.
- **"What it runs"** gives the first run after 2026-11-18 00:00 UTC, in UTC,
  from go-cron's `Next()`. Each go-cron spec in the tables was run that way.
  Rows with a time zone also give the local time. Rows with `H` use the hash
  key `example-job`. Seconds are shown only for specs with a seconds field.
- **Sources.** Each section links the scheduler's own reference. Where its
  docs are silent or disagree with what it runs, the section follows the code
  and links it. Each section says whether the scheduler itself was run.
- **Two entries.** Where a source runs a job when *either* day field matches,
  go-cron can do the same with `DowOrDom`, or with two entries, one per day
  field. On a day that matches both (a Friday the 13th), both entries run, so
  the job runs twice where the source runs it once: prefer `DowOrDom` unless a
  second run does no harm.
- **`-`** is a range everywhere except after `L` in the day of month, where it
  counts back from the last day (`L-3`).

## Summary

| Section | Fields | Day of week | Both day fields set | `L` / `#` / `W` | Time zone |
|---|---|---|---|---|---|
| [Vixie cron, cronie and Cygwin cron](#vixie-cron-cronie-and-cygwin-cron) | 5 | 0 or 7 = Sunday | OR | none (`$` for last day in Vixie cron 4.3) | local; cronie `CRON_TZ=` |
| [BusyBox crond](#busybox-crond) | 5 | 0 = Sunday, no 7 | OR | none | local |
| [nnCron](#nncron) | 6 with year, or 5 | 1 = Monday, 7 = Sunday | AND | none | local |
| [Kubernetes CronJob](#kubernetes-cronjob) | 5 | 0 = Sunday, no 7 | OR | none | `.spec.timeZone` |
| [GitHub Actions and Google Cloud Scheduler](#github-actions-and-google-cloud-scheduler) | 5 | 0 = Sunday | OR | none | UTC, or set per schedule |
| [Jenkins](#jenkins) | 5 | 0 or 7 = Sunday | AND | none (`H` hashes) | `TZ=` first line |
| [Quartz and Quartz.NET](#quartz-and-quartznet) | 6 or 7, seconds first | **1 = Sunday** | one must be `?` | `L`, `#`, `W`, `dL` | per trigger |
| [Spring](#spring) | 6, seconds first | 0 or 7 = Sunday | AND | `L`, `#`, `W`, `dL` | per trigger |
| [Azure Functions and NCrontab](#azure-functions-and-ncrontab) | 5 or 6, seconds first | 0 = Sunday, no 7 | AND | none | UTC, or one per app |
| [Hangfire (Cronos)](#hangfire-cronos) | 5 or 6, seconds first | 0 or 7 = Sunday | AND | `L`, `#`, `W`, `dL` | per job, UTC |
| [AWS EventBridge rules and Scheduler](#aws-eventbridge-rules-and-scheduler) | 6, year last | **1 = Sunday** | one must be `?` | `L`, `#`, `W`, `dL` | UTC; Scheduler per schedule |
| [APScheduler](#apscheduler) | 5, or keywords | **0 = Monday** | AND | `last`, `3rd fri` | per trigger or scheduler |
| [Celery](#celery) | keywords, 5 | 0 = Sunday | AND | none | per app, UTC |
| [croniter](#croniter) | 5, 6 or 7, **seconds last** | 0 or 7 = Sunday | OR | `l`, `#`, `W`, `Ln` | from the start time, UTC |
| [node-cron](#node-cron) | 5 or 6, seconds first | 0 or 7 = Sunday | AND | `L`, `#`, `W`, `dL` | option, local |
| [cron (kelektiv)](#cron-kelektiv) | 5 or 6, seconds first | 0 or 7 = Sunday | OR | none | option, local |
| [systemd timers](#systemd-timers) | calendar event | names only | AND | `~` from the end | suffix, local |
| [Windows Task Scheduler](#windows-task-scheduler) | triggers | names only | one per trigger | `LAST`, `LASTDAY` | start boundary, local |

go-cron itself: five fields by default (seconds with `Second` or
`SecondOptional`, a year with `Year` or `YearOptional`); 0 or 7 = Sunday;
both day fields must match unless `DowOrDom` is set; `L`, `#` and `W` with
`Extended` (`d#L`, not `dL`); the zone from `WithLocation` or a `CRON_TZ=` /
`TZ=` prefix, local time by default.

## Unix crontab

### Vixie cron, cronie and Cygwin cron

Reference: [Vixie cron crontab(5)](https://github.com/vixie/cron/blob/master/crontab.5),
[cronie crontab(5)](https://github.com/cronie-crond/cronie/blob/cronie-1.7.2/man/crontab.5),
[Cygwin cron package](https://cygwin.com/packages/summary/cron.html) (Vixie cron 4.1).
The three convert the same way except where a line names one of them.
None of them was run; the lines rest on the man pages and the code.

**Runs differently without an error**

| Spec | Source runs | go-cron runs, unchanged | For go-cron |
|---|---|---|---|
| `0 9 13 * FRI` (both day fields restricted) | the 13th and every Friday | Friday the 13th only | `DowOrDom`, or two entries |
| `0 9 */2 * MON` (a field starting with `*` counts as unrestricted) | Mondays on odd days | Mondays on odd days (same); with `DowOrDom`, odd days and Mondays | with `DowOrDom`, add `StepWildcard` |

The `*` rule is in
[`entry.c`](https://github.com/vixie/cron/blob/9cc8ab1087bb9ab861dd5595c41200683c9f6712/entry.c)
and [`cron.c`](https://github.com/vixie/cron/blob/9cc8ab1087bb9ab861dd5595c41200683c9f6712/cron.c).
`1-31` does not start with `*`, so it is restricted in both.

**Fails to parse in go-cron**

- `$` in the day of month (Vixie cron 4.3: last day of the month). Write `L`
  with `DomL`.
- A step at or above the field's size, such as `*/60` in the minute field.
  What the source runs differs: Vixie cron 4.3 selects the whole range
  (every minute; `set_range` in
  [`entry.c`](https://github.com/vixie/cron/blob/9cc8ab1087bb9ab861dd5595c41200683c9f6712/entry.c)),
  cronie and Cygwin cron select only the first value (minute 0;
  [cronie `entry.c`](https://github.com/cronie-crond/cronie/blob/cronie-1.7.2/src/entry.c)).
  Write the schedule out, or set `LenientSteps`, which gives minute 0.
- cronie's `~` (a random value, picked when the crontab is loaded). The
  nearest go-cron feature is `H` with `Hash`: one value per job key, fixed
  rather than picked again on each load.
- `@reboot`: go-cron has no start-up trigger.

**Parts**

- Fields: five (minute, hour, day of month, month, day of week). A system
  crontab adds a user name before the command; it is not part of the spec.
- Day numbers: as go-cron (0 or 7 is Sunday).
- Day of month and day of week: OR when both are restricted, AND when either
  starts with `*`.
- Special characters: `*` `,` `-` `/` and three-letter names, as go-cron. Vixie
  cron 4.3 adds `$`; cronie adds `~`. No `L`, `W`, `#`, `?` or `H`.
- `@` shortcuts: `@yearly`, `@annually`, `@monthly`, `@weekly`, `@daily`,
  `@midnight` and `@hourly` as go-cron with `Descriptor`; `@reboot` has no
  go-cron spec.
- Time zones: Vixie cron and Cygwin cron run in the daemon's local time; set
  the zone with `WithLocation` or a `CRON_TZ=` prefix. cronie reads a
  `CRON_TZ=<zone>` line for the entries that follow it; in go-cron, put the same
  prefix on each spec. DST: see [DST_HANDLING.md](DST_HANDLING.md).
- Parser setup: `Minute | Hour | Dom | Month | Dow | DowOrDom | StepWildcard`,
  plus `Descriptor` for `@` shortcuts, `DomL` for `$` and `Hash` for `~` (with
  a job name from `WithName`, or a hash key; `H` fails to parse without one).

| Source spec | go-cron spec | What it runs |
|---|---|---|
| `0 9 13 * FRI` | `0 9 13 * FRI` | Fri 2026-11-20 09:00: every Friday and the 13th (setup above) |
| `0 9 13 * FRI` | `0 9 13 * *` and `0 9 * * FRI` | Sun 2026-12-13 09:00 for the first entry; the second runs every Friday; on a Friday the 13th both run |
| `0 9 13 * FRI` | `0 9 13 * FRI` without `DowOrDom` | Fri 2027-08-13 09:00: Friday the 13th only. Not the source's schedule |
| `30 4 1,15 * 5` | `30 4 1,15 * 5` | Fri 2026-11-20 04:30: the 1st, the 15th and every Friday |
| `0 9 */2 * MON` | `0 9 */2 * MON` | Mon 2026-11-23 09:00: Mondays on odd days (needs `StepWildcard`) |
| `0 9 1-31 * MON` | `0 9 1-31 * MON` | Wed 2026-11-18 09:00: every day |
| `0 0 * * 7` | `0 0 * * 7` | Sun 2026-11-22 00:00 |
| `0 0 * * mon-fri` | `0 0 * * mon-fri` | Thu 2026-11-19 00:00 |
| `0 0 $ * *` | `0 0 L * *` | Mon 2026-11-30 00:00: last day of the month (`DomL`) |
| `0 0 $ * 5` | `0 0 L * 5` | Fri 2026-11-20 00:00: last day of the month or any Friday (`DomL`) |
| `*/60 * * * *` (Vixie cron 4.3) | `* * * * *` | Wed 2026-11-18 00:01: every minute |
| `*/60 * * * *` (cronie, Cygwin cron) | `0 * * * *` | Wed 2026-11-18 01:00: every hour |
| `*/35 * * * *` | `*/35 * * * *` | Wed 2026-11-18 00:35: minutes 0 and 35 |
| `6~15 * * * *` (cronie) | `H(6-15) * * * *` | Wed 2026-11-18 00:07: one minute from 6 to 15 for the job key (`Hash`) |
| `CRON_TZ=Japan` line, then `5 0 * * *` (cronie) | `CRON_TZ=Japan 5 0 * * *` | Wed 2026-11-18 15:05: 00:05 in Japan |
| `@weekly` | `@weekly` | Sun 2026-11-22 00:00 (`Descriptor`) |
| `@reboot` | no go-cron spec | go-cron has no start-up trigger |

### BusyBox crond

Reference: BusyBox documents crond's options only, not the crontab syntax;
the lines below come from its code,
[`miscutils/crond.c`](https://github.com/mirror/busybox/blob/1_36_1/miscutils/crond.c)
at 1_36_1.

**Runs differently without an error**

| Spec | Source runs | go-cron runs, unchanged | For go-cron |
|---|---|---|---|
| `0 9 13 * FRI` (both day fields restricted) | the 13th and every Friday | Friday the 13th only | `DowOrDom`, or two entries |
| `0 0 */5 * *` (day-of-month step) | days 5, 10, …, 30 (counts from 0) | days 1, 6, 11, … | `5-30/5` |
| `5/10 * * * *` | minute 5 only | minutes 5, 15, 25, … | `5` |
| `0 0 1 * 0-6` (a day of week with every day counts as `*`) | the 1st only | the 1st only (same); with `DowOrDom`, every day | with `DowOrDom`, write `*` for the full field |
| `0 0 * * 7` | every day (BusyBox logs a parse error but loads the line with every day set) | Sundays | `0 0 * * 0`, or `*` for every day |

**Fails to parse in go-cron**

- `@reboot`: go-cron has no start-up trigger.

**Parts**

- Fields: five, no seconds or year.
- Day numbers: 0 is Sunday; 7 is not accepted (above).
- Day of month and day of week: OR when both are restricted. The day of week
  is restricted when it does not select all seven days. The day of month is
  restricted unless it also selects a day-0 slot that BusyBox keeps, which
  `*` and `*/1` do: `1-31` is restricted, so `0 9 1-31 * MON` runs every day.
- Special characters: `*` `,` `-` `/` and three-letter names. A range written
  high to low wraps (`22-2`), as go-cron. No `L`, `W`, `#`, `?` or `H`.
- `@` shortcuts: as Vixie cron (when built with them, the default).
- Time zones: none; the daemon's local time. A `CRON_TZ=` line is ignored. Use
  `WithLocation` or a `CRON_TZ=` prefix.
- Parser setup: `Minute | Hour | Dom | Month | Dow | DowOrDom` (no
  `StepWildcard`), plus `Descriptor` for `@` shortcuts.

| Source spec | go-cron spec | What it runs |
|---|---|---|
| `0 9 13 * FRI` | `0 9 13 * FRI` | Fri 2026-11-20 09:00: every Friday and the 13th |
| `0 9 13 * FRI` | `0 9 13 * *` and `0 9 * * FRI` | Sun 2026-12-13 09:00 for the first entry; the second runs every Friday; on a Friday the 13th both run |
| `0 9 */2 * MON` | `0 9 2-30/2 * MON` | Wed 2026-11-18 09:00: even days and Mondays |
| `0 0 */5 * *` | `0 0 5-30/5 * *` | Fri 2026-11-20 00:00: days 5, 10, …, 30 |
| `0 0 */1 * MON` | `0 0 * * MON` | Mon 2026-11-23 00:00: Mondays only |
| `0 0 1 * 0-6` | `0 0 1 * *` | Tue 2026-12-01 00:00: the 1st only |
| `5/10 * * * *` | `5 * * * *` | Wed 2026-11-18 00:05: minute 5 of each hour |
| `0 22-2 * * *` | `0 22-2 * * *` | Wed 2026-11-18 01:00: 22:00 to 02:00 |
| `0 0 * * 7` | `0 0 * * 0` | Sun 2026-11-22 00:00 |
| `@reboot` | no go-cron spec | go-cron has no start-up trigger |

### nnCron

Reference: [nnCron cron format](https://nncron.ru/help/EN/working/cron-format.htm).
nnCron is closed source and was not run; the lines rest on its docs.

**Runs differently without an error**

| Spec | Source runs | go-cron runs, unchanged | For go-cron |
|---|---|---|---|
| `? ? * * * *` (`?` is the time nnCron started) | daily at the start-up hour and minute | every minute (`?` is `*`) | no go-cron spec; for `? * * * * *`, `@every 1h` runs hourly from the scheduler's start (first run after an hour) |

**Fails to parse in go-cron**

- An abridged spec with trailing fields left off (`45 17 7 6`). Write the
  missing `*` fields.

**Parts**

- Fields: six (minute, hour, day of month, month, day of week, year 1900–3000);
  five when the Year field is turned off in nnCron's options. Use go-cron's
  `Year` option for six fields (`YearOptional` rejects `*` as the year).
- Day numbers: 1 is Monday, 7 Sunday, as go-cron.
- Day of month and day of week: AND, as go-cron (`0 9 1-7 * 1 *` is the first
  Monday of the month).
- Special characters: `*` `,` `-` `/` and three-letter names, as go-cron; `?`
  above. No `L`, `W`, `#` or `H`.
- `@` shortcuts: none.
- Time zones: none documented; the machine's local time. Use `WithLocation` or
  a `CRON_TZ=` prefix.
- Parser setup: `Minute | Hour | Dom | Month | Dow | Year`; without `Year` for
  five fields.

| Source spec | go-cron spec | What it runs |
|---|---|---|
| `0 9 1-7 * 1 *` | `0 9 1-7 * 1 *` | Mon 2026-12-07 09:00: first Monday of the month |
| `59 23 31 12 5 *` | `59 23 31 12 5 *` | Fri 2027-12-31 23:59: December 31 when it is a Friday |
| `0 9 13 * FRI *` | `0 9 13 * FRI *` | Fri 2027-08-13 09:00: Friday the 13th |
| `*/15 */6 1,15,31 * 1-5 *` | `*/15 */6 1,15,31 * 1-5 *` | Tue 2026-12-01 00:00: the 1st, 15th and 31st when a weekday |
| `45 17 7 6 * 2027,2028` | `45 17 7 6 * 2027,2028` | Mon 2027-06-07 17:45 |
| `45 17 7 6` | `45 17 7 6 * *` | Mon 2027-06-07 17:45: June 7 every year |
| `? ? * * * *` | no go-cron spec | start-up time; the text runs every minute in go-cron |
| `? * * * * *` | `@every 1h` | Wed 2026-11-18 01:00: hourly from the scheduler's start (`Descriptor`) |

## Hosted schedulers

### Kubernetes CronJob

Reference: [Kubernetes CronJob](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/).
The controller parses schedules with robfig/cron v3.0.1
([`cronjob_controllerv2.go`](https://github.com/kubernetes/kubernetes/blob/v1.31.0/pkg/controller/cronjob/cronjob_controllerv2.go)),
the case go-cron's `RobfigCompat` exists for. Kubernetes was not run; the
lines rest on its docs and code.

**Runs differently without an error**

| Spec | Source runs | go-cron runs, unchanged | For go-cron |
|---|---|---|---|
| `0 9 13 * FRI` (both day fields restricted) | the 13th and every Friday | Friday the 13th only | `RobfigCompat`, or two entries |
| `0 0 */2 * MON` (a stepped star is restricted) | odd days and Mondays | Mondays on odd days | `RobfigCompat`; do not add `StepWildcard` |
| `0 0 * * 5/2` (a day-of-week step stops at Saturday) | Fridays | Fridays and Sundays | `RobfigCompat`, or `5` |

The day rules are robfig/cron's
([`spec.go`](https://github.com/robfig/cron/blob/v3.0.1/spec.go), `dayMatches`);
the Kubernetes docs do not state them.

**Fails to parse in go-cron** (without `RobfigCompat`)

- A step at or above the field's size (`*/60`); Kubernetes runs it hourly.

**Parts**

- Fields: five, no seconds or year.
- Day numbers: 0–6, 0 is Sunday. Kubernetes rejects 7; go-cron accepts it.
- Day of month and day of week: OR when both are restricted, as above.
- Special characters: `*` `,` `-` `/`, `?` (as `*`) and names, as go-cron. No
  `L`, `W`, `#` or `H`.
- `@` shortcuts: `@yearly`, `@annually`, `@monthly`, `@weekly`, `@daily`,
  `@midnight`, `@hourly`, and `@every <duration>`, which the controller accepts
  though the docs do not list it. As go-cron with `Descriptor`.
- Time zones: `.spec.timeZone`; without it, the controller manager's local
  zone. Kubernetes rejects `TZ=` and `CRON_TZ=` in `.spec.schedule` and turns
  `.spec.timeZone` into a `TZ=` prefix itself
  ([`validation.go`](https://github.com/kubernetes/kubernetes/blob/v1.31.0/pkg/apis/batch/validation/validation.go)).
  In go-cron, write the zone as a `CRON_TZ=` or `TZ=` prefix.
- Parser setup: `Minute | Hour | Dom | Month | Dow | Descriptor | RobfigCompat`.

| Source spec | go-cron spec | What it runs |
|---|---|---|
| `0 9 13 * FRI` | `0 9 13 * FRI` | Fri 2026-11-20 09:00: every Friday and the 13th |
| `0 9 13 * FRI` | `0 9 13 * *` and `0 9 * * FRI` | Sun 2026-12-13 09:00 for the first entry; the second runs every Friday; on a Friday the 13th both run |
| `0 0 */2 * MON` | `0 0 */2 * MON` | Thu 2026-11-19 00:00: odd days and Mondays |
| `0 0 * * 5/2` | `0 0 * * 5/2` | Fri 2026-11-20 00:00: Fridays only |
| `*/60 * * * *` | `*/60 * * * *` | Wed 2026-11-18 01:00: every hour |
| `30 5 * * 1-5` with `timeZone: America/New_York` | `CRON_TZ=America/New_York 30 5 * * 1-5` | Wed 2026-11-18 10:30: 05:30 in New York |
| `@every 90m` | `@every 90m` | Wed 2026-11-18 01:30: every 90 minutes from the start |

### GitHub Actions and Google Cloud Scheduler

Reference: [GitHub Actions `schedule`](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#schedule),
[Google Cloud Scheduler cron format](https://cloud.google.com/scheduler/docs/configuring/cron-job-schedules).

**Runs differently without an error**

| Spec | Source runs | go-cron runs, unchanged | For go-cron |
|---|---|---|---|
| `0 9 13 * FRI` (both day fields restricted) | the 13th and every Friday | Friday the 13th only | `DowOrDom`, or two entries |
| `30 2 * * *` on a spring-forward day (GitHub) | 03:00 | 03:30 (see [DST_HANDLING.md](DST_HANDLING.md)) | no option matches; `DSTSkip` skips the run |

Cloud Scheduler documents the OR ("unless one of these fields is marked as
unrestricted by the asterisk"). GitHub's docs say only "POSIX cron syntax";
the [POSIX crontab](https://pubs.opengroup.org/onlinepubs/9699919799/utilities/crontab.html)
text runs a job when either field matches. Neither was run.

**Fails to parse in go-cron**

- A step larger than its range but not than the field, such as `50-59/15`
  (minute 50): Cloud Scheduler limits a step only to the field's largest value.
  Write `50`, or set `LenientSteps`.
- Neither documents `@` shortcuts (GitHub rejects them), `L`, `W`, `#` or `H`.

**Parts**

- Fields: five, no seconds or year.
- Day numbers: as go-cron. GitHub lists 0–6; Cloud Scheduler accepts 0 or 7 for
  Sunday.
- Day of month and day of week: OR when both are restricted.
- Special characters: `*` `,` `-` `/` and names, as go-cron (`20/15` is minutes
  20, 35 and 50).
- `@` shortcuts: none; write the five fields (`@daily` is `0 0 * * *`).
- Time zones: GitHub runs in UTC unless the entry sets `timezone:` (on
  GitHub.com, and on GitHub Enterprise Server from 3.22); Cloud Scheduler takes a time zone per job (default UTC).
  Write it as a `CRON_TZ=` prefix.
- Parser setup: `Minute | Hour | Dom | Month | Dow | DowOrDom`.

| Source spec | go-cron spec | What it runs |
|---|---|---|
| `0 9 13 * FRI` | `0 9 13 * FRI` | Fri 2026-11-20 09:00: every Friday and the 13th |
| `0 9 13 * FRI` | `0 9 13 * *` and `0 9 * * FRI` | Sun 2026-12-13 09:00 for the first entry; the second runs every Friday; on a Friday the 13th both run |
| `0 0 1 * SAT` | `0 0 1 * SAT` | Sat 2026-11-21 00:00: the 1st and every Saturday |
| `20/15 * * * *` | `20/15 * * * *` | Wed 2026-11-18 00:20: minutes 20, 35 and 50 |
| `30 5 * * 1-5` with `timezone: America/New_York` (GitHub) | `CRON_TZ=America/New_York 30 5 * * 1-5` | Wed 2026-11-18 10:30: 05:30 in New York |
| `0 8 * * MON-FRI`, time zone Europe/Berlin (Cloud Scheduler) | `CRON_TZ=Europe/Berlin 0 8 * * MON-FRI` | Wed 2026-11-18 07:00: 08:00 in Berlin |
| `0 0 * * 7` | `0 0 * * 7` | Sun 2026-11-22 00:00 |

## CI

### Jenkins

Reference: [Jenkins cron syntax](https://www.jenkins.io/doc/book/pipeline/syntax/#cron-syntax);
the time-zone line is in the trigger's
[help text](https://github.com/jenkinsci/jenkins/blob/jenkins-2.591/core/src/main/resources/hudson/triggers/TimerTrigger/help-spec.jelly).
Jenkins was not run; the lines that rest on its code cite it at jenkins-2.591.

**Runs differently without an error**

| Spec | Source runs | go-cron runs, unchanged | For go-cron |
|---|---|---|---|
| `@daily` (and the other `@` shortcuts) | a hashed time each day (`H H * * *`) | 00:00 each day | the `H` form (rows below) |
| `H H H * *` | a hashed day from 1 to 28 | a hashed day from 1 to 31 | `H H H(1-28) * *` |
| `H 8 * * *` | a minute from a hash of the job's full name | a minute from go-cron's hash (FNV-1a) of the entry name or hash key | no option keeps Jenkins' minute; write a fixed minute if it matters |

Jenkins' hash is in
[`Hash.java`](https://github.com/jenkinsci/jenkins/blob/jenkins-2.591/core/src/main/java/hudson/scheduler/Hash.java).
The times in the rewrite table are for the key `example-job`.

**Fails to parse in go-cron**

- A step at or above the field's size (`*/60`; Jenkins runs minute 0). Write
  `0 * * * *` or set `LenientSteps`.
- A Java-only zone ID in the `TZ=` line (Jenkins takes the Java runtime's IDs,
  go-cron the IANA names Go loads).

**Parts**

- Fields: five, no seconds or year. Each line of the trigger field is its own
  schedule; in go-cron, add one entry per line.
- Day numbers: as go-cron (0 or 7 is Sunday).
- Day of month and day of week: AND, as go-cron. The docs do not say; the code
  requires every field to match
  ([`CronTab.java`](https://github.com/jenkinsci/jenkins/blob/jenkins-2.591/core/src/main/java/hudson/scheduler/CronTab.java),
  `check`).
- Special characters: `*` `,` `-` `/` and `H` (`H`, `H/n`, `H(a-b)`,
  `H(a-b)/n`), as go-cron with `Hash`. No names, `?`, `L`, `W` or `#`.
- `@` shortcuts: hashed, as above.
- Time zones: a `TZ=<zone>` first line applies to every line; otherwise the
  controller's zone. In go-cron, repeat it as a `TZ=` prefix on each spec, or
  use `WithLocation` for all entries.
- Parser setup: `Minute | Hour | Dom | Month | Dow | Hash`, with a job name
  (`WithName`) or a hash key for `H`.

| Source spec | go-cron spec | What it runs |
|---|---|---|
| `H H * * *` | `H H * * *` | Wed 2026-11-18 23:11: once a day at a hashed time |
| `H/15 * * * *` | `H/15 * * * *` | Wed 2026-11-18 00:11: every 15 minutes from a hashed minute |
| `H H(0-7) * * *` | `H H(0-7) * * *` | Wed 2026-11-18 07:11: once a day before 08:00 |
| `45 9-16/2 * * 1-5` | `45 9-16/2 * * 1-5` | Wed 2026-11-18 09:45: 09:45, 11:45, 13:45, 15:45 on weekdays |
| `0 9 13 * FRI` | `0 9 13 * FRI` | Fri 2027-08-13 09:00: Friday the 13th, as Jenkins |
| `@hourly` | `H * * * *` | Wed 2026-11-18 00:11: a hashed minute each hour |
| `@daily` | `H H * * *` | Wed 2026-11-18 23:11: a hashed time each day |
| `@midnight` | `H H(0-2) * * *` | Wed 2026-11-18 02:11: a hashed time from 00:00 to 02:59 |
| `@weekly` | `H H * * H` | Tue 2026-11-24 23:11: a hashed time on a hashed day |
| `@monthly` | `H H H(1-28) * *` | Tue 2026-11-24 23:11: a hashed day from 1 to 28 |
| `@yearly` | `H H H(1-28) H *` | Thu 2026-12-24 23:11: a hashed day and month |
| `H H H * *` | `H H H(1-28) * *` | Tue 2026-11-24 23:11: keeps Jenkins' 1–28 range |
| `*/60 * * * *` | `0 * * * *` | Wed 2026-11-18 01:00: every hour |
| `TZ=Asia/Tokyo` line, then `H 8 * * *` | `TZ=Asia/Tokyo H 8 * * *` | Wed 2026-11-18 23:11: 08:11 in Tokyo |

## Java and .NET Quartz

### Quartz and Quartz.NET

Reference: [Quartz 2.3 CronTrigger](https://www.quartz-scheduler.org/documentation/quartz-2.3.0/tutorials/crontrigger.html),
[Quartz.NET 3.x CronTrigger](https://www.quartz-scheduler.net/documentation/quartz-3.x/tutorial/crontrigger.html).
Both were run (Quartz 2.3.0,
[Quartz.NET 3.22.10](https://github.com/quartznet/quartznet/blob/v3.22.10/src/Quartz/CronExpression.cs)).
Lines that apply to one of them name it. cron-utils with `CronType.QUARTZ`
converts the same way, except that it rejects the specs Quartz runs with
text dropped.

**Runs differently without an error**

| Spec | Source runs | go-cron runs, unchanged | For go-cron |
|---|---|---|---|
| `0 0 9 ? * 1` (1 = Sunday in Quartz) | Sundays | Mondays | `0 0 9 ? * 0`: subtract 1 from every day number, in ranges, lists, `#n` and `L` forms too; or use day names, which mean the same in both |
| `0 0 9 ? * 6#3` | third Friday | third Saturday | `0 0 9 ? * 5#3` |
| `0 0 9 15 * ? 2026/2` (year step) | 2026, 2028, … | 2026 only | write an end year: `2026-2198/2` |
| `0 0 9 31W * ?` (Quartz.NET) | in a month without a 31st, the weekday nearest its last day | only months with a 31st (as Quartz in Java) | `0 0 9 LW * *` |
| `0 0 9 ? * FRI/2` (Quartz.NET) | every second week, counted from where its search starts | Fridays and Sundays | no go-cron spec |

**Fails to parse in go-cron** (Quartz runs these)

- `dL` / `DDDL` in the day of week (last Friday is `6L` or `FRIL`). Write
  `5#L` or `FRI#L`. A bare `L` in the day of week (Saturday) becomes `SAT`.
- Specs from which Quartz drops text without an error: `L` after a range
  (`MON-FRIL`, `2-6L`) is ignored, and `5L-3W` runs as `5L`. Write the
  schedule Quartz ran: `MON-FRI`, `1-5`, `4#L`.
- `L-3W` in the day of month (nearest weekday to three days before the last).
  There is no go-cron spec.
- `/15` without a start (Quartz reads `*/15`). Write `*/15` or `0/15`.
- A `*` year. Leave it out with `YearOptional`, or use `Year`, which then
  requires a year in every spec.
- Full month or day names (Quartz.NET reads the first three letters, so
  `JANUARY` works). Write `JAN`.

**Parts**

- Fields: six or seven: seconds, minutes, hours, day of month, month, day of
  week, and an optional year (the docs say 1970–2099; Quartz 2.3.0 accepts up
  to 100 years past the current one).
- Day numbers: 1 is Sunday, 7 Saturday; 0 is an error. See above.
- Day of month and day of week: one of them must be `?`; Quartz rejects specs
  that set both. go-cron accepts `?` as `*`, so the `?` can stay.
- Special characters: `*` `,` `-` `/` `?` `L` `L-n` `W` `LW` `#`, as go-cron with
  `Extended` (`L-n` is n days before the last day, so `L-3` is the fourth-last
  day; the Quartz 2.3 docs call it "third-to-last"). Quartz.NET adds `H`: in go-cron, `H` with `Hash`
  keeps the shape but not the value, which Quartz.NET derives from the
  trigger's identity.
- `@` shortcuts: none.
- Time zones: a trigger's zone, by default the JVM's or machine's local zone
  (`inTimeZone` / `InTimeZone`). Write it as a `CRON_TZ=` prefix with an IANA
  name; Quartz.NET's Windows zone IDs need translating.
- Parser setup: `Second | Minute | Hour | Dom | Month | Dow | YearOptional | Extended`.
  `YearOptional` reads a seventh field as a year only when its value is 100 or
  more; without it, a seven-field spec is an error.

| Source spec | go-cron spec | What it runs |
|---|---|---|
| `0 0 9 ? * 1` | `0 0 9 ? * 0` | Sun 2026-11-22 09:00: Sunday |
| `0 0 9 ? * 1` | `0 0 9 ? * 1` unchanged | Mon 2026-11-23 09:00: Monday, one day late |
| `0 0 9 ? * 7` | `0 0 9 ? * 6` | Sat 2026-11-21 09:00: Saturday |
| `0 0 9 ? * 7` | `0 0 9 ? * 7` unchanged | Sun 2026-11-22 09:00: Sunday, one day late |
| `0 0 9 ? * 2-6` | `0 0 9 ? * 1-5` | Wed 2026-11-18 09:00: Monday to Friday |
| `0 0 9 ? * 6#3` | `0 0 9 ? * 5#3` | Fri 2026-11-20 09:00: third Friday |
| `0 0 9 ? * 6#3` | `0 0 9 ? * 6#3` unchanged | Sat 2026-11-21 09:00: third Saturday |
| `0 0 9 ? * 6L` | `0 0 9 ? * 5#L` | Fri 2026-11-27 09:00: last Friday |
| `0 0 9 ? * 6L` | `0 0 9 ? * 6L` unchanged | parse error |
| `0 0 9 ? * FRIL` | `0 0 9 ? * FRI#L` | Fri 2026-11-27 09:00: last Friday |
| `0 0 9 ? * L` | `0 0 9 * * SAT` | Sat 2026-11-21 09:00: Saturday |
| `0 0 9 ? * MON-FRIL` | `0 0 9 ? * MON-FRI` | Wed 2026-11-18 09:00: Monday to Friday, as Quartz ran it |
| `0 0 9 ? * 2-6L` | `0 0 9 ? * 1-5` | Wed 2026-11-18 09:00: Monday to Friday, as Quartz ran it |
| `0 0 9 ? * 5L-3W` | `0 0 9 ? * 4#L` | Thu 2026-11-26 09:00: last Thursday, as Quartz ran it |
| `0 0 9 L-3W * ?` | no go-cron spec | go-cron cannot combine an `L` offset with `W` |
| `0 0 9 L-3 * ?` | `0 0 9 L-3 * ?` | Fri 2026-11-27 09:00: three days before the last day |
| `0 0 9 31W * ?` (Quartz.NET) | `0 0 9 LW * *` | Mon 2026-11-30 09:00: the last weekday, as Quartz.NET ran it |
| `0 0 9 31W * ?` (Quartz) | `0 0 9 31W * *` | Thu 2026-12-31 09:00: months with a 31st only |
| `0 0 9 ? * FRI/2` (Quartz.NET) | no go-cron spec | every second week from an unfixed start |
| `/15 * * * * ?` | `*/15 * * * * ?` | Wed 2026-11-18 00:00:15: every 15 seconds |
| `0 0 9 * * ? 2027` | `0 0 9 * * ? 2027` | Fri 2027-01-01 09:00 (`YearOptional`) |
| `0 0 9 * * ? *` | `0 0 9 * * ?` | Wed 2026-11-18 09:00: the `*` year left out |
| `0 0 9 15 * ? 2026/2` | `0 0 9 15 * ? 2026-2198/2` | Tue 2026-12-15 09:00, then 2028-01-15: every second year. `2026/2` would stop after 2026 |
| `0 0 9 ? JANUARY *` (Quartz.NET) | `0 0 9 * JAN *` | Fri 2027-01-01 09:00 |
| `0 0 9 ? * 2-6`, zone America/New_York | `CRON_TZ=America/New_York 0 0 9 ? * 1-5` | Wed 2026-11-18 14:00: 09:00 in New York |

### Spring

Reference: [Spring cron expressions](https://docs.spring.io/spring-framework/reference/integration/scheduling.html#scheduling-cron-expression)
(`CronExpression`, `@Scheduled(cron = …)`, `CronTrigger`; 5.3 and later). Spring
6.2.19 and 5.3.39 were run. cron-utils with `CronType.SPRING53` converts the
same way.

**Runs differently without an error**

- Nothing found: Spring numbers days as go-cron and needs both day fields to
  match, as go-cron's default.

**Fails to parse in go-cron**

- `dL` / `DDDL` in the day of week (`5L`, `FRIL`: last Friday). Write `5#L` or
  `FRI#L`; the day numbers stay.
- Upper-case shortcuts (`@DAILY`). Write `@daily`.

**Parts**

- Fields: exactly six: seconds, minutes, hours, day of month, month, day of
  week. No year.
- Day numbers: as go-cron (0 or 7 is Sunday).
- Day of month and day of week: AND, as go-cron. `0 0 9 13 * FRI` is Friday the
  13th in both.
- Special characters: `*` `,` `-` `/` `?` `L` `L-n` `W` `LW` `#` and names, as
  go-cron with `Extended`, plus `dL` above. Spring rejects `L` after a range
  (`MON-FRIL`), as go-cron does, and `#L`, which go-cron accepts.
- `@` shortcuts: `@yearly`, `@annually`, `@monthly`, `@weekly`, `@daily`,
  `@midnight`, `@hourly` in any case, as go-cron with `Descriptor` in lower
  case.
- Time zones: the trigger's zone (`@Scheduled(zone = …)`, `CronTrigger`), by
  default the scheduler's. Write it as a `CRON_TZ=` prefix.
- Parser setup: `Second | Minute | Hour | Dom | Month | Dow | Descriptor | Extended`.

| Source spec | go-cron spec | What it runs |
|---|---|---|
| `0 0 9 * * 5L` | `0 0 9 ? * 5#L` | Fri 2026-11-27 09:00: last Friday |
| `0 0 9 * * FRIL` | `0 0 9 ? * FRI#L` | Fri 2026-11-27 09:00: last Friday |
| `0 0 9 * * 7` | `0 0 9 * * 7` | Sun 2026-11-22 09:00 |
| `0 0 9 13 * FRI` | `0 0 9 13 * FRI` | Fri 2027-08-13 09:00: Friday the 13th |
| `0 0 9 L-3 * *` | `0 0 9 L-3 * *` | Fri 2026-11-27 09:00: three days before the last day |
| `@weekly` | `@weekly` | Sun 2026-11-22 00:00 |
| `@Scheduled(cron = "0 0 9 * * MON-FRI", zone = "America/New_York")` | `CRON_TZ=America/New_York 0 0 9 * * MON-FRI` | Wed 2026-11-18 14:00: 09:00 in New York |

## .NET

### Azure Functions and NCrontab

Reference: [Azure Functions timer trigger](https://learn.microsoft.com/en-us/azure/azure-functions/functions-bindings-timer#ncrontab-expressions),
[NCrontab](https://github.com/atifaziz/NCrontab/wiki/Crontab-Expression). The Azure
timer trigger parses with NCrontab
([`CronSchedule.cs`](https://github.com/Azure/azure-webjobs-sdk-extensions/blob/v3.0.3/src/WebJobs.Extensions/Extensions/Timers/Scheduling/CronSchedule.cs));
NCrontab 3.3 and 3.4.0 were run.

**Runs differently without an error**

| Spec | Source runs | go-cron runs, unchanged | For go-cron |
|---|---|---|---|
| `0 0 9 * * 5-1` (reversed range) | Monday to Friday (swapped) | Friday to Monday (wrapped) | `0 0 9 * * 1-5` |
| `0 0 9 * * 5/2` (a day-of-week step stops at Saturday) | Fridays | Fridays and Sundays | `0 0 9 * * 5`, or `DowStepToSaturday` |

The swap is in
[`CrontabField.cs`](https://github.com/atifaziz/NCrontab/blob/v3.4.0/NCrontab/CrontabField.cs).

**Fails to parse in go-cron**

- Full day and month names (`Monday`, `January`). Write `MON`, `JAN`.
- A step as large as the field (`*/90` in the minute field; NCrontab runs
  minute 0). Write `0`.
- Azure's TimeSpan schedule (`"00:05:00"`) is not a cron spec. `@every 5m` is
  the nearest go-cron feature. After a run longer than the interval, Azure
  invokes the function again at once; go-cron starts a run every five minutes
  whether or not the last one has finished, unless the job is wrapped with
  `SkipIfStillRunning` or `DelayIfStillRunning`.

**Parts**

- Fields: Azure takes five or six (seconds first); NCrontab takes five, or six
  with its `IncludingSeconds` option. No year.
- Day numbers: 0–6, 0 is Sunday, as go-cron. 7 is an error in NCrontab.
- Day of month and day of week: AND, as go-cron (`0 0 9 13 * 5` is Friday the
  13th; the docs do not say, the runs do).
- Special characters: `*` `,` `-` `/` and names, as go-cron except the two
  above. No `?`, `L`, `W`, `#` or `H`.
- `@` shortcuts: none.
- Time zones: Azure runs in UTC unless the app setting `WEBSITE_TIME_ZONE` sets
  one zone for every timer; that maps to `WithLocation`, or a `CRON_TZ=` prefix
  per spec, with an IANA name for a Windows zone name. NCrontab has no zone; the
  caller's time decides.
- Parser setup: `SecondOptional | Minute | Hour | Dom | Month | Dow` for Azure;
  `Minute | Hour | Dom | Month | Dow` (or `Second | …` with `IncludingSeconds`)
  for NCrontab.

| Source spec | go-cron spec | What it runs |
|---|---|---|
| `0 30 9 * * 1-5` | `0 30 9 * * 1-5` | Wed 2026-11-18 09:30 |
| `0 0 9 * * 5-1` | `0 0 9 * * 1-5` | Wed 2026-11-18 09:00: Monday to Friday |
| `0 0 9 * * 5/2` | `0 0 9 * * 5` | Fri 2026-11-20 09:00: Fridays |
| `0 0 9 * * Monday` | `0 0 9 * * MON` | Mon 2026-11-23 09:00 |
| `0 */90 * * * *` | `0 0 * * * *` | Wed 2026-11-18 01:00: every hour |
| `0 0 9 13 * 5` | `0 0 9 13 * 5` | Fri 2027-08-13 09:00: Friday the 13th |
| `0 0 10 * * *`, `WEBSITE_TIME_ZONE` Eastern Standard Time | `CRON_TZ=America/New_York 0 0 10 * * *` | Wed 2026-11-18 15:00: 10:00 in New York |
| `"00:05:00"` (TimeSpan) | `@every 5m` | Wed 2026-11-18 00:05: every five minutes from the start (`Descriptor`) |

### Hangfire (Cronos)

Reference: [Cronos syntax](https://github.com/HangfireIO/Cronos/blob/v0.11.1/README.md)
(Hangfire 1.8.25 parses with Cronos 0.11.1,
[`RecurringJobEntity.cs`](https://github.com/HangfireIO/Hangfire/blob/v1.8.25/src/Hangfire.Core/RecurringJobEntity.cs)),
[Hangfire recurrent tasks](https://docs.hangfire.io/en/latest/background-methods/performing-recurrent-tasks.html).
Cronos 0.11.1 was run.

**Runs differently without an error**

| Spec | Source runs | go-cron runs, unchanged | For go-cron |
|---|---|---|---|
| `0 9 * * *`, no time zone set | 09:00 UTC | 09:00 local time | `CRON_TZ=UTC 0 9 * * *` |
| `30 02 * * *` on a spring-forward day | 03:00 | 03:30 (see [DST_HANDLING.md](DST_HANDLING.md)) | no option matches; `DSTSkip` skips the run |

Otherwise Cronos numbers days as go-cron, needs both day fields to match, and
wraps reversed ranges as go-cron does.

**Fails to parse in go-cron**

- `dL` / `DDDL` in the day of week (`5L`, `FRIL`: last Friday). Write `5#L` or
  `FRI#L`; the day numbers stay.
- `L-3W` in the day of month. There is no go-cron spec.
- `@every_minute` and `@every_second`. Write `* * * * *` and `* * * * * *`
  (`@every 1m` counts from the start, not on the minute).
- Upper-case shortcuts (`@DAILY`). Write `@daily`.

**Parts**

- Fields: five, or six with seconds first. No year. (The Hangfire docs example
  `0 12 * */2` has four fields, which Hangfire rejects.)
- Day numbers: as go-cron (0 or 7 is Sunday).
- Day of month and day of week: AND, as go-cron.
- Special characters: `*` `,` `-` `/` `?` `L` `L-n` `W` `LW` `#` and names, as
  go-cron with `Extended`, plus `dL` above.
- `@` shortcuts: `@yearly`, `@annually`, `@monthly`, `@weekly`, `@daily`,
  `@midnight`, `@hourly` in any case, as go-cron with `Descriptor` in lower
  case.
- Time zones: per job (`RecurringJobOptions.TimeZone`, default UTC). Write it
  as a `CRON_TZ=` prefix with an IANA name.
- Parser setup: `SecondOptional | Minute | Hour | Dom | Month | Dow | Descriptor | Extended`.

| Source spec | go-cron spec | What it runs |
|---|---|---|
| `0 9 * * 5L` | `0 9 * * 5#L` | Fri 2026-11-27 09:00: last Friday |
| `0 9 * * FRIL` | `0 9 * * FRI#L` | Fri 2026-11-27 09:00: last Friday |
| `0 9 L-3W * *` | no go-cron spec | go-cron cannot combine an `L` offset with `W` |
| `@every_minute` | `* * * * *` | Wed 2026-11-18 00:01: every minute |
| `0 9 13 * 5` | `0 9 13 * 5` | Fri 2027-08-13 09:00: Friday the 13th |
| `0 9 * * 1-5`, time zone Eastern | `CRON_TZ=America/New_York 0 9 * * 1-5` | Wed 2026-11-18 14:00: 09:00 in New York |

## Cloud

### AWS EventBridge rules and Scheduler

Reference: [EventBridge cron expressions](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-scheduled-rule-pattern.html),
[EventBridge Scheduler schedule types](https://docs.aws.amazon.com/scheduler/latest/UserGuide/schedule-types.html).
AWS cannot be run here; every line is from those pages. Rules and Scheduler
share the cron syntax.

**Runs differently without an error**

| Spec | Source runs | go-cron runs, unchanged | For go-cron |
|---|---|---|---|
| `cron(0 9 ? * 1 *)` (1 = Sunday in AWS) | Sundays | Mondays | `0 9 ? * 0 *`: subtract 1 from every day number; or use day names, which mean the same in both |
| `cron(0 9 ? * 6#3 *)` | third Friday | third Saturday | `0 9 ? * 5#3 *` |
| `cron(0 9 15 * ? 2026/2)` (year step) | not stated in the AWS docs | 2026 only | `2026-2198/2` for every second year |
| `cron(0 9 * * ? *)` (a rule, or a schedule without a zone) | 09:00 UTC | 09:00 local time | `CRON_TZ=UTC 0 9 * * ? *` |
| a Scheduler time in the spring-forward gap | skipped that day | runs in the hour after the gap | `DSTSkip` |

AWS states 1 = Sunday only by example: `6L` is the last Friday and `3#2` the
second Tuesday.

**Fails to parse in go-cron**

- `dL` in the day of week (`6L`: last Friday). Write `5#L` or `FRI#L`.
- A bare `L` in the day of week ("the last day of the week", Saturday). Write
  `SAT`.
- `L` after a range (`MON-FRIL`) and `L-3W` are not in AWS's field table, and
  its docs do not say what AWS does with them. go-cron rejects them; write the
  schedule you meant.
- `rate(5 minutes)` is not a cron spec; `@every 5m` is the go-cron feature for a
  fixed interval.

**Parts**

- Fields: six: minutes, hours, day of month, month, day of week, and a required
  year (1970–2199). No seconds.
- Day numbers: 1 is Sunday; see above.
- Day of month and day of week: one of them must be `?`. go-cron accepts `?` as
  `*`, so the `?` can stay.
- Special characters: `*` `,` `-` `/` `?` `L` `W` `#` and names. `L`, `W` and `#`
  as go-cron with `Extended`, with the day numbers renumbered. Only one `#`
  item is allowed in AWS; go-cron takes a list.
- `@` shortcuts: none.
- Time zones: above.
- Parser setup: `Minute | Hour | Dom | Month | Dow | Year | Extended`.

| Source spec | go-cron spec | What it runs |
|---|---|---|
| `cron(0 18 ? * MON-FRI *)` | `0 18 ? * MON-FRI *` | Wed 2026-11-18 18:00: Monday to Friday |
| `cron(0 9 ? * 2-6 *)` | `0 9 ? * 1-5 *` | Wed 2026-11-18 09:00: Monday to Friday |
| `cron(0 9 ? * 1 *)` | `0 9 ? * 0 *` | Sun 2026-11-22 09:00: Sunday |
| `cron(0 9 ? * 1 *)` | `0 9 ? * 1 *` unchanged | Mon 2026-11-23 09:00: Monday, one day late |
| `cron(0 9 ? * 6#3 *)` | `0 9 ? * 5#3 *` | Fri 2026-11-20 09:00: third Friday |
| `cron(0 9 ? * 3#2 *)` | `0 9 ? * 2#2 *` | Tue 2026-12-08 09:00: second Tuesday |
| `cron(15 10 ? * 6L 2026-2027)` | `15 10 ? * 5#L 2026-2027` | Fri 2026-11-27 10:15: last Friday |
| `cron(0 9 ? * L *)` | `0 9 ? * SAT *` | Sat 2026-11-21 09:00: every Saturday |
| `cron(0 9 15 * ? 2026/2)` | `0 9 15 * ? 2026-2198/2` | Tue 2026-12-15 09:00, then 2028-01-15: every second year |
| `cron(30 8 * * ? *)`, zone America/New_York | `CRON_TZ=America/New_York 30 8 * * ? *` | Wed 2026-11-18 13:30: 08:30 in New York |

## Python

None of the three was run; each line rests on its docs or, where marked, its
code.

### APScheduler

Reference: [APScheduler 3.x cron trigger](https://apscheduler.readthedocs.io/en/3.x/modules/triggers/cron.html)
(3.11.3; 4.x is a pre-release and not covered).

**Runs differently without an error**

| Spec | Source runs | go-cron runs, unchanged | For go-cron |
|---|---|---|---|
| `from_crontab('0 9 * * 0-4')` (0 = Monday in APScheduler; its docs warn) | Monday to Friday | Sunday to Thursday | `0 9 * * 1-5`: add 1 to every day number (Sunday becomes 0 or 7); or use day names |
| `CronTrigger(hour=3)` (fields below the smallest given default to their minimum) | 03:00 once a day | `* 3 * * *`: every minute of hour 3 | `0 3 * * *` |
| `CronTrigger(day='1,3rd fri')` (a union inside the day field) | the 1st and the third Friday | `0 0 1 * FRI#3`: never (both fields must match) | `DowOrDom`, or two entries |
| `CronTrigger(year='2026/2', day=15, hour=9)` (year step) | 2026, 2028, … | `0 9 15 * * 2026/2`: 2026 only | write an end year: `2026-9998/2` |

**Fails to parse in go-cron**

- `day='3rd fri'`, `day='last fri'`, `day='last'`: write `FRI#3`, `FRI#L` in the
  day of week, and `L` in the day of month (`Extended`).
- `week=…` (ISO week): go-cron has no week field. There is no go-cron spec.

**Parts**

- Fields: `from_crontab` takes five (seconds are 0); `CronTrigger` takes
  keywords `year`, `month`, `day`, `week`, `day_of_week`, `hour`, `minute`,
  `second`. In go-cron, order them as seconds (with `Second`), minute, hour,
  day, month, day of week, year (with `Year`).
- Day numbers: 0 is Monday; see above.
- Day of month and day of week: AND, as go-cron.
- Special characters: `*` `*/n` `a-b` `a-b/n` `a/n` `,` and three-letter names, as
  go-cron, except `a/n` in the year; `xth y`, `last x` and `last` in the day
  field, as above.
- `@` shortcuts: none.
- Time zones: `timezone=` on the trigger, or the scheduler's `timezone`. The
  docs say a trigger defaults to the scheduler's zone; the code does that only
  for `add_job(…, 'cron', …)`. A `CronTrigger` built directly without
  `timezone=` uses the machine's local zone
  ([`triggers/cron/__init__.py`](https://github.com/agronholm/apscheduler/blob/3.11.3/src/apscheduler/triggers/cron/__init__.py)).
  Write the zone as a `CRON_TZ=` prefix.
- Parser setup: `Minute | Hour | Dom | Month | Dow`, plus `Second`, `Year` and
  `Extended` as the trigger uses them.

| Source spec | go-cron spec | What it runs |
|---|---|---|
| `from_crontab('0 9 * * 0-4')` | `0 9 * * 1-5` | Wed 2026-11-18 09:00: Monday to Friday |
| `from_crontab('0 9 * * 0-4')` | `0 9 * * 0-4` unchanged | Wed 2026-11-18 09:00: Sunday to Thursday |
| `from_crontab('0 9 * * 5-6')` | `0 9 * * 6-7` | Sat 2026-11-21 09:00: Saturday and Sunday |
| `CronTrigger(hour=3)` | `0 3 * * *` | Wed 2026-11-18 03:00 |
| `CronTrigger(day_of_week='mon-fri')` | `0 0 * * MON-FRI` | Thu 2026-11-19 00:00 |
| `CronTrigger(second='*/10')` | `*/10 * * * * *` | Wed 2026-11-18 00:00:10: every 10 seconds (`Second`) |
| `CronTrigger(day='last sun')` | `0 0 * * SUN#L` | Sun 2026-11-29 00:00: last Sunday |
| `CronTrigger(day='1,3rd fri')` | `0 0 1 * FRI#3` | Fri 2026-11-20 00:00: the 1st and the third Friday (`DowOrDom`) |
| `CronTrigger(year='2026/2', day=15, hour=9)` | `0 9 15 * * 2026-9998/2` | Tue 2026-12-15 09:00, then 2028-01-15: every second year (`Year`). `2026/2` would stop after 2026 |
| `CronTrigger(week='*/2')` | no go-cron spec | go-cron has no ISO-week field |
| `from_crontab('0 9 * * *', timezone='America/New_York')` | `CRON_TZ=America/New_York 0 9 * * *` | Wed 2026-11-18 14:00: 09:00 in New York |

### Celery

Reference: [Celery crontab schedules](https://docs.celeryq.dev/en/stable/userguide/periodic-tasks.html#crontab-schedules),
[`celery.schedules.crontab`](https://docs.celeryq.dev/en/stable/reference/celery.schedules.html) (5.7.0).

**Runs differently without an error**

| Spec | Source runs | go-cron runs, unchanged | For go-cron |
|---|---|---|---|
| `crontab(0, 0, 5)` (positional order: minute, hour, day_of_week, day_of_month, month_of_year) | Fridays at 00:00 | `0 0 5 * *` in crontab order: the 5th of each month | `0 0 * * 5` |
| `crontab(minute=30, hour=7)`, no time zone set | 07:30 UTC | 07:30 local time | `CRON_TZ=UTC 30 7 * * *` |

Unset fields are `*` in Celery as in go-cron: `crontab(hour=7)` runs every
minute of hour 7, as `* 7 * * *` does (the docs' `day_of_week='sunday'`
example says the same: "every minute (!)"). For 07:00, set `minute=0`.

**Fails to parse in go-cron**

- Full day and month names (`sunday`; Celery reads the first three letters).
  Write `SUN`.
- A step at or above the field's size (`minute='*/60'` is minute 0 in Celery,
  [`schedules.py`](https://github.com/celery/celery/blob/v5.7.0/celery/schedules.py)).
  Write `0`.

**Parts**

- Fields: keywords `minute`, `hour`, `day_of_week`, `day_of_month`,
  `month_of_year`; `crontab.from_string` takes the five crontab fields in
  crontab order. No seconds or year.
- Day numbers: 0–6, 0 is Sunday, as go-cron.
- Day of month and day of week: AND, as go-cron (the `crontab` reference's
  "first and third Monday" is `day_of_week=1, day_of_month='1-7,15-21'`).
- Special characters: `*` `/` `,` `-` and names, as go-cron. Ranges wrap
  (`fri-mon`), as go-cron.
- `@` shortcuts: none.
- Time zones: one for the whole app (`timezone`, default UTC). Write it as a
  `CRON_TZ=` prefix on each spec or use `WithLocation`.
- Parser setup: `Minute | Hour | Dom | Month | Dow`.

| Source spec | go-cron spec | What it runs |
|---|---|---|
| `crontab(hour=7)` | `* 7 * * *` | Wed 2026-11-18 07:00: every minute of hour 7 |
| `crontab(hour=7, minute=30, day_of_week=1)` | `30 7 * * 1` | Mon 2026-11-23 07:30 |
| `crontab(0, 0, 5)` | `0 0 * * 5` | Fri 2026-11-20 00:00: Fridays |
| `crontab(0, 0, day_of_week=1, day_of_month='1-7,15-21')` | `0 0 1-7,15-21 * 1` | Mon 2026-12-07 00:00: first and third Monday |
| `crontab(day_of_week='sunday')` | `* * * * SUN` | Sun 2026-11-22 00:00: every minute on Sundays |
| `crontab(minute='*/60')` | `0 * * * *` | Wed 2026-11-18 01:00: every hour |
| `crontab(minute=30, hour=7)`, no zone set | `CRON_TZ=UTC 30 7 * * *` | Wed 2026-11-18 07:30 |
| `crontab(minute=30, hour=7)`, `timezone='America/New_York'` | `CRON_TZ=America/New_York 30 7 * * *` | Wed 2026-11-18 12:30: 07:30 in New York |

### croniter

Reference: [croniter README](https://github.com/pallets-eco/croniter/blob/7.0.0/README.rst) (7.0.0).

**Runs differently without an error**

| Spec | Source runs | go-cron runs, unchanged | For go-cron |
|---|---|---|---|
| `* * * * * 5` (seconds go last) | second 5 of every minute | every second on Fridays (with `Second`, the last field is the day of week) | `5 * * * * *` |
| `0 9 13 * 5` (both day fields restricted, `day_or=True`, the default) | the 13th and every Friday | Friday the 13th only | `DowOrDom`, or two entries |
| `5-5 * * * *` (an equal range is the whole cycle) | every minute | minute 5 of each hour | `* * * * *` |
| `0 9 * * *`, naive or no start time | 09:00 UTC | 09:00 local time | `CRON_TZ=UTC 0 9 * * *` |
| `0 9 15 * * 0 2026/2` (year step) | 2026, 2028, … | with the seconds moved first (`0 0 9 15 * * 2026/2`), 2026 only | write an end year: `0 0 9 15 * * 2026-2098/2` |
| `0 9 31W * *` | in a month without a 31st, the weekday nearest its last day | only months with a 31st | `0 9 LW * *` |
| `@daily` with `hash_id` (every `@` shortcut) | a hashed time each day: croniter reads it as `H H * * * H` | 00:00 every day | `H H H * * *` (`Second` and `Hash`); go-cron picks its own minute, hour and second, so write fixed values if the time must stay the same |

With `day_or=False`, croniter needs both day fields, as go-cron. With
`implement_cron_bug=True`, a field starting with `*` counts as unrestricted,
which is go-cron's `StepWildcard`.

**Fails to parse in go-cron**

- `L5` (last Friday, `L` first). Write `5#L` or `FRI#L`.
- `W15` (`W` first). Write `15W`.
- Upper-case shortcuts (`@DAILY`). Write `@daily`.
- A step at or above the field's size (`*/60` is minute 0 in croniter). Write
  `0`.
- `R` (random per instance): there is no go-cron spec. `H` exists in both
  (`Hash`), but each picks its own values for a name.

**Parts**

- Fields: five; six with seconds last; seven with seconds and then the year.
- Day numbers: as go-cron (0 or 7 is Sunday).
- Day of month and day of week: OR by default; see above.
- Special characters: `*` `/` `,` `-` `?` `l` (last day of the month) `nW` `n#k`
  `Ln`, names, `H` and `R`. Ranges wrap.
- `@` shortcuts: `@midnight`, `@hourly`, `@daily`, `@weekly`, `@monthly`,
  `@yearly`, `@annually` in any case, as go-cron with `Descriptor` in lower case;
  with `hash_id`, croniter hashes them (see above).
- Time zones: above.
- Parser setup: `Minute | Hour | Dom | Month | Dow | DowOrDom` for the default
  `day_or=True` (no `StepWildcard`); add `Second`, `Year`, `Extended`,
  `Descriptor` or `Hash` as the specs use them.

| Source spec | go-cron spec | What it runs |
|---|---|---|
| `0 9 13 * 5` | `0 9 13 * 5` | Fri 2026-11-20 09:00: the 13th and every Friday (`DowOrDom`) |
| `0 9 13 * 5` | `0 9 13 * *` and `0 9 * * 5` | Sun 2026-12-13 09:00 for the first entry; the second runs every Friday; on a Friday the 13th both run |
| `* * * * * 15,25` | `15,25 * * * * *` | Wed 2026-11-18 00:00:15: seconds 15 and 25 of each minute (`Second`) |
| `0 0 1 1 * 0 2027-2029` | `0 0 0 1 1 * 2027-2029` | Fri 2027-01-01 00:00 (`Second` and `Year`) |
| `0 9 15 * * 0 2026/2` | `0 0 9 15 * * 2026-2098/2` | Tue 2026-12-15 09:00, then 2028-01-15: every second year. `2026/2` would stop after 2026 |
| `0 9 31W * *` | `0 9 LW * *` | Mon 2026-11-30 09:00: the last weekday, as croniter ran it |
| `@daily` with `hash_id` | `H H H * * *` | Wed 2026-11-18 23:11:11: a hashed time each day (`Second` and `Hash`) |
| `0 0 * * L5` | `0 0 * * 5#L` | Fri 2026-11-27 00:00: last Friday |
| `0 0 * * 5#3,L5` | `0 0 * * 5#3,5#L` | Fri 2026-11-20 00:00: third and last Friday |
| `0 0 l * *` | `0 0 L * *` | Mon 2026-11-30 00:00: last day of the month |
| `5-5 * * * *` | `* * * * *` | Wed 2026-11-18 00:01: every minute |
| `0 9 * * sun-sun` | `0 9 * * *` | Wed 2026-11-18 09:00: every day |
| `0 9 * * *`, no time zone | `CRON_TZ=UTC 0 9 * * *` | Wed 2026-11-18 09:00 |
| `0 9 * * *`, start time in America/New_York | `CRON_TZ=America/New_York 0 9 * * *` | Wed 2026-11-18 14:00: 09:00 in New York |

## Node.js

Neither package was run; each line rests on its docs or, where marked, its
code and tests.

### node-cron

Reference: [node-cron cron syntax](https://nodecron.com/cron-syntax.html),
[time zones and DST](https://nodecron.com/timezones-and-dst.html) (4.6.0).

**Runs differently without an error**

| Spec | Source runs | go-cron runs, unchanged | For go-cron |
|---|---|---|---|
| `30 2 * * *` on a spring-forward day | no run that day | 03:30 | `DSTSkip` |

**Fails to parse in go-cron**

- `dL` in the day of week (`5L`: last Friday). Write `5#L`.
- Full day and month names (`Monday`, `January`). Write `MON`, `JAN`.
- Upper-case shortcuts (`@DAILY`). Write `@daily`.

**Parts**

- Fields: five, or six with seconds first. No year.
- Day numbers: as go-cron (0 or 7 is Sunday).
- Day of month and day of week: AND, as go-cron. The docs do not say; the code
  requires both
  ([`time-matcher.ts`](https://github.com/node-cron/node-cron/blob/v4.6.0/src/time/time-matcher.ts)).
  node-cron's migration page says specs carry over unchanged from the `cron`
  package below; they do not, because `cron` uses OR.
- Special characters: `*` `,` `-` `/` `?` `L` `L-n` `W` `LW` `#` and names, as
  go-cron with `Extended`, plus `dL` above.
- `@` shortcuts: `@yearly`, `@annually`, `@monthly`, `@weekly`, `@daily`,
  `@midnight`, `@hourly`, as go-cron with `Descriptor` in lower case.
- Time zones: the `timezone` option (IANA name), default the system zone.
  Write it as a `CRON_TZ=` prefix.
- Parser setup: `SecondOptional | Minute | Hour | Dom | Month | Dow | Descriptor | Extended | DSTSkip`.

| Source spec | go-cron spec | What it runs |
|---|---|---|
| `0 0 18 * * 5L` | `0 0 18 * * 5#L` | Fri 2026-11-27 18:00: last Friday |
| `0 0 0 * * 7L` | `0 0 0 * * 7#L` | Sun 2026-11-29 00:00: last Sunday |
| `0 9 13 * FRI` | `0 9 13 * FRI` | Fri 2027-08-13 09:00: Friday the 13th |
| `0 9 * * Monday` | `0 9 * * MON` | Mon 2026-11-23 09:00 |
| `@DAILY` | `@daily` | Thu 2026-11-19 00:00 |
| `0 9 * * *`, `timezone: 'America/New_York'` | `CRON_TZ=America/New_York 0 9 * * *` | Wed 2026-11-18 14:00: 09:00 in New York |

### cron (kelektiv)

Reference: [cron README](https://github.com/kelektiv/node-cron/blob/v4.4.0/README.md)
(npm `cron`, 4.4.0). The README links crontab.org for the meaning of a spec,
so the lines below cite the code
([`time.ts`](https://github.com/kelektiv/node-cron/blob/v4.4.0/src/time.ts)) and its tests.

**Runs differently without an error**

| Spec | Source runs | go-cron runs, unchanged | For go-cron |
|---|---|---|---|
| `0 9 13 * FRI` (both day fields restricted) | the 13th and every Friday | Friday the 13th only | `DowOrDom`, or two entries |
| `0 9 1-31 * FRI` (a field with every value counts as `*`) | Fridays only | Fridays only (same); with `DowOrDom`, every day | with `DowOrDom`, write `0 9 * * FRI` |
| `30 2 * * *` on a spring-forward day | at the end of the gap | shifted by the gap's length | no option matches |

A stepped star (`*/2`) stays restricted in both, so do not add `StepWildcard`.

**Fails to parse in go-cron**

- `@minutely`, `@secondly`, `@weekdays`, `@weekends`. Write `* * * * *`,
  `* * * * * *`, `0 0 * * 1-5` and `0 0 * * 0,6`.
- Upper-case shortcuts (`@DAILY`). Write `@daily`.

**Parts**

- Fields: five, or six with seconds first. No year.
- Day numbers: as go-cron (0 or 7 is Sunday).
- Day of month and day of week: OR when both are restricted; see above.
- Special characters: `*` `,` `-` `/` and three-letter names. A range must not
  run high to low. No `L`, `W`, `#`, `?` or `H`.
- `@` shortcuts: `@yearly`, `@monthly`, `@weekly`, `@daily`, `@hourly` as go-cron
  with `Descriptor`; the four above have no go-cron descriptor.
- Time zones: the `timeZone` parameter (IANA name), or `utcOffset` in minutes,
  default local time. Write a zone as a `CRON_TZ=` prefix; a whole-hour offset
  as the `Etc/GMT` zone with the sign inverted (`utcOffset: 300` is
  `Etc/GMT-5`).
- Parser setup: `SecondOptional | Minute | Hour | Dom | Month | Dow | Descriptor | DowOrDom`.

| Source spec | go-cron spec | What it runs |
|---|---|---|
| `0 9 13 * FRI` | `0 9 13 * FRI` | Fri 2026-11-20 09:00: the 13th and every Friday (`DowOrDom`) |
| `0 9 13 * FRI` | `0 9 13 * *` and `0 9 * * FRI` | Sun 2026-12-13 09:00 for the first entry; the second runs every Friday; on a Friday the 13th both run |
| `0 9 1-31 * FRI` | `0 9 * * FRI` | Fri 2026-11-20 09:00: Fridays only |
| `0 9 13 * 0-7` | `0 9 13 * *` | Sun 2026-12-13 09:00: the 13th only |
| `@weekdays` | `0 0 * * 1-5` | Thu 2026-11-19 00:00 |
| `@weekends` | `0 0 * * 0,6` | Sat 2026-11-21 00:00 |
| `0 9 * * *`, `utcOffset: 300` | `CRON_TZ=Etc/GMT-5 0 9 * * *` | Wed 2026-11-18 04:00: 09:00 at UTC+5 |

## Calendar triggers

These are not cron specs; the rewrite tables map their calendar settings to
go-cron specs. Neither was run.

### systemd timers

Reference: [systemd.time(7), Calendar Events](https://www.freedesktop.org/software/systemd/man/latest/systemd.time.html#Calendar%20Events),
[systemd.timer(5)](https://www.freedesktop.org/software/systemd/man/latest/systemd.timer.html).

**Runs differently without an error**

| Spec | Source runs | go-cron runs, unchanged | For go-cron |
|---|---|---|---|
| `OnCalendar=weekly` | Mondays at 00:00 | `@weekly`: Sundays at 00:00 | `0 0 * * MON` |
| `OnCalendar=*-*~3` (`~N` counts back from 1 = the last day) | the third-last day | `L-3`: the fourth-last day | `0 0 L-2 * *` |

The `~` count is in
[`calendarspec.c`](https://github.com/systemd/systemd/blob/v262/src/shared/calendarspec.c)
(`find_end_of_month`). The last Friday is `Fri *-*~07/1`, go-cron `FRI#L`.

**Fails to parse in go-cron**

- `..` ranges (`Mon..Fri`, `1..5`). Write `-`.
- Fractional seconds: go-cron's seconds are whole.

**Parts**

- Fields: one string, `[weekday] [year-month-day] [hour:minute[:second]]`, with
  an optional zone at the end. In go-cron, reorder to minute, hour, day,
  month, day of week (seconds first with `Second`, year last with `Year`).
- Day numbers: weekday names only (no numbers), as go-cron's names.
- Day of month and day of week: AND, as go-cron (`Mon *-*-13` is a Monday the
  13th).
- Special characters: `*` `,` `..` (range) `/` (from a start value) and `~`.
  `*:5/15` is minutes 5, 20, 35, 50, as go-cron's `5/15`.
- Shortcuts: `hourly`, `daily`, `monthly`, `yearly`, `annually` as go-cron's
  `@` forms with `Descriptor`; `minutely`, `weekly`, `quarterly` and
  `semiannually` as written out below.
- Time zones: a zone name at the end (`*-*-* 09:00 Europe/Berlin`, `daily UTC`);
  without one, the system zone. Move it to a `CRON_TZ=` prefix.
- Parser setup: `Minute | Hour | Dom | Month | Dow`, plus `Second`, `Year`,
  `Descriptor` or `Extended` as the spec needs.

| Source spec | go-cron spec | What it runs |
|---|---|---|
| `OnCalendar=Mon..Fri *-*-* 09:00` | `0 9 * * MON-FRI` | Wed 2026-11-18 09:00 |
| `OnCalendar=Mon..Fri *-*-* 09..17:00` | `0 9-17 * * MON-FRI` | Wed 2026-11-18 09:00: hourly from 09:00 to 17:00 |
| `OnCalendar=*:5/15` | `5/15 * * * *` | Wed 2026-11-18 00:05: minutes 5, 20, 35, 50 |
| `OnCalendar=weekly` | `0 0 * * MON` | Mon 2026-11-23 00:00 |
| `OnCalendar=quarterly` | `0 0 1 1,4,7,10 *` | Fri 2027-01-01 00:00 |
| `OnCalendar=semiannually` | `0 0 1 1,7 *` | Fri 2027-01-01 00:00 |
| `OnCalendar=minutely` | `* * * * *` | Wed 2026-11-18 00:01 |
| `OnCalendar=*-*~1 09:00` | `0 9 L * *` | Mon 2026-11-30 09:00: last day of the month |
| `OnCalendar=*-*~3 00:00` | `0 0 L-2 * *` | Sat 2026-11-28 00:00: third-last day |
| `OnCalendar=Fri *-*~07/1 09:00` | `0 9 * * FRI#L` | Fri 2026-11-27 09:00: last Friday |
| `OnCalendar=Fri *-*-01..07 09:00` | `0 9 * * FRI#1` | Fri 2026-12-04 09:00: first Friday |
| `OnCalendar=*-*-* *:*:30` | `30 * * * * *` | Wed 2026-11-18 00:00:30: second 30 of each minute (`Second`) |
| `OnCalendar=2027-01-01 00:00` | `0 0 1 1 * 2027` | Fri 2027-01-01 00:00 (`Year`) |
| `OnCalendar=*-*-* 09:00 Europe/Berlin` | `CRON_TZ=Europe/Berlin 0 9 * * *` | Wed 2026-11-18 08:00: 09:00 in Berlin |
| `OnBootSec=`, `OnUnitActiveSec=` and other monotonic timers | no go-cron spec | relative to boot or the unit, not to the calendar |
| `RandomizedDelaySec=` | no go-cron spec | a random delay per run; `H` gives each job a fixed offset instead |
| `Persistent=true` | no go-cron spec | catch-up of missed runs is not part of a spec |

### Windows Task Scheduler

Reference: [`schtasks create`](https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/schtasks-create),
[monthly day-of-week trigger](https://learn.microsoft.com/en-us/windows/win32/taskschd/monthlydowtrigger),
[trigger start boundary](https://learn.microsoft.com/en-us/windows/win32/taskschd/trigger-startboundary).

**Runs differently without an error**

| Spec | Source runs | go-cron runs, unchanged | For go-cron |
|---|---|---|---|
| `/sc minute /mo 20` (intervals count from the start) | every 20 minutes from when the task starts | `*/20 * * * *`: at :00, :20 and :40 | `@every 20m`, which counts from when the job is added; the same for `/mo n` with hours. Day and week intervals as `@every 24h` steps drift an hour across a DST change, and months have no fixed length: no go-cron spec |
| 02:30 on a spring-forward day | 03:00 | 03:30 | no option matches; `DSTSkip` skips the run |

**Fails to parse in go-cron**

- Nothing: the triggers are settings, not text. Some have no go-cron spec
  (below).

**Parts**

- Fields: none; a task has triggers (`/sc MINUTE`, `HOURLY`, `DAILY`, `WEEKLY`,
  `MONTHLY`, `ONCE`, and event triggers) with a start time (`/st`), modifier
  (`/mo`), days (`/d`) and months (`/m`).
- Day numbers: day names only, as go-cron's names.
- Day of month and day of week: a monthly trigger takes one or the other.
  Several triggers on a task run on any of them (OR); in go-cron, add one entry
  per trigger.
- Special characters: none; `LASTDAY` is go-cron's `L`, `FIRST` … `FOURTH` with
  `/d` is `#1` … `#4`, and `LAST` with `/d` is `#L`. (The `schtasks` page lists
  `LAST` under `/d` but not under `/mo`, and does not define "last week of the
  month"; `FRI#L` is the reading here.)
- `@` shortcuts: none.
- Time zones: a trigger's start boundary with no offset follows the local zone;
  with `Z` it is UTC; with a fixed offset it ignores the local zone and DST.
  Write `CRON_TZ=` with an IANA zone, `CRON_TZ=UTC`, or for a fixed offset the
  `Etc/GMT` zone with the sign inverted (`-08:00` is `Etc/GMT+8`).
- Parser setup: `Minute | Hour | Dom | Month | Dow`, plus `Extended`,
  `Descriptor` or `Year` as the trigger needs.

| Source spec | go-cron spec | What it runs |
|---|---|---|
| `/sc daily /st 09:00` | `0 9 * * *` | Wed 2026-11-18 09:00 |
| `/sc weekly /d MON,FRI /st 09:00` | `0 9 * * MON,FRI` | Fri 2026-11-20 09:00 |
| `/sc monthly /d 15 /m MAY,JUN /st 15:00` | `0 15 15 MAY,JUN *` | Sat 2027-05-15 15:00 |
| `/sc monthly /mo lastday /m * /st 09:00` | `0 9 L * *` | Mon 2026-11-30 09:00 |
| `/sc monthly /mo SECOND /d SUN /st 09:00` | `0 9 * * SUN#2` | Sun 2026-12-13 09:00 |
| `/sc monthly /mo LAST /d FRI /st 09:00` | `0 9 * * FRI#L` | Fri 2026-11-27 09:00: last Friday |
| `/sc minute /mo 20` | `@every 20m` | Wed 2026-11-18 00:20: every 20 minutes from the start (`Descriptor`) |
| `/sc hourly /mo 3 /st 00:00 /du 0010:00` | `0 0,3,6,9 * * *` | Wed 2026-11-18 03:00 |
| `/sc once /sd 12/15/2026 /st 09:30` | `30 9 15 12 * 2026` | Tue 2026-12-15 09:30 (`Year`) |
| start boundary `T13:00:00-08:00` | `CRON_TZ=Etc/GMT+8 0 13 * * *` | Wed 2026-11-18 21:00: 13:00 at UTC−8 |
| `/sc weekly /mo 2 /d FRI` | no go-cron spec | every second Friday from the start date |
| `/sc onstart`, `onlogon`, `onidle`, `onevent` | no go-cron spec | event triggers, not times |
