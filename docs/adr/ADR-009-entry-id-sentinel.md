# ADR-009: Entry ID Sentinel Value (0 = Invalid)

## Status
Accepted

## Date
2025-12-14

## Context

Every scheduled entry needs a unique identifier for:
- Removing entries by ID
- Looking up entry state
- Correlating observability events
- User code referencing specific jobs

**Design question:** How should we represent "no entry" or "invalid entry"?

Options:
1. Use `EntryID(0)` as an invalid sentinel
2. Use pointer types (`*Entry`) with nil checks
3. Use a separate boolean field (`valid bool`)
4. Use an optional/maybe type pattern

## Decision

Use `EntryID(0)` as the invalid/null sentinel value. Valid entries always have `ID >= 1`.

```go
type EntryID int

// Entry.Valid() returns true if this is not the zero entry
func (e Entry) Valid() bool { return e.ID != 0 }

// ID allocation skips 0 on wraparound
c.nextID++
if c.nextID == 0 {
    c.nextID = 1 // Skip 0; Entry.Valid() uses 0 as invalid sentinel
}
```

**Invariants:**
- `Entry{}` (zero value) is always invalid
- `Entry(id).Valid()` returns `false` when `id == 0`
- ID allocation never assigns 0, even after wraparound

## Consequences

### Positive

- **Zero value is safe**: Uninitialized `Entry{}` is clearly invalid
- **Simple validity check**: Single comparison, no pointer dereference
- **No nil panics**: Works with value types, not pointers
- **Consistent with Go idioms**: Similar to `time.Time.IsZero()`
- **Efficient**: No allocation for "not found" returns

### Negative

- **Wraparound handling**: Must explicitly skip 0 during ID allocation
- **Signed type**: `EntryID` is `int`, so negative IDs are technically valid (though never assigned)
- **Documentation needed**: Users must know 0 is reserved

### Neutral

- **Large ID space**: With `int` (64-bit on most systems), wraparound is practically impossible in normal use

## Alternatives Considered

### 1. Pointer with Nil Check

```go
func (c *Cron) Entry(id EntryID) *Entry {
    if e, ok := c.entryIndex[id]; ok {
        return e
    }
    return nil
}
```

- **Rejected**: Requires nil checks everywhere
- Pointer semantics complicate copying/snapshots
- Nil dereference panics are common bugs

### 2. Separate Valid Field

```go
type Entry struct {
    ID    EntryID
    Valid bool
    // ...
}
```

- **Rejected**: Redundant storage
- Can become inconsistent (Valid=true, ID=0)
- Extra field to maintain

### 3. Optional Type Pattern

```go
type OptionalEntry struct {
    Entry Entry
    Ok    bool
}
```

- **Rejected**: Verbose API
- Not idiomatic Go (pre-generics pattern)
- Adds complexity without benefit

### 4. Error Returns Only

```go
func (c *Cron) Entry(id EntryID) (Entry, error) {
    // ...
}
```

- **Rejected**: Forces error handling even for simple lookups
- Can't use in expressions like `if c.Entry(id).Valid() { ... }`

## Usage Patterns

```go
// Check if entry exists
if entry := c.Entry(id); entry.Valid() {
    fmt.Printf("Next run: %v\n", entry.Next)
}

// Remove returns the entry if found
if removed := c.Remove(id); removed.Valid() {
    log.Printf("Removed entry %d", removed.ID)
}

// Zero value is safe
var entry Entry
if entry.Valid() { // Always false
    // Never reached
}
```

## References

- Go time package: `time.Time.IsZero()` pattern
- Database primary keys: 0/NULL as "no record"
