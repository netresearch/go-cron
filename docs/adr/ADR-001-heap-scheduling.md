# ADR-001: Use Min-Heap for Entry Scheduling

## Status
Accepted

## Date
2025-12-14

## Context

The cron scheduler needs to efficiently determine which job should run next among potentially thousands of scheduled entries. The original robfig/cron implementation used a sorted slice, which required O(n log n) re-sorting after each job execution.

As the number of scheduled jobs grows, this becomes a significant performance bottleneck. Users deploying go-cron in high-scale environments (100+ jobs) reported performance degradation during periods of high job churn.

**Requirements:**
- O(1) access to the next entry to run
- Efficient insertion of new entries
- Efficient removal of arbitrary entries
- Efficient rescheduling after job execution

## Decision

Use a min-heap data structure (via Go's `container/heap` package) indexed by `Next` execution time.

```go
type entryHeap []*Entry

func (h entryHeap) Less(i, j int) bool {
    return h[i].Next.Before(h[j].Next)
}
```

Each `Entry` maintains a `heapIndex` field for efficient updates:

```go
type Entry struct {
    // ...
    heapIndex int  // Position in heap for O(log n) updates
}
```

## Consequences

### Positive

| Operation | Before (Sorted Slice) | After (Min-Heap) |
|-----------|----------------------|------------------|
| Get next entry | O(1) | O(1) |
| Insert entry | O(n log n) | O(log n) |
| Remove next | O(n) shift | O(log n) |
| Reschedule | O(n log n) | O(log n) |
| Remove by ID | O(n) | O(n) find + O(log n) remove |

- **10x faster insertion** at scale (1000+ entries)
- **Predictable performance** regardless of entry count
- **Lower GC pressure** from reduced slice copying

### Negative

- **Increased complexity**: Heap invariant must be maintained correctly
- **Iteration order**: Entries are not sorted; full scans require additional work
- **Memory overhead**: Each entry stores a heap index (8 bytes)

### Neutral

- **Remove by ID** remains O(n) due to ID lookup, but this is a rare operation
- **Snapshot operations** still require O(n) iteration

## Alternatives Considered

### 1. Keep Sorted Slice
- **Rejected**: O(n log n) reschedule is unacceptable at scale
- Simplicity doesn't justify performance penalty

### 2. Red-Black Tree (e.g., `btree` package)
- **Rejected**: More complex implementation
- O(log n) for all operations, but higher constant factors
- External dependency required

### 3. Timing Wheel
- **Rejected**: Optimized for fixed-interval timers
- Poor fit for arbitrary cron schedules (variable next times)
- Significant implementation complexity

### 4. Priority Queue with Lazy Deletion
- **Rejected**: Accumulates deleted entries, requiring periodic compaction
- Unpredictable memory usage

## References

- Go `container/heap` documentation: https://pkg.go.dev/container/heap
- Original issue discussion: robfig/cron performance analysis
- Benchmark results in `heap_test.go`
