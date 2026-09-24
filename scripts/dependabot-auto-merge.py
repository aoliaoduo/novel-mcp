#!/usr/bin/env python3
"""Classify a Dependabot PR for conservative automatic merging.

Exit codes:
  0: safe to auto-merge (Dependabot, non-draft, patch-only updates)
  2: valid Dependabot PR but requires manual review
  1: malformed/unexpected input; fail closed

Input is the JSON produced by:
  gh pr view <number> --json author,body,headRefName,isDraft,state,title
"""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path


VERSION_RE = re.compile(r"^[vV]?(\d+)\.(\d+)\.(\d+)(?:[-+].*)?$")
FROM_TO_RE = re.compile(
    r"\bfrom\s+`?([vV]?\d+\.\d+\.\d+(?:[-+][^\s`,.)]+)?)`?\s+to\s+"
    r"`?([vV]?\d+\.\d+\.\d+(?:[-+][^\s`,.)]+)?)`?",
    re.IGNORECASE,
)
TABLE_RE = re.compile(
    r"^\|[^|]+\|\s*`?([vV]?\d+\.\d+\.\d+(?:[-+][^`|\s]+)?)`?\s*\|\s*"
    r"`?([vV]?\d+\.\d+\.\d+(?:[-+][^`|\s]+)?)`?\s*\|",
    re.MULTILINE,
)


def version_tuple(value: str) -> tuple[int, int, int] | None:
    match = VERSION_RE.match(value.strip())
    if not match:
        return None
    # Pre-release/build versions are deliberately not auto-merged.
    if "-" in value or "+" in value:
        return None
    return tuple(int(part) for part in match.groups())


def patch_only(old: str, new: str) -> bool:
    before = version_tuple(old)
    after = version_tuple(new)
    if before is None or after is None:
        return False
    return before[0] == after[0] and before[1] == after[1] and after[2] > before[2]


def transitions(body: str) -> list[tuple[str, str]]:
    found = TABLE_RE.findall(body)
    if found:
        return found
    return FROM_TO_RE.findall(body)


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: dependabot-auto-merge.py <pr-json>", file=sys.stderr)
        return 1
    try:
        data = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        print(f"invalid PR metadata: {exc}", file=sys.stderr)
        return 1

    author = ((data.get("author") or {}).get("login") or "").lower()
    head = str(data.get("headRefName") or "")
    if author not in {"dependabot", "app/dependabot"} or not head.startswith("dependabot/"):
        print("manual: PR is not an authenticated Dependabot branch")
        return 2
    if data.get("isDraft") or data.get("state") != "OPEN":
        print("manual: PR is draft or not open")
        return 2

    changes = transitions(str(data.get("body") or ""))
    if not changes:
        print("manual: could not confidently parse dependency version transitions")
        return 2
    if not all(patch_only(old, new) for old, new in changes):
        print("manual: at least one update is minor/major/prerelease or otherwise non-patch")
        return 2

    print(f"auto-merge: {len(changes)} patch-only dependency update(s)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
