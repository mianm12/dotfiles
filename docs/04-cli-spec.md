# 04 · CLI 规范:命令、Flag 与输出

## 1. 总览

```
dot <command> [flags] [args]

核心      init | apply | diff | status | add | doctor
同步      update | self-update | git
辅助      version | state rebuild [M2] | edit [M2]
```

## 2. 全局 flag

| flag | 说明 |
|---|---|
| `--repo <dir>` | 覆盖仓库位置(> `DOT_REPO` > 机器配置 > 默认) |
| `--home <dir>` | **隐藏**。整体重定向 `~`、全部状态路径**及 hook 子进程环境**,测试专用 |
| `--profile <name>` | 本次运行覆盖机器配置中的 profile |
| `-v / --verbose` | 详细输出(含 skip 项与内容 diff) |
| `--no-color` | 关闭彩色输出(pipe 时自动关闭) |

错误 → 退出码的映射集中于 `cli.Execute` 一处;深层包只返回语义化错误。
锁边界:mutation 命令持锁;`diff` / `status` / `doctor` 只读不取锁(02 号文档 §2)。

## 3. 退出码(全命令统一)

| 码 | 含义 |
|---|---|
| 0 | 成功;对 `diff` / `status` 表示「无差异 / 无异常」 |
| 1 | 运行错误(IO 失败、manifest 非法、requires 不满足、不变量校验失败、锁被占用、state fail-closed…) |
| 2 | `diff` / `status` 发现差异或 drift(供脚本判断) |
| 3 | 存在 conflict,需要用户决策 |

同一次运行满足多个条件时按 **1 > 3 > 2 > 0** 取最高优先级。`apply` 中「用户拒绝整模块
孤儿确认」或「存在 deferred prune 而无 conflict」以退出码 2 结束——工作未完成,与
`diff` 的「有差异」语义一致。

## 4. 命令规范

### 4.1 `dot init`

新机器初始化,幂等(重复运行进入「更新变量」模式)。流程:

1. 定位仓库(不存在则报错,提示走 bootstrap)。
2. requires 检查(宽松预读)。
3. 交互选择 profile(列出顶层 `[profiles]` 键)。
4. 按顶层 `[data]` 声明逐项询问变量(有 default 或 `from_env` [M2] 命中时回车即接受)。
5. 写入 `~/.config/dot/config.toml`(权限 0600)。
6. 询问「立即 apply?」,默认 yes。

Flag:`--profile <name>`、`--set key=value`(可重复)、`--yes` 支持无人值守:
`dot init --profile mac --set email=me@x.com --yes`。

### 4.2 `dot apply [module...]`

核心命令,pipeline 见 02 号文档 §4。**prune 作用域随调用形态变化**:

- **无参数(全量)**:应用当前 profile 全部模块;prune 候选 = 全部 state 条目中
  不在 desired 的。
- **指定模块(部分)**:仅应用给定模块;prune 候选 = 仅 `entry.module ∈ 请求集` 的
  孤儿条目。
- 请求的模块 **∉ 当前 profile → 报错**(ADR-18),提示将其加入 profile 后重试。

| flag | 行为 |
|---|---|
| `-n / --dry-run` | 只打印计划,不落盘(含 state),退出码规则同 `diff` |
| `--force` | conflict 项由 planner 直接产出 `BackupReplace`(备份后覆盖/重渲染/重建) |
| `--adopt` [M2] | 允许收养「内容与渲染结果一致但无 state 记录」的**普通文件**(05 号文档 M3c,managed 专属);symlink 收养始终自动,不需此 flag。M1 构建给出硬错误 |
| `--prune` / `--no-prune` | 是否计划 prune 阶段,默认 `--prune` |
| `-y / --yes` | 跳过交互确认(目前唯一确认点:整模块级孤儿清理) |

行为要点:

- 执行顺序 `mkdir → 创建/收养 → prune → hooks`(ADR-13);**prune 仅在创建阶段完全
  收敛后执行**(ADR-20):plan 中存在 conflict、执行中出现 error 或 Precond 失配、
  或用户拒绝整模块确认,任一发生 → 本次**全部** prune 转为 deferred(输出中标注,
  不执行),提示消解后重跑。hooks 不受收敛门控(05 号文档 §8)。
- 存在 conflict 且无 `--force` 时,其余创建动作照常执行,conflict 项汇总列出,
  退出码 3(部分成功优于全盘卡死,幂等保证重跑无害)。
- prune 集合中出现**整模块级孤儿**(典型于 profile 切换)→ 打印汇总并要求确认(y/N),
  `--yes` 跳过;拒绝 = 本次全部 prune 延迟,并以退出码 2 结束(工作未完成)。
- 计划为空(仅 skip)时输出 `Already up to date.`。

### 4.3 `dot diff`

执行 pipeline ②–⑨ 后打印计划,**无锁**、永不写盘(Adopt 与 deferred Prune 如实展示,
可收养的普通文件标注 `adoptable, rerun apply --adopt`)。`-v` 时:「将重渲染」与
「drift」条目均展示 **实际文件 vs 本次渲染结果** 的 unified diff(06 号文档 §5)。

### 4.4 `dot status`

面向「巡检」而非「预览」,无锁只读,分节输出:

```
Profile: mac (12 modules, 87 files managed)

DRIFT (2)
  ~/.config/git/config          rendered file modified by hand
  ~/.config/zsh/aliases.zsh     symlink re-pointed elsewhere

PENDING (1)
  macos/hooks/setup.sh          run_once not yet executed

UNASSIGNED MODULES (1)
  experimental-nvim             not referenced by any profile
```

