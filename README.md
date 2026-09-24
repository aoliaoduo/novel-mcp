# novel-mcp

[![safety](https://github.com/aoliaoduo/novel-mcp/actions/workflows/safety.yml/badge.svg)](https://github.com/aoliaoduo/novel-mcp/actions/workflows/safety.yml)
[![codeql](https://github.com/aoliaoduo/novel-mcp/actions/workflows/codeql.yml/badge.svg)](https://github.com/aoliaoduo/novel-mcp/actions/workflows/codeql.yml)

**English** · [简体中文](README.zh-CN.md)

A recoverable MCP service for AI-assisted novel writing.

The AI client does the creative reasoning and prose generation. `novel-mcp` owns the durable facts: outlines, chapters, summaries, validation, optimistic concurrency, deterministic routing, and crash recovery.

```text
AI / MCP Host  ── Streamable HTTP MCP ──▶  novel-mcp  ──▶  project files
  thinks + writes                               validates + persists
```

The server does **not** call a model, store model API keys, run a background writer, expose a shell, or accept arbitrary host file paths.

## Why use it

- **Long-form continuity** — chapters, arcs, volumes, summaries, world state, and story compass live on disk instead of only in chat history.
- **Deterministic next step** — `next_step` returns a machine-readable action plan so the client does not have to guess which tool comes next.
- **Safe retries** — project-wide `revision` values reject stale writes and accidental duplicate appends.
- **Crash recovery** — interrupted chapter commits resume from frozen payloads instead of reconstructing facts from conversation memory.
- **Public or local access** — run on loopback only, or expose the loopback service through Tailscale Funnel.

## Quick start

1. Download the Windows ZIP from **GitHub Releases**, extract it, then double-click `novel-mcp.exe`.
2. On first launch choose either **Web / cloud AI client** or **Local AI client**.
3. When the TUI is ready, press **`C`** to copy the complete MCP configuration.
4. Paste it into your MCP client. After connecting, the client should call `list_projects`, then `next_step` for the selected project.

For web/cloud clients, the default public mode uses Tailscale Funnel and Bearer auth. Local mode listens only on `127.0.0.1`.

Official release archives are portable. Keep the extracted folder together: when `portable.flag` is present, configuration, credentials, logs, and novel projects are stored under the sibling `data/` directory, so moving or backing up the whole folder moves the workspace with it. Generated `config.json` files store the data path as `"data": "."` instead of pinning an absolute install path. A standalone binary without `portable.flag` falls back to `~/.novel-mcp`.

Local/public mode defaults to `allow_origins: ["*"]`, so web MCP hosts do not need to be allowlisted one domain at a time. Public access still requires the secret route and Bearer.

If setup fails, run:

```bash
novel-mcp doctor
novel-mcp doctor --deep
```

`doctor` is read-only and intentionally redacts route tokens, Bearer tokens, public hostnames, project IDs, novel text, and local absolute paths so its output can be shared in an issue or with an AI assistant.

While the service is running it also appends a redacted JSONL call log at `<data>/logs/novel-mcp.log` (`data/logs/novel-mcp.log` in a portable bundle), which is useful for reconstructing the web AI's actual tool sequence, error codes, and latency. The log does not store prose, route tokens, Bearer tokens, or full request bodies.

## Common commands

| Command | Purpose |
| --- | --- |
| `novel-mcp local` | Start loopback-only mode with Bearer auth and TUI |
| `novel-mcp public` | Start the Tailscale Funnel/Serve preflight and MCP service |
| `novel-mcp url` | Print the current MCP URL |
| `novel-mcp prompt` | Print a connection snippet and starter prompt |
| `novel-mcp doctor [--deep] [--json]` | Run redacted diagnostics |
| `novel-mcp token rotate --i-understand-this-invalidates-current-clients` | Rotate route + Bearer credentials |
| `novel-mcp version` | Print the version |

## AI workflow

The normal control loop is deliberately small:

```text
next_step → execute the returned actions → next_step → ... → done=true
```

`next_step.result.actions` contains the tool name, server-known arguments, model-supplied inputs, dependencies, revision source, and mutually-exclusive choice groups.

A new project is staged in two passes: first choose the planning tier and save a seed premise with `scale=short|mid|long`; then call `next_step` again so the server can emit the correct `outline` or `layered_outline` actions. Clients should not guess the remaining foundation shape in the first pass.

Normal chapter writing:

```text
novel_context → plan_chapter → draft_chapter → read_chapter
→ check_consistency → semantic revision if needed → commit_chapter
```

Rewriting an already completed chapter skips `plan_chapter` and starts from the current final/context.

See [Workflow guide](docs/WORKFLOW.md) for action-plan semantics, revisions, recovery, arc/volume routing, and error handling.

## Security model

A public endpoint is protected by both a random route capability and, by default, an independent Bearer token. The service keeps an exact Host gate, allows any browser Origin by default, and keeps its HTTP surface intentionally small.

Project access is by restricted project ID only. Remote callers cannot submit arbitrary filesystem paths. There is no shell/exec tool and no model credential API.

Read [SECURITY.md](SECURITY.md) before exposing an instance outside loopback. Connectivity and Tailscale troubleshooting live in [Connectivity guide](docs/CONNECTIVITY.md).

## Documentation

| Topic | English | 中文 |
| --- | --- | --- |
| User workflow / MCP behavior | [WORKFLOW.md](docs/WORKFLOW.md) | [WORKFLOW.zh-CN.md](docs/WORKFLOW.zh-CN.md) |
| Startup / public access / troubleshooting | [CONNECTIVITY.md](docs/CONNECTIVITY.md) | [CONNECTIVITY.zh-CN.md](docs/CONNECTIVITY.zh-CN.md) |
| Architecture and invariants | [ARCHITECTURE.md](docs/ARCHITECTURE.md) | same document |
| Security model | [SECURITY.md](SECURITY.md) | same document |
| Contributing | [CONTRIBUTING.md](CONTRIBUTING.md) | same document |
| Agent coding rules | [AGENTS.md](AGENTS.md) | same document |
| Releases | [docs/RELEASING.md](docs/RELEASING.md) | same document |
| Changelog | [CHANGELOG.md](CHANGELOG.md) | same document |

## Development

Enable the repository privacy hooks after cloning:

```bash
git config core.hooksPath .githooks
```

Use the Go version declared by the repository. On Windows, when Go is not installed globally:

```bat
scripts\bootstrap-go.cmd
scripts\build-dev.cmd
```

Run the standard checks with:

```bash
python scripts/check.py
python scripts/check.py --race --history   # high-risk / pre-release changes
```

More development rules are in [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md).

## Upstream

The novel artifact layer is derived from `ainovel-cli` under Apache-2.0. See [UPSTREAM.md](UPSTREAM.md) for provenance and the boundary between reused business logic and this MCP adapter.
