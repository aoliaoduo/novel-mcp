# Startup, connectivity, and troubleshooting

[简体中文](CONNECTIVITY.zh-CN.md) · **English**

This guide covers the local process, MCP URL, Tailscale public mode, and common connection failures.

## Two startup modes

### Local AI client

```bash
novel-mcp local
```

The service listens on `127.0.0.1`, keeps Bearer authentication enabled, and does not use Tailscale.

Use this when the MCP client runs on the same machine.

### Web / cloud AI client

```bash
novel-mcp public
```

The service itself still listens on loopback. `novel-mcp` checks Tailscale, configures Serve/Funnel, verifies public DNS when using Funnel, then starts the MCP service.

Use this when a cloud-hosted MCP client must reach your machine over HTTPS.

On Windows, launching the `.exe` with no arguments asks for one of these modes the first time and remembers the choice.

## TUI shortcuts

On the running connection page:

- `C`: copy the complete MCP client configuration;
- `U`: copy only the MCP URL;
- `B`: copy the Bearer token;
- `P`: copy the connection starter prompt;
- `O`: open the health endpoint;
- `V`: temporarily reveal/hide credentials;
- `q` / `Ctrl+C`: stop the process.

Credentials are masked to reduce accidental screenshots. Clipboard content copied with `C`, `U`, or `B` still contains the real credential material.

## Endpoint shape

Public mode normally produces an endpoint like:

```text
https://<node>.<tailnet>.ts.net/mcp/<64-hex-route>
```

with a separate header:

```text
Authorization: Bearer <64-hex-token>
```

The health endpoint is:

```text
https://<node>.<tailnet>.ts.net/healthz
```

Do not post the real URL and Bearer together in public issues, screenshots, logs, or chat rooms.

## Useful commands

```bash
novel-mcp url
novel-mcp prompt
novel-mcp doctor
novel-mcp doctor --deep
novel-mcp doctor --json
novel-mcp public --dry-run
```

`doctor` stays local and does not probe the public hostname. `public --dry-run` is the better tool for Tailscale/public-network diagnosis because it performs the read-only public preflight.

## MCP client transport compatibility

`novel-mcp` speaks MCP protocol `2026-07-28` over Streamable HTTP. It does not expose the deprecated HTTP+SSE transport.

For this protocol revision, `GET` and `DELETE` on the MCP endpoint intentionally return `405 Method Not Allowed`; normal MCP traffic uses `POST`. Therefore an error such as:

```text
SSE error: ... 405
```

usually means the client is probing the endpoint as the old HTTP+SSE transport instead of using current Streamable HTTP. Updating the MCP host, selecting its Streamable HTTP transport, or using another current MCP host is the right fix. Do not remove the route/Bearer credentials or add a trailing slash to work around it.

## Public-mode flags

| Flag | Meaning |
| --- | --- |
| `--tailnet-only` | Use Tailscale Serve only; reachable inside the tailnet, not from the public internet |
| `--allow-origin https://...` | Add an allowed browser Origin; repeatable |
| `--smoke` | Run a lightweight read-only public smoke check after startup |
| `--dry-run` | Read-only preflight; do not write config, mount Funnel, or start the MCP service |
| `--no-tui` | Use plain text output |
| `--port` | Local MCP listen port |
| `--serve-port` | Tailscale HTTPS port |
| `--data` | Data directory |

Public Funnel without Bearer is intentionally refused unless the explicit token-only consent flag is supplied. In that mode the URL itself becomes the credential and is easier to leak through history, logs, proxies, and screenshots.

## When the client cannot connect

### 1. Run `doctor`

```bash
novel-mcp doctor --deep
```

It checks config parsing/safety, data access, credential shape, service presence, and optionally project integrity. The output is redacted for support use.

### 2. Check whether the service is already running

Launching a second copy normally detects the existing `novel-mcp` health endpoint and prints the reusable connection card instead of failing with a generic port error.

### 3. Check Tailscale state

For public mode, Tailscale should report `BackendState=Running`.

If it reports `NeedsLogin`, finish login in the Tailscale desktop client. If the device needs approval, approve it in the tailnet administration UI.

If it stays in `NoState`, `starting`, or cannot reach the control plane, and you use a TUN/system proxy, make Tailscale domains bypass the proxy:

```text
*.tailscale.com
*.tailscale.io
*.ts.net
```

Then retry. On Windows, `scripts\tailscale-repair.cmd` is the fallback repair path when the desktop service/process itself is unhealthy.

### 4. Funnel says on but public DNS is missing

A local "Funnel on" message only confirms the local Serve/Funnel configuration. Public DNS publication is asynchronous.

Avoid repeatedly toggling Funnel off/on: that restarts publication and can extend the wait. `novel-mcp public` checks public DNS separately instead of treating the local status line as proof of reachability.

If publication remains absent after a reasonable propagation window, re-check login/node health before changing application configuration.

### 5. Browser Origin is rejected

Browser-based clients send an `Origin` header. Add the exact origin with repeated `--allow-origin` flags or configuration entries. The service does not use wildcard reflection.

### 6. Cloud agent cannot reach `--tailnet-only`

A vendor-hosted/cloud agent is usually outside your tailnet. `--tailnet-only` is suitable for clients already inside that tailnet, not arbitrary cloud services. Use public Funnel with Bearer when external cloud reachability is required.

## Smoke testing

`novel-mcp public --smoke` performs a lightweight read-only check of the public URL: wrong-route 404, health, MCP handshake, and tool listing.

For a full environment test with real writes, use the Go SDK smoke tool from a machine that actually reaches the public route:

```bash
go run ./cmd/tailnet-smoke -url <full-mcp-url> -bearer <token>
```

It creates a temporary `smoke-*` project, exercises a write/commit/export loop, and deletes the temporary project after a successful run. If the run fails mid-way, the project is kept for diagnosis.

## Credential rotation

When a route or Bearer token may have leaked:

```bash
novel-mcp token rotate --i-understand-this-invalidates-current-clients
novel-mcp url
```

Rotation replaces both route and Bearer and invalidates existing clients immediately.

For the full threat model, see [../SECURITY.md](../SECURITY.md).
