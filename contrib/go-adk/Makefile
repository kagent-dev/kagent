.PHONY: build test vet clean help

# Default target
.DEFAULT_GOAL := build

# Build command that runs tests and go vet
build: vet test
	@echo "Building..."
	@go build ./...

# Run tests
test:
	@echo "Running tests..."
	@go test ./...

# Run go vet
vet:
	@echo "Running go vet..."
	@go vet ./...

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@go clean ./...

# Help target
help:
	@echo "Available targets:"
	@echo "  build  - Run go vet, tests, and build (default)"
	@echo "  test   - Run tests only"
	@echo "  vet    - Run go vet only"
	@echo "  clean  - Clean build artifacts"
	@echo "  help   - Show this help message"
