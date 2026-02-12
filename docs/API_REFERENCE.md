# go-cron API Reference

> Complete API documentation for all exported types, functions, and methods

## Table of Contents

- [Package cron](#package-cron)
- [Types](#types)
  - [Cron](#cron)
  - [Entry](#entry)
  - [EntryID](#entryid)
  - [Option](#option)
  - [JobOption](#joboption)
  - [Schedule](#schedule)
  - [ScheduleWithPrev](#schedulewithprev)
  - [SpecSchedule](#specschedule)
  - [ConstantDelaySchedule](#constantdelayschedule)
  - [Parser](#parser)
  - [ParseOption](#parseoption)
  - [Chain](#chain)
  - [JobWrapper](#jobwrapper)
  - [Job](#job)
  - [FuncJob](#funcjob)
  - [JobWithContext](#jobwithcontext)
  - [FuncJobWithContext](#funcjobwithcontext)
  - [ErrorJob](#errorjob)
  - [FuncErrorJob](#funcerrorjob)
  - [NamedJob](#namedjob)
  - [MissedPolicy](#missedpolicy)
  - [ObservabilityHooks](#observabilityhooks)
  - [SpecAnalysis](#specanalysis)
  - [ValidationError](#validationerror)
  - [PanicError](#panicerror)
  - [Logger](#logger)
  - [Clock](#clock)
  - [Timer](#timer)
  - [FakeClock](#fakeclock)
  - [RealClock](#realclock)
- [Functions](#functions)
- [Constants](#constants)
- [Errors](#errors)

---

## Package cron

```go
import "github.com/netresearch/go-cron"
```

Package cron implements a cron spec parser and job runner.

---

## Types

### Cron

```go
type Cron struct {
    // contains filtered or unexported fields
}
```

Cron keeps track of any number of entries, invoking the associated func as
specified by the schedule. It may be started, stopped, and the entries may
be inspected while running.

Entries are stored in a min-heap ordered by next execution time, providing
O(log n) insertion/removal and O(1) access to the next entry to run.

#### func New

```go
func New(opts ...Option) *Cron
```

New returns a new Cron job runner, modified by the given options.

**Default Settings:**
- **Time Zone**: `time.Local`
- **Parser**: Standard 5-field cron (minute, hour, dom, month, dow)
- **Chain**: Recovers panics and logs to stderr
- **Clock**: `RealClock{}` (real system time)

**Example:**
```go
c := cron.New()
c := cron.New(cron.WithSeconds())
c := cron.New(cron.WithLocation(time.UTC))
```

#### func (*Cron) AddFunc

```go
func (c *Cron) AddFunc(spec string, cmd func()) (EntryID, error)
```

AddFunc adds a func to the Cron to be run on the given schedule.
The spec is parsed using the time zone of this Cron instance as the default.
An opaque ID is returned that can be used to later remove it.

**Example:**
```go
id, err := c.AddFunc("30 * * * *", func() {
    fmt.Println("Every hour on the half hour")
})
```

#### func (*Cron) AddJob

```go
func (c *Cron) AddJob(spec string, cmd Job) (EntryID, error)
```

AddJob adds a Job to the Cron to be run on the given schedule.
The spec is parsed using the time zone of this Cron instance as the default.
An opaque ID is returned that can be used to later remove it.

**Example:**
```go
type MyJob struct{}
func (j MyJob) Run() { fmt.Println("Running") }

id, err := c.AddJob("@hourly", MyJob{})
```

#### func (*Cron) Schedule

```go
func (c *Cron) Schedule(schedule Schedule, cmd Job) EntryID
```

Schedule adds a Job to the Cron to be run on the given schedule.
The job is wrapped with the configured Chain.

**Example:**
```go
id := c.Schedule(cron.Every(time.Hour), cron.FuncJob(func() {
    fmt.Println("Every hour")
}))
```

#### func (*Cron) UpdateJob

```go
func (c *Cron) UpdateJob(id EntryID, spec string) error
```

UpdateJob updates the schedule of an existing entry, parsing the provided cron spec.
Returns `ErrEntryNotFound` if the entry does not exist.
Returns a parse error if the spec is invalid for the configured parser.

If the scheduler is running, the update is applied safely via the run loop and takes
effect immediately for next-run computation. If stopped, the schedule is updated
directly in place.

**Example:**
```go
id, _ := c.AddFunc("0 9 * * *", dailyReport)
// Change to run at 10:30 every day
if err := c.UpdateJob(id, "30 10 * * *"); err != nil {
    log.Println("update failed:", err)
}
```

#### func (*Cron) UpdateJobByName

```go
func (c *Cron) UpdateJobByName(name, spec string) error
```

UpdateJobByName updates the schedule of an existing entry identified by its name,
parsing the provided cron spec.
Returns `ErrEntryNotFound` if the entry does not exist.
Returns a parse error if the spec is invalid for the configured parser.

If the scheduler is running, the actual update is delegated to `UpdateSchedule`
which routes through the run loop safely.

**Example:**
```go
c.AddFunc("0 9 * * *", dailyReport, cron.WithName("daily-report"))
// Change to run at 10:30 every day
if err := c.UpdateJobByName("daily-report", "30 10 * * *"); err != nil {
    log.Println("update failed:", err)
}
```

#### func (*Cron) UpdateSchedule

```go
func (c *Cron) UpdateSchedule(id EntryID, schedule Schedule) error
```

UpdateSchedule updates the schedule of an existing entry.
Returns `ErrEntryNotFound` if the entry does not exist.
If the scheduler is running, the entry is re-positioned in the heap according to
its new `Next` time; if stopped, the new schedule will apply when started.

Notes:
- Preserves `WrappedJob`, `Job`, name, tags, and missed-run settings
- Recomputes `Next` from `clock.Now()`; `Prev` is unchanged

**Example:**
```go
id, _ := c.AddFunc("0 9 * * *", dailyReport)
// Change to run at 10:30 every day
schedule, _ := cron.ParseStandard("30 10 * * *")
if err := c.UpdateSchedule(id, schedule); err != nil {
    log.Println("update failed:", err)
}
```

#### func (*Cron) UpdateScheduleByName

```go
func (c *Cron) UpdateScheduleByName(name string, schedule Schedule) error
```

UpdateScheduleByName updates the Schedule of an existing entry identified by its name.
Returns `ErrEntryNotFound` if the entry does not exist.

Lookup is O(1) via the internal name index. If the scheduler is running,
the actual update is delegated to `UpdateSchedule` which routes through the
run loop safely.

**Example:**
```go
c.AddFunc("0 9 * * *", dailyReport, cron.WithName("daily-report"))
// Change to run at 10:30 every day
schedule, _ := cron.ParseStandard("30 10 * * *")
if err := c.UpdateScheduleByName("daily-report", schedule); err != nil {
    log.Println("update failed:", err)
}
```

#### func (*Cron) UpdateEntry

```go
func (c *Cron) UpdateEntry(id EntryID, schedule Schedule, job Job) error
```

UpdateEntry atomically replaces both the Schedule and the Job of an existing entry.
The new job is re-wrapped through the configured Chain, so middleware (Recover,
SkipIfStillRunning, etc.) is applied to the replacement job.

When the job is replaced, the old entry's per-entry context is canceled (signaling
any running `JobWithContext` to stop), and a fresh context is created for the new job.
This eliminates the need for callers to manage their own `context.WithCancel` per
reschedule.

Returns `ErrEntryNotFound` if the entry does not exist.
Returns `ErrNilJob` if job is nil (use `UpdateSchedule` to update only the schedule).

Concurrency semantics are the same as `UpdateSchedule`.

**Example:**
```go
id, _ := c.AddFunc("0 9 * * *", dailyReport)
// Replace both schedule and job atomically
// The old job's context is automatically canceled.
if err := c.UpdateEntry(id, cron.Every(5*time.Second), cron.FuncJob(func() {
    doWork()
})); err != nil {
    log.Println("update failed:", err)
}
```

#### func (*Cron) UpdateEntryByName

```go
func (c *Cron) UpdateEntryByName(name string, schedule Schedule, job Job) error
```

UpdateEntryByName atomically replaces both the Schedule and the Job of an existing
entry identified by its Name. Lookup is O(1) via the internal name index.
Delegates to `UpdateEntry` for the actual update.

Returns `ErrEntryNotFound` if the entry does not exist.
Returns `ErrNilJob` if job is nil.

**Example:**
```go
c.AddFunc("0 9 * * *", dailyReport, cron.WithName("daily-report"))
// Replace both schedule and job by name
if err := c.UpdateEntryByName("daily-report", cron.Every(5*time.Second), newJob); err != nil {
    log.Println("update failed:", err)
}
```

#### func (*Cron) UpdateEntryJob

```go
func (c *Cron) UpdateEntryJob(id EntryID, spec string, job Job) error
```

UpdateEntryJob parses spec with the Cron's configured parser, then atomically
replaces both schedule and job. This eliminates the need for callers to construct
their own parser matching the Cron's configuration.

Returns a parse error if spec is invalid for the configured parser.
Returns `ErrEntryNotFound` if the entry does not exist.
Returns `ErrNilJob` if job is nil.

**Example:**
```go
id, _ := c.AddFunc("0 9 * * *", dailyReport)
// Update both schedule and job using a spec string
if err := c.UpdateEntryJob(id, "@every 5m", newJob); err != nil {
    log.Println("update failed:", err)
}
```

#### func (*Cron) UpdateEntryJobByName

```go
func (c *Cron) UpdateEntryJobByName(name, spec string, job Job) error
```

UpdateEntryJobByName is the name-based variant of `UpdateEntryJob`.
Parses spec with the Cron's configured parser, then atomically replaces both
schedule and job of the entry identified by name.

Returns a parse error if spec is invalid for the configured parser.
Returns `ErrEntryNotFound` if the entry does not exist.
Returns `ErrNilJob` if job is nil.

**Example:**
```go
c.AddFunc("0 9 * * *", dailyReport, cron.WithName("daily-report"))
// Update both schedule and job by name using a spec string
if err := c.UpdateEntryJobByName("daily-report", "@every 5m", newJob); err != nil {
    log.Println("update failed:", err)
}
```

#### func (*Cron) UpsertJob

```go
func (c *Cron) UpsertJob(spec string, cmd Job, opts ...JobOption) (EntryID, error)
```

UpsertJob creates or updates a named job entry. If an entry with the given
name already exists, its schedule and job are atomically replaced via
`UpdateEntry`. If no entry exists, a new one is created via `ScheduleJob`.

A `WithName` option is required. Returns `ErrNameRequired` if no name is provided.

This eliminates the common "try update, fallback to add" boilerplate:

**Example:**
```go
// Before (manual upsert):
if err := c.UpdateEntryJobByName(name, spec, job); errors.Is(err, cron.ErrEntryNotFound) {
    c.AddJob(spec, job, cron.WithName(name))
}

// After:
id, err := c.UpsertJob(spec, job, cron.WithName(name))
```

#### func (*Cron) WaitForJob

```go
func (c *Cron) WaitForJob(id EntryID)
```

WaitForJob blocks until all currently-running invocations of the given entry
complete. Returns immediately if the entry is not currently running or does not
exist.

This enables graceful job replacement — wait for the current execution to finish
before replacing the job:

```go
cr.WaitForJob(id)
cr.UpsertJob(newSpec, newJob, cron.WithName("my-job"))
```

#### func (*Cron) WaitForJobByName

```go
func (c *Cron) WaitForJobByName(name string)
```

WaitForJobByName is the named variant of WaitForJob. Blocks until all
currently-running invocations of the named entry complete. Returns immediately
if the entry is not currently running or no entry has the given name.

```go
cr.WaitForJobByName("my-job")
cr.UpsertJob(newSpec, newJob, cron.WithName("my-job"))
```

#### func (*Cron) IsJobRunning

```go
func (c *Cron) IsJobRunning(id EntryID) bool
```

IsJobRunning reports whether the entry with the given ID has any invocations
currently in flight. Returns false for non-existent entries.

#### func (*Cron) IsJobRunningByName

```go
func (c *Cron) IsJobRunningByName(name string) bool
```

IsJobRunningByName reports whether the named entry has any invocations currently
in flight. Returns false if no entry has the given name.

```go
if cr.IsJobRunningByName("my-job") {
    cr.WaitForJobByName("my-job")
}
cr.UpsertJob(newSpec, newJob, cron.WithName("my-job"))
```

#### func (*Cron) ScheduleJob

```go
func (c *Cron) ScheduleJob(schedule Schedule, cmd Job, opts ...JobOption) (EntryID, error)
```

ScheduleJob adds a Job to the Cron to be run on the given schedule.
The job is wrapped with the configured Chain. Unlike `Schedule`, returns an error
instead of silently logging on failure, and supports `JobOption` arguments.

Returns `ErrMaxEntriesReached` if the maximum entry limit has been reached.
Returns `ErrDuplicateName` if a name is provided and already exists.

#### func (*Cron) AddOnceFunc

```go
func (c *Cron) AddOnceFunc(spec string, cmd func(), opts ...JobOption) (EntryID, error)
```

AddOnceFunc adds a func to run once on the given schedule, then automatically
remove itself. Convenience wrapper combining `AddFunc` with `WithRunOnce()`.

#### func (*Cron) AddOnceJob

```go
func (c *Cron) AddOnceJob(spec string, cmd Job, opts ...JobOption) (EntryID, error)
```

AddOnceJob adds a Job to run once on the given schedule, then automatically
remove itself. Convenience wrapper combining `AddJob` with `WithRunOnce()`.

#### func (*Cron) ScheduleOnceJob

```go
func (c *Cron) ScheduleOnceJob(schedule Schedule, cmd Job, opts ...JobOption) (EntryID, error)
```

ScheduleOnceJob adds a Job to run once on the given schedule, then automatically
remove itself. Convenience wrapper combining `ScheduleJob` with `WithRunOnce()`.

#### func (*Cron) ValidateSpec

```go
func (c *Cron) ValidateSpec(spec string) error
```

ValidateSpec validates a cron expression using this Cron instance's configured
parser. Returns nil if valid, or an error describing the problem. Useful for
pre-validating user input when the Cron uses a custom parser.

**Example:**
```go
c := cron.New(cron.WithSeconds())
if err := c.ValidateSpec("0 30 * * * *"); err != nil {
    return fmt.Errorf("invalid cron expression: %w", err)
}
```

#### func (*Cron) Entries

```go
func (c *Cron) Entries() []Entry
```

Entries returns a snapshot of the cron entries, sorted by next execution time.

#### func (*Cron) Entry

```go
func (c *Cron) Entry(id EntryID) Entry
```

Entry returns a snapshot of the given entry, or a zero Entry if not found.
This operation is O(1) using the internal index map.

#### func (*Cron) EntryByName

```go
func (c *Cron) EntryByName(name string) Entry
```

EntryByName returns a snapshot of the entry with the given name,
or an invalid Entry (`Entry.Valid() == false`) if not found.
This operation is O(1) using the internal name index.

**Example:**
```go
c.AddFunc("0 9 * * *", job, cron.WithName("daily-report"))
entry := c.EntryByName("daily-report")
if entry.Valid() {
    fmt.Println("Next run:", entry.Next)
}
```

#### func (*Cron) EntriesByTag

```go
func (c *Cron) EntriesByTag(tag string) []Entry
```

EntriesByTag returns snapshots of all entries that have the given tag.
Returns an empty slice if no entries match.

**Example:**
```go
c.AddFunc("0 9 * * *", job1, cron.WithTags("reports"))
c.AddFunc("0 * * * *", job2, cron.WithTags("reports"))
entries := c.EntriesByTag("reports") // Returns both entries
```

#### func (*Cron) Remove

```go
func (c *Cron) Remove(id EntryID)
```

Remove an entry from being run in the future. Cancels the entry's per-entry context.

#### func (*Cron) RemoveByName

```go
func (c *Cron) RemoveByName(name string) bool
```

RemoveByName removes the entry with the given name.
Returns true if an entry was removed, false if no entry had that name.

#### func (*Cron) RemoveByTag

```go
func (c *Cron) RemoveByTag(tag string) int
```

RemoveByTag removes all entries that have the given tag.
Returns the number of entries removed.

#### func (*Cron) IsRunning

```go
func (c *Cron) IsRunning() bool
```

IsRunning returns true if the cron scheduler is currently running.

#### func (*Cron) Start

```go
func (c *Cron) Start()
```

Start the cron scheduler in its own goroutine, or no-op if already started.

#### func (*Cron) Run

```go
func (c *Cron) Run()
```

Run the cron scheduler in the current goroutine (blocking), or no-op if already running.

#### func (*Cron) Stop

```go
func (c *Cron) Stop() context.Context
```

Stop stops the cron scheduler if it is running; otherwise it does nothing.
A context is returned so the caller can wait for running jobs to complete.

**Example:**
```go
ctx := c.Stop()
<-ctx.Done() // Wait for all jobs to finish
```

#### func (*Cron) StopAndWait

```go
func (c *Cron) StopAndWait()
```

StopAndWait stops the cron scheduler and blocks until all running jobs complete.
Equivalent to `<-c.Stop().Done()`.

#### func (*Cron) StopWithTimeout

```go
func (c *Cron) StopWithTimeout(timeout time.Duration) bool
```

StopWithTimeout stops the cron scheduler and waits for running jobs to complete
with a timeout. Returns true if all jobs completed within the timeout, false if
the timeout was reached. A timeout of zero or negative waits indefinitely.

**Example:**
```go
if !c.StopWithTimeout(30 * time.Second) {
    log.Println("Warning: some jobs did not complete within 30s")
}
```

#### func (*Cron) Location

```go
func (c *Cron) Location() *time.Location
```

Location gets the time zone location.

---

### Entry

```go
type Entry struct {
    ID                EntryID       // Cron-assigned unique identifier
    Schedule          Schedule      // Schedule for this entry
    Next              time.Time     // Next activation time (zero if not started)
    Prev              time.Time     // Last run time (zero if never run)
    WrappedJob        Job           // Job with chain wrappers applied
    Job               Job           // Original job as submitted
    Name              string        // Optional name for the entry
    MissedPolicy      MissedPolicy  // Policy for handling missed executions
    MissedGracePeriod time.Duration // Maximum age for catch-up runs
}
```

Entry consists of a schedule and the func to execute on that schedule.

**Per-entry context:** Each entry has its own `context.Context` derived from the
Cron's base context. Jobs implementing `JobWithContext` receive this per-entry
context, enabling fine-grained cancellation:
- **Remove/RemoveByName**: cancels the removed entry's context
- **UpdateEntry** (with new job): cancels the old context, creates a fresh one
- **UpdateSchedule** (schedule-only): does NOT cancel the context
- **Stop()**: cancels the base context, which cascades to all entry contexts

#### func (Entry) Valid

```go
func (e Entry) Valid() bool
```

Valid returns true if this is not the zero entry.

#### func (Entry) Run

```go
func (e Entry) Run()
```

Run executes the entry's job through the configured chain wrappers.
Use this instead of `Entry.Job.Run()` when chain behavior is needed.

---

### EntryID

```go
type EntryID uint64
```

EntryID identifies an entry within a Cron instance.
Using uint64 prevents overflow and ID collisions on all platforms.

---

### MissedPolicy

```go
type MissedPolicy int

const (
    MissedSkip    MissedPolicy = iota // Do not catch up on missed executions (default)
    MissedRunOnce                      // Run once for the most recent missed execution
    MissedRunAll                       // Run for every missed execution (capped at 100)
)
```

MissedPolicy defines how to handle jobs that were scheduled to run while the
scheduler was not running (e.g., application restart).

**Important:** This feature requires the user to provide the last run time via
`WithPrev()`. The scheduler does NOT persist state - users are responsible for
storing and loading last run times from their own persistence layer.

**Policies:**
- `MissedSkip`: Default behavior. No catch-up runs occur.
- `MissedRunOnce`: Run the job once immediately for the most recent missed time.
- `MissedRunAll`: Run the job for every missed execution (up to 100 runs for safety).

**Example:**
```go
// Load last run time from your database
lastRun := loadFromDatabase("daily-report")

c.AddFunc("0 9 * * *", dailyReport,
    cron.WithPrev(lastRun),                      // When it last ran
    cron.WithMissedPolicy(cron.MissedRunOnce),   // Run once if missed
    cron.WithMissedGracePeriod(2*time.Hour),     // Only if within 2 hours
)
```

See [PERSISTENCE_GUIDE.md](PERSISTENCE_GUIDE.md) for complete integration patterns.

---

### JobOption

```go
type JobOption func(*Entry)
```

JobOption represents a modification to a specific job entry.
Job options are passed to `AddFunc`, `AddJob`, or `Schedule` methods.

#### func WithName

```go
func WithName(name string) JobOption
```

WithName sets a name for the job entry. Named jobs can be looked up via
`EntryByName()` and filtered via `EntriesByTag()`.

**Example:**
```go
c.AddFunc("0 9 * * *", dailyReport, cron.WithName("daily-report"))
entry := c.EntryByName("daily-report")
```

#### func WithPrev

```go
func WithPrev(t time.Time) JobOption
```

WithPrev sets the last execution time for the job entry. This is used to
calculate missed executions when combined with `WithMissedPolicy()`.

**Example:**
```go
lastRun := loadFromDatabase("my-job")
c.AddFunc("0 * * * *", myJob,
    cron.WithPrev(lastRun),
    cron.WithMissedPolicy(cron.MissedRunOnce),
)
```

#### func WithMissedPolicy

```go
func WithMissedPolicy(policy MissedPolicy) JobOption
```

WithMissedPolicy sets the policy for handling missed job executions.
See `MissedPolicy` for available options.

#### func WithMissedGracePeriod

```go
func WithMissedGracePeriod(d time.Duration) JobOption
```

WithMissedGracePeriod sets the maximum age for missed executions to be
caught up. Missed runs older than this duration are skipped even with
`MissedRunAll` policy.

**Example:**
```go
// Only catch up on missed runs from the last hour
c.AddFunc("*/5 * * * *", job,
    cron.WithPrev(lastRun),
    cron.WithMissedPolicy(cron.MissedRunAll),
    cron.WithMissedGracePeriod(time.Hour),
)
```

#### func WithTags

```go
func WithTags(tags ...string) JobOption
```

WithTags sets tags for categorizing the job entry. Multiple entries can
share the same tags, enabling group operations.

**Example:**
```go
c.AddFunc("* * * * *", job1, cron.WithTags("reports", "daily"))
c.AddFunc("0 * * * *", job2, cron.WithTags("reports", "hourly"))
entries := c.EntriesByTag("reports")
```

---

### Option

```go
type Option func(*Cron)
```

Option represents a modification to the default behavior of a Cron.

#### func WithLocation

```go
func WithLocation(loc *time.Location) Option
```

WithLocation overrides the timezone of the cron instance.

#### func WithSeconds

```go
func WithSeconds() Option
```

WithSeconds overrides the parser to include a seconds field as the first field.
Equivalent to Quartz scheduler format.

#### func WithParser

```go
func WithParser(p ScheduleParser) Option
```

WithParser overrides the parser used for interpreting job schedules.

#### func WithChain

```go
func WithChain(wrappers ...JobWrapper) Option
```

WithChain specifies Job wrappers to apply to all jobs added to this cron.

#### func WithLogger

```go
func WithLogger(logger Logger) Option
```

WithLogger uses the provided logger.

#### func WithClock

```go
func WithClock(clock Clock) Option
```

WithClock uses the provided Clock implementation instead of RealClock.
Useful for testing time-dependent behavior without waiting.

**Example:**
```go
// For testing with FakeClock
fakeClock := cron.NewFakeClock(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
c := cron.New(cron.WithClock(fakeClock))
c.Start()
fakeClock.Advance(time.Hour) // Trigger jobs deterministically

// RealClock is used by default, no need to specify unless overriding
```

#### func WithContext

```go
func WithContext(ctx context.Context) Option
```

WithContext sets the base context for all job executions. When `Stop()` is called,
this context is canceled, signaling all running `JobWithContext` jobs to shut down.
If not specified, `context.Background()` is used.

**Example:**
```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()
c := cron.New(cron.WithContext(ctx))
```

#### func WithMaxEntries

```go
func WithMaxEntries(maxEntries int) Option
```

WithMaxEntries limits the maximum number of entries. When the limit is reached,
`AddFunc` and `AddJob` return `ErrMaxEntriesReached`. A limit of 0 means unlimited.

#### func WithObservability

```go
func WithObservability(hooks ObservabilityHooks) Option
```

WithObservability configures observability hooks for monitoring cron operations.

#### func WithSecondOptional

```go
func WithSecondOptional() Option
```

WithSecondOptional overrides the parser to accept an optional seconds field.
Expressions can have either 5 fields (standard) or 6 fields (with seconds).

#### func WithMinEveryInterval

```go
func WithMinEveryInterval(d time.Duration) Option
```

WithMinEveryInterval configures the minimum interval allowed for `@every` expressions.
Default is 1 second. Set to 0 for sub-second intervals (useful for testing).

#### func WithMaxSearchYears

```go
func WithMaxSearchYears(years int) Option
```

WithMaxSearchYears configures how far into the future schedule matching searches
before giving up. Default is 5 years.

#### func WithRunImmediately

```go
func WithRunImmediately() JobOption
```

WithRunImmediately causes the job to run immediately upon registration,
then follow the normal schedule thereafter.

#### func WithRunOnce

```go
func WithRunOnce() JobOption
```

WithRunOnce causes the job to be automatically removed after its first execution.

#### func WithCapacity

```go
func WithCapacity(n int) Option
```

WithCapacity pre-allocates internal data structures for the expected number
of entries. This reduces map rehashing and slice growth during bulk additions,
improving performance when adding many jobs at startup.

Pre-allocates:
- `entryIndex` map with capacity n (O(1) lookup by ID)
- `nameIndex` map with capacity n (O(1) lookup by name)
- `entries` heap slice with capacity n

For applications adding fewer than 100 jobs, the default allocation is sufficient.
Use this option when bulk-loading hundreds or thousands of jobs.

A capacity of 0 or negative has no effect (uses default allocation).

**Example:**
```go
// Expect ~1000 jobs at startup
c := cron.New(cron.WithCapacity(1000))
for _, job := range jobs {
    c.AddFunc(job.Schedule, job.Func)
}
```

---

### Schedule

```go
type Schedule interface {
    Next(time.Time) time.Time
}
```

Schedule describes a job's duty cycle. Implementations must return the next
activation time, later than the given time.

---

### ScheduleWithPrev

```go
type ScheduleWithPrev interface {
    Schedule
    Prev(time.Time) time.Time
}
```

ScheduleWithPrev is an optional interface that schedules can implement to support
backward time traversal. This is useful for detecting missed executions or
determining the last scheduled run time.

Built-in schedules (`SpecSchedule`, `ConstantDelaySchedule`) implement this interface.
Custom Schedule implementations may optionally implement it.

**Usage:**

```go
schedule, _ := cron.ParseStandard("0 9 * * *")

// Type assert to access Prev()
if sp, ok := schedule.(cron.ScheduleWithPrev); ok {
    prev := sp.Prev(time.Now())
    fmt.Println("Last scheduled run:", prev)
}
```

---

### ScheduleParser

```go
type ScheduleParser interface {
    Parse(spec string) (Schedule, error)
}
```

ScheduleParser is an interface for schedule spec parsers.

---

### SpecSchedule

```go
type SpecSchedule struct {
    Second, Minute, Hour, Dom, Month, Dow uint64
    Location *time.Location
}
```

SpecSchedule represents a cron schedule defined by bit fields for each time component.

#### func (*SpecSchedule) Next

```go
func (s *SpecSchedule) Next(t time.Time) time.Time
```

Next returns the next activation time after the given time.
Returns zero time if no valid time exists.

---

### ConstantDelaySchedule

```go
type ConstantDelaySchedule struct {
    Delay time.Duration
}
```

ConstantDelaySchedule represents a simple recurring duty cycle.

#### func Every

```go
func Every(duration time.Duration) ConstantDelaySchedule
```

Every returns a crontab Schedule that activates once every duration.
Delays of less than 1 second are not supported (rounds up to 1 second).

**Example:**
```go
c.Schedule(cron.Every(5*time.Minute), job)
```

#### func (ConstantDelaySchedule) Next

```go
func (s ConstantDelaySchedule) Next(t time.Time) time.Time
```

Next returns the next activation time after t.

---

### Parser

```go
type Parser struct {
    // contains filtered or unexported fields
}
```

Parser parses cron specs into Schedule objects.

#### func NewParser

```go
func NewParser(options ParseOption) Parser
```

NewParser creates a Parser with custom options.

**Example:**
```go
// Quartz-style with seconds
parser := cron.NewParser(
    cron.Second | cron.Minute | cron.Hour |
    cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)
```

#### func (Parser) Parse

```go
func (p Parser) Parse(spec string) (Schedule, error)
```

Parse returns a Schedule from a cron spec or error if invalid.

---

### ParseOption

```go
type ParseOption int

const (
    Second         ParseOption = 1 << iota // Seconds field, required
    SecondOptional                         // Seconds field, optional
    Minute                                  // Minutes field
    Hour                                    // Hours field
    Dom                                     // Day of month field
    Month                                   // Month field
    Dow                                     // Day of week field
    DowOptional                             // Day of week, optional
    Descriptor                              // Enable @hourly, @every, etc.
)
```

ParseOption represents parser configuration flags.

---

### Chain

```go
type Chain struct {
    // contains filtered or unexported fields
}
```

Chain is a sequence of JobWrappers.

#### func NewChain

```go
func NewChain(c ...JobWrapper) Chain
```

NewChain returns a Chain of the given JobWrappers.

#### func (Chain) Then

```go
func (c Chain) Then(j Job) Job
```

Then applies all wrappers to the given job and returns the wrapped job.

---

### JobWrapper

```go
type JobWrapper func(Job) Job
```

JobWrapper is a function that wraps a Job with additional behavior.

All chain wrappers implement `JobWithContext` and propagate the incoming context
to inner jobs that also implement `JobWithContext`. This means per-entry context
flows through the entire wrapper chain to context-aware jobs.

#### func Recover

```go
func Recover(logger Logger, opts ...RecoverOption) JobWrapper
```

Recover catches panics in jobs, logs them, and continues.
Propagates context to context-aware inner jobs.

#### func SkipIfStillRunning

```go
func SkipIfStillRunning(logger Logger) JobWrapper
```

SkipIfStillRunning skips a job invocation if the previous one is still running.
Propagates context to context-aware inner jobs.

#### func DelayIfStillRunning

```go
func DelayIfStillRunning(logger Logger) JobWrapper
```

DelayIfStillRunning delays a job invocation until the previous one completes.
Propagates context to context-aware inner jobs.

#### func Timeout

```go
func Timeout(logger Logger, timeout time.Duration, opts ...TimeoutOption) JobWrapper
```

Timeout wraps a job with a timeout using the abandonment model.
Propagates context to context-aware inner jobs.

**Example:**
```go
c := cron.New(cron.WithChain(
    cron.Timeout(logger, 30*time.Second),
    cron.Recover(logger),
))
```

#### func Jitter

```go
func Jitter(maxJitter time.Duration) JobWrapper
```

Jitter adds a random delay before job execution to prevent thundering herd.
Propagates context to context-aware inner jobs.

#### func JitterWithLogger

```go
func JitterWithLogger(logger Logger, maxJitter time.Duration) JobWrapper
```

JitterWithLogger is like Jitter but logs the applied delay.
Propagates context to context-aware inner jobs.

#### func RetryWithBackoff

```go
func RetryWithBackoff(logger Logger, maxRetries int, initialDelay, maxDelay time.Duration, multiplier float64) JobWrapper
```

RetryWithBackoff wraps a job to retry on panic with exponential backoff.
- `maxRetries`: 0 = no retries, >0 = retry up to N times, -1 = unlimited
- `initialDelay`: First retry delay
- `maxDelay`: Maximum delay cap
- `multiplier`: Delay multiplier per retry (typically 2.0)

Jitter of +/-10% is applied to prevent thundering herd.

**Example:**
```go
c := cron.New(cron.WithChain(
    cron.Recover(logger),
    cron.RetryWithBackoff(logger, 3, time.Second, time.Minute, 2.0),
))
```

#### func RetryOnError

```go
func RetryOnError(logger Logger, maxRetries int, initialDelay, maxDelay time.Duration, multiplier float64) JobWrapper
```

RetryOnError wraps an `ErrorJob` to retry on returned errors with exponential backoff.
Unlike `RetryWithBackoff` which catches panics, this wrapper uses Go-idiomatic error
returns. Jobs must implement `ErrorJob`; regular `Job` implementations are passed through
unchanged.

**Example:**
```go
c := cron.New(cron.WithChain(
    cron.Recover(logger),
    cron.RetryOnError(logger, 3, time.Second, time.Minute, 2.0),
))
c.AddJob("@every 5m", cron.FuncErrorJob(func() error {
    return callAPI()
}))
```

#### func CircuitBreaker

```go
func CircuitBreaker(logger Logger, threshold int, cooldown time.Duration) JobWrapper
```

CircuitBreaker wraps a job to stop execution after consecutive failures.
- **Closed**: Normal execution. Failures increment counter.
- **Open**: Execution skipped for `cooldown` duration after `threshold` failures.
- **Half-Open**: After cooldown, one execution attempted. Success closes, failure reopens.

**Example:**
```go
c := cron.New(cron.WithChain(
    cron.Recover(logger),
    cron.CircuitBreaker(logger, 5, 5*time.Minute),
))
```

#### func TimeoutWithContext

```go
func TimeoutWithContext(logger Logger, timeout time.Duration, opts ...TimeoutOption) JobWrapper
```

TimeoutWithContext wraps a job with a timeout that supports true cancellation.
Unlike `Timeout`, this passes a context with deadline to `JobWithContext` jobs,
allowing cooperative cancellation. A 5-second grace period is allowed after
context cancellation before the goroutine is abandoned.

**Example:**
```go
c := cron.New(cron.WithChain(
    cron.TimeoutWithContext(cron.DefaultLogger, 5*time.Minute),
))
c.AddJob("@every 1h", cron.FuncJobWithContext(func(ctx context.Context) {
    select {
    case <-ctx.Done():
        return // Timeout - clean up
    case <-time.After(1 * time.Minute):
        // Done
    }
}))
```

---

### Job

```go
type Job interface {
    Run()
}
```

Job is an interface for submitted cron jobs.

---

### FuncJob

```go
type FuncJob func()
```

FuncJob is a wrapper that turns a `func()` into a cron.Job.

#### func (FuncJob) Run

```go
func (f FuncJob) Run()
```

Run calls the wrapped function.

---

### JobWithContext

```go
type JobWithContext interface {
    Job
    RunWithContext(ctx context.Context)
}
```

JobWithContext is an optional interface for jobs that support `context.Context`.
If a job implements this interface, `RunWithContext` is called instead of `Run`,
allowing the job to receive cancellation signals, respect deadlines, and access
request-scoped values.

Each entry has its own per-entry context derived from the Cron's base context.
The context is canceled when the entry is removed or its job is replaced.

---

### FuncJobWithContext

```go
type FuncJobWithContext func(ctx context.Context)
```

FuncJobWithContext is a wrapper that turns a `func(context.Context)` into a
`JobWithContext`. This enables context-aware jobs using simple functions.

#### func (FuncJobWithContext) Run

```go
func (f FuncJobWithContext) Run()
```

Run implements Job by calling `RunWithContext(context.Background())`.

#### func (FuncJobWithContext) RunWithContext

```go
func (f FuncJobWithContext) RunWithContext(ctx context.Context)
```

RunWithContext implements JobWithContext.

**Example:**
```go
c.AddJob("@every 1m", cron.FuncJobWithContext(func(ctx context.Context) {
    for i := 0; i < 6; i++ {
        if ctx.Err() != nil {
            return // Entry removed or Stop() called
        }
        // Do a chunk of work...
        time.Sleep(5 * time.Second)
    }
}))
```

---

### ErrorJob

```go
type ErrorJob interface {
    Job
    RunE() error
}
```

ErrorJob is an optional interface for jobs that return errors instead of panicking.
Used by `RetryOnError` for Go-idiomatic error-based retry.

---

### FuncErrorJob

```go
type FuncErrorJob func() error
```

FuncErrorJob is a wrapper that turns a `func() error` into an `ErrorJob`.

#### func (FuncErrorJob) Run

```go
func (f FuncErrorJob) Run()
```

Run implements Job by calling `RunE()` and panicking on error.

#### func (FuncErrorJob) RunE

```go
func (f FuncErrorJob) RunE() error
```

RunE implements ErrorJob.

**Example:**
```go
c.AddJob("@every 5m", cron.FuncErrorJob(func() error {
    return callExternalAPI()
}))
```

---

### NamedJob

```go
type NamedJob interface {
    Job
    Name() string
}
```

NamedJob is an optional interface for jobs that provide a name for observability.
If implemented, the name is passed to `ObservabilityHooks` callbacks.

---

### ObservabilityHooks

```go
type ObservabilityHooks struct {
    OnJobStart    func(entryID EntryID, name string, scheduledTime time.Time)
    OnJobComplete func(entryID EntryID, name string, duration time.Duration, recovered any)
    OnSchedule    func(entryID EntryID, name string, nextRun time.Time)
}
```

ObservabilityHooks provides callbacks for monitoring cron operations.
All callbacks are optional. Hooks are called asynchronously in separate goroutines
to prevent slow callbacks from blocking the scheduler.

**Example with Prometheus:**
```go
hooks := cron.ObservabilityHooks{
    OnJobStart: func(id cron.EntryID, name string, scheduled time.Time) {
        jobsStarted.WithLabelValues(name).Inc()
    },
    OnJobComplete: func(id cron.EntryID, name string, dur time.Duration, recovered any) {
        jobDuration.WithLabelValues(name).Observe(dur.Seconds())
        if recovered != nil {
            jobPanics.WithLabelValues(name).Inc()
        }
    },
}
c := cron.New(cron.WithObservability(hooks))
```

---

### SpecAnalysis

```go
type SpecAnalysis struct {
    Valid        bool
    Error        error
    NextRun      time.Time
    Location     *time.Location
    Fields       map[string]string
    IsDescriptor bool
    Interval     time.Duration
    Schedule     Schedule
    Warnings     []string
}
```

SpecAnalysis contains detailed information about a parsed cron specification.
Returned by `AnalyzeSpec`.

---

### ValidationError

```go
type ValidationError struct {
    Message string
    Field   string
    Value   string
}
```

ValidationError represents a cron expression validation error with optional
field and value context.

---

### PanicError

```go
type PanicError struct {
    Value any
    Stack []byte
}
```

PanicError wraps a panic value with the stack trace at the point of panic.
Implements `error` and `Unwrap()`. Used by `RetryWithBackoff` and `safeExecute`.

---

### Logger

```go
type Logger interface {
    Info(msg string, keysAndValues ...interface{})
    Error(err error, msg string, keysAndValues ...interface{})
}
```

Logger is the logging interface used by cron. Compatible with go-logr/logr.

#### Variables

```go
var DefaultLogger Logger = PrintfLogger(log.New(os.Stdout, "cron: ", log.LstdFlags))
var DiscardLogger Logger = PrintfLogger(log.New(io.Discard, "", 0))
```

#### func PrintfLogger

```go
func PrintfLogger(l *log.Logger) Logger
```

PrintfLogger wraps a `*log.Logger` in the cron Logger interface.

#### func VerbosePrintfLogger

```go
func VerbosePrintfLogger(l *log.Logger) Logger
```

VerbosePrintfLogger wraps a `*log.Logger` with verbose output enabled.

#### func NewSlogLogger

```go
func NewSlogLogger(l *slog.Logger) *SlogLogger
```

NewSlogLogger creates a Logger that delegates to `*slog.Logger`.

---

### Clock

```go
type Clock interface {
    Now() time.Time
    NewTimer(d time.Duration) Timer
}
```

Clock provides an abstraction over time operations, enabling deterministic testing.

---

### Timer

```go
type Timer interface {
    C() <-chan time.Time  // Channel that receives fire time
    Stop() bool           // Stop the timer
    Reset(d time.Duration) bool  // Reset to new duration
}
```

Timer abstracts `time.Timer` for testability.

---

### RealClock

```go
type RealClock struct{}
```

RealClock implements Clock using the real system time.

#### func (RealClock) Now

```go
func (RealClock) Now() time.Time
```

Now returns the current system time.

#### func (RealClock) NewTimer

```go
func (RealClock) NewTimer(d time.Duration) Timer
```

NewTimer creates a real `time.Timer`.

---

### FakeClock

```go
type FakeClock struct {
    // contains filtered or unexported fields
}
```

FakeClock is a Clock implementation for testing with controlled time.

#### func NewFakeClock

```go
func NewFakeClock(t time.Time) *FakeClock
```

NewFakeClock creates a FakeClock initialized to the given time.

#### func (*FakeClock) Now

```go
func (f *FakeClock) Now() time.Time
```

Now returns the fake clock's current time.

#### func (*FakeClock) NewTimer

```go
func (f *FakeClock) NewTimer(d time.Duration) Timer
```

NewTimer creates a fake timer that fires when the clock advances past its target.

#### func (*FakeClock) Set

```go
func (f *FakeClock) Set(t time.Time)
```

Set updates the fake clock to a specific time, firing any expired timers.

#### func (*FakeClock) Advance

```go
func (f *FakeClock) Advance(d time.Duration)
```

Advance moves the fake clock forward by the given duration, firing expired timers.

#### func (*FakeClock) BlockUntil

```go
func (f *FakeClock) BlockUntil(n int)
```

BlockUntil blocks until at least n timers are registered with the clock.
Useful for synchronizing test setup with scheduler startup.

#### func (*FakeClock) TimerCount

```go
func (f *FakeClock) TimerCount() int
```

TimerCount returns the number of active timers.

---

## Functions

### ParseStandard

```go
func ParseStandard(spec string) (Schedule, error)
```

ParseStandard returns a Schedule for the standard 5-field cron spec.

**Example:**
```go
schedule, err := cron.ParseStandard("0 6 * * ?")
if err != nil {
    log.Fatal(err)
}
next := schedule.Next(time.Now())
```

### Every

```go
func Every(duration time.Duration) ConstantDelaySchedule
```

Every returns a crontab Schedule that activates once every duration.
Delays of less than 1 second are rounded up to 1 second.

### ValidateSpec

```go
func ValidateSpec(spec string, options ...ParseOption) error
```

ValidateSpec validates a cron expression without scheduling a job.
Returns nil if valid. Uses the standard parser by default; pass `ParseOption`
flags to customize validation (e.g., to require a seconds field).

**Example:**
```go
if err := cron.ValidateSpec("0 9 * * MON-FRI"); err != nil {
    return fmt.Errorf("invalid: %w", err)
}
```

### ValidateSpecWith

```go
func ValidateSpecWith(spec string, parser ScheduleParser) error
```

ValidateSpecWith validates a cron expression using any `ScheduleParser` implementation.
Useful with custom parsers or pre-configured `Parser` instances.

### ValidateSpecs

```go
func ValidateSpecs(specs []string, options ...ParseOption) map[int]error
```

ValidateSpecs validates multiple cron expressions at once.
Returns a map of index to error for any invalid specs.

**Example:**
```go
specs := []string{"* * * * *", "invalid", "0 9 * * MON-FRI"}
errs := cron.ValidateSpecs(specs)
for idx, err := range errs {
    log.Printf("Spec %d invalid: %v", idx, err)
}
```

### AnalyzeSpec

```go
func AnalyzeSpec(spec string, options ...ParseOption) SpecAnalysis
```

AnalyzeSpec provides detailed analysis of a cron expression including validation
status, next run time, parsed fields, timezone, and warnings.

**Example:**
```go
result := cron.AnalyzeSpec("0 9 * * MON-FRI")
if result.Valid {
    fmt.Println("Next run:", result.NextRun)
    fmt.Println("Fields:", result.Fields)
    fmt.Println("Warnings:", result.Warnings)
}
```

### AnalyzeSpecWithHash

```go
func AnalyzeSpecWithHash(spec string, options ParseOption, hashSeed string) SpecAnalysis
```

AnalyzeSpecWithHash analyzes a cron expression containing H hash expressions.
The seed (e.g., job name) produces deterministic, distributed scheduling times.

---

## Constants

### MaxSpecLength

```go
const MaxSpecLength = 1024
```

MaxSpecLength is the maximum allowed length for a cron spec string.

---

## Errors

### ErrEntryNotFound

```go
var ErrEntryNotFound = errors.New("cron: entry not found")
```

Returned by update methods (`UpdateSchedule`, `UpdateEntry`, `UpsertJob`, etc.)
when the specified entry does not exist.

### ErrNilJob

```go
var ErrNilJob = errors.New("cron: job must not be nil; use UpdateSchedule to update only the schedule")
```

Returned by `UpdateEntry`, `UpdateEntryByName`, `UpdateEntryJob`, and
`UpdateEntryJobByName` when a nil job is passed.

### ErrNameRequired

```go
var ErrNameRequired = errors.New("cron: UpsertJob requires WithName option")
```

Returned by `UpsertJob` when no `WithName` option is provided.

### ErrMaxEntriesReached

```go
var ErrMaxEntriesReached = errors.New("cron: max entries limit reached")
```

Returned by `AddFunc`, `AddJob`, and `ScheduleJob` when the `WithMaxEntries` limit
has been reached.

### ErrDuplicateName

```go
var ErrDuplicateName = errors.New("cron: duplicate entry name")
```

Returned when adding an entry with a name that already exists.

### ErrEmptySpec

```go
var ErrEmptySpec = &ValidationError{Message: "empty spec string"}
```

Returned by `AnalyzeSpec` when an empty spec string is provided.

---

*Generated: 2026-02-12*
