# ADR-028: Entry Name as Hash Key

## Status
Accepted

## Date
2026-11-01

## Context

`H` selects a value from a hash of a key, so that jobs with the same spec run at
different times. The key was a parser setting (`Parser.WithHashKey`) or an
argument of `Parser.ParseWithHashKey`. `AddJob` parses the spec before it reads
the job options, so the entry name never reached the parser:

- Without a parser key, `AddFunc("H * * * *", f, WithName("job-a"))` failed with
  "h expressions require a hash key".
- With a parser key, every job with the same `H` spec on that `Cron` ran at the
  same time, which is the situation `H` exists to prevent.

The only way to spread jobs was `ParseWithHashKey` plus `ScheduleJob` per job.
Jenkins, where `H` comes from, hashes the job name. Issue #230 proposed a key per
job, and #470 reports the gap.

## Decision

A named entry uses its name as the hash key. The key is resolved in this order:

1. the entry's name, if it has one and the parser is the built-in `Parser`;
2. the parser's own key (`WithHashKey`);
3. no key: a spec with `H` is an error.

`Cron` checks for the concrete type `Parser` (or `*Parser`). `ScheduleParser` stays
unchanged, and every other parser, including a wrapper that embeds `Parser`, is
called through `Parse`, also for named entries. A spec without an H expression
always goes through `Parse`.

Every method that takes a spec for a named entry applies this rule: `AddFunc`,
`AddJob`, `AddOnceFunc`, `AddOnceJob`, `UpsertJob`, `DrainAndUpsertJob`,
`UpdateJob`, `UpdateJobByName`, `UpdateEntryJob`, `UpdateEntryJobByName` and
`AddWorkflow`. An update therefore keeps the values of an entry added with a
spec. An entry added with `ParseWithHashKey` and `ScheduleJob` under a different
key moves to the values of its name on such an update.

`Cron.ValidateSpec` parses as an unnamed job would, with the parser's own key.
Only when the spec fails for a missing key does it retry with a stand-in key,
because the job's name is unknown there and a named job on the built-in `Parser`
can run the spec. For any other parser the retry calls `Parse` again, so the
error stays.

Pinned by `TestEntryNameIsHashKey`, `TestUpdateKeepsEntryNameAsHashKey`,
`TestUpdateUnnamedEntryUsesParserKey`, `TestUnnamedJobHashKey`,
`TestOtherParserUsesParse`, `TestAddWorkflowHashKey`, `TestCronValidateSpecHash`,
`TestJobOptionsWithoutHash`, `TestJobOptionReadsScheduleWithHash` and
`TestNilJobOption` in `hash_entry_name_test.go`.

## Consequences

### Positive

- `H` spreads named jobs without further setup, as in Jenkins.
- The parser key stays useful as the key for unnamed jobs.

### Negative

- A job with a name and a parser key gets new values with this version, once.
  This is a behavior change and needs a release note.
- Renaming a job moves it to other values.
- `UpdateJob` and `UpdateEntryJob` look the entry up before they parse, so an
  unknown id with an invalid spec now reports `ErrEntryNotFound`, not the parse
  error.
- A wrapper around `Parser` does not get the entry name as key. It needs its own
  key, as before.
- An unnamed job with an `H` spec passes `Cron.ValidateSpec` on a parser without a
  key and fails in `AddFunc`, with the missing-key error.
- `ValidateSpec` cannot predict results that depend on the chosen value: with
  `StrictDays` and no parser key, `0 0 H 2 *` is judged by the stand-in key,
  although it fails only for a key that selects a day February lacks.
- For a spec with an H expression, `AddJob` applies the job options twice, once on
  an empty `Entry` to read the name, as `UpsertJob` already did. A custom
  `JobOption` with a side effect runs twice. A panic on the empty `Entry` is
  ignored, because an option that reads `Entry.Schedule` works on the real entry.
  A spec without H is not affected.

## Alternatives Considered

### 1. A job option `WithHashKey`, as proposed in #230

It keeps the name and the key apart, but every caller has to repeat the name
(`WithName(n), WithHashKey(n)`), and a caller who forgets it has the same defect
as before. It can still be added for a key that differs from the name;
`ParseWithHashKey` plus `ScheduleJob` covers that case today.

### 2. The parser key wins over the name

It avoids the behavior change for jobs that have both. But a parser key then
keeps exactly the defect this decision removes: all jobs on one `Cron` at the
same time.

### 3. Extending `ScheduleParser`

Adding `ParseWithHashKey` to the interface would break every custom parser and
the drop-in compatibility with `robfig/cron/v3`.

### 4. An optional interface for parsers that accept a key

An exported `HashKeyParser` interface would let custom parsers opt in. But a
wrapper that embeds `Parser` gets `ParseWithHashKey` by promotion, so the type
assertion matches and the call bypasses the wrapper's own `Parse`, without any
error. Review of the first version measured that: a wrapper that prepends
`TZ=Asia/Tokyo` lost its zone. No unexported method can fix this, because a
custom parser could not implement one. It can be added if a custom parser needs
it, with a method name `Parser` does not have.
