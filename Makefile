# Makefile for Go BitTorrent Client

# Configuration
BINARY_NAME = torrent-client
GO_BUILD_FLAGS = -v
GO_TEST_FLAGS = -v -race
GO_SOURCES = $(shell find . -name '*.go')

.PHONY: all build clean test lint fmt deps install release help

all: build

build: $(GO_SOURCES)
	@echo "Building $(BINARY_NAME)..."
	go build $(GO_BUILD_FLAGS) -o $(BINARY_NAME) Start.go

test:
	@echo "Running tests..."
	go test $(GO_TEST_FLAGS) ./...
	@echo "Tests completed."

test-coverage:
	@echo "Running tests with coverage..."
	go test $(GO_TEST_FLAGS) -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

clean:
	@echo "Cleaning up..."
	@rm -f $(BINARY_NAME) coverage.out coverage.html
	@rm -rf dist/

lint:
	@echo "Linting code..."
	@golangci-lint run ./...

fmt:
	@echo "Formatting code..."
	@go fmt ./...

deps:
	@echo "Tidying dependencies..."
	@go mod tidy
	@go mod verify

install: build
	@echo "Installing to /usr/local/bin..."
	@sudo install -m 755 $(BINARY_NAME) /usr/local/bin/$(BINARY_NAME)

release: clean
	@echo "Building releases for multiple platforms..."
	@mkdir -p dist
	GOOS=linux GOARCH=amd64 go build -o dist/$(BINARY_NAME)-linux-amd64
	GOOS=windows GOARCH=amd64 go build -o dist/$(BINARY_NAME)-windows-amd64.exe
	GOOS=darwin GOARCH=amd64 go build -o dist/$(BINARY_NAME)-darwin-amd64

help:
	@echo "Available targets:"
	@echo "  all       - Build the project (default)"
	@echo "  build     - Build the binary"
	@echo "  clean     - Remove build artifacts"
	@echo "  test      - Run tests"
	@echo "  test-coverage - Generate test coverage report"
	@echo "  lint      - Run static analysis (requires golangci-lint)"
	@echo "  fmt       - Format source code"
	@echo "  deps      - Tidy Go module dependencies"
	@echo "  install   - Install to /usr/local/bin"
	@echo "  release   - Build binaries for multiple platforms"
	@echo "  help      - Show this help message"
