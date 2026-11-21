#!/usr/bin/env bash
# FIN-014 release gate (W8).
#
# Evaluates one FIN-014 report JSON (produced by
# backend/cmd/fin014-report, or by GetFIN014Report) and decides
# whether the release may proceed.
#
# Usage:
#   scripts/fin014-release-gate.sh --report <report.json>
#       [--max-certain N] [--marker <path>]
#
#   --report      required: the report JSON to evaluate.
#   --max-certain the certain-defect threshold (owner decision Q5).
#                 Defaults to 0 (fail-closed). It may be raised only
#                 by an explicit parameter — never invent a lax
#                 allowance. Q5 may set a per-class allowance; that
#                 is recorded in the runbook, not in this script.
#   --marker      optional: path of the success marker. Written ONLY
#                 on pass (JSON: threshold, counts, report SHA-256).
#                 A stale marker is deleted at the start of every
#                 run, so a failure never leaves a passing marker
#                 behind.
#
# Exit code: 0 on pass (marker written, if requested); 1 on failure
# (no marker) or on a malformed/unusable report. The contract is the
# same as the BAK-01 restore checks: exit non-zero on failure, no
# success marker on failure.
set -euo pipefail

REPORT=""
MAX_CERTAIN=0
MARKER=""

while [ $# -gt 0 ]; do
  case "$1" in
    --report) REPORT="$2"; shift 2 ;;
    --max-certain) MAX_CERTAIN="$2"; shift 2 ;;
    --marker) MARKER="$2"; shift 2 ;;
    -h|--help)
      sed -n '2,30p' "$0"
      exit 0 ;;
    *) echo "fin014-release-gate: unknown argument: $1" >&2; exit 2 ;;
  esac
done

if [ -z "$REPORT" ]; then
  echo "fin014-release-gate: --report is required" >&2
  exit 2
fi
if [ ! -f "$REPORT" ]; then
  echo "fin014-release-gate: report not found: $REPORT" >&2
  exit 2
fi
case "$MAX_CERTAIN" in
  ''|*[!0-9]*) echo "fin014-release-gate: --max-certain must be a non-negative integer, got: $MAX_CERTAIN" >&2; exit 2 ;;
esac

# A stale marker must never survive a failing run.
if [ -n "$MARKER" ]; then
  rm -f "$MARKER"
fi

python3 - "$REPORT" "$MAX_CERTAIN" "$MARKER" <<'PYEOF'
import hashlib, json, sys

report_path, max_certain, marker = sys.argv[1], int(sys.argv[2]), sys.argv[3]
try:
    raw = open(report_path, 'rb').read()
    report = json.loads(raw)
except Exception as e:
    print(f"fin014-release-gate: report is not valid JSON: {e}", file=sys.stderr)
    sys.exit(1)

for field in ("certainCount", "ambiguousCount", "unresolvedCount", "totalRows", "fin011Pinned"):
    if field not in report:
        print(f"fin014-release-gate: report is missing required field: {field}", file=sys.stderr)
        sys.exit(1)

certain = report["certainCount"]
if not isinstance(certain, int) or certain < 0:
    print(f"fin014-release-gate: certainCount is not a non-negative integer: {certain!r}", file=sys.stderr)
    sys.exit(1)

print(f"fin014-release-gate: certain={certain} ambiguous={report['ambiguousCount']} "
      f"unresolved={report['unresolvedCount']} total={report['totalRows']} "
      f"fin011Pinned={report['fin011Pinned']} max-certain={max_certain}")
if certain > max_certain:
    print(f"fin014-release-gate: FAIL: {certain} certain defect(s) exceed the threshold {max_certain} "
          f"(Q5 default is 0, fail-closed). No success marker written.", file=sys.stderr)
    sys.exit(1)

if marker:
    digest = hashlib.sha256(raw).hexdigest()
    with open(marker, 'w') as f:
        json.dump({
            "gate": "fin014-release-gate",
            "result": "pass",
            "maxCertain": max_certain,
            "certainCount": certain,
            "ambiguousCount": report["ambiguousCount"],
            "unresolvedCount": report["unresolvedCount"],
            "totalRows": report["totalRows"],
            "fin011Pinned": report["fin011Pinned"],
            "reportSha256": digest,
        }, f, indent=2)
        f.write("\n")
    print(f"fin014-release-gate: PASS: success marker written to {marker}")
else:
    print("fin014-release-gate: PASS")
PYEOF
