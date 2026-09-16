[中文版](INSTALL.zh-CN.md) | English

# Installation Guide (for the agent performing the install)

This document is an operating spec: run the steps in order — every step lists an **expected output** and **failure handling**. When everything passes, run the "Acceptance checklist" at the end and report the results. **Do not skip steps** — the pieces are independent, and a missing one shows up as "installed but silently not working" (e.g. a client registered against a binary that was never built).

Let `$REPO` be the repository root. All commands below run from `$REPO`. Platforms: **macOS / Windows** (prebuilt release archives) and **Linux** (build from source, section 1). Prebuilt releases cover `darwin/arm64` and `windows/amd64` only; everything else builds from source.

## 0. Prerequisites

| # | Check | Command | Expected | Failure handling |
| ---- | ---- | ---- | ---- | ---- |
| 0.1 | Go >= 1.25 | `go version` | go1.25.x or newer | install Go, then restart |
| 0.2 | git | `git --version` | a git version | install git |
| 0.3 | MCP client | `command -v codex; command -v claude; command -v opencode` | at least one present | install any supported client first |

### 0.5 Probe the clients first — pick your install profile

| Profile | Clients detected | Register with | Run sections |
| ---- | ---- | ---- | ---- |
| **A** | Codex only | `~/.codex/config.toml` | 1 + 4A (+ 2, 3 for the release flow) |
| **B** | Claude Code only | `claude mcp add` | 1 + 4B (+ 2, 3 for the release flow) |
| **C** | OpenCode only | OpenCode MCP config | 1 + 4C (+ 2, 3 for the release flow) |
| **D** | several present | every client present | 1 + each 4X section that applies |

## 1. Build and verify the source (all platforms)

```bash
go test ./...
go test -race ./...
go vet ./...
go build -o pubmed-surfing ./cmd/pubmed-surfing
```

Expected: all tests pass, `vet` clean, `pubmed-surfing` exists in the repo root.
Failure: Go too old → install 1.25+ and restart; module download fails → network or proxy; fix and re-run.

A blocking development server runs with `go run ./cmd/pubmed-surfing`. This section is also the Linux install path: the built binary is the server.

## 2. Release archives (optional but recommended; macOS / Windows)

Prebuilt archives are also published to GitHub Releases, built by CI from the release tag. Download the archive for your platform and jump straight to section 3. The commands below build the same archives yourself on macOS or Windows:

```bash
go run ./cmd/pubmed-surfingctl release
```

What it does:

- Builds both binaries with `-trimpath` and `-ldflags=-s -w`, wraps them with a `RELEASE.json` manifest into `artifacts/pubmed-surfing-go-<version>-<os>-<arch>.tar.gz` (macOS) / `.zip` (Windows), and writes a `.sha256` sidecar next to each archive.
- Refuses a dirty worktree; `--allow-dirty` marks the release id with the git commit and a `-dirty-` suffix (a development artifact).
- Errors if the artifact already exists.

Expected: two archives plus two `.sha256` files under `artifacts/`.
Failure: dirty worktree → commit first or pass `--allow-dirty`; artifact exists → remove it or bump the version in `internal/pubmed/types.go` (`const Version`).

## 3. Install into the per-user runtime home

```bash
go run ./cmd/pubmed-surfingctl install artifacts/pubmed-surfing-go-<version>-<os>-<arch>.tar.gz
go run ./cmd/pubmed-surfingctl verify
```

`install` runs this chain before anything becomes active:

1. Sidecar check: the archive's SHA-256 must match its `.sha256` file.
2. Unpack safety: rejects path traversal, symlinks, and any file not in the manifest.
3. Platform match: manifest `goos`/`goarch` must equal the host's.
4. Payload hashes: every file's SHA-256 must match `RELEASE.json`.
5. MCP smoke test: the new binary must complete MCP initialize, list exactly the 11 tools, and call `pubmed_clear_cache` without error.
6. Only then: move the verified tree to `<home>/releases/<id>` and activate it as `current`.

- `<home>` is `~/.local/share/pubmed-surfing-go` on macOS and `%LOCALAPPDATA%\pubmed-surfing-go` on Windows; override with `PUBMED_SURFING_GO_HOME`.
- macOS uses an atomic `current` symlink; Windows uses `current.json`, switched via a temp-file rename, plus a `pubmed-surfingctl.exe` at the home root for `run-current`.
- Existing releases are never overwritten or deleted; `activate <release-id>` rolls back to an already installed release.

