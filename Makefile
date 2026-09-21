.PHONY: build test vet lint bench check

build:
	go build -o bin/nexus ./cmd/nexus

test:
	go test -race ./...

vet:
	go vet ./...

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; exit 1; }
	golangci-lint run

bench:
	go test ./internal/app -run '^$$' -bench . -benchmem

check: build vet test lint
