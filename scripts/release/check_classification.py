#!/usr/bin/env python3
"""MIG-02 step 1 — standalone migration-classification check.

Validates backend/migrations/classification.json against the *.up.sql
set without needing a staged release (validate_release.py runs the
same check as part of release validation; this script is the
repo-local entry point for authors and CI).

Usage: python3 scripts/release/check_classification.py [--migrations-dir <dir>]
Exit 0 only when every *.up.sql file is classified, hashes match, and
the generated .md matches the JSON.
"""
import argparse
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parents[1]
sys.path.insert(0, str(HERE))

from validate_release import Failures, check_classification


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--migrations-dir", default=str(REPO / "backend/migrations"))
    args = ap.parse_args()
    f = Failures()
    check_classification(Path(args.migrations_dir), f)
    if f.items:
        print(f"CLASSIFICATION CHECK FAILED: {len(f.items)} failure(s)", file=sys.stderr)
        return 1
    print("CLASSIFICATION CHECK OK")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
