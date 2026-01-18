# ADR-010: Channel-Based Synchronization Model

## Status
Accepted

## Date
2025-12-14

## Context

The Cron scheduler must handle concurrent operations safely:
- Adding entries while running
- Removing entries while running
- Taking snapshots of current state
- Looking up entries by ID or name
- Stopping the scheduler

**The run loop owns all mutable state** (entries heap, index maps). External goroutines need to interact with this state safely.

**Deadlock risk:** If the scheduler uses mutexes and a job callback tries to modify entries, it can deadlock:

```go
// DEADLOCK SCENARIO with mutex approach:
// 1. Run loop holds mutex, executes job
// 2. Job calls c.Remove(id)
// 3. Remove() tries to acquire mutex → deadlock
```

## Decision

Use **channel-based synchronization** where the run loop is the sole owner of mutable state. All external operations send requests via channels and receive responses.

```go
type Cron struct {
    entries     entryHeap
    entryIndex  map[EntryID]*Entry
    nameIndex   map[string]*Entry

    // Channels for synchronized operations
    add         chan request[*Entry, struct{}]
    remove      chan request[EntryID, struct{}]
    snapshot    chan chan []Entry
    entryLookup chan entryLookupRequest
    nameLookup  chan nameLookupRequest
    stop        chan struct{}
}

// Request-response pattern with buffered reply channel
type request[T, C any] struct {
    value T
    reply chan C
}

func makeReq[T, C any](v T) request[T, C] {
    return request[T, C]{value: v, reply: make(chan C, 1)}
}
```

**Run loop handles all operations:**

```go
for {
    select {
    case req := <-c.add:
        // Add entry to heap and indexes
        req.reply <- struct{}{}

    case req := <-c.remove:
        // Remove entry from heap and indexes
        req.reply <- struct{}{}

    case replyChan := <-c.snapshot:
        replyChan <- c.entrySnapshot()

    case req := <-c.entryLookup:
        // O(1) lookup in entryIndex
        req.reply <- entry

    case <-c.stop:
        return
    }
}
```

## Consequences

### Positive

- **Deadlock-free**: Jobs can safely call `Add()`, `Remove()`, etc.
- **No lock contention**: Single goroutine owns state
- **Clear ownership**: Run loop is authoritative source
- **Predictable ordering**: Operations processed in channel receive order
- **Testable**: Channel operations are observable

### Negative

- **Latency**: Operations wait for run loop's select
- **Complexity**: Request-response pattern is more code than mutex
- **Buffered channels**: Reply channels need buffer to avoid blocking sender
- **Not running state**: Different code path when scheduler isn't running

### Neutral

- **Synchronous semantics**: Callers block until operation completes
- **Channel overhead**: Minimal compared to operation cost

## Implementation Details

### Buffered Reply Channels

Reply channels use buffer size 1 to prevent the run loop from blocking:

```go
reply: make(chan C, 1)  // Sender never blocks
```

### Running vs Not-Running Paths

When the scheduler isn't running, operations work directly on state (no channels needed):

```go
func (c *Cron) Remove(id EntryID) Entry {
    c.runningMu.Lock()
    defer c.runningMu.Unlock()

    if c.running {
        // Send request via channel
        req := makeReq[EntryID, struct{}](id)
        c.remove <- req
        <-req.reply
        return c.Entry(id)
    }

    // Direct access when not running
    return c.removeEntry(id)
}
```

### Exclusive Run Loop Access

Some operations must only be called from the run loop:

```go
// removeEntry must only be called from the run loop, or when holding runningMu
// with c.running == false. Calling from elsewhere causes data races.
func (c *Cron) removeEntry(id EntryID) Entry {
    // ...
}
```

## Alternatives Considered

### 1. RWMutex for All Operations

```go
func (c *Cron) Remove(id EntryID) Entry {
    c.mu.Lock()
    defer c.mu.Unlock()
    return c.removeEntry(id)
}
```

- **Rejected**: Deadlocks when jobs call Remove()
- Run loop holds lock while executing jobs
- Jobs calling Remove() would block forever

### 2. Mutex with Deferred Operations

```go
// Queue operation, don't execute immediately
func (c *Cron) Remove(id EntryID) {
    c.pendingOps <- func() { c.removeEntry(id) }
}
```

- **Rejected**: No return value possible
- Callers can't confirm operation succeeded
- Complex error handling

### 3. Lock-Free Data Structures

```go
entries sync.Map  // Concurrent map
```

- **Rejected**: Heap can't be lock-free
- Index consistency harder to maintain
- Overkill for typical job counts

### 4. Per-Operation Channels

```go
addChan    chan *Entry      // No reply
removeChan chan EntryID     // No reply
```

- **Rejected**: No confirmation of completion
- Caller can't know when operation finished
- Race conditions in tests

## References

- Go Concurrency Patterns: https://go.dev/blog/pipelines
- Share Memory By Communicating: https://go.dev/blog/codelab-share
- Effective Go - Channels: https://go.dev/doc/effective_go#channels
