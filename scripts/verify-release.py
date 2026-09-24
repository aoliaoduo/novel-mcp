#!/usr/bin/env python3
"""Verify release artifacts produced by scripts/build-release.py."""

from __future__ import annotations

import argparse
import hashlib
import json
import tarfile
import zipfile
from pathlib import Path


def sha256(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def main() -> int:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("directory", nargs="?", default="dist/release")
    args = p.parse_args()
    root = Path(args.directory)
    manifest_path = root / "RELEASE-MANIFEST.json"
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))

    expected = {item["name"]: item for item in manifest["artifacts"]}
    for name, item in expected.items():
        path = root / name
        if not path.is_file():
            raise SystemExit(f"missing artifact: {name}")
        got = sha256(path)
        if got != item["sha256"]:
            raise SystemExit(f"checksum mismatch for {name}: {got} != {item['sha256']}")
        if path.stat().st_size != item["size"]:
            raise SystemExit(f"size mismatch for {name}")

    sums = {}
    for line in (root / "SHA256SUMS.txt").read_text(encoding="utf-8").splitlines():
        digest, name = line.split("  ", 1)
        sums[name] = digest
    expected_sums = {name: item["sha256"] for name, item in expected.items()}
    expected_sums[manifest_path.name] = sha256(manifest_path)
    if sums != expected_sums:
        raise SystemExit("SHA256SUMS.txt does not match RELEASE-MANIFEST.json")

    required = {"START_HERE.txt", "README.md", "README.zh-CN.md", "LICENSE", "portable.flag"}
    for path in root.iterdir():
        if path.suffix == ".zip":
            with zipfile.ZipFile(path) as zf:
                names = set(zf.namelist())
                want = required | {"novel-mcp.exe"}
                if names != want:
                    raise SystemExit(f"portable ZIP file set mismatch: {path.name}: {sorted(names)}")
                mode = zf.getinfo("novel-mcp.exe").external_attr >> 16
                if mode & 0o111 == 0:
                    raise SystemExit(f"portable ZIP executable bit missing: {path.name}")
        elif path.name.endswith(".tar.gz"):
            with tarfile.open(path, "r:gz") as tf:
                names = set(tf.getnames())
                want = required | {"novel-mcp"}
                if names != want:
                    raise SystemExit(f"portable tarball file set mismatch: {path.name}: {sorted(names)}")
                if tf.getmember("novel-mcp").mode & 0o111 == 0:
                    raise SystemExit(f"portable tarball executable bit missing: {path.name}")

    print(f"release artifacts verified: {len(expected)} files, version v{manifest['version']}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
