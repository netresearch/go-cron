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
    ID         EntryID   // Cron-assigned unique identifier
    Schedule   Schedule  // Schedule for this entry
    Next       time.Time // Next activation time (zero if not started)
    Prev       time.Time // Last run time (zero if never run)
    WrappedJob Job       // Job with chain wrappers applied
    Job        Job       // Original job as submitted
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

// For backward compatibility with function-based clock
c := cron.New(cron.WithClock(cron.ClockFunc(time.Now)))
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
var DefaultLogger Logger = PrintfLogger(log.New(os.Stderr, "cron: ", log.LstdFlags))
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

### ClockFunc

```go
func ClockFunc(fn func() time.Time) Clock
```

ClockFunc creates a Clock from a simple `func() time.Time`.
Provides backward compatibility with the old Clock type.
Timers created by this clock use real `time.Timer`.

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
const MaxSpecLength = 256
```

MaxSpecLength is the maximum allowed length for a cron spec string.

---

*Generated: 2024-11-28*
