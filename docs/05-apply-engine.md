# 05 · Apply 引擎:所有权、决策表与执行

## 1. 期望状态模型

对 profile 内每个模块,enumerate 产出文件条目,定级优先级见 03 号文档 §3
(内置忽略 > `[files]` > ignore patterns > 后缀推断):

| 级别 | 判定 | apply 动作 |
|---|---|---|
| **link**(默认) | — | target 处创建指向仓库源文件的绝对路径 symlink |
| **managed** | `.tmpl` 后缀或 `kind="managed"` | 渲染模板,产物写入 target |
| **scaffold** | `.template` 后缀或 `kind="scaffold"` | target 不存在时渲染写入一次 |

模块顶层 `hooks/` 为保留目录,连同 `[hooks]` 引用的脚本路径一起被排除在枚举之外。
目录不是动作对象:target 侧缺失的中间目录由 executor `MkdirAll` 创建为**真实目录**
(ADR-3),不记 state、不参与 prune。

managed 与 scaffold 的**渲染发生在 plan 阶段**(pipeline ⑥):产物 bytes 直接放进
`Action.Content`,executor 不读仓库、不做渲染,plan/execute 之间不存在「重新渲染结果
不一致」的问题。

## 2. state.json 结构

```json
{
  "version": 1,
  "entries": {
    "~/.config/zsh/.zshrc": {
      "module": "zsh",
      "kind": "symlink",
      "source": "modules/zsh/.config/zsh/.zshrc",
      "applied_at": "2026-07-14T10:00:00Z"
    },
    "~/.config/git/config": {
      "module": "git",
      "kind": "rendered",
      "source": "modules/git/.config/git/config.tmpl",
      "hash": "sha256:9f2c…",
      "applied_at": "2026-07-14T10:00:00Z"
    },
    "~/.config/zsh/.zshrc.local": {
      "module": "zsh",
      "kind": "scaffold",
      "source": "modules/zsh/.config/zsh/.zshrc.local.template",
      "applied_at": "2026-07-14T10:00:00Z"
    }
  },
  "run_once": {
    "macos/hooks/setup.sh": { "hash": "sha256:ab31…", "executed_at": "2026-07-14T10:00:01Z" }
  }
}
```

键统一存 `~/` 前缀的规范化路径(`paths.Display`);`hash` 是**上次渲染产物**的 sha256。
整文件读入、修改、临时文件 + rename 写回。state 损坏或缺失不阻塞 apply(退化为「无历史」
模式:无法 prune 与 drift 检测,打印醒目警告)——配合 §3 的收养规则,重跑数次即可
重建绝大部分记录;彻底重建走 `dot state rebuild` [M2]。

**不变量(ADR-18):任何 state 条目的 `module` 必须属于本机当前 profile。** 由两处执行
保证:`apply <module>` 要求模块 ∈ profile;`add` 要求目标模块 ∈ profile(或 `--activate`)。

## 3. 所有权谓词与决策表

### 3.1 owned() 谓词(ADR-14)

「state 有记录」只证明*曾经*创建,不证明*现在*磁盘上的对象还是我们的。一切破坏性动作
(prune、替换、`--force` 覆盖)必须以下述谓词为前置:

```
owned(entry, observed) =
  kind == symlink : observed 为链接 ∧ normalize(observed.LinkDest) == normalize(源绝对路径)
  kind == rendered: observed 为普通文件 ∧ observed.Hash == entry.hash
  kind == scaffold: 恒 false(产物创建即归用户,ADR-12)
```

`normalize` = `paths` 包统一的 `EvalSymlinks` + 绝对化(macOS `/tmp → /private/tmp`
之类的差异在此吸收)。

### 3.2 决策表(decide 纯函数的规格)

managed 条目已在 plan 阶段渲染完成,记产物 hash 为 `newHash`。规则**自上而下短路匹配**。

**link 条目**(源 S → 目标 T):

| # | 条件 | 动作 |
|---|---|---|
| L1 | T 不存在 | create-link |
| L2 | T 是 symlink 且指向 S(normalize 后相等) | skip;state 无记录 → **adopt**(补录) |
| L3 | T 是 symlink,指向他处,但 LinkDest 的仓库内相对尾部 == S 的仓库内相对路径 | create-link(陈旧仓库克隆位置,视为自家旧产物,静默修复 + 打印说明) |
| L4 | T 是 symlink 指向他处,state 有记录 | **CONFLICT-drift**(链接被手工改指;`--force` 恢复,或提示若有意替换请先处理) |
| L5 | T 是 symlink 指向他处,state 无记录 | CONFLICT |
| L6 | T 是普通文件 / 目录 | CONFLICT(`--force` 备份后替换;建议 `dot add` 收编) |