Expected: `current` resolves to the new release id; `verify` prints OK.
Failure: checksum mismatch → damaged or tampered archive; re-run release. Platform mismatch → you picked an archive for another OS/arch (see section 1 on Linux).

The entry point clients will launch — record it as `{{ENTRY}}` for section 4:

```text
macOS:    <home>/current/pubmed-surfing
Windows:  <home>\pubmed-surfingctl.exe run-current
```

## 4. Register the MCP client(s)

Only register clients detected in 0.5. The server speaks MCP over stdio: protocol on stdout, diagnostics on stderr. No API key, no URL.

### 4A Codex — `~/.codex/config.toml`

Append (skip if an `[mcp_servers.pubmed_surfing]` stanza already exists):

```toml
[mcp_servers.pubmed_surfing]
command = "{{ENTRY}}"
```

On Windows, `{{ENTRY}}` is two argv entries (`<home>\pubmed-surfingctl.exe run-current`); split it — `command` gets the `.exe` path and `args = ["run-current"]`. If tools time out on huge results, add `tool_timeout_sec = 60.0`. Verify with `codex mcp list` — the server should appear connected.

### 4B Claude Code

```bash
claude mcp add --transport stdio pubmed-surfing -- {{ENTRY}}
```

Verify with `claude mcp list`. To remove later: `claude mcp remove pubmed-surfing`.

### 4C OpenCode

OpenCode's MCP config format has changed across releases; register a stdio server named `pubmed-surfing` with command `{{ENTRY}}`, per the current OpenCode documentation. Verify with `opencode mcp` (or `opencode mcp list` on newer versions).

## 5. Acceptance checklist (all items must pass)

| # | Verify | Command | Pass criteria |
| ---- | ---- | ---- | ---- |
| 1 | source builds | `go test ./... && go vet ./...` | all pass |
| 2 | release flow (macOS/Windows) | `ls artifacts/` | archive + `.sha256` present |
| 3 | runtime installed | `go run ./cmd/pubmed-surfingctl verify` | OK; `current` resolves |
| 4 | client sees the server | `codex mcp list` / `claude mcp list` / OpenCode equivalent | `pubmed-surfing` present, connected |
| 5 | tools listed | ask the client to list tools | exactly the 11 PubMed Surfing tools |
| 6 | live search (optional, needs network) | one `pubmed_search` call, `retmax <= 10` | a text result with a count |

## 6. Upgrade / uninstall

**Upgrade**: build a new release, `install` it, `verify` — old releases stay in `<home>/releases/`; `activate <older-id>` rolls back.

**Uninstall**:

1. Remove the client registration from section 4 (the `pubmed_surfing` stanza in `~/.codex/config.toml`, `claude mcp remove pubmed-surfing`, or the OpenCode entry).
2. `rm -rf <home>`.
3. On Linux (or any source-built install) remove the binary you built in section 1.

No other files are touched; projects and data do not live in this runtime.

## 7. Troubleshooting

| Symptom | Cause & fix |
| ---- | ---- |
| `go` commands report a version error | Go in PATH is < 1.25; install 1.25+ and restart the shell |
| `release` refuses to run | dirty worktree — commit, or `--allow-dirty` for a development artifact |
| `release: artifact already exists` | release id collision — remove the `artifacts/` entry or bump `const Version` |
| `install: artifact checksum mismatch` | archive or sidecar damaged/tampered — re-run release |
| `install: platform mismatch` | the archive is for another OS/arch; releases only cover darwin/arm64 and windows/amd64 (Linux: build from source) |
| `verify` can't resolve `current` | nothing activated yet — run `install` (which activates) or `activate <release-id>` |
| client shows the server but tool calls time out | NCBI unreachable or rate-limited — the search returns PMID-only partials on esummary timeout; retry later, or raise `tool_timeout_sec` |
| client shows the server as failed to start | `{{ENTRY}}` stale — the active release changed after configuration; point the client at the current `<home>/current/...` path again |
| search returns fewer results than expected | that's the size cap, not a bug: `retmax` default 10 / cap 100, `truncated: true` marks a cut; `confirm_full: true` raises it to 10,000 |