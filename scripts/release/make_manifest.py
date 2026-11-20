#!/usr/bin/env python3
"""REL-01 (G3) — build the extended release manifest.

Writes <stage>/release.json for a staged release. The manifest is the
single traceable record of what a release IS; validate_release.py
recomputes every checkable field from the staged files and fails the
release on any mismatch. No secret values are ever recorded — only
booleans (e.g. psp_configured) and hashes.

Fields (D3):
  commit, branch, builtAt, builtBy
  toolchain: {go, node, npm, pnpm} — exact versions captured at build
    time (canonical release toolchain: Go 1.27, Node 24; see
    scripts/release/README.md — G7).
  artifacts: {server_linux_sha256, migrate_linux_sha256,
    dist_sha256_tree: {relative path: sha256, ...}} (+ tarball sha256
    when --tarball is given).
  migrations: {files: [{name, sha256}], count,
    lastCompatibleRevision, timeouts, post_conditions} — the file set
    in this release; lastCompatibleRevision is the MIG-02 rollback
    floor, recomputed per release from the classification + git
    (D-2: currently ff57367); timeouts carries per-migration
    lock/statement budgets (D-5); post_conditions carries the
    backfill checks the deploy enforces.
  config: {vite: {allowed: [], values: {}},
    payment: {mode, bridge, psp_configured, psp_verified}}
    — vite values MUST be empty (D4: no VITE_* variable is allowed in
    a release build); payment mode is the explicitly-set effective
    mode (D5).
  contract: {routesHash, version} — G9 contract version.
  images: {<compose image>: {tag, digest}} — digest is
    "n/a: native build" where the release does not build/pull a
    container image (D3: recorded honestly, never invented). Runtime
    image digests are a REL-02-owned pin; this manifest records the
    tags the release was validated against.
  exclusions: {docs_fa_present, networkreward_present} — both must be
    false (G10).
  nodeModules: how the frontend dependencies were provided
    ("npm ci" or "symlink:<path>").

Usage:
    python3 make_manifest.py --stage <dir> --commit <sha> [--branch main]
        [--payment-mode mock|real] [--psp-configured] [--psp-verified]
        [--node-modules "npm ci"] [--tarball <file>]
        [--go-version "$(go version)"] [--node-version "$(node --version)"]
"""
import argparse
import hashlib
import json
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parents[1]


