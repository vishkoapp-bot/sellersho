"""MIG-02 manifest fix — release-commit scoping of the rollback floor.

The c47a9d5 deploy stopped fail-closed at the server-side manifest
gate: make_manifest.py resolved each migration's introducing commit
with `git log --diff-filter=A ...` against the repo's HEAD, not the
release commit. On the deploy host /opt/drup's HEAD is the
feature/dropshipper-discounts branch (2 commits), which does not
contain ff57367, so the query found no introducing commit for
202611110003_sku_inventory.up.sql and the manifest refused to stamp
a rollback floor (exit 2). Locally the gate passed only because the
local checkout's HEAD happened to BE c47a9d5.

These tests reproduce the server layout in a scratch clone — HEAD on
a 2-commit branch whose history contains neither the release commit
nor the floor commit — and prove the fixed code still computes
lastCompatibleRevision = ff57367 for the c47a9d5 release, while the
old unscoped query fails exactly the way the server did.

Run: python3 scripts/release/test_make_manifest_release_commit.py -v
"""
import json
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
sys.path.insert(0, str(HERE))

from make_manifest import compute_last_compatible  # noqa: E402


def git_checkout() -> Path:
    """The git checkout for this working copy.

    ~/workspace/sellersho itself has no .git (manifest pull); the
    checkout lives next to it at ~/workspace/sellersho-git. If this
    file ever runs from inside a real checkout, that wins.
    """
    if (ROOT / ".git").exists():
        return ROOT
    sibling = ROOT.parent / "sellersho-git"
    if (sibling / ".git").exists():
        return sibling
    raise RuntimeError(f"no git checkout found for {ROOT}")

RELEASE_COMMIT = "c47a9d5badb1bdfc3c1562fc5e507a177c0dd60c"
EXPECTED_FLOOR = "ff573673179777202f21b6a647c499fa49a6b132"
FLOOR_MIGRATION = "202611110003_sku_inventory.up.sql"


def sh(argv, cwd=None, check=True):
    return subprocess.run(argv, cwd=cwd, capture_output=True, text=True,
                          timeout=120, check=check)


class ReleaseCommitScoping(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tmp = Path(tempfile.mkdtemp(prefix="sellersho-manifest-fix-"))
        cls.repo = cls.tmp / "repo"
        # Scratch clone of the real checkout (local clone: objects are
        # hardlinked, so this is cheap). HEAD starts on c47a9d5.
        sh(["git", "clone", "--quiet", str(git_checkout()), str(cls.repo)])
        sh(["git", "-C", str(cls.repo), "config", "user.email",
            "test@example.invalid"])
        sh(["git", "-C", str(cls.repo), "config", "user.name",
            "manifest-fix-test"])
        # Reproduce the server layout: HEAD moves to a 2-commit branch
        # whose history contains neither the release commit nor the
        # floor commit (mirrors /opt/drup's feature/dropshipper-
        # discounts: initial snapshot + one follow-up commit).
        sh(["git", "-C", str(cls.repo), "checkout", "--orphan",
            "feature/dropshipper-discounts"])
        sh(["git", "-C", str(cls.repo), "rm", "-rf", "."])
        (cls.repo / "README.md").write_text("# server snapshot\n")
        sh(["git", "-C", str(cls.repo), "add", "README.md"])
        sh(["git", "-C", str(cls.repo), "commit", "--quiet", "-m",
            "initial snapshot"])
        (cls.repo / "notes.txt").write_text("follow-up\n")
        sh(["git", "-C", str(cls.repo), "add", "notes.txt"])
        sh(["git", "-C", str(cls.repo), "commit", "--quiet", "-m",
            "follow-up"])
        head = sh(["git", "-C", str(cls.repo), "rev-parse", "HEAD"]).stdout.strip()
        assert head != RELEASE_COMMIT, "scratch HEAD must not be the release commit"
        cls.classification = json.loads(
            sh(["git", "-C", str(cls.repo), "show",
                f"{RELEASE_COMMIT}:backend/migrations/classification.json"]
               ).stdout)

    @classmethod
    def tearDownClass(cls):
        shutil.rmtree(cls.tmp, ignore_errors=True)

    def test_floor_computed_with_head_not_release_commit(self):
        """The fixed query, scoped to the release commit, still finds
        ff57367 even though the repo HEAD is the server-like branch."""
        floor = compute_last_compatible(self.classification, self.repo,
                                        RELEASE_COMMIT)
        self.assertEqual(floor, EXPECTED_FLOOR)

    def test_old_unscoped_query_reproduces_server_failure(self):
        """Negative control: the OLD query (no release commit, HEAD
        only) finds no introducing commit for 202611110003 in this
        repo — exactly the server-side failure."""
        r = sh(["git", "-C", str(self.repo), "log", "--diff-filter=A",
                "--format=%H", "--",
                f"backend/migrations/{FLOOR_MIGRATION}"], check=False)
        self.assertEqual(r.stdout.strip(), "")
        # And the fixed, scoped query finds it in the same repo.
        r = sh(["git", "-C", str(self.repo), "log", RELEASE_COMMIT,
                "--diff-filter=A", "--format=%H", "--",
                f"backend/migrations/{FLOOR_MIGRATION}"], check=False)
        self.assertEqual(r.stdout.strip(), EXPECTED_FLOOR)

    def test_unknown_release_commit_fails_closed(self):
        """A release commit that is not in the repo must fail closed
        (empty floor), never fall back to HEAD."""
        floor = compute_last_compatible(
            self.classification, self.repo,
            "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
        self.assertEqual(floor, "")

    def test_empty_release_commit_fails_closed(self):
        self.assertEqual(
            compute_last_compatible(self.classification, self.repo, ""), "")

    def test_make_manifest_cli_end_to_end_with_mismatched_head(self):
        """Full make_manifest.py CLI run against a git-archive stage,
        with --repo pointing at the scratch repo whose HEAD is NOT
        the release commit: exit 0 and LCR == ff57367."""
        stage = self.tmp / "stage"
        stage.mkdir()
        proc = subprocess.run(
            ["git", "-C", str(self.repo), "archive", "--format=tar",
             RELEASE_COMMIT],
            capture_output=True, timeout=120, check=True)
        untar = subprocess.run(["tar", "-xf", "-", "-C", str(stage)],
                               input=proc.stdout, capture_output=True,
                               timeout=120)
        self.assertEqual(untar.returncode, 0, untar.stderr.decode())
        (stage / "backend/bin").mkdir(parents=True)
        (stage / "backend/bin/server-linux").write_bytes(b"fake-server")
        (stage / "backend/bin/migrate-linux").write_bytes(b"fake-migrate")
        (stage / "dist").mkdir()
        (stage / "dist/index.html").write_text("<html></html>\n")
        r = subprocess.run(
            [sys.executable, str(HERE / "make_manifest.py"),
             "--stage", str(stage), "--commit", RELEASE_COMMIT,
             "--branch", "main", "--payment-mode", "real",
             "--repo", str(self.repo)],
            capture_output=True, text=True, timeout=120)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        manifest = json.loads((stage / "release.json").read_text())
        self.assertEqual(manifest["commit"], RELEASE_COMMIT)
        self.assertEqual(manifest["migrations"]["lastCompatibleRevision"],
                         EXPECTED_FLOOR)


if __name__ == "__main__":
    unittest.main()
