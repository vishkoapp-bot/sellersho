#!/usr/bin/env python3
"""MIG-02 step 6 — staging rehearsal R-1..R-6 (audit §6 step 6).

Runs the REAL scripts/deploy_remote.py end-to-end against a fake
/opt/drup root with a fake docker CLI (scripts/release/mig02_fakes.py),
disposable PostgreSQL databases and a scratch Redis. No prod contact,
no SSH, no real Docker, no OTP, no deploy to production.

Usage:
    python3 scripts/release/rehearse_mig02.py [--evidence-dir DIR]
        [--keep] [--scenarios R-1,R-2,R-3,R-4,R-5,R-6]

    --evidence-dir  where the per-scenario logs + JSON evidence land
                    (default ~/workspace/mig02-evidence)
    --keep          do not delete the fake root, DBs or scratch Redis
    --scenarios     comma-separated subset (default: all six)

What each scenario proves (evidence under the evidence dir):
    R-1  fail migration before commit: deploy aborts, no binary swap,
         no persistent-dir change, no schema_migrations row for the
         failed file, backup dir with the pre-deploy snapshot.
    R-2  fail migration after commit (Mode A): negative control with
         the pre-MIG-02 deploy_remote.py (92f9cca) — old binary fails
         boot with `applied migration ... missing from required
         directory` — then the real run: Mode A files the stranded
         migration sha-verified, old binary boots, health + probe pass.
    R-3  fail backend health after migration (Mode B): rollback
         refuses the pre-floor old binary, forward-repair release
         lands healthy without re-triggering the stranded file.
    R-4  overlap-window compatibility matrix (probe legacy/current
         x schema pre/post 202611110003) + write workload.
    R-5  unavailable rollback points recorded in .rollback-points.json.
    R-6  accepted data never erased: row counts + checksums of orders
         and ledger rows before/after a rollback.
    R-7  mid-migrate failure commits a breaking-shaped migration
         (supervisor fix item 1): the rollback pre-check must see the
         changed schema via backup/applied-before.json and fire the
         floor check (pre-floor old binary => incompatible => Mode B
         taken, old binary never restored). The repair itself cannot
         complete here — the failing migration is deterministic, so
         the new binary retries it at boot and fails closed; the
         honest terminal state is ROLLBACK NEEDS ATTENTION (runbook
         §4), not a blind restore.
"""
import argparse
import hashlib
import json
import os
import shutil
import signal
import subprocess
import sys
import time
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parents[1]
GIT_CHECKOUT = Path(os.environ.get("MIG02_GIT_CHECKOUT", str(Path.home() / "workspace/sellersho-git")))
FAKE_ROOT = Path("/opt/drup")
PG_BIN = Path.home() / "pgx-rootfs/usr/lib/postgresql/16/bin"
PSQL = str(PG_BIN / "psql")
PG_DUMP = str(PG_BIN / "pg_dump")
PG_RESTORE = str(PG_BIN / "pg_restore")
REDIS_PORT = 18771
GO = str(Path.home() / "go/bin/go")

# Files of this task, overlaid from the working copy onto the clean
# tree (git checkout at 92f9cca). The EDGE-01 211th migration
# (202611200003, uncommitted in the working copy) is deliberately
# NOT in this list: it belongs to EDGE-01, not MIG-02.
MY_FILES = [
    "backend/migrations/classification.json",
    "backend/migrations/MIGRATION_CLASSIFICATION.md",
    "backend/migrations/README.md",
    "scripts/release/build_classification.py",
    "scripts/release/generate_classification_md.py",
    "scripts/release/check_classification.py",
    "scripts/release/write_path_probe.py",
    "scripts/release/mig02_fakes.py",
    "scripts/release/rehearse_mig02.py",
    "scripts/rollback_points.py",
    "scripts/release/validate_release.py",
    "scripts/release/make_manifest.py",
    "backend/internal/migrations/runner.go",
    "backend/cmd/migrate/main.go",
    "backend/internal/migrations/runner_test.go",
    "scripts/deploy_remote.py",
    "deploy.ps1",
    "backend/internal/migrations/README.md",
    "scripts/backup/backup-entrypoint.sh",
    "scripts/backup/test-backup-entrypoint.sh",
    "docs/operations/backup-runbook.md",
    "docs/operations/rollback-runbook.md",
    "scripts/release/MIGRATION_INTERFACE.md",
    "scripts/deploy/deploy-backend.sh",
]

FAKE_COMMITS = {"R-1": "a" * 40, "R-2": "b" * 40, "R-3": "c" * 40,
                "R-6-ok": "d" * 40, "R-6-fail": "e" * 40, "R-7": "f" * 40}


def sh(argv, env=None, timeout=300, input_text=None, check=False, cwd=None):
    merged = dict(os.environ)
    if env:
        merged.update(env)
    result = subprocess.run(argv, capture_output=True, text=True,
                            timeout=timeout, env=merged, input=input_text, cwd=cwd)
    if check and result.returncode != 0:
        raise RuntimeError(f"command failed ({result.returncode}): {' '.join(argv)}\n"
                           f"stdout: {result.stdout[-2000:]}\nstderr: {result.stderr[-2000:]}")
    return result


def pg(db, sql, timeout=60):
    result = sh([PSQL, "-h", "127.0.0.1", "-p", "5433", "-U", "gap13",
                 "-d", db, "-v", "ON_ERROR_STOP=1", "-Atc", sql], timeout=timeout)
    if result.returncode != 0:
        raise RuntimeError(f"psql {db}: {sql[:120]} -> {result.stderr.strip()[:300]}")
    return result.stdout.strip()


def pg_file(db, sql_text, timeout=120):
    result = sh([PSQL, "-h", "127.0.0.1", "-p", "5433", "-U", "gap13",
                 "-d", db, "-v", "ON_ERROR_STOP=1"], timeout=timeout, input_text=sql_text)
    if result.returncode != 0:
        raise RuntimeError(f"psql file {db} -> {result.stderr.strip()[:500]}")
    return result.stdout


def create_db(name):
    sh([PSQL, "-h", "127.0.0.1", "-p", "5433", "-U", "gap13", "-d", "postgres",
        "-c", f"DROP DATABASE IF EXISTS {name}", "-c", f"CREATE DATABASE {name}"],
       timeout=60, check=True)


def drop_db(name):
    sh([PSQL, "-h", "127.0.0.1", "-p", "5433", "-U", "gap13", "-d", "postgres",
        "-c", f"DROP DATABASE IF EXISTS {name}"], timeout=60)


def db_url(name):
    return f"postgres://gap13@127.0.0.1:5433/{name}?sslmode=disable"


