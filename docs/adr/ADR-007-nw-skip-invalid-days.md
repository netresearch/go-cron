# ADR-007: nW Syntax Skips Months Without Target Day

## Status
Accepted

## Date
2025-12-15

## Context

The extended cron syntax includes `nW` (nearest weekday to day n). A design decision was needed for how `nW` should behave when the target day doesn't exist in a given month.

**Example:** `31W` (nearest weekday to the 31st) in February, which only has 28/29 days.

**Two possible approaches:**

### Option A: Clamp to Last Day
- `31W` in February → clamps to `29W` → runs on nearest weekday to Feb 29
- Effectively makes `31W` behave like `LW` in short months
- Used by Quartz and Spring frameworks

### Option B: Skip Month
- `31W` in February → no match → skip to March
- Consistent with standard cron behavior (`31 * * *` skips February)
- Requires users to use `LW` for "last weekday of every month"

**Key Consideration:** go-cron already provides `LW` (last weekday of month) syntax, which explicitly handles the "run on last weekday every month" use case.

## Decision

**Adopt Option B: Skip months where the target day doesn't exist.**

When `nW` is specified and day `n` doesn't exist in the current month, return no match for that month. The scheduler advances to the next month that contains day `n`.

```go
func nearestWeekday(year int, month time.Month, targetDay int, loc *time.Location) int {
    lastDay := time.Date(year, month+1, 0, 12, 0, 0, 0, loc).Day()

    // Skip month if target day doesn't exist
    if targetDay > lastDay {
        return -1  // No match
    }
    // ... find nearest weekday
}
```

## Rationale

1. **Semantic Clarity**: `31W` literally means "nearest weekday to the 31st." If there's no 31st, there's nothing to find the nearest weekday to.

2. **Consistency with Standard Cron**: The expression `31 * * *` skips February. Adding `W` should modify *which* day (weekday vs any day), not *which months* match.

3. **No Redundancy**: `LW` already exists for "last weekday of month." If `31W` also meant "last weekday" in short months, we'd have two syntaxes for the same thing.

4. **Explicit Over Implicit**: Go philosophy favors explicit behavior. Users who want every-month execution should explicitly use `LW`, not rely on implicit clamping.

5. **Predictable Behavior**: `31W` always means the same thing - no context-dependent interpretation.

## Consequences

### Positive
- Clear semantics: `nW` always refers to day `n`
- No ambiguity between `31W` and `LW`
- Consistent with standard cron day-of-month behavior
- Users must be explicit about their intent

### Negative
- Differs from Quartz/Spring behavior (which clamp)
- Users migrating from Java schedulers may expect clamping
- `31W * * *` only runs 7 months per year (Jan, Mar, May, Jul, Aug, Oct, Dec)

### Migration Guide

| If you want... | Use this |
|----------------|----------|
| Last weekday of every month | `LW` |
| Nearest weekday to 31st (7 months/year) | `31W` |
| Nearest weekday to 30th (skip Feb) | `30W` |
| Nearest weekday to 15th (every month) | `15W` |

## Implementation Notes

### Two Distinct Behaviors

The `nW` syntax has two distinct edge case behaviors:

#### 1. Target Day Doesn't Exist → Skip Month (go-cron specific)

When the target day doesn't exist in a month (e.g., `31W` in February), **skip the month entirely**.

| Expression | February (28/29 days) | Result |
|------------|----------------------|--------|
| `31W` | No day 31 exists | Skip to March |
| `30W` | No day 30 exists | Skip to March |
| `29W` | Only in leap year | Skip to March (non-leap) |

**Rationale:** Use `LW` if you want "last weekday of every month."

#### 2. Target Day Is Weekend → Stay Within Month (Quartz-compatible)

When the target day exists but falls on a weekend, find the nearest weekday **within the same month** (per [Quartz CronTrigger documentation](https://www.quartz-scheduler.org/documentation/quartz-2.3.0/tutorials/crontrigger.html)).

| Scenario | Result |
|----------|--------|
| Target is weekday (Mon-Fri) | Use target day |
| Target is Saturday, not 1st | Use Friday (target - 1) |
| Target is Saturday, is 1st | Use Monday the 3rd |
| Target is Sunday, not last day | Use Monday (target + 1) |
| Target is Sunday, is last day | Use Friday (target - 2) |

**Example:** March 31, 2024 is Sunday. `31W` returns Friday March 29 (not Monday April 1) because Quartz specifies that `W` will not "jump over the boundary of a month's days."

### Why Different Behaviors?

| Case | Behavior | Rationale |
|------|----------|-----------|
| Day doesn't exist | Skip month | go-cron has `LW` for "last weekday"; `31W` is literal |
| Day is weekend | Stay in month | Quartz compatibility; `nW` is a month-relative constraint |

The key distinction: "day doesn't exist" is a **structural** impossibility (February has no 31st), while "day is weekend" is a **calendar** detail that `W` is designed to handle.

## Related

- `LW` syntax: Last weekday of month (always matches every month)
- `L` syntax: Last day of month
- `L-n` syntax: nth day before last day of month
