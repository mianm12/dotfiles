# dot

`dot` 是面向个人使用的 dotfiles 管理 CLI，采用 symlink 为主、模板生成为辅的模型。
它的数据保护目标是避免工具自身的 bug 误删或误覆盖用户数据，不把恶意仓库、恶意 hook、
被攻陷的本机或主动并发篡改当作需要对抗的环境。

项目正在实现 M1。目前已提供首个只读切片 `dot version`；其他命令以
[路线图](docs/09-roadmap.md)为准，未实现能力不会以静默降级替代。

## 文档与实现

[设计与行为规范](docs/README.md)规定必须成立的性质、公开行为和持久化契约；代码与测试
决定如何实现和验证这些性质。内部包、算法和测试组织不是独立的规范来源。

## 本地开发

需要 Go 1.25 或更高版本，以及兼容当前 `.golangci.yml` 的 golangci-lint。本地命令使用
已安装版本，CI 使用 `latest`。常用命令：

```sh
make build
make fmt
make tidy
make lint
make test
make check
```

运行开发构建：

```sh
make version
# 或透传其他参数
make run ARGS='version --repo ~/src/dotfiles'
```

未显式设置 `VERSION` 时，Makefile 只在工作区干净且当前提交精确命中 Git tag 时自动注入
该 tag；其他构建使用 `dev`。短 commit 和 UTC 构建时间仍会自动注入，无需日常手工传递
`-ldflags`。发布或复现构建时可以显式覆盖，例如：

```sh
make build VERSION=v0.1.0 COMMIT=abc123 BUILD_TIME=2026-07-16T00:00:00Z
```

`version=dev` 的开发构建仍会校验 `requires` 的存在和语法，只跳过发布版本的大小比较，并
输出不单独改变退出码的 development compatibility notice。

配置改动一旦使用新版 CLI 能力，必须在同一 commit 提升顶层 `requires`；严格解码只是
遗漏时的失效安全兜底，不能替代这项维护纪律。

分支、提交与评审约定见 [CONTRIBUTING.md](CONTRIBUTING.md)。
