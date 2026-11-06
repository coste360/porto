# Porto: marina and boatyard management system (Coste360 SRL).
#
# The Makefile is the supported interface for building, testing and checking the project: humans, CI and
# coding agents use the same targets. Run `make` or `make help` for the list.
#
# GNU Make 3.81 compatible (the version shipped with macOS): no .ONESHELL, no .SHELLFLAGS. Multi-line recipes
# start with `set -euo pipefail`. Targets that need web/, e2e/ or api/openapi.yaml print a notice and succeed
# while those parts do not exist yet.

SHELL := /bin/bash
.DEFAULT_GOAL := help

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

MODULE                := github.com/coste360/porto
VERSION               ?= $(shell scripts/version.sh)
IMAGE                 ?= ghcr.io/coste360/porto
GOLANGCI_LINT_VERSION ?= v2.15.0
POSTGRES_IMAGE        ?= postgres:17-alpine
# Go packages for test, test-db and lint; narrow it for focused runs: make test PKG=./internal/qty/...
PKG                   ?= ./...

BIN_DIR       := $(CURDIR)/bin
GOLANGCI_LINT := $(BIN_DIR)/golangci-lint

# Local development environment (never use these values in production).
DEV_ENV := PORTO_DEV=true \
           PORTO_DATA_DIR=$(CURDIR)/tmp/data \
           PORTO_ADDR=:8080 \
           PORTO_BASE_URL=http://localhost:8080 \
           PORTO_LOG_LEVEL=debug \
           PORTO_AUTO_MIGRATE=true \
           PORTO_MAIL_DIR=$(CURDIR)/tmp/mail

