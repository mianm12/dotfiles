# dot

> [!WARNING]
> 历史项目说明，记录重设计前的实现状态，不再指导当前开发。

> [!IMPORTANT]
> 项目已经进入替换式重设计阶段。[新设计基线](docs/design-baseline.md)规定后续目标；当前代码
> 仍是待替换的旧实现，下面的能力说明只描述当前 checkout，不代表新设计继续保留这些能力。
> 重设计前规范已经移入[历史存档](docs/archive/)。

## 当前旧实现状态

`dot` 是面向个人使用的 dotfiles 管理 CLI，采用 symlink 为主、模板生成为辅的模型。
它的数据保护目标是避免工具自身的 bug 误删或误覆盖用户数据，不把恶意仓库、恶意 hook、
被攻陷的本机或主动并发篡改当作需要对抗的环境。

项目正在实现 M1。目前已提供 `dot version`、`dot doctor --manifest-only`、`dot init`、`dot diff`、
`dot apply`、`dot status` 和 `dot add`。init 会先在无锁、零写入阶段从用户终端完成 profile、data
与立即 apply 决策，也支持 `--profile`、可重复 `--set key=value` 和 `--yes` 无人值守；配置以 0600
安全提交后，可在同一锁所有权内复用完整 apply pipeline。`--yes` 只授权立即 apply 与整模块 prune
确认，不隐含 force 或 adopt；后续 state/apply 失败不会回滚已提交配置。
diff、apply dry-run 与 status 通过同一个严格 planner 分别展示
动作计划和当前 profile 的健康巡检；这些只读入口不取锁，也不写 target、state 或 backup。
非 dry-run apply 已接入 link/scaffold、force backup replacement、P1/P2/P3 prune 与 M1
string-form `run_once`，支持 full/partial scope、整模块 orphan 确认和部分成功后的安全重跑；
hook 在 file/prune 后按模块与声明顺序串行执行，实时继承调用方 stdio，成功指纹与 file/prune
效果通过同一次 state commit 发布。
add 支持把 HOME 中的普通文件收编为默认 link 或 `--scaffold`，可用 `-m` 显式选择当前 profile
中的既有 module；它先对整个输入批次完成同源 manifest、路径与系统 Git 可跟踪性预检，再按
source-first 协议提交，且不会创建 module、修改 manifest 或执行 `git add`/`git commit`。
`add --dry-run` 无锁零写入；M1 的 `add --template` 明确报不支持，不会按 link 静默处理。
`doctor --manifest-only` 检查仓库 manifest、当前 GOOS 下各 profile 的 effective 路径边界、模板和
Git index 中已跟踪的 `*.local`，不读取机器配置或 state。裸 `dot doctor` 的完整环境巡检属于
M2；当前会明确提示改用 `--manifest-only` 并失败，不把静态子集伪装成完整检查。旧实现曾按
[历史路线图](docs/archive/legacy/09-roadmap.md)推进；该路线图不再约束后续实现。

当前受版本管理的根 `dot.toml` 只有空的 `mac` profile，`modules/` 尚未建立。`mac` 只是 profile
名，不是 Darwin 过滤条件；不传 `--profile` 时，macOS 与 Linux 都会在各自当前 GOOS 上验证它。

## 文档与实现

[设计基线](docs/design-baseline.md)是后续实现唯一的规范契约；[文档索引](docs/README.md)
说明当前设计与历史存档的边界。README、代码和测试决定当前已经实现什么。重写完成前，旧
实现与新基线不一致是已知过渡状态，不得把目标行为描述为当前能力。

## 本地开发

需要 Go 1.25 或更高版本；当前静态检查基线是 golangci-lint v2.12.2。CI 固定安装该版本，
本地也应使用同一版本，避免工具升级与代码改动混在一次门禁结果中。常用命令：

```sh
make build
make fmt
make tidy
make lint
make test
make doctor-manifest
make check
```

`make doctor-manifest` 构建 CLI，并用自动清理的隔离 HOME 检查当前真实仓库；`make check` 已包含
该门禁，CI 在 macOS 与 Linux 上运行同一入口。Git index 中任何已跟踪的 `*.local` 都会使
manifest-only 与 CI 失败；`.gitignore` 只负责预防新的误跟踪，不能替代这项历史巡检。

运行开发构建：

```sh
make version
# 或透传其他参数
make run ARGS='version --repo ~/src/dotfiles'
make run ARGS='doctor --manifest-only --repo ~/src/dotfiles'
```

未显式设置 `VERSION` 时，Makefile 只在工作区干净且当前提交精确命中 Git tag 时自动注入
该 tag；其他构建使用 `dev`。短 commit 和 UTC 构建时间仍会自动注入，无需日常手工传递
`-ldflags`。发布或复现构建时可以显式覆盖，例如：

```sh
make build VERSION=v0.1.0 COMMIT=abc123 BUILD_TIME=2026-07-16T00:00:00Z
```

旧实现中，`version=dev` 的开发构建仍会校验 `requires` 的存在和语法，只跳过发布版本的大小比较，并
输出不单独改变退出码的 development compatibility notice。

在旧实现被替换前，配置改动一旦使用其新版 CLI 能力，仍须在同一 commit 提升顶层
`requires`。新设计不包含 `requires`，不得把这套兼容机制带入重写实现。

分支、提交与评审约定见 [CONTRIBUTING.md](CONTRIBUTING.md)。
