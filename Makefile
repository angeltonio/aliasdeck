.DEFAULT_GOAL := check

.PHONY: check
check: fmt vet test ## Run everything CI runs

.PHONY: test
test: ## Run the test suite
	go test ./...

.PHONY: test-race
test-race: ## Run the test suite with the race detector
	go test -race ./...

.PHONY: cover
cover: ## Report per-package coverage
	go test -cover ./...

.PHONY: golden
golden: ## Rewrite renderer golden files, then read the diff before committing
	go test ./internal/renderers -update
	@echo
	@echo "Golden files rewritten. Review the diff — these bytes go into a user's shell."

.PHONY: fmt
fmt: ## Format the module
	gofmt -l -w .

.PHONY: fmt-check
fmt-check: ## Fail if anything needs formatting, without rewriting it
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "These files are not gofmt-clean:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

.PHONY: ci
ci: fmt-check vet test-race ## What CI runs
	@echo "CI checks passed"

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: tidy
tidy: ## Tidy go.mod
	go mod tidy

# Version-pinned per design decision 6: sqlc is invoked via `go run`, never
# tracked as a go.mod tool directive (its own dependency tree forces an
# automatic `go` directive bump this project does not want).
SQLC_VERSION := v1.29.0

DEV_COMPOSE := docker compose -p aliasdeck-dev -f compose.dev.yaml
DEV_ROOT := $(CURDIR)/build/aliasdeck-dev
DEV_BINARY := $(DEV_ROOT)/bin/aliasdeck
DEV_CLIENT_HOME := $(DEV_ROOT)/client
DEV_WATCH_INTERVAL := 5s

.PHONY: dev-up
dev-up: ## Run the hot-reloading development server in the foreground
	$(DEV_COMPOSE) up --build

.PHONY: dev-down
dev-down: ## Stop the development server while preserving its state
	$(DEV_COMPOSE) down

.PHONY: dev-logs
dev-logs: ## Follow development server and hot-reload logs
	$(DEV_COMPOSE) logs --follow server

.PHONY: dev-reset
dev-reset: ## Remove only the dedicated development server and client state
	$(DEV_COMPOSE) down --volumes --remove-orphans
	@if [ "$$(uname -s)" = Darwin ]; then \
		go run ./cmd/aliasdeck agent uninstall --if-executable "$(DEV_BINARY)" --if-home "$(DEV_CLIENT_HOME)" --if-interval "$(DEV_WATCH_INTERVAL)"; \
	fi
	@if [ -x "$(DEV_BINARY)" ] && [ -f "$(DEV_CLIENT_HOME)/config.yaml" ] && [ -f "$(DEV_CLIENT_HOME)/state.json" ]; then \
		ALIASDECK_HOME="$(DEV_CLIENT_HOME)" "$(DEV_BINARY)" uninstall --yes; \
	fi
	@test "$(DEV_ROOT)" = "$(CURDIR)/build/aliasdeck-dev"
	@if [ -d "$(DEV_ROOT)" ]; then find "$(DEV_ROOT)" -depth -delete; fi

.PHONY: sqlc-generate
sqlc-generate: ## Regenerate internal/store/sqlitestore from query.sql (sqlc)
	cd internal/store/sqlitestore && go run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION) generate

.PHONY: sqlc-diff
sqlc-diff: sqlc-generate ## Fail if `sqlc generate` drifts from the checked-in code (task 10.3)
	@if ! git diff --exit-code -- internal/store/sqlitestore; then \
		echo "sqlc generate produced a diff above — regenerate and commit internal/store/sqlitestore."; \
		exit 1; \
	fi

.PHONY: help
help: ## List targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