class Rehearsal:
    def __init__(self, evidence_dir, keep=False):
        self.evidence = Path(evidence_dir)
        self.evidence.mkdir(parents=True, exist_ok=True)
        self.keep = keep
        self.work = Path("/tmp/mig02-rehearsal")
        self.clean_tree = self.work / "clean-tree"
        self.server_bin = self.work / "server-linux"
        self.migrate_bin = self.work / "migrate-linux"
        self.state_dir = self.work / "state"
        self.container_root = self.work / "container"
        self.fakebin = self.work / "fakebin"
        self.vol_dir = self.work / "vol"
        self.results = {}
        self._redis_started = False
        self._obs_proc = None

    # --- setup ------------------------------------------------------
    def git_rev(self, rev):
        return sh(["git", "-C", str(GIT_CHECKOUT), "rev-parse", rev],
                  timeout=30, check=True).stdout.strip()

    def build_clean_tree(self):
        print("== setup: clean tree (git checkout + MIG-02 files) ==", flush=True)
        if self.clean_tree.exists():
            shutil.rmtree(self.clean_tree)
        self.clean_tree.mkdir(parents=True)
        for sub in ("backend", "scripts", "ops"):
            shutil.copytree(GIT_CHECKOUT / sub, self.clean_tree / sub)
        for name in ("Caddyfile", "docker-compose.yml", "docker-compose.prod.yml",
                     "docker-compose.ssl.yml", "docker-compose.observability.yml",
                     "docker-compose.metrics-ports.yml", "nginx.conf",
                     "backup-auto.sh", "backup-daily.sh"):
            shutil.copy2(GIT_CHECKOUT / name, self.clean_tree / name)
        for f in MY_FILES:
            src = REPO / f
            if not src.is_file():
                raise RuntimeError(f"MIG-02 file missing from working copy: {f}")
            dst = self.clean_tree / f
            dst.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(src, dst)
        n_mig = len(list((self.clean_tree / "backend/migrations").glob("*.up.sql")))
        assert n_mig == 210, f"clean tree must have 210 migrations, has {n_mig}"
        env = dict(os.environ, CGO_ENABLED="0", GOOS="linux", GOARCH="amd64")
        sh([GO, "build", "-buildvcs=false", "-trimpath", "-ldflags", "-s -w",
            "-o", str(self.server_bin), "./cmd/server"],
           env=env, timeout=300, check=True, cwd=str(self.clean_tree / "backend"))
        sh([GO, "build", "-buildvcs=false", "-trimpath", "-ldflags", "-s -w",
            "-o", str(self.migrate_bin), "./cmd/migrate"],
           env=env, timeout=300, check=True, cwd=str(self.clean_tree / "backend"))
        print(f"clean tree: 210 migrations, binaries built", flush=True)

    def start_infra(self):
        print("== setup: scratch Redis + obs stub ==", flush=True)
        self.state_dir.mkdir(parents=True, exist_ok=True)
        self.container_root.mkdir(parents=True, exist_ok=True)
        redis_shim = Path.home() / "workspace/.dev/redis-shim"
        env = dict(os.environ, PATH=str(redis_shim) + ":" + os.environ["PATH"])
        sh(["redis-server", "--port", str(REDIS_PORT), "--daemonize", "yes",
            "--save", "", "--appendonly", "no", "--protected-mode", "no",
            "--pidfile", str(self.state_dir / "redis.pid")],
           env=env, timeout=30, check=True)
        self._redis_started = True
        for _ in range(20):
            r = sh(["redis-cli", "-p", str(REDIS_PORT), "ping"], env=env, timeout=10)
            if r.stdout.strip() == "PONG":
                break
            time.sleep(0.5)
        self.fakebin.mkdir(parents=True, exist_ok=True)
        for mode in ("docker", "curl"):
            script = self.fakebin / mode
            script.write_text(f'#!/bin/bash\nexec python3 "{HERE / "mig02_fakes.py"}" {mode} "$@"\n')
            script.chmod(0o755)
        self._obs_proc = subprocess.Popen(
            [sys.executable, str(HERE / "mig02_fakes.py"), "obs-stub"],
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        for _ in range(30):
            try:
                import urllib.request
                with urllib.request.urlopen("http://127.0.0.1:9091/-/healthy", timeout=2) as resp:
                    if resp.status == 200:
                        break
            except Exception:
                time.sleep(0.5)
        print("infra up: redis :%d, obs stub :9091/:3000" % REDIS_PORT, flush=True)

    def init_fake_root(self):
        print("== setup: fake /opt/drup root ==", flush=True)
        if FAKE_ROOT.exists():
            if not (FAKE_ROOT / ".mig02-rehearsal").is_file():
                raise RuntimeError("/opt/drup exists without the rehearsal marker; refusing to clobber it")
            shutil.rmtree(FAKE_ROOT)
        FAKE_ROOT.mkdir(parents=True)
        (FAKE_ROOT / ".mig02-rehearsal").write_text("MIG-02 rehearsal fake root (disposable)\n")
        shutil.copytree(GIT_CHECKOUT / ".git", FAKE_ROOT / ".git")
        # MIG-02 manifest fix (Nov 20, 2026): make_manifest.py now
        # requires the release commit to resolve in --repo (fail-closed,
        # never HEAD). The rehearsal's fake commit IDs ("a"*40, ...)
        # are not git objects, so materialize one real commit per
        # scenario — parented on the copied HEAD — and use its hash as
        # the scenario's commit ID. The IDs stay opaque strings to the
        # scenarios (release dir names, manifest "commit", deploy arg);
        # only their values change from placeholder hex to real commit
        # hashes, so `git log <release>` resolves the floor exactly as
        # it did when the query ran against HEAD.
        head = sh(["git", "-C", str(FAKE_ROOT), "rev-parse", "HEAD"],
                  timeout=30, check=True).stdout.strip()
        tree = sh(["git", "-C", str(FAKE_ROOT), "rev-parse",
                   f"{head}^{{tree}}"], timeout=30, check=True).stdout.strip()
        for name in FAKE_COMMITS:
            FAKE_COMMITS[name] = sh(
                ["git", "-C", str(FAKE_ROOT), "commit-tree", tree,
                 "-p", head, "-m", f"MIG-02 rehearsal fake commit {name}"],
                env={"GIT_AUTHOR_NAME": "mig02-rehearsal",
                     "GIT_AUTHOR_EMAIL": "rehearsal@example.invalid",
                     "GIT_COMMITTER_NAME": "mig02-rehearsal",
                     "GIT_COMMITTER_EMAIL": "rehearsal@example.invalid"},
                timeout=30, check=True).stdout.strip()
        (self.vol_dir / "drup_caddy_data").mkdir(parents=True, exist_ok=True)
        (self.vol_dir / "drup_caddy_config").mkdir(parents=True, exist_ok=True)
        (self.vol_dir / "drup_caddy_data" / "acme.json").write_text("{}")
        (self.vol_dir / "drup_caddy_config" / "caddy.json").write_text("{}")
        key = FAKE_ROOT / ".backup-encryption-key"
        key.write_bytes(os.urandom(48))
        key.chmod(0o600)
        # The backup entry point fails loudly without an uploads dir.
        uploads = FAKE_ROOT / "static/uploads"
        uploads.mkdir(parents=True, exist_ok=True)
        (uploads / "mig02-fixture.txt").write_text("MIG-02 rehearsal fixture upload\n")
        print("fake root ready (git graph copied, test key generated)", flush=True)

    def runtime_env(self, db_name):
        """The backend environment a healthy deploy leaves behind (what
        the fake docker inspect returns and what the previous deploy's
        .env.deploy.runtime.json holds)."""
        return [
            f"DATABASE_URL={db_url(db_name)}",
            f"REDIS_URL=redis://127.0.0.1:{REDIS_PORT}/0",
            "APP_ENV=test",
            "PAYMENT_GATEWAY_MODE=mock",
            f"MIGRATIONS_DIR={FAKE_ROOT}/backend/migrations",
            "PORT=8080",
            "STARTUP_TIMEOUT_SECONDS=30",
            "SHUTDOWN_GRACE_SECONDS=1",
            "DOMAIN_EDGE_HOST=127.0.0.1",
            "TRUSTED_PROXY_CIDRS=172.18.0.0/16",
            "PLATFORM_DOMAINS=127.0.0.1",
            "PLATFORM_HTTP_HOST=127.0.0.1",
        ]

    def reset_fake_root_state(self, old_commit, db_name, old_migrations_dir=None):
        """Restore the pre-deploy fake-root state: old binary, old dist,
        old persistent migration dir, old .deployed-commit."""
        root = FAKE_ROOT
        for sub in ("backend/bin", "backend/migrations", "dist", "releases",
                    "ops/observability", "scripts", "scripts/backup", "backups"):
            path = root / sub
            if path.exists():
                shutil.rmtree(path) if path.is_dir() else path.unlink()
            path.mkdir(parents=True, exist_ok=True)
        shutil.copy2(self.server_bin, root / "backend/bin/server-linux")
        (root / "backend/bin/server-linux").chmod(0o755)
        for name in ("Caddyfile", "docker-compose.yml", "docker-compose.prod.yml",
                     "docker-compose.ssl.yml", "docker-compose.observability.yml",
                     "docker-compose.metrics-ports.yml", "nginx.conf"):
            shutil.copy2(self.clean_tree / name, root / name)
        shutil.copytree(self.clean_tree / "ops/observability", root / "ops/observability",
                        dirs_exist_ok=True)
        for name in ("backup-entrypoint.sh", "publish-backup-failure.sh",
                     "check-backup-health.sh", "restore-verify.sh"):
            shutil.copy2(self.clean_tree / "scripts/backup" / name, root / "scripts/backup" / name)
        for name in ("publish-backup-success.sh",):
            shutil.copy2(self.clean_tree / "scripts" / name, root / "scripts" / name)
        for name in ("backup-auto.sh", "backup-daily.sh"):
            shutil.copy2(self.clean_tree / name, root / name)
        # Persistent migration dir: the old release's set.
        src_mig = old_migrations_dir or (self.clean_tree / "backend/migrations")
        for path in Path(src_mig).glob("*.up.sql"):
            shutil.copy2(path, root / "backend/migrations" / path.name)
        # Old dist.
        (root / "dist" / "assets").mkdir(parents=True, exist_ok=True)
        (root / "dist" / "index.html").write_text("<html>old dist</html>\n")
        (root / "dist" / "assets" / "old.js").write_text("// old bundle\n")
        (root / ".deployed-commit").write_text(old_commit + "\n")
        # Old runtime file (what the previous deploy left behind): a
        # WORKING environment for the scenario DB — the rollback
        # restores it and then health-checks the old binary against it.
        # An empty env here makes the restored server unbootable.
        (root / ".env.deploy.runtime.json").write_text(json.dumps(
            {"services": {"backend": {"environment": dict(
                kv.split("=", 1) for kv in self.runtime_env(db_name))}}}) + "\n")
        # The previous deploy's staged release dir (what the rollback
        # pre-check enumerates for the old binary's migration set).
        # In prod this is the previous deploy's /opt/drup/releases/<sha>.
        self.stage_old_release(old_commit)

    # --- release staging ---------------------------------------------
    def test_classification(self, entries):
        doc = json.loads((self.clean_tree / "backend/migrations/classification.json").read_text())
        for entry in entries:
            doc["entries"].append(entry)
        doc["counts"]["total"] = len(doc["entries"])
        return doc

    @staticmethod
    def test_entry(name, sql, cls, verdict, post_conditions, notes=""):
        return {
            "name": name,
            "class": cls,
            "sha256": hashlib.sha256(sql.encode()).hexdigest(),
            "has_down_sql": False,
            "tables": [],
            "operations": [],
            "runtime_ms_disposable": None,
            "chunked": False,
            "timeout_lock_seconds": None,
            "timeout_statement_seconds": None,
            "post_conditions": post_conditions,
            "post_condition_status": "test",
            "verdict": verdict,
            "moves_floor": False,
            "old_binary_write": "Test-only migration for the MIG-02 rehearsal; not a production migration.",
            "notes": notes,
        }

    def stage_release(self, commit, extra_migrations, test_entries,
                      old_deploy_remote=False, manifest_commit=None):
        """Stage a release under /opt/drup/releases/<commit> and build
        its release.json with make_manifest.py (real manifest path)."""
        release = FAKE_ROOT / "releases" / commit
        if release.exists():
            shutil.rmtree(release)
        release.mkdir(parents=True)
        shutil.copytree(self.clean_tree / "backend", release / "backend")
        shutil.copytree(self.clean_tree / "scripts", release / "scripts")
        shutil.copytree(self.clean_tree / "ops", release / "ops")
        for name in ("Caddyfile", "docker-compose.yml", "docker-compose.prod.yml",
                     "docker-compose.ssl.yml", "docker-compose.observability.yml",
                     "docker-compose.metrics-ports.yml", "nginx.conf",
                     "backup-auto.sh", "backup-daily.sh"):
            shutil.copy2(self.clean_tree / name, release / name)
        (release / "dist" / "assets").mkdir(parents=True, exist_ok=True)
        (release / "dist" / "index.html").write_text("<html>new dist</html>\n")
        (release / "dist" / "assets" / "app.js").write_text("// new bundle\n")
        (release / "backend/bin").mkdir(parents=True, exist_ok=True)
        shutil.copy2(self.server_bin, release / "backend/bin/server-linux")
        shutil.copy2(self.migrate_bin, release / "backend/bin/migrate-linux")
        for name, sql in extra_migrations.items():
            (release / "backend/migrations" / name).write_text(sql)
        if test_entries:
            doc = self.test_classification(test_entries)
            (release / "backend/migrations" / "classification.json").write_text(
                json.dumps(doc, indent=1, ensure_ascii=False) + "\n")
            gen_path = str(self.clean_tree / "scripts/release")
            if gen_path not in sys.path:
                sys.path.insert(0, gen_path)
            import generate_classification_md as gen_mod
            (release / "backend/migrations" / "MIGRATION_CLASSIFICATION.md").write_text(
                gen_mod.render(doc), encoding="utf-8")
        if old_deploy_remote:
            # Negative control: stage the PRE-MIG-02 deploy_remote.py
            # from git history, never from the live checkout. Copying
            # the live file silently stages the NEW script once the
            # checkout moves past 92f9cca, and the control stops
            # controlling (supervisor re-run, Nov 20 2026). Fail loudly
            # when the blob is unavailable; the "MIG-02" guard proves
            # the staged blob really predates the change set.
            old_script = sh(["git", "-C", str(GIT_CHECKOUT), "show",
                             "92f9cca:scripts/deploy_remote.py"],
                            timeout=60, check=True).stdout
            assert "MIG-02" not in old_script, \
                "staged control script is not pre-MIG-02 (contains MIG-02)"
            (release / "scripts/deploy_remote.py").write_text(old_script)
        result = sh([sys.executable, str(self.clean_tree / "scripts/release/make_manifest.py"),
                     "--stage", str(release), "--commit", manifest_commit or commit,
                     "--branch", "main", "--payment-mode", "real",
                     "--repo", str(FAKE_ROOT)], timeout=120)
        if result.returncode != 0:
            raise RuntimeError(f"make_manifest failed for {commit[:12]}: {result.stdout[-1000:]}{result.stderr[-1000:]}")
        return release

    def stage_old_release(self, commit):
        """Minimal old release: only what the rollback pre-check reads
        (migration files + legacy aliases + a manifest)."""
        release = FAKE_ROOT / "releases" / commit
        if release.exists():
            shutil.rmtree(release)
        (release / "backend/migrations").mkdir(parents=True)
        (release / "backend/internal/migrations").mkdir(parents=True)
        for path in (self.clean_tree / "backend/migrations").glob("*.up.sql"):
            shutil.copy2(path, release / "backend/migrations" / path.name)
        shutil.copy2(self.clean_tree / "backend/internal/migrations/legacy-aliases.json",
                     release / "backend/internal/migrations/legacy-aliases.json")
        (release / "release.json").write_text(json.dumps(
            {"commit": commit, "branch": "main"}) + "\n")
        return release

    # --- deploy / probe / boot ---------------------------------------
    def deploy_env(self, db_name, extra=None):
        env = {
            "PATH": f"{self.fakebin}:{Path.home() / 'workspace/.dev/redis-shim'}:{Path.home() / 'go/bin'}:/usr/local/bin:/usr/bin:/bin",
            "MIG02_FAKE_ROOT": str(FAKE_ROOT),
            "MIG02_CONTAINER_ROOT": str(self.container_root),
            "MIG02_STATE_DIR": str(self.state_dir),
            "MIG02_PG_URL": db_url(db_name),
            "MIG02_PG_DB": db_name,
            "MIG02_PG_USER": "gap13",
            "MIG02_PGHOST": "127.0.0.1",
            "MIG02_PGPORT": "5433",
            "MIG02_PSQL_BIN": PSQL,
            "MIG02_BACKEND_ENV_JSON": str(self.state_dir / "backend-env.json"),
            "MIG02_VOL_DIR": str(self.vol_dir),
            "BAK_PG_DUMP_CMD": f"{PG_DUMP} -h 127.0.0.1 -p 5433 -U gap13 -d {db_name}",
            "BAK_PSQL_CMD": f"{PSQL} -h 127.0.0.1 -p 5433 -U gap13 -d {db_name} -v ON_ERROR_STOP=1 -At",
            "BAK_PG_RESTORE_LIST_CMD": f"{PG_RESTORE} --list",
            "BAK_DOCKER_BIN": str(self.fakebin / "docker"),
            "BAK_PUBLISHER": "/bin/true",
            "BAK_FAILURE_PUBLISHER": "/bin/true",
            "BAK_KEY_FILE": str(FAKE_ROOT / ".backup-encryption-key"),
        }
        if extra:
            env.update(extra)
        (self.state_dir / "backend-env.json").write_text(json.dumps(self.runtime_env(db_name)))
        return env

    def run_deploy(self, commit, db_name, log_path, extra_env=None):
        env = self.deploy_env(db_name, extra_env)
        script = FAKE_ROOT / "releases" / commit / "scripts/deploy_remote.py"
        result = sh([sys.executable, str(script), str(FAKE_ROOT), commit, "0", "0"],
                    env=env, timeout=900)
        Path(log_path).write_text(
            f"$ python3 {script} {FAKE_ROOT} {commit} 0 0\n"
            f"exit={result.returncode}\n--- stdout ---\n{result.stdout}\n--- stderr ---\n{result.stderr}\n")
        return result.returncode, result.stdout + result.stderr

    def run_probe(self, db_name, expect="current", checks="inventory,review,withdrawal"):
        result = sh([sys.executable, str(REPO / "scripts/release/write_path_probe.py"),
                     "--db-url", db_url(db_name), "--psql", PSQL,
                     "--expect", expect, "--checks", checks], timeout=180)
        return result.returncode, result.stdout + result.stderr

    def stop_fake_backend(self):
        """Kill the server the fake docker started (PID in the state
        dir). A deploy's rollback leaves it running; a later scenario's
        boot check would otherwise poll the LEFTOVER server on :8080
        instead of the server it started (false healthy / false
        failed)."""
        pid_file = self.state_dir / "backend.pid"
        if not pid_file.is_file():
            return
        try:
            pid = int(pid_file.read_text().strip())
        except ValueError:
            return
        try:
            os.kill(pid, signal.SIGTERM)
            for _ in range(20):
                time.sleep(0.25)
                try:
                    os.kill(pid, 0)
                except ProcessLookupError:
                    break
            else:
                os.kill(pid, signal.SIGKILL)
        except (ProcessLookupError, PermissionError):
            pass
        try:
            pid_file.unlink()
        except FileNotFoundError:
            pass

    def boot_check(self, db_name, migrations_dir, label, timeout=60):
        """Boot the real server binary against a DB + migrations dir;
        return (healthy, log)."""
        self.stop_fake_backend()
        # Belt-and-braces: refuse to measure a port another server
        # already holds (a leftover would make the poll meaningless).
        import socket
        probe_sock = socket.socket()
        try:
            if probe_sock.connect_ex(("127.0.0.1", 8080)) == 0:
                raise RuntimeError("port 8080 is already in use; refusing to boot-check against a leftover server")
        finally:
            probe_sock.close()
        env = dict(os.environ,
                   APP_ENV="test", DATABASE_URL=db_url(db_name),
                   REDIS_URL=f"redis://127.0.0.1:{REDIS_PORT}/0",
                   PAYMENT_GATEWAY_MODE="mock",
                   MIGRATIONS_DIR=str(migrations_dir),
                   STARTUP_TIMEOUT_SECONDS="30", SHUTDOWN_GRACE_SECONDS="1")
        log_path = self.evidence / f"boot-{label}.log"
        with open(log_path, "wb") as handle:
            proc = subprocess.Popen([str(self.server_bin)], env=env,
                                    stdout=handle, stderr=subprocess.STDOUT,
                                    start_new_session=True)
        healthy = False
        try:
            import urllib.request
            deadline = time.time() + timeout
            while time.time() < deadline:
                if proc.poll() is not None:
                    break
                try:
                    with urllib.request.urlopen("http://127.0.0.1:8080/api/health", timeout=3) as resp:
                        if resp.status == 200 and json.load(resp).get("status") == "ok":
                            healthy = True
                            break
                except Exception:
                    time.sleep(1)
        finally:
            if proc.poll() is None:
                proc.terminate()
                try:
                    proc.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    proc.kill()
        return healthy, log_path.read_text(errors="replace")

    def seed(self, db_name, negative_stock=False):
        sql = """
INSERT INTO suppliers (id, name) VALUES ('sup-fix-1', 'MIG-02 fixture supplier');
INSERT INTO products (id, title, slug, category_slug, supplier_id, original_price, supplier_base_cost, platform_margin, stock, image_url)
VALUES ('prod-fix-1', 'MIG-02 fixture product', 'mig02-fix-1', 'appliances', 'sup-fix-1', 1000000, 800000, 100000, 10, '/static/uploads/products/mig02-fix-1.jpg');
INSERT INTO users (id, mobile, full_name, role) VALUES ('11111111-1111-1111-1111-111111111111', '09110000001', 'MIG-02 fixture buyer', 'customer');
"""
        if negative_stock:
            sql += """
INSERT INTO products (id, title, slug, category_slug, supplier_id, original_price, supplier_base_cost, platform_margin, stock, image_url)
VALUES ('prod-fix-neg', 'MIG-02 negative-stock product', 'mig02-fix-neg', 'appliances', 'sup-fix-1', 500000, 400000, 50000, -5, '/static/uploads/products/mig02-fix-neg.jpg');
"""
        pg_file(db_name, sql)

    def apply_base(self, db_name, migrations_dir=None):
        env = dict(os.environ, DATABASE_URL=db_url(db_name))
        result = sh([str(self.migrate_bin), "-dir",
                     str(migrations_dir or (self.clean_tree / "backend/migrations"))],
                    env=env, timeout=300)
        if result.returncode != 0:
            raise RuntimeError(f"migrate failed for {db_name}: {result.stdout[-1000:]}{result.stderr[-1000:]}")

    def counts(self, db_name):
        orders = pg(db_name, "SELECT count(*) FROM orders")
        ledger = pg(db_name, "SELECT count(*) || '/' || COALESCE(sum(amount),0) FROM ledger_entries")
        migs = pg(db_name, "SELECT count(*) FROM schema_migrations")
        return {"orders": orders, "ledger": ledger, "migrations": migs}

    def write_evidence(self, name, data):
        path = self.evidence / name
        path.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n")
        return path


# --- test migrations ------------------------------------------------
M_FAIL_SQL = """-- Test-only migration for the MIG-02 rehearsal (R-1/R-2/R-6).
-- Adds a CHECK the fixture violates: the fixture product prod-fix-neg
-- has stock=-5, so this migration fails with a check_violation.
ALTER TABLE products ADD CONSTRAINT mig02_r1_stock_nonneg CHECK (stock >= 0);
"""

M_OK_SQL = """-- Test-only migration for the MIG-02 rehearsal (R-2/R-6).
-- Applies cleanly and leaves one row behind (post-condition count=1).
CREATE TABLE mig02_r2_scratch (id INT PRIMARY KEY, note TEXT NOT NULL DEFAULT '');
INSERT INTO mig02_r2_scratch (id, note) VALUES (1, 'mig02-r2');
"""

M_BREAK_SQL = """-- Test-only migration for the MIG-02 rehearsal (R-3).
-- Mirrors the 202611110003 pattern on a scratch table: creates a legacy
-- unique constraint, drops it, and replaces it with partial unique
-- indexes. An old binary whose upserts rely on the legacy arbiter
-- would fail against the resulting schema.
CREATE TABLE mig02_r3_balances (
  id TEXT PRIMARY KEY,
  product_id VARCHAR(50) NOT NULL,
  owner_type TEXT NOT NULL,
  location_type TEXT NOT NULL,
  location_key TEXT NOT NULL,
  status TEXT NOT NULL,
  variant_id UUID,
  quantity INT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX mig02_r3_legacy_unique ON mig02_r3_balances(product_id, owner_type, location_type, location_key, status);
DROP INDEX mig02_r3_legacy_unique;
CREATE UNIQUE INDEX mig02_r3_unallocated_unique ON mig02_r3_balances(product_id, owner_type, location_type, location_key, status) WHERE variant_id IS NULL;
CREATE UNIQUE INDEX mig02_r3_sku_unique ON mig02_r3_balances(product_id, owner_type, location_type, location_key, status, variant_id) WHERE variant_id IS NOT NULL;
"""

FAIL_ENTRY = Rehearsal.test_entry(
    "202611210002_mig02_r1_stock_check.up.sql", M_FAIL_SQL, "CONTRACT",
    "tightening",
    [{"sql": "SELECT count(*) FROM products WHERE stock < 0", "expected": 0}],
    "Applies a CHECK (stock >= 0) that the fixture's negative-stock product violates; must fail.")
OK_ENTRY = Rehearsal.test_entry(
    "202611210001_mig02_r2_scratch.up.sql", M_OK_SQL, "EXPAND",
    "compatible",
    [{"sql": "SELECT count(*) FROM mig02_r2_scratch", "expected": 1}])
BREAK_ENTRY = Rehearsal.test_entry(
    "202611210003_mig02_r3_legacy_break.up.sql", M_BREAK_SQL, "CONTRACT",
    "breaking",
    [{"sql": "SELECT count(*) FROM mig02_r3_balances", "expected": 0}],
    "Drops a legacy unique constraint and replaces it with partial unique indexes (202611110003 pattern); old-binary upserts on the legacy arbiter would fail.")

R7_BREAK_SQL = M_BREAK_SQL.replace("mig02_r3_balances", "mig02_r7_balances")

R7_BREAK_ENTRY = Rehearsal.test_entry(
    "202611210001_mig02_r7_legacy_break.up.sql", R7_BREAK_SQL, "CONTRACT",
    "breaking",
    [{"sql": "SELECT count(*) FROM mig02_r7_balances", "expected": 0}],
    "R-7 (supervisor fix item 1): breaking-shaped migration that the failing deploy COMMITS before the next file fails — the pre-deploy snapshot must not contain it, so the rollback pre-check must see a changed schema and fire the floor check instead of restoring the pre-floor binary.")

ACCEPTED_ORDERS_SQL = """
INSERT INTO stores (id, name, slug, owner_type, owner_name, total_sales, status)
VALUES ('store-fix-1', 'MIG-02 fixture store', 'mig02-fix', 'dropshipper', 'fixture-owner', 0, 'active');
INSERT INTO orders (id, order_number, store_id, store_name, total_amount, discount_amount, payable_amount, payment_method, customer_full_name, customer_phone, customer_province, customer_city, customer_address, customer_postal_code, status, tracking_code, shipping_cost, dropshipper_profit, platform_profit, referral_commission, profit_released)
VALUES ('11111111-2222-3333-4444-555555555555', 'ORD-MIG02-1', 'store-fix-1', 'MIG-02 fixture store', 1000000, 0, 1000000, 'cash', 'MIG-02 buyer', '09110000001', 'Qom', 'Qom', 'fixture address', '3711111111', 'delivered', 'TRK-1', 0, 100000, 50000, 0, false);
INSERT INTO orders (id, order_number, store_id, store_name, total_amount, discount_amount, payable_amount, payment_method, customer_full_name, customer_phone, customer_province, customer_city, customer_address, customer_postal_code, status, tracking_code, shipping_cost, dropshipper_profit, platform_profit, referral_commission, profit_released)
VALUES ('11111111-2222-3333-4444-555555555556', 'ORD-MIG02-2', 'store-fix-1', 'MIG-02 fixture store', 2000000, 0, 2000000, 'cash', 'MIG-02 buyer', '09110000001', 'Qom', 'Qom', 'fixture address', '3711111111', 'delivered', 'TRK-2', 0, 200000, 100000, 0, false);
INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price, product_title_snapshot)
VALUES ('21111111-1111-1111-1111-111111111111', '11111111-2222-3333-4444-555555555555', 'prod-fix-1', 1, 800000, 100000, 100000, 1000000, 'MIG-02 fixture product');
INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price, product_title_snapshot)
VALUES ('21111111-1111-1111-1111-111111111112', '11111111-2222-3333-4444-555555555556', 'prod-fix-1', 1, 1600000, 200000, 200000, 2000000, 'MIG-02 fixture product');
INSERT INTO ledger_entries (id, target_account, target_type, type, amount, reason, operator, related_order_or_dispute)
VALUES ('31111111-1111-1111-1111-111111111111', 'platform', 'platform', 'debit', 1000000, 'fixture-order-1', 'system', 'ORD-MIG02-1');
INSERT INTO ledger_entries (id, target_account, target_type, type, amount, reason, operator, related_order_or_dispute)
VALUES ('31111111-1111-1111-1111-111111111112', 'platform', 'platform', 'debit', 2000000, 'fixture-order-2', 'system', 'ORD-MIG02-2');
"""

WORKLOAD_SQL = """
BEGIN;
INSERT INTO stores (id, name, slug, owner_type, owner_name, total_sales, status)
VALUES ('store-fix-1', 'MIG-02 fixture store', 'mig02-fix', 'dropshipper', 'fixture-owner', 0, 'active')
ON CONFLICT (id) DO NOTHING;
INSERT INTO orders (id, order_number, store_id, store_name, total_amount, discount_amount, payable_amount, payment_method, customer_full_name, customer_phone, customer_province, customer_city, customer_address, customer_postal_code, status, tracking_code, shipping_cost, dropshipper_profit, platform_profit, referral_commission, profit_released)
VALUES ('11111111-2222-3333-4444-555555555557', 'ORD-MIG02-W', 'store-fix-1', 'MIG-02 fixture store', 1000000, 0, 1000000, 'cash', 'MIG-02 buyer', '09110000001', 'Qom', 'Qom', 'fixture address', '3711111111', 'pending', 'TRK-W', 0, 100000, 50000, 0, false);
INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price, product_title_snapshot)
VALUES ('21111111-1111-1111-1111-111111111113', '11111111-2222-3333-4444-555555555557', 'prod-fix-1', 1, 800000, 100000, 100000, 1000000, 'MIG-02 fixture product');
INSERT INTO ledger_entries (id, target_account, target_type, type, amount, reason, operator, related_order_or_dispute)
VALUES ('31111111-1111-1111-1111-111111111113', 'platform', 'platform', 'debit', 1000000, 'fixture-workload', 'system', 'ORD-MIG02-W');
SELECT count(*) FROM orders WHERE order_number = 'ORD-MIG02-W';
ROLLBACK;
"""


def scenario_r1(r: Rehearsal):
    print("\n===== R-1: fail migration BEFORE commit =====", flush=True)
    db = "mig02_r1"
    old = r.git_rev("92f9cca")
    create_db(db)
    r.apply_base(db)
    r.seed(db, negative_stock=True)
    r.reset_fake_root_state(old, db)
    release = r.stage_release(FAKE_COMMITS["R-1"],
                              {"202611210002_mig02_r1_stock_check.up.sql": M_FAIL_SQL},
                              [FAIL_ENTRY])
    new_bin_sha = hashlib.sha256((FAKE_ROOT / "backend/bin/server-linux").read_bytes()).hexdigest()
    persistent_before = sorted(p.name for p in (FAKE_ROOT / "backend/migrations").glob("*.up.sql"))
    code, out = r.run_deploy(FAKE_COMMITS["R-1"], db, r.evidence / "r1-deploy.log")
    assert code != 0, "R-1 deploy must fail"
    deployed_commit = (FAKE_ROOT / ".deployed-commit").read_text().strip()
    binary_sha_after = hashlib.sha256((FAKE_ROOT / "backend/bin/server-linux").read_bytes()).hexdigest()
    persistent_after = sorted(p.name for p in (FAKE_ROOT / "backend/migrations").glob("*.up.sql"))
    applied = pg(db, "SELECT count(*) FROM schema_migrations WHERE version LIKE '%mig02_r1%'")
    checks = {
        "deploy_failed": code != 0,
        "deployed_commit_unchanged": deployed_commit == old,
        "binary_unchanged": binary_sha_after == new_bin_sha,
        "persistent_dir_unchanged": persistent_after == persistent_before,
        "failed_migration_not_applied": applied == "0",
        "migration_count": pg(db, "SELECT count(*) FROM schema_migrations"),
    }
    backups = sorted((FAKE_ROOT / "backups").glob("*"))
    assert backups, "R-1 must leave a backup dir"
    backup = backups[-1]
    assert (backup / "applied-before.json").is_file(), "applied-before.json missing"
    # Old binary still healthy.
    healthy, log = r.boot_check(db, FAKE_ROOT / "backend/migrations", "r1")
    checks["old_binary_healthy_after_failed_deploy"] = healthy
    checks["backup_dir"] = backup.name
    # The deploy's own rollback must have succeeded (not just the
    # driver's external boot check): status rolled-back + the restore
    # message in the log. (Nov 20 2026: an earlier run passed the
    # external checks while the rollback itself died at `compose up`
    # because the restored binary had lost its +x bit.)
    rp_path = backup / "rollback-point.json"
    rp = json.loads(rp_path.read_text()) if rp_path.is_file() else {}
    checks["rollback_status_rolled_back"] = rp.get("status") == "rolled-back"
    checks["restore_message_in_log"] = "Previous code restored" in out
    checks["restored_binary_executable"] = os.access(FAKE_ROOT / "backend/bin/server-linux", os.X_OK)
    r.results["R-1"] = {"verdict": "PASS" if all(checks.values()) else "FAIL", "checks": checks}
    r.write_evidence("r1.json", r.results["R-1"])
    print("R-1:", r.results["R-1"]["verdict"], checks, flush=True)


def scenario_r2(r: Rehearsal):
    print("\n===== R-2: fail migration AFTER commit (Mode A) =====", flush=True)
    db = "mig02_r2"
    old = r.git_rev("92f9cca")
    extra = {"202611210001_mig02_r2_scratch.up.sql": M_OK_SQL,
             "202611210002_mig02_r1_stock_check.up.sql": M_FAIL_SQL}
    entries = [OK_ENTRY, FAIL_ENTRY]

    # --- negative control: pre-MIG-02 deploy_remote.py (92f9cca) ---
    create_db(db)
    r.apply_base(db)
    r.seed(db, negative_stock=True)
    r.reset_fake_root_state(old, db)
    r.stage_release(FAKE_COMMITS["R-2"], extra, entries, old_deploy_remote=True)
    code, out = r.run_deploy(FAKE_COMMITS["R-2"], db, r.evidence / "r2-control-deploy.log")
    control = {
        "deploy_failed": code != 0,
        "rollback_needs_attention": "ROLLBACK NEEDS ATTENTION" in out,
        # The pre-MIG-02 script predates rollback-point.json entirely
        # (0 mentions in 92f9cca:scripts/deploy_remote.py), so a real
        # control run records no rollback point. (Was `... or True` —
        # always true — supervisor fix item 3.)
        "no_rollback_point": not list((FAKE_ROOT / "backups").glob("*/rollback-point.json")),
    }
    healthy, log = r.boot_check(db, FAKE_ROOT / "backend/migrations", "r2-control")
    # The server logs the generic fail-closed line (the migrate CLI's
    # verbatim "missing from required directory" is not in the server log).
    control["old_binary_boot_failed_with_missing_migration"] = (
        not healthy and "database migration validation failed (fail-closed)" in log)
    r.write_evidence("r2-control.json", control)
    print("R-2 control:", control, flush=True)
    assert control["deploy_failed"] and control["rollback_needs_attention"], "control did not reproduce the failure"
    assert control["no_rollback_point"], \
        "control wrote a rollback-point.json; the pre-MIG-02 script must not record one"
    assert control["old_binary_boot_failed_with_missing_migration"], \
        "control boot did not reproduce the §2.2 failure"

    # --- real run: MIG-02 deploy_remote.py ---
    create_db(db)
    r.apply_base(db)
    r.seed(db, negative_stock=True)
    r.reset_fake_root_state(old, db)
    r.stage_release(FAKE_COMMITS["R-2"], extra, entries)
    code, out = r.run_deploy(FAKE_COMMITS["R-2"], db, r.evidence / "r2-deploy.log")
    assert code != 0, "R-2 deploy must fail at M2"
    backups = sorted((FAKE_ROOT / "backups").glob("*"))
    rp = None
    for b in backups:
        if (b / "rollback-point.json").is_file():
            rp = json.loads((b / "rollback-point.json").read_text())
    assert rp is not None, "rollback-point.json missing"
    checks = {
        "deploy_failed": code != 0,
        "precheck_mode_a": rp.get("repair") == "mode-a",
        "stranded_filed": (FAKE_ROOT / "backend/migrations/202611210001_mig02_r2_scratch.up.sql").is_file(),
        "status_rolled_back": rp.get("status") == "rolled-back",
        "restored_commit_is_old": rp.get("restoredCommit") == old,
        "probe_passed_in_log": "PROBE_OK" in out,
    }
    healthy, log = r.boot_check(db, FAKE_ROOT / "backend/migrations", "r2")
    checks["old_binary_healthy_after_mode_a"] = healthy
    checks["scratch_row_present"] = pg(db, "SELECT count(*) FROM mig02_r2_scratch") == "1"
    checks["failed_migration_not_applied"] = pg(
        db, "SELECT count(*) FROM schema_migrations WHERE version LIKE '%mig02_r1%'") == "0"
    r.results["R-2"] = {"verdict": "PASS" if all(checks.values()) else "FAIL",
                        "checks": checks, "rollback_point": rp, "control": control}
    r.write_evidence("r2.json", r.results["R-2"])
    print("R-2:", r.results["R-2"]["verdict"], checks, flush=True)


def scenario_r3(r: Rehearsal):
    print("\n===== R-3: fail backend health after migration (Mode B) =====", flush=True)
    db = "mig02_r3"
    old = r.git_rev("2df713b")          # pre-floor binary (before ff57367)
    new_commit = FAKE_COMMITS["R-3"]
    create_db(db)
    r.apply_base(db)
    r.seed(db)
    r.reset_fake_root_state(old, db)
    # Old release (for the pre-check) staged with the pre-floor commit.
    r.stage_old_release(old)
    release = r.stage_release(new_commit,
                              {"202611210003_mig02_r3_legacy_break.up.sql": M_BREAK_SQL},
                              [BREAK_ENTRY], manifest_commit=new_commit)
    flag = r.state_dir / "fail-backend-once"
    flag.touch()  # the fake docker fails the next `compose up backend` only when the flag file EXISTS
    env = {"MIG02_FAIL_BACKEND_ONCE": str(flag)}
    code, out = r.run_deploy(new_commit, db, r.evidence / "r3-deploy.log", extra_env=env)
    assert code != 0, "R-3 deploy must fail at health"
    backups = sorted((FAKE_ROOT / "backups").glob("*"))
    rp = None
    for b in backups:
        if (b / "rollback-point.json").is_file():
            rp = json.loads((b / "rollback-point.json").read_text())
    assert rp is not None, "rollback-point.json missing"
    new_bin_sha = hashlib.sha256((release / "backend/bin/server-linux").read_bytes()).hexdigest()
    live_bin_sha = hashlib.sha256((FAKE_ROOT / "backend/bin/server-linux").read_bytes()).hexdigest()
    checks = {
        "deploy_failed": code != 0,
        "precheck_incompatible": rp.get("compatibility", {}).get("verdict") == "incompatible",
        "forward_repair": rp.get("status") == "forward-repaired" and rp.get("repair") == "mode-b",
        "deployed_commit_is_new": (FAKE_ROOT / ".deployed-commit").read_text().strip() == new_commit,
        "live_binary_is_new": live_bin_sha == new_bin_sha,
        "repair_note": "FORWARD REPAIR SUCCEEDED" in out,
    }
    healthy, log = r.boot_check(db, FAKE_ROOT / "backend/migrations", "r3")
    checks["forward_repaired_binary_healthy"] = healthy
    code2, out2 = r.run_probe(db, "current")
    checks["probe_current_passes"] = code2 == 0
    checks["breaking_migration_applied_once"] = pg(
        db, "SELECT count(*) FROM schema_migrations WHERE version LIKE '%mig02_r3%'") == "1"
    # Re-running migrate is a no-op (proves no retry of the stranded file).
    env2 = dict(os.environ, DATABASE_URL=db_url(db))
    m2 = sh([str(r.migrate_bin), "-dir", str(FAKE_ROOT / "backend/migrations")], env=env2, timeout=120)
    checks["migrate_rerun_noop"] = m2.returncode == 0 and "up-to-date" in (m2.stdout + m2.stderr)
    r.results["R-3"] = {"verdict": "PASS" if all(checks.values()) else "FAIL",
                        "checks": checks, "rollback_point": rp}
    r.write_evidence("r3.json", r.results["R-3"])
    print("R-3:", r.results["R-3"]["verdict"], checks, flush=True)


def scenario_r7(r: Rehearsal):
    print("\n===== R-7: mid-migrate failure commits a breaking migration (supervisor fix item 1) =====", flush=True)
    db = "mig02_r7"
    old = r.git_rev("2df713b")          # pre-floor binary (before ff57367)
    new_commit = FAKE_COMMITS["R-7"]
    # The breaking-shaped migration sorts BEFORE the failing one, so
    # `migrate` commits it and then fails on M2 — the exact Mode A
    # shape. With the old `migrationsApplied` test the schema was
    # misread as unchanged (the list is only set after a *successful*
    # migrate) and the floor check was bypassed.
    extra = {"202611210001_mig02_r7_legacy_break.up.sql": R7_BREAK_SQL,
             "202611210002_mig02_r1_stock_check.up.sql": M_FAIL_SQL}
    entries = [R7_BREAK_ENTRY, FAIL_ENTRY]
    create_db(db)
    r.apply_base(db)
    r.seed(db, negative_stock=True)
    r.reset_fake_root_state(old, db)
    r.stage_old_release(old)
    release = r.stage_release(new_commit, extra, entries, manifest_commit=new_commit)
    code, out = r.run_deploy(new_commit, db, r.evidence / "r7-deploy.log")
    assert code != 0, "R-7 deploy must fail at M2 (mid-migrate)"
    backups = sorted((FAKE_ROOT / "backups").glob("*"))
    rp = None
    for b in backups:
        if (b / "rollback-point.json").is_file():
            rp = json.loads((b / "rollback-point.json").read_text())
    assert rp is not None, "rollback-point.json missing"
    assert backups, "R-7 must leave a backup dir"
    before = json.loads((backups[-1] / "applied-before.json").read_text())
    new_bin_sha = hashlib.sha256((release / "backend/bin/server-linux").read_bytes()).hexdigest()
    live_bin_sha = hashlib.sha256((FAKE_ROOT / "backend/bin/server-linux").read_bytes()).hexdigest()
    # What this scenario proves — the pre-check DECISION, not a full
    # repair: the failing migration is deterministic (a CHECK the
    # fixture violates), so the forward repair CANNOT complete: Mode B
    # copies the manifest into the persistent dir and the new binary
    # retries the pending migration at boot, failing closed on the
    # same check violation the deploy hit. The honest terminal state
    # is therefore ROLLBACK NEEDS ATTENTION (runbook §4: diagnose
    # from the backup, repair forward with a hotfix — never restore
    # the proven-incompatible old binary). R-3 already proves Mode B
    # *succeeds* when the failure is post-migrate; R-7 proves the
    # fix-item-1 decision (floor check fires on the fresh applied
    # set, Mode B is taken, no blind restore) on the mid-migrate
    # failure the old `migrationsApplied` test misread.
    checks = {
        "deploy_failed": code != 0,
        "breaking_migration_committed_before_failure": pg(
            db, "SELECT count(*) FROM schema_migrations WHERE version LIKE '%mig02_r7%'") == "1",
        "breaking_migration_absent_from_snapshot": not any(
            "mig02_r7" in v for v in before.get("versions", [])),
        # The floor check must fire on the fresh applied set (not the
        # empty migrationsApplied list): pre-floor old binary + schema
        # changed => incompatible => Mode B path taken, never a blind
        # restore of the pre-floor binary.
        "precheck_incompatible": rp.get("compatibility", {}).get("verdict") == "incompatible",
        "floor_check_fired": "predates lastCompatibleRevision" in rp.get("compatibility", {}).get("reason", ""),
        "mode_b_attempted": rp.get("repair") == "mode-b",
        "live_binary_is_new": live_bin_sha == new_bin_sha,
        "old_binary_not_restored": rp.get("restoredCommit") is None,
        "rollback_needs_attention": "ROLLBACK NEEDS ATTENTION" in out,
        "terminal_status_rollback_failed": rp.get("status") == "rollback-failed",
        "rollback_error_is_health": "Backend health check failed" in str(rp.get("rollbackError", "")),
    }
    healthy, log = r.boot_check(db, FAKE_ROOT / "backend/migrations", "r7")
    # The repaired binary must fail boot on the SAME deterministic
    # migration (pending-file retry at boot), not on a wrong binary
    # or a missing-migration validation error.
    checks["new_binary_fails_on_same_migration"] = (
        not healthy and "202611210002_mig02_r1_stock_check.up.sql failed" in log)
    checks["breaking_migration_applied_once"] = pg(
        db, "SELECT count(*) FROM schema_migrations WHERE version LIKE '%mig02_r7%'") == "1"
    checks["failed_migration_not_applied"] = pg(
        db, "SELECT count(*) FROM schema_migrations WHERE version LIKE '%mig02_r1%'") == "0"
    r.results["R-7"] = {"verdict": "PASS" if all(checks.values()) else "FAIL",
                        "checks": checks, "rollback_point": rp}
    r.write_evidence("r7.json", r.results["R-7"])
    print("R-7:", r.results["R-7"]["verdict"], checks, flush=True)


def scenario_r4(r: Rehearsal):
    print("\n===== R-4: overlap-window compatibility matrix =====", flush=True)
    # Pre-schema: migrations < 202611110003 only. Post-schema: all 210.
    pre_dir = r.work / "migrations-pre"
    if pre_dir.exists():
        shutil.rmtree(pre_dir)
    pre_dir.mkdir()
    for path in (r.clean_tree / "backend/migrations").glob("*.up.sql"):
        if path.name < "202611110003_sku_inventory.up.sql":
            shutil.copy2(path, pre_dir / path.name)
    results = {}
    for label, migrations_dir in (("pre", pre_dir), ("post", r.clean_tree / "backend/migrations")):
        db = f"mig02_r4_{label}"
        create_db(db)
        r.apply_base(db, migrations_dir=migrations_dir)
        r.seed(db)
        legacy_code, legacy_out = r.run_probe(db, "legacy", checks="inventory")
        current_code, current_out = r.run_probe(db, "current", checks="inventory")
        results[label] = {"legacy_probe": "PASS" if legacy_code == 0 else "FAIL",
                          "current_probe": "PASS" if current_code == 0 else "FAIL"}
        (r.evidence / f"r4-{label}-legacy.log").write_text(legacy_out)
        (r.evidence / f"r4-{label}-current.log").write_text(current_out)
    expected = {"pre": {"legacy_probe": "PASS", "current_probe": "FAIL"},
                "post": {"legacy_probe": "FAIL", "current_probe": "PASS"}}
    matrix_ok = results == expected
    # Rolled-back write workload on the new schema (order + items + ledger).
    # NOTE: the 000002 seed migration already inserts 1 order, so the
    # "unchanged" check compares before/after counts, never a literal 0.
    orders_before = pg("mig02_r4_post", "SELECT count(*) FROM orders")
    wl = pg_file("mig02_r4_post", WORKLOAD_SQL)
    # The workload's inner SELECT returns 1 (the order existed inside
    # the transaction); the last output line is ROLLBACK, so scan all lines.
    workload_ok = any(line.strip() == "1" for line in wl.splitlines())
    checks = {"matrix_diagonal": matrix_ok,
              "workload_order_rolled_back": bool(workload_ok),
              "workload_orders_unchanged": pg("mig02_r4_post", "SELECT count(*) FROM orders") == orders_before}
    r.results["R-4"] = {"verdict": "PASS" if all(checks.values()) else "FAIL",
                        "checks": checks, "matrix": results}
    r.write_evidence("r4.json", r.results["R-4"])
    print("R-4:", r.results["R-4"]["verdict"], checks, results, flush=True)


def scenario_r6(r: Rehearsal):
    print("\n===== R-6: accepted data never erased =====", flush=True)
    db = "mig02_r6"
    old = r.git_rev("92f9cca")
    create_db(db)
    r.apply_base(db)
    r.seed(db, negative_stock=True)
    r.reset_fake_root_state(old, db)
    r.stage_release(FAKE_COMMITS["R-6-ok"],
                    {"202611210001_mig02_r2_scratch.up.sql": M_OK_SQL}, [OK_ENTRY])
    code, out = r.run_deploy(FAKE_COMMITS["R-6-ok"], db, r.evidence / "r6-ok-deploy.log")
    assert code == 0, f"R-6 success deploy must pass: {out[-1000:]}"
    pg_file(db, ACCEPTED_ORDERS_SQL)
    before = r.counts(db)
    checksum = pg(db, "SELECT md5(string_agg(order_number, ',' ORDER BY order_number)) FROM orders")
    # Failing deploy (migrates M1_ok again? no — M_fail only) then rollback.
    r.stage_release(FAKE_COMMITS["R-6-fail"],
                    {"202611210002_mig02_r1_stock_check.up.sql": M_FAIL_SQL}, [FAIL_ENTRY])
    code, out = r.run_deploy(FAKE_COMMITS["R-6-fail"], db, r.evidence / "r6-fail-deploy.log")
    assert code != 0, "R-6 failing deploy must fail"
    after = r.counts(db)
    checksum_after = pg(db, "SELECT md5(string_agg(order_number, ',' ORDER BY order_number)) FROM orders")
    checks = {
        "success_deploy_passed": True,
        "failing_deploy_failed": code != 0,
        "orders_unchanged": before["orders"] == after["orders"],
        "ledger_unchanged": before["ledger"] == after["ledger"],
        "order_checksum_unchanged": checksum == checksum_after,
        "deployed_commit_restored": (FAKE_ROOT / ".deployed-commit").read_text().strip() == FAKE_COMMITS["R-6-ok"],
    }
    r.results["R-6"] = {"verdict": "PASS" if all(checks.values()) else "FAIL",
                        "checks": checks, "before": before, "after": after}
    r.write_evidence("r6.json", r.results["R-6"])
    print("R-6:", r.results["R-6"]["verdict"], checks, flush=True)


def scenario_r5(r: Rehearsal):
    print("\n===== R-5: unavailable rollback points recorded =====", flush=True)
    ledger = FAKE_ROOT / ".rollback-points.json"
    assert ledger.is_file(), "ledger missing"
    records = json.loads(ledger.read_text())
    # Record two genuinely-unavailable points (pre-floor old release with
    # no release dir, and a point with no manifest) via the CLI.
    sys.path.insert(0, str(REPO / "scripts"))
    import importlib
    import rollback_points
    importlib.reload(rollback_points)
    rollback_points.append_record(FAKE_ROOT, {
        "commit": r.git_rev("2df713b"),
        "lastCompatibleRevision": r.git_rev("ff57367"),
        "migrationsApplied": [],
        "status": "unavailable",
        "reason": "rollback pre-check: old commit predates lastCompatibleRevision (pre-floor)",
    })
    rollback_points.append_record(FAKE_ROOT, {
        "commit": "0" * 40,
        "lastCompatibleRevision": r.git_rev("ff57367"),
        "migrationsApplied": [],
        "status": "unavailable",
        "reason": "release dir missing from /opt/drup/releases; cannot verify the old binary's migration set",
    })
    records = json.loads(ledger.read_text())
    statuses = [x["status"] for x in records]
    checks = {
        "ledger_exists": True,
        "has_unavailable": statuses.count("unavailable") >= 2,
        "has_rolled_back_mode_a": any(
            x["status"] == "rolled-back" and x.get("repair") == "mode-a" for x in records),
        "has_forward_repaired_mode_b": any(
            x["status"] == "forward-repaired" and x.get("repair") == "mode-b" for x in records),
        "total_records": len(records),
    }
    r.results["R-5"] = {"verdict": "PASS" if all(checks.values()) else "FAIL",
                        "checks": checks, "ledger": records}
    r.write_evidence("r5.json", r.results["R-5"])
    print("R-5:", r.results["R-5"]["verdict"], checks, flush=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--evidence-dir", default=str(Path.home() / "workspace/mig02-evidence"))
    parser.add_argument("--keep", action="store_true")
    parser.add_argument("--scenarios", default="R-1,R-2,R-3,R-4,R-5,R-6,R-7")
    args = parser.parse_args()
    wanted = [s.strip() for s in args.scenarios.split(",") if s.strip()]
    r = Rehearsal(args.evidence_dir, keep=args.keep)
    try:
        r.build_clean_tree()
        r.start_infra()
        r.init_fake_root()
        if "R-1" in wanted:
            scenario_r1(r)
        if "R-2" in wanted:
            scenario_r2(r)
        if "R-3" in wanted:
            scenario_r3(r)
        if "R-4" in wanted:
            scenario_r4(r)
        if "R-6" in wanted:
            scenario_r6(r)
        if "R-7" in wanted:
            scenario_r7(r)
        if "R-5" in wanted:
            scenario_r5(r)
    finally:
        if args.keep:
            print("--keep: fake root, DBs and scratch Redis left in place", flush=True)
        else:
            # Best-effort teardown: one step failing must not skip the rest.
            # Stop the fake-docker backend first: it holds DB connections
            # that would block the DROP DATABASEs below.
            try:
                r.stop_fake_backend()
            except Exception:
                pass
            for name in ("mig02_r1", "mig02_r2", "mig02_r3", "mig02_r4_pre",
                         "mig02_r4_post", "mig02_r6", "mig02_r7"):
                try:
                    drop_db(name)
                except Exception:
                    pass
            if r._obs_proc is not None:
                try:
                    r._obs_proc.terminate()
                    r._obs_proc.wait(timeout=10)
                except Exception:
                    try:
                        r._obs_proc.kill()
                    except Exception:
                        pass
            if r._redis_started:
                try:
                    redis_env = dict(os.environ, PATH=str(Path.home() / "workspace/.dev/redis-shim") + ":" + os.environ["PATH"])
                    sh(["redis-cli", "-p", str(REDIS_PORT), "shutdown", "nosave"], env=redis_env)
                except Exception:
                    pass
            if FAKE_ROOT.exists() and (FAKE_ROOT / ".mig02-rehearsal").is_file():
                shutil.rmtree(FAKE_ROOT)
    verdicts = {k: v["verdict"] for k, v in r.results.items()}
    summary = {"scenarios": verdicts,
               "overall": "PASS" if verdicts and all(v == "PASS" for v in verdicts.values()) else "FAIL"}
    r.write_evidence("REHEARSAL-SUMMARY.json", summary)
    print("\n===== rehearsal summary =====", flush=True)
    print(json.dumps(summary, indent=2), flush=True)
    return 0 if summary["overall"] == "PASS" else 1


if __name__ == "__main__":
    sys.exit(main())
