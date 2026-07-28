SHELL := /bin/sh
.DEFAULT_GOAL := help

# 工具和输出路径允许调用方覆盖，CI 无需复制本地构建命令。
GO ?= go
BINARY ?= bin/dot
FUZZ_TIME ?= 30s

# 未显式覆盖时，只有干净工作区中当前提交上的精确 tag 才作为版本；其他构建使用 dev。
VERSION ?= $(shell status=$$(git status --porcelain --untracked-files=normal 2>/dev/null) \
	&& test -z "$$status" \
	&& git describe --tags --exact-match 2>/dev/null \
	|| printf 'dev')
COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || printf 'unknown')
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# 从 module path 推导 -X 的完整包名，仓库迁移后无需同步硬编码路径。
MODULE = $(shell $(GO) list -m)
BUILDINFO_PACKAGE = $(MODULE)/internal/buildinfo
# 集中构造 ldflags，确保 build、run 和 version 注入相同的构建信息。
LDFLAGS = -X '$(BUILDINFO_PACKAGE).Version=$(VERSION)' \
	-X '$(BUILDINFO_PACKAGE).Commit=$(COMMIT)' \
	-X '$(BUILDINFO_PACKAGE).BuildTime=$(BUILD_TIME)'

.PHONY: help build run version fmt fmt-check tidy tidy-check mod-verify lint \
	test test-race fuzz vuln check

help:
	@printf '%s\n' \
		'make build              构建 bin/dot 并注入构建信息' \
		'make run ARGS=version   直接运行开发构建' \
		'make version            构建并运行 dot version' \
		'make fmt                格式化 Go 代码' \
		'make tidy               整理 Go 模块依赖' \
		'make mod-verify         校验已下载模块与 go.sum' \
		'make lint               运行静态分析' \
		'make test               运行快速测试' \
		'make fuzz               对 state 与 target 安全边界各 fuzz 30 秒' \
		'make vuln               使用固定版本 govulncheck 扫描可达漏洞' \
		'make check              运行当前平台的完整门禁（CI 在 macOS/Linux 分别执行）'

build:
	@mkdir -p "$(dir $(BINARY))"
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o "$(BINARY)" ./cmd/dot

run:
	$(GO) run -trimpath -ldflags "$(LDFLAGS)" ./cmd/dot $(ARGS)

version: build
	"$(BINARY)" version $(ARGS)

# fmt 和 tidy 会修改工作区；对应的 *-check 目标只验证，不产生修复性改动。
fmt:
	$(GO) tool golangci-lint fmt

fmt-check:
	$(GO) tool golangci-lint fmt --diff

tidy:
	$(GO) mod tidy

tidy-check:
	$(GO) mod tidy -diff

mod-verify:
	$(GO) mod verify

lint:
	$(GO) tool golangci-lint run

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

fuzz:
	$(GO) test ./internal/core/state -run '^$$' -fuzz '^FuzzDecode$$' -fuzztime '$(FUZZ_TIME)'
	$(GO) test ./internal/core/paths -run '^$$' -fuzz '^FuzzTargetExpression$$' -fuzztime '$(FUZZ_TIME)'

vuln:
	$(GO) tool govulncheck ./...

# 汇总当前平台的完整门禁，作为本地与 CI 的共同入口；任一失败都会立即停止。
check: mod-verify tidy-check fmt-check lint test-race
