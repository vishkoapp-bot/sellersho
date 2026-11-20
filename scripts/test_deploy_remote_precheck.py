#!/usr/bin/env python3
"""Unit tests for the MIG-02 rollback pre-check schema-change decision
(scripts/deploy_remote.py).

Covers supervisor fix item 1 (Nov 20, 2026): `migrationsApplied` in
the rollback point is only set after a *successful* migrate run, so a
mid-migrate failure left it empty and the schema was misread as
unchanged, bypassing the lastCompatibleRevision floor check. The fix
compares the fresh applied set against the pre-deploy snapshot
`backup/applied-before.json` (see `compute_schema_changed`).

The deploy script cannot be imported — module import runs the deploy
— so the helpers are extracted from its source by content (the same
extract-by-content pattern as the OBS-01 F-4 dry run and the MIG-02
evidence helper tests) and exec'd in an isolated namespace. If the
extraction fails, every test here fails loudly instead of silently
passing against a stale copy.

Run from the repo root:  python3 scripts/test_deploy_remote_precheck.py
"""
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
DEPLOY_REMOTE = REPO / "scripts/deploy_remote.py"
REPO_GIT = Path.home() / "workspace/sellersho-git"

# Real SHAs: the floor check uses git merge-base, which fails closed
# on fake SHAs.
LCR = "ff573673179777202f21b6a647c499fa49a6b132"
OLD_POST_FLOOR = "92f9cca8d2d0bb94079cf0f93d91034f7ac459b8"
OLD_PRE_FLOOR = "2df713ba118cbb151405300583667086549b3ec9"


def extract_precheck_namespace():
    """Exec the MIG-02 helper block from deploy_remote.py."""
    src = DEPLOY_REMOTE.read_text()
    start = src.index("# MIG-02 (steps 2/3/4/5)")
    end = src.index("previous_edge = run(")
    ns = {"Path": Path, "hashlib": hashlib, "json": json, "os": os,
          "shutil": shutil, "subprocess": subprocess, "sys": sys,
          "tempfile": tempfile, "time": __import__("time"),
          "run": lambda *a, **k: (_ for _ in ()).throw(
              RuntimeError("run() stub called")),
          "inspect": lambda *a, **k: (_ for _ in ()).throw(
              RuntimeError("inspect() stub called"))}
    exec(compile(src[start:end], "deploy_remote_mig02_block", "exec"), ns)
    for name in ("compute_schema_changed", "rollback_precheck",
                 "normalized_sha256"):
        assert name in ns, f"missing helper {name} in deploy_remote.py"
    return ns


NS = extract_precheck_namespace()
compute_schema_changed = NS["compute_schema_changed"]
rollback_precheck = NS["rollback_precheck"]


class ComputeSchemaChangedTest(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp(prefix="mig02-precheck-"))
        self.backup = self.tmp / "backup"
        self.backup.mkdir()

    def tearDown(self):
        shutil.rmtree(self.tmp, ignore_errors=True)

    def write_snapshot(self, versions):
        (self.backup / "applied-before.json").write_text(
            json.dumps({"versions": sorted(versions),
                        "checksums": {v: "sha-" + v for v in versions}},
                       indent=2, sort_keys=True) + "\n")

    def test_snapshot_equal_means_no_change(self):
        # Migrate failed on the first file before committing anything.
        self.write_snapshot(["000001_a.up.sql", "000002_b.up.sql"])
        applied = {"000001_a.up.sql": "c1", "000002_b.up.sql": "c2"}
        self.assertFalse(compute_schema_changed(self.backup, applied, []))

    def test_snapshot_differs_means_changed(self):
        # Mid-migrate failure: the breaking file committed, the next
        # one failed — migrationsApplied stays [] (only set after a
        # *successful* migrate), but the snapshot comparison sees it.
        self.write_snapshot(["000001_a.up.sql"])
        applied = {"000001_a.up.sql": "c1",
                   "202611210001_mig02_r7_legacy_break.up.sql": "cB"}
        self.assertTrue(compute_schema_changed(self.backup, applied, []))

    def test_snapshot_differs_on_removed_version(self):
        self.write_snapshot(["000001_a.up.sql", "000002_b.up.sql"])
        applied = {"000001_a.up.sql": "c1"}
        self.assertTrue(compute_schema_changed(self.backup, applied, []))

    def test_snapshot_wins_over_stale_migrations_applied(self):
        # Sets are equal, so the schema did not change even if the
        # rollback-point list is stale/non-empty.
        self.write_snapshot(["000001_a.up.sql"])
        applied = {"000001_a.up.sql": "c1"}
        self.assertFalse(
            compute_schema_changed(self.backup, applied, ["000001_a.up.sql"]))

    def test_missing_snapshot_falls_back_to_migrations_applied(self):
        applied = {"000001_a.up.sql": "c1"}
        self.assertFalse(compute_schema_changed(self.backup, applied, []))
        self.assertTrue(
            compute_schema_changed(self.backup, applied, ["000001_a.up.sql"]))

    def test_corrupt_snapshot_falls_back(self):
        (self.backup / "applied-before.json").write_text("{not json")
        applied = {"000001_a.up.sql": "c1"}
        self.assertFalse(compute_schema_changed(self.backup, applied, []))
        self.assertTrue(
            compute_schema_changed(self.backup, applied, ["000001_a.up.sql"]))


