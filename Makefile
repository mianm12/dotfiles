SHELL := /bin/sh
.DEFAULT_GOAL := help

GO ?= go
GOFMT ?= gofmt
BINARY ?= bin/dot

VERSION ?= $(shell git describe --tags --exact-match 2>/dev/null || printf 'dev')
COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || printf 'unknown')
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

MODULE = $(shell $(GO) list -m)
BUILDINFO_PACKAGE = $(MODULE)/internal/buildinfo
LDFLAGS = -X $(BUILDINFO_PACKAGE).Version=$(VERSION) \
	-X $(BUILDINFO_PACKAGE).Commit=$(COMMIT) \
	-X $(BUILDINFO_PACKAGE).BuildTime=$(BUILD_TIME)

.PHONY: help build run version fmt fmt-check deps deps-check vet test test-race check

help:
	@printf '%s\n' \
		'make build              构建 bin/dot 并注入构建信息' \
		'make run ARGS=version   直接运行开发构建' \
		'make version            构建并运行 dot version' \
		'make fmt                格式化 Go 代码' \
		'make test               运行快速测试' \
		'make check              运行与 CI 相同的完整门禁'

build:
	@mkdir -p "$(dir $(BINARY))"
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o "$(BINARY)" ./cmd/dot

run:
	$(GO) run -trimpath -ldflags "$(LDFLAGS)" ./cmd/dot $(ARGS)

version: build
	"$(BINARY)" version $(ARGS)

fmt:
	$(GO) fmt ./...

fmt-check:
	@unformatted="$$($(GOFMT) -l .)"; \
	if [ -n "$$unformatted" ]; then \
		printf '%s\n' 'The following files need gofmt:' "$$unformatted"; \
		exit 1; \
	fi

deps:
	$(GO) mod tidy

deps-check:
	$(GO) mod verify
	$(GO) mod tidy -diff

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

check: deps-check fmt-check vet test-race build
