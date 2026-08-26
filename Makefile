# Project variables
BINARY_NAME=hivemind
MAIN_PACKAGE=main.go
GO_FILES=$(shell find . -name "*.go" -not -path "./vendor/*")

# Version metadata, injected via -ldflags -X into internal/buildinfo.
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS=-ldflags "-s -w \
	-X github.com/0funct0ry/hivemind/internal/buildinfo.Version=$(VERSION) \
	-X github.com/0funct0ry/hivemind/internal/buildinfo.Commit=$(COMMIT) \
	-X github.com/0funct0ry/hivemind/internal/buildinfo.Date=$(DATE)"

.PHONY: all
all: build

## help: Show this help message
.PHONY: help
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' |  sed -e 's/^/ /'

## build: Build the frontend, then the binary
.PHONY: build
build: ui
	CGO_ENABLED=0 go build $(LDFLAGS) -o bin/$(BINARY_NAME) $(MAIN_PACKAGE)

## run: Build and run the server
.PHONY: run
run: build
	./bin/$(BINARY_NAME) serve

## ui: Build the web frontend into internal/web/dist
.PHONY: ui
ui:
	cd web && npm install && npm run build

## clean: Remove build artifacts
.PHONY: clean
clean:
	rm -rf bin/

## fmt: Format the code
.PHONY: fmt
fmt:
	go fmt ./...

## vet: Run go vet
.PHONY: vet
vet:
	go vet ./...

## test: Run tests
.PHONY: test
test:
	go test -v -race ./...

## snapshot: Build a local multi-platform snapshot release with goreleaser
.PHONY: snapshot
snapshot:
	goreleaser release --snapshot --clean
