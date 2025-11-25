# go-cron Makefile
# Build automation for development and CI

.PHONY: all build test test-race test-coverage lint lint-fix fmt clean help

# Default target
all: lint test build

# Build the package
build:
	@echo "==> Building..."
	@go build -v ./...

# Run tests
test:
	@echo "==> Running tests..."
	@go test -v ./...

# Run tests with race detection
test-race:
	@echo "==> Running tests with race detection..."
	@go test -race -v ./...

# Run tests with coverage
test-coverage:
	@echo "==> Running tests with coverage..."
	@go test -race -covermode=atomic -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | tail -n 1
	@echo ""
	@echo "To view coverage in browser: go tool cover -html=coverage.out"

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

# Clean build artifacts
clean:
	@echo "==> Cleaning..."
	@rm -f coverage.out
	@go clean ./...

# Verify everything before commit
verify: tidy lint test-race
	@echo "==> All checks passed!"

# CI target (matches GitHub Actions)
ci: tidy lint test-coverage security
	@echo "==> CI checks passed!"

# Help
help:
	@echo "Available targets:"
	@echo "  all           - Run lint, test, build (default)"
	@echo "  build         - Build the package"
	@echo "  test          - Run tests"
	@echo "  test-race     - Run tests with race detection"
	@echo "  test-coverage - Run tests with coverage report"
	@echo "  lint          - Run golangci-lint"
	@echo "  lint-fix      - Run golangci-lint with auto-fix"
	@echo "  fmt           - Format code with gofmt and goimports"
	@echo "  tidy          - Tidy and verify go modules"
	@echo "  security      - Run govulncheck"
	@echo "  clean         - Clean build artifacts"
	@echo "  verify        - Run all checks before commit"
	@echo "  ci            - Run CI checks"
	@echo "  help          - Show this help"
