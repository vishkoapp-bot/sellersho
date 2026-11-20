#!/usr/bin/env python3
"""MIG-02 step 1 — generate backend/migrations/MIGRATION_CLASSIFICATION.md
from backend/migrations/classification.json (the source of truth).

Usage: python3 scripts/release/generate_classification_md.py [--check]
  --check  exit non-zero if the .md on disk differs from the generated
           text (CI gate: the doc must never drift from the JSON).
"""
import argparse
import json
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
JSON_PATH = REPO / "backend/migrations/classification.json"
MD_PATH = REPO / "backend/migrations/MIGRATION_CLASSIFICATION.md"


def render(doc: dict) -> str:
    counts = doc["counts"]
    verdicts = doc["verdicts"]
    lines = []
    lines.append("# Migration classification — all 210 migrations")
    lines.append("")
    lines.append("> Generated from `backend/migrations/classification.json` — the JSON is the")
    lines.append("> source of truth. Regenerate with")
    lines.append("> `python3 scripts/release/generate_classification_md.py` after editing the")
    lines.append("> JSON (via `scripts/release/build_classification.py`). Do not hand-edit")
    lines.append("> this file. `validate_release.py` fails a release when any `*.up.sql`")
    lines.append("> file is missing from the classification.")
    lines.append("")
    lines.append(f"Repo commit classified: `{doc['repo_commit']}`. Counts: "
                 f"EXPAND {counts['EXPAND']} / BACKFILL {counts['BACKFILL']} / "
                 f"CONTRACT {counts['CONTRACT']} / DESTRUCTIVE {counts['DESTRUCTIVE']} "
                 f"(total {counts['total']}). Verdicts: compatible {verdicts['compatible']} / "
                 f"tightening {verdicts['tightening']} / breaking {verdicts['breaking']}.")
    lines.append("")
    lines.append("## Verdict vocabulary")
    lines.append("")
    lines.append("- **compatible** — the old binary is unaffected (widening, new table,")
    lines.append("  new defaulted/nullable column, drop+recreate in-file).")
    lines.append("- **tightening** — an old-binary write can fail, but only when it produces")
    lines.append("  a value the new constraint rejects (duplicate key, out-of-range value,")
    lines.append("  narrowed CHECK value). Normal traffic is unaffected; the failure is")
    lines.append("  fail-closed. A tightening does NOT move the rollback floor.")
    lines.append("- **breaking** — an old binary's normal write path fails structurally")
    lines.append("  (arbiter removed, column renamed, trigger side-effect the old code")
    lines.append("  depends on removed). Moves the rollback floor to the introducing commit.")
    lines.append("")
    lines.append("## Rollback floor")
    lines.append("")
    lines.append(doc["floor_note"])
    lines.append("")
    lines.append("Breaking files: " + ", ".join(f"`{n}`" for n in doc["breaking_files"]) + ".")
    lines.append("")
    lines.append("## Runtimes and timeouts")
    lines.append("")
    lines.append(doc["runtime_note"])
    lines.append("")
    lines.append(doc["timeout_note"])
    lines.append("")
    lines.append("## Contract files (61) — per-file verdicts")
    lines.append("")
    lines.append("Each entry names the old-binary write that could violate the change, or")
    lines.append("states that none exists. CHECK replacements were compared old-set vs")
    lines.append("new-set (named CHECK in the file, or the unnamed CHECK in the CREATE TABLE")
    lines.append("that PostgreSQL auto-named `<table>_<column>_check`).")
    lines.append("")
    for e in doc["entries"]:
        if e["class"] != "CONTRACT":
            continue
        lines.append(f"### `{e['name']}` — {e['verdict']}")
        lines.append("")
        lines.append(f"Old-binary write: {e['old_binary_write']}")
        lines.append("")
        if e["notes"]:
            lines.append(e["notes"])
            lines.append("")
        if e["post_conditions"]:
            lines.append("Post-conditions:")
            lines.append("")
            for pc in e["post_conditions"]:
                comp = pc.get("comparator", "eq")
                lines.append(f"- `{pc['sql']}` → {comp} {pc['expected']}")
            lines.append("")
    lines.append("## Backfill files (53)")
    lines.append("")
    lines.append("Single full-table operation each (no chunking in any of the 210 files).")
    lines.append("`runtime_ms_disposable` is the measured empty-schema disposable number.")
    lines.append("Post-conditions run after `migrate` succeeds, only for migrations the")
    lines.append("deploy applied; a failure fails the deploy.")
    lines.append("")
    for e in doc["entries"]:
        if e["class"] != "BACKFILL":
            continue
        rt = e["runtime_ms_disposable"]
        rt_s = f"{rt} ms" if rt is not None else "unmeasured"
        lines.append(f"### `{e['name']}` — {rt_s}, tables: {', '.join(e['tables']) or '—'}")
        lines.append("")
        if e["notes"]:
            lines.append(e["notes"])
            lines.append("")
        for pc in e["post_conditions"]:
            comp = pc.get("comparator", "eq")
            lines.append(f"- `{pc['sql']}` → {comp} {pc['expected']}")
        if e["post_condition_status"] == "todo":
            lines.append(f"- TODO: {e.get('post_condition_note', '')}")
        lines.append("")
    lines.append("## Destructive file (1)")
    lines.append("")
    for e in doc["entries"]:
        if e["class"] != "DESTRUCTIVE":
            continue
        lines.append(f"### `{e['name']}` — {e['verdict']}")
        lines.append("")
        lines.append(e["old_binary_write"])
        lines.append("")
        lines.append(e["notes"])
        lines.append("")
    lines.append("## Expand files (95)")
    lines.append("")
    lines.append("Additive only; the old binary is unaffected. Listed by name — see the")
    lines.append("JSON for tables, operations and runtimes.")
    lines.append("")
    for e in doc["entries"]:
        if e["class"] == "EXPAND":
            lines.append(f"- `{e['name']}`")
    lines.append("")
    return "\n".join(lines)


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--check", action="store_true")
    args = ap.parse_args()
    doc = json.loads(JSON_PATH.read_text(encoding="utf-8"))
    text = render(doc)
    if args.check:
        if not MD_PATH.is_file() or MD_PATH.read_text(encoding="utf-8") != text:
            print(f"FAIL: {MD_PATH} differs from generated text; regenerate it", file=sys.stderr)
            return 1
        print(f"ok: {MD_PATH} matches classification.json")
        return 0
    MD_PATH.write_text(text, encoding="utf-8")
    print(f"wrote {MD_PATH} ({len(text)} bytes)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
