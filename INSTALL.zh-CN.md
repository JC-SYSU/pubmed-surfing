[English](INSTALL.md) | 中文版

# 安装指南（面向执行安装的 agent）

本文是操作规范：按顺序执行，每一步都有「期望输出」与「失败处理」。全部通过后运行文末「验收清单」，并把结果一并报告。**不要跳过任何一步**——本系统的部件相互独立，漏装任何一个都会表现为"装了但静默不工作"（例如客户端注册到了一个根本没构建过的二进制上）。

仓库根目录记为 `$REPO`。以下命令均在 `$REPO` 下执行。平台：**macOS / Windows**（有预编译发布包）与 **Linux**（从源码构建，见第 1 节）。预编译发布包只覆盖 `darwin/arm64` 和 `windows/amd64`；其余平台一律从源码构建。

## 0. 前置检查

| # | 检查项 | 命令 | 期望输出 | 失败处理 |
| ---- | ---- | ---- | ---- | ---- |
| 0.1 | Go ≥ 1.25 | `go version` | go1.25.x 或更高 | 安装对应版本后重来 |
| 0.2 | git | `git --version` | 有版本号输出 | 安装 git |
| 0.3 | MCP 客户端 | `command -v codex; command -v claude; command -v opencode` | 至少检测到一个 | 先装任意一个受支持的客户端 |

### 0.5 先探测客户端，确定安装形态

| 形态 | 机器上检测到 | 注册到哪 | 执行哪些节 |
| ---- | ---- | ---- | ---- |
| **A** | 只有 Codex | `~/.codex/config.toml` | 第 1 节 + 4A（走发布流程再加 2、3） |
| **B** | 只有 Claude Code | `claude mcp add` | 第 1 节 + 4B（走发布流程再加 2、3） |
| **C** | 只有 OpenCode | OpenCode 的 MCP 配置 | 第 1 节 + 4C（走发布流程再加 2、3） |
| **D** | 检测到多个 | 每个检测到的客户端 | 第 1 节 + 对应的每个 4X 节 |

## 1. 构建并验证源码（所有平台）

```bash
go test ./...
go test -race ./...
go vet ./...
go build -o pubmed-surfing ./cmd/pubmed-surfing
```

期望输出：全部测试通过、`vet` 无告警、仓库根下出现 `pubmed-surfing`。
失败处理：Go 版本太旧 → 安装 1.25+ 后重开终端；模块下载失败 → 检查网络或代理，修好再跑。

阻塞式开发服务器用 `go run ./cmd/pubmed-surfing` 启动。本节也是 Linux 的安装路径：构建产物就是服务器本体。

## 2. 发布归档（可选但推荐；macOS / Windows）

预编译归档也会发布到 GitHub Releases，由 CI 从发布 tag 构建。下载本平台的归档后直接跳到第 3 节。下面的命令是在 macOS 或 Windows 上自己构建同样的归档：

```bash
go run ./cmd/pubmed-surfingctl release
```

它做的事：

- 用 `-trimpath` 和 `-ldflags=-s -w` 构建两个二进制，连同 `RELEASE.json` 清单打包成 `artifacts/pubmed-surfing-go-<版本>-<os>-<arch>.tar.gz`（macOS）/ `.zip`（Windows），并在每个归档旁写出 `.sha256` 校验文件。
- 拒绝在 dirty worktree 上发布；`--allow-dirty` 会把 git 提交号和 `-dirty-` 后缀标进发布 id（开发产物）。
- 归档已存在时报错。

期望输出：`artifacts/` 下出现两个归档加两个 `.sha256`。
失败处理：worktree 不干净 → 先提交，或加 `--allow-dirty`；归档已存在 → 删掉旧的，或把 `internal/pubmed/types.go` 里的 `const Version` 升个版。

## 3. 安装到用户级运行时目录

```bash
go run ./cmd/pubmed-surfingctl install artifacts/pubmed-surfing-go-<版本>-<os>-<arch>.tar.gz
go run ./cmd/pubmed-surfingctl verify
```

`install` 在激活前依次执行这一串校验：

1. 旁车校验：归档 SHA-256 必须与 `.sha256` 文件一致。
2. 解压安全检查：拒绝路径穿越、符号链接、以及任何不在清单里的文件。
3. 平台匹配：清单里的 `goos`/`goarch` 必须等于本机。
4. 载荷校验：每个文件的 SHA-256 必须与 `RELEASE.json` 一致。
5. MCP 冒烟测试：新二进制必须完成 MCP initialize、恰好列出 11 个工具、并成功调用 `pubmed_clear_cache`。
6. 全部通过后：把验证过的目录挪到 `<home>/releases/<id>` 并激活为 `current`。

