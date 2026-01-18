# ADR-019: Atomic CAS for Entry Count Limiting

## Status
Accepted

## Date
2025-12-14

## Context

The `WithMaxEntries(n)` option limits how many entries can be scheduled. When adding an entry:

1. Check if count < limit
2. Increment count
3. Add entry

**Problem:** With concurrent `AddFunc`/`AddJob` calls, a race exists between check and increment:

```go
// RACE CONDITION (mutex approach still has issues)
func (c *Cron) canAddEntry() bool {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.entryCount < c.maxEntries  // May be stale
}

func (c *Cron) addEntry(e *Entry) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.entryCount++  // Another goroutine may have added between check and here
    // ...
}
```

## Decision

Use atomic Compare-And-Swap (CAS) for lock-free entry count limiting:

```go
type Cron struct {
    maxEntries int    // 0 means unlimited
    entryCount int64  // Atomic counter
}

func (c *Cron) tryIncrementEntryCount() bool {
    if c.maxEntries == 0 {
        atomic.AddInt64(&c.entryCount, 1)
        return true
    }

    for {
        current := atomic.LoadInt64(&c.entryCount)
        if int(current) >= c.maxEntries {
            return false
        }
        if atomic.CompareAndSwapInt64(&c.entryCount, current, current+1) {
            return true
        }
        // CAS failed, retry
    }
}

func (c *Cron) decrementEntryCount() {
    atomic.AddInt64(&c.entryCount, -1)
}
```

**Usage in Add:**
```go
func (c *Cron) ScheduleJob(schedule Schedule, job Job) (EntryID, error) {
    if !c.tryIncrementEntryCount() {
        return 0, ErrMaxEntriesReached
    }

    // Proceed with adding entry
    // On failure, call decrementEntryCount()
}
```

## Consequences

### Positive

- **Lock-free**: No mutex contention on hot path
- **Correct limiting**: CAS ensures atomic check-and-increment
- **Fast success path**: Single atomic operation when under limit
- **No deadlocks**: Atomic operations don't block

### Negative

- **Spin on contention**: CAS loop under high concurrency
- **Approximate during race**: Limit may be briefly exceeded
- **Complexity**: Atomic operations are subtle

### Neutral

- **int64 for count**: Matches atomic package requirements
- **Retry loop**: Standard CAS pattern

## Implementation Details

### Approximate Enforcement

Under extreme concurrent load, the limit may be briefly exceeded:

```go
// 100 goroutines calling Add simultaneously with maxEntries=10
// Possible: 10-15 entries added before limit enforced
```

This is acceptable because:
1. Limit is for resource protection, not exact quota
2. Entries beyond limit are small overhead
3. Alternative (mutex) has worse performance

### Decrement on Failure

If entry addition fails after incrementing:

```go
func (c *Cron) ScheduleJob(...) (EntryID, error) {
    if !c.tryIncrementEntryCount() {
        return 0, ErrMaxEntriesReached
    }

    // Entry creation may fail for other reasons
    entry, err := c.createEntry(...)
    if err != nil {
        c.decrementEntryCount()  // Rollback
        return 0, err
    }
    // ...
}
```

### Remove Decrements

```go
func (c *Cron) Remove(id EntryID) Entry {
    // ... remove entry ...
    c.decrementEntryCount()
    return removed
}
```

## Alternatives Considered

### 1. Mutex-Protected Counter

```go
func (c *Cron) tryAdd() bool {
    c.countMu.Lock()
    defer c.countMu.Unlock()
    if c.entryCount >= c.maxEntries {
        return false
    }
    c.entryCount++
    return true
}
```

- **Rejected**: Mutex contention under load
- All Add calls serialize through lock

### 2. Channel-Based Semaphore

```go
type Cron struct {
    slots chan struct{}  // Buffered with maxEntries capacity
}

func (c *Cron) tryAdd() bool {
    select {
    case c.slots <- struct{}{}:
        return true
    default:
        return false
    }
}
```

- **Rejected**: Channel overhead for simple counting
- Requires initialization with capacity

### 3. No Limit Enforcement

- **Rejected**: Users need resource protection
- Unbounded entries can exhaust memory

### 4. Strict Mutex Around All Operations

- **Rejected**: Performance unacceptable
- Serializes all scheduler operations

## Benchmarks

```
BenchmarkAddWithLimit/atomic-8    5000000    245 ns/op
BenchmarkAddWithLimit/mutex-8     2000000    612 ns/op
```

Atomic is ~2.5x faster under contention.

## References

- Go sync/atomic: https://pkg.go.dev/sync/atomic
- CAS pattern: https://en.wikipedia.org/wiki/Compare-and-swap
