SHELL := /bin/sh
.DEFAULT_GOAL := help

GO ?= go
GOLANGCI_LINT ?= golangci-lint
BINARY ?= bin/dot

VERSION ?= $(shell git describe --tags --exact-match 2>/dev/null || printf 'dev')
COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || printf 'unknown')
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

MODULE = $(shell $(GO) list -m)
BUILDINFO_PACKAGE = $(MODULE)/internal/buildinfo
# 把版本信息拼成 ldflags,避免每个 target 重复写
LDFLAGS = -X '$(BUILDINFO_PACKAGE).Version=$(VERSION)' \
	-X '$(BUILDINFO_PACKAGE).Commit=$(COMMIT)' \
	-X '$(BUILDINFO_PACKAGE).BuildTime=$(BUILD_TIME)'

.PHONY: help build run version fmt fmt-check tidy tidy-check lint test test-race check

help:
	@printf '%s\n' \
		'make build              构建 bin/dot 并注入构建信息' \
		'make run ARGS=version   直接运行开发构建' \
		'make version            构建并运行 dot version' \
		'make fmt                格式化 Go 代码' \
		'make tidy               整理 Go 模块依赖' \
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

fmt:
	$(GOLANGCI_LINT) fmt

fmt-check:
	$(GOLANGCI_LINT) fmt --diff

tidy:
	$(GO) mod tidy

tidy-check:
	$(GO) mod tidy -diff

lint:
	$(GOLANGCI_LINT) run

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

check: tidy-check fmt-check lint test-race build
