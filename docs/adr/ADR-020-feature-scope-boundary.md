# ADR-020: Feature Scope and Boundary Definition

## Status
Accepted

## Date
2026-01-18

## Context

As go-cron gains adoption, users request features inspired by other scheduling ecosystems (Node.js, Python, Java, Ruby). A cross-language analysis of 40+ scheduler libraries revealed common feature requests:

| Feature | Examples | Frequency |
|---------|----------|-----------|
| Persistence | Agenda (MongoDB), BullMQ (Redis), Quartz (JDBC) | Very High |
| Distributed locking | Sidekiq-Pro, Quartz Clustering, Dkron | High |
| Job queues/workers | Celery, Bull, Sidekiq | High |
| Web UI/Dashboard | Airflow, Sidekiq, Bull Board | Medium |
| Missed job handling | APScheduler, Quartz misfire | Medium |
| Human-readable syntax | Agenda, Celery | Low |

**The core question**: Which features belong in go-cron, and which should be left to external tools?

Without clear boundaries, go-cron risks:
- **Scope creep**: Becoming a full job orchestration system
- **Dependency bloat**: Requiring Redis, PostgreSQL, or other infrastructure
- **Maintenance burden**: Supporting features outside core competency
- **Breaking the drop-in promise**: Diverging from simple robfig/cron replacement

## Decision

Define go-cron's scope as **in-process cron scheduling** with clear boundaries:

### IN SCOPE (Core Competency)

Features that are **pure scheduling logic** with no external dependencies:

| Category | Features | Rationale |
|----------|----------|-----------|
| **Cron Parsing** | Standard, Quartz, seconds, years, hash expressions | Core functionality |
| **Time Calculation** | Next/prev runs, DST handling, timezones, wraparound | Core functionality |
| **Job Execution** | Goroutine-based, context support, cancellation | Core functionality |
| **Job Lifecycle** | Wrappers/decorators, panic recovery, retry, circuit breaker | Composable middleware |
| **Observability** | Hooks for metrics/logging (bring your own backend) | Integration points |
| **Testing** | FakeClock, deterministic time control | Developer experience |
| **Introspection** | Schedule analysis, warnings, next N times | Debugging/validation |
| **Missed Job Policy** | With user-provided last-run time (no built-in persistence) | Stateless catch-up |

### OUT OF SCOPE (External Concerns)

Features requiring **external infrastructure** or **different problem domains**:

| Category | Why Out of Scope | Use Instead |
|----------|------------------|-------------|
| **Persistence** | Requires DB/Redis dependency, violates drop-in compatibility | User's own DB + ObservabilityHooks |
| **Distributed Locking** | Requires consensus system (Redis, etcd, Postgres) | redsync, etcd, pg advisory locks |
| **Job Queues** | Different problem: task distribution, not scheduling | asynq, machinery, river, Temporal |
| **Worker Pools** | Different problem: execution scaling, not timing | Go worker pool libraries |
| **Web UI/Dashboard** | Visualization concern, not scheduling | Prometheus + Grafana |
| **Message Brokers** | Integration concern, not scheduling | Direct integration in user code |
| **Rate Limiting** | Execution concern, not scheduling | Bottleneck pattern in wrappers |

### INTEGRATION POINTS (Document, Don't Build)

Provide documentation showing how to integrate go-cron with external systems:

