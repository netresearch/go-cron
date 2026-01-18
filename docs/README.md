# go-cron Documentation

Welcome to the go-cron documentation. This guide helps you find the right documentation for your needs.

## Quick Start

| If you want to... | Read... |
|-------------------|---------|
| Get started quickly | [README.md](../README.md) |
| Migrate from robfig/cron | [MIGRATION.md](MIGRATION.md) |
| See practical examples | [COOKBOOK.md](COOKBOOK.md) |
| Understand the API | [API_REFERENCE.md](API_REFERENCE.md) |

## Documentation Index

### User Guides

| Document | Description |
|----------|-------------|
| [COOKBOOK.md](COOKBOOK.md) | Practical recipes for common patterns |
| [MIGRATION.md](MIGRATION.md) | Step-by-step migration from robfig/cron |
| [FAQ.md](FAQ.md) | Frequently asked questions |
| [DST_HANDLING.md](DST_HANDLING.md) | How daylight saving time transitions are handled |

### Operations & Troubleshooting

| Document | Description |
|----------|-------------|
| [OPERATIONS.md](OPERATIONS.md) | Production deployment, graceful shutdown, monitoring |
| [TROUBLESHOOTING.md](TROUBLESHOOTING.md) | Common issues and debugging techniques |
| [TESTING_GUIDE.md](TESTING_GUIDE.md) | Testing strategies with FakeClock |

### Technical Reference

| Document | Description |
|----------|-------------|
| [API_REFERENCE.md](API_REFERENCE.md) | Complete public API documentation |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Internal design, data structures, algorithms |
| [PERFORMANCE.md](PERFORMANCE.md) | Benchmarks and performance characteristics |
| [PROJECT_INDEX.md](PROJECT_INDEX.md) | Complete file listing with descriptions |

### Architecture Decision Records (ADRs)

ADRs document significant architectural decisions. Read these to understand *why* things are designed the way they are.

| ADR | Decision |
|-----|----------|
| [ADR-000](adr/ADR-000-fork-rationale.md) | Why this fork exists |
| [ADR-001](adr/ADR-001-heap-scheduling.md) | Min-heap for O(log n) scheduling |
| [ADR-002](adr/ADR-002-panic-for-failures.md) | Panic-based job failure signaling |
| [ADR-003](adr/ADR-003-async-observability.md) | Asynchronous observability hooks |
| [ADR-004](adr/ADR-004-functional-options.md) | Functional options pattern |
| [ADR-005](adr/ADR-005-decorator-pattern.md) | Decorator pattern for job wrappers |
| [ADR-006](adr/ADR-006-sync-map-cache.md) | sync.Map for parser cache |
| [ADR-007](adr/ADR-007-nw-skip-invalid-days.md) | nW syntax skips invalid months |
| [ADR-008](adr/ADR-008-dom-dow-and-logic.md) | DOM/DOW AND logic by default |
| [ADR-009](adr/ADR-009-entry-id-sentinel.md) | Entry ID sentinel value |
| [ADR-010](adr/ADR-010-channel-synchronization.md) | Channel-based synchronization |
| [ADR-011](adr/ADR-011-dual-index-maps.md) | Dual-index maps for O(1) lookup |
| [ADR-012](adr/ADR-012-index-compaction.md) | Map index compaction for memory reclamation |
| [ADR-013](adr/ADR-013-heap-index-tracking.md) | Entry stores heap index for O(log n) removal |
| [ADR-014](adr/ADR-014-max-idle-duration.md) | Maximum idle duration (100,000 hours) |
| [ADR-015](adr/ADR-015-zero-time-sentinel.md) | Zero time as schedule exhaustion sentinel |
| [ADR-016](adr/ADR-016-dst-normalization.md) | DST handling via normalization |
| [ADR-017](adr/ADR-017-job-with-context.md) | Optional JobWithContext interface |
| [ADR-018](adr/ADR-018-run-flags.md) | Run-immediately and run-once entry flags |
| [ADR-019](adr/ADR-019-atomic-entry-limit.md) | Atomic CAS for entry count limiting |

See [adr/README.md](adr/README.md) for the complete ADR index and template.

## Reading Order

### New Users
1. [README.md](../README.md) - Overview and installation
2. [COOKBOOK.md](COOKBOOK.md) - Common patterns
3. [FAQ.md](FAQ.md) - Common questions

### Migrating from robfig/cron
1. [MIGRATION.md](MIGRATION.md) - What's different
2. [DST_HANDLING.md](DST_HANDLING.md) - Behavior changes

### Going to Production
1. [OPERATIONS.md](OPERATIONS.md) - Deployment guide
2. [TESTING_GUIDE.md](TESTING_GUIDE.md) - Testing strategies
3. [TROUBLESHOOTING.md](TROUBLESHOOTING.md) - When things go wrong

### Contributors
1. [ARCHITECTURE.md](ARCHITECTURE.md) - How it works internally
2. [adr/](adr/) - Why things are designed this way
3. [../CONTRIBUTING.md](../CONTRIBUTING.md) - How to contribute

## External Resources

- [pkg.go.dev documentation](https://pkg.go.dev/github.com/netresearch/go-cron)
- [GitHub repository](https://github.com/netresearch/go-cron)
- [Issue tracker](https://github.com/netresearch/go-cron/issues)
