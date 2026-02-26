.PHONY: build test lint fmt vet tidy clean help

BINARY := bin/robodev
GO := go
GOFLAGS := -v

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

build: ## Build the controller binary
	$(GO) build $(GOFLAGS) -o $(BINARY) ./cmd/robodev/

test: ## Run all unit tests
	$(GO) test $(GOFLAGS) ./...

lint: ## Run golangci-lint
	golangci-lint run ./...

fmt: ## Format code with gofumpt
	gofumpt -w .

vet: ## Run go vet
	$(GO) vet ./...

tidy: ## Run go mod tidy
	$(GO) mod tidy

clean: ## Remove build artefacts
	rm -rf bin/

.DEFAULT_GOAL := help
