.PHONY: bench
bench: ## Run Go benchmarks
	go test ./... -bench . -benchtime 5s -timeout 0 -run='^$$' -benchmem

.PHONY: proto
proto: ## Build protos
	./buf.gen.yaml
