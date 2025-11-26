# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

This is a fork of [robfig/cron](https://github.com/robfig/cron) with additional
features, bug fixes, and modernization improvements.

## [Unreleased]

### Planned for v2
- Context-aware Job interface with graceful shutdown support

## [1.0.0] - Unreleased

### Added
- **Min-heap scheduling**: O(log n) insertion/removal, O(1) next job lookup (upstream PR #423)
- **WithClock option**: Inject custom time source for deterministic testing
- **Benchmark suite**: Comprehensive benchmark tests for parser, scheduler, and job operations
- **CI benchmarks**: CI job to run benchmarks and upload results as artifacts
- **Input validation**: Maximum spec length limit (1024 chars) to prevent DoS
- **Timeout JobWrapper**: `chain.Timeout(duration)` for job execution time limits
- **slog adapter**: `SlogLogger` for structured logging with Go 1.21+ slog
- **Multi-platform CI**: Windows, macOS, and Linux testing

### Fixed
- **Panic on NewParser with no fields**: Returns error instead of panicking

### Changed
- **EntryID uint64**: Changed from `int` to `uint64` for larger job capacity
- **slices package**: Uses Go 1.21+ `slices.SortFunc` and `slices.DeleteFunc`
- **Linting**: Uses golangci-lint v2.6.1 with modern rule set

### Security
- **Timezone validation**: Character and length restrictions for timezone strings to prevent DoS

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
| @every minimum duration | No | 1 second |
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

[Unreleased]: https://github.com/netresearch/go-cron/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/netresearch/go-cron/compare/v0.5.0...v1.0.0
[0.5.0]: https://github.com/netresearch/go-cron/releases/tag/v0.5.0
