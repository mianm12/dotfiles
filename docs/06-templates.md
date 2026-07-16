# 06 · 模板系统:managed 与 scaffold 双语义

## 1. 为什么是两类模板

同样是「渲染变量生成文件」,两种意图截然相反,必须在类型层面区分而不是靠使用纪律:

| | **managed**(`.tmpl`) | **scaffold**(`.template`) |
|---|---|---|
| 意图 | 内容由变量完全决定 | 给用户一个起手蓝本 |
| 渲染时机 | 每次 apply(源或变量变则重渲染) | 仅 target 不存在时一次 |
| 产物归属 | 工具。手改 = drift,`status` 警告 | 用户。工具此后永不触碰 |
| 删除产物后 | 下次 apply 重建 | 视为用户有意为之,不重建(`--force` 除外) |
| owned() 判定 | hash 匹配时为真 | 恒为假(ADR-12/14) |
| 模块删除时 prune | 会删(owned ∧ 收敛前提下) | 永不删 |
| 典型例子 | `.gitconfig`(email 因机器而异) | `.zshrc.local`(本机私有内容) |
| 助记 | 短后缀 = 高频渲染 | 长后缀 = 一次性蓝本 |

scaffold 的 state 记录只表示“一次性生成机会已经满足”,不表示文件归工具所有。target
已存在而记录缺失时可以自动补录且绝不触碰 target,从而让创建成功但落账失败的场景重跑收敛。

## 2. `*.local` 约定(纵深防御,ADR-32)

私有/本机内容的闭环由一组互补机制组成——前两项解决体验,其余机制压低误入库风险。
定位是**纵深而非保证**:`git add -f` 属于用户对自己防护的直接绕过,列为已接受风险
(01 号文档 §4),工具不对抗自己的主人。

1. **共享配置负责挂载点(仓库约定示例,非 CLI 语义)**:本项目可让入库的 `.zshrc` 包含
   `[ -f "$ZDOTDIR/.zshrc.local" ] && source "$ZDOTDIR/.zshrc.local"`(git 则是
   `[include] path = config.local`)。工具不解析或强制注入这些配置内容。
2. **scaffold 负责首次体验**:`modules/zsh/.config/zsh/.zshrc.local.template` 提供带
   注释的空壳,新机器 apply 后立即有地方写私货。
3. **CLI 负责拒绝**:`dot add` 对 `*.local` 路径硬拒绝(04 号文档 §4.5)——`.gitignore`
   挡不住已跟踪文件,工具入口必须自己把关。通用的 Git 可跟踪性预检仍独立执行;
   `*.local` 不因未被 Git ignore 而获得例外。
4. **doctor 负责巡检与门禁**:发现 git 已跟踪的 `*.local` → 交互模式警告,
   **`--manifest-only`/CI 模式判为错误**(机械门禁,抓历史上已被跟踪的漏网之鱼)。完整
   doctor 另检查机器配置(0600)、state 与 backup 目录(0700)的权限;manifest-only 不读取
   这些本机文件。

仓库 `.gitignore` 忽略 `*.local` 仍然保留,作为额外的纯预防性措施。这套约定应写进
仓库 README 的显要位置。

## 3. 模板语法、变量与命名空间

引擎为 Go `text/template`,`missingkey=error`(引用未定义变量直接报错,好过渲染出空
字符串静默污染配置)。**渲染只依赖显式输入**(ADR-17):内建变量 + 当前 manifest 已声明且
机器配置 `[data]` 已提供的变量,
不读进程环境、不读磁盘上的其他文件——这是 plan 可复现、drift 检测可信的前提。
manifest 的 `default`/`from_env` 只用于 init 收集值,渲染期不回退读取它们;声明键在当前
机器配置中缺失时,需要渲染的命令必须在生成 plan 前失败并提示重新运行 init,因此也必然
早于 execute。

**命名空间强制隔离**:内建变量一律大写字母开头,用户 `[data]` 键必须小写字母开头
(加载时校验,03 号文档 §2),碰撞从此不可能。

内建变量:

| 变量 | 值 |
|---|---|
| `.OS` | `darwin` / `linux`(GOOS) |
| `.Arch` | `arm64` / `amd64` |
| `.Hostname` | 操作系统报告的 hostname,不承诺自动截成短名 |
| `.Profile` | 当前 profile 名 |
| `.Home` | 展开后的 home 目录(尊重 `--home`) |

环境变量的正确用法:顶层 `[data.foo] from_env = "FOO"` [M2] —— `dot init` 时读取环境
作为默认值快照进机器配置,渲染期只见机器配置里的稳定值。

## 4. 函数表

M1 刻意只开放极小集合,防止配置逻辑化:

| 函数 | 用途 |
|---|---|
| `default <fallback> <value>` | 字符串为空时取兜底值 |
| `eq / ne / and / or / not` | text/template 内建比较 |

这是函数白名单而非示例:`text/template` 预声明的其他函数(如 `printf`、`len`、`index`、
`call`)不属于当前配置语言,使用时必须在 parse/静态校验阶段报错。如何在引擎上落实白名单
由代码决定。**不提供 `env` 函数**(ADR-17)。`if`、`range`、`with`、`define/template` 等
不调用额外函数的 text/template 原生 action 仍可使用;配置复杂到需要更多逻辑时应拆文件
或写 hook。

示例(managed,`modules/git/.config/git/config.tmpl`):

```gotemplate
[user]
    name = Me
    email = {{ .email }}
{{ if eq .OS "darwin" }}
[credential]
    helper = osxkeychain
{{ end }}
[include]
    path = config.local
```

## 5. drift 的检测与展示

检测:三元 hash 对比(state 记录 / 实际文件 / 本次渲染)驱动 05 号文档 §3.2 的
M3–M6 规则;实际权限偏离声明 mode 时也必须收敛(M3b,ADR-26)。换行与编码不做任何
规范化,渲染结果逐字节即最终产物,判定因此
简单可靠。

展示(`dot diff -v`):「将重渲染」与「drift」条目统一展示 **实际文件 vs 本次渲染结果**
的 unified diff——它直接回答用户决策所需的问题:「apply(--force)会把磁盘改成什么样」。
*设计注记*:state 只存 hash、不存内容,无法还原「记录产物」,因此不承诺三方对比;
若未来需要三方对比,必须另行定义持久化与清理契约。

## 6. 渲染错误策略:fail-fast(ADR-24)

本次动作作用域内的全部模板在 plan 阶段(pipeline ⑥)完成 parse 与渲染;**任一失败(语法错、缺变量、
引用未声明键)→ 整次运行报错退出,execute 不启动**。理由:渲染失败即配置 bug,
半套配置上线没有意义;fail-fast 同时让执行计划保持自包含,不引入“单文件降级、其余
继续”的额外行为。`doctor` 静态
扫描全部模板,可在 apply 之前提前暴露问题。

其余渲染安全事项:执行计划携带已完成的渲染产物,并按 05 号文档 §6 的原子性契约落盘;
权限按 `[files].mode`,语法与比较口径见 03 号文档 §3,缺省 `0644`——**不按文件名猜测
敏感性**(「名字含 secret 则 0600」之类的启发式已明确否决),需要收紧权限时在
`[files]` 显式声明。`add --template`
与 `add --scaffold` 只有在首次渲染字节与有效 mode 都等于原文件时才能建账(05 §9)。
