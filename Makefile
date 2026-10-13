# Runcoil server. Tools run through `go run`, pinned by version. Nothing is installed globally.

GOLANGCI_LINT_VERSION ?= v2.14.0
SQLC_VERSION ?= v1.31.1
GOOSE_VERSION ?= v3.26.0
GOVULNCHECK_VERSION ?= v1.9.0
ACTIONLINT_VERSION ?= v1.7.12

#########################
# Run
#########################
# dev-token is for local development only. It applies only when the caller has not set ENGINE__TOKEN,
# so the caller's value is passed through as it is and is not echoed by make.
ifeq ($(origin ENGINE__TOKEN),undefined)
DEV_TOKEN_ENV = ENGINE__TOKEN=dev-token
endif

.PHONY: run-runcoil
run-runcoil:
	$(DEV_TOKEN_ENV) go run ./cmd/runcoil/

.PHONY: run-migrate
run-migrate:
	go run ./cmd/migrate/

#########################
# Docker Compose
#########################
.PHONY: dc-up
dc-up:
	docker compose -f docker-compose.yml up -d

.PHONY: dc-down
dc-down:
	docker compose -f docker-compose.yml down

#########################
# Database migration
#########################
GOOSE_CMD = go run github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION)

GOOSE_DRIVER ?= postgres
GOOSE_DBSTRING ?= postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable
GOOSE_MIGRATION_DIR ?= internal/storage/postgres/migration

GOOSE_ENV = \
	GOOSE_DRIVER=$(GOOSE_DRIVER) \
	GOOSE_DBSTRING=$(GOOSE_DBSTRING) \
	GOOSE_MIGRATION_DIR=$(GOOSE_MIGRATION_DIR)

.PHONY: migrate-up
migrate-up:
	$(GOOSE_ENV) $(GOOSE_CMD) up

.PHONY: migrate-down
migrate-down:
	$(GOOSE_ENV) $(GOOSE_CMD) down

.PHONY: migrate-status
migrate-status:
	$(GOOSE_ENV) $(GOOSE_CMD) status

.PHONY: migrate-create
migrate-create:
	@if [ -z "$(name)" ]; then \
		echo "invalid command. Usage: make migrate-create name=<migration_name>"; exit 1; \
	fi
	$(GOOSE_ENV) $(GOOSE_CMD) create "$(name)" sql

#########################
# Generate
#########################
.PHONY: gen-sqlc
gen-sqlc:
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION) generate -f internal/storage/postgres/sqlc/sqlc.yml

#########################
# Testing
#########################
.PHONY: test
test:
	go test -race -count=1 ./...

.PHONY: test-cov
test-cov:
	mkdir -p bin
	go test -coverprofile=bin/coverage.out ./...
	go tool cover -html=bin/coverage.out -o bin/coverage.html
	@echo "Coverage report saved to bin/coverage.html"

#########################
# Lint
#########################
.PHONY: lint
lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./... --config .golangci.yml

#########################
# Vulnerability and workflow checks
#########################
.PHONY: vuln
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

.PHONY: actionlint
actionlint:
	go run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)
