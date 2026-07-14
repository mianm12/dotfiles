# 03 · Manifest 规范:两级结构与合并语义

## 1. 职责划分总则

**模块自身的事写在模块里,跨模块的事写在顶层。** 每个模块目录必须自包含:把目录拷走
即是完整一份该工具的配置。顶层 manifest 不得出现任何针对单个模块的映射规则。

| 关注点 | 顶层 `dot.toml` | 模块 `modules/<name>/dot.toml` |
|---|---|---|
| CLI 版本要求 `requires` | ✅ | ❌ |
| 全局默认值 `[defaults]` | ✅ | ❌ |
| 全局忽略规则 `[ignore]` | ✅ | 追加自己的 |
| profile 分组 | ✅ | ❌ |
| init 变量声明 `[data]` | ✅ | ❌ |
| OS 过滤、target | 仅提供默认 | ✅ 覆盖 |
| 文件级声明 `[files]` | ❌ | ✅ |
| hooks | ❌ | ✅ |

## 2. 顶层 manifest 完整字段

```toml
# dot.toml(仓库根)
requires = ">=0.3.0"        # 本仓库配置要求的最低 CLI 版本(仅支持 >= 语法,ADR-11)

[defaults]                  # 每个模块 manifest 的兜底值,键与模块级字段同名
target = "~"
os = ["darwin", "linux"]    # 缺省即全平台,通常无需写

[ignore]                    # gitignore 风格 pattern,模块内相对路径匹配
patterns = [".DS_Store", "README.md", "*.md"]
# 内置恒定忽略(无法关闭):dot.toml、.git/、*.swp

[profiles]                  # 模块分组;"@name" 引用其他 profile,解析时展开去重
base = ["zsh", "git", "tmux", "nvim"]
mac  = ["@base", "karabiner", "hammerspoon", "macos"]
linux = ["@base", "linux-pkgs"]
work = ["@mac", "work-ssh"]

[data.email]                # 声明 init 时需要收集的模板变量(供 dot init 交互)
prompt = "Git commit email"
default = "me@example.com"

[data.machine]
prompt = "Machine name"
```

约束与校验规则:

1. profile 引用**不得成环**,解析时检测,成环即报错。
2. profile 中引用的模块目录不存在 → `apply` 报错(而非静默跳过),防止拼写错误。
3. `modules/` 下存在但未被任何 profile 引用的模块是合法的(`dot status` 会列为
   "unassigned" 供参考,不算错误)。
4. 未知顶层键 → 警告不报错,为向后兼容留余地(新版配置字段被旧版 CLI 读到时,
   应先被 `requires` 拦截;警告是第二道防线)。

## 3. 模块 manifest 完整字段

模块 manifest **整体可选**。没有 `dot.toml` 的模块等价于:全平台、`target` 取顶层默认、
无 hooks、文件级行为全按后缀推断。

```toml
# modules/vscode/dot.toml
os = ["darwin", "linux"]    # 取值为 GOOS:darwin | linux;缺省 = defaults.os

# target 二选一写法:
target = "~"                                          # 写法 A:字符串,全平台同一位置
[target]                                              # 写法 B:按 OS 分表(处理路径差异)
darwin = "~/Library/Application Support/Code/User"
linux  = "~/.config/Code/User"

[ignore]
patterns = ["*.bak"]        # 与顶层 ignore 取并集(这是唯一的并集特例,见 §4)

[files.".config/git/config.tmpl"]   # 键 = 模块内相对路径,精确匹配
mode = "0600"               # 落地权限(仅对 render/scaffold 产物有意义;symlink 不适用)
# kind = "link"             # 显式覆盖后缀推断:link | managed | scaffold
# target = "~/other/path"   # 单文件 target 覆盖(完整落地路径,含文件名)

[files.".config/zsh/.zshrc.local.template"]
# scaffold 由 .template 后缀自动推断,此处仅在需要 mode 等附加声明时出现

[hooks]
run_once = ["setup.sh"]     # 相对模块目录;执行语义见 05 号文档 §6
```

字段推断优先级:`[files]` 显式声明 > 文件后缀(`.tmpl` → managed,`.template` → scaffold)
> 默认(link)。落地文件名 = 源文件名去掉 `.tmpl` / `.template` 后缀。

## 4. 合并语义(精确定义)

有效配置的计算,对每个模块独立进行:

```
effective(module) = 顶层 [defaults] 打底,被模块 manifest 中出现的键整键覆盖
```

- **整键覆盖(ADR-7)**:模块写了 `os = ["darwin"]`,则顶层 `defaults.os` 完全不参与,
  不做数组并集;`[target]` 表同理,模块出现该键即整表替换。
- **唯一例外:`ignore.patterns` 取并集**。忽略规则语义上是「都要排除」,覆盖反而危险
  (模块加一条忽略却意外解除了 `.DS_Store` 全局忽略)。此特例必须在文档与 `dot doctor`
  输出中显式可见。
- 模块之间互不引用、互不合并;不存在"模块继承"。
- profile 只是名字集合,不携带任何配置,不参与合并。

## 5. ignore 规则细节

- pattern 相对模块根匹配,语法取 gitignore 子集:`*`、`**`、目录尾 `/`;不支持 `!` 反选
  (需要例外时用 `[files]` 显式声明 kind,而不是玩 pattern 优先级)。
- 匹配阶段发生在 enumerate(数据流 ③),被忽略文件不进入 desired state,自然也不会
  被 prune 逻辑视为孤儿。

## 6. 校验命令

`dot doctor` 对 manifest 做静态检查:TOML 语法、未知键、profile 环、悬空模块引用、
`[files]` 键指向不存在的文件、`target` 表中出现不支持的 OS 键。CI 中可跑
`dot doctor --manifest-only` 作为配置的 lint(退出码非零即失败)。

## 7. 兼容性原则

manifest 格式的演进规则:**新版 CLI 必须能读旧格式;新字段必须有缺省行为;删除字段前
先经历一个"读到即警告"的版本**。配置反向依赖新 CLI 特性时,提升顶层 `requires` 即可,
由 07 号文档的版本铰链兜底。