无异常输出 `Clean.`,退出码 0;有 DRIFT/PENDING 退出码 2。state 损坏或版本过新时,
status 不崩溃而是报告该事实(fail-closed 诊断入口,05 号文档 §2)。

### 4.5 `dot add [-m <module>] [--template|--scaffold] <path>...`

把 `$HOME` 中已有文件收编入库。反向映射与模块推断算法见 05 号文档 §9。要点:

- **只接受普通文件**:目录、symlink、特殊文件一律拒绝(目录请逐文件 add 或手工搬移)。
- **仓库侧预检**(任何 mutation 之前):仓库目标已存在(含 `.tmpl`/`.template` 后缀
  变体)一律拒绝、绝不覆盖;多路径输入先全部预检(复用 target 唯一性/后缀碰撞校验),
  任一失败则全部不执行;仓库副本 `O_EXCL` 排他创建;rename 换链前按快照复核原文件
  未被修改(05 号文档 §9)。
- **硬拒绝 `*.local` 路径**(06 号文档 §2)。
- 推断唯一命中 → 直接归入;多命中或零命中 → 退出码 3,提示加 `-m`。`-m` 必须满足
  模块名合法性(03 号文档 §6);指向不存在的模块时创建目录(提示)。
- **目标模块 ∉ 当前 profile → 报错**,并打印需手动添加进 `[profiles]` 的确切行
  (ADR-18/28,CLI 不代改 manifest);编辑后重跑即可。
- **默认(link)**:copy 入仓库(保留 mode,含可执行位)→ 校验副本 hash → 目标同目录
  建临时 symlink → **原子 rename 覆盖原文件**——rename 前原文件未动,换链失败时必然
  完好;登记 `kind=symlink` + `link_dest`。
- **`--template`** [M2]:仓库侧存为 `.tmpl`;**原文件留在原位作为产物**,登记
  `kind=rendered`(hash = 当前内容)。随后用户把机器相关值替换为 `{{ .var }}`,渲染
  一致则下次 apply 自然 skip,闭环。M1 构建给出硬错误。
- **`--scaffold`**:仓库侧存为 `.template`;原文件保留为产物,登记 `kind=scaffold`。
- `--dry-run` 支持。

### 4.6 `dot doctor`

环境与配置自检,**严格只读、无锁**。检查项:二进制是否在 PATH、仓库存在且为 git 仓库、
manifest 静态校验(03 号文档 §7,宽松模式报告未知键)、**路径合法性**(03 号文档 §6)、
target 全局唯一性、模板静态扫描(未声明变量)、**state 三态诊断**(缺失/正常/损坏/
版本过新,后两者给出手动恢复指引)、state 记录的链接死链或被改指、机器配置与 state
目录权限(0600/0700)、`git ls-files '*.local'` 非空即警告、当前 OS 是否受支持。
`--manifest-only` 供 CI;**M1 提供该最小子集**(manifest 静态校验 + 路径合法性 +
target 唯一性,CI 自 lint 依赖它),完整检查集随 M2。

### 4.7 `dot update`

配置侧更新,**自 `git pull` 起持锁**(pull 改仓库、apply 读仓库,读写同锁):
`git pull --ff-only`(仓库脏时报错,提示先 `dot git status`)→ requires 检查(不满足 →
报错提示 `dot self-update`,不执行 apply)→ `apply`。`--no-apply` 只拉取。

拉取到的新 hook **不做单独确认**(威胁模型出界,01 号文档 §4):计划输出中 `run-hook`
动词可见;想先审查就 `dot update --no-apply` 后 `dot diff`,再手动 `dot apply`。

### 4.8 `dot self-update`

二进制侧更新:查询 GitHub Releases latest → 版本比对 → 下载对应 GOOS/GOARCH 资产至
临时文件 → 校验 checksums.txt → 同目录 rename 原子替换自身。`--tag v0.4.0` 指定版本。

### 4.9 `dot git [args...]`

透传:等价于 `git -C <repo-dir> <args...>` 直接 exec(继承 stdio,退出码透传)。
唯一增强:仓库目录不存在时给出走 bootstrap 的提示。

### 4.10 `dot version`

输出 CLI 版本、commit、构建时间,以及当前仓库顶层 `requires` 与满足情况
(`dev` 构建注明 requires 检查处于放行状态)。

### 4.11 `dot state rebuild` [M2]

从文件系统反推重建 state:对当前 profile 的 desired 逐项观测,一致者收养(含普通文件,
等价于带 `--adopt` 的全量收养)。M1 的手动恢复路径:`mv state.json state.json.bak`
把「损坏」显式转化为「缺失」后重新 apply。

### 4.12 `dot edit <target-path>` [M2]

打开 `$EDITOR` 编辑给定落地路径对应的**源文件**(symlink 直接就是源;rendered 反查
state 找到 `.tmpl` 源),模板保存后自动 re-render 该文件。

## 5. 输出与日志约定

- 计划行格式:`<verb>  <target>  (<reason>)`,verb ∈ `link | render | scaffold | adopt |
  backup+replace | prune | prune (deferred) | run-hook | skip | CONFLICT`。
  verb 左对齐彩色,`skip` 仅 `-v` 显示。
- 人类输出走 stdout;错误与警告走 stderr;为脚本消费预留 `--json` [M3]。
- 所有会写文件系统的命令开头打印一行上下文:`repo=… profile=… os=darwin`。
- 输出顺序确定性:模块、文件、prune 条目均排序后输出(golden 测试依赖此性质)。
