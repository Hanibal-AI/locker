BINARY := locker
BIN_DIR := bin

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: build test lint run clean release-dry-run

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/locker

test:
	go test ./...

lint:
	golangci-lint run ./...

run: build
	./$(BIN_DIR)/$(BINARY) start

clean:
	rm -rf $(BIN_DIR) dist

# Runs the full release pipeline locally (binaries for every OS/arch,
# multi-arch Docker images) without publishing anything — see
# Docs/roadmap.md Phase 7.1 and .goreleaser.yaml. Requires goreleaser
# and Docker.
release-dry-run:
	goreleaser release --snapshot --clean
