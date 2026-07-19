GO ?= go
GORELEASER ?= goreleaser

BINARY := claude-code-acp-adapter
CMD := ./cmd/$(BINARY)
VERSION ?= $(patsubst v%,%,$(shell git describe --tags --always --dirty 2>/dev/null || echo 0.0.0-dev))
LDFLAGS := -s -w -X github.com/hecatehq/claude-code-acp-adapter/internal/app.Version=$(VERSION)

.PHONY: test test-race real-cli-smoke vet tidy-check build version-smoke release-check snapshot clean

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

real-cli-smoke:
	ACP_ADAPTER_REAL_CLI_SMOKE=1 $(GO) test -tags real_cli -run TestRealClaudeCodeCLISmoke -count=1 -timeout 10m ./claudecodeadapter

vet:
	$(GO) vet ./...

tidy-check:
	$(GO) mod tidy -diff

build:
	mkdir -p bin
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(CMD)

version-smoke: build
	test "$$(bin/$(BINARY) --version)" = "$(BINARY) $(VERSION)"

release-check: test test-race vet tidy-check version-smoke

snapshot:
	$(GORELEASER) release --snapshot --clean

clean:
	rm -rf bin dist
