# 07 · Bootstrap、版本与发布

## 1. 问题定义

同一仓库承载两种变更频率不同的产物:**CLI(构建产物,低频)** 与 **配置(仓库内容,高频)**。
因此分发渠道分离——二进制走 GitHub Releases,配置走 git——并用顶层 manifest 的
`requires` 字段做兼容性铰链。git tag 只是 CLI 的里程碑,配置更新就是普通 commit。

## 2. bootstrap.sh

职责严格限定三步,保持「极薄」,可通过
`curl -fsSL https://raw.githubusercontent.com/<user>/dotfiles/main/bootstrap.sh | sh` 执行:

```sh
#!/bin/sh
set -eu
REPO="you/dotfiles"
BIN_DIR="$HOME/.local/bin"
REPO_DIR="${DOT_REPO:-$HOME/.local/share/dot/repo}"

# 1. 探测平台,下载最新 release 二进制
os=$(uname -s | tr '[:upper:]' '[:lower:]')          # darwin | linux
arch=$(uname -m); [ "$arch" = x86_64 ] && arch=amd64; [ "$arch" = aarch64 ] && arch=arm64
mkdir -p "$BIN_DIR"
curl -fsSL "https://github.com/$REPO/releases/latest/download/dot_${os}_${arch}.tar.gz" \
  | tar -xz -C "$BIN_DIR" dot

# 2. 克隆配置仓库(已存在则跳过)
[ -d "$REPO_DIR/.git" ] || git clone "https://github.com/$REPO.git" "$REPO_DIR"

# 3. 移交 CLI
exec "$BIN_DIR/dot" init
```

明确不做的事:不从源码编译(裸机无 Go 工具链,这正是发 Release 的原因)、不装
Homebrew/软件(那是 `macos` 模块 run_once 脚本的职责)、不改 shell rc(PATH 提示由
`dot doctor` 给出)。硬依赖仅 `curl` + `git` + `tar`,三者在 macOS 与主流发行版默认可用
或首次使用时由系统引导安装。

## 3. 版本策略

- CLI 版本:semver `vMAJOR.MINOR.PATCH` git tag;`goreleaser` 于 tag push 时交叉编译
  `darwin/arm64`、`darwin/amd64`、`linux/amd64`、`linux/arm64`,产出 tar.gz +
  `checksums.txt`。版本号经 `-ldflags "-X main.version=…"` 注入。
- 配置「版本」即 git commit,无独立编号。
- `requires`(顶层 manifest):声明**本套配置需要的最低 CLI 版本**,仅 `>=x.y.z` 语法
  (ADR-11)。维护纪律:**当某次配置改动用到了新版 CLI 的能力,同一个 commit 里必须
  提升 requires**——这是整个解耦方案里唯一需要人肉自觉的点,写进 README。

## 4. 兼容性矩阵与铰链行为

| 场景 | 行为 |
|---|---|
| 新 CLI + 旧配置 | 必须可用(manifest 向后兼容原则,03 号文档 §7) |
| 旧 CLI + 新配置,requires 满足 | 正常工作 |
| 旧 CLI + 新配置,requires 不满足 | 所有读 manifest 的命令(apply/diff/update/init…)在第①步即拒绝执行,提示 `dot self-update`;`self-update`、`git`、`version`、`doctor` 不受拦截 |
| requires 字段本身不认识(极旧 CLI) | 顶层未知键警告 + 尽力解析;此窗口只存在于 requires 机制引入之前,M1 就带上它即可消灭 |

## 5. 日常同步流程

三种变更情形与对应操作:

| 情形 | 发布侧 | 消费侧(其他机器) |
|---|---|---|
| 只改配置 | `dot git commit && dot git push` | `dot update`(pull → requires 检查 → apply) |
| 只改 CLI | push 代码,打 tag 发 Release | `dot self-update`(配置无需动) |
| 都改且配置依赖新 CLI | 同一 PR:代码 + 配置 + 提升 requires;先发 Release 再 push main | `dot update` 被 requires 拦下 → 按提示 `dot self-update` → 再 `dot update` |

`dot update` 细节:`git pull --ff-only`(本地脏/分叉即报错,把决策还给用户走 `dot git`);
拉取后先 requires 检查再 apply,顺序不可反。`dot self-update` 细节:GitHub API 查
latest → 比对自身版本 → 下载 + `checksums.txt` 校验 → 写临时文件 → rename 覆盖自身
(POSIX 下替换运行中的二进制安全)。

## 6. git 透传

`dot git <args...>` = `git -C <repo-dir> <args...>` 的直接 exec(继承 stdio,退出码
透传)。不包装任何语义、不解析输出——保留 git 的全部能力与用户已有的肌肉记忆,同时
免去记忆仓库藏在哪个角落。唯一增强:若仓库目录不存在,给出走 bootstrap 的提示而非
git 的原生报错。

## 7. 仓库公开性与安全边界

仓库可以公开(配置本身无密级,私密内容已被 `*.local` 约定隔离在机器本地)。但设计上
**不把「私有仓库」当作任何安全边界**:即使将来转私有,密钥依然不入库。bootstrap 下载
二进制经 checksums 校验;更进一步的签名验证(cosign/minisign)列为 M3 可选项,单人
场景优先级低。
