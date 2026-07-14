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
| 模块删除时 prune | 会删(owned 前提下) | 永不删 |
| 典型例子 | `.gitconfig`(email 因机器而异) | `.zshrc.local`(本机私有内容) |
| 助记 | 短后缀 = 高频渲染 | 长后缀 = 一次性蓝本 |

## 2. `*.local` 约定(与 scaffold 的配合)

私有/本机内容的闭环由**四道**机制组成——前两道解决体验,后两道保证「永不入库」不是
一句口号:

1. **共享配置负责挂载点**:入库的 `.zshrc` 固定包含
   `[ -f "$ZDOTDIR/.zshrc.local" ] && source "$ZDOTDIR/.zshrc.local"`(git 则是
   `[include] path = config.local`)。
2. **scaffold 负责首次体验**:`modules/zsh/.config/zsh/.zshrc.local.template` 提供带
   注释的空壳,新机器 apply 后立即有地方写私货。
3. **CLI 负责拒绝**:`dot add` 对 `*.local` 路径硬拒绝(04 号文档 §4.5)——`.gitignore`
   挡不住 `git add -f`,也挡不住已跟踪文件,工具入口必须自己把关。
4. **doctor 负责巡检**:`git ls-files '*.local'` 非空即警告(抓历史上已被跟踪的漏网
   之鱼);同时检查机器配置(0600)、state 与 backup 目录(0700)的权限。

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
作为默认值快照进机器配置,渲染期只见机器配置里的稳定值。模板中引用了未声明也未提供的
变量时,apply 在渲染阶段(pipeline ⑥)报错并指出变量名与文件;`doctor` 静态扫描全部
模板提前汇报。

## 4. 函数表

M1 刻意只开放极小集合,防止配置逻辑化:

| 函数 | 用途 |
|---|---|
| `default <fallback> <value>` | 变量为空时取兜底值 |
| `eq / ne / and / or / not` | text/template 内建比较 |

**不提供 `env` 函数**(ADR-17,理由见 §3)。`range`、`with`、`define/template` 是
text/template 的原生能力,无法也不值得用 parse-tree 审查去禁止——立场是**不推荐、
不为其提供任何辅助函数**;需要那种复杂度时,正确做法是拆文件或写 hook 脚本,而不是
把配置变成程序。

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
M3–M6 规则。换行与编码不做任何规范化,渲染结果逐字节即最终产物,判定因此简单可靠。

展示(`dot diff -v`):「将重渲染」与「drift」条目统一展示 **实际文件 vs 本次渲染结果**
的 unified diff——它直接回答用户决策所需的问题:「apply(--force)会把磁盘改成什么样」。
*设计注记*:state 只存 hash、不存内容,无法还原「记录产物」,因此不承诺展示
「记录产物 vs 实际文件」的三方对比;若未来确有需要,方案是内容寻址的产物快照缓存,
列 M3 可选项——其边际价值目前配不上复杂度。

## 6. 渲染安全

- 渲染在 plan 阶段于内存完成(全部模板先 parse,语法错误 fail fast),产物随
  `Action.Content` 交给 executor 原子落盘;权限按 `[files].mode`,缺省 `0644`。
  不按文件名猜测敏感性(「名字含 secret 则 0600」之类的启发式已明确否决——按名猜
  密级不可靠,需要收紧权限时在 `[files]` 显式声明 mode)。
- 单个模板渲染失败(缺变量等)时,该文件动作降级为 error,不影响同批其他文件;
  依 05 号文档 §6,error 的存在会使本次运行跳过 prune,apply 整体退出码 1。
