MODULE  := github.com/aphexddb/omarket
VERSION := $(shell cat VERSION)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%d)
BIN     := omarket$(shell go env GOEXE)

LDFLAGS := -s -w \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT) \
	-X $(MODULE)/internal/version.Date=$(DATE)

.PHONY: build test examples
build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/omarket

test:
	go test ./...

examples:
	$(MAKE) -C examples demo