**managed 条目**:

| # | 条件 | 动作 |
|---|---|---|
| M1 | T 不存在 | render |
| M2 | T 非普通文件(symlink/目录) | CONFLICT |
| M3 | actualHash == newHash | skip;state 无记录 → adopt;记录 hash 过期 → 静默刷新为 newHash |
| M4 | state 无记录(且 actual ≠ new) | CONFLICT |
| M5 | actualHash == 记录 hash(产物未被手改) | render(源或变量变更) |
| M6 | actualHash ≠ 记录 hash | **CONFLICT-drift**(手改;`--force` 备份后重渲染) |

**scaffold 条目**:

| # | 条件 | 动作 |
|---|---|---|
| S1 | T 存在(任意形态) | skip(永不覆盖) |
| S2 | T 不存在,state 有记录 | skip + 提示(用户有意删除;`--force` 重建) |
| S3 | T 不存在,state 无记录 | scaffold |

**收养(adopt,ADR-15)**:只写 state、不动文件系统的动作。作用是让「执行了但没记账」
的崩溃窗口通过重跑自愈,也覆盖「用户手工预先建好正确链接」的场景。

### 3.3 prune(方向相反:从 state 找孤儿)

**作用域**:全量 apply → 全部 state 条目;`apply <module...>` → 仅 `entry.module ∈ 请求集`
的条目。候选 = 作用域内且不在本次 desired 的条目。

| # | 条件 | 动作 |
|---|---|---|
| P1 | kind == scaffold | 不删文件(ADR-12);state 摘除 + 提示 |
| P2 | owned(entry, observed) 为真 | prune(symlink 删链 / rendered 删文件) |
| P3 | owned 为假 | 不动文件;state 摘除 + 警告(对象已非我们所有) |

**整模块级孤儿**(某模块全部条目都在候选中,典型于 profile 切换/改动)→ 汇总列出并
交互确认(y/N),`--yes` 跳过。

## 4. 崩溃与并发边界

- **单实例锁(ADR-19)**:pipeline 第①步对 `~/.local/state/dot/lock` 取 flock,失败即
  报错「另一个 dot 进程正在运行」。不支持并发,只防事故(如 `update` 触发的 apply 与
  手动 apply 撞车)。`diff`/`status` 只读不取锁。
- **崩溃恢复**:state 落盘本身原子(temp + rename);「已执行未记账」窗口由收养规则
  收敛——重跑 apply,L2/M3 命中即补录。父目录 fsync 级别的断电持久化不做,记为已接受
  风险(state 本质是可重建的缓存性质数据)。

## 5. 全局不变量校验(pipeline ⑨)

plan 完成、execute 之前,对 desired 全集做校验;**任一违反 → 整体拒绝执行,一个动作
都不做,exit 1**(这类冲突是配置 bug,与单文件 conflict 的「部分继续」语义不同):

1. **target 唯一**:按 target 绝对路径建 map,重复键报错并列出两个来源——跨模块撞车、
   `foo` 与 `foo.tmpl` 去后缀碰撞均在此被抓。
2. **无祖先冲突**:排序后检查相邻项,任一 desired **文件** target 是另一 target 的祖先
   (`paths.Within`,非字符串前缀)→ 报错。
3. **中间目录不穿文件**:target 落地路径的待建中间目录不得与任何 desired 文件条目重合。

`dot doctor` 复用同一校验做静态检查。

## 6. execute 顺序与原子性(ADR-13)

阶段顺序:**`mkdir → create-link / render / scaffold / adopt → prune → hooks`**。

创建先于清理的安全性论证:孤儿定义为「在 state 但不在 desired」,故 prune 集与创建集的
target **必然不相交**,顺序反转不产生相互干扰;而失败时的最坏结果从「旧的已删、新的
未建」(改名场景直接丢配置)变为「新旧并存」,幂等重跑即收敛。附加规则:**本次运行
发生过 error(IO/渲染失败)→ 跳过 prune 阶段**并提示修复后重跑;conflict 不触发跳过。

原子性原语(`internal/fsutil`,executor 不写任何裸 `os.WriteFile`):

- 写文件:同目录临时文件(`CreateTemp(dir, ".dot-tmp-*")`)→ 写入 → `Chmod` 到目标
  权限 → `Sync` → `Rename`。
