#!/usr/bin/env python3
"""Build deterministic novel-mcp release artifacts from the current source tree."""

from __future__ import annotations

import argparse
import gzip
import hashlib
import io
import json
import os
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import zipfile
from dataclasses import dataclass
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
VERSION_FILE = ROOT / "internal" / "server" / "version.go"
GO_VERSION_FILE = ROOT / ".go-version"
TARGETS = (
    ("windows", "amd64"),
    ("linux", "amd64"),
    ("linux", "arm64"),
    ("darwin", "amd64"),
    ("darwin", "arm64"),
)


@dataclass(frozen=True)
class Artifact:
    path: Path
    sha256: str
    size: int


def run(*args: str, env: dict[str, str] | None = None) -> str:
    proc = subprocess.run(
        args,
        cwd=ROOT,
        env=env,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    if proc.returncode != 0:
        raise SystemExit(
            f"command failed ({proc.returncode}): {' '.join(args)}\n{proc.stdout}{proc.stderr}"
        )
    return proc.stdout.strip()


def source_version() -> str:
    text = VERSION_FILE.read_text(encoding="utf-8")
    match = re.search(r'const\s+Version\s*=\s*"([^"]+)"', text)
    if not match:
        raise SystemExit(f"cannot find Version in {VERSION_FILE}")
    return match.group(1)


def pinned_go_version() -> str:
    version = GO_VERSION_FILE.read_text(encoding="utf-8").strip()
    if not re.fullmatch(r"\d+\.\d+(?:\.\d+)?", version):
        raise SystemExit(f"invalid Go version in {GO_VERSION_FILE}: {version!r}")
    return version


def git_commit() -> str:
    return run("git", "rev-parse", "HEAD")


def git_commit_epoch() -> int:
    return int(run("git", "show", "-s", "--format=%ct", "HEAD"))


def git_is_clean() -> bool:
    return run("git", "status", "--porcelain") == ""


def find_go(explicit: str | None) -> str:
    if explicit:
        return explicit
    local = ROOT / ".toolchain" / "go" / "bin" / ("go.exe" if os.name == "nt" else "go")
    if local.exists():
        return str(local)
    found = shutil.which("go")
    if found:
        return found
    raise SystemExit("Go not found; run scripts/bootstrap-go.cmd or pass --go")


def sha256(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def artifact(path: Path) -> Artifact:
    return Artifact(path=path, sha256=sha256(path), size=path.stat().st_size)


def start_here(goos: str, version: str) -> str:
    common = f"""novel-mcp v{version}

novel-mcp exposes novel artifacts and workflow state over MCP.
The server does not call an AI model; connect it from your MCP-capable AI client.

"""
    if goos == "windows":
        launch = """QUICK START (Windows)
1. Double-click novel-mcp.exe.
2. On first launch choose:
   - Web / cloud AI client: uses Tailscale Funnel for an HTTPS MCP URL.
   - Local AI client: listens only on 127.0.0.1.
3. When the TUI says the service is ready, press C to copy the complete MCP config.
4. Paste that config into your AI client's MCP settings.

The route and Bearer token are credentials. Do not share screenshots with them revealed.
This project is not code-signed yet. Windows SmartScreen may warn on first launch; verify
the GitHub Release and SHA256SUMS.txt before choosing to run an unsigned binary.
"""
    elif goos == "darwin":
        launch = """QUICK START (macOS)
1. Open Terminal in this folder and run: ./novel-mcp
2. Choose web/cloud or local mode on first launch.
3. Press C in the TUI to copy the complete MCP config.

If macOS blocks an unsigned downloaded binary, use the system Security settings only if
you trust the GitHub Release and have verified SHA256SUMS.txt.
"""
    else:
        launch = """QUICK START (Linux)
1. Open a terminal in this folder and run: ./novel-mcp
2. Choose web/cloud or local mode on first launch.
3. Press C in the TUI to copy the complete MCP config.

For clipboard shortcuts install a supported clipboard helper (wl-copy or xclip) if needed.
"""
    return common + launch + "\nSee README.md (English) or README.zh-CN.md (简体中文) for more details.\n"


def zip_epoch(epoch: int) -> tuple[int, int, int, int, int, int]:
    import datetime

    dt = datetime.datetime.fromtimestamp(max(epoch, 315532800), tz=datetime.timezone.utc)
    # ZIP timestamps have 2-second precision.
    second = dt.second - dt.second % 2
    return (dt.year, dt.month, dt.day, dt.hour, dt.minute, second)


def write_zip(path: Path, files: list[tuple[str, bytes, int]], epoch: int) -> None:
    stamp = zip_epoch(epoch)
    with zipfile.ZipFile(path, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as zf:
        for name, data, mode in sorted(files, key=lambda item: item[0]):
            info = zipfile.ZipInfo(name, date_time=stamp)
            info.create_system = 3
            info.external_attr = (mode & 0xFFFF) << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            zf.writestr(info, data, compress_type=zipfile.ZIP_DEFLATED, compresslevel=9)


def write_tar_gz(path: Path, files: list[tuple[str, bytes, int]], epoch: int) -> None:
    raw = io.BytesIO()
    with tarfile.open(fileobj=raw, mode="w", format=tarfile.PAX_FORMAT) as tf:
        for name, data, mode in sorted(files, key=lambda item: item[0]):
            info = tarfile.TarInfo(name=name)
            info.size = len(data)
            info.mode = mode
            info.mtime = epoch
            info.uid = 0
            info.gid = 0
            info.uname = ""
            info.gname = ""
            tf.addfile(info, io.BytesIO(data))
    with path.open("wb") as f:
        with gzip.GzipFile(filename="", mode="wb", fileobj=f, compresslevel=9, mtime=epoch) as gz:
            gz.write(raw.getvalue())


def build_target(go: str, version: str, goos: str, goarch: str, staging: Path) -> Path:
    exe_name = "novel-mcp.exe" if goos == "windows" else "novel-mcp"
    out = staging / f"{goos}-{goarch}" / exe_name
    out.parent.mkdir(parents=True, exist_ok=True)
    env = os.environ.copy()
    env.update(
        {
            "GOOS": goos,
            "GOARCH": goarch,
            "CGO_ENABLED": "0",
            "GOAMD64": "v1",
        }
    )
    if goarch != "amd64":
        env.pop("GOAMD64", None)
    subprocess.run(
        [
            go,
            "build",
            "-trimpath",
            "-buildvcs=false",
            "-ldflags=-s -w -buildid=",
            "-o",
            str(out),
            "./cmd/novel-mcp",
        ],
        cwd=ROOT,
        env=env,
        check=True,
    )
    if not out.exists() or out.stat().st_size == 0:
        raise SystemExit(f"build did not produce {out}")
    return out


def package_files(binary: Path, goos: str, version: str) -> list[tuple[str, bytes, int]]:
    binary_name = "novel-mcp.exe" if goos == "windows" else "novel-mcp"
    return [
        (binary_name, binary.read_bytes(), 0o755),
        ("START_HERE.txt", start_here(goos, version).encode("utf-8"), 0o644),
        ("README.md", (ROOT / "README.md").read_bytes(), 0o644),
        ("README.zh-CN.md", (ROOT / "README.zh-CN.md").read_bytes(), 0o644),
        ("LICENSE", (ROOT / "LICENSE").read_bytes(), 0o644),
    ]


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", default="dist/release", help="output directory")
    parser.add_argument("--go", help="Go executable")
    parser.add_argument("--expect-tag", help="require this tag to equal v<source Version>")
    parser.add_argument("--allow-dev", action="store_true", help="allow source versions containing -dev")
    parser.add_argument("--allow-dirty", action="store_true", help="allow a dirty Git tree")
    parser.add_argument("--allow-toolchain-mismatch", action="store_true", help="allow Go version different from .go-version")
    args = parser.parse_args()

    version = source_version()
    if "-dev" in version and not args.allow_dev:
        raise SystemExit(f"refusing release build for development version {version!r}; pass --allow-dev only for local testing")
    if args.expect_tag and args.expect_tag != f"v{version}":
        raise SystemExit(f"tag/version mismatch: tag={args.expect_tag!r}, source=v{version}")
    if not args.allow_dirty and not git_is_clean():
        raise SystemExit("refusing release build from a dirty Git tree; commit first or pass --allow-dirty for local testing")

    go = find_go(args.go)
    go_version = run(go, "version")
    go_env_version = run(go, "env", "GOVERSION")
    expected_go = "go" + pinned_go_version()
    if go_env_version != expected_go and not args.allow_toolchain_mismatch:
        raise SystemExit(f"release toolchain mismatch: got {go_env_version}, want {expected_go} from .go-version")
    commit = git_commit()
    epoch = git_commit_epoch()
    out_dir = (ROOT / args.out).resolve()
    if out_dir.exists():
        shutil.rmtree(out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    with tempfile.TemporaryDirectory(prefix="novel-mcp-release-") as tmp:
        staging = Path(tmp)
        built: dict[tuple[str, str], Path] = {}
        for goos, goarch in TARGETS:
            print(f"build {goos}/{goarch}")
            built[(goos, goarch)] = build_target(go, version, goos, goarch, staging)

        produced: list[Artifact] = []
        for goos, goarch in TARGETS:
            binary = built[(goos, goarch)]
            files = package_files(binary, goos, version)
            base = f"novel-mcp-v{version}-{goos}-{goarch}"
            if goos == "windows":
                raw_exe = out_dir / f"{base}.exe"
                shutil.copyfile(binary, raw_exe)
                produced.append(artifact(raw_exe))
                archive = out_dir / f"{base}.zip"
                write_zip(archive, files, epoch)
            else:
                archive = out_dir / f"{base}.tar.gz"
                write_tar_gz(archive, files, epoch)
            produced.append(artifact(archive))

    produced.sort(key=lambda a: a.path.name)
    manifest = {
        "schema": 1,
        "version": version,
        "tag": f"v{version}",
        "commit": commit,
        "go": go_version,
        "source_date_epoch": epoch,
        "artifacts": [
            {"name": a.path.name, "sha256": a.sha256, "size": a.size}
            for a in produced
        ],
    }
    manifest_path = out_dir / "RELEASE-MANIFEST.json"
    manifest_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8", newline="\n")
    manifest_artifact = artifact(manifest_path)

    sums = out_dir / "SHA256SUMS.txt"
    checksummed = sorted([*produced, manifest_artifact], key=lambda a: a.path.name)
    sums.write_text(
        "".join(f"{a.sha256}  {a.path.name}\n" for a in checksummed),
        encoding="utf-8",
        newline="\n",
    )
    sums_artifact = artifact(sums)

    print(f"version: v{version}")
    print(f"commit:  {commit}")
    print(f"go:      {go_version}")
    print(f"output:  {out_dir}")
    for item in produced:
        print(f"{item.sha256}  {item.path.name}")
    print(f"{manifest_artifact.sha256}  {manifest_artifact.path.name}")
    print(f"{sums_artifact.sha256}  {sums_artifact.path.name}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
