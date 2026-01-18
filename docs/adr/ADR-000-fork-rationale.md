# ADR-000: Fork of robfig/cron

## Status
Accepted

## Date
2025-11-01

## Context

[robfig/cron](https://github.com/robfig/cron) is the most widely-used cron library for Go, with ~13k GitHub stars and broad adoption. However, the project has become effectively unmaintained:

- **Stale PRs**: Critical bug fixes and improvements have languished for years without merge
- **No releases**: Last release (v3.0.1) was in 2020; no tagged releases since
- **Unresponsive**: Issue and PR comments go unanswered

**Specific issues motivating the fork:**

| Issue | Impact | Status in robfig/cron |
|-------|--------|----------------------|
| [#554](https://github.com/robfig/cron/issues/554), [#555](https://github.com/robfig/cron/issues/555) | Panic on `TZ=` prefix parsing | Open since 2023 |
| [#551](https://github.com/robfig/cron/issues/551) | `Entry.Run()` bypasses chain decorators | Open since 2023 |
| [#541](https://github.com/robfig/cron/pull/541) | DST spring-forward jobs skipped entirely | Open PR since 2022 |
| [#277](https://github.com/robfig/cron/issues/277) | DOM/DOW OR logic causes confusion | Discussed but unresolved |

**Additional limitations:**
- No deterministic testing support (real clock only)
- No observability hooks for metrics integration
- O(n) entry removal performance
- No schedule introspection capabilities

## Decision

Create and maintain a fork at `github.com/netresearch/go-cron` that:

1. **Fixes critical bugs** - Immediately address panics and incorrect behavior
2. **Merges valuable PRs** - Incorporate community improvements stuck in review
3. **Maintains API compatibility** - Drop-in replacement for `robfig/cron/v3`
4. **Adds missing features** - Testing support, observability, performance improvements
5. **Documents decisions** - Use ADRs to explain design choices

**Maintainer:** [Netresearch](https://www.netresearch.de/) - a software development company using this library in production systems.

**Versioning:** Semantic versioning starting at v0.x to indicate active development. Will reach v1.0 when API stabilizes and production usage is proven.

## Consequences

### Positive

- **Immediate bug fixes**: Users get working software without waiting for upstream
- **Active maintenance**: Issues and PRs are reviewed and addressed
- **Enhanced features**: Testing, observability, and performance improvements
- **Transparent decisions**: ADRs document why things work the way they do
- **Community contribution**: Fork accepts PRs and community input

### Negative

- **Ecosystem fragmentation**: Another cron library in the Go ecosystem
- **Maintenance burden**: Netresearch commits to ongoing maintenance
- **Upstream divergence**: May become incompatible with future robfig/cron changes
- **Discovery**: Users must find this fork among alternatives

### Neutral

- **Migration effort**: Existing users must update import paths
- **Trust establishment**: New project must build reputation

## Alternatives Considered

### 1. Wait for Upstream Fixes

- **Rejected**: Issues have been open for 1-3+ years with no response
- No indication of renewed maintenance activity
- Production systems can't wait indefinitely

### 2. Contribute to Upstream Only

- **Rejected**: PRs are not being reviewed or merged
- Multiple contributors have tried; PRs languish
- Cannot fix issues without merge access

### 3. Use Alternative Libraries

Evaluated alternatives:

| Library | Issue |
|---------|-------|
| `go-co-op/gocron` | Different API, not drop-in compatible |
| `jasonlvhit/gocron` | Abandoned, security concerns |
| Custom implementation | Unnecessary when robfig/cron core is solid |

- **Rejected**: robfig/cron's core design is sound; only maintenance is lacking

### 4. Request Maintainer Access

- **Attempted**: No response from maintainer
- Even with access, would inherit technical debt without clear direction

## Upstream Relationship

This fork:
- Credits robfig/cron as the original work
- Maintains LICENSE attribution
- Documents all changes from upstream
- Would consider reconciliation if upstream becomes active again

## References

- Original repository: https://github.com/robfig/cron
- This fork: https://github.com/netresearch/go-cron
- Netresearch: https://www.netresearch.de/
