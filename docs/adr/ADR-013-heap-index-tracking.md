# ADR-013: Heap Index Tracking for O(log n) Removal

## Status
Accepted

## Date
2025-12-14

## Context

The min-heap (ADR-001) stores entries ordered by next execution time. To remove an entry by ID, we need to find its position in the heap.

**Without index tracking:**
```go
func (c *Cron) removeFromHeap(id EntryID) {
    for i, e := range c.entries {
        if e.ID == id {
            heap.Remove(&c.entries, i)  // O(log n)
            return
        }
    }
}
// Total: O(n) search + O(log n) removal = O(n)
```

**Goal:** O(log n) removal without linear search.

## Decision

Each Entry stores its own heap index, maintained by the heap implementation:

```go
type Entry struct {
    ID        EntryID
    // ... other fields
    heapIndex int  // Position in heap, -1 if removed
}

// heap.Interface implementation updates heapIndex
func (h entryHeap) Swap(i, j int) {
    h[i], h[j] = h[j], h[i]
    h[i].heapIndex = i
    h[j].heapIndex = j
}

func (h *entryHeap) Push(x any) {
    e := x.(*Entry)
    e.heapIndex = len(*h)
    *h = append(*h, e)
}

func (h *entryHeap) Pop() any {
    old := *h
    n := len(old)
    e := old[n-1]
    e.heapIndex = -1  // Mark as removed
    *h = old[0 : n-1]
    return e
}
```

**Removal with index:**
```go
func (c *Cron) removeFromHeap(entry *Entry) {
    if entry.heapIndex >= 0 {
        heap.Remove(&c.entries, entry.heapIndex)  // O(log n)
    }
}
```

Combined with entryIndex map (ADR-011):
- Lookup by ID: O(1) via map
- Remove from heap: O(log n) via heapIndex
- **Total: O(log n)**

## Consequences

### Positive

- **O(log n) removal**: No linear search needed
- **Removed detection**: `heapIndex == -1` indicates removed entry
- **Standard pattern**: Common in priority queue implementations
- **No extra storage**: Index stored in Entry itself

### Negative

- **Coupling**: Entry knows about heap internals
- **Synchronization**: heapIndex must be updated on every swap
- **Invalid after removal**: heapIndex is meaningless after Pop

### Neutral

- **Container/heap compatible**: Works with standard library
- **Pointer requirement**: Heap must store `*Entry`, not `Entry`

## Implementation Details

### Invariants

1. `entry.heapIndex == i` iff `heap[i] == entry`
2. `entry.heapIndex == -1` iff entry is not in heap
3. After any heap operation, all indices are consistent

### Edge Cases

```go
// Entry removed but pointer still held
entry := c.Entry(id)
c.Remove(id)
// entry.heapIndex is now -1
// entry is safe to inspect but not in heap
```

## Alternatives Considered

### 1. Separate Index Map

```go
heapPositions map[EntryID]int
```

- **Rejected**: Duplicate storage
- Must sync two data structures
- Entry lookup requires two map accesses

### 2. Linear Search

- **Rejected**: O(n) removal is unacceptable for large entry counts
- Performance degrades linearly with entries

### 3. Use Entry Pointer as Key

```go
positions map[*Entry]int
```

- **Rejected**: Pointer equality is fragile
- Entry copies would break lookup

### 4. Binary Search by ID

- **Rejected**: Heap is ordered by time, not ID
- Would require separate sorted structure

## References

- Go container/heap: https://pkg.go.dev/container/heap
- ADR-001: Min-Heap for Entry Scheduling
- ADR-011: Dual-Index Maps
