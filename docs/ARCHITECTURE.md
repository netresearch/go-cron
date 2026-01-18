# go-cron Architecture

> Design patterns, internal structures, and implementation details

## Table of Contents

- [Overview](#overview)
- [Component Architecture](#component-architecture)
- [Data Structures](#data-structures)
- [Scheduling Algorithm](#scheduling-algorithm)
- [Design Patterns](#design-patterns)
- [Concurrency Model](#concurrency-model)
- [Testing Architecture](#testing-architecture)
- [Extension Points](#extension-points)

---

## Overview

go-cron is designed around these core principles:

1. **Efficiency**: Min-heap for O(log n) scheduling operations
2. **Testability**: Clock abstraction for deterministic tests
3. **Extensibility**: Interfaces and functional options
4. **Safety**: Panic recovery, proper synchronization

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                           User Code                                 │
├─────────────────────────────────────────────────────────────────────┤
│  AddFunc() / AddJob() / Schedule() / Remove() / Start() / Stop()   │
└───────────────────────────────┬─────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────────┐
│                         Cron Scheduler                              │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │                    Configuration Layer                        │  │
│  │  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐│  │
│  │  │Location │ │ Parser  │ │  Chain  │ │ Logger  │ │  Clock  ││  │
│  │  └─────────┘ └─────────┘ └─────────┘ └─────────┘ └─────────┘│  │
│  └──────────────────────────────────────────────────────────────┘  │
│                                                                     │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │                     Entry Management                          │  │
│  │  ┌───────────────────────────────────────────────────────┐   │  │
│  │  │                   Entry Min-Heap                       │   │  │
│  │  │  ┌───────┐ ┌───────┐ ┌───────┐         ┌───────┐     │   │  │
│  │  │  │Entry 1│ │Entry 2│ │Entry 3│   ...   │Entry N│     │   │  │
│  │  │  └───────┘ └───────┘ └───────┘         └───────┘     │   │  │
│  │  └───────────────────────────────────────────────────────┘   │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                                                                     │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │                      Run Loop                                 │  │
│  │  Timer ──► Wake ──► Execute Due Jobs ──► Reschedule ──► Sleep│  │
│  └──────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────────┐
│                        Job Execution                                │
│  ┌────────────────────────────────────────────────────────────────┐│
│  │  Chain.Then() ──► Wrapper ──► Wrapper ──► ... ──► Job.Run()   ││
│  │  (Recover)       (Skip)      (Delay)            (User Code)   ││
│  └────────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────────┘
```

---

## Component Architecture

### 1. Scheduler Core (`cron.go`)

The `Cron` struct is the central coordinator:

```go
type Cron struct {
    entries   entryHeap          // Min-heap of entries
    chain     Chain              // Job wrappers
    stop      chan struct{}      // Stop signal
    add       chan *Entry        // Add entry (while running)
    remove    chan EntryID       // Remove entry (while running)
    snapshot  chan chan []Entry  // Request entry snapshot
    running   bool               // Running state
    logger    Logger             // Logging
    runningMu sync.Mutex         // Protects running state
    location  *time.Location     // Default timezone
    parser    ScheduleParser     // Cron expression parser
    nextID    EntryID            // Next entry ID (monotonic)
    jobWaiter sync.WaitGroup     // Tracks running jobs
    clock     Clock              // Time source
}
```

**Responsibilities:**
- Entry lifecycle management (add/remove)
- Run loop coordination
- Time/timezone handling
- Graceful shutdown

### 2. Parser (`parser.go`)

Converts cron expressions into `Schedule` objects:

```
Input: "*/15 9-17 * * MON-FRI"
                │
                ▼
        ┌───────────────┐
        │    Parser     │
        │  ┌─────────┐  │
        │  │ Options │──┼──► Second | Minute | Hour | Dom | Month | Dow
        │  └─────────┘  │
        └───────┬───────┘
                │
                ▼
        ┌───────────────┐
        │ SpecSchedule  │
        │  Second: 0x1  │
        │  Minute: 0x... │──► Bit fields for each time component
        │  Hour: 0x...  │
        │  ...          │
        └───────────────┘
```

**Key Features:**
- Configurable field support (5-field standard, 6-field with seconds)
- Named values (JAN-DEC, SUN-SAT)
- Special characters (*, /, ,, -, ?)
- Predefined descriptors (@hourly, @daily, @every)
- Per-schedule timezone (CRON_TZ=)

### 3. Schedule (`spec.go`)

The `SpecSchedule` uses bit fields for efficient time matching:

```go
type SpecSchedule struct {
    Second, Minute, Hour, Dom, Month, Dow uint64
    Location *time.Location
}
```

**Bit Field Layout:**
```
Second/Minute: bits 0-59 represent each second/minute
Hour:          bits 0-23 represent each hour
Dom:           bits 1-31 represent each day (bit 0 unused)
Month:         bits 1-12 represent each month
Dow:           bits 0-6 represent each day of week
```

**Next() Algorithm:**
1. Start from given time + 1 second
2. Find next matching month (wrap to next year if needed)
3. Find next matching day (wrap to next month if needed)
4. Find next matching hour (wrap to next day if needed)
5. Find next matching minute (wrap to next hour if needed)
6. Find next matching second (wrap to next minute if needed)

### 4. Clock (`clock.go`)

Abstraction for time operations:

```
┌─────────────────────────────────────────────────────────┐
│                    Clock Interface                      │
│  Now() time.Time                                        │
│  NewTimer(d time.Duration) Timer                        │
└─────────────────────────────────────────────────────────┘
            │                           │
            ▼                           ▼
┌─────────────────────┐     ┌─────────────────────────────┐
│     RealClock       │     │        FakeClock            │
│  (Production)       │     │  (Testing)                  │
│                     │     │                             │
│  Now() → time.Now() │     │  now      time.Time         │
│  NewTimer() →       │     │  timers   timerHeap         │
│    time.NewTimer()  │     │  waiters  []chan struct{}   │
└─────────────────────┘     │                             │
                            │  Advance(d) → fire timers   │
                            │  Set(t) → jump to time      │
                            │  BlockUntil(n) → sync       │
                            └─────────────────────────────┘
```

### 5. Chain (`chain.go`)

Middleware pattern for job wrapping:

```
┌─────────────────────────────────────────────────────────┐
│                   Chain.Then(job)                       │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│  Recover(logger)                                        │
│  ┌───────────────────────────────────────────────────┐ │
│  │  defer func() { recover(); log }                  │ │
│  │  ┌─────────────────────────────────────────────┐  │ │
│  │  │  SkipIfStillRunning(logger)                 │  │ │
│  │  │  ┌───────────────────────────────────────┐  │  │ │
│  │  │  │  if running { skip } else { run }     │  │  │ │
│  │  │  │  ┌─────────────────────────────────┐  │  │  │ │
│  │  │  │  │  Timeout(30s)                   │  │  │  │ │
│  │  │  │  │  ┌───────────────────────────┐  │  │  │  │ │
│  │  │  │  │  │     Original Job          │  │  │  │  │ │
│  │  │  │  │  │      job.Run()            │  │  │  │  │ │
│  │  │  │  │  └───────────────────────────┘  │  │  │  │ │
│  │  │  │  └─────────────────────────────────┘  │  │  │ │
│  │  │  └───────────────────────────────────────┘  │  │ │
│  │  └─────────────────────────────────────────────┘  │ │
│  └───────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────┘
```

### 6. Heap (`heap.go`)

Min-heap implementation for entry scheduling:

```go
type entryHeap []*Entry

// heap.Interface implementation
func (h entryHeap) Len() int
func (h entryHeap) Less(i, j int) bool  // Compare by Next time
func (h entryHeap) Swap(i, j int)
func (h *entryHeap) Push(x interface{})
func (h *entryHeap) Pop() interface{}

// Additional operations
func (h entryHeap) Peek() *Entry        // O(1) - get next entry
func (h *entryHeap) Update(e *Entry)    // O(log n) - reheapify after change
func (h *entryHeap) Remove(id EntryID)  // O(n) find + O(log n) remove
```

**Complexity:**
| Operation | Time |
|-----------|------|
| Peek (next entry) | O(1) |
| Push (add entry) | O(log n) |
| Pop (remove next) | O(log n) |
| Update (reschedule) | O(log n) |
| Remove by ID | O(n) |

---

## Data Structures

### Entry

```go
type Entry struct {
    ID         EntryID   // Unique identifier (monotonic uint64)
    Schedule   Schedule  // When to run
    Next       time.Time // Next scheduled time
    Prev       time.Time // Last run time
    WrappedJob Job       // Job with chain applied
    Job        Job       // Original job
    heapIndex  int       // Position in heap (for efficient updates)
}
```

### Min-Heap Invariant

```
                    Entry(Next: 10:00)
                   /                  \
         Entry(Next: 10:15)    Entry(Next: 10:30)
         /            \
Entry(Next: 10:45)  Entry(Next: 11:00)
```

Parent's `Next` time is always <= children's `Next` times.

---

## Scheduling Algorithm

### Run Loop

```go
func (c *Cron) run() {
    // 1. Initialize: calculate Next times, build heap
    now := c.now()
    for _, entry := range c.entries {
        entry.Next = entry.Schedule.Next(now)
    }
    heap.Init(&c.entries)

    for {
        // 2. Get next entry to run (O(1) peek)
        next := c.entries.Peek()

        // 3. Sleep until next entry or event
        var timer Timer
        if next == nil || next.Next.IsZero() {
            timer = c.clock.NewTimer(100000 * time.Hour)  // Long sleep
        } else {
            timer = c.clock.NewTimer(next.Next.Sub(now))
        }

        select {
        case now = <-timer.C():
            // 4. Handle backward time jump (NTP, VM restore)
            for _, e := range c.entries {
                if e.Prev.After(now) {
                    e.Next = e.Schedule.Next(now)
                    c.entries.Update(e)
                }
            }

            // 5. Execute all due entries
            for c.entries.Peek() != nil {
                e := c.entries.Peek()
                if e.Next.After(now) || e.Next.IsZero() {
                    break
                }
                c.startJob(e.WrappedJob)  // Async goroutine
                e.Prev = e.Next
                e.Next = e.Schedule.Next(now)
                c.entries.Update(e)  // Reheapify
            }

        case newEntry := <-c.add:
            timer.Stop()
            newEntry.Next = newEntry.Schedule.Next(now)
            heap.Push(&c.entries, newEntry)

        case id := <-c.remove:
            timer.Stop()
            c.entries.Remove(id)

        case replyChan := <-c.snapshot:
            replyChan <- c.entrySnapshot()

        case <-c.stop:
            timer.Stop()
            return
        }
    }
}
```

### Time Zone Handling

```
User Input: "CRON_TZ=Asia/Tokyo 0 9 * * *"
                        │
                        ▼
              ┌─────────────────┐
              │  Parser extracts │
              │  TZ prefix       │
              └────────┬────────┘
                       │
                       ▼
              ┌─────────────────┐
              │  SpecSchedule   │
              │  Location:      │
              │  Asia/Tokyo     │
              └────────┬────────┘
                       │
                       ▼
              ┌─────────────────┐
              │  Next() uses    │
              │  Location for   │
              │  time calc      │
              └─────────────────┘
```

### DST Handling

Jobs scheduled during DST leap-ahead are run immediately after the skipped hour (ISC cron-compatible behavior).

---

## Design Patterns

### 1. Functional Options

```go
// Option type
type Option func(*Cron)

// Option constructors
func WithLocation(loc *time.Location) Option {
    return func(c *Cron) { c.location = loc }
}

// Usage
c := cron.New(
    cron.WithLocation(time.UTC),
    cron.WithSeconds(),
    cron.WithChain(cron.Recover(logger)),
)
```

**Benefits:**
- Optional configuration with sensible defaults
- Backward compatible (new options don't break existing code)
- Self-documenting API

### 2. Interface Segregation

```go
// Small, focused interfaces
type Job interface { Run() }
type Schedule interface { Next(time.Time) time.Time }
type Logger interface {
    Info(msg string, keysAndValues ...interface{})
    Error(err error, msg string, keysAndValues ...interface{})
}
type Clock interface {
    Now() time.Time
    NewTimer(d time.Duration) Timer
}
```

### 3. Middleware/Chain Pattern

```go
type JobWrapper func(Job) Job

// Composable wrappers
chain := cron.NewChain(
    cron.Recover(logger),
    cron.SkipIfStillRunning(logger),
)
wrappedJob := chain.Then(originalJob)
```

### 4. Strategy Pattern

Multiple `Schedule` implementations:
- `SpecSchedule`: Cron expression
- `ConstantDelaySchedule`: Fixed interval

---

## Concurrency Model

### Thread Safety

```
┌─────────────────────────────────────────────────────────────────┐
│                      Main Goroutine                             │
│  c.Add(), c.Remove(), c.Entries(), c.Start(), c.Stop()          │
│                                                                 │
│  runningMu.Lock() protects:                                     │
│  - c.running state                                              │
│  - c.entries (when not running)                                 │
│  - c.nextID                                                     │
└─────────────────────────────────────────────────────────────────┘
                              │
              ┌───────────────┼───────────────┐
              │               │               │
              ▼               ▼               ▼
       ┌──────────┐    ┌──────────┐    ┌──────────┐
       │ c.add    │    │ c.remove │    │ c.stop   │
       │ channel  │    │ channel  │    │ channel  │
       └────┬─────┘    └────┬─────┘    └────┬─────┘
            │               │               │
            └───────────────┼───────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Scheduler Goroutine                          │
│  c.run() - single goroutine owns entries while running          │
│                                                                 │
│  select {                                                       │
│      case <-timer.C(): // execute jobs                          │
│      case entry := <-c.add: // add entry                        │
│      case id := <-c.remove: // remove entry                     │
│      case reply := <-c.snapshot: // return copy                 │
│      case <-c.stop: // shutdown                                 │
│  }                                                              │
└─────────────────────────────────────────────────────────────────┘
                            │
              ┌─────────────┼─────────────┐
              ▼             ▼             ▼
       ┌──────────┐  ┌──────────┐  ┌──────────┐
       │  Job 1   │  │  Job 2   │  │  Job N   │
       │goroutine │  │goroutine │  │goroutine │
       └──────────┘  └──────────┘  └──────────┘
              │             │             │
              └─────────────┼─────────────┘
                            │
                            ▼
                    ┌──────────────┐
                    │ jobWaiter    │
                    │ WaitGroup    │
                    └──────────────┘
```

### Shutdown Sequence

```go
ctx := c.Stop()        // 1. Send stop signal
<-ctx.Done()           // 2. Wait for jobs to complete
// c.jobWaiter.Wait() is called internally
```

---

## Testing Architecture

### FakeClock Pattern

```go
func TestScheduledJob(t *testing.T) {
    // 1. Create FakeClock at known time
    start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
    fakeClock := cron.NewFakeClock(start)

    // 2. Create cron with FakeClock
    c := cron.New(cron.WithClock(fakeClock))

    // 3. Add jobs
    var called atomic.Bool
    c.AddFunc("@every 1h", func() { called.Store(true) })
    c.Start()
    defer c.Stop()

    // 4. Wait for scheduler to register timer
    fakeClock.BlockUntil(1)

    // 5. Advance time deterministically
    fakeClock.Advance(time.Hour)
    time.Sleep(10 * time.Millisecond)  // Let goroutine run

    // 6. Assert
    if !called.Load() {
        t.Error("job should have been called")
    }
}
```

### FakeClock Internal Structure

```go
type FakeClock struct {
    mu      sync.Mutex
    now     time.Time
    timers  timerHeap         // Min-heap of fake timers
    waiters []chan struct{}   // BlockUntil waiters
}

type fakeTimer struct {
    clock    *FakeClock
    target   time.Time         // When to fire
    c        chan time.Time    // Fire channel
    stopped  bool
    heapIdx  int               // Position in heap
}
```

---

## Extension Points

### Custom Schedule

```go
type MySchedule struct{}

func (s MySchedule) Next(t time.Time) time.Time {
    // Custom logic
    return t.Add(time.Hour)
}

c.Schedule(MySchedule{}, myJob)
```

### Custom Parser

```go
type MyParser struct{}

func (p MyParser) Parse(spec string) (cron.Schedule, error) {
    // Custom parsing
    return MySchedule{}, nil
}

c := cron.New(cron.WithParser(MyParser{}))
```

### Custom Logger

```go
type MyLogger struct{}

func (l MyLogger) Info(msg string, keysAndValues ...interface{}) {
    // Custom logging
}

func (l MyLogger) Error(err error, msg string, keysAndValues ...interface{}) {
    // Custom error logging
}

c := cron.New(cron.WithLogger(MyLogger{}))
```

### Custom Job Wrapper

```go
func MetricsWrapper(m *metrics.Registry) cron.JobWrapper {
    return func(j cron.Job) cron.Job {
        return cron.FuncJob(func() {
            start := time.Now()
            j.Run()
            m.RecordDuration("cron.job.duration", time.Since(start))
        })
    }
}

c := cron.New(cron.WithChain(MetricsWrapper(registry)))
```

---

*Generated: 2025-11-28*
