# doze — build automation.

BINARY      := doze
PKG         := github.com/doze-dev/doze
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

.PHONY: bench
bench: ## Measure idle memory and wake time on the fixture stack (boots real engines)
	go test -tags bench -count=1 -v -timeout 20m ./test/bench/

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

GOLANGCI    := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
GOVULNCHECK := go run golang.org/x/vuln/cmd/govulncheck@v1.1.4

.PHONY: lint
lint: ## Run golangci-lint (the CI lint gate)
	$(GOLANGCI) run ./...

.PHONY: vuln
vuln: ## Report known vulnerabilities reachable from doze
	$(GOVULNCHECK) ./...

.PHONY: tidy-check
tidy-check: ## Fail if go.mod or go.sum is not tidy
	go mod tidy -diff

.PHONY: check
check: fmt-check tidy-check vet lint vuln race ## The PR checks that run locally (CI adds the coverage floor, the tagged-deps check and the bench)

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
