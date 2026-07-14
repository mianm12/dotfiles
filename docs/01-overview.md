# 01 · 概览:目标、术语与关键决策

## 1. 背景与动机

现有工具在个人场景下各有别扭之处:GNU Stow 无法模板化、目录折叠会把运行时垃圾卷进仓库;
chezmoi 全 generate 模型丢失了「改了就是改了」的直觉,且概念负担偏重。`dot` 的定位是
**为一个人、两类系统(macOS → Linux)量身定制的最小完备工具**,同时作为一个可长期维护的
Go 练手项目。

## 2. 目标(Goals)

1. 一条命令从裸机到可用环境:`curl … | sh` → `dot init` → 完成。
2. 混合模型:默认 symlink(双向、直觉),模板文件才 generate(单向、可参数化)。
3. 支持 macOS 与 Linux 的路径/内容差异,不靠复制粘贴两份配置。
4. 私有内容(密钥、公司配置)通过 `*.local` 约定留在本机,永不入库。
5. `apply` 严格幂等、可 dry-run、破坏性操作必须显式确认。
6. CLI 与配置同仓库,但版本解耦:配置更新走 `git pull`,二进制走 Releases,
   以 `requires` 字段作为兼容性铰链。

## 3. 非目标(Non-Goals)

明确不做,避免范围蔓延:

- **多用户/团队特性**:权限体系、共享 profile 市场、远端配置中心。
- **密钥托管**:不集成系统钥匙串、1Password。私密内容走 `*.local`;若未来需要同步
  加密文件,直接集成 age 加解密单个文件即可(M3 之后再议)。
- **包管理器抽象层**:不封装 brew/apt 的统一接口,软件安装交给 `Brewfile` + hooks 脚本。
- **配置内容的语义理解**:不解析 zshrc/vimrc 内容,只管文件级别的分发。
- **Windows 支持**。
- **守护进程/文件监听**:不做 watch 自动同步,所有动作由用户显式触发。

## 4. 术语表

| 术语 | 定义 |
|---|---|
| **模块(module)** | `modules/` 下的一个子目录,是分发的最小单元,内部结构镜像 target 路径。贯穿代码、manifest、CLI 的统一用语(刻意不用 "package",避免与 Go package 混淆) |
| **profile** | 顶层 manifest 中定义的模块分组,如 `base`、`mac`、`work`,支持 `@` 嵌套引用 |
| **target** | 模块内容要落地的根目录,默认 `~` |
| **managed 模板** | `.tmpl` 后缀,每次 apply 重新渲染,产物归工具管,用户手改视为 drift |
| **scaffold 模板** | `.template` 后缀,目标不存在时生成一次,之后永不覆盖,产物归用户 |
| **`*.local` 约定** | 共享配置中固定 source 同名 `.local` 文件;`.local` 文件本身不入库,通常由 scaffold 首次生成 |
| **state 清单** | `~/.local/state/dot/state.json`,记录所有由 dot 创建的文件,用于孤儿清理与 drift 检测 |
| **机器配置** | `~/.config/dot/config.toml`,不入库,记录本机 profile 与模板变量 |
| **desired state** | 由 manifest + 模块文件树 + 机器配置推导出的「apply 后应有的文件系统状态」 |
| **drift** | 实际文件系统状态偏离 state 清单记录(managed 产物被手改、链接被删等) |
| **孤儿(orphan)** | state 清单中存在、但已不在 desired state 中的条目(源文件被删、模块被移出 profile 等) |

## 5. 关键决策记录(ADR)

| # | 决策 | 备选项 | 理由 |
|---|---|---|---|
| ADR-1 | 实现语言 **Go** | Rust、脚本语言 | 交叉编译单二进制,裸机可跑;`text/template` 标准库直接服务模板功能 |
| ADR-2 | **混合模型**:symlink 默认 + 模板 generate | 全 symlink(stow)/ 全 generate(chezmoi) | 90% 文件不需模板,保留双向直觉;仅在必要处付出单向复杂度 |
| ADR-3 | **文件级链接**,不做目录折叠 | stow 式目录级链接 | 防止程序写入的缓存/lock 进入仓库;代价是链接数量多,可接受 |
| ADR-4 | CLI 与配置**同仓库** | 分仓 | 单人场景版本天然对齐,bootstrap 只克隆一个东西;解耦靠 Releases + `requires` |
| ADR-5 | 收集目录命名 **`modules/`** | `packages/`、`config/` | 与 Go package 概念区隔;"module" 成为全局统一术语 |
| ADR-6 | manifest 用 **TOML** | YAML、JSON | 注释友好、无缩进陷阱、Go 生态解析成熟(BurntSushi/toml) |
| ADR-7 | 合并语义**整键覆盖**,不深合并 | 深合并、数组拼接 | 单人场景可预测性 > 灵活性;半年后仍能一眼看懂生效配置 |
| ADR-8 | 双模板后缀:`.tmpl`(managed)/ `.template`(scaffold) | 单后缀 + manifest 标记 | 文件名自说明;助记:短后缀 = 高频渲染,长后缀 = 一次性蓝本。manifest 仍可覆盖 |
| ADR-9 | 模块发现**靠目录存在**,profiles 只分组不枚举 | manifest 显式注册 | 避免「加模块改两处」的双重记账 |
| ADR-10 | state 用**单个 JSON 文件** | bolt/sqlite(chezmoi 方案) | 规模上限是个人配置,几百条;JSON 可 diff、可手工急救 |
| ADR-11 | `requires` 仅支持 `>=x.y.z` | 完整 semver 约束语法 | 解析器 20 行写完;个人项目不需要范围锁定 |
| ADR-12 | scaffold 产物**永不自动删除** | 随模块删除清理 | scaffold 产物含用户手写内容,是用户数据不是工具产物 |

## 6. 与现有工具的能力对照

| 能力 | stow | chezmoi | dot |
|---|---|---|---|
| symlink 双向直觉 | ✅ | ❌ | ✅(默认) |
| 模板/机器差异 | ❌ | ✅ | ✅(按需) |
| 文件级链接 | ⚠️ 需 --no-folding | n/a | ✅ 唯一模式 |
| 一次性脚手架 | ❌ | ⚠️ create_ 前缀 | ✅ scaffold 一等公民 |
| 孤儿清理 | ⚠️ -D 手动 | ✅ | ✅ state 驱动 |
| CLI/配置版本铰链 | n/a | n/a | ✅ requires |
