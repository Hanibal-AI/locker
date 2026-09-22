BINARY := locker
BIN_DIR := bin

.PHONY: build test lint run clean

build:
	go build -o $(BIN_DIR)/$(BINARY) ./cmd/locker

test:
	go test ./...

lint:
	golangci-lint run ./...

run: build
	./$(BIN_DIR)/$(BINARY)

clean:
	rm -rf $(BIN_DIR)
