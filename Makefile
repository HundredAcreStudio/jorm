.PHONY: help bootstrap build run test test-e2e test-e2e-focus lint vet fmt clean tidy
.DEFAULT_GOAL := help

CGO_ENABLED := 1

help: ## Show this help
	@grep -E '^[a-zA-Z0-9_-]+:.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    := $(shell date -u +%Y-%m-%d)
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(DATE)

bootstrap: ## Install/check required dev tools
	@echo "Checking required tools..."
	@command -v go >/dev/null 2>&1 || { echo "  MISSING: go — install from https://go.dev/dl/"; exit 1; }
	@echo "  go: $$(go version | awk '{print $$3}')"
	@command -v gcc >/dev/null 2>&1 || { echo "  MISSING: gcc — required for CGO (sqlite3)"; exit 1; }
	@echo "  gcc: $$(gcc --version | head -1)"
	@command -v ginkgo >/dev/null 2>&1 || { echo "  Installing ginkgo..."; go install github.com/onsi/ginkgo/v2/ginkgo@latest; }
	@echo "  ginkgo: $$(ginkgo version 2>&1 | grep -o 'Version.*')"
	@command -v golangci-lint >/dev/null 2>&1 || { echo "  MISSING: golangci-lint — install from https://golangci-lint.run/usage/install/"; }
	@command -v golangci-lint >/dev/null 2>&1 && echo "  golangci-lint: $$(golangci-lint --version 2>&1 | head -1)" || true
	@command -v goimports >/dev/null 2>&1 || { echo "  Installing goimports..."; go install golang.org/x/tools/cmd/goimports@latest; }
	@echo "  goimports: installed"
	@command -v claude >/dev/null 2>&1 || { echo "  MISSING: claude CLI — required for jorm agent execution"; }
	@command -v claude >/dev/null 2>&1 && echo "  claude: installed" || true
	@echo "All required tools present."

build: ## Build binary to bin/jorm
	CGO_ENABLED=$(CGO_ENABLED) go build -ldflags "$(LDFLAGS)" -o bin/jorm ./cmd/jorm

run: ## Run via go run (use ARGS="run 42")
	CGO_ENABLED=$(CGO_ENABLED) go run ./cmd/jorm $(ARGS)

test: ## Run unit tests (excludes e2e)
	CGO_ENABLED=$(CGO_ENABLED) go test ./...

test-e2e: ## Run e2e calibration suite via ginkgo
	CGO_ENABLED=$(CGO_ENABLED) ginkgo -tags e2e -timeout 30m -v ./internal/e2e/

test-e2e-focus: ## Run single e2e test (use FOCUS="Issue 1")
	CGO_ENABLED=$(CGO_ENABLED) ginkgo -tags e2e -timeout 10m -v -focus "$(FOCUS)" ./internal/e2e/

lint: ## Run golangci-lint
	golangci-lint run

vet: ## Run go vet
	go vet ./...

fmt: ## Format with goimports
	goimports -w .

clean: ## Remove build artifacts
	rm -rf bin/

tidy: ## Run go mod tidy
	go mod tidy
