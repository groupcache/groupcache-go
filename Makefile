GOLANGCI_LINT = $(GOPATH)/bin/golangci-lint
GOLANGCI_LINT_VERSION = v2.15.0

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

# Tiger reads the Go toolchain at run time, so pin it to the go.mod version.
# A newer local Go makes every package fail to load (tiger exit 2).
# make exits 2 on any failing recipe, so the message says which Tiger result it was.
.PHONY: tiger
tiger: ## Run the Tiger static analyzer
	@command -v tiger >/dev/null || { echo "tiger is not installed; run: go install github.com/kapetan-io/tiger/cmd/tiger@latest"; exit 2; }
	@GOTOOLCHAIN=go$$(go list -m -f '{{.GoVersion}}') tiger check ./...; rc=$$?; \
	case $$rc in \
	0) ;; \
	1) echo "tiger: findings (exit 1)";; \
	*) echo "tiger: could not check the packages (exit $$rc)";; \
	esac; exit $$rc
