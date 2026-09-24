#!/usr/bin/env python3
"""Fail closed on repository content that is unsafe to publish.

Default mode scans the current publishable tree (tracked files plus non-ignored
untracked files). --history additionally scans Git patch history and author metadata.
Findings never print secret values: only category, location and commit identifiers.
"""

from __future__ import annotations

import argparse
import os
import re
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path


SCRIPT_REPO = Path(__file__).resolve().parent.parent
ROOT = Path(subprocess.check_output(
    ["git", "-C", str(SCRIPT_REPO), "rev-parse", "--show-toplevel"], text=True
).strip())


@dataclass(frozen=True)
class Finding:
    scope: str
    category: str
    location: str


SECRET_PATTERNS: tuple[tuple[str, re.Pattern[str]], ...] = (
    ("github-token", re.compile(r"\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{20,}\b")),
    ("github-fine-grained-token", re.compile(r"\bgithub_pat_[A-Za-z0-9_]{20,}\b")),
    ("gitlab-token", re.compile(r"\bglpat-[A-Za-z0-9_-]{20,}\b")),
    ("openai-style-api-key", re.compile(r"\bsk-[A-Za-z0-9_-]{20,}\b")),
    ("aws-access-key", re.compile(r"\bAKIA[0-9A-Z]{16}\b")),
    ("google-api-key", re.compile(r"\bAIza[0-9A-Za-z_-]{35}\b")),
    ("npm-token", re.compile(r"\bnpm_[A-Za-z0-9]{30,}\b")),
    ("pypi-token", re.compile(r"\bpypi-[A-Za-z0-9_-]{20,}\b")),
    ("slack-token", re.compile(r"\bxox[baprs]-[A-Za-z0-9-]{20,}\b")),
    ("stripe-live-secret", re.compile(r"\bsk_live_[A-Za-z0-9]{16,}\b")),
    ("tailscale-key", re.compile(r"\btskey-(?:auth|client|api)-[A-Za-z0-9_-]{16,}\b")),
    ("authorization-bearer", re.compile(
        r"Authorization:\s*Bearer\s+(?!<)[A-Za-z0-9._~-]{16,}", re.IGNORECASE
    )),
    ("json-route-or-bearer", re.compile(
        r'"(?:route|bearer)"\s*:\s*"[0-9a-f]{64}"', re.IGNORECASE
    )),
    ("mcp-route-token", re.compile(r"/mcp/[0-9a-f]{64}\b", re.IGNORECASE)),
    ("private-key", re.compile(
        r"-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----"
    )),
)

ABSOLUTE_PATH_PATTERNS: tuple[re.Pattern[str], ...] = (
    re.compile(r"(?i)\b[A-Z]:\\Users\\(?!<)[^\\/\s]+"),
    re.compile(r"/" + r"Users/(?!<)[^/\s]+"),
    re.compile(r"/" + r"home/(?!<)[^/\s]+"),
)

EMAIL_RE = re.compile(
    r"\b[A-Za-z0-9.!#$%&'*+/=?^_\x60{|}~-]+@"
    r"[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?"
    r"(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*"
    r"\.[A-Za-z]{2,}\b"
)

TUNNEL_RE = re.compile(
    r"https://([A-Za-z0-9.-]+\.(?:ts\.net|ngrok-free\.dev))",
    re.IGNORECASE,
)

SAFE_EMAIL_DOMAINS = {"example.com", "users.noreply.github.com"}
SAFE_TUNNEL_MARKERS = ("example", "xxxx", "abc.", "node.tail", "a.b.ts.net", "<")

SENSITIVE_EXACT = {
    "credentials.json",
    "credentials",
    "secrets.json",
    "secret.json",
    "config.json",
    ".git-credentials",
    ".npmrc",
    ".pypirc",
    ".netrc",
    "id_rsa",
    "id_ed25519",
    "id_ecdsa",
    "id_dsa",
}
SENSITIVE_SUFFIXES = (
    ".pem", ".key", ".p12", ".pfx", ".jks", ".keystore", ".ovpn"
)


def git(*args: str) -> str:
    return subprocess.check_output(
        ["git", *args], cwd=ROOT, text=True, errors="replace"
    )


def publishable_files() -> list[str]:
    # 同时扫描已跟踪文件和“尚未 git add、但如果 add . 就会进入仓库”的文件。
    # 被 .gitignore 排除的本地运行时数据（.local、credentials 等）不参与。
    raw = subprocess.check_output(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
        cwd=ROOT,
    ).decode("utf-8", "replace")
    return [x for x in raw.split("\0") if x]


def sensitive_filename(path: str) -> bool:
    name = Path(path).name.lower()
    return (
        name in SENSITIVE_EXACT
        or name.startswith(".env")
        or name.endswith(SENSITIVE_SUFFIXES)
    )


