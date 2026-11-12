.PHONY: all build test test-race lint run clean

BINARY_NAME=forge
BUILD_DIR=bin

all: build test

build:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/forge

test:
	go test -v ./...

test-race:
	go test -race -v ./...

lint:
	go vet ./...
	@which golangci-lint > /dev/null 2>&1 && golangci-lint run ./... || echo "golangci-lint not installed, skipped"

run:
	go run ./cmd/forge --demo

demo-gif: build
	@which vhs > /dev/null 2>&1 && vhs docs/demo/demo.tape || echo "vhs not found. Install via: winget install charmbracelet.vhs or brew install vhs"

clean:
	rm -rf $(BUILD_DIR)

