BINARY_NAME := archery-cli
MODULE      := github.com/fatecannotbealtered/archery-cli
CMD_PATH    := ./cmd/archery-cli
BIN_DIR     := bin

# Version from git tag, fallback to dev
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS     := -s -w -X $(MODULE)/cmd.version=$(VERSION)

.PHONY: build test vet lint fmt check-fmt check clean install snapshot help

## build: compile the binary into bin/
build:
	@mkdir -p $(BIN_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_PATH)

## test: run all unit tests with race detection
test: vet
	go test -race ./...

## vet: run static analysis
vet:
	go vet ./...

## check-fmt: verify formatting (fails if unformatted, Unix only)
check-fmt:
	@test -z "$$(gofmt -l .)" || (echo "Run 'make fmt' to fix formatting" && gofmt -l . && exit 1)

## fmt: apply gofmt formatting
fmt:
	gofmt -w .

## check: run formatting check + vet (Unix only)
check: check-fmt vet

## lint: run golangci-lint (install if missing)
lint:
	@which golangci-lint > /dev/null 2>&1 || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest)
	golangci-lint run ./...

## clean: remove build artifacts
clean:
	rm -rf $(BIN_DIR) dist

## install: build and install to GOPATH/bin
install:
	go install -ldflags "$(LDFLAGS)" $(CMD_PATH)

## snapshot: build a local goreleaser snapshot (no publish)
snapshot:
	goreleaser release --snapshot --clean

## help: show this help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | column -t -s ':'
