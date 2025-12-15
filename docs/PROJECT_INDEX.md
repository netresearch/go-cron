# go-cron Project Index

> Comprehensive documentation for the go-cron scheduler library

## Project Overview

| Attribute | Value |
|-----------|-------|
| **Module** | `github.com/netresearch/go-cron` |
| **Go Version** | 1.25+ |
| **Total Lines** | ~5,870 |
| **Test Coverage** | Comprehensive (unit, fuzz, benchmark, integration) |
| **License** | MIT |

### Description

A robust cron spec parser and job runner for Go. Features include:
- Standard 5-field cron expressions (minute, hour, dom, month, dow)
- Optional seconds field support
- Predefined schedules (`@hourly`, `@daily`, `@weekly`, `@monthly`, `@yearly`)
- Interval schedules (`@every 1h30m`)
- Per-job timezone support (`CRON_TZ=Asia/Tokyo`)
- Job wrappers (recover, skip-if-running, delay-if-running, timeout)
- Min-heap scheduler for O(log n) insertion/removal
- Deterministic testing via `FakeClock`

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         Cron Scheduler                          │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐             │
│  │   Parser    │  │   Clock     │  │   Logger    │             │
│  │  (spec→     │  │ (time ctrl) │  │ (observ.)   │             │
│  │  schedule)  │  │             │  │             │             │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘             │
│         │                │                │                     │
│         ▼                ▼                ▼                     │
│  ┌─────────────────────────────────────────────────────────────┤
│  │                    Entry Min-Heap                           │
│  │  ┌─────────┐ ┌─────────┐ ┌─────────┐                       │
│  │  │ Entry 1 │ │ Entry 2 │ │ Entry N │  (sorted by Next)     │
│  │  │ ID, Job │ │ ID, Job │ │ ID, Job │                       │
│  │  │Schedule │ │Schedule │ │Schedule │                       │
│  │  └─────────┘ └─────────┘ └─────────┘                       │
│  └─────────────────────────────────────────────────────────────┤
│         │                                                       │
│         ▼                                                       │
│  ┌─────────────────────────────────────────────────────────────┤
│  │                    Job Execution                            │
│  │  Chain → Wrapper → Wrapper → ... → Job.Run()               │
│  └─────────────────────────────────────────────────────────────┤
└─────────────────────────────────────────────────────────────────┘
```

### Core Components

| Component | File | Purpose |
|-----------|------|---------|
| **Scheduler** | `cron.go` | Main scheduler, entry management, run loop |
| **Parser** | `parser.go` | Cron expression parsing with configurable fields |
| **Schedule** | `spec.go` | Schedule implementation with `Next()` calculation |
| **Clock** | `clock.go` | Time abstraction for production/testing |
| **Chain** | `chain.go` | Job wrapper middleware (recover, skip, delay, timeout) |
| **Heap** | `heap.go` | Min-heap for O(log n) entry scheduling |
| **Logger** | `logger.go` | Structured logging interface |
| **Options** | `option.go` | Functional options pattern |

---

## File Structure

```
go-cron/
├── Core Implementation
│   ├── cron.go           (390 LOC)  # Main scheduler
│   ├── clock.go          (291 LOC)  # Clock/Timer interfaces
│   ├── parser.go         (499 LOC)  # Expression parser
│   ├── spec.go           (203 LOC)  # Schedule implementation
│   ├── heap.go           (79 LOC)   # Entry min-heap
│   ├── chain.go          (144 LOC)  # Job wrappers
│   ├── logger.go         (110 LOC)  # Logging
│   ├── option.go         (68 LOC)   # Configuration options
│   ├── constantdelay.go  (27 LOC)   # @every schedules
│   └── doc.go            (231 LOC)  # Package documentation
│
├── Tests
│   ├── cron_test.go      (1095 LOC) # Scheduler tests
│   ├── clock_test.go     (375 LOC)  # Clock tests
│   ├── parser_test.go    (589 LOC)  # Parser tests
│   ├── spec_test.go      (301 LOC)  # Schedule tests
│   ├── chain_test.go     (356 LOC)  # Wrapper tests
│   ├── heap_test.go      (283 LOC)  # Heap tests
│   ├── example_test.go   (283 LOC)  # Runnable examples
│   ├── fuzz_test.go      (174 LOC)  # Fuzz tests
│   ├── benchmark_test.go (158 LOC)  # Benchmarks
│   └── *_test.go         (various)  # Other tests
│
├── Configuration
│   ├── go.mod                       # Module definition
│   ├── .golangci.yml                # Linter config
│   ├── Makefile                     # Build commands
│   └── lefthook.yml                 # Git hooks
│
├── CI/CD
│   └── .github/workflows/
│       ├── ci.yml                   # Tests, lint, coverage
│       └── release.yml              # Automated releases
│
└── Documentation
    ├── README.md                    # User guide
    ├── CHANGELOG.md                 # Version history
    ├── CONTRIBUTING.md              # Contribution guide
    ├── CODE_OF_CONDUCT.md           # Community standards
    ├── SECURITY.md                  # Security policy
    ├── AGENTS.md                    # AI agent guidelines
    └── claudedocs/                  # Generated docs
