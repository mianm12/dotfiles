# 05 · Apply 引擎:状态模型、算法与安全策略

## 1. 期望状态模型

对 profile 内每个模块,enumerate 产出文件条目,定级规则:

| 判定 | 级别 | apply 动作 |
|---|---|---|
| 默认 | **link** | target 处创建指向仓库源文件的绝对路径 symlink |
| `.tmpl` 后缀或 `[files] kind="managed"` | **managed** | 渲染模板,产物写入 target |
| `.template` 后缀或 `kind="scaffold"` | **scaffold** | target 不存在时渲染写入一次 |

目录不是动作对象:target 侧缺失的中间目录由 executor `MkdirAll` 创建为**真实目录**
(ADR-3,绝不链接目录),这些目录不记入 state、也不参与 prune(空目录残留无害,清理
它们的收益配不上误删风险)。

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
    "macos/setup.sh": { "hash": "sha256:ab31…", "executed_at": "2026-07-14T10:00:01Z" }
  }
}
```

要点:键统一存 `~/` 前缀的规范化路径(便于 `--home` 重定向与人读);`hash` 是
**上次渲染产物**的 sha256,drift 检测的依据;文件整体读入、修改、临时文件 + rename 写回。
state 损坏或缺失不阻塞 apply(退化为「无历史」模式:无法 prune 与 drift 检测,打印醒目
警告),`doctor` 提供 `--rebuild-state` [M2] 从文件系统反推重建。

## 3. plan 算法(数据流第 ④ 步)

对 desired state 中每个条目,对照实际文件系统与 state:

```
link 条目:
  target 是指向正确源的 symlink            → skip
  target 不存在                            → create-link
  target 是指向他处的 symlink:
      state 中有记录(我们建的旧链)        → create-link(修正,直接替换)
      state 中无记录                        → CONFLICT
  target 是真实文件/目录                    → CONFLICT(--force 时 backup+replace)

managed 条目(先渲染到内存得 newHash):
  target 不存在                             → render
  state 有记录 且 实际文件 hash == 记录 hash:
      newHash == 记录 hash                  → skip
      newHash != 记录 hash                  → render(源或变量变了)
  state 有记录 但 实际 hash != 记录 hash    → CONFLICT-drift(用户手改过;
                                              --force 备份后重渲染)
  state 无记录 且 target 存在               → CONFLICT

scaffold 条目:
  target 存在(不论谁建的)                 → skip(永不覆盖)
  target 不存在 且 state 有记录             → skip + 提示(用户删除了产物,
                                              视为有意为之;--force 可重建)
  target 不存在 且 state 无记录             → scaffold

prune 扫描(方向相反):
  遍历 state.entries,凡不在本次 desired state 中的:
    kind == symlink 且 target 仍指向本仓库   → prune(删链)
    kind == rendered 且 实际 hash == 记录 hash → prune(删文件)
    kind == scaffold                          → 不删(ADR-12),仅从 state 摘除并提示
    实际状态与记录不符(用户改过/删过)      → 不动文件,从 state 摘除 + 警告
```

**幂等契约:任意状态下连续两次 apply,第二次计划必须为空**(conflict 项除外,它们
稳定复现直至用户决策)。这是 08 号文档集成测试的核心断言。

## 4. execute 顺序与原子性

动作按 `prune → mkdir → link/render/scaffold → hooks` 排序执行。原子性原语
(`internal/fsutil`):

- 写文件:同目录临时文件写入 + fsync + `rename`。
- 换链接:`Symlink` 到临时名 + `Rename` 覆盖(Linux 上 rename 覆盖 symlink 原子;
  macOS 同卷同语义)。
- state 保存放在所有动作之后;中途失败时已执行动作逐条即时写入内存 state 并落盘一次,
  保证「执行了但没记账」窗口最小。

## 5. 安全策略汇总

1. **绝不静默覆盖非托管文件**——conflict 三态(报错 / `--force` 备份覆盖 / 建议 `dot add`
   收编)是铁律。
2. `--force` 备份至 `~/.local/state/dot/backup/<RFC3339>/<原相对路径>`,备份失败则
   放弃该动作。
3. prune 只删「确认仍是我们产物」的文件(见 §3 判定),宁可漏删 + 警告。
4. 所有路径写操作前校验:规范化后必须位于 target 根之下,防 manifest 恶意/手误路径逃逸
   (如 `target = "/etc"` 需要 `--allow-outside-home` [M3],M1 直接拒绝 `~` 之外)。

## 6. hooks:run_once 语义

- 声明于模块 manifest,脚本相对模块目录,要求可执行位或以 `sh` 调起。
- 以 `run_once["<module>/<script>"]` 的**内容 hash** 为键:未记录 → 执行;hash 变化 →
  视为新脚本重新执行;未变 → 跳过。(即实际语义为 run-on-change,对外仍叫 run_once,
  文档如实说明。)
- 注入环境变量:`DOT_MODULE`、`DOT_OS`、`DOT_PROFILE`、`DOT_REPO`、`DOT_TARGET`。
- 脚本非零退出:记为失败**不写入 state**(下次重试),apply 整体退出码 1,但不回滚
  已完成的文件动作。
- `--dry-run` 下只打印将执行的脚本。

## 7. add 的反向映射算法

输入 `dot add <abs-path>`(先规范化、解 `~`):

```
1. 若 path 本身已在 state 中 → 报错「已被管理」。
2. 候选收集:遍历当前 profile 全部模块(含 os 过滤后的),对每个模块计算其
   target 根;若 path 位于该根之下,得到候选 (module, 模块内相对路径)。
3. 亲缘加权:候选模块中,若 state 显示该模块已管理 path 的祖先目录下的其他文件
   (同目录或父目录),标记为「强候选」。
4. 决策:
   恰一个强候选            → 采用
   零强候选但恰一个候选     → 采用
   其余(多候选/零候选)    → 退出码 3,列出候选,要求 -m
5. 执行:copy path → modules/<m>/<rel>(--template/--scaffold 时追加后缀);
   校验副本 hash;删除原文件;创建 symlink(scaffold/template 则按其语义处理:
   --scaffold 保留原文件为产物并登记 state,不建链);登记 state;打印 git 提示
   「记得 dot git add && commit」。
```

推断刻意保守(宁可要求 `-m`):add 移动的是用户真实文件,猜错模块的代价远大于
多敲一个 flag。
