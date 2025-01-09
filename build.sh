#!/bin/bash

gofmt -s -w .

go mod tidy

go env -w CGO_ENABLED=1

go test -race ./...

#go test -bench=BenchmarkController ./cmd/secrets

go env -w CGO_ENABLED=0

go install ./...

go env -u CGO_ENABLED
