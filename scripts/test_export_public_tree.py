from __future__ import annotations

import importlib.util
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("export-public-tree.py")
SPEC = importlib.util.spec_from_file_location("export_public_tree", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class ExportPublicTreeSafetyTests(unittest.TestCase):
    def test_force_rejects_source_ancestor_without_deleting_it(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp).resolve()
            ancestor = base / "workspace"
            root = ancestor / "novel-mcp"
            root.mkdir(parents=True)
            marker = ancestor / "keep.txt"
            marker.write_text("keep", encoding="utf-8")

            with self.assertRaises(SystemExit):
                MODULE.ensure_safe_destination(root, ancestor, force=True)

            self.assertTrue(root.is_dir())
            self.assertEqual(marker.read_text(encoding="utf-8"), "keep")

    def test_rejects_destination_inside_source(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = (Path(tmp) / "repo").resolve()
            root.mkdir()
            dest = root / "export"
            with self.assertRaises(SystemExit):
                MODULE.ensure_safe_destination(root, dest, force=True)
            self.assertFalse(dest.exists())


if __name__ == "__main__":
    unittest.main()