def sha256_file(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def run_capture(argv: list[str]) -> str:
    try:
        out = subprocess.run(argv, capture_output=True, text=True, timeout=30)
    except Exception:
        return ""
    return (out.stdout or "").strip()


def compose_images(stage: Path) -> dict:
    """Record the runtime image tags from the staged compose files.

    Digests are NOT resolved here: this environment has no Docker
    daemon (containers cannot run on this VM) and inventing a digest
    would be worse than recording its absence. REL-02 owns pinning
    these tags to digests; until then the manifest says so plainly.
    """
    images: dict[str, dict[str, str]] = {}
    for name in ("docker-compose.yml", "docker-compose.prod.yml",
                 "docker-compose.ssl.yml", "docker-compose.observability.yml"):
        path = stage / name
        if not path.is_file():
            continue
        for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
            stripped = line.strip()
            if stripped.startswith("image:"):
                tag = stripped.split("image:", 1)[1].strip().strip("\"'")
                if tag and tag not in images:
                    images[tag] = {
                        "tag": tag,
                        "digest": "n/a: native build — digest not resolved on this host (REL-02 owns image pinning)",
                    }
    return images


def compute_last_compatible(classification: dict, repo: Path,
                            release_commit: str) -> str:
    """MIG-02 step 2 — the rollback floor, recomputed per release.

    Takes the classification entries with moves_floor=true (the
    'breaking' verdicts) and resolves each file's introducing commit
    (the commit that ADDED the file) via git, then returns the newest
    of those commits by commit timestamp. The newest breaking
    migration's commit is the last app revision whose binary is
    compatible with this schema: any binary at or after it understands
    the schema; anything before it may not.

    The introducing-commit query is scoped to the RELEASE commit
    (``git log <release_commit> ...``), never to the repo's HEAD:
    the deploy host's checkout may sit on a branch whose history
    does not contain the release commit at all (c47a9d5 deploy: the
    server's HEAD was feature/dropshipper-discounts, which does not
    contain ff57367, so the unscoped query found no introducing
    commit for 202611110003 and the manifest gate failed closed).
    The release commit is REQUIRED: it must resolve to a commit in
    the repo, or this returns "" and the caller fails closed.

    The two pre-floor breakers (202610170009 trigger drop,
    202610250005 rename) resolve to the Nov 4 snapshot commit 2df713b,
    which is older than ff57367, so the current answer is ff57367
    (D-2). The DESTRUCTIVE file does not move the floor (0 rows, 0
    code references — see the classification). Returns "" on any
    failure so the caller fails closed instead of stamping a guess.
    """
    if not release_commit:
        print("release commit is required to compute lastCompatibleRevision "
              "(refusing to default to HEAD)", file=sys.stderr)
        return ""
    try:
        r = subprocess.run(["git", "-C", str(repo), "rev-parse", "--verify",
                            f"{release_commit}^{{commit}}"],
                           capture_output=True, text=True, timeout=30)
    except Exception as exc:
        print(f"git rev-parse failed for release commit {release_commit}: {exc}",
              file=sys.stderr)
        return ""
    resolved = (r.stdout or "").strip()
    if r.returncode != 0 or not resolved:
        print(f"release commit {release_commit} is not a commit in {repo}; "
              "refusing to compute lastCompatibleRevision", file=sys.stderr)
        return ""
    breaking = [e["name"] for e in classification.get("entries", [])
                if e.get("moves_floor")]
    if not breaking:
        print("classification marks no migration as floor-moving; refusing to stamp a floor", file=sys.stderr)
        return ""
    best_commit = ""
    best_time = -1
    for name in breaking:
        rel = f"backend/migrations/{name}"
        try:
            r = subprocess.run(["git", "-C", str(repo), "log", resolved,
                                "--diff-filter=A",
                                "--format=%H", "--", rel],
                               capture_output=True, text=True, timeout=30)
        except Exception as exc:
            print(f"git log failed for {rel}: {exc}", file=sys.stderr)
            return ""
        commits = [c for c in (r.stdout or "").splitlines() if c.strip()]
        if not commits:
            print(f"no introducing commit found for {rel} in the history of "
                  f"release commit {resolved[:12]} (repo: {repo})", file=sys.stderr)
            return ""
        introducing = commits[0]
        try:
            t = subprocess.run(["git", "-C", str(repo), "show", "-s",
                                "--format=%ct", introducing],
                               capture_output=True, text=True, timeout=30)
            when = int((t.stdout or "0").strip())
        except Exception as exc:
            print(f"git show failed for {introducing}: {exc}", file=sys.stderr)
            return ""
        print(f"floor candidate: {name} introduced by {introducing[:12]} ({when})")
        if when > best_time:
            best_time, best_commit = when, introducing
    return best_commit


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--stage", required=True)
    ap.add_argument("--commit", required=True)
    ap.add_argument("--branch", default="main")
    ap.add_argument("--payment-mode", choices=("mock", "real"), required=True,
                    help="Explicitly-set effective payment mode (D5).")
    ap.add_argument("--psp-configured", action="store_true",
                    help="Both PSP_TERMINAL_ID and PSP_API_KEY present (boolean only).")
    ap.add_argument("--psp-verified", action="store_true",
                    help="FIN-010: real PSP verified end-to-end (boolean only).")
    ap.add_argument("--node-modules", default="npm ci")
    ap.add_argument("--tarball", default="")
    ap.add_argument("--go-version", default="")
    ap.add_argument("--node-version", default="")
    ap.add_argument("--npm-version", default="")
    ap.add_argument("--pnpm-version", default="")
    ap.add_argument("--repo", default="",
                    help="Git repo to resolve introducing commits from "
                         "(default: the repo containing this script; "
                         "on the deploy host pass the install dir, e.g. "
                         "/opt/drup, because the staged release has no .git).")
    args = ap.parse_args()

    stage = Path(args.stage)
    if not stage.is_dir():
        print(f"stage not a directory: {stage}", file=sys.stderr)
        return 2

    server_bin = stage / "backend/bin/server-linux"
    migrate_bin = stage / "backend/bin/migrate-linux"
    dist = stage / "dist"
    if not server_bin.is_file():
        print(f"missing backend binary: {server_bin}", file=sys.stderr)
        return 2
    if not migrate_bin.is_file():
        print(f"missing migrate binary: {migrate_bin}", file=sys.stderr)
        return 2
    if not (dist / "index.html").is_file():
        print(f"missing frontend entrypoint: {dist}/index.html", file=sys.stderr)
        return 2

    # G10 exclusion assertions — fail the manifest build if either
    # excluded path is present in the staged archive.
    docs_fa = stage / "docs/fa"
    netrw = stage / "backend/internal/networkreward"
    if docs_fa.exists() or netrw.exists():
        print("EXCLUSION VIOLATION: staged release contains an excluded path:",
              file=sys.stderr)
        if docs_fa.exists():
            print(f"  present: {docs_fa}", file=sys.stderr)
        if netrw.exists():
            print(f"  present: {netrw}", file=sys.stderr)
        return 2

    dist_tree: dict[str, str] = {}
    for path in sorted(dist.rglob("*")):
        if path.is_file():
            dist_tree[str(path.relative_to(dist))] = sha256_file(path)

    migrations_dir = stage / "backend/migrations"
    mig_files = []
    if migrations_dir.is_dir():
        for path in sorted(migrations_dir.glob("*.up.sql")):
            mig_files.append({"name": path.name, "sha256": sha256_file(path)})

    # MIG-02 step 2 — lastCompatibleRevision, recomputed per release
    # (never hardcoded): the newest commit introducing a migration the
    # classification marks moves_floor (a 'breaking' verdict whose
    # introducing commit is not the Nov 4 snapshot). The classification
    # is required input — a manifest without it cannot stamp a floor.
    # Per-migration timeout budgets and post-conditions are copied from
    # the classification into the manifest (steps 5.1/5.3); the deploy
    # passes the timeouts to `migrate` and enforces the post-conditions
    # for the migrations it applies.
    classification_path = migrations_dir / "classification.json"
    if not classification_path.is_file():
        print(f"missing migration classification: {classification_path} (MIG-02 step 1)", file=sys.stderr)
        return 2
    classification = json.loads(classification_path.read_text())
    repo = Path(args.repo) if args.repo else REPO
    last_compatible = compute_last_compatible(classification, repo,
                                              args.commit)
    if not last_compatible:
        print("cannot compute lastCompatibleRevision (see above); refusing to write a manifest without a rollback floor", file=sys.stderr)
        return 2
    timeouts = {}
    post_conditions = {}
    for entry in classification.get("entries", []):
        if entry.get("timeout_lock_seconds") or entry.get("timeout_statement_seconds"):
            item = {}
            if entry.get("timeout_lock_seconds"):
                item["lock_timeout"] = f"{entry['timeout_lock_seconds']}s"
            if entry.get("timeout_statement_seconds"):
                item["statement_timeout"] = f"{entry['timeout_statement_seconds']}s"
            timeouts[entry["name"]] = item
        if entry.get("post_conditions"):
            post_conditions[entry["name"]] = entry["post_conditions"]

    sys.path.insert(0, str(HERE))
    from contract_routes import extract_patterns, routes_hash  # noqa: E402

    main_go = stage / "backend/cmd/server/main.go"
    routes = extract_patterns(main_go)
    contract_hash = routes_hash(routes)

    contract_path = dist / "contract.json"
    contract_doc = {
        "commit": args.commit,
        "contractVersion": contract_hash,
        "routesHash": contract_hash,
        "routes": len(routes),
    }
    contract_path.write_text(json.dumps(contract_doc, indent=2) + "\n",
                             encoding="utf-8")
    # contract.json was written after the tree walk: record it too.
    dist_tree["contract.json"] = sha256_file(contract_path)

    manifest = {
        "commit": args.commit,
        "branch": args.branch,
        "builtAt": datetime.now(timezone.utc).isoformat(),
        "builtBy": "scripts/release/make_release.sh (REL-01 canonical scripted path)",
        "toolchain": {
            "go": args.go_version or run_capture(["go", "version"]),
            "node": args.node_version or run_capture(["node", "--version"]),
            "npm": args.npm_version or run_capture(["npm", "--version"]),
            "pnpm": args.pnpm_version or run_capture(["pnpm", "--version"]),
            "canonical": "Go 1.27 / Node 24 (G7 — see scripts/release/README.md)",
        },
        "artifacts": {
            "server_linux_sha256": sha256_file(server_bin),
            "migrate_linux_sha256": sha256_file(migrate_bin),
            "dist_sha256_tree": dist_tree,
            "dist_file_count": len(dist_tree),
        },
        "migrations": {
            "files": mig_files,
            "count": len(mig_files),
            # MIG-02 step 2: the last app revision compatible with this
            # schema — the newest commit introducing a floor-moving
            # (breaking) migration, recomputed per release by
            # compute_last_compatible() from the classification + git.
            # D-2 confirms the current value is ff57367 (2026-11-11).
            "lastCompatibleRevision": last_compatible,
            # MIG-02 D-5: per-migration lock/statement budgets (only
            # files that declare a non-default budget appear here).
            "timeouts": timeouts,
            # MIG-02 step 5.3: post-conditions, enforced by the deploy
            # only for the migrations it applies.
            "post_conditions": post_conditions,
        },
        "config": {
            "vite": {"allowed": [], "values": {}},
            "payment": {
                "mode": args.payment_mode,
                "bridge": "manual-confirmation",
                "psp_configured": bool(args.psp_configured),
                "psp_verified": bool(args.psp_verified),
            },
        },
        "contract": contract_doc,
        "images": compose_images(stage),
        "exclusions": {
            "docs_fa_present": False,
            "networkreward_present": False,
        },
        "nodeModules": args.node_modules,
    }
    if args.tarball:
        tarball = Path(args.tarball)
        if tarball.is_file():
            manifest["artifacts"]["tarball_sha256"] = sha256_file(tarball)
            manifest["artifacts"]["tarball_name"] = tarball.name

    out = stage / "release.json"
    out.write_text(json.dumps(manifest, indent=2, ensure_ascii=False) + "\n",
                   encoding="utf-8")
    print(f"manifest written: {out} ({len(dist_tree)} dist files, "
          f"{len(mig_files)} migrations, contract {contract_hash[:12]})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
