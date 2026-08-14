SHELL := /bin/sh
.DEFAULT_GOAL := help

# 工具和输出路径允许调用方覆盖，CI 无需复制本地构建命令。
GO ?= go
# 仓库命令只使用 go.mod/tools/go.mod；本机或父目录的 go.work 不得改变门禁与构建。
override GOWORK := off
export GOWORK
BINARY ?= bin/dot
INSTALL_DIR ?= $(if $(strip $(HOME)),$(HOME)/.local/bin)
FUZZ_TIME ?= 30s
GO_TOOL = $(GO) tool -modfile=tools/go.mod

# 未显式覆盖时，只有干净工作区中当前提交上的精确 tag 才作为版本；其他构建使用 dev。
VERSION ?= $(shell status=$$(git status --porcelain --untracked-files=normal 2>/dev/null) \
	&& test -z "$$status" \
	&& git describe --tags --exact-match 2>/dev/null \
	|| printf 'dev')
VERSION := $(VERSION)
COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || printf 'unknown')
COMMIT := $(COMMIT)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BUILD_TIME := $(BUILD_TIME)

# 从 module path 推导 -X 的完整包名，仓库迁移后无需同步硬编码路径。
# GNU Make 3.81 的 $(shell ...) 不继承本 Makefile 新 export 的值，必须在此显式固定。
MODULE = $(shell GOWORK=off $(GO) list -m)
BUILDINFO_PACKAGE = $(MODULE)/internal/buildinfo
# 集中构造 ldflags，确保 build、run 和 version 注入相同的构建信息。
LDFLAGS = -X '$(BUILDINFO_PACKAGE).Version=$(VERSION)' \
	-X '$(BUILDINFO_PACKAGE).Commit=$(COMMIT)' \
	-X '$(BUILDINFO_PACKAGE).BuildTime=$(BUILD_TIME)'

.PHONY: help build install run version fmt fmt-check tidy tidy-check mod-verify \
	lint test test-race test-bootstrap fuzz vuln check

help:
	@printf '%s\n' \
		'make build              构建 bin/dot 并注入构建信息' \
		'make install            安装独立 binary 到 INSTALL_DIR（默认 ~/.local/bin）' \
		'make run ARGS=version   直接运行开发构建' \
		'make version            构建并运行 dot version' \
		'make fmt                格式化 Go 代码' \
		'make tidy               整理 Go 模块依赖' \
		'make mod-verify         校验产品与工具模块的已下载依赖' \
		'make lint               运行静态分析' \
		'make test               运行快速测试' \
		'make fuzz               对 state、target 与 os-release 边界各 fuzz 30 秒' \
		'make vuln               使用固定版本 govulncheck 扫描可达漏洞' \
		'make check              运行当前平台的完整门禁（CI 在 macOS/Linux 分别执行）'

build:
	@mkdir -p "$(dir $(BINARY))"
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o "$(BINARY)" ./cmd/dot

install: build
	@install_dir="$(INSTALL_DIR)"; \
	case "$$install_dir" in \
		/*) ;; \
		*) printf 'INSTALL_DIR must be a non-empty absolute path: %s\n' "$$install_dir" >&2; exit 1 ;; \
	esac; \
	if test -e "$$install_dir" && ! test -d "$$install_dir"; then \
		printf 'INSTALL_DIR is not a directory: %s\n' "$$install_dir" >&2; \
		exit 1; \
	fi; \
	if ! test -d "$$install_dir"; then \
		mkdir -p "$$install_dir"; \
		chmod 0755 "$$install_dir"; \
	fi; \
	if test -d "$$install_dir/dot"; then \
		printf 'install destination is a directory: %s\n' "$$install_dir/dot" >&2; \
		exit 1; \
	fi; \
	tmp=$$(mktemp "$$install_dir/.dot.tmp.XXXXXX"); \
	trap 'rm -f "$$tmp"' 0 1 2 3 15; \
	cp "$(BINARY)" "$$tmp"; \
	chmod 0755 "$$tmp"; \
	mv -f "$$tmp" "$$install_dir/dot"; \
	tmp=; \
	trap - 0 1 2 3 15; \
	printf 'installed %s\n' "$$install_dir/dot"

run:
	$(GO) run -trimpath -ldflags "$(LDFLAGS)" ./cmd/dot $(ARGS)

version: build
	"$(BINARY)" version $(ARGS)

# fmt 和 tidy 会修改工作区；对应的 *-check 目标只验证，不产生修复性改动。
fmt:
	$(GO_TOOL) golangci-lint fmt

fmt-check:
	$(GO_TOOL) golangci-lint fmt --diff

tidy:
	$(GO) mod tidy
	$(GO) -C tools mod tidy

tidy-check:
	$(GO) mod tidy -diff
	$(GO) -C tools mod tidy -diff

mod-verify:
	$(GO) mod verify
	$(GO) -C tools mod verify

lint:
	$(GO_TOOL) golangci-lint run

test:
	$(GO) test ./...
	$(MAKE) test-bootstrap

test-race:
	$(GO) test -race ./...
	$(MAKE) test-bootstrap

test-bootstrap:
	./tests/bootstrap_test.sh

fuzz:
	$(GO) test ./internal/core/state -run '^$$' -fuzz '^FuzzDecode$$' -fuzztime '$(FUZZ_TIME)'
	$(GO) test ./internal/core/paths -run '^$$' -fuzz '^FuzzTargetExpression$$' -fuzztime '$(FUZZ_TIME)'
	$(GO) test ./internal/cli -run '^$$' -fuzz '^FuzzOSReleaseID$$' -fuzztime '$(FUZZ_TIME)'

vuln:
	$(GO_TOOL) govulncheck ./...

# 汇总当前平台的完整门禁，作为本地与 CI 的共同入口；任一失败都会立即停止。
check: mod-verify tidy-check fmt-check lint test-race build
	@actual=$$("$(BINARY)" version); status=$$?; \
	expected=$$(printf 'version=%s\ncommit=%s\nbuild_time=%s' \
		"$(VERSION)" "$(COMMIT)" "$(BUILD_TIME)"); \
	if test "$$status" -ne 0 || test "$$actual" != "$$expected"; then \
		printf 'built binary version check failed (exit %s)\n' "$$status" >&2; \
		printf '%s\n%s\n%s\n%s\n' "expected:" "$$expected" "actual:" "$$actual" >&2; \
		exit 1; \
	fi
