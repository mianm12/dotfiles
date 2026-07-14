# 04 · CLI 规范:命令、Flag 与输出

## 1. 总览

```
dot <command> [flags] [args]

核心      init | apply | diff | status | add | doctor
同步      update | self-update | git
辅助      edit [M2] | module [M2] | version
```

## 2. 全局 flag

| flag | 说明 |
|---|---|
| `--repo <dir>` | 覆盖仓库位置(> `DOT_REPO` > 机器配置 > 默认) |
| `--home <dir>` | **隐藏**。整体重定向 `~` 与所有状态路径,测试专用 |
| `--profile <name>` | 本次运行覆盖机器配置中的 profile |
| `-v / --verbose` | 详细输出(含跳过项与内容 diff) |
| `--no-color` | 关闭彩色输出(pipe 时自动关闭) |

## 3. 退出码(全命令统一)

| 码 | 含义 |
|---|---|
| 0 | 成功;对 `diff` / `status` 表示「无差异 / 无异常」 |
| 1 | 运行错误(IO 失败、manifest 非法、requires 不满足…) |
| 2 | `diff` / `status` 发现差异或 drift(供脚本判断,类比 `git diff --exit-code`) |
| 3 | 存在 conflict,需要用户决策(`apply` 遇到未托管的同名真实文件且未给 `--force`) |

## 4. 命令规范

### 4.1 `dot init`

新机器初始化,幂等(重复运行进入「更新变量」模式)。流程:

1. 定位仓库(不存在则报错,提示走 bootstrap)。
2. requires 检查。
3. 交互选择 profile(列出顶层 `[profiles]` 键)。
4. 按顶层 `[data]` 声明逐项询问变量(带 default 的回车即接受)。
5. 写入 `~/.config/dot/config.toml`。
6. 询问「立即 apply?」,默认 yes。

Flag:`--profile <name>` 与 `--set key=value`(可重复)跳过对应交互,支持无人值守:
`dot init --profile mac --set email=me@x.com --yes`。

### 4.2 `dot apply [module...]`

核心命令。无参数 = 应用当前 profile 的全部模块;带参数 = 仅指定模块(仍受 os 过滤)。

| flag | 行为 |
|---|---|
| `-n / --dry-run` | 只打印计划,不落盘,退出码规则同 `diff` |
| `--force` | conflict 项:备份原文件后覆盖(备份至 state 目录,见 05 号文档 §5) |
| `--prune` / `--no-prune` | 是否清理孤儿,默认 `--prune` |

行为要点:计划为空时输出 `Already up to date.`;存在 conflict 且无 `--force` 时,
**其余动作照常执行**,conflict 项列出并以退出码 3 结束(部分成功优于全盘卡死,且幂等
保证重跑无害)。

### 4.3 `dot diff`

等价 `apply --dry-run` 的只读版本,输出动作计划。`-v` 时对 managed 模板追加渲染内容的
unified diff。设计上 `diff` 永不写盘(包括 state)。

### 4.4 `dot status`

面向「巡检」而非「预览」,分节输出:

```
Profile: mac (12 modules, 87 files managed)

DRIFT (2)
  ~/.config/git/config          rendered file modified by hand
  ~/.config/zsh/aliases.zsh     symlink removed

PENDING (1)
  macos/setup.sh                run_once not yet executed

UNASSIGNED MODULES (1)
  experimental-nvim             not referenced by any profile
```

无异常输出 `Clean.`,退出码 0;有 DRIFT/PENDING 退出码 2。

### 4.5 `dot add [-m <module>] [--template|--scaffold] <path>...`

把 `$HOME` 中已有文件收编入库并原位替换为 symlink。反向映射与模块推断算法见
05 号文档 §7。要点:

- 推断唯一命中 → 直接归入该模块;多命中或零命中 → 退出码 3,提示加 `-m`。
- `-m` 指向不存在的模块时创建模块目录(打印提示)。
- `--template`:入库为 `.tmpl`(managed),入库后打印「请手动将机器相关值替换为
  {{ .var }}」提醒;`--scaffold` 同理入库为 `.template`。
- `--dry-run` 支持;移动文件 + 建链两步整体失败回滚(先 copy 验证后删原,不用 rename
  跨设备陷阱)。

### 4.6 `dot doctor`

环境与配置自检,只读。检查项:二进制是否在 PATH、仓库存在且为 git 仓库、manifest 静态
校验(见 03 号文档 §6)、state.json 可解析、state 中记录的链接是否死链、`--home` 未泄漏
到生产路径、当前 OS 是否受支持。`--manifest-only` 供 CI。

### 4.7 `dot update`

配置侧更新:`git pull --ff-only`(仓库脏时报错,提示先 `dot git status`)→ requires
检查(不满足 → 报错提示 `dot self-update`,**不执行 apply**)→ `apply`。
`--no-apply` 只拉取。

### 4.8 `dot self-update`

二进制侧更新:查询 GitHub Releases latest → 版本比对 → 下载对应 GOOS/GOARCH 资产至
临时文件 → 校验 checksums.txt → 原子替换自身。`--tag v0.4.0` 指定版本。详见 07 号文档。

### 4.9 `dot git [args...]`

透传:等价于在仓库目录执行 `git <args...>`。不做任何包装语义,帮用户省掉 `cd` 进
`~/.local/share/dot/repo` 这一步。示例:`dot git add -A && dot git commit -m x && dot git push`。

### 4.10 `dot version`

输出 CLI 版本、commit、构建时间,以及当前仓库顶层 `requires` 与满足情况。

### 4.11 `dot edit <target-path>` [M2]

打开 `$EDITOR` 编辑给定落地路径对应的**源文件**(symlink 直接就是源;rendered 反查
state 找到 `.tmpl` 源),模板保存后自动 re-render 该文件。

## 5. 输出与日志约定

- 计划行格式:`<verb>  <target>  (<reason>)`,verb ∈ `link | render | scaffold | prune |
  backup+replace | skip | CONFLICT`。verb 左对齐彩色,`skip` 仅 `-v` 显示。
- 人类输出走 stdout;错误与警告走 stderr;为脚本消费预留 `--json` [M3]。
- 所有会写文件系统的命令开头打印一行上下文:`repo=… profile=… os=darwin`,事后排查全靠它。
