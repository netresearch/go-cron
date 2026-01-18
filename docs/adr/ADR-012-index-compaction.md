# ADR-012: Index Compaction for Memory Reclamation

## Status
Accepted

## Date
2025-12-14

## Context

Go maps don't release memory when entries are deleted. The `delete()` operation marks slots as empty but doesn't shrink the underlying hash table. In high-churn scenarios (frequent add/remove cycles), index maps grow unbounded.

```go
// This leaks memory over time
for i := 0; i < 1000000; i++ {
    id, _ := c.AddFunc("@hourly", job)
    c.Remove(id)
}
// Maps still hold memory for 1M entries
```

**Problem:** Memory bloat in long-running schedulers with dynamic job management.

## Decision

Track deletion count and periodically rebuild maps when threshold exceeded:

```go
type Cron struct {
    entryIndex     map[EntryID]*Entry
    nameIndex      map[string]*Entry
    indexDeletions int  // Removals since last compaction
}

const indexCompactionThreshold = 1000

func (c *Cron) maybeCompactIndexes() {
    if c.indexDeletions < indexCompactionThreshold {
        return
    }
    // Only compact if deletions are significant portion
    if c.indexDeletions < len(c.entryIndex)/2 {
        return
    }

    // Rebuild maps from heap (source of truth)
    c.entryIndex = make(map[EntryID]*Entry, len(c.entries))
    c.nameIndex = make(map[string]*Entry)
    for _, e := range c.entries {
        c.entryIndex[e.ID] = e
        if e.Name != "" {
            c.nameIndex[e.Name] = e
        }
    }
    c.indexDeletions = 0
}
```

**Trigger conditions:**
1. At least 1000 deletions since last compaction
2. Deletions are at least 50% of current entry count

## Consequences

### Positive

- **Memory reclamation**: Maps shrink after high-churn periods
- **Bounded overhead**: Only compacts when beneficial
- **No API changes**: Transparent to users
- **Heap is source of truth**: Rebuild is safe and consistent

### Negative

- **O(n) compaction cost**: Rebuilding maps takes linear time
- **Deletion tracking overhead**: Counter increment on every remove
- **Threshold tuning**: Fixed values may not suit all workloads

### Neutral

- **Amortized cost**: Compaction is rare in typical usage
- **Only affects high-churn**: Stable workloads never trigger

## Alternatives Considered

### 1. Never Compact

- **Rejected**: Memory grows unbounded in dynamic workloads
- Production systems can't tolerate memory leaks

### 2. Compact on Every Delete

- **Rejected**: O(n) cost per deletion is prohibitive
- Would make Remove() unacceptably slow

### 3. Time-Based Compaction

```go
if time.Since(lastCompaction) > time.Hour {
    compact()
}
```

- **Rejected**: May compact when unnecessary
- Doesn't correlate with actual memory pressure

### 4. Memory Pressure Detection

```go
if runtime.MemStats.HeapAlloc > threshold {
    compact()
}
```

- **Rejected**: Complex to tune threshold
- Global metric doesn't identify specific map bloat

### 5. Use sync.Map

- **Rejected**: sync.Map has same issue (no shrinking)
- Would lose type safety

## References

- Go maps internals: https://go.dev/blog/maps
- ADR-011: Dual-Index Maps Strategy
