APP_NAME      := task-management-api
BIN_DIR       := bin
PKG           := ./...
COVER_MIN     := 95

COMPOSE       := docker compose -f docker-compose.yml
COMPOSE_DB    := docker compose -f docker-compose.yml -f docker-compose.db.yml

# Load .env so `make run` uses the same configuration as Docker.
-include .env
export

DB_EXPOSE_PORT ?= 5433

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make <target>\n\n"} /^[a-zA-Z0-9_-]+:.*##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# Copy .env.example and replace the placeholder JWT_SECRET with a random one.
.env:
	@sed "s|^JWT_SECRET=.*|JWT_SECRET=$$(openssl rand -hex 32 2>/dev/null || od -An -tx1 -N32 /dev/urandom | tr -d ' \n')|" .env.example > .env
	@echo ".env created from .env.example with a random JWT_SECRET"

.PHONY: env
env: .env ## Create .env from .env.example if it does not exist

## ---------- Docker ----------

.PHONY: up
up: .env ## Run API + bundled PostgreSQL in Docker
	$(COMPOSE_DB) up -d --build
	@echo "API: http://localhost:$${HTTP_PORT:-8080}  |  PostgreSQL: localhost:$(DB_EXPOSE_PORT)"

.PHONY: up-external-db
up-external-db: .env ## Run API in Docker against an existing PostgreSQL (DB_HOST_DOCKER/DB_PORT/...)
	$(COMPOSE) up -d --build
	@echo "API: http://localhost:$${HTTP_PORT:-8080}"

.PHONY: down
down: ## Stop containers
	$(COMPOSE_DB) down

.PHONY: down-volumes
down-volumes: ## Stop containers and delete the bundled PostgreSQL data
	$(COMPOSE_DB) down -v

.PHONY: logs
logs: ## Follow API logs
	$(COMPOSE_DB) logs -f api

.PHONY: ps
ps: ## Show container status
	$(COMPOSE_DB) ps

.PHONY: db-up
db-up: .env ## Start only the bundled PostgreSQL container (for `make dev`)
	$(COMPOSE_DB) up -d --wait postgres

.PHONY: docker-build
docker-build: ## Build the API image
	docker build -t $(APP_NAME):latest .

## ---------- Local ----------

.PHONY: run
run: .env ## Run the API on the host (uses DB_HOST/DB_PORT from .env)
	go run ./cmd/api

.PHONY: dev
dev: db-up ## Start bundled PostgreSQL in Docker and run the API on the host
	DB_HOST=localhost DB_PORT=$(DB_EXPOSE_PORT) go run ./cmd/api

.PHONY: build
build: ## Build the binary into ./bin
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BIN_DIR)/$(APP_NAME) ./cmd/api

## ---------- Quality ----------

.PHONY: test
test: ## Run unit tests (no database needed)
	go test -count=1 $(PKG)

.PHONY: test-race
test-race: ## Run unit tests with the race detector
	go test -race -count=1 $(PKG)

.PHONY: test-idempotency
test-idempotency: ## Run the idempotency and concurrency tests with the race detector
	go test -race -count=1 -run 'Idempotency|Concurrent' -v $(PKG)

.PHONY: e2e
e2e: ## Run end-to-end tests against a running API (start it with `make up` first)
	BASE_URL=http://localhost:$${HTTP_PORT:-8080} bash scripts/e2e.sh

.PHONY: cover
cover: ## Coverage of every package with the race detector; fails below 95%
	@bash scripts/cover.sh $(COVER_MIN)

.PHONY: vet
vet: ## Run go vet
	go vet $(PKG)

.PHONY: fmt
fmt: ## Format code
	gofmt -s -w .

.PHONY: tidy
tidy: ## Tidy go modules
	go mod tidy

.PHONY: clean
clean: ## Remove build and coverage artifacts
	rm -rf $(BIN_DIR) coverage.out coverage.html coverage.log