```

---

## Public API Reference

### Core Types

#### `Cron` (struct)
Main scheduler that manages entries and runs jobs.

```go
type Cron struct { /* private fields */ }
```

**Methods:**
| Method | Signature | Description |
|--------|-----------|-------------|
| `New` | `func New(opts ...Option) *Cron` | Create new scheduler |
| `AddFunc` | `func (c *Cron) AddFunc(spec string, cmd func()) (EntryID, error)` | Add function job |
| `AddJob` | `func (c *Cron) AddJob(spec string, cmd Job) (EntryID, error)` | Add Job interface |
| `Schedule` | `func (c *Cron) Schedule(schedule Schedule, cmd Job) EntryID` | Add with custom schedule |
| `Start` | `func (c *Cron) Start()` | Start scheduler (async) |
| `Run` | `func (c *Cron) Run()` | Run scheduler (blocking) |
| `Stop` | `func (c *Cron) Stop() context.Context` | Stop scheduler |
| `Entries` | `func (c *Cron) Entries() []Entry` | Get entry snapshot |
| `Entry` | `func (c *Cron) Entry(id EntryID) Entry` | Get specific entry |
| `Remove` | `func (c *Cron) Remove(id EntryID)` | Remove entry |
| `Location` | `func (c *Cron) Location() *time.Location` | Get timezone |

#### `Entry` (struct)
Represents a scheduled job.

```go
type Entry struct {
    ID         EntryID
    Schedule   Schedule
    Next       time.Time
    Prev       time.Time
    WrappedJob Job
    Job        Job
}
```

**Methods:**
- `Valid() bool` - Returns true if not zero entry
- `Run()` - Execute job through chain wrappers

#### `EntryID` (type)
```go
type EntryID uint64
```

### Interfaces

#### `Job`
```go
type Job interface {
    Run()
}
```

#### `Schedule`
```go
type Schedule interface {
    Next(time.Time) time.Time
}
```

#### `ScheduleWithPrev`
```go
type ScheduleWithPrev interface {
    Schedule
    Prev(time.Time) time.Time
}
```

Optional interface for backward time traversal. Built-in schedules implement this.

#### `ScheduleParser`
```go
type ScheduleParser interface {
    Parse(spec string) (Schedule, error)
}
```

#### `Logger`
```go
type Logger interface {
    Info(msg string, keysAndValues ...interface{})
    Error(err error, msg string, keysAndValues ...interface{})
}
```

#### `Clock`
```go
type Clock interface {
    Now() time.Time
    NewTimer(d time.Duration) Timer
}
```

#### `Timer`
```go
type Timer interface {
    C() <-chan time.Time
    Stop() bool
    Reset(d time.Duration) bool
}
```

### Options (Functional Pattern)

| Function | Description |
|----------|-------------|
| `WithLocation(loc *time.Location)` | Set timezone |
| `WithSeconds()` | Enable seconds field |
| `WithParser(p ScheduleParser)` | Custom parser |
| `WithChain(wrappers ...JobWrapper)` | Add job wrappers |
| `WithLogger(logger Logger)` | Custom logger |
| `WithClock(clock Clock)` | Custom clock (for testing) |

### Job Wrappers

| Wrapper | Description |
|---------|-------------|
| `Recover(logger Logger)` | Recover panics, log errors |
| `SkipIfStillRunning(logger Logger)` | Skip if previous still running |
| `DelayIfStillRunning(logger Logger)` | Wait for previous to complete |
| `Timeout(timeout time.Duration)` | Cancel job after timeout |

### Parser Functions

| Function | Description |
|----------|-------------|
| `ParseStandard(spec string) (Schedule, error)` | Parse standard 5-field cron |
| `NewParser(options ParseOption) Parser` | Create custom parser |

**Parse Options:**
```go
const (
    Second         ParseOption = 1 << iota  // Enable seconds (required)
    SecondOptional                          // Enable seconds (optional)
    Minute                                   // Minutes field
    Hour                                     // Hours field
    Dom                                      // Day of month
    Month                                    // Month field
    Dow                                      // Day of week
    DowOptional                              // Day of week (optional)
    Descriptor                               // @hourly, @every, etc.
)
```

### Clock Implementations

| Type | Usage |
|------|-------|
| `RealClock{}` | Production (default) |
| `FakeClock` | Testing with controlled time |
| `ClockFunc(fn)` | Backward-compatible adapter |

**FakeClock Methods:**
```go
func NewFakeClock(t time.Time) *FakeClock
func (f *FakeClock) Advance(d time.Duration)  // Move time forward
func (f *FakeClock) Set(t time.Time)          // Jump to specific time
func (f *FakeClock) BlockUntil(n int)         // Wait for n timers
func (f *FakeClock) TimerCount() int          // Active timer count
```

### Schedule Types

| Type | Constructor | Description |
|------|-------------|-------------|
| `SpecSchedule` | `Parser.Parse()` | Cron expression schedule |
| `ConstantDelaySchedule` | `Every(duration)` | Fixed interval schedule |

---

## Cron Expression Syntax

### Standard Format (5 fields)
```
┌───────────── minute (0-59)
│ ┌───────────── hour (0-23)
│ │ ┌───────────── day of month (1-31)
│ │ │ ┌───────────── month (1-12 or JAN-DEC)
│ │ │ │ ┌───────────── day of week (0-6 or SUN-SAT)
│ │ │ │ │
* * * * *
```

### With Seconds (6 fields)
```
┌───────────── second (0-59)
│ ┌───────────── minute (0-59)
│ │ ┌───────────── hour (0-23)
│ │ │ ┌───────────── day of month (1-31)
│ │ │ │ ┌───────────── month (1-12)
│ │ │ │ │ ┌───────────── day of week (0-6)
│ │ │ │ │ │
* * * * * *
```

### Special Characters
| Char | Meaning | Example |
|------|---------|---------|
| `*` | All values | `* * * * *` (every minute) |
| `/` | Step | `*/15 * * * *` (every 15 min) |
| `,` | List | `1,15 * * * *` (min 1 and 15) |
| `-` | Range | `9-17 * * * *` (9am-5pm) |
| `?` | Any (dom/dow) | `0 0 ? * MON` |

### Predefined Schedules
| Entry | Equivalent | Description |
|-------|------------|-------------|
| `@yearly` | `0 0 1 1 *` | Jan 1st midnight |
| `@monthly` | `0 0 1 * *` | 1st of month |
| `@weekly` | `0 0 * * 0` | Sunday midnight |
| `@daily` | `0 0 * * *` | Every midnight |
| `@hourly` | `0 * * * *` | Every hour |
| `@every <dur>` | - | Fixed interval |

---

## Testing Guide

### Test Categories

| Category | Files | Purpose |
|----------|-------|---------|
| **Unit** | `*_test.go` | Component isolation |
| **Integration** | `cron_test.go` | Full scheduler tests |
| **Fuzz** | `fuzz_test.go` | Input fuzzing |
| **Benchmark** | `benchmark_test.go` | Performance |
| **Examples** | `example_test.go` | Runnable docs |

### Running Tests

```bash
# All tests
make test

