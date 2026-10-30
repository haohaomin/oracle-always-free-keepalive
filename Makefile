# keepwarm — build automation. Standard-library Go, no external deps.

BINARY  := keepwarm
PKG     := ./
DIST    := dist
# Derive a version from git tags, falling back to "dev" outside a checkout.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.Version=$(VERSION)

GO ?= go

.PHONY: all build test vet fmt fmtcheck cross clean install help

all: build

## build: compile the binary for the host platform into ./$(BINARY)
build:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

## test: run all tests
test:
	$(GO) test ./...

## vet: run go vet
vet:
	$(GO) vet ./...

## fmt: format all Go files in place
fmt:
	gofmt -w .

## fmtcheck: fail if any file is not gofmt-clean
fmtcheck:
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then echo "gofmt needed on:"; echo "$$out"; exit 1; fi

## cross: build static linux/amd64 and linux/arm64 binaries into ./$(DIST)
cross:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-linux-amd64 $(PKG)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-linux-arm64 $(PKG)
	@echo "built:"
	@ls -lh $(DIST)

## install: build then run the system installer (needs sudo for systemd)
install: cross
	sudo ./scripts/install.sh

## clean: remove build artifacts
clean:
	rm -rf $(DIST) $(BINARY)
	$(GO) clean

## help: list targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /'
