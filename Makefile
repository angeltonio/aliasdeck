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

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: tidy
tidy: ## Tidy go.mod
	go mod tidy

.PHONY: help
help: ## List targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
