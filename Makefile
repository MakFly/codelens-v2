.PHONY: build test bench install clean lint watcher-start watcher-stop watcher-status release

BINARY_DIR := bin
SERVER_BIN  := $(BINARY_DIR)/codelens
HOOK_BIN    := $(BINARY_DIR)/codelens-hook

## Build both binaries
build:
	@mkdir -p $(BINARY_DIR)
	go build -o $(SERVER_BIN) ./cmd/codelens
	go build -o $(HOOK_BIN) ./cmd/hook
	@echo "✓ Built: $(SERVER_BIN) and $(HOOK_BIN)"

## Build release archives (requires GoReleaser)
release:
	@goreleaser release --clean --snapshot || echo "Run 'goreleaser release' to create releases"

## Install binaries to /usr/local/bin (requires sudo or adjust PATH)
install: build
	cp $(SERVER_BIN) /usr/local/bin/codelens
	cp $(HOOK_BIN)   /usr/local/bin/codelens-hook
	@echo "✓ Installed codelens and codelens-hook to /usr/local/bin"

## Run unit tests (no Ollama required)
test:
	go test ./internal/... -v -count=1

## Run unit tests with race detector
test-race:
	go test ./internal/... -race -count=1

## Run benchmarks (requires Ollama + indexed project)
bench:
	CODELENS_BENCHMARK=1 go test ./test/benchmark/... -run TestTokenSavings -v
	CODELENS_BENCHMARK=1 go test ./test/benchmark/... -bench=BenchmarkSearchLatency -benchtime=10s

## Index the current project (requires Ollama)
index:
	go run ./cmd/codelens index . --watch=false
	@echo "✓ Project indexed"

## Start MCP server (stdio mode, for testing)
serve:
	go run ./cmd/codelens serve

## Lint
lint:
	golangci-lint run ./...

## Clean build artifacts and index
clean:
	rm -rf $(BINARY_DIR)
	rm -rf .codelens/

## Show index stats
stats:
	go run ./cmd/codelens stats

## Test a search query
search:
	@read -p "Query: " q && go run ./cmd/codelens search "$$q"

## Start background watcher daemon
watcher-start:
	go run ./cmd/codelens watcher start .

## Stop background watcher daemon
watcher-stop:
	go run ./cmd/codelens watcher stop .

## Show watcher daemon status
watcher-status:
	go run ./cmd/codelens watcher status .
