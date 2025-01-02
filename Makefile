GOLANGCI_LINT = $(GOPATH)/bin/golangci-lint
GOLANGCI_LINT_VERSION = v1.61.0

$(GOLANGCI_LINT): ## Download Go linter
	curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOPATH)/bin $(GOLANGCI_LINT_VERSION)

.PHONY: ci
ci: tidy lint test
	@echo
	@echo "\033[32mEVERYTHING PASSED!\033[0m"

.PHONY: lint
lint: $(GOLANGCI_LINT) ## Run Go linter
	$(GOLANGCI_LINT) run -v ./...

.PHONY: tidy
tidy:
	go mod tidy && git diff --exit-code

.PHONY: test
test:
	go test ./...

.PHONY: bench
bench: ## Run Go benchmarks
	go test ./... -bench . -benchtime 5s -timeout 0 -run='^$$' -benchmem

.PHONY: proto
proto: ## Build protos
	./buf.gen.yaml
