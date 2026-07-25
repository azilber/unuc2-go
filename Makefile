# unuc2-go — build into bin/

BIN     := bin/unuc2
PKG     := ./cmd/unuc2
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.8)
LDFLAGS := -s -w -X main.version=$(VERSION)

.DEFAULT_GOAL := build

.PHONY: build install test race vet fmt lint tidy clean help

## build: compile the unuc2 binary into bin/
build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) $(PKG)

## install: install unuc2 into $GOBIN / $GOPATH/bin
install:
	go install -trimpath -ldflags '$(LDFLAGS)' $(PKG)

## test: run all tests
test:
	go test ./...

## race: run all tests under the race detector
race:
	go test -race ./...

## vet: run go vet
vet:
	go vet ./...

## fmt: format all Go sources
fmt:
	gofmt -w .

## lint: run golangci-lint if installed
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed; skipping"; \
	fi

## tidy: tidy go.mod / go.sum
tidy:
	go mod tidy

## clean: remove build output
clean:
	rm -rf bin

## help: list available targets
help:
	@echo "Targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /'