| Integration | Documentation |
|-------------|---------------|
| Persistence | [PERSISTENCE_GUIDE.md](../PERSISTENCE_GUIDE.md) |
| Distributed Locking | [DISTRIBUTED_LOCKING_GUIDE.md](../DISTRIBUTED_LOCKING_GUIDE.md) |
| Prometheus Metrics | [COOKBOOK.md](../COOKBOOK.md#prometheus-metrics) |
| Graceful Shutdown | [OPERATIONS.md](../OPERATIONS.md#graceful-shutdown) |

## Consequences

### Positive

- **Clear focus**: go-cron excels at one thing: in-process cron scheduling
- **Zero dependencies**: No external infrastructure required for core functionality
- **Drop-in replacement**: Maintains promise of simple robfig/cron migration
- **Composable**: Users integrate with their existing infrastructure
- **Maintainable**: Smaller surface area, fewer edge cases
- **Predictable**: Behavior is deterministic and testable

### Negative

- **Feature requests declined**: Some users want "batteries included"
- **Documentation burden**: Must explain integration patterns
- **Perception**: May seem "less powerful" than Quartz or Celery

### Neutral

- **Ecosystem position**: go-cron is a scheduling engine, not an orchestration platform
- **Competition**: Different niche than Dkron, Temporal, or Airflow

## Alternatives Considered

### 1. Full-Featured Scheduler (Like Quartz)

Build persistence, clustering, and misfire handling directly into go-cron.

**Rejected because:**
- Introduces mandatory dependencies (database driver, Redis client)
- Breaks drop-in compatibility with robfig/cron
- Massive increase in maintenance burden
- Already solved by Dkron, Temporal, Airflow

### 2. Plugin Architecture

Support optional plugins for persistence, distributed locking, etc.

**Rejected because:**
- Plugin interfaces are hard to design well
- Version compatibility issues between core and plugins
- Still increases maintenance burden
- Go's interface system already enables this via wrappers

### 3. Separate Modules

Publish `go-cron/redis`, `go-cron/postgres`, etc. as separate Go modules.

**Partially accepted:**
- May publish `go-cron/prometheus` for metrics (optional dependency)
- Will NOT publish persistence modules (too many backends to support)

## Guiding Principles

When evaluating future feature requests, apply these criteria:

1. **Does it require external infrastructure?** → Out of scope
2. **Is it pure scheduling logic?** → In scope
3. **Can it be implemented as a wrapper/hook?** → Prefer that pattern
4. **Does it break drop-in compatibility?** → Strong bias against
5. **Is the maintenance burden proportional to value?** → Consider carefully

## Visual Boundary

```
┌─────────────────────────────────────────────────────────────────┐
│                     go-cron SCOPE                               │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │                    IN SCOPE                              │   │
│  │  • Cron expression parsing                               │   │
│  │  • Time calculation (next/prev, DST, timezones)          │   │
│  │  • In-process job execution (goroutines)                 │   │
│  │  • Job wrappers (retry, timeout, skip, circuit breaker)  │   │
│  │  • Observability hooks (OnJobStart, OnJobComplete)       │   │
│  │  • Testing utilities (FakeClock)                         │   │
│  │  • Schedule introspection (AnalyzeSpec, NextN)           │   │
│  │  • Missed job policy (with user-provided last run)       │   │
│  └─────────────────────────────────────────────────────────┘   │
│                              │                                  │
│                    Integration Points                           │
│                              │                                  │
│  ┌───────────┬───────────┬──┴────────┬────────────┐            │
│  │           │           │           │            │            │
│  ▼           ▼           ▼           ▼            ▼            │
│ Your DB   redsync    Prometheus   asynq      Temporal          │
│ (persist) (locking)  (metrics)   (queues)   (workflows)        │
│                                                                 │
│  └──────────────────────────────────────────────────────────┘  │
│                       OUT OF SCOPE                              │
│              (Use these external tools instead)                 │
└─────────────────────────────────────────────────────────────────┘
```

## References

- Cross-language scheduler comparison (research, Jan 2026)
- [robfig/cron issue #561](https://github.com/robfig/cron/issues/561): "Looking for maintained version"
- [Quartz Scheduler](http://www.quartz-scheduler.org/): Example of full-featured scheduler
- [Dkron](https://dkron.io/): Distributed cron for Go (out of scope alternative)
- [Temporal](https://temporal.io/): Workflow orchestration (out of scope alternative)
