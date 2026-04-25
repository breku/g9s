BIN         := gcptui
MODULE      := github.com/brekol/gcp-terminal-dashboard
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS     := -s -w -X $(MODULE)/cmd.Version=$(VERSION)

.PHONY: build run test lint tidy clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BIN) ./main.go

run:
	go run -ldflags "$(LDFLAGS)" ./main.go

test:
	go test ./...

lint:
	golangci-lint run ./...

tidy:
	go mod tidy

clean:
	rm -rf bin/

release-dry-run:
	goreleaser release --snapshot --clean
