SHELL := /bin/sh

GO ?= ./scripts/go.sh

.PHONY: help fmt tidy vet test test-race test-integration build up down logs

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'

fmt: ## Format Go code
	$(GO) go fmt ./...

tidy: ## Tidy Go modules
	$(GO) go mod tidy

vet: ## Run go vet
	$(GO) go vet ./...

test: ## Run unit and HTTP tests
	$(GO) go test -count=1 ./...

test-race: ## Run tests with the race detector
	docker run --rm \
		-v "$(PWD)":/app \
		-v devices-api-gomod:/go/pkg/mod \
		-v devices-api-gocache:/root/.cache/go-build \
		-w /app \
		golang:1.27 \
		go test -race -count=1 ./...

test-integration: ## Run all tests against a disposable Postgres
	@docker compose -f docker-compose.test.yml up --build --abort-on-container-exit --exit-code-from tests; \
	status=$$?; docker compose -f docker-compose.test.yml down -v; exit $$status

build: ## Build the API container image
	docker build -t devices-api:local .

up: ## Start API and Postgres
	docker compose up --build

down: ## Stop services and remove volumes
	docker compose down -v

logs: ## Follow API logs
	docker compose logs -f api
