# doze — build automation.

BINARY      := doze
PKG         := github.com/doze-dev/doze
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X main.version=$(VERSION)
# Wails in purego mode needs the tag everywhere, or the GUI package does not
# compile under CGO_ENABLED=0; the release build adds production.
export GOFLAGS := -tags=purego
GOFILES     := $(shell find . -name '*.go' -not -path './vendor/*')

.DEFAULT_GOAL := build

.PHONY: frontend
frontend: ## Build the app's page (React, Vite) into internal/desk/frontend/dist with bun
	cd internal/desk/frontend && bun install --frozen-lockfile && bun run build

.PHONY: bindings
bindings: ## Regenerate the page's TypeScript bindings from the bound Go service (wails3)
	rm -rf internal/desk/frontend/src/bindings
	go run github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0 generate bindings -ts -i -names -f "-tags purego" -d internal/desk/frontend/src/bindings ./internal/desk/app/

.PHONY: bindings-check
bindings-check: ## Fail when the committed bindings differ from the Go service
	@tmp=$$(mktemp -d) && go run github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0 generate bindings -ts -i -names -silent -f "-tags purego" -d "$$tmp" ./internal/desk/app/ && diff -r "$$tmp" internal/desk/frontend/src/bindings && rm -rf "$$tmp" && echo "bindings are current"

.PHONY: build
build: frontend ## Build the doze binary, page included
	CGO_ENABLED=0 go build -tags purego,production -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/doze

.PHONY: icons
icons: ## Render the app icon (.icns) and the menu bar bat from website/brand
	cd tools/icons && go run . -brand ../../website/brand -out ../../build/darwin
	iconutil -c icns build/darwin/doze.iconset -o build/darwin/doze.icns
	cp build/darwin/bat-22.png build/darwin/bat-44.png build/darwin/bat-alert-44.png internal/desk/app/icons/

.PHONY: app
app: build ## Build Doze.app around the binary, ad-hoc signed (DOZE_SIGN_ID for a Developer ID)
	sh build/darwin/bundle.sh $(BINARY) dist $(VERSION)

.PHONY: install
install: ## Install doze into GOBIN
	CGO_ENABLED=0 go install -tags purego,production -ldflags "$(LDFLAGS)" ./cmd/doze

.PHONY: test
test: ## Run unit tests
	go test ./...

.PHONY: race
race: ## Run tests with the race detector
	go test -race ./...

.PHONY: bench
bench: ## Measure idle memory and wake time on the fixture stack (boots real engines)
	go test -tags bench -count=1 -v -timeout 20m ./test/bench/

.PHONY: deskbench
deskbench: frontend ## Measure the app (menu bar, window, after close, CPU at rest) on the fixture, against test/deskbench/budget.json
	go test -tags deskbench,purego -count=1 -v -timeout 10m ./test/deskbench/

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
GOVULNCHECK := go run golang.org/x/vuln/cmd/govulncheck@v1.9.0

.PHONY: lint
lint: ## Run golangci-lint (the CI lint gate)
	$(GOLANGCI) run ./...

.PHONY: vuln
vuln: ## Report known vulnerabilities reachable from doze
	$(GOVULNCHECK) ./...

.PHONY: tidy-check
tidy-check: ## Fail if go.mod or go.sum is not tidy
	go mod tidy -diff

.PHONY: cross
cross: ## Prove every release target builds without cgo, and the desk tests compile on Linux
	@for target in linux/amd64 linux/arm64 darwin/arm64; do \
		CGO_ENABLED=0 GOOS=$${target%/*} GOARCH=$${target#*/} go build -o /dev/null ./cmd/doze || exit 1; \
		CGO_ENABLED=0 GOOS=$${target%/*} GOARCH=$${target#*/} go test -c -o /dev/null ./internal/desk/app/ || exit 1; \
	done

.PHONY: check
check: fmt-check tidy-check vet lint vuln race cross ## The PR checks that run locally (CI adds the coverage floor, the tagged-deps check and the bench)

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

.PHONY: dmg
dmg: app ## Build Doze.app and wrap it in a disk image (dist/Doze-<version>.dmg)
	sh build/darwin/dmg.sh dist $(VERSION)

# The version the docs' screenshots show (Settings, About, Machine): the
# release they document, not this checkout's git describe.
DOCS_VERSION ?= 0.2.0

.PHONY: docs-shots
docs-shots: frontend ## Shoot the docs' app screenshots (website/docs-shots.json) into website/src/assets/app as WebP
	CGO_ENABLED=0 go build -tags purego,production -ldflags "-s -w -X main.version=$(DOCS_VERSION)" -o test/desk-e2e/.doze-docs ./cmd/doze
	cd test/desk-e2e && bun install --frozen-lockfile && DOZE_BIN=.doze-docs bun run shot.ts docs ../../website/docs-shots.json ../../website/src/assets/app
	rm -f test/desk-e2e/.doze-docs

.PHONY: ground
ground: build ## Check that every doze command, flag and doze.hcl field the docs name exists (GROUND_REPORT_ONLY=1 reports without failing)
	bun website/scripts/ground.ts

.PHONY: llms
llms: ## Write website/public/llms.txt from the docs' order (doze-website design/docs-ia.json) and each page's title and description
	bun website/scripts/gen-llms.ts
