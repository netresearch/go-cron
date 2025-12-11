# go-cron Makefile
# Build automation for development and CI

.PHONY: all build test test-race test-coverage lint lint-fix fmt clean help setup dev-setup dev-check precommit

# Build flags for reproducible, optimized binaries
LDFLAGS := -s -w
BUILDFLAGS := -trimpath -ldflags "$(LDFLAGS)"

# Default target
all: lint test build

# Build the package
build:
	@echo "==> Building..."
	@go build -v ./...

# Build with optimization flags (for release verification)
build-release:
	@echo "==> Building with release flags..."
	@CGO_ENABLED=0 go build $(BUILDFLAGS) -v ./...

# Run tests
test:
	@echo "==> Running tests..."
	@go test -v ./...

# Run tests with race detection
test-race:
	@echo "==> Running tests with race detection..."
	@CGO_ENABLED=1 go test -race -v ./...

# Run tests with coverage
test-coverage:
	@echo "==> Running tests with coverage..."
	@go test -race -covermode=atomic -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | tail -n 1
	@echo ""
	@echo "To view coverage in browser: go tool cover -html=coverage.out"

# Run fuzz tests (short duration for local dev)
test-fuzz:
	@echo "==> Running fuzz tests (30s each)..."
	@go test -fuzz=FuzzParseStandard -fuzztime=30s ./...
	@go test -fuzz=FuzzSpec_Next -fuzztime=30s ./...

# Run benchmarks
benchmark:
	@echo "==> Running benchmarks..."
	@go test -bench=. -benchmem -run=^$$ -count=3 ./...

# Run linter
lint:
	@echo "==> Running linter..."
	@golangci-lint run ./...

# Run linter with auto-fix
lint-fix:
	@echo "==> Running linter with fixes..."
	@golangci-lint run --fix ./...

# Format code
fmt:
	@echo "==> Formatting code..."
	@gofmt -w .
	@goimports -w .

# Tidy modules
tidy:
	@echo "==> Tidying modules..."
	@go mod tidy
	@go mod verify

# Security checks
security:
	@echo "==> Running security checks..."
	@go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# Run gosec security scanner
gosec:
	@echo "==> Running gosec..."
	@go run github.com/securego/gosec/v2/cmd/gosec@latest ./...

# Run gitleaks secret scanner
gitleaks:
	@echo "==> Running gitleaks..."
	@gitleaks detect --source . --no-banner --redact

# Clean build artifacts
clean:
	@echo "==> Cleaning..."
	@rm -f coverage.out
	@go clean ./...

# Verify everything before commit
verify: tidy lint test-race security
	@echo "==> All checks passed!"

# CI target (matches GitHub Actions)
ci: tidy lint test-coverage security gosec
	@echo "==> CI checks passed!"

# Full security audit
audit: security gosec gitleaks
	@echo "==> Security audit complete!"

# Development environment setup
setup: dev-setup
	@echo "🎉 Setup complete! You're ready to develop."

dev-setup:
	@echo "🔧 Setting up development environment..."
	@echo "📦 Installing required tools..."
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	@echo "✅ golangci-lint v2 installed"
	@go install golang.org/x/tools/cmd/goimports@latest
	@echo "✅ goimports installed"
	@go install github.com/evilmartians/lefthook@latest
	@echo "✅ lefthook installed"
	@lefthook install
	@echo "✅ Git hooks installed via lefthook"
	@echo ""
	@echo "🛡️ All commits will now be automatically validated!"

# Run all development checks (same as pre-commit)
dev-check: tidy fmt lint test-race
	@echo "🎉 All development checks passed! Ready to commit."

# Pre-commit validation
precommit: dev-check
	@echo "✅ Pre-commit checks complete - your code is ready!"

# Help
help:
	@echo "Available targets:"
	@echo "  all           - Run lint, test, build (default)"
	@echo "  build         - Build the package"
	@echo "  build-release - Build with optimization flags (-trimpath, -ldflags)"
	@echo "  test          - Run tests"
	@echo "  test-race     - Run tests with race detection"
	@echo "  test-coverage - Run tests with coverage report"
	@echo "  test-fuzz     - Run fuzz tests (30s each)"
	@echo "  benchmark     - Run benchmarks"
	@echo "  lint          - Run golangci-lint"
	@echo "  lint-fix      - Run golangci-lint with auto-fix"
	@echo "  fmt           - Format code with gofmt and goimports"
	@echo "  tidy          - Tidy and verify go modules"
	@echo "  security      - Run govulncheck"
	@echo "  gosec         - Run gosec security scanner"
	@echo "  gitleaks      - Run gitleaks secret scanner"
	@echo "  clean         - Clean build artifacts"
	@echo "  verify        - Run all checks before commit"
	@echo "  ci            - Run CI checks"
	@echo "  audit         - Run full security audit"
	@echo "  setup         - Set up development environment with git hooks"
	@echo "  dev-setup     - Install tools and configure lefthook"
	@echo "  dev-check     - Run all development checks"
	@echo "  precommit     - Run pre-commit validation"
	@echo "  help          - Show this help"
