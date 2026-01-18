# Performance

> Benchmark results, comparison with robfig/cron, and optimization tips

## Table of Contents

- [Overview](#overview)
- [Comparison with robfig/cron](#comparison-with-robfigcron)
- [Benchmark Results](#benchmark-results)
- [Optimization Tips](#optimization-tips)

---

## Overview

go-cron makes a deliberate trade-off: **slightly higher per-job memory** in exchange for **dramatically faster lookups and removals**. This is achieved through:

- **Index maps** for O(1) entry lookup by ID and name
- **Min-heap with index tracking** for O(log n) removal

For applications that add jobs once and never remove them, the overhead is ~23% more memory per job. For applications with dynamic job sets (frequent add/remove), go-cron is significantly faster.

---

## Comparison with robfig/cron

### Trade-off Summary

| Aspect | robfig/cron | netresearch/go-cron | Trade-off |
|--------|-------------|---------------------|-----------|
| Scheduler creation | 580 B | 1456 B | +150% (index maps allocated) |
| Per-job overhead | ~640 B | ~770 B | +20% for indexes |
| Entry lookup | O(n) scan | O(1) map | 150-1900x faster |
| Entry removal | O(n) scan | O(log n) heap | 8x faster at 1000 jobs |
| Dynamic workloads | Degrades with size | Stays constant | netresearch wins at scale |

### Head-to-Head Benchmarks

Measured on AMD Ryzen 9 9950X3D, Go 1.25.5, Linux (WSL2), 2026-01-18:

#### Where netresearch WINS (dramatically)

| Operation | robfig/cron | netresearch | Speedup |
|-----------|-------------|-------------|---------|
| Lookup (100 jobs) | 1,948 ns | 12.7 ns | **153x faster** |
| Lookup (1000 jobs) | 23,484 ns | 12.3 ns | **1,909x faster** |
| High churn (add+remove) | 991 ns | 601 ns | **40% faster** |
| High churn memory | 2,824 B | 770 B | **73% less** |

#### Where robfig wins (slightly)

| Operation | robfig/cron | netresearch | Difference |
|-----------|-------------|-------------|------------|
| New scheduler + 1 job | 633 ns, 1280 B | 953 ns, 2120 B | +50% slower, +66% memory |
| Bulk add 1000 jobs | 467 ms, 674 KB | 578 ms, 869 KB | +24% slower, +29% memory |

*Note: robfig is faster for bulk adds because it uses simpler data structures. The trade-off is O(n) lookups and removals.*

#### Scaling Behavior (Add+Remove cycle)

This benchmark measures adding and removing a single job to a cron with N existing jobs:

| Existing Jobs | robfig/cron | netresearch | Winner |
|---------------|-------------|-------------|--------|
| 10 | 568 ns, 904 B | 600 ns, 768 B | ~Equal (robfig 5% faster, netresearch 15% less memory) |
| 100 | 1,044 ns, 2,824 B | 620 ns, 770 B | **netresearch 40% faster, 73% less memory** |
| 1,000 | 5,098 ns, 18,184 B | 637 ns, 805 B | **netresearch 8x faster, 96% less memory** |

**Key insight:** robfig's Remove() is O(n), so performance degrades linearly with job count. netresearch's O(log n) removal stays nearly constant.

### Why the Difference?

**Always-on features in go-cron (the overhead):**

```go
type Cron struct {
    entries     entryHeap              // Min-heap (vs slice in robfig)
    entryIndex  map[EntryID]*Entry     // O(1) lookup by ID
    nameIndex   map[string]*Entry      // O(1) lookup by name
    // ... plus tracking for heap indexes
}
```

**What you get for the overhead:**

| Feature | Complexity | Benefit |
|---------|------------|---------|
| `c.Entry(id)` | O(1) | Instant lookup vs scanning all entries |
| `c.EntryByName(name)` | O(1) | Lookup by name (robfig doesn't have this) |
| `c.Remove(id)` | O(log n) | Fast removal vs O(n) scan |
| High-churn scenarios | Amortized | Index overhead pays off with frequent changes |

### When to Choose Each

**Choose robfig/cron if:**
- You add jobs once at startup and never remove them
- Memory is extremely constrained
- You have <10 jobs total

**Choose netresearch/go-cron if:**
- You need to look up jobs by ID frequently
- You add/remove jobs dynamically
- You have many jobs (100+)
- You need active maintenance and bug fixes
- You need DST handling, panic safety, or modern Go support

---

## Benchmark Results

### Test Environment

- **CPU:** AMD Ryzen 9 9950X3D 16-Core Processor
- **OS:** Linux (WSL2)
- **Go:** 1.25.5
- **Date:** 2026-01-18

### Parsing Performance

| Benchmark | ops/sec | ns/op | allocs/op | bytes/op |
|-----------|---------|-------|-----------|----------|
| ParseStandard | 2,341,600 | 510 ns | 21 | 611 B |
| ParseWithTimezone | 371,400 | 3,179 ns | 34 | 6,640 B |
| ParseDescriptor (@hourly) | 35,738,977 | 33.7 ns | 1 | 93 B |

**Key insight:** Descriptor parsing (`@hourly`, `@daily`, etc.) is ~15x faster than standard cron expressions.

### Scheduling Performance

| Benchmark | ops/sec | ns/op | allocs/op | bytes/op |
|-----------|---------|-------|-----------|----------|
| Next (simple) | 2,513,629 | 475 ns | 0 | 0 B |
| Next (complex) | 7,674,238 | 156 ns | 0 | 0 B |
| Next (with timezone) | 6,801,468 | 178 ns | 0 | 0 B |
| AddJob | 1,583,707 | 778 ns | 22 | 844 B |

**Key insight:** `Next()` calculations are zero-allocation operations.

### Heap Operations

| Benchmark | ops/sec | ns/op | allocs/op | bytes/op |
|-----------|---------|-------|-----------|----------|
| HeapPopPush | 10,348,286 | 116 ns | 0 | 0 B |
| HeapUpdate | 10,970,777 | 110 ns | 0 | 0 B |
| RemoveAtWithIndex | 6,413,700 | 188 ns | 1 | 160 B |

**Key insight:** The min-heap enables O(log n) job removal, critical for dynamic workloads.

### Wrapper Chain Overhead

| Benchmark | ops/sec | ns/op | allocs/op | bytes/op |
|-----------|---------|-------|-----------|----------|
| ChainWrappers | 235,000,000 | 5.1 ns | 0 | 0 B |

### Observability Hooks (Optional)

| Benchmark | ops/sec | ns/op | allocs/op | bytes/op |
|-----------|---------|-------|-----------|----------|
| With hooks | 2,752,043 | 433 ns | 3 | 192 B |
| Without hooks (nil) | 17,956,778 | 66 ns | 0 | 0 B |

**Note:** Observability hooks are **disabled by default** (nil). Only enable if needed.

---

## Optimization Tips

### 1. Use Descriptors When Possible

```go
// Faster (~34ns parse)
c.AddFunc("@hourly", job)

// Slower (~510ns parse)
c.AddFunc("0 * * * *", job)
```

### 2. Pre-parse Schedules for Hot Paths

```go
// Parse once at startup
schedule, _ := cron.ParseStandard("*/5 * * * *")

// Reuse the schedule
c.Schedule(schedule, job1)
c.Schedule(schedule, job2)
```

### 3. Use Entry Lookup Instead of Scanning

```go
// Fast: O(1) lookup
entry := c.Entry(id)

// Also fast: O(1) by name
entry := c.EntryByName("my-job")

// Slow: O(n) scan (avoid if possible)
for _, e := range c.Entries() {
    if e.ID == targetID { ... }
}
```

### 4. Pre-allocate Capacity for Bulk Additions

```go
// Slower: Default allocation with repeated growth
c := cron.New()
for i := 0; i < 1000; i++ {
    c.AddFunc("@every 1h", job)  // May trigger map rehashing/slice growth
}

// Faster: Pre-allocated capacity
c := cron.New(cron.WithCapacity(1000))
for i := 0; i < 1000; i++ {
    c.AddFunc("@every 1h", job)  // No reallocation needed
}
```

`WithCapacity` pre-allocates the internal maps and heap slice, reducing overhead
when bulk-loading many jobs at startup. Benchmark results show ~10-15% improvement
in bulk addition time and ~1% fewer allocations for 1000 jobs.

### 5. Batch Removals if Possible

```go
// Good: Remove multiple jobs
for _, id := range idsToRemove {
    c.Remove(id) // O(log n) each
}

// Better for large batches: Stop, rebuild, start
c.Stop()
// Rebuild with only needed jobs
c.Start()
```

---

## Running Benchmarks

```bash
# Full benchmark suite
go test -bench=. -benchmem -count=3 ./...

# Specific benchmark
go test -bench=BenchmarkNext -benchmem -count=5

# Compare with robfig/cron
# See /tmp/cron-compare for comparison benchmarks
```

---

## See Also

- [Architecture](ARCHITECTURE.md) — Internal design and data structures
- [ADR-001](adr/001-min-heap-scheduling.md) — Min-heap scheduling decision
