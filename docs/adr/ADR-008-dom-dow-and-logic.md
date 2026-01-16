# ADR-008: DOM/DOW Matching Uses AND Logic by Default

## Status
Accepted

## Date
2026-01-16

## Context

When a cron expression specifies both day-of-month (DOM) and day-of-week (DOW) fields with restricted values (not `*`), there are two possible interpretations:

| Logic | Behavior | Example: `0 0 15 * FRI` |
|-------|----------|-------------------------|
| **OR** | Either condition matches | Runs on the 15th OR any Friday (~4-5 runs/month) |
| **AND** | Both conditions must match | Runs only when the 15th is a Friday (~once per 7 months) |

### Historical Background

**Version 6 Unix cron (1975):** Used AND logic. The [original source](https://github.com/lsahn-org/unix-v6/blob/master/source/s1/cron.c#L35-L48) checked day-of-month and day-of-week as independent conditions that were AND'ed together.

**Vixie/ISC cron (1987+):** Introduced OR logic when both fields are restricted. This became the de-facto standard as Vixie cron was widely adopted.

**robfig/cron:** Followed Vixie cron's OR behavior.

### Problem with OR Logic

1. **Inconsistency:** Every other cron field uses AND logic. The expression `30 3-6 * * *` means "at minute 30 AND hours 3-6", not "at minute 30 OR hours 3-6". OR logic for DOM/DOW is a special case that surprises users.

2. **Limited usefulness:** The OR pattern has few practical use cases. When would you want "run on the 15th or any Friday"?

3. **Prevents useful patterns:** AND logic enables natural expressions:
   - `0 0 25-31 * FRI` = last Friday of month
   - `0 0 1-7 * MON` = first Monday of month
   - `0 0 13 * FRI` = Friday the 13th

4. **Extended syntax conflict:** The new `#L` syntax (`FRI#L` = last Friday) works because of AND logic. With OR logic, you'd need to also restrict DOM, which is confusing.

## Decision

**Change the default to AND logic for DOM/DOW matching.**

When both day-of-month and day-of-week are restricted (neither is `*`), both conditions must match for the schedule to trigger. This aligns with:
- All other cron field combinations
- The original V6 Unix cron behavior
- User intuition about how field combinations work

Provide `DowOrDom` parser option for users who need legacy OR behavior.

## Rationale

1. **Consistency:** Matches how all other cron fields work together. No special case to remember.

2. **Enables useful patterns:** "Last Friday of month" and "first Monday of month" are common scheduling needs that AND logic makes trivial.

3. **Extended syntax alignment:** The `#n` and `#L` syntax (e.g., `FRI#L`) assumes AND logic. Using OR would make these expressions behave unexpectedly.

4. **Original intent:** V6 Unix cron used AND logic. The OR behavior was arguably a misinterpretation that became entrenched.

5. **Clear migration path:** `DowOrDom` parser option provides explicit opt-in for legacy behavior.

## Consequences

### Positive
- Consistent semantics across all cron fields
- Enables intuitive patterns (last Friday, first Monday, Friday the 13th)
- Aligns with original V6 cron behavior
- Extended syntax (`#n`, `#L`) works naturally

### Negative
- **Breaking change** from robfig/cron and Vixie cron behavior
- Existing expressions with both DOM and DOW restricted will change behavior
- Users who unknowingly relied on OR logic may have jobs that stop running as expected

### Migration

Users depending on OR behavior can use the `DowOrDom` parser option:

```go
parser := cron.NewParser(
    cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.DowOrDom,
)
c := cron.New(cron.WithParser(parser))
// Now "0 0 15 * FRI" means "15th OR any Friday" (legacy behavior)
```

### Risk Assessment

The breaking change risk is mitigated by:

1. **Low intentional usage:** Few users intentionally use OR logic. Most either:
   - Use `*` for DOM or DOW (unaffected)
   - Copied expressions without understanding the OR behavior

2. **Semantic versioning:** Released as v0.9.0 (pre-1.0), signaling instability

3. **Prominent documentation:** Change documented in README, CHANGELOG, MIGRATION.md, and release notes

4. **Easy diagnosis:** Jobs that stop running can be traced to this change via documentation

## Implementation

```go
// dayMatches returns true if schedule matches the given time
func dayMatches(s *SpecSchedule, t time.Time) bool {
    domMatch := /* day-of-month matches */
    dowMatch := /* day-of-week matches */

    // Legacy OR mode (robfig/cron compatibility)
    if s.DowOrDom {
        if s.Dom&starBit > 0 || s.Dow&starBit > 0 {
            return domMatch && dowMatch  // Wildcard: AND
        }
        return domMatch || dowMatch  // Both restricted: OR
    }

    // Default AND mode: both must match
    return domMatch && dowMatch
}
```

## Related

- [#277](https://github.com/netresearch/go-cron/issues/277): Original feature request
- [PR#279](https://github.com/netresearch/go-cron/pull/279): Implementation
- [docs/MIGRATION.md](../MIGRATION.md#domdow-and-logic-277): Migration guide
- ADR-007: nW syntax skip invalid days (related day-matching decision)
