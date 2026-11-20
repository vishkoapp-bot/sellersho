#!/usr/bin/env python3
"""REL-01 (G2/G3/G9/G10) — validate a staged release before any switch.

Recomputes every checkable field of <stage>/release.json from the
staged files and fails loudly on any mismatch. This is the release
gate: make_release.sh runs it after building, the deploy scripts run
it before touching production, and CI runs it on every push.

Checks:
  G3  manifest exists, is valid JSON, carries all required fields,
      and commit matches --commit (or the stage directory name).
      Artifact checksums (backend binary, migrate binary, full dist
      tree) are recomputed and compared; extra/missing/changed files
      fail.
  G2  vite values in the manifest are empty (D4: no VITE_* variable
      is allowed in a release build) AND no VITE_* variable is set in
      the validator's own environment AND no .env* file is present
      anywhere in the stage AND the literal string VITE_USE_MOCK does
      not appear in the built dist (tripwire: Vite inlines the value,
      so the identifier must not survive into the bundle).
  G9  the contract hash recomputed from the staged main.go matches
      the manifest AND dist/contract.json.
  G10 docs/fa and backend/internal/networkreward are absent from the
      stage (both on disk and in the manifest's exclusion record).
  N1  node_modules is absent from the stage (make_release.sh removes
      it after the frontend build; a staged release must not carry
      231 MB of retained dependencies).
  Payment (D5): mode must be explicitly set; mock mode in a
      --production release fails (a placeholder provider must never
      ship to prod); real mode with PSP credentials present but
      FIN-010 verification not recorded fails; real mode with no PSP
      pair passes with a recorded warning (the documented live prod
      state — config.go: FIN-010 still blocked, VerifyTransaction
      fails closed at payment time).
  Migrations: the manifest's migration file list + hashes match the
      staged backend/migrations/*.up.sql set exactly.
  MIG-02 step 1: backend/migrations/classification.json covers every
      staged *.up.sql (sha256-verified) and the generated
      MIGRATION_CLASSIFICATION.md matches it; any unclassified file
      fails the release.
  MIG-02 step 2: manifest migrations.lastCompatibleRevision must be
      non-null and an ancestor of the release commit (checked via git
      when available; MIG02_REPO overrides the repo path).
  --db-url (optional): against a live (disposable or prod-read)
      database, enumerate pending migrations with the same
      classify_history rules deploy_remote.py uses, and fail on
      unreviewed historical pending (pre-20261109, outside the
      reviewed set). This needs only read access (SELECT version FROM
      schema_migrations); use `psql -Atc` when available, else skip
      with a warning and a non-zero exit only with --db-required.

Exit code 0 = release is valid to proceed to the smoke suite.
Any failure prints FAIL lines and exits non-zero.
"""
import argparse
import hashlib
import json
import os
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parents[1]
sys.path.insert(0, str(HERE))

REQUIRED_TOP = ("commit", "branch", "builtAt", "toolchain", "artifacts",
                "migrations", "config", "contract", "images", "exclusions")
REVIEWED_HISTORICAL = {"202611050001_customer_store_links.up.sql"}
HISTORICAL_CUTOFF = "202611090001_operations_automation.up.sql"


