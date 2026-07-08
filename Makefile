BINARY  := weft
PKG     := github.com/HeoJeongBo/weft
CMD     := ./cmd/weft
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.Date=$(DATE)

.DEFAULT_GOAL := build

## build: compile the weft binary into ./weft
.PHONY: build
build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)

## install: go install weft into GOBIN
.PHONY: install
install:
	go install -ldflags "$(LDFLAGS)" $(CMD)

## run: run weft from source (pass args with ARGS=...)
.PHONY: run
run:
	go run $(CMD) $(ARGS)

## test: run all tests with the race detector
.PHONY: test
test:
	go test -race ./...

## cover: run tests and print total coverage
.PHONY: cover
cover:
	go test -race -covermode=atomic -coverprofile=coverage.txt ./...
	go tool cover -func=coverage.txt | tail -1

## vet: run go vet
.PHONY: vet
vet:
	go vet ./...

## lint: run golangci-lint
.PHONY: lint
lint:
	golangci-lint run

## fmt: format the code (gofumpt if available, else gofmt)
.PHONY: fmt
fmt:
	@command -v gofumpt >/dev/null 2>&1 && gofumpt -w . || gofmt -w .

## tidy: tidy go.mod / go.sum
.PHONY: tidy
tidy:
	go mod tidy

## doctor: run weft's environment checks from source
.PHONY: doctor
doctor:
	go run $(CMD) doctor

## snapshot: build a local goreleaser snapshot (no publish)
.PHONY: snapshot
snapshot:
	goreleaser release --snapshot --clean

## clean: remove build artifacts
.PHONY: clean
clean:
	rm -rf $(BINARY) dist coverage.txt coverage.html

## help: list available targets
.PHONY: help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | awk -F': ' '{printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'
