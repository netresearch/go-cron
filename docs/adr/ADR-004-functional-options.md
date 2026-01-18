# ADR-004: Functional Options Pattern

## Status
Accepted

## Date
2025-12-14

## Context

The `Cron` scheduler has many optional configuration parameters:
- Location (timezone)
- Logger
- Parser
- Job wrapper chain
- Clock (for testing)
- Entry limits
- Observability hooks

A constructor with all these parameters would be unwieldy:

```go
// BAD: Too many parameters, hard to read and maintain
func New(loc *time.Location, logger Logger, parser Parser, chain Chain,
         clock Clock, maxEntries int, hooks ObservabilityHooks) *Cron
```

**Requirements:**
- Sensible defaults for all configuration
- Easy to specify only the options you need
- Backward compatible when adding new options
- Self-documenting API
- Type-safe configuration

## Decision

Use the Functional Options pattern, where each configuration option is a function that modifies the `Cron` struct:

```go
type Option func(*Cron)

func WithLocation(loc *time.Location) Option {
    return func(c *Cron) {
        c.location = loc
    }
}

func WithLogger(logger Logger) Option {
    return func(c *Cron) {
        c.logger = logger
    }
}

// Usage
c := cron.New(
    cron.WithLocation(time.UTC),
    cron.WithLogger(myLogger),
    cron.WithSeconds(),
)
```

**Implementation:**

```go
func New(opts ...Option) *Cron {
    c := &Cron{
        // Sensible defaults
        entries:  make(entryHeap, 0),
        chain:    NewChain(),
        add:      make(chan *Entry),
        stop:     make(chan struct{}),
        snapshot: make(chan chan []Entry),
        remove:   make(chan EntryID),
        running:  false,
        logger:   DefaultLogger,
        location: time.Local,
        parser:   standardParser,
        clock:    &realClock{},
    }

    for _, opt := range opts {
        opt(c)
    }

    return c
}
```

## Consequences

### Positive

- **Clean API**: Only specify what you need
- **Self-documenting**: `WithLocation(time.UTC)` is clear
- **Backward compatible**: New options don't break existing code
- **Type-safe**: Compiler catches invalid options
- **Composable**: Options can be combined freely
- **Testable**: Options are just functions, easy to test

### Negative

- **Verbose implementation**: Each option requires a function
- **Less discoverable**: Options are functions, not struct fields
- **Potential conflicts**: Multiple options may set same field
- **No validation**: Invalid combinations aren't caught at compile time

### Neutral

- **Common pattern**: Well-understood in Go ecosystem
- **IDE support**: Autocomplete works with modern IDEs

## Alternatives Considered

### 1. Configuration Struct

```go
type Config struct {
    Location   *time.Location
    Logger     Logger
    Parser     Parser
    // ...
}

func New(cfg Config) *Cron
```

- **Rejected**: Requires specifying all fields (or using pointers)
- Zero values may not be sensible defaults
- Adding fields is a breaking change if struct is copied

### 2. Builder Pattern

```go
c := cron.NewBuilder().
    WithLocation(time.UTC).
    WithLogger(myLogger).
    Build()
```

- **Rejected**: More complex implementation
- Requires additional Builder type
- No significant advantage over functional options

### 3. Constructor Overloads

```go
func New() *Cron
func NewWithLocation(loc *time.Location) *Cron
func NewWithLocationAndLogger(loc *time.Location, logger Logger) *Cron
```

- **Rejected**: Combinatorial explosion of functions
- Impossible to maintain with many options

### 4. Setter Methods

```go
c := cron.New()
c.SetLocation(time.UTC)
c.SetLogger(myLogger)
c.Start()
```

- **Rejected**: Configuration after construction is error-prone
- Must validate state on Start()
- Not idiomatic for immutable-after-construction types

## Common Options

| Option | Purpose | Default |
|--------|---------|---------|
| `WithLocation(loc)` | Set default timezone | `time.Local` |
| `WithLogger(logger)` | Set logger | `DefaultLogger` |
| `WithParser(parser)` | Set cron parser | Standard 5-field |
| `WithSeconds()` | Enable seconds field | Disabled |
| `WithChain(wrappers...)` | Set job wrapper chain | Empty chain |
| `WithClock(clock)` | Set time source | Real clock |
| `WithMaxEntries(n)` | Limit entry count | Unlimited |
| `WithObservability(hooks)` | Set observability hooks | None |

## References

- Rob Pike's blog post: https://commandcenter.blogspot.com/2014/01/self-referential-functions-and-design.html
- Dave Cheney's article: https://dave.cheney.net/2014/10/17/functional-options-for-friendly-apis
- Google Cloud Go client patterns
