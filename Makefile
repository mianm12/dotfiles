SHELL := /bin/sh
.DEFAULT_GOAL := help

# 工具和输出路径允许调用方覆盖，CI 无需复制本地构建命令。
GO ?= go
GOLANGCI_LINT ?= golangci-lint
BINARY ?= bin/dot

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

.PHONY: help build run version doctor-manifest fmt fmt-check tidy tidy-check lint test test-race check

help:
	@printf '%s\n' \
		'make build              构建 bin/dot 并注入构建信息' \
		'make run ARGS=version   直接运行开发构建' \
		'make version            构建并运行 dot version' \
		'make doctor-manifest    检查真实仓库的 manifest' \
		'make fmt                格式化 Go 代码' \
		'make tidy               整理 Go 模块依赖' \
		'make lint               运行静态分析' \
		'make test               运行快速测试' \
		'make check              运行当前平台的完整门禁（CI 在 macOS/Linux 分别执行）'

build:
	@mkdir -p "$(dir $(BINARY))"
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o "$(BINARY)" ./cmd/dot

run:
	$(GO) run -trimpath -ldflags "$(LDFLAGS)" ./cmd/dot $(ARGS)

version: build
	"$(BINARY)" version $(ARGS)

# 真实仓库检查只创建隔离 HOME 根；machine-local 控制面路径保持缺失，并由调用 shell 负责清理。
doctor-manifest: build
	@set -eu; \
	doctor_root=$$(mktemp -d /tmp/dot-doctor.XXXXXX); \
	trap 'doctor_status=$$?; rm -rf "$$doctor_root"; exit $$doctor_status' 0; \
	doctor_home="$$doctor_root/home"; \
	mkdir "$$doctor_home"; \
	env -u DOT_CONFIG -u DOT_REPO \
		HOME="$$doctor_home" \
		XDG_CONFIG_HOME="$$doctor_home/.config" \
		XDG_STATE_HOME="$$doctor_home/.local/state" \
		XDG_CACHE_HOME="$$doctor_home/.cache" \
		"$(abspath $(BINARY))" doctor --manifest-only \
		--home "$$doctor_home" --repo "$(abspath .)"

# fmt 和 tidy 会修改工作区；对应的 *-check 目标只验证，不产生修复性改动。
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

# 汇总当前平台的完整门禁，作为本地与 CI 的共同入口；任一失败都会立即停止。
check: tidy-check fmt-check lint test-race doctor-manifest
