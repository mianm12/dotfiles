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

需要 Go 1.25 或更高版本。常用检查：

```sh
gofmt -w ./cmd ./internal
go mod tidy -diff
go vet ./...
go test -race ./...
go build -trimpath -o ./bin/dot ./cmd/dot
```

运行开发构建：

```sh
./bin/dot version
```

开发构建报告 `version=dev`。它仍会校验 `requires` 的存在和语法，只跳过发布版本的大小
比较，并明确输出警告。

分支、提交与评审约定见 [CONTRIBUTING.md](CONTRIBUTING.md)。
