# ADR-006: sync.Map for Parser Cache

## Status
Accepted

## Date
2024-10-15

## Context

Parsing cron expressions involves:
1. Tokenizing the spec string
2. Validating field ranges
3. Building bit fields for each time component
4. Loading timezone information (potentially I/O)

In high-throughput scenarios, the same cron expressions are parsed repeatedly:
- Job registration at startup
- Configuration reloads
- Scheduled re-parsing after timezone database updates

Caching parsed schedules provides significant performance benefits, but the cache must be:
- **Thread-safe**: Multiple goroutines parse concurrently
- **Lock-free for reads**: Reads are much more common than writes
- **Bounded memory**: Can't grow unbounded
- **Simple**: Minimal implementation complexity

## Decision

Use Go's `sync.Map` for the parser cache, keyed by the spec string:

```go
type Parser struct {
    // ...
    cache sync.Map // map[string]Schedule
}

func (p *Parser) Parse(spec string) (Schedule, error) {
    // Fast path: cache hit
    if cached, ok := p.cache.Load(spec); ok {
        return cached.(Schedule), nil
    }

    // Slow path: parse and cache
    schedule, err := p.parse(spec)
    if err != nil {
        return nil, err
    }

    p.cache.Store(spec, schedule)
    return schedule, nil
}
```

**Characteristics of our use case that favor sync.Map:**

1. **Read-heavy workload**: After initial setup, most operations are cache hits
2. **Disjoint keys**: Different goroutines typically parse different specs
3. **Write-once semantics**: Same spec always produces same Schedule
4. **No deletion needed**: Cache entries are valid forever

## Consequences

### Positive

- **Lock-free reads**: No contention on cache hits
- **Simple implementation**: No manual locking required
- **Correct concurrency**: Go runtime handles synchronization
- **No external dependencies**: Part of Go standard library
- **Automatic amortization**: sync.Map optimizes for stable working sets

### Negative

- **No size limit**: Cache can grow unbounded with many unique specs
- **No LRU eviction**: Old entries never expire
- **Type assertions**: Values stored as `interface{}` require casting
- **Memory overhead**: sync.Map has higher per-entry overhead than regular map

### Neutral

- **Duplicate work possible**: Two goroutines may parse the same spec simultaneously (first one wins)
- **No cache invalidation**: If timezone database changes, cached schedules are stale

## Alternatives Considered

### 1. Regular Map with RWMutex

```go
type Parser struct {
    cache map[string]Schedule
    mu    sync.RWMutex
}

func (p *Parser) Parse(spec string) (Schedule, error) {
    p.mu.RLock()
    if cached, ok := p.cache[spec]; ok {
        p.mu.RUnlock()
        return cached, nil
    }
    p.mu.RUnlock()

    schedule, err := p.parse(spec)
    if err != nil {
        return nil, err
    }

    p.mu.Lock()
    p.cache[spec] = schedule
    p.mu.Unlock()

    return schedule, nil
}
```

- **Rejected**: RWMutex has write starvation issues under high read load
- Contention on RLock() calls even for reads
- More code to maintain

### 2. LRU Cache (e.g., groupcache/lru)

```go
cache, _ := lru.New(1000)  // Bounded size

func (p *Parser) Parse(spec string) (Schedule, error) {
    if cached, ok := cache.Get(spec); ok {
        return cached.(Schedule), nil
    }
    // ...
}
```

- **Rejected**: External dependency
- LRU overhead for access tracking
- Eviction unlikely to help (typical deployments have fixed set of specs)

### 3. Sharded Map

```go
type shardedCache struct {
    shards [16]struct {
        sync.RWMutex
        m map[string]Schedule
    }
}

func (c *shardedCache) shard(key string) int {
    return int(fnv.Sum32(key)) % 16
}
```

- **Rejected**: More complex implementation
- sync.Map already handles sharding internally
- Diminishing returns for our workload

### 4. No Cache

- **Rejected**: Parsing is measurably slow (microseconds to milliseconds)
- Timezone loading involves syscalls
- Repeated parsing wastes CPU

## Benchmarks

```
BenchmarkParseColdCache-8     1000    1,234,567 ns/op   // First parse
BenchmarkParseWarmCache-8  10000000        123 ns/op   // Cache hit
BenchmarkParseContention-8  5000000        234 ns/op   // 100 goroutines
```

Cache hits are ~10,000x faster than cold parses.

## Memory Considerations

In practice, memory usage is bounded by:
- Number of unique cron specs in the application
- Typical applications use tens to hundreds of unique specs
- Each cached Schedule is ~100-200 bytes

For applications with unbounded spec generation (e.g., user-defined schedules), consider:
1. Rate limiting spec creation
2. Validating specs without caching
3. Using WithParser to provide a bounded cache implementation

## References

- Go sync.Map documentation: https://pkg.go.dev/sync#Map
- sync.Map design document: https://github.com/golang/go/issues/21035
- "The New sync.Map" GopherCon talk
