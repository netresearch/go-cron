# Contributing to go-cron

Thank you for your interest in contributing to go-cron! This document provides guidelines and information for contributors.

## Code of Conduct

This project adheres to the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code.

## How to Contribute

### Reporting Bugs

Before creating a bug report:

1. Check the [existing issues](https://github.com/netresearch/go-cron/issues) to avoid duplicates
2. Ensure you're using the latest version
3. Collect relevant information (Go version, OS, stack traces)

When creating a bug report, include:

- A clear, descriptive title
- Steps to reproduce the issue
- Expected vs. actual behavior
- Code samples or test cases if applicable
- Environment details (Go version, OS)

### Suggesting Enhancements

Enhancement suggestions are welcome! Please:

1. Check existing issues and PRs first
2. Clearly describe the use case
3. Explain why existing functionality doesn't suffice
4. Consider backwards compatibility

### Pull Requests

1. **Fork and clone** the repository
2. **Create a branch** from `main`:
   ```bash
   git checkout -b feature/your-feature-name
   ```
3. **Make your changes** following the coding standards below
4. **Write or update tests** for your changes
5. **Run the test suite**:
   ```bash
   make test
   ```
6. **Run linting**:
   ```bash
   make lint
   ```
7. **Commit with conventional commits**:
   ```bash
   git commit -m "feat: add new scheduling option"
   ```
8. **Push and create a PR** against `main`

## Development Setup

### Prerequisites

- Go 1.25 or later
- golangci-lint (for linting)
- make (for build automation)

### Getting Started

```bash
# Clone your fork
git clone https://github.com/YOUR_USERNAME/go-cron.git
cd go-cron

# Install dependencies
go mod download

# Run tests
make test

# Run linter
make lint
```

## Coding Standards

### Go Style

- Follow [Effective Go](https://go.dev/doc/effective_go) guidelines
- Use `gofmt` and `goimports` for formatting
- Follow existing code patterns in the repository

### Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <description>

[optional body]

[optional footer(s)]
```

Types:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation only
- `style`: Formatting, no code change
- `refactor`: Code restructuring
- `test`: Adding/updating tests
- `chore`: Maintenance tasks

Examples:
```
feat(parser): add support for custom timezone formats
fix(scheduler): handle DST transitions correctly
docs: update README with new examples
```

### Testing

- Write tests for new functionality
- Maintain or improve code coverage (minimum 70%)
- Test edge cases, especially:
  - Timezone handling
  - DST transitions
  - Panic recovery
  - Concurrent access

Run tests with:
```bash
# Unit tests
make test

# With race detection
go test -race ./...

# With coverage
make test-coverage
```

### Documentation

- Document exported functions, types, and methods
- Update README for user-facing changes
- Include examples for new features

## Review Process

1. All PRs require at least one approval
2. CI must pass (tests, linting, security scans)
3. Coverage must not decrease significantly
4. Breaking changes need discussion first

## API Compatibility

This library maintains backwards compatibility with `robfig/cron/v3`. When making changes:

- Don't remove or rename exported identifiers
- Don't change function signatures incompatibly
- Add new functionality via new functions/options
- Document any behavioral changes

## Questions?

- Open a [discussion](https://github.com/netresearch/go-cron/discussions) for questions
- Check existing issues for similar topics
- Review the [documentation](https://pkg.go.dev/github.com/netresearch/go-cron)

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
