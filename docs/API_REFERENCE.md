# go-cron API Reference

> Complete API documentation for all exported types, functions, and methods

## Table of Contents

- [Package cron](#package-cron)
- [Types](#types)
  - [Cron](#cron)
  - [Entry](#entry)
  - [EntryID](#entryid)
  - [Option](#option)
  - [Schedule](#schedule)
  - [SpecSchedule](#specschedule)
  - [ConstantDelaySchedule](#constantdelayschedule)
  - [Parser](#parser)
  - [ParseOption](#parseoption)
  - [Chain](#chain)
  - [JobWrapper](#jobwrapper)
  - [Job](#job)
  - [FuncJob](#funcjob)
  - [Logger](#logger)
  - [Clock](#clock)
  - [Timer](#timer)
  - [FakeClock](#fakeclock)
  - [RealClock](#realclock)
- [Functions](#functions)
- [Constants](#constants)

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

#### func (*Cron) Remove

```go
func (c *Cron) Remove(id EntryID)
```

Remove an entry from being run in the future.

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

#### func Recover

```go
func Recover(logger Logger) JobWrapper
```

Recover catches panics in jobs, logs them, and continues.

#### func SkipIfStillRunning

```go
func SkipIfStillRunning(logger Logger) JobWrapper
```

SkipIfStillRunning skips a job invocation if the previous one is still running.

#### func DelayIfStillRunning

```go
func DelayIfStillRunning(logger Logger) JobWrapper
```

DelayIfStillRunning delays a job invocation until the previous one completes.

#### func Timeout

```go
func Timeout(timeout time.Duration) JobWrapper
```

Timeout cancels a job's context after the given duration.
Requires jobs to check `ctx.Done()` for cancellation.

**Example:**
```go
c := cron.New(cron.WithChain(
    cron.Timeout(30*time.Second),
    cron.Recover(logger),
))
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

`ErrEntryNotFound` is returned by `UpdateSchedule`, `UpdateJob`, `UpdateScheduleByName`, and `UpdateJobByName` when the requested entry is not found.

---

*Generated: 2025-11-28*
