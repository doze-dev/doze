# doze — build automation.

BINARY      := doze
PKG         := github.com/nerdmenot/doze
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X main.version=$(VERSION)
GOFILES     := $(shell find . -name '*.go' -not -path './vendor/*')

.DEFAULT_GOAL := build

.PHONY: build
build: ## Build the doze binary
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/doze

.PHONY: install
install: ## Install doze into GOBIN
	go install -ldflags "$(LDFLAGS)" ./cmd/doze

.PHONY: test
test: ## Run unit tests
	go test ./...

.PHONY: race
race: ## Run tests with the race detector
	go test -race ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: fmt
fmt: ## Format all Go source
	gofmt -w $(GOFILES)

.PHONY: fmt-check
fmt-check: ## Fail if any file is not gofmt-clean
	@unformatted=$$(gofmt -l $(GOFILES)); \
	if [ -n "$$unformatted" ]; then echo "unformatted files:"; echo "$$unformatted"; exit 1; fi

.PHONY: check
check: fmt-check vet test ## Run all checks (fmt, vet, test)

.PHONY: clean
clean: ## Remove build artifacts
	rm -f $(BINARY)
	rm -rf dist

.PHONY: dist
dist: ## Build release archives with goreleaser (requires goreleaser)
	goreleaser release --clean --snapshot

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
