## What changed

Describe the problem and the smallest change that solves it.

## Architecture impact

Check any public or recovery contract touched by this PR:

- [ ] MCP tools / schemas / prompts / resources / completion
- [ ] HTTP auth / Host / Origin / public access
- [ ] project revision / locking / concurrency
- [ ] `commit_chapter` / crash recovery / idempotency
- [ ] persisted project format / store layout
- [ ] `next_step` routing / state transitions
- [ ] build / release artifacts
- [ ] none of the above

## Verification

List the tests you ran. At minimum, normal changes should pass:

```bash
python scripts/check.py
```

High-risk changes should also pass:

```bash
python scripts/check.py --race --history
```

## Public-repository privacy

- [ ] I did not include real MCP URLs, routes, Bearers, API keys, cookies, credentials, private keys, private novel content, personal email addresses, local home paths, machine hostnames, or private/internal network identifiers.
- [ ] Examples use obvious placeholders such as `<route>`, `<bearer>`, `<host>`, or `example.com`.
- [ ] I did not import branches/tags/commits from the old private archive.
- [ ] I did not bypass repository privacy hooks/audits.
