# Architecture Decision Records

This directory contains Architecture Decision Records (ADRs) documenting significant architectural decisions made in go-cron.

## What is an ADR?

An Architecture Decision Record captures a significant decision made about the architecture, along with its context and consequences. ADRs help:

- **Onboard new contributors** by explaining why things are built a certain way
- **Prevent re-litigation** of decisions that have already been thoroughly considered
- **Document trade-offs** that were evaluated during design
- **Guide future decisions** by establishing precedent and principles

## ADR Index

| ADR | Title | Status | Date |
|-----|-------|--------|------|
| [ADR-000](ADR-000-fork-rationale.md) | Fork of robfig/cron | Accepted | 2025-11 |
| [ADR-001](ADR-001-heap-scheduling.md) | Use Min-Heap for Entry Scheduling | Accepted | 2025-12 |
| [ADR-002](ADR-002-panic-for-failures.md) | Panic-Based Job Failure Signaling | Accepted | 2025-12 |
| [ADR-003](ADR-003-async-observability.md) | Asynchronous Observability Hooks | Accepted | 2025-12 |
| [ADR-004](ADR-004-functional-options.md) | Functional Options Pattern | Accepted | 2025-12 |
| [ADR-005](ADR-005-decorator-pattern.md) | Decorator Pattern for Job Wrappers | Accepted | 2025-12 |
| [ADR-006](ADR-006-sync-map-cache.md) | sync.Map for Parser Cache | Accepted | 2025-12 |
| [ADR-007](ADR-007-nw-skip-invalid-days.md) | nW Syntax Skips Invalid Months | Accepted | 2025-12 |
| [ADR-008](ADR-008-dom-dow-and-logic.md) | DOM/DOW AND Logic by Default | Accepted | 2026-01 |
| [ADR-009](ADR-009-entry-id-sentinel.md) | Entry ID Sentinel Value (0 = Invalid) | Accepted | 2025-12 |
| [ADR-010](ADR-010-channel-synchronization.md) | Channel-Based Synchronization Model | Accepted | 2025-12 |
| [ADR-011](ADR-011-dual-index-maps.md) | Dual-Index Maps for O(1) Lookup | Accepted | 2025-12 |
| [ADR-012](ADR-012-index-compaction.md) | Map Index Compaction | Accepted | 2025-12 |
| [ADR-013](ADR-013-heap-index-tracking.md) | Entry Heap Index Tracking | Accepted | 2025-12 |
| [ADR-014](ADR-014-max-idle-duration.md) | Maximum Idle Duration | Accepted | 2025-12 |
| [ADR-015](ADR-015-zero-time-sentinel.md) | Zero Time as Schedule Exhaustion Sentinel | Accepted | 2025-12 |
| [ADR-016](ADR-016-dst-normalization.md) | DST Handling via Normalization | Accepted | 2025-12 |
| [ADR-017](ADR-017-job-with-context.md) | Optional JobWithContext Interface | Accepted | 2025-12 |
| [ADR-018](ADR-018-run-flags.md) | Run-Immediately and Run-Once Entry Flags | Accepted | 2025-12 |
| [ADR-019](ADR-019-atomic-entry-limit.md) | Atomic CAS for Entry Count Limiting | Accepted | 2025-12 |
| [ADR-020](ADR-020-feature-scope-boundary.md) | Feature Scope and Boundary Definition | Accepted | 2026-01 |

## ADR Template

```markdown
# ADR-NNN: Title

## Status
Accepted | Superseded by ADR-XXX | Deprecated

## Date
YYYY-MM-DD

## Context
What is the issue that we're seeing that is motivating this decision or change?

## Decision
What is the change that we're proposing and/or doing?

## Consequences
What becomes easier or more difficult to do because of this change?

## Alternatives Considered
What other options were evaluated and why were they rejected?
```

## Contributing

When making significant architectural changes:

1. Create a new ADR following the template
2. Number it sequentially (ADR-007, ADR-008, etc.)
3. Add it to the index above
4. Reference the ADR in relevant code comments or PRs
