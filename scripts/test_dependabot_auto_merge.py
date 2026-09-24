from __future__ import annotations

import importlib.util
import json
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("dependabot-auto-merge.py")
SPEC = importlib.util.spec_from_file_location("dependabot_auto_merge", SCRIPT)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class DependabotAutoMergeTest(unittest.TestCase):
    def test_patch_only(self) -> None:
        self.assertTrue(MODULE.patch_only("1.2.3", "1.2.4"))
        self.assertTrue(MODULE.patch_only("0.0.16", "0.0.30"))

    def test_minor_major_and_prerelease_are_manual(self) -> None:
        self.assertFalse(MODULE.patch_only("1.2.3", "1.3.0"))
        self.assertFalse(MODULE.patch_only("1.2.3", "2.0.0"))
        self.assertFalse(MODULE.patch_only("1.2.3", "1.2.4-rc.1"))

    def test_group_table(self) -> None:
        body = """
| Package | From | To |
| --- | --- | --- |
| alpha | 1.2.3 | 1.2.4 |
| beta | 0.0.8 | 0.0.9 |
"""
        self.assertEqual(
            MODULE.transitions(body),
            [("1.2.3", "1.2.4"), ("0.0.8", "0.0.9")],
        )

    def test_dependabot_identity_variants(self) -> None:
        for login in ("dependabot", "app/dependabot"):
            payload = {
                "author": {"login": login},
                "body": "Bumps example from 1.2.3 to 1.2.4.",
                "headRefName": "dependabot/go_modules/example-1.2.4",
                "isDraft": False,
                "state": "OPEN",
            }
            with tempfile.TemporaryDirectory() as tmp:
                path = Path(tmp) / "pr.json"
                path.write_text(json.dumps(payload), encoding="utf-8")
                old_argv = MODULE.sys.argv
                try:
                    MODULE.sys.argv = [str(SCRIPT), str(path)]
                    self.assertEqual(MODULE.main(), 0)
                finally:
                    MODULE.sys.argv = old_argv


if __name__ == "__main__":
    unittest.main()
