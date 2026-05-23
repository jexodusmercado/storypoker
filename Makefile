.PHONY: help dev up down clean restart logs logs-server logs-web ps \
        shell-server shell-web \
        lint lint-go lint-web typecheck test \
        build build-server build-web

# Default target shows help.
.DEFAULT_GOAL := help

help: ## Show this help.
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make <target>\n\nTargets:\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# ── dev lifecycle ──────────────────────────────────────────────────────────────

dev: ## Run full stack with hot reload (server + web). Visit http://localhost:5173
	docker compose up

up: ## Start stack detached.
	docker compose up -d

down: ## Stop stack and remove containers.
	docker compose down

clean: ## Stop stack and wipe volumes (node_modules, go caches).
	docker compose down -v
	rm -rf web/dist web/node_modules server/server server/tmp

restart: ## Restart both services (use when air gets stuck on a build error).
	docker compose restart

restart-server: ## Restart only the server container.
	docker compose restart server

restart-web: ## Restart only the web container.
	docker compose restart web

# ── inspection ─────────────────────────────────────────────────────────────────

logs: ## Tail logs from both services.
	docker compose logs -f

logs-server: ## Tail server logs only.
	docker compose logs -f server

logs-web: ## Tail web logs only.
	docker compose logs -f web

ps: ## Show running containers.
	docker compose ps

shell-server: ## Drop into a shell in the server container.
	docker compose exec server sh

shell-web: ## Drop into a shell in the web container.
	docker compose exec web sh

# ── linting + typechecking ─────────────────────────────────────────────────────

lint: lint-go lint-web ## Lint Go + JS/TS.

lint-go: ## Run gofmt check + go vet.
	@cd server && \
		out=$$(gofmt -l .) && \
		if [ -n "$$out" ]; then echo "gofmt issues:"; echo "$$out"; exit 1; fi && \
		go vet ./...

lint-web: ## Run ESLint on the web app.
	cd web && npm run lint

typecheck: ## Run TypeScript and Go type checks.
	cd web && npx tsc --noEmit -p tsconfig.app.json
	cd server && go vet ./...

# ── tests ──────────────────────────────────────────────────────────────────────

test: ## Run Go tests.
	cd server && go test ./...

# ── builds ─────────────────────────────────────────────────────────────────────

build: build-server build-web ## Build both backend and frontend artifacts.

build-server: ## Build the Railway backend Docker image (Go binary).
	docker build -t storypoker-server:latest .

build-web: ## Build the Vercel frontend bundle into web/dist.
	cd web && npm run build
	@echo "bundle at: $$(pwd)/web/dist"