# Optional parts of the tree.
HAS_WEB     = $(wildcard web/package.json)
HAS_WEB_LCK = $(wildcard web/package-lock.json)
HAS_E2E     = $(wildcard e2e/package.json)
HAS_E2E_LCK = $(wildcard e2e/package-lock.json)
HAS_API     = $(wildcard api/openapi.yaml)
HAS_API_CFG = $(wildcard api/oapi-codegen.yaml)
HAS_CMD     = $(wildcard cmd/porto/*.go)

GO_GENERATED  := internal/http/api/api.gen.go internal/geo/sintable_gen.go internal/domain/layout/kitassets_gen.go
WEB_GENERATED := web/shared/api/generated web/shared/src/map/kit/generated
STATIC_DIR    := internal/http/static
LICENSES_OK   := MIT,BSD-2-Clause,BSD-3-Clause,Apache-2.0,ISC,MPL-2.0

LDFLAGS := -s -w -X main.version=$(VERSION)

define skip
	echo "make: $(1): skipped ($(2) not present yet)"
endef

# ---------------------------------------------------------------------------
# Help
# ---------------------------------------------------------------------------

.PHONY: help
help: ## Show this help
	@echo "Porto — make targets:"
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

# ---------------------------------------------------------------------------
# Setup and development
# ---------------------------------------------------------------------------

.PHONY: setup
setup: ## Download Go modules, install golangci-lint into ./bin, install web/e2e dependencies
	@set -euo pipefail; \
	go mod download; \
	want="$(GOLANGCI_LINT_VERSION)"; want="$${want#v}"; \
	if [ -x "$(GOLANGCI_LINT)" ] && "$(GOLANGCI_LINT)" version 2>/dev/null | grep -q "version $$want "; then \
		echo "golangci-lint $(GOLANGCI_LINT_VERSION) already installed"; \
	else \
		echo "installing golangci-lint $(GOLANGCI_LINT_VERSION) into ./bin"; \
		GOBIN="$(BIN_DIR)" go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION); \
	fi; \
	if [ -n "$(HAS_WEB)" ]; then \
		if [ -n "$(HAS_WEB_LCK)" ]; then npm --prefix web ci; else npm --prefix web install; fi; \
	else $(call skip,setup web,web/package.json); fi; \
	if [ -n "$(HAS_E2E)" ]; then \
		if [ -n "$(HAS_E2E_LCK)" ]; then npm --prefix e2e ci; else npm --prefix e2e install; fi; \
		(cd e2e && npx playwright install chromium); \
	else $(call skip,setup e2e,e2e/package.json); fi

.PHONY: dev
dev: ## Run backend (air if installed, else go run) and web dev servers
	@set -euo pipefail; \
	mkdir -p tmp/data tmp/mail; \
	pids=""; \
	trap 'kill $$pids 2>/dev/null || true' EXIT INT TERM; \
	if command -v air >/dev/null 2>&1; then \
		$(DEV_ENV) air --build.cmd "go build -o tmp/bin/porto ./cmd/porto" --build.bin "tmp/bin/porto" --build.args_bin "serve" & \
	else \
		$(DEV_ENV) go run ./cmd/porto serve & \
	fi; \
	pids="$$!"; \
	if [ -n "$(HAS_WEB)" ]; then npm --prefix web run dev & pids="$$pids $$!"; \
	else $(call skip,dev web,web/package.json); fi; \
	wait

.PHONY: seed
seed: ## Seed the local dev database with the demo marina
	@set -euo pipefail; \
	mkdir -p tmp/data; \
	$(DEV_ENV) PORTO_ADMIN_PASSWORD="$${PORTO_ADMIN_PASSWORD:-porto-demo-password}" go run ./cmd/porto seed demo

# ---------------------------------------------------------------------------
# Code generation and formatting
# ---------------------------------------------------------------------------

.PHONY: gen
gen: ## Regenerate code: OpenAPI server, trig table, kit asset ids, TypeScript client and kit symbols
	@set -euo pipefail; \
	if [ -n "$(HAS_API)" ] && [ -n "$(HAS_API_CFG)" ]; then \
		go tool oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml; \
	else $(call skip,gen api,api/openapi.yaml); fi; \
	go generate ./internal/geo/...; \
	go generate ./internal/domain/layout/...; \
	if [ -n "$(HAS_WEB)" ]; then npm --prefix web run gen; \
	else $(call skip,gen web,web/package.json); fi

.PHONY: gen-check
gen-check: ## Fail if generated files are out of date (content changes after make gen; uncommitted in CI)
	@set -euo pipefail; \
	sums() { for p in $(GO_GENERATED) $(WEB_GENERATED); do if [ -e "$$p" ]; then find "$$p" -type f -not -name .gitkeep | LC_ALL=C sort | xargs shasum -a 256; fi; done; }; \
	before="$$(sums)"; \
	$(MAKE) --no-print-directory gen; \
	after="$$(sums)"; \
	if [ "$$before" != "$$after" ]; then \
		echo "gen-check: generated files were out of date (now regenerated; review and commit them)"; \
		diff <(echo "$$before") <(echo "$$after") || true; \
		exit 1; \
	fi; \
	paths=""; \
	for p in $(GO_GENERATED) $(WEB_GENERATED); do if [ -e "$$p" ]; then paths="$$paths $$p"; fi; done; \
	if [ -n "$$paths" ] && git rev-parse --git-dir >/dev/null 2>&1; then \
		changes="$$(git status --porcelain -- $$paths)"; \
		if [ -n "$$changes" ]; then \
			if [ -n "$${CI:-}" ]; then echo "gen-check: generated files are not committed:"; echo "$$changes"; exit 1; fi; \
			echo "gen-check: note: generated files are not committed yet (fails in CI):"; echo "$$changes"; \
		fi; \
	fi; \
	echo "gen-check: generated files up to date"

# Non-generated Go files (generated files carry "Code generated ... DO NOT EDIT").
GO_SOURCES = $(shell find . -name '*.go' -not -path './node_modules/*' -not -path './web/*' -not -path './e2e/*' -not -path './tmp/*' -not -name '*.gen.go' -not -name '*_gen.go' 2>/dev/null)

.PHONY: fmt
fmt: ## Format Go (gofumpt, goimports) and web sources
	@set -euo pipefail; \
	files="$(GO_SOURCES)"; \
	if [ -n "$$files" ]; then \
		go tool gofumpt -w $$files; \
		go tool goimports -local $(MODULE) -w $$files; \
	fi; \
	if [ -n "$(HAS_WEB)" ]; then npm --prefix web run format; \
	else $(call skip,fmt web,web/package.json); fi

# ---------------------------------------------------------------------------
# Quality gates
# ---------------------------------------------------------------------------

.PHONY: lint
lint: ## Run golangci-lint and the web linters, formatter check and typecheck
	@set -euo pipefail; \
	if [ -x "$(GOLANGCI_LINT)" ]; then \
		"$(GOLANGCI_LINT)" run $(PKG); \
	else \
		echo "WARNING: $(GOLANGCI_LINT) missing (run make setup); falling back to go vet"; \
		go vet $(PKG); \
	fi; \
	if [ -n "$(HAS_WEB)" ]; then \
		npm --prefix web run lint; \
		npm --prefix web run format:check; \
		npm --prefix web run typecheck; \
	else $(call skip,lint web,web/package.json); fi

.PHONY: test
test: ## Run Go unit tests and web unit tests
	@set -euo pipefail; \
	go test $(PKG); \
	if [ -n "$(HAS_WEB)" ]; then npm --prefix web run test; \
	else $(call skip,test web,web/package.json); fi

.PHONY: test-db
test-db: ## Run Go integration tests on SQLite and an ephemeral PostgreSQL
	@scripts/with-postgres.sh go test -tags=integration $(PKG)

.PHONY: test-race
test-race: ## Run the TestConcurrency_* tests with the race detector (SQLite + PostgreSQL)
	@set -euo pipefail; \
	n="$$(go test -tags=integration -list '^TestConcurrency' ./internal/... | grep -c '^TestConcurrency' || true)"; \
	if [ "$$n" -eq 0 ]; then echo "test-race: no TestConcurrency_* tests found"; exit 1; fi; \
	echo "test-race: $$n TestConcurrency_* tests"; \
	scripts/with-postgres.sh go test -race -tags=integration -run '^TestConcurrency' ./internal/...

.PHONY: e2e
e2e: ## Build the binary and run the Playwright end-to-end tests
	@set -euo pipefail; \
	if [ -z "$(HAS_E2E)" ]; then $(call skip,e2e,e2e/package.json); exit 0; fi; \
	$(MAKE) build; \
	cd e2e && npx playwright test

.PHONY: check
check: gen-check lint test ## Fast gate: gen-check, lint, test (target < 2 min)

.PHONY: check-full
check-full: check test-db test-race e2e ## Full gate: check, integration tests, race tests, e2e

# ---------------------------------------------------------------------------
# Build and packaging
# ---------------------------------------------------------------------------

.PHONY: web-build
web-build: ## Build the SPAs and copy them into the embedded static directory
	@set -euo pipefail; \
	if [ -z "$(HAS_WEB)" ]; then $(call skip,web-build,web/package.json); exit 0; fi; \
	npm --prefix web run build; \
	for app in admin booking; do \
		dest="$(STATIC_DIR)/$$app"; \
		mkdir -p "$$dest"; \
		find "$$dest" -mindepth 1 -not -name .gitkeep -exec rm -rf {} +; \
		cp -R "web/$$app/dist/." "$$dest/"; \
	done

.PHONY: build
build: web-build ## Build bin/porto (static, no CGO) with the embedded SPAs
	@set -euo pipefail; \
	if [ -z "$(HAS_CMD)" ]; then $(call skip,build,cmd/porto); exit 0; fi; \
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/porto ./cmd/porto; \
	echo "built bin/porto $(VERSION)"

.PHONY: docker
docker: ## Build the container image (IMAGE, tag dev) from deploy/Dockerfile
	docker build -f deploy/Dockerfile --build-arg VERSION=$(VERSION) -t $(IMAGE):dev .

.PHONY: up
up: ## Start the local stack with docker compose
	docker compose up --build

.PHONY: licenses
licenses: ## Check third-party Go and npm licences against the allow-list (Porto itself is ignored)
	@set -euo pipefail; \
	go tool go-licenses check ./... --ignore $(MODULE) --allowed_licenses=$(LICENSES_OK); \
	if [ -n "$(HAS_WEB)" ]; then npm --prefix web run licenses; \
	else $(call skip,licenses web,web/package.json); fi

.PHONY: clean
clean: ## Remove build outputs (keeps bin/golangci-lint and .gitkeep files)
	@set -euo pipefail; \
	rm -rf bin/porto tmp dist web/*/dist e2e/test-results e2e/playwright-report; \
	for app in admin booking; do \
		[ -d "$(STATIC_DIR)/$$app" ] && find "$(STATIC_DIR)/$$app" -mindepth 1 -not -name .gitkeep -exec rm -rf {} + || true; \
	done
