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

## 2. `*.local` 约定(纵深防御,ADR-32)

私有/本机内容的闭环由**四道机制**组成——前两道解决体验,后两道压低误入库风险。
定位是**纵深而非保证**:`git add -f` 属于用户对自己防护的直接绕过,列为已接受风险
(01 号文档 §4),工具不对抗自己的主人。

1. **共享配置负责挂载点**:入库的 `.zshrc` 固定包含
   `[ -f "$ZDOTDIR/.zshrc.local" ] && source "$ZDOTDIR/.zshrc.local"`(git 则是
   `[include] path = config.local`)。
2. **scaffold 负责首次体验**:`modules/zsh/.config/zsh/.zshrc.local.template` 提供带
   注释的空壳,新机器 apply 后立即有地方写私货。
3. **CLI 负责拒绝**:`dot add` 对 `*.local` 路径硬拒绝(04 号文档 §4.5)——`.gitignore`
   挡不住已跟踪文件,工具入口必须自己把关。
4. **doctor 负责巡检与门禁**:`git ls-files '*.local'` 非空 → 交互模式警告,
   **`--manifest-only`/CI 模式判为错误**(机械门禁,抓历史上已被跟踪的漏网之鱼);
   同时检查机器配置(0600)、state 与 backup 目录(0700)的权限。

仓库 `.gitignore` 忽略 `*.local` 仍然保留,作为第五道纯预防性措施。这套约定应写进
仓库 README 的显要位置。

## 3. 模板语法、变量与命名空间

引擎为 Go `text/template`,`missingkey=error`(引用未定义变量直接报错,好过渲染出空
字符串静默污染配置)。**渲染只依赖显式输入**(ADR-17):内建变量 + 机器配置 `[data]`,
不读进程环境、不读磁盘上的其他文件——这是 plan 可复现、drift 检测可信的前提。

**命名空间强制隔离**:内建变量一律大写字母开头,用户 `[data]` 键必须小写字母开头
(加载时校验,03 号文档 §2),碰撞从此不可能。

内建变量:

| 变量 | 值 |
|---|---|
| `.OS` | `darwin` / `linux`(GOOS) |
| `.Arch` | `arm64` / `amd64` |
| `.Hostname` | `os.Hostname()` 短名 |
| `.Profile` | 当前 profile 名 |
| `.Home` | 展开后的 home 目录(尊重 `--home`) |

环境变量的正确用法:顶层 `[data.foo] from_env = "FOO"` [M2] —— `dot init` 时读取环境
作为默认值快照进机器配置,渲染期只见机器配置里的稳定值。

## 4. 函数表

M1 刻意只开放极小集合,防止配置逻辑化:

| 函数 | 用途 |
|---|---|
| `default <fallback> <value>` | 变量为空时取兜底值 |
| `eq / ne / and / or / not` | text/template 内建比较 |

**不提供 `env` 函数**(ADR-17)。`range`、`with`、`define/template` 是 text/template
的原生能力,无法也不值得用 parse-tree 审查去禁止——立场是**不推荐、不为其提供任何
辅助函数**;需要那种复杂度时,正确做法是拆文件或写 hook 脚本。

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
M3–M6 规则;权限漂移由 `Observed.Mode` 对比期望 mode 检出,经复用 render 修正
(M3b,ADR-26)。换行与编码不做任何规范化,渲染结果逐字节即最终产物,判定因此
简单可靠。

展示(`dot diff -v`):「将重渲染」与「drift」条目统一展示 **实际文件 vs 本次渲染结果**
的 unified diff——它直接回答用户决策所需的问题:「apply(--force)会把磁盘改成什么样」。
*设计注记*:state 只存 hash、不存内容,无法还原「记录产物」,因此不承诺三方对比;
若未来确有需要,方案是内容寻址的产物快照缓存,列 M3 可选项。

## 6. 渲染错误策略:fail-fast(ADR-24)

全部模板在 plan 阶段(pipeline ⑥)完成 parse 与渲染;**任一失败(语法错、缺变量、
引用未声明键)→ 整次运行报错退出,execute 不启动**。理由:渲染失败即配置 bug,
半套配置上线没有意义;fail-fast 同时让「executor 只消费 Action」的契约保持干净
(不需要 Error 动作,也不需要"单文件降级、其余继续"的分支逻辑)。`doctor` 静态
扫描全部模板,可在 apply 之前提前暴露问题。

其余渲染安全事项:产物随 `Action.Content` 经 05 号文档 §6 的原子写落盘;权限按
`[files].mode`,缺省 `0644`——**不按文件名猜测敏感性**(「名字含 secret 则 0600」
之类的启发式已明确否决),需要收紧权限时在 `[files]` 显式声明。
