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

## 3. 退出码(全命令统一)

| 码 | 含义 |
|---|---|
| 0 | 成功;对 `diff` / `status` 表示「无差异 / 无异常」 |
| 1 | 运行错误(IO 失败、manifest 非法、requires 不满足、不变量校验失败、锁被占用…) |
| 2 | `diff` / `status` 发现差异或 drift(供脚本判断,类比 `git diff --exit-code`) |
| 3 | 存在 conflict,需要用户决策 |

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

- **无参数(全量)**:应用当前 profile 全部模块;prune 候选 = **全部** state 条目中
  不在 desired 的(profile 是本机应有状态的完整声明,退出 profile 的模块被清理)。
- **指定模块(部分)**:仅应用给定模块;prune 候选 = **仅 `entry.module ∈ 请求集`** 的
  孤儿条目,其他模块的 state 在本次运行中不可见、不可删。
- 请求的模块 **∉ 当前 profile → 报错**(ADR-18),提示将其加入 profile 后重试。

| flag | 行为 |
|---|---|
| `-n / --dry-run` | 只打印计划,不落盘(含 state),退出码规则同 `diff` |
| `--force` | conflict 项:备份原文件后覆盖/重渲染/重建(备份至 state 目录) |
| `--prune` / `--no-prune` | 是否执行 prune 阶段,默认 `--prune` |
| `-y / --yes` | 跳过交互确认(目前唯一确认点:整模块级孤儿清理,见下) |

行为要点:

- 执行顺序 `mkdir → 创建/收养 → prune → hooks`(ADR-13);**本次运行发生过 error
  (IO/渲染失败)则跳过 prune** 并提示修复后重跑;conflict 不触发跳过(它是预期中的
  用户决策态)。
- 存在 conflict 且无 `--force` 时,其余动作照常执行,conflict 项汇总列出,退出码 3
  (部分成功优于全盘卡死,幂等保证重跑无害)。
- prune 集合中出现**整模块级孤儿**(某模块全部条目待删,典型于 profile 切换)→ 打印
  汇总并要求确认(y/N),`--yes` 跳过。防 `--profile` 打错字引发批量删链。
- 计划为空(仅 skip)时输出 `Already up to date.`。

### 4.3 `dot diff`

执行 pipeline ①–⑨ 后打印计划,永不写盘(含 state;Adopt 动作仅展示)。`-v` 时:
「将重渲染」与「drift」条目均展示 **实际文件 vs 本次渲染结果** 的 unified diff
——即回答「apply(--force)会把磁盘改成什么样」(06 号文档 §5)。

### 4.4 `dot status`

面向「巡检」而非「预览」,分节输出:

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

无异常输出 `Clean.`,退出码 0;有 DRIFT/PENDING 退出码 2。

### 4.5 `dot add [-m <module>] [--activate] [--template|--scaffold] <path>...`

把 `$HOME` 中已有文件收编入库并原位替换为 symlink。反向映射与模块推断算法见
05 号文档 §9。要点:

- **硬拒绝 `*.local` 路径**:该约定的意义就是不入库(06 号文档 §2),报错并说明。
- 推断唯一命中 → 直接归入该模块;多命中或零命中 → 退出码 3,提示加 `-m`。
- `-m` 指向不存在的模块时创建模块目录(打印提示)。
- **目标模块 ∉ 当前 profile → 报错**(否则条目将被下次全量 apply 当孤儿清理,ADR-18);
  `--activate` 则自动将模块名追加进当前 profile 的列表(顶层 dot.toml,CLI 唯一的
  manifest 写入点)并提示 commit。
- `--template`:入库为 `.tmpl`(managed),入库后打印「请手动将机器相关值替换为
  {{ .var }}」提醒;`--scaffold` 入库为 `.template`,保留原文件为产物并登记 state,不建链。
- `--dry-run` 支持;移动文件 + 建链两步整体失败回滚(先 copy 校验后删原,规避 rename
  跨设备陷阱)。

### 4.6 `dot doctor`

环境与配置自检,**严格只读**。检查项:二进制是否在 PATH、仓库存在且为 git 仓库、
manifest 静态校验(03 号文档 §6,宽松模式报告未知键)、target 全局唯一性、模板静态扫描
(未声明变量)、state.json 可解析、state 记录的链接是否死链/被改指、机器配置与 state
目录权限(0600/0700)、**`git ls-files '*.local'` 非空即警告**(私有文件已被 git 跟踪)、
当前 OS 是否受支持。`--manifest-only` 供 CI。

### 4.7 `dot update`

配置侧更新:`git pull --ff-only`(仓库脏时报错,提示先 `dot git status`)→ requires
检查(不满足 → 报错提示 `dot self-update`,**不执行 apply**)→ `apply`。
`--no-apply` 只拉取。

### 4.8 `dot self-update`

二进制侧更新:查询 GitHub Releases latest → 版本比对 → 下载对应 GOOS/GOARCH 资产至
临时文件 → 校验 checksums.txt → 原子替换自身。`--tag v0.4.0` 指定版本。详见 07 号文档。

### 4.9 `dot git [args...]`

透传:等价于 `git -C <repo-dir> <args...>` 直接 exec(继承 stdio,退出码透传)。
不包装任何语义。唯一增强:仓库目录不存在时给出走 bootstrap 的提示。

### 4.10 `dot version`

输出 CLI 版本、commit、构建时间,以及当前仓库顶层 `requires` 与满足情况
(本地开发构建显示 `dev` 并注明 requires 检查处于放行状态)。

### 4.11 `dot state rebuild` [M2]

从文件系统反推重建 state:对当前 profile 的 desired 逐项观测,一致者收养。由于 apply
自带收养规则(05 号文档 §3),本命令仅用于 state 全毁且想一次性重建的场景,优先级低。

### 4.12 `dot edit <target-path>` [M2]

打开 `$EDITOR` 编辑给定落地路径对应的**源文件**(symlink 直接就是源;rendered 反查
state 找到 `.tmpl` 源),模板保存后自动 re-render 该文件。

## 5. 输出与日志约定

- 计划行格式:`<verb>  <target>  (<reason>)`,verb ∈ `link | render | scaffold | adopt |
  prune | backup+replace | run-hook | skip | CONFLICT`。verb 左对齐彩色,`skip` 仅 `-v` 显示。
- 人类输出走 stdout;错误与警告走 stderr;为脚本消费预留 `--json` [M3]。
- 所有会写文件系统的命令开头打印一行上下文:`repo=… profile=… os=darwin`,事后排查全靠它。
- 输出顺序确定性:模块、文件、prune 条目均排序后输出(golden 测试依赖此性质)。