class RollbackPrecheckFloorTest(unittest.TestCase):
    """The floor check must fire exactly when the schema changed."""

    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp(prefix="mig02-precheck-"))
        self.old_release = self.tmp / "old-release"
        (self.old_release / "backend/migrations").mkdir(parents=True)
        self.persist = self.tmp / "persist"
        self.persist.mkdir()
        self.new_release = self.tmp / "new-release"
        (self.new_release / "backend/migrations").mkdir(parents=True)
        self.body = b"SELECT 1;\n"
        self.sha = hashlib.sha256(self.body).hexdigest()
        for name in ("000001_a.up.sql", "000002_b.up.sql"):
            (self.old_release / "backend/migrations" / name).write_bytes(self.body)
            (self.persist / name).write_bytes(self.body)
            (self.new_release / "backend/migrations" / name).write_bytes(self.body)
        self.manifest_sums = {n: self.sha for n in
                              ("000001_a.up.sql", "000002_b.up.sql")}
        self.applied = {"000001_a.up.sql": self.sha,
                        "000002_b.up.sql": self.sha}
        self.old_set = {"000001_a.up.sql", "000002_b.up.sql"}

    def tearDown(self):
        shutil.rmtree(self.tmp, ignore_errors=True)

    def precheck(self, old_commit, schema_changed, applied=None):
        return rollback_precheck(
            applied if applied is not None else self.applied,
            self.old_set, {}, self.persist, self.new_release,
            self.old_release, self.manifest_sums, LCR, old_commit,
            REPO_GIT, schema_changed=schema_changed)

    def test_prefloor_old_with_schema_changed_is_incompatible(self):
        v = self.precheck(OLD_PRE_FLOOR, True)
        self.assertEqual(v["verdict"], "incompatible")
        self.assertIn("lastCompatibleRevision", v["reason"])
        self.assertFalse(v["old_ge_floor"])

    def test_prefloor_old_with_no_schema_change_is_compatible(self):
        # The failed deploy applied nothing: the live schema is the
        # one the old binary was already running, so the floor check
        # does not apply (runbook §1 NOTE).
        v = self.precheck(OLD_PRE_FLOOR, False)
        self.assertEqual(v["verdict"], "compatible")

    def test_postfloor_old_with_schema_changed_is_compatible(self):
        v = self.precheck(OLD_POST_FLOOR, True)
        self.assertEqual(v["verdict"], "compatible")

    def test_floor_checked_before_mode_a(self):
        # Even with a stranded migration fileable from the release,
        # a pre-floor old binary + changed schema is incompatible —
        # the floor verdict must win over mode-a.
        stranded_body = b"SELECT 3;\n"
        stranded_sha = hashlib.sha256(stranded_body).hexdigest()
        (self.new_release / "backend/migrations" / "000003_c.up.sql"
         ).write_bytes(stranded_body)
        applied = dict(self.applied)
        applied["000003_c.up.sql"] = stranded_sha
        v = self.precheck(OLD_PRE_FLOOR, True, applied=applied)
        self.assertEqual(v["verdict"], "incompatible")
        self.assertIn("lastCompatibleRevision", v["reason"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
