.PHONY: test test-unit test-integration test-all build clean

# Run only unit tests (fast, excludes integration tests)
test-unit:
	@echo "Running unit tests..."
	go test -v ./... -short

# Run only integration tests (I/O tests with network/disk)
test-integration:
	@echo "Running integration tests..."
	go test -v ./... -tags=integration

# Run all tests (unit + integration)
test-all:
	@echo "Running all tests..."
	go test -v ./... -tags=integration

# Default: run unit tests only
test: test-unit

# Build the server binary
build:
	@echo "Building server..."
	go build -o bin/harmonydb-server ./cmd/server.go

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	rm -f /tmp/raft_test.log

# Run the server
run: build
	./bin/harmonydb-server

.DEFAULT_GOAL := test
