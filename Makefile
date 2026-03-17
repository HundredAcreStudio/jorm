.PHONY: help build run test lint vet fmt clean tidy
.DEFAULT_GOAL := help

CGO_ENABLED := 1

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

build: ## Build binary to bin/jorm
	CGO_ENABLED=$(CGO_ENABLED) go build -o bin/jorm ./cmd/jorm

run: ## Run via go run (use ARGS="run 42")
	CGO_ENABLED=$(CGO_ENABLED) go run ./cmd/jorm $(ARGS)

test: ## Run all tests
	CGO_ENABLED=$(CGO_ENABLED) go test ./...

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
