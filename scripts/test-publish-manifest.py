"""Check CI's stale-build guard and digest update without committing or pushing."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

SOURCE_SHA = "a" * 40
IMAGE = "artifacts.r-und-t.app/herbsfest@sha256:" + "b" * 64
ROOT = Path(__file__).resolve().parent.parent

FAKE_GIT = r'''#!/usr/bin/env python3
import json, os, pathlib, sys
state_path = pathlib.Path(os.environ["FAKE_GIT_STATE"])
state = json.loads(state_path.read_text())
args = sys.argv[1:]
state["calls"].append(args)
scenario = os.environ["SCENARIO"]
result = 0
if args == ["rev-parse", "HEAD"]:
    print("c" * 40 if scenario == "wrong-head" else "a" * 40)
elif args == ["fetch", "origin", "main"]:
    state["fetches"] += 1
elif args == ["rev-parse", "origin/main"]:
    stale = scenario == "stale" or (scenario == "race" and state["fetches"] > 1)
    print("c" * 40 if stale else "a" * 40)
elif args == ["diff", "--cached", "--quiet"]:
    result = 0 if scenario == "no-change" else 1
elif args == ["push", "origin", "HEAD:main"]:
    result = 1 if scenario in ("race", "push-fails") else 0
elif args[0] not in ("config", "add", "commit", "diff"):
    raise SystemExit("Unexpected git call")
state_path.write_text(json.dumps(state))
raise SystemExit(result)
'''


class PublishingTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        shutil.copytree(ROOT / "config", self.root / "config")
        (self.root / "scripts").mkdir()
        shutil.copyfile(ROOT / "scripts/publish-manifest.sh", self.root / "scripts/publish-manifest.sh")
        bin_dir = self.root / "bin"
        bin_dir.mkdir()
        fake_git = bin_dir / "git"
        fake_git.write_text(FAKE_GIT)
        fake_git.chmod(0o755)
        self.state = self.root / "state.json"
        self.state.write_text(json.dumps({"calls": [], "fetches": 0}))
        self.env = dict(os.environ, PATH=f"{bin_dir}:{os.environ['PATH']}",
                        FAKE_GIT_STATE=str(self.state),
                        GITHUB_STEP_SUMMARY=str(self.root / "summary"))
        self.overlay = self.root / "config/overlays/prod/kustomization.yaml"
        self.before = self.overlay.read_text()

    def run_publish(self, scenario="success", sha=SOURCE_SHA, image=IMAGE):
        result = subprocess.run(["bash", str(self.root / "scripts/publish-manifest.sh"), sha, image],
                                env=dict(self.env, SCENARIO=scenario), capture_output=True, text=True)
        self.calls = json.loads(self.state.read_text())["calls"]
        return result

    def test_current_revision_updates_both_container_images(self):
        result = self.run_publish()
        self.assertEqual(result.returncode, 0, result.stderr)
        rendered = subprocess.check_output(["kustomize", "build", "config/overlays/prod"],
                                           cwd=self.root, text=True)
        self.assertEqual(rendered.count("image: " + IMAGE), 2)
        self.assertIn(["push", "origin", "HEAD:main"], self.calls)
        self.assertIn(IMAGE, (self.root / "summary").read_text())
        commit = next(args for args in self.calls if args[0] == "commit")
        self.assertIn("[skip ci]", commit[-1])

    def test_stale_revision_never_edits_or_commits(self):
        self.assertEqual(self.run_publish("stale").returncode, 0)
        self.assertEqual(self.overlay.read_text(), self.before)
        self.assertFalse(any(args[0] in ("commit", "push") for args in self.calls))

    def test_new_commit_winning_push_race_is_preserved(self):
        self.assertEqual(self.run_publish("race").returncode, 0)
        self.assertEqual(sum(args[0] == "push" for args in self.calls), 1)
        self.assertFalse(any(args[0] in ("rebase", "reset") for args in self.calls))

    def test_push_failure_fails_ci_after_retries(self):
        self.assertNotEqual(self.run_publish("push-fails").returncode, 0)
        self.assertEqual(sum(args[0] == "push" for args in self.calls), 5)

    def test_existing_digest_does_not_commit(self):
        self.assertEqual(self.run_publish("no-change").returncode, 0)
        self.assertFalse(any(args[0] in ("commit", "push") for args in self.calls))

    def test_rejects_invalid_or_mismatched_source(self):
        self.assertNotEqual(self.run_publish(sha="main").returncode, 0)
        self.assertEqual(self.calls, [])
        self.assertNotEqual(self.run_publish("wrong-head").returncode, 0)
        self.assertFalse(any(args[0] == "fetch" for args in self.calls))

    def test_rejects_unpinned_image(self):
        self.assertNotEqual(self.run_publish(image="artifacts.r-und-t.app/herbsfest:latest").returncode, 0)
        self.assertEqual(self.calls, [])


if __name__ == "__main__":
    unittest.main()
