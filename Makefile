GO ?= go
GORELEASER ?= goreleaser

BINARY := claude-code-acp-adapter
CMD := ./cmd/$(BINARY)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.0.0-dev)
LDFLAGS := -s -w -X github.com/hecatehq/claude-code-acp-adapter/internal/app.Version=$(VERSION)

.PHONY: test test-race vet build version-smoke release-check snapshot clean

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

build:
	mkdir -p bin
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(CMD)

version-smoke: build
	test "$$(bin/$(BINARY) --version)" = "$(BINARY) $(VERSION)"

release-check: test test-race vet version-smoke

snapshot:
	$(GORELEASER) release --snapshot --clean

clean:
	rm -rf bin dist
