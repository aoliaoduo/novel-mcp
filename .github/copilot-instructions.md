# GitHub Copilot instructions for novel-mcp

Before making non-trivial changes, read `/AGENTS.md` and `/docs/ARCHITECTURE.md`. If working under `internal/server`, `internal/store`, or `internal/tools`, also read that directory's `AGENTS.md`.

Core invariants:

- The server never calls an LLM. Creative/semantic reasoning belongs to the MCP client.
- `next_step` is deterministic and driven by persisted facts.
- Remote callers must never gain arbitrary host path, file, or shell access.
- Project writes respect whole-project revision / `expected_revision` semantics.
- `commit_chapter` is a recoverable Saga; preserve frozen payload and idempotent replay behavior.
- MCP protocol/tool/resource/prompt changes are public API changes and require contract tests/docs.
- Do not weaken Host/Origin/Bearer/route security or redact less information in errors.
- Never commit real secrets, MCP URLs/routes/Bearers, private email addresses, local home paths, machine hostnames, private novel data, or old private Git history.

Use the smallest relevant change and keep tests close to the behavior being changed.

Canonical local verification:

```bash
python scripts/check.py
```

For concurrency, HTTP/MCP lifecycle, persistence, recovery, or other high-risk changes:

```bash
python scripts/check.py --race --history
```

Do not bypass `.githooks` or privacy audits to make a commit pass.
