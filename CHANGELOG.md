# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

This is a fork of [robfig/cron](https://github.com/robfig/cron) with additional
features, bug fixes, and modernization improvements.

## [Unreleased]

### Planned for v2
- Context-aware Job interface with graceful shutdown support

## [0.7.0] - 2025-12-16

### Added
- **Year field support**: Full year field in cron expressions (e.g., `0 0 1 1 * 2025`) (#229)
- **Jenkins H hash expressions**: Load distribution with deterministic hashing (e.g., `H H * * *`) (#230)
- **Extended cron syntax**: Support for `#n` (nth weekday), `#L` (last weekday of month),
  `L` (last day of month), and `W` (nearest weekday) modifiers
- **Schedule introspection API**: Query schedule metadata and field constraints (#210)
- **Validation API**: `Validate()` and `ValidateSpecs()` for cron expression validation (#198)
- **Run-once jobs**: `WithRunOnce` option for single-execution scheduling
- **Schedule.Prev() method**: Calculate previous execution time (inverse of `Next()`)
- **Entry options**: `WithPrev` and `WithRunImmediately` for fine-grained entry control
- **IsRunning() method**: Query scheduler running state
- **WithSecondOptional parser option**: Flexible 5 or 6-field cron expressions
- **Sunday=7 support**: Accept `7` as Sunday in day-of-week field (in addition to `0`)
- **Jitter wrappers**: `WithJitter` and `WithJitterByHash` to prevent thundering herd

### Fixed
- **Test stability**: Eliminated flaky timing in `SkipIfStillRunning` and `StopAndWait` tests

### Changed
- **ScheduleWithPrev interface**: Now optional via interface assertion for backward compatibility
  with custom Schedule implementations that don't implement `Prev()`
- **PanicWithStack**: Added type alias for backward compatibility with code referencing
  the internal panic wrapper type
- **Year field storage**: Sparse storage for memory efficiency with expanded year bounds

### Documentation
- Added cookbook with practical recipes for common patterns
- Added Architecture Decision Records (ADRs) for key design decisions
- Added real-time integration tests documentation

## [0.6.1] - 2025-12-03

### Changed
- **Go toolchain**: Updated from go1.25.0 to go1.25.5
- **CodeQL action**: Upgraded from v3.28.0 to v4.31.6
- **CodeQL workflow**: Added explicit workflow file for shields.io badge compatibility

## [0.6.0] - 2025-12-03

### Breaking Changes
- **RetryWithBackoff semantics**: `maxRetries=0` now means "no retries" (execute once, fail on panic).
  Previously `0` meant unlimited retries, which was a DoS risk.
  - **Migration**: Use `maxRetries=-1` for unlimited retries (explicit opt-in)
  - **Rationale**: Zero-value safety - forgotten configs now fail-fast instead of retrying forever

### Added
- **Min-heap scheduling**: O(log n) insertion/removal, O(1) next job lookup (upstream PR #423)
- **Index map compaction**: Automatic cleanup of index maps after frequent entry removals
- **WithClock option**: Inject custom time source for deterministic testing
- **WithMaxSearchYears option**: Configure how many years schedule matching searches before giving up
- **WithLogLevel option for Recover**: Configure log level (Error/Info) for recovered panics
- **WithMinEveryInterval option**: Configure minimum interval for `@every` expressions
  - Allow sub-second intervals for testing: `WithMinEveryInterval(0)` or `WithMinEveryInterval(100*time.Millisecond)`
  - Enforce longer minimums for rate limiting: `WithMinEveryInterval(time.Minute)`
- **EveryWithMin function**: Create constant delay schedules with custom minimum interval
- **Parser.WithMinEveryInterval**: Configure minimum interval on parser level
- **StandardParser function**: Get a copy of the standard parser for customization
- **StopWithTimeout**: Graceful shutdown with configurable timeout
- **StopAndWait**: Convenience method for blocking until all jobs complete
- **Context support**: `JobWithContext` interface and `WithContext` option
- **Job metadata**: `WithName` and `WithTags` options for job identification
- **RetryWithBackoff wrapper**: Exponential backoff retry for transient failures
- **CircuitBreaker wrapper**: Prevent cascading failures with automatic recovery
- **WithMaxEntries option**: Limit maximum entries to prevent memory exhaustion
- **Observability hooks**: `WithObservability` option for metrics integration
- **TryNewParser/MustNewParser**: Safe and panic-on-error parser constructors
- **Timeout callback**: Optional callback when job times out
- **Benchmark suite**: Comprehensive benchmark tests for parser, scheduler, and job operations
- **CI benchmarks**: CI job to run benchmarks and upload results as artifacts
- **Input validation**: Maximum spec length limit (1024 chars) to prevent DoS
- **Timeout JobWrapper**: `chain.Timeout(duration)` for job execution time limits
- **slog adapter**: `SlogLogger` for structured logging with Go 1.21+ slog
- **Multi-platform CI**: Windows, macOS, and Linux testing
- **ExampleTimeout_withContext**: Demonstrates idiomatic context-based cancellation pattern
- **Fuzz tests**: Fuzz testing for parser and scheduler robustness
- **Enterprise security**: SLSA provenance, gosec, govulncheck, gitleaks, trivy scanning

### Fixed
- **Panic on NewParser with no fields**: Returns error instead of panicking
- **Entry limit race condition**: Use atomic CAS for thread-safe limit checking
- **Flaky tests**: Fixed timing-sensitive tests with channel synchronization
  - `TestChainSkipIfStillRunning`
  - `TestStopAndWait`
  - `TestTimeoutWithContext`
  - `TestFakeClockSchedulerIntegration` subtests
- **Heap corruption**: Prevent stale heapIndex in Update operations
- **Time backwards handling**: Scheduler iterates over copy when time goes backwards
- **EntryID overflow**: Skip EntryID 0 on uint64 overflow

### Changed
- **EntryID uint64**: Changed from `int` to `uint64` for larger job capacity
- **slices package**: Uses Go 1.21+ `slices.SortFunc` and `slices.DeleteFunc`
- **Linting**: Uses golangci-lint v2.6.1 with modern rule set
- **Timeout wrapper logging**: Enhanced message clarifies "goroutine still running in background"
- **Parser complexity reduction**: Extracted helpers for better maintainability
- **safeExecute consolidation**: Unified panic recovery across codebase

### Security
- **Timezone validation**: Character and length restrictions for timezone strings to prevent DoS
- **RetryWithBackoff DoS prevention**: Zero-value is now safe default (no retries vs unlimited)
- **Enterprise-grade CI**: GitHub Actions hardened with SHA pinning and SLSA

## [0.5.0] - 2025-11-25

Initial release of netresearch/go-cron fork.

### Added
- **Step range validation**: Step size must be less than range size (upstream #543)
- **Minimum duration enforcement**: `@every` requires at least 1 second duration
- **DST handling**: ISC cron-compatible behavior for spring forward transitions
- **Time backwards handling**: Scheduler handles system time moving backwards gracefully
- **GitHub Actions CI**: Migrated from Travis CI with comprehensive workflow

### Fixed
- **Panic on nil receiver** (upstream #551): `Entry.Run()` no longer panics
- **Panic on empty timezone** (upstream #554): Parser returns error instead
- **Panic on timezone-only spec** (upstream #555): Parser returns error instead
- **removeEntry optimization**: Pre-allocates slice to reduce allocations
- **SkipIfStillRunning graceful quit**: Fixed jobWrapper cleanup behavior

### Changed
- **Go version**: Requires Go 1.25+
- **Module path**: Changed to `github.com/netresearch/go-cron`
- **Code style**: Applied De Morgan's law optimizations
- **Spelling**: Corrected 'cancelled' to 'canceled' (American English)

### Security
- Integrated gosec, govulncheck, gitleaks, and trivy security scanning

## Differences from upstream robfig/cron

This fork includes all features from robfig/cron v3 plus:

| Feature | robfig/cron | netresearch/go-cron |
|---------|-------------|---------------------|
| Scheduling algorithm | O(n) sort | O(log n) min-heap |
| Custom time source | No | WithClock option |
| Step range validation | No | Yes |
| @every minimum duration | No | 1 second (configurable) |
| Timezone validation | No | Yes |
| Input length limits | No | Yes |
| Timeout wrapper | No | Yes |
| slog adapter | No | Yes |
| EntryID type | int | uint64 |
| DST spring forward | Skips | ISC-compatible |
| Time backwards handling | No | Yes |
| Multi-platform CI | Linux only | Win/Mac/Linux |

## Migration from robfig/cron

1. Update import path:
   ```go
   // Before
   import "github.com/robfig/cron/v3"

   // After
   import "github.com/netresearch/go-cron"
   ```

2. Update `EntryID` usage if storing as `int`:
   ```go
   // Before
   var id int = c.AddJob(...)

   // After
   var id cron.EntryID = c.AddJob(...) // or uint64
   ```

3. Review cron expressions for step validation:
   ```go
   // Now returns error (step >= range size)
   _, err := cron.ParseStandard("*/60 * * * *") // Error: step (60) must be less than range size (60)
   ```

[Unreleased]: https://github.com/netresearch/go-cron/compare/v0.7.0...HEAD
[0.7.0]: https://github.com/netresearch/go-cron/compare/v0.6.1...v0.7.0
[0.6.1]: https://github.com/netresearch/go-cron/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/netresearch/go-cron/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/netresearch/go-cron/releases/tag/v0.5.0
