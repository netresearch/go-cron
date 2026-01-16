# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

This is a fork of [robfig/cron](https://github.com/robfig/cron) with additional
features, bug fixes, and modernization improvements.

## [Unreleased]

### Planned for v2
- Context-aware Job interface with graceful shutdown support

## [0.9.0] - 2026-01-16

### Added
- **Wraparound ranges** ([#276], [PR#278]): Cyclic fields now support ranges where start > end
  - Hours: `22-2` spans midnight (22, 23, 0, 1, 2)
  - Day-of-week: `FRI-MON` spans weekend (FRI, SAT, SUN, MON)
  - Month: `NOV-FEB` spans year boundary (NOV, DEC, JAN, FEB)
  - Supports step values: `22-2/2` = every 2 hours from 10pm to 2am
- **`DowOrDom` parser option** ([#277], [PR#279]): Legacy OR mode for DOM/DOW matching
  - Provides robfig/cron compatibility for users depending on OR behavior

### Changed
- **BREAKING: DOM/DOW matching now uses AND logic** ([#277], [PR#279]): When both day-of-month
  and day-of-week are specified, both must match. This is consistent with all other cron fields
  and enables useful patterns:
  - `0 0 25-31 * FRI` = last Friday of month
  - `0 0 1-7 * MON` = first Monday of month
  - `0 0 13 * FRI` = Friday the 13th
  - Use `DowOrDom` parser option for legacy OR behavior

### Fixed
- **Test reliability** ([PR#278]): Fixed flaky chain tests using channel synchronization

[#276]: https://github.com/netresearch/go-cron/issues/276
[#277]: https://github.com/netresearch/go-cron/issues/277
[PR#278]: https://github.com/netresearch/go-cron/pull/278
[PR#279]: https://github.com/netresearch/go-cron/pull/279

## [0.8.0] - 2025-12-26

### Added
- **`FullParser()` convenience function** ([PR#266]): Pre-configured parser with all features enabled
  (seconds, year, hash, extended syntax)
- **`YearOptional` parser option** ([PR#266]): Auto-detect year field by value >= 100

### Changed
- **Security hardening**: Improved workflow permissions and branch protection documentation

### Documentation
- Updated changelog with v0.7.1 contributor attribution

[PR#266]: https://github.com/netresearch/go-cron/pull/266

## [0.7.1] - 2025-12-17

### Fixed
- **Synchronize add/remove operations while cron is running** ([#262], [PR#264]): Fixed race condition
  when adding or removing jobs while the scheduler is running. Operations now use synchronous
  request/reply channels to ensure completion before returning. Thanks to [@jrouzierinverse] for
  the contribution!
- **Test reliability**: Fixed flaky `TestRunOnce_AddWhileRunning` test using polling instead of
  sleep, with increased timeout for Windows CI

### Changed
- **CI: Skip gitleaks on fork PRs**: Fork PRs now pass CI without requiring secrets, as GitHub
  Actions doesn't expose secrets to forks for security reasons

[#262]: https://github.com/netresearch/go-cron/issues/262
[PR#264]: https://github.com/netresearch/go-cron/pull/264
[@jrouzierinverse]: https://github.com/jrouzierinverse

## [0.7.0] - 2025-12-16

### Added
- **Extended cron syntax** ([#224], [#225], [PR#259]): Quartz/Jenkins-style modifiers as opt-in parser options
  - `#n` nth weekday of month (e.g., `FRI#3` = 3rd Friday) — see [`ExampleDowNth`]
  - `#L` last weekday of month (e.g., `FRI#L` = last Friday)
  - `L` last day of month, `L-n` nth from last — see [`ExampleDomL`]
  - `nW` nearest weekday, `LW` last weekday — see [`ExampleDomW`], [ADR-007]
  - New parser options: `DowNth`, `DowLast`, `DomL`, `DomW`, `Extended`
- **Year field support** ([#229], [PR#250], [PR#253]): Full year field in cron expressions
  - Sparse map storage for memory efficiency (years 1–2147483647)
  - Examples: `0 0 1 1 * 2025`, `0 0 * * * 2025-2030` — see [`ExampleNewParser_yearField`]
- **Jenkins H hash expressions** ([#230], [PR#251]): Deterministic load distribution
  - Hash-based scheduling: `H H * * *` distributes jobs across time
  - Configurable hash key: `Parser.WithHashKey()` — see [`ExampleNewParser_hash`]
- **Schedule introspection API** ([#210], [PR#249]): Query schedule metadata and field constraints
  - `Bounds()`, `Fields()`, `Matches()` for runtime schedule analysis
- **Validation API** ([#198], [PR#248]): Validate cron expressions without creating schedules
  - `Validate(spec)` single expression, `ValidateSpecs(specs...)` bulk validation
- **Run-once jobs** ([#231], [PR#252]): Single-execution scheduling with automatic removal
  - `WithRunOnce()` option, `AddOnceFunc()`, `AddOnceJob()` — see [`ExampleWithRunOnce`]
- **Schedule.Prev() method** ([#222], [PR#246]): Calculate previous execution time (inverse of `Next()`)
  - Useful for missed job detection — see [`ExampleScheduleWithPrev_Prev_detectMissed`]
- **Entry options** ([#221], [PR#245]): Fine-grained entry control
  - `WithPrev` stores previous run time — see [`ExampleWithPrev`]
  - `WithRunImmediately` triggers immediate first execution — see [`ExampleWithRunImmediately`]
- **IsRunning() method** ([#232], [PR#244]): Query scheduler running state — see [`ExampleCron_IsRunning`]
- **WithSecondOptional parser option** ([#220], [PR#242]): Flexible 5 or 6-field cron expressions
- **Sunday=7 support** ([#234], [PR#243]): Accept `7` as Sunday in day-of-week field (POSIX extension)
  - See [`ExampleParseStandard_sundayFormats`]
- **Jitter wrappers** ([#227], [PR#258]): Prevent thundering herd with randomized delays
  - `Jitter(maxJitter)`, `JitterWithLogger()` — see [`ExampleJitter`]

### Fixed
- **Test stability** ([PR#256]): Eliminated flaky timing in `SkipIfStillRunning` and `StopAndWait` tests
  using proper channel synchronization

### Changed
- **ScheduleWithPrev interface** ([PR#260]): Now optional via interface assertion for backward
  compatibility with custom Schedule implementations that don't implement `Prev()`
- **PanicWithStack** ([PR#261]): Added type alias for backward compatibility with code referencing
  the internal panic wrapper type
- **Year field storage** ([PR#253]): Sparse map storage for memory efficiency with expanded bounds

### Documentation
- Added [COOKBOOK] ([#204]) with practical recipes for common patterns
- Added [Architecture Decision Records][ADRs] ([#193]) for key design decisions
- Added [TESTING_GUIDE] ([#211]) with FakeClock usage and real-time integration tests

[#193]: https://github.com/netresearch/go-cron/issues/193
[#198]: https://github.com/netresearch/go-cron/issues/198
[#204]: https://github.com/netresearch/go-cron/issues/204
[#210]: https://github.com/netresearch/go-cron/issues/210
[#211]: https://github.com/netresearch/go-cron/issues/211
[#220]: https://github.com/netresearch/go-cron/issues/220
[#221]: https://github.com/netresearch/go-cron/issues/221
[#222]: https://github.com/netresearch/go-cron/issues/222
[#224]: https://github.com/netresearch/go-cron/issues/224
[#225]: https://github.com/netresearch/go-cron/issues/225
[#227]: https://github.com/netresearch/go-cron/issues/227
[#229]: https://github.com/netresearch/go-cron/issues/229
[#230]: https://github.com/netresearch/go-cron/issues/230
[#231]: https://github.com/netresearch/go-cron/issues/231
[#232]: https://github.com/netresearch/go-cron/issues/232
[#234]: https://github.com/netresearch/go-cron/issues/234
[PR#242]: https://github.com/netresearch/go-cron/pull/242
[PR#243]: https://github.com/netresearch/go-cron/pull/243
[PR#244]: https://github.com/netresearch/go-cron/pull/244
[PR#245]: https://github.com/netresearch/go-cron/pull/245
[PR#246]: https://github.com/netresearch/go-cron/pull/246
[PR#248]: https://github.com/netresearch/go-cron/pull/248
[PR#249]: https://github.com/netresearch/go-cron/pull/249
[PR#250]: https://github.com/netresearch/go-cron/pull/250
[PR#251]: https://github.com/netresearch/go-cron/pull/251
[PR#252]: https://github.com/netresearch/go-cron/pull/252
[PR#253]: https://github.com/netresearch/go-cron/pull/253
[PR#256]: https://github.com/netresearch/go-cron/pull/256
[PR#258]: https://github.com/netresearch/go-cron/pull/258
[PR#259]: https://github.com/netresearch/go-cron/pull/259
[PR#260]: https://github.com/netresearch/go-cron/pull/260
[PR#261]: https://github.com/netresearch/go-cron/pull/261
[ADR-007]: docs/adr/ADR-007-nw-skip-invalid-days.md
[ADRs]: docs/adr/
[COOKBOOK]: docs/COOKBOOK.md
[TESTING_GUIDE]: docs/TESTING_GUIDE.md
[`ExampleCron_IsRunning`]: example_test.go#ExampleCron_IsRunning
[`ExampleDomL`]: example_test.go#ExampleDomL
[`ExampleDomW`]: example_test.go#ExampleDomW
[`ExampleDowNth`]: example_test.go#ExampleDowNth
[`ExampleJitter`]: example_test.go#ExampleJitter
[`ExampleNewParser_hash`]: example_test.go#ExampleNewParser_hash
[`ExampleNewParser_yearField`]: example_test.go#ExampleNewParser_yearField
[`ExampleParseStandard_sundayFormats`]: example_test.go#ExampleParseStandard_sundayFormats
[`ExampleScheduleWithPrev_Prev_detectMissed`]: example_test.go#ExampleScheduleWithPrev_Prev_detectMissed
[`ExampleWithPrev`]: example_test.go#ExampleWithPrev
[`ExampleWithRunImmediately`]: example_test.go#ExampleWithRunImmediately
[`ExampleWithRunOnce`]: example_test.go#ExampleWithRunOnce

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

[Unreleased]: https://github.com/netresearch/go-cron/compare/v0.7.1...HEAD
[0.7.1]: https://github.com/netresearch/go-cron/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/netresearch/go-cron/compare/v0.6.1...v0.7.0
[0.6.1]: https://github.com/netresearch/go-cron/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/netresearch/go-cron/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/netresearch/go-cron/releases/tag/v0.5.0
