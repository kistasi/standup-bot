BINARY := standup-bot

.DEFAULT_GOAL := help

.PHONY: help
help: ## List the available targets
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk -F':.*?## ' '{printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

.PHONY: standup
standup: ## Run the standup (prints the summary, posts to Slack when configured)
	go run .

.PHONY: monthly
monthly: ## Write a monthly report; prompts for year and month
	go run . monthly

.PHONY: build
build: ## Compile the binary
	go build -o $(BINARY) .

.PHONY: test
test: ## Run the tests as CI does (race detector + coverage)
	go test -race -cover ./...

.PHONY: check
check: fmt vet test ## Run fmt, vet, and test — everything CI checks

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: fmt
fmt: ## Report unformatted files (does not rewrite them)
	@files=$$(gofmt -l .); \
	if [ -n "$$files" ]; then echo "unformatted:"; echo "$$files"; exit 1; fi

.PHONY: tidy
tidy: ## Sync go.mod/go.sum
	go mod tidy

.PHONY: clean
clean: ## Remove the built binary
	rm -f $(BINARY)