def scan_text(scope: str, location_prefix: str, text: str) -> set[Finding]:
    findings: set[Finding] = set()
    for lineno, line in enumerate(text.splitlines(), 1):
        loc = f"{location_prefix}:{lineno}"
        for category, pattern in SECRET_PATTERNS:
            if pattern.search(line):
                findings.add(Finding(scope, category, loc))
        if any(p.search(line) for p in ABSOLUTE_PATH_PATTERNS):
            findings.add(Finding(scope, "user-absolute-path", loc))
        for match in EMAIL_RE.finditer(line):
            email = match.group(0)
            # URL userinfo（如 https://user:pw@example.test）是 URL 解析测试，
            # 不是联系人邮箱，不按隐私泄露处理。
            scheme = line.rfind("://", 0, match.start())
            if scheme >= 0 and not any(ch.isspace() for ch in line[scheme + 3:match.start()]):
                continue
            domain = email.rsplit("@", 1)[-1].lower()
            if domain not in SAFE_EMAIL_DOMAINS:
                findings.add(Finding(scope, "personal-email", loc))
        for host in TUNNEL_RE.findall(line):
            lower = host.lower()
            if not any(marker in lower for marker in SAFE_TUNNEL_MARKERS):
                findings.add(Finding(scope, "specific-tunnel-host", loc))
    return findings


def scan_current() -> set[Finding]:
    findings: set[Finding] = set()
    for rel in publishable_files():
        if sensitive_filename(rel):
            findings.add(Finding("current", "sensitive-filename", rel))
        path = ROOT / rel
        try:
            if path.is_symlink():
                findings.add(Finding("current", "symlink-file", rel))
                continue
            if not path.is_file():
                findings.add(Finding("current", "non-regular-file", rel))
                continue
            if path.stat().st_size > 4 * 1024 * 1024:
                findings.add(Finding("current", "large-unscanned-file", rel))
                continue
            text = path.read_text(encoding="utf-8")
        except UnicodeDecodeError:
            # Fail closed: binary/non-UTF8 files can carry credentials, EXIF or
            # other private metadata that this text scanner cannot inspect.
            findings.add(Finding("current", "binary-unscanned-file", rel))
            continue
        except OSError:
            findings.add(Finding("current", "unreadable-file", rel))
            continue
        findings |= scan_text("current", rel, text)
    return findings


def scan_history() -> set[Finding]:
    findings: set[Finding] = set()

    # Patch history catches secrets or private paths that were later removed.
    log = git(
        "log", "--all", "--no-color", "--format=commit %H",
        "-p", "--", ".", ":!.toolchain/**"
    )
    commit = "unknown"
    path = "unknown"
    line_no = 0
    for raw in log.splitlines():
        if raw.startswith("commit "):
            commit = raw.split(" ", 1)[1].strip()
            path = "unknown"
            line_no = 0
            continue
        if raw.startswith("diff --git "):
            parts = raw.split()
            if len(parts) >= 4:
                path = parts[3][2:] if parts[3].startswith("b/") else parts[3]
            line_no = 0
            continue
        if raw.startswith("@@"):
            line_no = 0
            continue
        if raw.startswith(("+++", "---")):
            continue
        if raw.startswith("Binary files ") or raw.startswith("GIT binary patch"):
            findings.add(Finding(
                "history", "binary-unscanned-object", f"{commit[:12]}:{path}"
            ))
            continue
        if raw.startswith(("+", "-")):
            line_no += 1
            findings |= scan_text(
                "history", f"{commit[:12]}:{path}:patch{line_no}", raw[1:]
            )

    # Author/committer metadata is not visible in file patches but becomes public
    # with history. Treat either address as publication metadata.
    for row in git("log", "--all", "--format=%H%x09%ae%x09%ce").splitlines():
        parts = row.split("\t")
        if len(parts) != 3:
            continue
        commit_hash, author_email, committer_email = parts
        for role, email in (("author", author_email), ("committer", committer_email)):
            email = email.strip()
            if not email:
                continue
            domain = email.rsplit("@", 1)[-1].lower() if "@" in email else ""
            if domain not in SAFE_EMAIL_DOMAINS:
                findings.add(Finding(
                    "history", f"{role}-email",
                    f"{commit_hash[:12]}:commit-metadata"
                ))

    # Sensitive filenames matter even when Git treats a blob as binary or the
    # contents do not match one of the token regexes.
    for rel in git("log", "--all", "--name-only", "--format=").splitlines():
        rel = rel.strip()
        if rel and sensitive_filename(rel):
            findings.add(Finding("history", "sensitive-filename", rel))

    return findings


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--history", action="store_true",
        help="also scan all reachable Git patch history and author metadata",
    )
    args = parser.parse_args()

    findings = scan_current()
    if args.history:
        findings |= scan_history()

    if findings:
        print(f"public-audit: FAIL ({len(findings)} redacted finding(s))")
        for finding in sorted(findings, key=lambda f: (f.scope, f.category, f.location)):
            print(f"- [{finding.scope}] {finding.category}: {finding.location}")
        print("No secret values are printed. Fix findings before making repository history public.")
        return 1

    scope = "current publishable tree + history" if args.history else "current publishable tree"
    print(f"public-audit: PASS ({scope})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
