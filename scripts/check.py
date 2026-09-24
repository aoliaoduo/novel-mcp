#!/usr/bin/env python3
"""Run the repository's standard local verification gates.

Default: privacy audit + go test + go vet + git diff --check.
Optional: --race adds go test -race; --history scans all reachable Git history.
"""

from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent


def run(*args: str) -> None:
    print("+", " ".join(args), flush=True)
    subprocess.run(args, cwd=ROOT, check=True)


def go_healthy(exe: Path | str) -> bool:
    try:
        version = subprocess.run(
            [str(exe), "version"], cwd=ROOT, check=True,
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        )
        del version
        goroot = subprocess.check_output(
            [str(exe), "env", "GOROOT"], cwd=ROOT, text=True, errors="replace"
        ).strip()
    except (OSError, subprocess.CalledProcessError):
        return False
    root = Path(goroot)
    return (
        (root / "src" / "runtime").is_dir()
        and (root / "src" / "unsafe" / "unsafe.go").is_file()
        and (root / "pkg" / "tool").is_dir()
    )


def find_go() -> str:
    exe = "go.exe" if os.name == "nt" else "go"
    pinned = ROOT / ".toolchain" / "go" / "bin" / exe
    if pinned.is_file():
        if go_healthy(pinned):
            return str(pinned)
        raise SystemExit(
            "Local .toolchain is incomplete or corrupted. Remove .toolchain and rerun "
            "scripts/bootstrap-go.cmd on Windows."
        )
    found = shutil.which("go")
    if found and go_healthy(found):
        return found
    raise SystemExit(
        "Go toolchain not found. Install Go or run scripts/bootstrap-go.cmd on Windows."
    )


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--race", action="store_true", help="also run go test -race ./...")
    parser.add_argument(
        "--history", action="store_true",
        help="also audit the complete reachable Git history",
    )
    args = parser.parse_args()

    audit = [sys.executable, str(ROOT / "scripts" / "public-audit.py")]
    run(*audit)
    if args.history:
        run(*audit, "--history")

    run(sys.executable, "-m", "unittest", "discover", "scripts", "-p", "test_*.py")

    go = find_go()
    run(go, "test", "-count=1", "./...")
    run(go, "vet", "./...")
    if args.race:
        run(go, "test", "-race", "-count=1", "./...")

    run("git", "diff", "--check")
    print("check: PASS")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
