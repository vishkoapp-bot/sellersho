#!/usr/bin/env python3
"""MIG-02 step 2 — append-only rollback-points ledger.

Maintains ``<install-root>/.rollback-points.json``: one record per
deploy (and per rollback / forward repair), so any later rollback can
tell which binaries are still safe to restore and which points are
unavailable — instead of guessing from a bare ``.deployed-commit``.

Record shape (fields after ``status`` are optional and only present
when they apply)::

    {"commit": "<40-hex sha of the release>",
     "lastCompatibleRevision": "<40-hex floor from its manifest>",
     "deployedAt": "<UTC ISO timestamp>",
     "migrationsApplied": ["<file>.up.sql", ...],
     "status": "deployed" | "rolled-back" | "forward-repaired"
               | "rolled-back-probe-failed" | "rollback-failed"
               | "unavailable",
     "reason": "<why this record exists (rollback / unavailable)>",
     "restoredCommit": "<40-hex sha restored by a rollback>",
     "filesCopied": ["<file>.up.sql", ...],
     "filesRemoved": ["<file>.up.sql", ...],
     "compatibility": {"verdict": "compatible" | "mode-a"
                       | "incompatible" | "unavailable", ...}}

The ledger is append-only: records are never edited in place, so a
failed deploy that rolled back leaves both the failed attempt and
the restore visible. ``deploy_remote.py`` appends on every terminal
state (deployed, rolled-back, forward-repaired); the rehearsal
(step 6, R-5) records pre-``ff57367`` points as ``unavailable``
with their reasons.

Usage:
    python3 scripts/rollback_points.py record --root /opt/drup \\
        --record-json <backup-dir>/rollback-point.json
    python3 scripts/rollback_points.py list --root /opt/drup [--last N]
    python3 scripts/rollback_points.py record-unavailable --root /opt/drup \\
        --commit <sha> [--last-compatible <sha>] --reason <text>

No secrets are read or printed. No prod contact is made by the
``record`` path beyond the ledger file itself.
"""
import argparse
import fcntl
import json
import re
import sys
import time
from pathlib import Path

SHA_RE = re.compile(r"[a-f0-9]{40}")
STATUSES = ("deployed", "rolled-back", "forward-repaired",
            "rolled-back-probe-failed", "rollback-failed", "unavailable")


def ledger_path(root) -> Path:
    return Path(root) / ".rollback-points.json"


def load_records(root) -> list:
    path = ledger_path(root)
    if not path.is_file():
        return []
    try:
        data = json.loads(path.read_text())
    except Exception as exc:
        raise RuntimeError(f"rollback-points ledger is not valid JSON: {path}: {exc}") from exc
    if not isinstance(data, list):
        raise RuntimeError(f"rollback-points ledger is not a list: {path}")
    return data


def append_record(root, record: dict) -> dict:
    """Validate and append one record to the ledger (atomic replace).

    Returns the record as stored (with deployedAt filled in when the
    caller did not set it). Raises ValueError on a malformed record
    instead of writing a guess.
    """
    commit = record.get("commit", "")
    if not SHA_RE.fullmatch(commit or ""):
        raise ValueError(f"rollback record needs a 40-hex commit, got {commit!r}")
    lcr = record.get("lastCompatibleRevision", "")
    if lcr and not SHA_RE.fullmatch(lcr):
        raise ValueError(f"rollback record lastCompatibleRevision is not a 40-hex sha: {lcr!r}")
    if record.get("status") not in STATUSES:
        raise ValueError(f"rollback record status must be one of {STATUSES}, got {record.get('status')!r}")
    if not isinstance(record.get("migrationsApplied", []), list):
        raise ValueError("rollback record migrationsApplied must be a list")
    stored = dict(record)
    stored.setdefault("deployedAt", time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()))
    path = ledger_path(root)
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_name(path.name + ".tmp")
    with open(tmp, "w", encoding="utf-8") as handle:
        fcntl.flock(handle, fcntl.LOCK_EX)
        records = load_records(root)
        records.append(stored)
        handle.write(json.dumps(records, indent=2) + "\n")
        handle.flush()
    tmp.replace(path)
    return stored


def record_unavailable(root, commit: str, last_compatible: str, reason: str) -> dict:
    """Record a rollback point that cannot be restored (R-5).

    Used for pre-floor binaries (e.g. anything before ``ff57367``)
    and for any deploy whose migration run left state the rollback
    path cannot repair: the point stays visible with its reason
    instead of silently vanishing from the ledger.
    """
    if not reason:
        raise ValueError("record_unavailable needs a reason")
    return append_record(root, {
        "commit": commit,
        "lastCompatibleRevision": last_compatible or "",
        "migrationsApplied": [],
        "status": "unavailable",
        "reason": reason,
    })


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    sub = ap.add_subparsers(dest="command", required=True)
    rec = sub.add_parser("record", help="append a rollback-point.json record to the ledger")
    rec.add_argument("--root", required=True)
    rec.add_argument("--record-json", required=True)
    lst = sub.add_parser("list", help="print the ledger (newest last)")
    lst.add_argument("--root", required=True)
    lst.add_argument("--last", type=int, default=0)
    una = sub.add_parser("record-unavailable", help="record a rollback point as unavailable (R-5)")
    una.add_argument("--root", required=True)
    una.add_argument("--commit", required=True)
    una.add_argument("--last-compatible", default="")
    una.add_argument("--reason", required=True)
    args = ap.parse_args()
    try:
        if args.command == "record":
            record = json.loads(Path(args.record_json).read_text())
            stored = append_record(args.root, record)
            print(f"recorded {stored['commit'][:12]} status={stored['status']} in {ledger_path(args.root)}")
        elif args.command == "list":
            records = load_records(args.root)
            if args.last:
                records = records[-args.last:]
            print(json.dumps(records, indent=2))
        elif args.command == "record-unavailable":
            stored = record_unavailable(args.root, args.commit, args.last_compatible, args.reason)
            print(f"recorded {stored['commit'][:12]} status=unavailable in {ledger_path(args.root)}")
    except (ValueError, RuntimeError, OSError) as exc:
        print(f"rollback_points: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