- 换链接:`Symlink` 到临时名 + `Rename` 覆盖(同卷 rename 原子)。
- 备份:copy + hash 校验(不用 rename,规避 EXDEV 跨卷陷阱),目录 0700、文件 0600。
- state 保存在所有动作之后一次落盘;中途 error 时把已执行动作的记账立即落盘一次,
  最小化未记账窗口。

**Precond 复核(TOCTOU 防线)**:plan 与 execute 之间存在时间窗,executor 对每个破坏性
动作执行前用 `Action.Precond` 复核现势——prune 前重新 `Lstat`/`Readlink` 确认仍 owned、
`--force` 替换前确认 hash 未变;不符则该动作降级为警告跳过。代价是几次 `Lstat`,
换来「永不误删」的底线。

## 7. 安全策略汇总

1. **绝不静默覆盖非托管文件**:conflict 三态(报错 / `--force` 备份覆盖 / 建议
   `dot add` 收编)是铁律;被改指的链接同样按 conflict 处理(L4/L5)。
2. **破坏性动作 = owned() ∧ Precond 复核** 双重前置。
3. `--force` 备份至 `~/.local/state/dot/backup/<RFC3339>/<原相对路径>`(0700/0600),
   备份失败则放弃该动作。
4. 路径写操作前校验:规范化后必须位于 target 根之下,M1 直接拒绝 `~` 之外的 target
   (`--allow-outside-home` [M3])。
5. 敏感面权限:机器配置 0600、state 目录 0700、backup 0700/0600。

## 8. hooks:run_once 语义

- 声明形态见 03 号文档 §3:字符串 [M1],或 `{ script, watch = [...] }` [M2]。
- 执行指纹 = sha256(脚本内容 ‖ 各 watch 文件内容,按声明顺序)[M1 仅脚本内容]。
  以 `run_once["<module>/<script>"]` 为 state 键:无记录 → 执行;指纹变化 → 重新执行;
  未变 → 跳过。(实际语义为 run-on-change,对外仍叫 run_once,文档如实说明。
  Brewfile 进 watch 后,其变更即触发重跑。)
- hook 是 `Kind=RunHook` 的 Action,参与 diff/dry-run 展示;执行恒在最后阶段。
- 子进程环境由 `paths` 注入:`HOME`、`XDG_CONFIG_HOME`、`XDG_STATE_HOME`、`XDG_DATA_HOME`
  (`--home` 下指向假 home,生产与测试同路径),外加 `DOT_MODULE`、`DOT_OS`、
  `DOT_PROFILE`、`DOT_REPO`、`DOT_TARGET`。
- 脚本非零退出:记为失败**不写指纹**(下次重试),apply 整体退出码 1,不回滚已完成的
  文件动作。`--dry-run` 只打印。

## 9. add 的反向映射算法

输入 `dot add <abs-path>`(先 normalize、解 `~`):

```
0. path 匹配 *.local → 硬拒绝(06 号文档 §2 约定)。
1. path 已在 state 中 → 报错「已被管理」。
2. 候选收集:遍历当前 profile 的模块(os 过滤后),path 位于其 target 根之下者,
   得候选 (module, 模块内相对路径)。
3. 亲缘加权:候选中,state 显示已管理 path 同目录或祖先目录下其他文件的 → 「强候选」。
4. 决策:恰一强候选 → 采用;零强候选且恰一候选 → 采用;
   其余 → 退出码 3,列出候选,要求 -m。
5. profile 校验(ADR-18):目标模块 ∉ profile → 报错;--activate 则把模块名追加进
   当前 profile 列表并提示 commit。-m 指向不存在的模块 → 创建目录(提示)。
6. 执行:copy path → modules/<m>/<rel>(--template/--scaffold 追加对应后缀);
   校验副本 hash;非 --scaffold:删除原文件 + 创建 symlink + 登记 state;
   --scaffold:原文件保留为产物,登记 kind=scaffold;
   打印「记得 dot git add && commit」。
```

推断刻意保守(宁可要求 `-m`):add 移动的是用户真实文件,猜错模块的代价远大于多敲
一个 flag。

## 10. 幂等契约(精确表述)

**任意初始状态下连续两次 apply,第二次的动作列表中只允许出现 `Skip` 与既有 `Conflict`
——不得含任何 mutation 动作(create-link/render/scaffold/prune/backup),也不得含
`Adopt`。** conflict 稳定复现直至用户决策,不违反契约。这是 08 号文档集成测试对每个
场景的通用附加断言。
