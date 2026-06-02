# ADR-022: Windowless Drain-and-Replace (`DrainAndUpsertJob`)

## Status
Accepted

## Date
2026-06-02

## Context

The idiomatic way to reschedule a named entry without overlapping the
in-flight invocation is the two-step sequence:

```go
cr.WaitForJobByName("my-job")
cr.UpsertJob(newSpec, newJob, cron.WithName("my-job"))
```

`WaitForJobByName` drains the running invocation, then `UpsertJob` replaces
the entry. This was reported (via the [weaviate](https://github.com/weaviate/weaviate)
object-TTL scheduler) to have a narrow but real correctness gap:

> Previously a `RemoveByName` → wait → `AddJob` sequence guaranteed that no
> further invocation could start once the wait returned. With
> `WaitForJobByName` → `UpsertJob` there is a window between the two calls in
> which the old schedule can still fire one more time.

The gap exists because `WaitForJobByName` does **not** unschedule the entry —
it only blocks on the per-entry job tracker. Between the wait returning and
`UpsertJob` installing the new schedule, the run loop is free to fire the
stale schedule once more (e.g. if the drained invocation ran long enough to
cross the next tick boundary). `SkipIfStillRunning` prevents a concurrent
double-run but not this extra invocation under the old schedule.

The old `RemoveByName` → wait → `AddJob` pattern did not have the gap because
`RemoveByName` unschedules the entry *before* the wait, so no fire is possible
after the drain. The migration to `UpsertJob` traded that guarantee for the
ergonomics of atomic create-or-update.

## Decision

Add `DrainAndUpsertJob(spec string, cmd Job, opts ...JobOption) (EntryID, error)`
— a windowless variant of `UpsertJob` that closes the gap by **pausing the
entry before draining**:

1. Resolve the entry by name once; capture its `ID` and prior `Paused` state.
   If absent, create via `ScheduleJob` (no stale schedule ⇒ no window).
2. `PauseEntry(id)` — atomic with respect to the run loop.
3. `WaitForJobByName(name)` — drain any in-flight invocation.
4. `UpdateEntry(id, schedule, cmd)` — swap schedule and job while paused.
5. `ResumeEntry(id)` — only if the caller had not already paused the entry.

The method **composes existing public methods**; it does not add a new run-loop
request type or a monolithic locked operation. Each composed call acquires and
releases `runningMu` independently, so the blocking drain in step 3 never holds
a cron lock and never stalls the run loop or other mutators.

### Why this is windowless

The guarantee — *no invocation can start under the stale schedule after the
drain returns and before the swap completes* — rests on three properties of the
scheduler, all verified in `cron.go`:

1. **Single-goroutine select.** The run loop processes the timer-fire arm and
   the pause arm as mutually exclusive cases of one `select` on one goroutine.
   `processDueEntries` runs to completion within an arm before the loop returns
   to `select`.
2. **Pause is honored before any later fire.** `PauseEntry` sends synchronously
   on the pause channel and blocks on the reply. Because the run loop is
   sequential, every iteration after the reply observes `Paused == true` and
   takes the skip branch in `processDueEntries`, launching no job.
3. **Synchronous tracker increment.** The only invocation that can still be in
   flight when `PauseEntry` returns is one launched by a timer fire processed in
   the same iteration immediately before the pause request was read. That fire
   increments the per-entry job tracker **synchronously** (inside
   `processDueEntries`, before the `go func()` that runs the job), so
   `WaitForJobByName` provably observes and joins it.

`updateSchedule` does not touch `Paused`, so the entry stays paused across the
swap; the resume in step 5 re-enables the **new** schedule, whose `Next` was
computed at swap time.

### Explicit non-guarantee

`DrainAndUpsertJob` is **not** atomic against *other* concurrent mutators of the
same entry. Between the composed calls another goroutine may `Remove`, `Resume`,
or `Trigger` the entry. These are handled gracefully — removal surfaces as the
`ErrEntryNotFound` → create fallthrough; a `Trigger` while paused is rejected
with `ErrEntryPaused` or fires the already-swapped new job — but they fall
outside the no-stale-fire guarantee, which is specifically about the **old
schedule** not firing after the drain. The swap targets the captured `ID`
(not the name), so a name reused by a different entry between drain and swap
cannot cause an ABA.

## Consequences

### Positive
- Closes the stale-fire window for the common graceful-reschedule use case with
  a single call.
- No new run-loop machinery, no new struct fields, no lock held across the
  blocking drain — low risk, easy to reason about.
- Mirrors `UpsertJob`'s contract exactly (same options, same errors), so it is a
  drop-in replacement for the two-step pattern.

### Negative
- Composes four run-loop round trips (lookup, pause, swap, resume) plus the
  drain, versus `UpsertJob`'s one. The extra round trips are O(1) and dwarfed by
  the drain wait; not a concern in practice.
- The windowless guarantee depends on the run loop incrementing the job tracker
  **synchronously before** launching the job goroutine. A future refactor that
  moved `start()` into the goroutine would reopen the window. Flagged here and
  pinned by `TestDrainAndUpsertNoStaleFire`.

### Neutral
- Purely additive. `UpsertJob` and `WaitForJobByName` are unchanged.

## Alternatives Considered

### 1. Keep the two-step `WaitForJobByName` + `UpsertJob`
Document the window and rely on idempotent jobs.

**Rejected** as the default answer: the window is subtle, surfaces only under
load, and "make your job idempotent" is not always available to callers. Keeping
the two-step API is fine (it stays), but a windowless option should exist.

### 2. A single monolithic run-loop operation
Add one request type carrying `{pause, drainSignal, swap, restore}` handled
atomically inside the run loop.

**Rejected:** the drain can block for the full duration of a long-running job.
Performing it inside the run loop would stall every other entry and mutator for
that duration — a worse failure mode than the narrow window it closes. The
composition achieves the required guarantee without blocking the loop.

### 3. Restore the `RemoveByName` → wait → `AddJob` ordering
Unschedule first, then drain, then re-create.

**Rejected:** it loses the atomic create-or-update and entry-ID stability that
`UpsertJob` provides, and briefly leaves the entry absent. Pausing achieves the
same "cannot fire" property while keeping the entry (and its ID) in place.

## References

- weaviate object-TTL scheduler review that motivated this change
  ([weaviate#10477](https://github.com/weaviate/weaviate/pull/10477))
- [ADR-017](ADR-017-job-with-context.md): per-entry context (canceled on replacement)
- `WaitForJobByName`, `UpsertJob`, `PauseEntry`/`ResumeEntry` in `cron.go`