- `<home>`：macOS 为 `~/.local/share/pubmed-surfing-go`，Windows 为 `%LOCALAPPDATA%\pubmed-surfing-go`；可用 `PUBMED_SURFING_GO_HOME` 覆盖。
- macOS 用原子的 `current` 符号链接；Windows 用 `current.json`（临时文件改名切换），运行时根目录还有一个 `pubmed-surfingctl.exe` 供 `run-current` 使用。
- 已安装的 release 从不覆盖或删除；`activate <release-id>` 可回滚到已装的任意版本。

期望输出：`current` 指向新 release id；`verify` 输出 OK。
失败处理：校验和不符 → 归档损坏或被改过，重跑 release；平台不匹配 → 选错了别的机器用的归档（Linux 见第 1 节）。

客户端要启动的入口路径，记为 `{{ENTRY}}` 供第 4 节使用：

```text
macOS:    <home>/current/pubmed-surfing
Windows:  <home>\pubmed-surfingctl.exe run-current
```

## 4. 注册 MCP 客户端

只注册 0.5 节检测到的客户端。服务器通过 stdio 讲 MCP 协议：协议输出在 stdout，诊断在 stderr。不需要 API key，不需要 URL。

### 4A Codex —— `~/.codex/config.toml`

追加（若已有 `[mcp_servers.pubmed_surfing]` 段则跳过）：

```toml
[mcp_servers.pubmed_surfing]
command = "{{ENTRY}}"
```

Windows 上 `{{ENTRY}}` 是两个 argv 项（`<home>\pubmed-surfingctl.exe run-current`），要拆开写：`command` 填 exe 路径，`args = ["run-current"]`。大批量结果导致工具超时的话，加一行 `tool_timeout_sec = 60.0`。用 `codex mcp list` 验证——服务器应显示 connected。

### 4B Claude Code

```bash
claude mcp add --transport stdio pubmed-surfing -- {{ENTRY}}
```

用 `claude mcp list` 验证。以后移除用 `claude mcp remove pubmed-surfing`。

### 4C OpenCode

OpenCode 的 MCP 配置格式在历次版本里变过；按当前 OpenCode 官方文档注册一个名为 `pubmed-surfing`、command 为 `{{ENTRY}}` 的 stdio 服务器。用 `opencode mcp`（新版本为 `opencode mcp list`）验证。

## 5. 验收清单（所有项必须通过）

| # | 验证项 | 命令 | 通过标准 |
| ---- | ---- | ---- | ---- |
| 1 | 源码可构建 | `go test ./... && go vet ./...` | 全部通过 |
| 2 | 发布流程（macOS/Windows） | `ls artifacts/` | 归档 + `.sha256` 存在 |
| 3 | 运行时已安装 | `go run ./cmd/pubmed-surfingctl verify` | OK；`current` 可解析 |
| 4 | 客户端能看到服务器 | `codex mcp list` / `claude mcp list` / OpenCode 等价命令 | `pubmed-surfing` 在列、connected |
| 5 | 工具列表 | 让客户端列出工具 | 恰好 11 个 PubMed Surfing 工具 |
| 6 | 真实检索（可选，需网络） | 一次 `pubmed_search` 调用，`retmax <= 10` | 返回带计数的文本结果 |

## 6. 升级 / 卸载

**升级**：构建新版本、`install`、`verify`——旧 release 留在 `<home>/releases/`；`activate <旧版本-id>` 可回滚。

**卸载**：

1. 删除第 4 节的客户端注册（`~/.codex/config.toml` 里的 `pubmed_surfing` 段、`claude mcp remove pubmed-surfing`、或 OpenCode 的对应条目）。
2. `rm -rf <home>`。
3. Linux（或任何源码构建的安装）删掉第 1 节构建的二进制。

不碰其他任何文件；项目和数据都不在这个运行时里。

## 7. 故障排查

| 症状 | 原因与处理 |
| ---- | ---- |
| `go` 命令报版本错误 | PATH 里的 Go 低于 1.25；装 1.25+ 并重开终端 |
| `release` 拒绝执行 | worktree 不干净——先提交，或加 `--allow-dirty`（开发产物） |
| `release: artifact already exists` | 发布 id 撞车——删掉 `artifacts/` 里的旧条目，或升 `const Version` |
| `install: artifact checksum mismatch` | 归档或旁车文件损坏/被改——重跑 release |
| `install: platform mismatch` | 归档不是本机的系统/架构；发布只覆盖 darwin/arm64 与 windows/amd64（Linux：源码构建） |
| `verify` 解析不到 `current` | 还没激活过——跑 `install`（会自动激活）或 `activate <release-id>` |
| 客户端能看到服务器但工具调用超时 | NCBI 不可达或限流——esummary 超时时检索会返回仅 PMID 的部分结果；稍后重试，或调大 `tool_timeout_sec` |
| 客户端显示服务器启动失败 | `{{ENTRY}}` 过期——激活的 release 变了；把客户端重新指向当前 `<home>/current/...` 路径 |
| 检索返回的结果比预期少 | 这是上限设计不是 bug：`retmax` 默认 10 / 上限 100，截断会标 `truncated: true`；`confirm_full: true` 可抬到 10000 |