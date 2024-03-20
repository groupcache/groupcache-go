GOLANGCI_LINT = $(GOPATH)/bin/golangci-lint
GOLANGCI_LINT_VERSION = 1.56.2

$(GOLANGCI_LINT): ## Download Go linter
	curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOPATH)/bin $(GOLANGCI_LINT_VERSION)

.PHONY: lint
lint: $(GOLANGCI_LINT) ## Run Go linter
	$(GOLANGCI_LINT) run -v ./...
	#$(GOLANGCI_LINT) run -v -c .golangci.yml ./...

.PHONY: test
test:
	go test ./...

.PHONY: bench
bench: ## Run Go benchmarks
	go test ./... -bench . -benchtime 5s -timeout 0 -run='^$$' -benchmem

.PHONY: proto
proto: ## Build protos
	./buf.gen.yaml