def sha256_file(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


class Failures:
    def __init__(self):
        self.items: list[str] = []
        self.warnings: list[str] = []

    def fail(self, msg: str):
        self.items.append(msg)
        print(f"FAIL: {msg}", flush=True)

    def warn(self, msg: str):
        self.warnings.append(msg)
        print(f"WARN: {msg}", flush=True)

    def ok(self, msg: str):
        print(f"ok: {msg}", flush=True)


def check_classification(mig_dir: Path, f: "Failures") -> None:
    """MIG-02 step 1 — the classification is the source of truth for what
    each migration does to the old binary. Fail the release when:
      * backend/migrations/classification.json is missing/unparseable,
      * any staged *.up.sql is unclassified (or vice versa),
      * a classified sha256 differs from the staged file,
      * an entry lacks a class/verdict, or the counts disagree.
    The generated MIGRATION_CLASSIFICATION.md must also match the JSON
    when both are present in the stage (drift check).
    """
    cpath = mig_dir / "classification.json"
    if not cpath.is_file():
        f.fail(f"missing migration classification: {cpath} (MIG-02 step 1)")
        return
    try:
        doc = json.loads(cpath.read_text())
    except Exception as exc:
        f.fail(f"migration classification is not valid JSON: {exc}")
        return
    staged = {p.name for p in mig_dir.glob("*.up.sql")}
    entries = doc.get("entries", [])
    classified = {e.get("name") for e in entries}
    missing = sorted(staged - classified)
    extra = sorted(classified - staged)
    if missing:
        f.fail(f"unclassified migrations (add them to classification.json): {missing}")
    if extra:
        f.fail(f"classification lists files not in the staged set: {extra}")
    bad_hash = []
    bad_entry = []
    for e in entries:
        name = e.get("name", "")
        if not e.get("class") or not e.get("verdict"):
            bad_entry.append(name)
            continue
        staged_file = mig_dir / name
        if staged_file.is_file() and e.get("sha256") and sha256_file(staged_file) != e["sha256"]:
            bad_hash.append(name)
    if bad_entry:
        f.fail(f"classification entries missing class/verdict: {bad_entry}")
    if bad_hash:
        f.fail(f"classification sha256 mismatch vs staged files: {bad_hash}")
    counts = doc.get("counts", {})
    if counts.get("total") is not None and counts["total"] != len(staged):
        f.fail(f"classification total {counts['total']} != staged migration count {len(staged)}")
    if not (missing or extra or bad_hash or bad_entry):
        f.ok(f"migration classification covers all {len(staged)} files "
             f"(compatible={doc.get('verdicts', {}).get('compatible')}, "
             f"tightening={doc.get('verdicts', {}).get('tightening')}, "
             f"breaking={doc.get('verdicts', {}).get('breaking')})")
    md_path = mig_dir / "MIGRATION_CLASSIFICATION.md"
    if md_path.is_file():
        try:
            sys.path.insert(0, str(HERE))
            from generate_classification_md import render
            if md_path.read_text() != render(doc):
                f.fail("MIGRATION_CLASSIFICATION.md drifted from classification.json; regenerate it")
            else:
                f.ok("MIGRATION_CLASSIFICATION.md matches classification.json")
        except ImportError:
            f.warn("generate_classification_md.py unavailable; .md drift check skipped")
    else:
        f.fail("missing generated MIGRATION_CLASSIFICATION.md (run generate_classification_md.py)")


def check_db_pending(stage: Path, db_url: str, f: Failures, required: bool):
    """Enumerate pending migrations against a database (read-only)."""
    sys.path.insert(0, str(REPO / "scripts"))
    try:
        from migration_history import classify_history
    except Exception as exc:  # pragma: no cover - import environment
        msg = f"cannot import migration_history: {exc}"
        (f.fail if required else f.warn)(msg)
        return
    psql = None
    for cand in ("psql", "/usr/bin/psql",
                 str(Path.home() / "pgx-rootfs/usr/lib/postgresql/16/bin/psql")):
        try:
            r = subprocess.run([cand, "--version"], capture_output=True,
                               timeout=10)
            if r.returncode == 0:
                psql = cand
                break
        except Exception:
            continue
    if psql is None:
        (f.fail if required else f.warn)(
            "psql not found; cannot enumerate pending migrations")
        return
    env = dict(os.environ, PGPASSWORD="", PGSSLMODE="disable")
    # DATABASE_URL form: postgres://user:pass@host:port/db
    r = subprocess.run(
        [psql, db_url, "-Atc", "SELECT version FROM schema_migrations"],
        capture_output=True, text=True, timeout=30, env=env)
    if r.returncode != 0:
        (f.fail if required else f.warn)(
            f"cannot read schema_migrations: {r.stderr.strip()[:200]}")
        return
    applied = set(r.stdout.split())
    canonical = {p.name for p in (stage / "backend/migrations").glob("*.up.sql")}
    aliases_path = stage / "backend/internal/migrations/legacy-aliases.json"
    aliases = json.loads(aliases_path.read_text()) if aliases_path.is_file() else {}
    try:
        pending, _ = classify_history(applied, canonical, aliases)
    except ValueError as exc:
        f.fail(f"migration history classification failed: {exc}")
        return
    print(f"ok: pending migrations against target DB: {len(pending)}", flush=True)
    for name in pending:
        print(f"  pending: {name}", flush=True)
    bad = [p for p in pending
           if p < HISTORICAL_CUTOFF and p not in REVIEWED_HISTORICAL]
    if bad:
        f.fail(f"unreviewed historical pending migrations: {bad}")


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--stage", required=True)
    ap.add_argument("--commit", default="")
    ap.add_argument("--production", action="store_true",
                    help="Validate as a production release (mock payment mode fails).")
    ap.add_argument("--db-url", default="",
                    help="Optional DATABASE_URL to enumerate pending migrations (read-only).")
    ap.add_argument("--db-required", action="store_true")
    args = ap.parse_args()

    stage = Path(args.stage)
    f = Failures()
    if not stage.is_dir():
        print(f"FAIL: stage not a directory: {stage}", file=sys.stderr)
        return 2

    manifest_path = stage / "release.json"
    if not manifest_path.is_file():
        print(f"FAIL: missing manifest: {manifest_path}", file=sys.stderr)
        return 2
    try:
        manifest = json.loads(manifest_path.read_text())
    except Exception as exc:
        print(f"FAIL: manifest is not valid JSON: {exc}", file=sys.stderr)
        return 2

    expected_commit = args.commit or stage.name
    if manifest.get("commit") != expected_commit:
        f.fail(f"manifest commit {manifest.get('commit')!r} != expected {expected_commit!r}")
    else:
        f.ok(f"manifest commit {expected_commit[:12]}")
    for field in REQUIRED_TOP:
        if field not in manifest:
            f.fail(f"manifest missing required field: {field}")
    if f.items:
        return 1

    # G3 — artifact checksums recomputed from the staged files.
    artifacts = manifest["artifacts"]
    for key, rel in (("server_linux_sha256", "backend/bin/server-linux"),
                     ("migrate_linux_sha256", "backend/bin/migrate-linux")):
        path = stage / rel
        if not path.is_file():
            f.fail(f"missing artifact: {rel}")
        elif sha256_file(path) != artifacts.get(key):
            f.fail(f"checksum mismatch: {rel}")
        else:
            f.ok(f"{rel} sha256 matches manifest")
    dist = stage / "dist"
    if not (dist / "index.html").is_file():
        f.fail("missing dist/index.html")
    tree = artifacts.get("dist_sha256_tree", {})
    actual: dict[str, str] = {}
    for path in sorted(dist.rglob("*")):
        if path.is_file():
            actual[str(path.relative_to(dist))] = sha256_file(path)
    if actual != tree:
        missing = sorted(set(tree) - set(actual))
        extra = sorted(set(actual) - set(tree))
        changed = sorted(k for k in set(tree) & set(actual)
                         if tree[k] != actual[k])
        if missing:
            f.fail(f"dist files missing vs manifest: {missing}")
        if extra:
            f.fail(f"dist files present but not in manifest: {extra}")
        if changed:
            f.fail(f"dist files differ from manifest: {changed}")
    else:
        f.ok(f"dist tree matches manifest ({len(actual)} files)")

    # G2 — VITE allow-list is EMPTY (D4).
    vite = manifest.get("config", {}).get("vite", {})
    if vite.get("allowed") not in ([], None) or vite.get("values"):
        f.fail(f"release manifest carries VITE values (allow-list is empty): {vite}")
    else:
        f.ok("manifest vite values empty (D4)")
    leaked = {k: v for k, v in os.environ.items() if k.startswith("VITE_")}
    if leaked:
        f.fail(f"VITE_* variables set in the validating environment: {sorted(leaked)}")
    else:
        f.ok("no VITE_* in validator environment")
    env_files = [str(p.relative_to(stage)) for p in stage.rglob(".env*")
                 if p.is_file() and p.name != ".env.example"]
    if env_files:
        f.fail(f"env files present in staged release (archive must contain none): {env_files}")
    else:
        f.ok("no .env* files in staged release")
    # N1: node_modules must not ship in the staged release
    # (make_release.sh removes it after the frontend build; it was
    # 231 MB of retained dependencies). is_symlink() is checked too:
    # Path.exists() is False for a dangling symlink.
    nm = stage / "node_modules"
    if nm.is_symlink() or nm.exists():
        kind = "symlink" if nm.is_symlink() else "directory"
        f.fail(f"node_modules present in staged release ({kind}) — "
               f"dependencies must not ship (N1)")
    else:
        f.ok("no node_modules in staged release (N1)")
    for path in dist.rglob("*.js"):
        if b"VITE_USE_MOCK" in path.read_bytes():
            f.fail(f"VITE_USE_MOCK literal survived into built bundle: {path.relative_to(stage)}")
            break
    else:
        f.ok("no VITE_USE_MOCK literal in built dist")

    # G10 — exclusions.
    for rel in ("docs/fa", "backend/internal/networkreward"):
        if (stage / rel).exists():
            f.fail(f"excluded path present in staged release: {rel}")
    excl = manifest.get("exclusions", {})
    if excl.get("docs_fa_present") or excl.get("networkreward_present"):
        f.fail(f"manifest exclusion record is not clean: {excl}")
    else:
        f.ok("exclusions clean (docs/fa, networkreward absent)")

    # G9 — contract hash.
    from contract_routes import extract_patterns, routes_hash
    try:
        recomputed = routes_hash(extract_patterns(stage / "backend/cmd/server/main.go"))
    except SystemExit as exc:
        f.fail(f"contract extraction failed: {exc}")
        recomputed = None
    if recomputed:
        if manifest.get("contract", {}).get("routesHash") != recomputed:
            f.fail("manifest contract.routesHash != recomputed routes hash")
        else:
            f.ok(f"contract routesHash matches ({recomputed[:12]})")
        cpath = dist / "contract.json"
        if not cpath.is_file():
            f.fail("missing dist/contract.json")
        else:
            try:
                cdoc = json.loads(cpath.read_text())
                if cdoc.get("routesHash") != recomputed:
                    f.fail("dist/contract.json routesHash != recomputed routes hash")
                elif cdoc.get("commit") != expected_commit:
                    f.fail("dist/contract.json commit != release commit")
                else:
                    f.ok("dist/contract.json matches manifest + source")
            except Exception as exc:
                f.fail(f"dist/contract.json unreadable: {exc}")

    # Payment mode (D5).
    payment = manifest.get("config", {}).get("payment", {})
    mode = payment.get("mode")
    if mode not in ("mock", "real"):
        f.fail(f"payment mode not explicitly set to mock|real: {mode!r}")
    elif mode == "mock" and args.production:
        f.fail("payment mode is mock in a production release — placeholder provider rejected")
    elif mode == "real" and payment.get("psp_configured") and not payment.get("psp_verified"):
        f.fail("payment mode real with PSP credentials present but FIN-010 verification not recorded")
    else:
        f.ok(f"payment mode {mode!r} (psp_configured={payment.get('psp_configured')}, "
             f"psp_verified={payment.get('psp_verified')})")
        if mode == "real" and not payment.get("psp_configured"):
            f.warn("real mode with no PSP pair (documented live state; "
                   "VerifyTransaction fails closed at payment time until FIN-010 lands)")

    # Migrations — manifest list must equal the staged set exactly.
    mig_dir = stage / "backend/migrations"
    staged_migs = {p.name: sha256_file(p) for p in sorted(mig_dir.glob("*.up.sql"))} \
        if mig_dir.is_dir() else {}
    manifest_migs = {m["name"]: m["sha256"]
                     for m in manifest.get("migrations", {}).get("files", [])}
    if staged_migs != manifest_migs:
        f.fail("manifest migration set != staged backend/migrations/*.up.sql set")
    else:
        f.ok(f"migration set matches manifest ({len(staged_migs)} files)")

    # MIG-02 step 1 — classification coverage (source of truth for what
    # each migration does to the old binary).
    if mig_dir.is_dir():
        check_classification(mig_dir, f)
    else:
        f.fail("missing staged migrations directory")

    # MIG-02 step 2 — lastCompatibleRevision must be stamped and must be
    # an ancestor of the release commit (fail-closed: a null floor means
    # no rollback target was proven compatible).
    lcr = manifest.get("migrations", {}).get("lastCompatibleRevision")
    if not lcr:
        f.fail("manifest migrations.lastCompatibleRevision is null — MIG-02 step 2 has not stamped the rollback floor")
    else:
        f.ok(f"lastCompatibleRevision stamped: {lcr[:12]}")
        repo = Path(os.environ.get("MIG02_REPO", str(REPO)))
        try:
            r = subprocess.run(["git", "-C", str(repo), "merge-base", "--is-ancestor", lcr, expected_commit],
                               capture_output=True, timeout=30)
            if r.returncode == 0:
                f.ok(f"lastCompatibleRevision {lcr[:12]} is an ancestor of {expected_commit[:12]}")
            else:
                f.fail(f"lastCompatibleRevision {lcr[:12]} is NOT an ancestor of the release commit {expected_commit[:12]}")
        except Exception as exc:
            f.warn(f"could not verify lastCompatibleRevision ancestry (git unavailable: {exc})")

    if args.db_url:
        check_db_pending(stage, args.db_url, f, args.db_required)

    if f.items:
        print(f"VALIDATE FAILED: {len(f.items)} failure(s)", file=sys.stderr)
        return 1
    print(f"VALIDATE OK: {stage} ({len(f.warnings)} warning(s))")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
