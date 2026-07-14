# 06 · 模板系统:managed 与 scaffold 双语义

## 1. 为什么是两类模板

同样是「渲染变量生成文件」,两种意图截然相反,必须在类型层面区分而不是靠使用纪律:

| | **managed**(`.tmpl`) | **scaffold**(`.template`) |
|---|---|---|
| 意图 | 内容由变量完全决定 | 给用户一个起手蓝本 |
| 渲染时机 | 每次 apply(源或变量变则重渲染) | 仅 target 不存在时一次 |
| 产物归属 | 工具。手改 = drift,`status` 警告 | 用户。工具此后永不触碰 |
| 删除产物后 | 下次 apply 重建 | 视为用户有意为之,不重建(`--force` 除外) |
| 模块删除时 prune | 会删(hash 匹配前提下) | 永不删(ADR-12) |
| 典型例子 | `.gitconfig`(email 因机器而异) | `.zshrc.local`(本机私有内容) |
| 助记 | 短后缀 = 高频渲染 | 长后缀 = 一次性蓝本 |

## 2. `*.local` 约定(与 scaffold 的配合)

私有/本机内容的完整闭环由三方约定组成:

1. **共享配置负责挂载点**:入库的 `.zshrc` 固定包含
   `[ -f "$ZDOTDIR/.zshrc.local" ] && source "$ZDOTDIR/.zshrc.local"`(git 则是
   `[include] path = config.local`)。
2. **scaffold 负责首次体验**:`modules/zsh/.config/zsh/.zshrc.local.template` 提供带
   注释的空壳,新机器 apply 后立即有地方写私货。
3. **ignore 负责兜底**:仓库 `.gitignore` 全局忽略 `*.local`,即使误 `dot add` 也进
   不了 git 历史。

这套约定应写进仓库 README 的显要位置——它是「密钥永不入库」目标的实现主体。

## 3. 模板语法与变量

引擎为 Go `text/template`,`missingkey=error`(引用未定义变量直接报错,好过渲染出
空字符串静默污染配置)。

变量来源与优先级(后者覆盖前者):

1. 内建变量(CLI 注入):

| 变量 | 值 |
|---|---|
| `.OS` | `darwin` / `linux`(GOOS) |
| `.Arch` | `arm64` / `amd64` |
| `.Hostname` | `os.Hostname()` 短名 |
| `.Profile` | 当前 profile 名 |
| `.Home` | 展开后的 home 目录(尊重 `--home`) |

2. 机器配置 `[data]` 表:以顶层键直接可用,`{{ .email }}`。

`dot init` 依据顶层 manifest 的 `[data]` 声明收集变量(03 号文档 §2);模板中引用了
未声明也未提供的变量时,apply 报错并指出变量名与文件——`doctor` 会静态扫描全部模板,
提前汇报「引用了未声明变量」的问题。

## 4. 函数表

M1 刻意只开放极小集合,防止配置逻辑化:

| 函数 | 用途 |
|---|---|
| `default <fallback> <value>` | 变量为空时取兜底值 |
| `env "VAR"` | 读环境变量(渲染时的环境) |
| `eq / ne / and / or / not` | text/template 内建比较 |

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

不提供 `include` 子模板、循环生成文件等能力;需要那种复杂度时,正确做法是拆成多个
文件或写 hook 脚本,而不是把 manifest 变成编程语言。

## 5. drift 检测细节

managed 产物的三方对比(state 记录 hash / 实际文件 hash / 本次渲染 hash)决策表见
05 号文档 §3。补充两点:

- 换行与编码不做任何规范化,渲染结果逐字节即最终产物——drift 判定因此简单可靠。
- `dot diff -v` 对「将重渲染」的条目展示 unified diff(旧产物 vs 新渲染),对
  「drift」条目展示(记录产物 vs 实际文件),让用户看清自己改了什么再决定
  `--force` 还是把改动回填模板。

## 6. 渲染安全

- 渲染在内存完成,产物经 05 号文档 §4 的原子写落盘;权限按 `[files].mode`,缺省 `0644`
  (含 `secret`、`token` 等敏感字样的目标 [M3] 可考虑默认 `0600` + 警告)。
- 模板渲染失败(语法错、缺变量)时,该文件动作降级为 error,不影响同批其他文件;
  apply 整体退出码 1。
