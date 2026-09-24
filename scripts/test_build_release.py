from __future__ import annotations

import importlib.util
import sys
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("build-release.py")
SPEC = importlib.util.spec_from_file_location("build_release", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


class BuildReleaseOutputSafetyTests(unittest.TestCase):
    def with_root(self, root: Path):
        class RootGuard:
            def __enter__(guard):
                guard.old = MODULE.ROOT
                MODULE.ROOT = root

            def __exit__(guard, exc_type, exc, tb):
                MODULE.ROOT = guard.old

        return RootGuard()

    def test_rejects_repository_ancestor(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp).resolve()
            root = base / "workspace" / "repo"
            root.mkdir(parents=True)
            marker = base / "workspace" / "keep.txt"
            marker.write_text("keep", encoding="utf-8")
            with self.with_root(root), self.assertRaises(SystemExit):
                MODULE.prepare_output_dir(base / "workspace")
            self.assertTrue(root.is_dir())
            self.assertEqual(marker.read_text(encoding="utf-8"), "keep")

    def test_refuses_to_clear_unknown_files(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = (Path(tmp) / "repo").resolve()
            out = root / "dist" / "release"
            out.mkdir(parents=True)
            marker = out / "notes.txt"
            marker.write_text("keep", encoding="utf-8")
            with self.with_root(root), self.assertRaises(SystemExit):
                MODULE.prepare_output_dir(out)
            self.assertEqual(marker.read_text(encoding="utf-8"), "keep")

    def test_clears_only_known_release_artifacts(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = (Path(tmp) / "repo").resolve()
            out = root / "dist" / "release"
            out.mkdir(parents=True)
            (out / "novel-mcp-v0.1.0-windows-amd64.zip").write_bytes(b"old")
            (out / "SHA256SUMS.txt").write_text("old\n", encoding="utf-8")
            with self.with_root(root):
                MODULE.prepare_output_dir(out)
            self.assertTrue(out.is_dir())
            self.assertEqual(list(out.iterdir()), [])


if __name__ == "__main__":
    unittest.main()
