#!/usr/bin/env python3
"""REL-01 (G9) — contract (routes) hash.

Extracts every mux.Handle/mux.HandleFunc string-literal pattern from
backend/cmd/server/main.go (the same extraction
TestRouteTableNoConflicts uses), sorts them, and prints the sha256 of
the newline-joined list. That hash IS the API contract version:

  * make_release.sh injects it into the backend binary via
    -ldflags "-X main.ReleaseContract=<hash>", so /api/version
    reports it at runtime;
  * make_release.sh writes it into the staged frontend's
    dist/contract.json, so the frontend records the contract it was
    built against;
  * validate_release.py and smoke_release.py compare the two and fail
    the release on any mismatch — a frontend/backend pair whose
    contract versions differ is never switched live.

Usage:
    python3 contract_routes.py [--main backend/cmd/server/main.go]
    python3 contract_routes.py --count   # also print the pattern count
"""
import hashlib
import re
import sys
from pathlib import Path

CALL_RE = re.compile(r"mux\.Handle(?:Func)?\(")
LIT_RE = re.compile(r"mux\.Handle(?:Func)?\(\s*\"([^\"]+)\"")


def extract_patterns(main_go: Path) -> list[str]:
    src = main_go.read_text(encoding="utf-8", errors="replace")
    calls = CALL_RE.findall(src)
    matches = LIT_RE.findall(src)
    if len(calls) != len(matches):
        raise SystemExit(
            f"route extraction drift: {main_go} has {len(calls)} mux.Handle* calls "
            f"but only {len(matches)} take a string-literal pattern; "
            "extend this extractor before trusting it"
        )
    if len(matches) < 200:
        raise SystemExit(
            f"route extraction sanity: only {len(matches)} patterns found; "
            "the real table has 270+, so extraction is broken"
        )
    return sorted(set(matches))


def routes_hash(patterns: list[str]) -> str:
    return hashlib.sha256("\n".join(patterns).encode("utf-8")).hexdigest()


def main() -> int:
    args = sys.argv[1:]
    show_count = "--count" in args
    args = [a for a in args if a != "--count"]
    main_go = Path(args[0]) if args else Path("backend/cmd/server/main.go")
    if not main_go.is_file():
        # Allow running from the repo root or from scripts/release/.
        alt = Path(__file__).resolve().parents[2] / "backend/cmd/server/main.go"
        main_go = alt
    patterns = extract_patterns(main_go)
    print(routes_hash(patterns))
    if show_count:
        print(f"patterns: {len(patterns)}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
