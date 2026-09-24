#!/usr/bin/env python3
"""Export the current publishable tree into a fresh, history-free Git repo.

This is the safe path for publishing a private development repository whose old
Git history contains credentials, personal metadata, hostnames, or local paths.
The exporter:

1. runs scripts/public-audit.py against the current publishable tree;
2. copies only tracked and non-ignored untracked files;
3. refuses symlinks and refuses an output directory inside the source repo;
4. never copies .git, ignored caches, runtime data, credentials, or build output;
5. initializes a brand-new Git repository with no commits and no remote.

It intentionally does not create the first commit: configure a public-safe Git
identity (for example the noreply address shown by GitHub in Settings > Emails)
before committing.
"""

from __future__ import annotations

import argparse
import os
import shutil
import stat
import subprocess
import sys
from pathlib import Path


def run(*args: str, cwd: Path | None = None, check: bool = True) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        list(args), cwd=cwd, text=True, errors="replace", check=check,
        stdout=subprocess.PIPE, stderr=subprocess.PIPE,
    )


def repo_root() -> Path:
    out = run("git", "rev-parse", "--show-toplevel").stdout.strip()
    return Path(out).resolve()


def publishable_files(root: Path) -> list[str]:
    raw = subprocess.check_output(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
        cwd=root,
    ).decode("utf-8", "replace")
    return [p for p in raw.split("\0") if p]


def is_within(path: Path, parent: Path) -> bool:
    try:
        path.relative_to(parent)
        return True
    except ValueError:
        return False


def remove_tree(path: Path) -> None:
    def onerror(func, failed_path, _exc_info):
        # Windows may mark files under .git/objects read-only. Make only the
        # failed path writable, then retry the operation that rmtree requested.
        os.chmod(failed_path, stat.S_IWRITE)
        func(failed_path)

    shutil.rmtree(path, onerror=onerror)


def ensure_safe_destination(root: Path, dest: Path, force: bool) -> None:
    if dest == root or is_within(dest, root):
        raise SystemExit("拒绝：输出目录必须位于源仓库之外，避免递归复制或误提交。")
    if dest == Path(dest.anchor) or dest == Path.home().resolve():
        raise SystemExit("拒绝：输出目录过于宽泛。请指定一个新的专用目录。")
    if dest.exists():
        if not force:
            raise SystemExit(f"输出目录已存在：{dest}\n如确认可删除并重建，请加 --force。")
        remove_tree(dest)
    dest.mkdir(parents=True)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--out",
        default="../novel-mcp-public",
        help="导出目录；必须位于当前仓库之外（默认 ../novel-mcp-public）",
    )
    parser.add_argument(
        "--force",
        action="store_true",
        help="若输出目录已存在，删除后重建",
    )
    args = parser.parse_args()

    root = repo_root()
    dest = Path(args.out)
    if not dest.is_absolute():
        dest = (root / dest).resolve()
    else:
        dest = dest.resolve()

    audit = subprocess.run(
        [sys.executable, str(root / "scripts" / "public-audit.py")],
        cwd=root,
    )
    if audit.returncode != 0:
        print("导出已中止：当前可发布树未通过 public-audit。", file=sys.stderr)
        return audit.returncode

    files = publishable_files(root)
    ensure_safe_destination(root, dest, args.force)

    copied = 0
    copied_bytes = 0
    try:
        for rel in files:
            src = root / rel
            if src.is_symlink():
                raise RuntimeError(f"拒绝复制符号链接：{rel}")
            if not src.is_file():
                continue
            target = dest / rel
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(src, target)
            copied += 1
            copied_bytes += src.stat().st_size

        run("git", "init", "-b", "main", cwd=dest)
    except Exception:
        shutil.rmtree(dest, ignore_errors=True)
        raise

    mib = copied_bytes / (1024 * 1024)
    print(f"public tree exported: {dest}")
    print(f"files: {copied}, size: {mib:.2f} MiB")
    print("fresh Git repository initialized with no commits and no remote")
    print("next: configure a public-safe Git name/email, review git status, then create the initial commit")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
