.DEFAULT_GOAL := help

.PHONY: help
help: ## List available commands
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
	  | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: run
run: ## Run the API in development mode
	go run ./cmd/api

.PHONY: migrate
migrate: ## Apply pending database migrations
	go run ./cmd/migrate

.PHONY: build
build: ## Build every binary into bin/
	go build -trimpath -o bin/ ./cmd/...

.PHONY: test
test: ## Run tests with the race detector
	go test -race ./...

.PHONY: cover
cover: ## Generate the coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "report written to coverage.html"

.PHONY: check
check: fmt-check ## Run everything CI runs
	go vet ./...
	go test -race ./...

.PHONY: fmt-check
fmt-check: ## Fail if any file is not gofmt-clean
	@out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then \
	  echo "Files not gofmt-clean:"; echo "$$out"; exit 1; \
	fi

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum
	go mod tidy

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin coverage.out coverage.html
