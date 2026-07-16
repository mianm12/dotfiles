SHELL := /bin/sh
.DEFAULT_GOAL := help

GO ?= go
GOLANGCI_LINT ?= golangci-lint
BINARY ?= bin/dot

VERSION ?= $(shell git describe --tags --exact-match 2>/dev/null || printf 'dev')
COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || printf 'unknown')
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GOLANGCI_LINT_VERSION = $(patsubst v%,%,$(shell cat .golangci-lint-version))

MODULE = $(shell $(GO) list -m)
BUILDINFO_PACKAGE = $(MODULE)/internal/buildinfo
# 把版本信息拼成 ldflags,避免每个 target 重复写
LDFLAGS = -X '$(BUILDINFO_PACKAGE).Version=$(VERSION)' \
	-X '$(BUILDINFO_PACKAGE).Commit=$(COMMIT)' \
	-X '$(BUILDINFO_PACKAGE).BuildTime=$(BUILD_TIME)'

.PHONY: help build run version fmt fmt-check deps deps-check lint lint-version test test-race check

help:
	@printf '%s\n' \
		'make build              构建 bin/dot 并注入构建信息' \
		'make run ARGS=version   直接运行开发构建' \
		'make version            构建并运行 dot version' \
		'make fmt                格式化 Go 代码' \
		'make lint               运行静态分析' \
		'make test               运行快速测试' \
		'make check              运行与 CI 相同的完整门禁'

build:
	@mkdir -p "$(dir $(BINARY))"
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o "$(BINARY)" ./cmd/dot

run:
	$(GO) run -trimpath -ldflags "$(LDFLAGS)" ./cmd/dot $(ARGS)

version: build
	"$(BINARY)" version $(ARGS)

fmt: lint-version
	$(GOLANGCI_LINT) fmt

fmt-check: lint-version
	$(GOLANGCI_LINT) fmt --diff

deps:
	$(GO) mod tidy

deps-check:
	$(GO) mod verify
	$(GO) mod tidy -diff

lint-version:
	@actual="$$($(GOLANGCI_LINT) version --short 2>/dev/null)"; \
	if [ "$$actual" != "$(GOLANGCI_LINT_VERSION)" ]; then \
		printf 'golangci-lint version %s required, found %s\n' "$(GOLANGCI_LINT_VERSION)" "$${actual:-not installed}"; \
		exit 1; \
	fi

lint: lint-version
	$(GOLANGCI_LINT) run

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

check: deps-check fmt-check lint test-race build
