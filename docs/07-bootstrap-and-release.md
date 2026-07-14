# 07 · Bootstrap、版本与发布

## 1. 问题定义

同一仓库承载两种变更频率不同的产物:**CLI(构建产物,低频)** 与 **配置(仓库内容,高频)**。
因此分发渠道分离——二进制走 GitHub Releases,配置走 git——并用顶层 manifest 的
`requires` 字段 + 严格解码(ADR-16)做双重兼容性铰链。git tag 只是 CLI 的里程碑,
配置更新就是普通 commit。

## 2. bootstrap.sh

职责严格限定三步,可通过
`curl -fsSL https://raw.githubusercontent.com/<user>/dotfiles/main/bootstrap.sh | sh` 执行。
**下载必经 checksum 校验,安装必须原子**(临时目录验完、再经 bin 目录内同目录 rename 入位):

```sh
#!/bin/sh
set -eu
REPO="you/dotfiles"
BIN_DIR="$HOME/.local/bin"
REPO_DIR="${DOT_REPO:-$HOME/.local/share/dot/repo}"
BASE="https://github.com/$REPO/releases/latest/download"

# 1. 探测平台,下载 + 校验 + 原子安装二进制
os=$(uname -s | tr '[:upper:]' '[:lower:]')          # darwin | linux
arch=$(uname -m); [ "$arch" = x86_64 ] && arch=amd64; [ "$arch" = aarch64 ] && arch=arm64
asset="dot_${os}_${arch}.tar.gz"

tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
curl -fsSL -o "$tmp/$asset"         "$BASE/$asset"
curl -fsSL -o "$tmp/checksums.txt"  "$BASE/checksums.txt"
( cd "$tmp"
  if command -v sha256sum >/dev/null 2>&1; then
    grep " $asset\$" checksums.txt | sha256sum -c -
  else
    grep " $asset\$" checksums.txt | shasum -a 256 -c -
  fi )
tar -xzf "$tmp/$asset" -C "$tmp" dot
mkdir -p "$BIN_DIR"
install -m 0755 "$tmp/dot" "$BIN_DIR/.dot.new"   # 先落同目录临时名
mv -f "$BIN_DIR/.dot.new" "$BIN_DIR/dot"          # 同目录 rename,原子入位

# 2. 克隆配置仓库(已存在则跳过)
[ -d "$REPO_DIR/.git" ] || git clone "https://github.com/$REPO.git" "$REPO_DIR"

# 3. 移交 CLI
exec "$BIN_DIR/dot" init
```

明确不做的事:不从源码编译(裸机无 Go 工具链,这正是发 Release 的原因)、不装
Homebrew/软件(那是 `macos` 模块 `hooks/setup.sh` 的职责)、不改 shell rc(PATH 提示由
`dot doctor` 给出)。硬依赖仅 `curl` + `git` + `tar` + `shasum|sha256sum`,均为 macOS
与主流发行版默认可用或首次使用时由系统引导安装。

## 3. 版本策略

- CLI 版本:semver `vMAJOR.MINOR.PATCH` git tag;`goreleaser` 于 tag push 时交叉编译
  `darwin/arm64`、`darwin/amd64`、`linux/amd64`、`linux/arm64`,产出 tar.gz +
  `checksums.txt`。版本号经 `-ldflags "-X main.version=…"` 注入。
- **本地开发构建**(`go run` / 未注入 ldflags)version 为 `dev`:requires 检查放行 +
  打印警告,否则开发期间每条命令都会被自己的 requires 拦住。
- 配置「版本」即 git commit,无独立编号。
- `requires`(顶层 manifest):声明**本套配置需要的最低 CLI 版本**,仅 `>=x.y.z` 语法
  (ADR-11)。维护纪律:**当某次配置改动用到了新版 CLI 的能力,同一个 commit 里必须
  提升 requires**——这是唯一需要人肉自觉的点,写进 README;遗忘时由严格解码兜底(见 §4)。

## 4. 兼容性矩阵与铰链行为

| 场景 | 行为 |
|---|---|
| 新 CLI + 旧配置 | 必须可用(manifest 向后兼容原则,03 号文档 §7) |
| 旧 CLI + 新配置,requires 满足且无未知键 | 正常工作 |
| 旧 CLI + 新配置,requires 不满足 | pipeline 第②步(宽松预读)即拒绝,提示 `dot self-update`;`self-update`、`git`、`version`、`doctor` 不受拦截 |
| 旧 CLI + 新配置,**requires 忘记提升**但含未知字段 | 严格解码(第③步)拒绝 mutation 命令——失效安全的第二道防线;`doctor` 宽松模式可诊断出未知键 |
| 本地开发构建(version=dev) | requires 放行 + 警告 |

两道防线的分工:`requires` 是**作者显式声明**的兼容界线,报错信息精确(「需要 ≥0.4.0,
当前 0.3.2」);严格解码是**机械兜底**,牺牲报错友好度换取「绝不带着不理解的字段去
prune」的安全底线。

## 5. 日常同步流程

三种变更情形与对应操作:

| 情形 | 发布侧 | 消费侧(其他机器) |
|---|---|---|
| 只改配置 | `dot git commit && dot git push` | `dot update`(pull → requires → apply) |
| 只改 CLI | push 代码,打 tag 发 Release | `dot self-update`(配置无需动) |
| 都改且配置依赖新 CLI | 同一 PR:代码 + 配置 + 提升 requires;先发 Release 再 push main | `dot update` 被 requires 拦下 → 按提示 `dot self-update` → 再 `dot update` |

`dot update` 细节:`git pull --ff-only`(本地脏/分叉即报错,把决策还给用户走 `dot git`);
拉取后先 requires 检查再 apply,顺序不可反。`dot self-update` 细节:GitHub API 查
latest → 比对自身版本 → 下载 + `checksums.txt` 校验 → 写同目录临时文件 → rename 覆盖
自身(POSIX 下替换运行中的二进制安全)。

## 6. git 透传

`dot git <args...>` = `git -C <repo-dir> <args...>` 的直接 exec(继承 stdio,退出码
透传)。不包装任何语义、不解析输出——保留 git 的全部能力与既有肌肉记忆,同时免去记忆
仓库藏在哪个角落。唯一增强:仓库目录不存在时,给出走 bootstrap 的提示而非 git 原生报错。

## 7. 仓库公开性与安全边界

仓库可以公开:配置本身无密级,私密内容由 `*.local` 四道机制(06 号文档 §2)隔离在
机器本地,敏感落地面(机器配置 0600、state/backup 0700)已收紧权限。设计上**不把
「私有仓库」当作任何安全边界**:即使将来转私有,密钥依然不入库。bootstrap 与
self-update 均经 checksums 校验;更进一步的签名验证(cosign/minisign)列为 M3 可选项,
单人场景优先级低。