# With race detector
make test-race

# Coverage
make coverage

# Fuzz tests (1 minute)
go test -fuzz=FuzzParseStandard -fuzztime=1m

# Benchmarks
go test -bench=. -benchmem
```

### Using FakeClock for Deterministic Tests

```go
func TestScheduledJob(t *testing.T) {
    // Create FakeClock at known time
    start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
    fakeClock := cron.NewFakeClock(start)

    // Create cron with FakeClock
    c := cron.New(cron.WithClock(fakeClock))

    var called bool
    c.AddFunc("@every 1h", func() { called = true })
    c.Start()
    defer c.Stop()

    // Wait for scheduler to register timer
    fakeClock.BlockUntil(1)

    // Advance time to trigger job
    fakeClock.Advance(time.Hour)
    time.Sleep(10 * time.Millisecond) // Let goroutine execute

    if !called {
        t.Error("job should have been called")
    }
}
```

---

## Development

### Make Targets

```bash
make help        # Show all targets
make test        # Run tests
make test-race   # Run with race detector
make coverage    # Generate coverage report
make lint        # Run golangci-lint
make fmt         # Format code
make vet         # Run go vet
make build       # Build package
make clean       # Clean artifacts
```

### Code Quality

```bash
# Lint
golangci-lint run

# Format
gofmt -w .

# Vet
go vet ./...
```

### Git Hooks (lefthook)

Pre-commit hooks run automatically:
- `go fmt`
- `go vet`
- `golangci-lint`

---

## Version History

See [CHANGELOG.md](../CHANGELOG.md) for detailed release notes.

### Recent Changes
- **Clock Interface**: Full `Clock`/`Timer` interfaces for deterministic testing
- **FakeClock**: Test implementation with `Advance()`, `Set()`, `BlockUntil()`
- **Min-Heap**: O(log n) entry scheduling (replaces sorted slice)
- **Timeout Wrapper**: Cancel jobs after specified duration
- **Slog Logger**: Native `log/slog` integration

---

## Cross-References

| Document | Description |
|----------|-------------|
| [README.md](../README.md) | User guide and quick start |
| [CONTRIBUTING.md](../CONTRIBUTING.md) | How to contribute |
| [CHANGELOG.md](../CHANGELOG.md) | Version history |
| [SECURITY.md](../SECURITY.md) | Security policy |
| [AGENTS.md](../AGENTS.md) | AI agent guidelines |
| [API_REFERENCE.md](./API_REFERENCE.md) | Detailed API docs |
| [ARCHITECTURE.md](./ARCHITECTURE.md) | Design patterns |

---

*Generated: 2024-11-28 | go-cron v3.x*
