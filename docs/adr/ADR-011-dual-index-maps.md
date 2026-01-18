# ADR-011: Dual-Index Maps for O(1) Entry Lookup

## Status
Accepted

## Date
2025-12-14

## Context

Entries need to be accessed by multiple keys:
- **By ID**: For `Remove(id)`, `Entry(id)`, observability correlation
- **By Name**: For `EntryByName(name)`, duplicate detection, user-friendly references

The min-heap (ADR-001) provides O(1) access to the next entry but O(n) search by ID or name.

**Requirements:**
- O(1) lookup by ID (frequent operation)
- O(1) lookup by name (when names are used)
- O(1) duplicate name detection on add
- Memory efficiency for typical workloads

## Decision

Maintain **two separate index maps** alongside the heap:

```go
type Cron struct {
    entries    entryHeap           // Min-heap for scheduling
    entryIndex map[EntryID]*Entry  // O(1) lookup by ID
    nameIndex  map[string]*Entry   // O(1) lookup by Name
}
```

**Invariants:**
- Every entry in the heap exists in `entryIndex`
- Named entries also exist in `nameIndex`
- Maps point to the same `*Entry` as the heap

**Operations:**

| Operation | Heap | entryIndex | nameIndex |
|-----------|------|------------|-----------|
| Add | O(log n) push | O(1) insert | O(1) insert if named |
| Remove by ID | O(log n) remove | O(1) delete | O(1) delete if named |
| Lookup by ID | - | O(1) | - |
| Lookup by Name | - | - | O(1) |
| Next to run | O(1) peek | - | - |

## Consequences

### Positive

- **Fast lookups**: O(1) by both ID and name
- **Fast duplicate detection**: `nameIndex[name]` check before add
- **Pointer sharing**: Maps reference heap entries, no duplication
- **Selective indexing**: Only named entries in nameIndex

### Negative

- **Memory overhead**: Two maps in addition to heap
- **Synchronization**: All three structures must stay consistent
- **Compaction needed**: Go maps don't shrink (see below)

### Neutral

- **Pointer indirection**: Maps store `*Entry`, not values
- **Name optionality**: Unnamed entries only in entryIndex

## Memory Considerations

### Map Memory Leak

Go maps don't release memory when entries are deleted. In high-churn scenarios (frequent add/remove), maps grow but never shrink.

**Solution:** Track deletions and periodically rebuild maps:

```go
type Cron struct {
    // ...
    indexDeletions int  // Removals since last compaction
}

const (
    indexCompactionThreshold = 1000  // Trigger after N deletions
)

func (c *Cron) maybeCompactIndexes() {
    if c.indexDeletions < indexCompactionThreshold {
        return
    }

    // Only compact if deletions are significant portion of entries
    if c.indexDeletions < len(c.entryIndex)/2 {
        return
    }

    // Rebuild maps from heap
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

### Pre-allocation

For bulk entry addition, `WithCapacity()` pre-allocates maps:

```go
func WithCapacity(n int) Option {
    return func(c *Cron) {
        if n > 0 {
            c.entryIndex = make(map[EntryID]*Entry, n)
            c.nameIndex = make(map[string]*Entry, n)
            c.entries = make(entryHeap, 0, n)
        }
    }
}
```

## Alternatives Considered

### 1. Single Combined Index

```go
type entryKey struct {
    id   EntryID
    name string
}
index map[entryKey]*Entry
```

- **Rejected**: Can't efficiently query by just ID or just name
- Requires scanning for partial key matches

### 2. Name-to-ID Map + ID Index

```go
entryIndex map[EntryID]*Entry
nameToID   map[string]EntryID  // Indirection
```

- **Rejected**: Extra indirection for name lookups
- Two map accesses instead of one

### 3. Linear Search for Names

```go
func (c *Cron) EntryByName(name string) Entry {
    for _, e := range c.entries {
        if e.Name == name {
            return *e
        }
    }
    return Entry{}
}
```

- **Rejected**: O(n) for every name lookup
- Duplicate detection becomes O(n) on every add

### 4. Sorted Slice with Binary Search

```go
nameIndex []struct{ name string; entry *Entry }  // Sorted by name
```

- **Rejected**: O(log n) lookup instead of O(1)
- O(n) insertion to maintain sort order

### 5. Skip List or B-Tree

- **Rejected**: Overkill for typical entry counts
- Added complexity without proportional benefit
- Standard library doesn't include these

## Typical Workload Analysis

| Scenario | Entry Count | Name Usage | Recommendation |
|----------|-------------|------------|----------------|
| Simple app | 5-20 | Rare | Overhead negligible |
| Medium service | 50-200 | Common | Indexes provide clear benefit |
| Large scheduler | 1000+ | Heavy | Essential for performance |
| High churn | Any | Any | Enable compaction |

## References

- ADR-001: Min-Heap for Entry Scheduling
- Go Maps in Action: https://go.dev/blog/maps
- WithCapacity option for pre-allocation
