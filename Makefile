# Common commands. Run `make setup` once, then `make up migrate`, then `make api` and `make web`.
SHELL := /bin/bash
COMPOSE := docker compose -f deploy/local/compose.yaml
DOCKER_LOCAL := docker compose -f deploy/docker-local/compose.yaml
GO_TOOL := go tool -modfile=tools.mod
ENV := set -a; source ../.env; set +a;

.PHONY: setup up down reset migrate api worker web gen test test-go test-web lint e2e admin check docker-up docker-down docker-reset docker-logs docker-admin

setup: ## Create a local .env with fresh keys and install dependencies
	@test -f .env || (cp .env.example .env && sed -i "s|^ENCRYPTION_KEY=.*|ENCRYPTION_KEY=$$(openssl rand -base64 32)|; s|^HMAC_KEY=.*|HMAC_KEY=$$(openssl rand -base64 32)|" .env && echo "created .env")
	pnpm install

up: ## Start PostgreSQL, Redis, Mailpit, SeaweedFS and Gotenberg. Add ClamAV with: docker compose -f deploy/local/compose.yaml --profile scan up -d
	$(COMPOSE) up -d --wait

down:
	$(COMPOSE) down

reset: ## Delete local data and start clean
	$(COMPOSE) down -v
	$(COMPOSE) up -d --wait

migrate:
	cd backend && $(ENV) go run ./cmd/engine migrate

api:
	cd backend && $(ENV) go run ./cmd/engine api

worker:
	cd backend && $(ENV) go run ./cmd/engine worker

web: ## Start the client on 5180 and the admin app on 5181
	pnpm -r --parallel --filter @msp/client --filter @msp/admin dev

admin: ## Create a Super Admin: make admin EMAIL=you@example.test NAME="You" (prompts for a password)
	@read -rsp "Password (12+ characters): " ADMIN_PASSWORD; echo; export ADMIN_PASSWORD; cd backend && $(ENV) go run ./cmd/engine create-admin --email "$(EMAIL)" --name "$(NAME)"

gen: ## Regenerate code from the contracts and SQL
	cd backend && $(GO_TOOL) sqlc generate
	cd backend && $(GO_TOOL) oapi-codegen -config internal/api/tenantapi/oapi.yaml ../api/openapi/tenant.yaml
	cd backend && $(GO_TOOL) oapi-codegen -config internal/api/adminapi/oapi.yaml ../api/openapi/admin.yaml
	cd backend && $(GO_TOOL) oapi-codegen -config internal/api/modulesapi/oapi.yaml ../api/openapi/modules.yaml
	cd backend && $(GO_TOOL) oapi-codegen -config internal/modules/leadgeneration/httpapi/oapi.yaml ../api/openapi/modules/lead-generation.yaml
	cd backend && $(GO_TOOL) oapi-codegen -config internal/modules/competitorintel/httpapi/oapi.yaml ../api/openapi/modules/competitor-intelligence.yaml
	cd backend && $(GO_TOOL) oapi-codegen -config internal/modules/crm/httpapi/oapi.yaml ../api/openapi/modules/crm.yaml
	cd backend && $(GO_TOOL) oapi-codegen -config internal/modules/proposals/httpapi/oapi.yaml ../api/openapi/modules/proposals.yaml
	cd backend && $(GO_TOOL) oapi-codegen -config internal/modules/itassessment/httpapi/oapi.yaml ../api/openapi/modules/it-assessment.yaml
	cd backend && $(GO_TOOL) oapi-codegen -config internal/modules/outreach/httpapi/oapi.yaml ../api/openapi/modules/outreach.yaml
	cd backend && $(GO_TOOL) oapi-codegen -config internal/modules/visitorintel/httpapi/oapi.yaml ../api/openapi/modules/visitor-intelligence.yaml
	cd backend && $(GO_TOOL) oapi-codegen -config internal/modules/chatbot/httpapi/oapi.yaml ../api/openapi/modules/chatbot.yaml
	cd backend && $(GO_TOOL) oapi-codegen -config internal/modules/customerexpansion/httpapi/oapi.yaml ../api/openapi/modules/customer-expansion.yaml
	cd backend && $(GO_TOOL) oapi-codegen -config internal/modules/salesintel/httpapi/oapi.yaml ../api/openapi/modules/sales-intelligence.yaml
	pnpm gen:api

test: test-go test-web

test-go: ## Needs Docker: the suite runs against a real PostgreSQL
	cd backend && go test -race -count=1 -p 2 ./...

test-web:
	pnpm test

lint:
	cd backend && $(GO_TOOL) golangci-lint run ./...
	pnpm lint
	pnpm typecheck

test-live: ## Real SeaweedFS and Gotenberg adapters. Needs `make up`
	cd backend && MSP_LIVE_INFRA=1 go test -count=1 -run 'SeaweedFS|Gotenberg|ClamAV' ./internal/platform/files ./internal/platform/pdf

e2e: ## Full browser flow. Needs `make up migrate` first
	pnpm e2e

check: gen lint test ## Everything CI runs, except the browser tests
	git diff --exit-code --stat -- '*.gen.go' 'backend/internal/**/repo/*' 'packages/types/src/*.gen.ts' || (echo "generated code is out of date" && exit 1)

# The whole app in Docker, with no Go or Node needed on the machine. Local development only.
docker-up: ## Build and start everything in Docker: client on 8180, admin on 8181, Mailpit on 8025
	@test -f deploy/docker-local/.env || (printf 'ENCRYPTION_KEY=%s\nHMAC_KEY=%s\n' "$$(openssl rand -base64 32)" "$$(openssl rand -base64 32)" > deploy/docker-local/.env && echo "created deploy/docker-local/.env")
	$(DOCKER_LOCAL) up -d --build --wait

docker-down: ## Stop the Docker stack and keep its data
	$(DOCKER_LOCAL) down

docker-reset: ## Delete the Docker stack's data and start clean
	$(DOCKER_LOCAL) down --volumes
	$(MAKE) docker-up

docker-logs:
	$(DOCKER_LOCAL) logs -f api worker

docker-admin: ## Create a Super Admin in the Docker stack: make docker-admin EMAIL=you@example.test NAME="You"
	@read -rsp "Password (12+ characters): " ADMIN_PASSWORD; echo; export ADMIN_PASSWORD; $(DOCKER_LOCAL) run --rm --no-deps -e ADMIN_PASSWORD api create-admin --email "$(EMAIL)" --name "$(NAME)"
