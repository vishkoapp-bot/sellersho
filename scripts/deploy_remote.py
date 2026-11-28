"""Deploy to the verified bind-mounted stack; preserve secrets and back up data.

Failure rolls back code/config, never the database over newly received orders.

MIG-02: the staged release manifest (release.json -> migrations) is the
single source of truth — it is verified before migrations run, its
timeouts are passed to `migrate`, its post-conditions are enforced for
the migrations this deploy applies, and the rollback is schema-aware:
a compatible old binary is restored (Mode A files applied-but-unfiled
migrations first), otherwise the old binary is refused and a forward
repair lands the new release instead (Mode B). Every deploy and
rollback is recorded in the deploy backup dir (rollback-point.json)
and in /opt/drup/.rollback-points.json. See
docs/operations/rollback-runbook.md.
"""
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request

from migration_history import classify_history
from deploy_migration_stage import prepare_migrator
from rollback_points import append_record

root = Path(sys.argv[1]).resolve()
commit, ssl, move_xray = sys.argv[2], sys.argv[3] == '1', sys.argv[4] == '1'
require_primary_https = len(sys.argv) > 5 and sys.argv[5] == '1'
if root != Path('/opt/drup') or not re.fullmatch(r'[a-f0-9]{40}', commit):
    raise SystemExit('Invalid installation or commit')
os.umask(0o077)
lock = open(root / '.deployment.lock', 'w')
fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
release = root / 'releases' / commit
backup = root / 'backups' / ('deploy-' + time.strftime('%Y%m%dT%H%M%SZ', time.gmtime()) + '-' + commit[:8])
# BAK-01: `backup` is NOT mkdir'd here — the entry point creates it
# itself (atomic .tmp + mv, after verification) in the block below.


def run(*args, capture=False, **kwargs):
    return subprocess.run(args, check=True, text=True, stdout=subprocess.PIPE if capture else None, **kwargs).stdout


def inspect(name):
    return json.loads(run('docker', 'inspect', name, capture=True))[0]


# MIG-02 (steps 2/3/4/5) — schema-aware deploy/rollback helpers.
# Self-contained (json/hashlib/subprocess only, plus the rollback_points
# ledger) so the staging rehearsal can exec them standalone against a
# disposable database; deploy_remote.py wires them into the deploy and
# rollback flows below.
PSQL = ['docker', 'exec', 'drup_postgres', 'psql', '-U', 'drup_admin', '-d', 'drup_db']


def normalized_sha256(path):
    """sha256 of a migration file the way the runner hashes it:
    runner.go load() replaces CRLF with LF before hashing, so the
    checksum recorded in schema_migrations is the NORMALIZED hash.
    The manifest and classification record RAW bytes instead — the two
    agree except for the (currently 8) files containing CRLF. Every
    comparison against a RECORDED checksum must use this function;
    comparisons against the manifest use raw sha256."""
    return hashlib.sha256(Path(path).read_bytes().replace(b'\r\n', b'\n')).hexdigest()


def verify_manifest_migrations(manifest, migrations_dir):
    """Step 3a — the staged manifest must match the staged files
    exactly: every *.up.sql sha256 matches, no extra/missing files,
    count matches. Raises RuntimeError naming the mismatch; returns
    the file count. Called before any DB touch or backup."""
    files = manifest.get('migrations', {}).get('files', [])
    expected = {entry['name']: entry['sha256'] for entry in files}
    staged = {}
    for path in Path(migrations_dir).glob('*.up.sql'):
        staged[path.name] = hashlib.sha256(path.read_bytes()).hexdigest()
    missing = sorted(set(expected) - set(staged))
    extra = sorted(set(staged) - set(expected))
    changed = sorted(name for name in set(expected) & set(staged)
                     if expected[name] != staged[name])
    if missing or extra or changed:
        detail = []
        if missing:
            detail.append(f"missing from staged set: {missing}")
        if extra:
            detail.append(f"not in manifest: {extra}")
        if changed:
            detail.append(f"sha256 mismatch: {changed}")
        raise RuntimeError('Staged migrations do not match the release manifest (' + '; '.join(detail) + ')')
    count = manifest.get('migrations', {}).get('count')
    if count is not None and count != len(expected):
        raise RuntimeError(f"Manifest migration count {count} != file count {len(expected)}")
    return len(expected)


def query_applied(psql_argv=PSQL):
    """Return {version: checksum} from schema_migrations (fresh read).

    A checksum of '' means a legacy row that was never adopted; callers
    must treat that as 'no checksum recorded', not as a mismatch."""
    result = subprocess.run(psql_argv + ['-Atc',
                            "SELECT version || '|' || COALESCE(checksum,'') FROM schema_migrations"],
                            capture_output=True, text=True, timeout=60)
    if result.returncode != 0:
        raise RuntimeError(f"cannot read schema_migrations: {result.stderr.strip()[:200]}")
    applied = {}
    for line in result.stdout.splitlines():
        if '|' not in line:
            continue
        version, checksum = line.split('|', 1)
        applied[version] = checksum
    return applied


def psql_scalar(psql_argv, sql):
    """Run one read-only scalar query; return its stripped output."""
    result = subprocess.run(psql_argv + ['-Atc', sql],
                            capture_output=True, text=True, timeout=60)
    if result.returncode != 0:
        raise RuntimeError(f"post-condition query failed: {sql} ({result.stderr.strip()[:200]})")
    return result.stdout.strip()


def check_manifest_recorded(manifest, applied, migrations_dir):
    """Step 2 — replaces the old hardcoded 202611090001 assertion:
    every manifest file must be present in schema_migrations with a
    matching checksum. The comparison uses the runner's normalized
    hash (see normalized_sha256): the recorded checksum is normalized,
    the manifest's is raw. Raises RuntimeError on the first gap."""
    for entry in manifest.get('migrations', {}).get('files', []):
        name = entry['name']
        got = applied.get(name)
        if got is None:
            raise RuntimeError(f"Manifest migration was not recorded in schema_migrations: {name}")
        staged = Path(migrations_dir) / name
        if not staged.is_file():
            raise RuntimeError(f"Manifest migration missing from the staged release: {name}")
        if got and got != normalized_sha256(staged):
            raise RuntimeError(f"Checksum drift for recorded migration {name}")
    return len(manifest.get('migrations', {}).get('files', []))


def run_post_conditions(manifest, applied_names, psql_argv=PSQL):
    """Step 5.3 — enforce the manifest's post-conditions, but ONLY for
    the migrations this deploy applied. Each condition is a scalar
    SELECT compared with its comparator ('eq' or 'ge'). Raises
    RuntimeError on the first failure; returns the count checked."""
    conditions = manifest.get('migrations', {}).get('post_conditions', {})
    checked = 0
    for name in sorted(applied_names):
        for cond in conditions.get(name, []):
            sql, expected = cond['sql'], cond['expected']
            comparator = cond.get('comparator', 'eq')
            if comparator not in ('eq', 'ge'):
                raise RuntimeError(f"Unknown post-condition comparator {comparator!r} for {name}")
            raw = psql_scalar(psql_argv, sql)
            try:
                value = int(raw)
            except ValueError:
                raise RuntimeError(f"Post-condition for {name} returned non-integer {raw!r}: {sql}") from None
            ok = value == expected if comparator == 'eq' else value >= expected
            if not ok:
                raise RuntimeError(
                    f"Post-condition FAILED for {name}: {sql} returned {value}, "
                    f"expected {comparator} {expected}")
            checked += 1
    return checked


def copy_manifest_migrations(manifest, release_dir, persistent_dir, applied):
    """Step 3c — the persistent-dir copy is manifest-driven: copy
    exactly the manifest files (verifying sha256 after each copy) and
    record the copied set. A *.up.sql in the persistent dir that is
    NOT in the manifest is removed when it is not applied (it can only
    break the next boot's out-of-order check); when it IS applied the
    deploy fails closed — the manifest is incomplete for live history.
    *.down.sql files (never executed by any tooling) left by the old
    copy loop are removed as inert leftovers. Returns
    (copied, removed)."""
    wanted = {entry['name']: entry['sha256']
              for entry in manifest.get('migrations', {}).get('files', [])}
    persistent = Path(persistent_dir)
    persistent.mkdir(parents=True, exist_ok=True)
    copied, removed = [], []
    for name, want in sorted(wanted.items()):
        source = Path(release_dir) / 'backend/migrations' / name
        dest = persistent / name
        if dest.is_file() and hashlib.sha256(dest.read_bytes()).hexdigest() == want:
            continue
        if not source.is_file():
            raise RuntimeError(f"Manifest migration missing from the staged release: {name}")
        shutil.copy2(source, dest)
        if hashlib.sha256(dest.read_bytes()).hexdigest() != want:
            raise RuntimeError(f"sha256 mismatch after copying {name} to the persistent dir")
        copied.append(name)
    for path in sorted(persistent.glob('*.up.sql')):
        if path.name in wanted:
            continue
        if path.name in applied:
            raise RuntimeError(
                f"Applied migration {path.name} is in the persistent dir but not in the release manifest; "
                'refusing to deploy a manifest that does not cover live history')
        path.unlink()
        removed.append(path.name)
    for path in sorted(persistent.glob('*.down.sql')):
        path.unlink()
        removed.append(path.name)
    return copied, removed


def git_is_ancestor(repo, ancestor, commit):
    """True when `ancestor` is an ancestor of (or equal to) `commit`.
    Any git failure fails closed (False)."""
    try:
        result = subprocess.run(['git', '-C', str(repo), 'merge-base', '--is-ancestor',
                                 ancestor, commit], capture_output=True, timeout=30)
    except Exception:
        return False
    return result.returncode == 0


def compute_schema_changed(backup_dir, applied_now, migrations_applied):
    """Step 4 — did this deploy change the schema?

    `migrationsApplied` in the rollback point is only assigned after
    a *successful* migrate run, so a mid-migrate failure (the exact
    Mode A scenario: files 1..k-1 committed, file k failed) left it
    empty and the old test misread the schema as unchanged — which
    bypassed the lastCompatibleRevision floor check in
    rollback_precheck() and could restore a pre-floor binary onto the
    new schema. Compare the fresh applied set against the pre-deploy
    snapshot `backup/applied-before.json` (written before `migrate`
    runs) instead; fall back to the rollback-point list only when the
    snapshot is unreadable (e.g. the deploy failed before the
    snapshot was written — in which case `migrate` never ran, so the
    fallback value is correct anyway).
    """
    try:
        before = json.loads((Path(backup_dir) / 'applied-before.json').read_text())
        before_versions = set(before.get('versions') or [])
        return set(applied_now) != before_versions
    except Exception:
        return bool(migrations_applied)


def rollback_precheck(applied, old_set, aliases, persistent_dir, release_dir,
                      old_release_dir, manifest_checksums, lcr, old_commit, repo,
                      schema_changed=True):
    """Step 4 — schema-aware rollback pre-check.

    `applied` is {version: checksum} from schema_migrations (fresh);
    `old_set` is the file names in the old release's migration dir;
    `aliases` maps legacy version names to canonical files;
    `persistent_dir` is /opt/drup/backend/migrations (what the old
    binary actually reads at boot); `release_dir` is the failed
    release (Mode A filing source); `old_release_dir` is the old
    release (second filing source). `schema_changed` is False when the
    failed deploy applied no migrations — then the live schema is the
    one the old binary was already running, so the floor check does
    not apply.

    Returns a verdict dict:
      verdict "compatible" — every applied version resolves into the
        persistent dir (or the old set) and the old binary is at/after
        the floor: restore the old binary as before.
      verdict "mode-a" — some applied versions are missing from the
        persistent dir but can be filed from a release dir,
        sha-verified: restore after filing (files in `stranded`).
      verdict "incompatible" — the old binary predates the floor, or
        an applied version resolves to no file anywhere: refuse the
        blind restore, go to forward repair.
      verdict "unavailable" — no old release to restore (no previous
        commit, or its release dir is gone): forward repair.
    """
    if not old_commit:
        return {'verdict': 'unavailable', 'reason': 'no previous deployed commit recorded; nothing to restore',
                'stranded': [], 'old_ge_floor': False}
    if not Path(old_release_dir).is_dir():
        return {'verdict': 'unavailable',
                'reason': f'old release dir is gone: {old_release_dir} (commit {old_commit[:12]}); cannot enumerate the old binary migration set',
                'stranded': [], 'old_ge_floor': False}
    old_ge_floor = (not schema_changed) or (bool(lcr) and git_is_ancestor(repo, lcr, old_commit))
    if not old_ge_floor:
        return {'verdict': 'incompatible',
                'reason': f'old binary {old_commit[:12]} predates lastCompatibleRevision {lcr[:12]}; '
                          'its write paths may not understand the current schema (e.g. the 202611110003 legacy upsert)',
                'stranded': [], 'old_ge_floor': False}
    persistent_files = {p.name for p in Path(persistent_dir).glob('*.up.sql')} if Path(persistent_dir).is_dir() else set()
    stranded, unresolvable = [], []
    seen = set()
    for version, checksum in sorted(applied.items()):
        # Resolve the canonical file: the version itself when it is a
        # file name, else its legacy alias target.
        canonical = version
        if canonical not in old_set and canonical not in persistent_files:
            canonical = aliases.get(version, version)
        if canonical in seen:
            continue
        seen.add(canonical)
        # Expected checksum: the recorded one when present; else the
        # manifest's (for a file this release carries); else name-only.
        expected = checksum or manifest_checksums.get(canonical) or ''
        in_persistent = canonical in persistent_files
        if in_persistent and expected:
            actual = normalized_sha256(Path(persistent_dir) / canonical)
            if actual == expected:
                continue
        elif in_persistent:
            continue
        # Not filed (or drifted): can Mode A file it from a release dir?
        filed = False
        for source_dir in (release_dir, old_release_dir):
            candidate = Path(source_dir) / 'backend/migrations' / canonical
            if candidate.is_file() and (not expected or normalized_sha256(candidate) == expected):
                stranded.append({'name': canonical, 'sha256': expected or normalized_sha256(candidate)})
                filed = True
                break
        if not filed:
            unresolvable.append(version)
    if unresolvable:
        return {'verdict': 'incompatible',
                'reason': f"applied migrations resolve to no file in the persistent dir or either release dir: {sorted(unresolvable)}",
                'stranded': stranded, 'old_ge_floor': True}
    if stranded:
        return {'verdict': 'mode-a',
                'reason': f"{len(stranded)} applied migration(s) are unfiled in the persistent dir; Mode A will file them sha-verified before recreating the old binary",
                'stranded': stranded, 'old_ge_floor': True}
    return {'verdict': 'compatible',
            'reason': 'every applied version is present in the old binary migration set and the old binary is at/after lastCompatibleRevision',
            'stranded': [], 'old_ge_floor': True}


def file_stranded_migrations(stranded, release_dir, old_release_dir, persistent_dir):
    """Step 4 Mode A — copy each stranded file from the first release
    dir holding it with a matching sha256, verifying after copy.
    Raises RuntimeError when no source copy matches."""
    persistent = Path(persistent_dir)
    persistent.mkdir(parents=True, exist_ok=True)
    copied = []
    for entry in stranded:
        name, want = entry['name'], entry['sha256']
        source = None
        for base in (release_dir, old_release_dir):
            candidate = Path(base) / 'backend/migrations' / name
            if candidate.is_file() and normalized_sha256(candidate) == want:
                source = candidate
                break
        if source is None:
            raise RuntimeError(f"Mode A cannot file stranded migration {name}: no source copy matches the recorded checksum")
        dest = persistent / name
        shutil.copy2(source, dest)
        if normalized_sha256(dest) != want:
            raise RuntimeError(f"Mode A sha256 mismatch after filing {name}")
        copied.append(name)
    return copied


def run_write_probe(release_dir, expect):
    """Step 4 (D-3) — deploy-time write-path readiness probe
    (scripts/release/write_path_probe.py): the three write paths a
    rollback can silently break (inventory upsert, review insert,
    withdrawal-intent insert) inside one always-rolled-back
    transaction, so it provably creates no real orders. Raises
    RuntimeError on failure; the probe's own output is passed through
    so the deploy log carries the verdict."""
    probe = Path(release_dir) / 'scripts/release/write_path_probe.py'
    result = subprocess.run(['python3', str(probe), '--docker-container', 'drup_postgres',
                             '--expect', expect],
                            capture_output=True, text=True, timeout=300)
    sys.stdout.write(result.stdout)
    sys.stderr.write(result.stderr)
    if result.returncode != 0:
        raise RuntimeError(f'Write-path readiness probe failed (expect={expect}); see probe output above')
    return True


def write_rollback_point(backup_dir, record):
    """Write the per-deploy rollback record into the backup dir."""
    path = Path(backup_dir) / 'rollback-point.json'
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(record, indent=2, ensure_ascii=False) + '\n')
    return path


def decrypt_saved_files(backup_dir, saved_files, existed, key_file):
    """Decrypt the backup entry point's --include saved files for a rollback.

    BAK-01 gap found by the MIG-02 rehearsal (R-1/R-2, Nov 20 2026):
    the entry point encrypts EVERY artifact (including the --include
    saved files) and deletes the plaintext, so the published backup dir
    holds only <relative>.enc. The pre-MIG-02 rollback read
    backup/<relative> as plaintext and died with FileNotFoundError on
    the first file — no rollback ever restored anything. Decrypt each
    saved file that existed at deploy time into a temp dir (plain copy
    when the backup already holds it unencrypted) and return the dir.
    Raises RuntimeError when a needed file is missing or undecryptable:
    a restore must fail loudly, never silently skip a file.
    """
    out = Path(tempfile.mkdtemp(prefix='sellersho-rollback-decrypt-'))
    for relative in saved_files:
        if not existed.get(relative):
            continue
        plain = Path(backup_dir) / relative
        enc = Path(backup_dir) / (relative + '.enc')
        target = out / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        if plain.is_file():
            shutil.copy2(plain, target)
        elif enc.is_file():
            result = subprocess.run(['openssl', 'enc', '-d', '-aes-256-cbc',
                                     '-pbkdf2', '-md', 'sha256', '-salt',
                                     '-pass', f'file:{key_file}',
                                     '-in', str(enc), '-out', str(target)],
                                    capture_output=True, text=True)
            if result.returncode != 0:
                raise RuntimeError(f'rollback decrypt failed for {relative}: {result.stderr.strip()[-300:]}')
        else:
            raise RuntimeError(f'rollback saved file missing from backup (neither {relative} nor {relative}.enc present); refusing a partial restore')
    return out


def load_aliases(release_dir):
    """Legacy version -> canonical file map from a release dir (empty
    when the release predates the aliases file)."""
    path = Path(release_dir) / 'backend/internal/migrations/legacy-aliases.json'
    if not path.is_file():
        return {}
    try:
        return json.loads(path.read_text())
    except Exception:
        return {}


previous_edge = run('docker', 'ps', '--filter', 'label=com.docker.compose.project=drup',
                    '--filter', 'label=com.docker.compose.service=edge', '--format', '{{.ID}}', capture=True).splitlines()
# REL-02 deploy fix — `ssl` is the deploy's effective SSL flag, not the
# caller's bare argument: a live edge container means this IS an SSL
# deployment (the edge serves host 80/443, compose_files includes
# ssl.yml below, and the deploy recreates the edge). Every downstream
# use — compose_files, the metrics-ports assert, require_edge=ssl —
# already treats it that way, so require_edge=ssl is correct as
# written; the b464d03 failure was NOT this boolean, it was the stale
# ssl file the assert read (see the early ship below). Do not "fix"
# this line by dropping the `or bool(previous_edge)`: that would make
# the assert skip the edge on a host that runs one.
ssl = ssl or bool(previous_edge)
if require_primary_https and not ssl:
    raise SystemExit('Primary HTTPS requires the TLS edge')
# OBS-01 (C-02): docker-compose.observability.yml is DELIBERATELY absent
# here. It holds Grafana's ${GRAFANA_ADMIN_PASSWORD:?} fail-fast; this
# list is used by every deploy and by rollback, so including it would let
# an unset password abort the next deploy of any kind. Only
# compose_observability() (the collector step) adds that file.
# OBS-01 deploy-fix (Option B): the metrics-ports override file. ONE
# constant feeds every list that must name it — compose_files, saved_files,
# the ship copy loop, rollback_files — so the three-list sync hazard
# (audit F-11) cannot silently recur. scripts/test_deploy_metrics_ports.py
# fails if any of those four uses stops referencing this constant.
METRICS_PORTS_FILE = 'docker-compose.metrics-ports.yml'
compose_files = [root / 'docker-compose.yml', root / 'docker-compose.prod.yml']
old_backend, old_frontend = inspect('drup_backend'), inspect('drup_frontend')
old_env = dict(item.split('=', 1) for item in old_backend['Config']['Env'])
for container, destination in [(old_backend, '/app/server'), (old_frontend, '/usr/share/nginx/html')]:
    mounts = {m['Destination']: m['Source'] for m in container['Mounts']}
    expected = root / ('backend/bin/server-linux' if destination == '/app/server' else 'dist')
    if mounts.get(destination) != str(expected):
        raise SystemExit('Unexpected production mount; refusing replacement')
if json.loads((release / 'release.json').read_text())['commit'] != commit:
    raise SystemExit('Release manifest mismatch')
# MIG-02 step 2/3 — the staged manifest is the single source of truth
# for this release (REL-01's release.json -> migrations; deploy.ps1
# builds it with make_manifest.py instead of hand-writing a minimal
# JSON). Load it once here; every later step reads from this object,
# never from a second list.
manifest = json.loads((release / 'release.json').read_text())
manifest_migs = manifest.get('migrations', {})
# Runner-normalized checksums for the staged files (NOT the manifest's
# raw sha256 — see normalized_sha256): these are the values comparable
# to the checksums recorded in schema_migrations, and the values the
# rollback pre-check uses when a recorded checksum is absent.
manifest_checksums = {path.name: normalized_sha256(path)
                      for path in (release / 'backend/migrations').glob('*.up.sql')}
last_compatible = manifest_migs.get('lastCompatibleRevision') or ''
if not last_compatible:
    raise SystemExit('Release manifest has no migrations.lastCompatibleRevision; '
                     'refusing to deploy an unstamped release (MIG-02 step 2)')
# MIG-02 step 3a — verify the manifest BEFORE any DB touch or backup:
# every staged *.up.sql sha256 must match the manifest, no extra or
# missing files, count must match. A tampered staged file fails here,
# with the database untouched.
verify_manifest_migrations(manifest, release / 'backend/migrations')
# MIG-02 step 2 — the per-deploy rollback record. Written into the
# deploy backup dir as rollback-point.json as the deploy progresses
# (migrationsApplied once the migration step runs; terminal status on
# success or rollback) and appended to /opt/drup/.rollback-points.json
# on every terminal state.
rollback_point = {
    'commit': commit,
    'lastCompatibleRevision': last_compatible,
    'deployedAt': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
    'migrationsApplied': [],
    'status': 'in-progress',
}
applied = set(run('docker', 'exec', 'drup_postgres', 'psql', '-U', 'drup_admin', '-d', 'drup_db', '-Atc',
                  'SELECT version FROM schema_migrations', capture=True).splitlines())
canonical = {p.name for p in (release / 'backend/migrations').glob('*.up.sql')}
aliases = json.loads((release / 'backend/internal/migrations/legacy-aliases.json').read_text())
try:
    pending_names, accepted_aliases = classify_history(applied, canonical, aliases)
except ValueError as error:
    raise SystemExit(str(error)) from error
pending = [release / 'backend/migrations' / name for name in pending_names]
print('Pending migrations:', pending_names, 'reviewed legacy aliases:', len(accepted_aliases), flush=True)
# Reviewed live gap: attribution table is absent; its idempotent backfill
# derives customer/store links without changing users, orders or balances.
reviewed_historical = {'202611050001_customer_store_links.up.sql'}
if any(p.name < '202611090001_operations_automation.up.sql' and p.name not in reviewed_historical for p in pending):
    raise SystemExit('Unexpected historical pending migration; review before deployment')

runtime_file = root / '.env.deploy.runtime.json'
old_runtime = runtime_file.read_bytes() if runtime_file.exists() else None
runtime_env = {k: v for k, v in old_env.items() if k != 'PATH'}
runtime_env.setdefault('DOMAIN_EDGE_HOST', '46.249.98.190')
runtime_env.setdefault('TRUSTED_PROXY_CIDRS', '172.18.0.0/16')
runtime_env.setdefault('PLATFORM_DOMAINS', '46.249.98.190')
# Compose resolves ${...} before merging service environments. Supply the
# public edge settings to that interpolation as well as the backend process.
for key in ['DOMAIN_EDGE_HOST', 'PLATFORM_DOMAINS']:
    os.environ[key] = runtime_env[key]
os.environ.setdefault('PLATFORM_HTTP_HOST', '46.249.98.190')
# REL-02 reconciliation (S-1): TAPIN_TOKEN_KEY lives only in the running
# backend container's env — it is ABSENT from /opt/drup/.env (verified
# 2026-11-20) — while docker-compose.prod.yml requires it with
# ${...:?}. Export it into the interpolation environment from the live
# container (the same source the runtime override above draws from) so
# `compose config` can resolve it. If the live container ever lacks it,
# the `:?` fails loudly instead of silently shipping an empty key. The
# value is never printed: it travels only via the process environment
# into compose interpolation and the runtime file, as before.
if 'TAPIN_TOKEN_KEY' in runtime_env:
    os.environ['TAPIN_TOKEN_KEY'] = runtime_env['TAPIN_TOKEN_KEY']
EDGE_IMAGE = 'caddy:2-alpine@sha256:003a97808e781aa9f7e75cc2f43d34d461099031faf5e20faefb9f61126836f0'


def route_no_sni_to_primary(config, primary_host):
    policies = config['apps']['tls']['automation']['policies']
    primary = [policy for policy in policies if primary_host in policy.get('subjects', [])]
    if len(primary) != 1 or primary[0].get('on_demand') or '' in primary[0]['subjects']:
        raise RuntimeError('Missing dedicated primary certificate policy')
    # Browsers omit SNI for an IP URL. Match the empty name before the
    # on-demand policy, which would otherwise select the container's IP.
    primary[0]['subjects'].append('')
    return config


def build_edge_config(edge_environment):
    args = ['docker', 'run', '--rm']
    for key in edge_environment:
        args += ['-e', key]
    args += ['-v', str(release / 'Caddyfile') + ':/etc/caddy/Caddyfile:ro', EDGE_IMAGE,
             'caddy', 'adapt', '--config', '/etc/caddy/Caddyfile', '--adapter', 'caddyfile']
    config = json.loads(run(*args, capture=True, env={**os.environ, **edge_environment}))
    route_no_sni_to_primary(config, edge_environment['PLATFORM_HTTP_HOST'])
    path = release / 'Caddy.json'
    path.write_text(json.dumps(config) + '\n', encoding='utf-8')
    run('docker', 'run', '--rm', '-v', str(path) + ':/etc/caddy/Caddy.json:ro', EDGE_IMAGE,
        'caddy', 'validate', '--config', '/etc/caddy/Caddy.json')


runtime = {'services': {'backend': {'environment': runtime_env, 'stop_grace_period': '30s'}}}
if ssl:
    edge_env = {'ACME_EMAIL': '', 'PLATFORM_HTTP_HOST': '46.249.98.190', 'PLATFORM_PRIMARY_HTTPS_ONLY': 'false'}
    if previous_edge:
        actual_edge_env = dict(v.split('=', 1) for v in inspect(previous_edge[0])['Config']['Env'])
        edge_env.update({k: actual_edge_env[k] for k in edge_env if k in actual_edge_env})
    runtime['services']['edge'] = {'environment': edge_env}
    run('docker', 'pull', EDGE_IMAGE)
    build_edge_config(edge_env)
    compose_files.append(root / 'docker-compose.ssl.yml')
    # OBS-01 deploy-fix: the metrics-ports file MUST come after ssl.yml —
    # its frontend `!override` supersedes ssl.yml's `!reset []`. SSL-only:
    # the file declares edge ports and the edge service exists only with
    # ssl.yml, so a non-SSL deploy must not include it.
    compose_files.append(root / METRICS_PORTS_FILE)
compose_files.append(runtime_file)


def compose(*args, capture=False):
    cmd = ['docker', 'compose', '--project-directory', str(root)]
    for file in compose_files:
        cmd += ['-f', str(file)]
    return run(*cmd, *args, capture=capture)


def deploy_edge():
    compose('up', '-d', '--no-deps', '--force-recreate', 'edge')
    https_only = edge_env['PLATFORM_PRIMARY_HTTPS_ONLY'] == 'true'
    if not https_only and not require_primary_https:
        return
    primary = edge_env['PLATFORM_HTTP_HOST']
    secure_health = 'https://' + primary + '/api/health'

    def verify_tls():
        run('curl', '--fail', '--silent', '--show-error', '--retry', '4', '--retry-all-errors',
            '--retry-delay', '2', '--max-time', '8', '--output', '/dev/null', secure_health)

    # Start with the existing HTTP policy. Change it only once browsers can
    # reach the staged edge over TLS with a publicly trusted certificate.
    verify_tls()
    if not https_only:
        accepted_edge_env = {**edge_env, 'PLATFORM_PRIMARY_HTTPS_ONLY': 'true'}
        build_edge_config(accepted_edge_env)
        shutil.copy2(release / 'Caddy.json', root / 'Caddy.json')
        edge_env.update(accepted_edge_env)
        runtime_file.write_text(json.dumps(runtime))
        compose('up', '-d', '--no-deps', '--force-recreate', 'edge')
        verify_tls()
    redirect = run('curl', '--silent', '--show-error', '--max-time', '8',
                   '--output', '/dev/null', '--write-out', '%{http_code} %{redirect_url}',
                   'http://' + primary + '/api/health', capture=True)
    if redirect != '308 ' + secure_health:
        raise RuntimeError('Primary HTTP requests did not redirect to the trusted HTTPS endpoint')
    print('Primary HTTPS verified; HTTP redirects enabled', flush=True)


# BAK-01: the deploy backup is a call into the consolidated entry
# point (scripts/backup/backup-entrypoint.sh) — not a second inline
# pg_dump. The entry point writes the encrypted, manifested backup
# set (DB -Fc + plain dumps, uploads, config, Caddy volumes) into
# `backup`, verifies it (manifest + decrypt + gzip + pg_restore
# --list — G5; the old --list-only check passed 4/6 single-byte
# data corruptions the audit proved a real restore rejects), prunes
# deploy backups to the newest 5 (G10), and publishes the
# verified-success signal (kind='deploy'). It fails loudly (exit 1,
# no success marker, immediate failure notice) on a pg_dump failure,
# a missing encryption key, or a verification failure — the deploy
# aborts before anything is touched. Exit 3 (backup files verified
# good, success signal or off-site copy failed) is a loud WARNING,
# not an abort: the marker file + failure notice already fired and
# the 26 h staleness alert is the backstop. The saved files below
# are passed as --include: paths under root keep their relative
# path inside the backup dir, so the rollback below (which reads
# backup/<relative>) works unchanged; backend-environment.json
# (written to a temp file, not under root) lands in extra/.
saved_files = ['backend/bin/server-linux', 'nginx.conf', 'docker-compose.ssl.yml', 'Caddyfile', 'Caddy.json', '.deployed-commit',
               'scripts/publish-backup-success.sh', 'scripts/backup/backup-entrypoint.sh', 'scripts/backup/publish-backup-failure.sh',
               'scripts/backup/check-backup-health.sh', 'scripts/backup/restore-verify.sh',
               'backup-auto.sh', 'backup-daily.sh', 'docker-compose.observability.yml',
               # REL-02 reconciliation: the base + prod compose files are
               # shipped from the release (see the early ship below), so
               # the previous root copies must be in the backup or a
               # rollback cannot restore them.
               'docker-compose.yml', 'docker-compose.prod.yml',
               METRICS_PORTS_FILE]
existed = {}
for relative in saved_files:
    existed[relative] = (root / relative).exists()
entrypoint = release / 'scripts/backup/backup-entrypoint.sh'
if not entrypoint.is_file():
    entrypoint = root / 'scripts/backup/backup-entrypoint.sh'
if not entrypoint.is_file():
    raise SystemExit('BAK-01 backup entry point not found in the staged release or at /opt/drup/scripts/backup/backup-entrypoint.sh (the deploy coordinator installs it; refusing to deploy without a verified backup)')
env_backup = Path(tempfile.mkdtemp(prefix='sellersho-backend-env-')) / 'backend-environment.json'
env_backup.write_text(json.dumps(old_env))
includes = []
for relative in saved_files:
    if existed[relative]:
        includes += ['--include', str(root / relative)]
includes += ['--include', str(env_backup)]
backup_proc = subprocess.run([str(entrypoint), 'run', 'deploy', '--commit', commit, '--dest', str(backup)] + includes,
                             env={**os.environ, 'BAK_ROOT': str(root)})
if backup_proc.returncode == 3:
    print('WARNING: entry-point backup files verified good but the success signal / off-site copy failed (exit 3; marker left in the backup dir; continuing the deploy)', flush=True)
elif backup_proc.returncode != 0:
    raise SystemExit(f'Entry-point backup failed (exit {backup_proc.returncode}); deploy aborted before any change')
print('Backup:', backup, flush=True)
xray_path = xray_previous = None
dist_saved = changed = False
migration_stage = '/tmp/sellersho-migrate-' + commit[:12]
# MIG-02 step 4 — the backup-script ship list is defined here (not
# inside the try block) so the Mode B forward-repair path in the
# except block below can re-ship the new release's scripts.
backup_scripts = [
    (release / 'scripts/publish-backup-success.sh', root / 'scripts/publish-backup-success.sh'),
    (release / 'scripts/backup/backup-entrypoint.sh', root / 'scripts/backup/backup-entrypoint.sh'),
    (release / 'scripts/backup/publish-backup-failure.sh', root / 'scripts/backup/publish-backup-failure.sh'),
    (release / 'scripts/backup/check-backup-health.sh', root / 'scripts/backup/check-backup-health.sh'),
    (release / 'scripts/backup/restore-verify.sh', root / 'scripts/backup/restore-verify.sh'),
    (release / 'backup-auto.sh', root / 'backup-auto.sh'),
    (release / 'backup-daily.sh', root / 'backup-daily.sh'),
]


def health():
    for _ in range(45):
        try:
            with urllib.request.urlopen('http://127.0.0.1:8080/api/health', timeout=3) as response:
                value = json.load(response)
                if value.get('status') == 'ok' and value.get('postgres') == 'up' and value.get('redis') == 'up':
                    return
        except Exception:
            pass
        time.sleep(2)
    raise RuntimeError('Backend health check failed')


def verify_release_version(contract_path, release_manifest):
    route_hash = release_manifest['contract']['routesHash']
    expected = {
        'commit': release_manifest['commit'],
        'contractVersion': route_hash,
        'routesHash': route_hash,
    }
    with urllib.request.urlopen('http://127.0.0.1:8080/api/version', timeout=5) as response:
        backend_version = json.load(response)
    frontend_version = json.loads(contract_path.read_text(encoding='utf-8'))
    for component, version in (('backend', backend_version), ('frontend', frontend_version)):
        if not isinstance(version, dict) or any(version.get(key) != value for key, value in expected.items()):
            raise RuntimeError(f'Active {component} version differs from the release manifest')
    print(f"Release identity verified: {expected['commit'][:12]} ({route_hash[:12]})", flush=True)


# OBS-01 (C-04): the four Prometheus scrape jobs (prometheus.yml). The
# deploy verifies at least one target UP per job before it may claim the
# collector is running — Prometheus /-/healthy is true with zero targets
# up, so health alone proves nothing about scraping.
OBSERVABILITY_JOBS = ('sellersho-backend', 'caddy', 'nginx', 'node')
OBSERVABILITY_PROMETHEUS_URL = 'http://127.0.0.1:9091'
OBSERVABILITY_GRAFANA_URL = 'http://127.0.0.1:3000'

# OBS-01 (C-03 / B-1): minimum free disk before the collector step may
# pull images. ~1.5GB covers the ~324MB compressed collector images plus
# decompression plus the ~220MB release + deploy-backup overhead, with
# margin. This guard REFUSES only - it never deletes anything. Disk
# cleanup (pruning releases/backups) still needs the owner's explicit
# scope; see docs/operations/observability.md.
OBSERVABILITY_MIN_FREE_BYTES = 1_500_000_000


def require_free_space(path, minimum_bytes=OBSERVABILITY_MIN_FREE_BYTES, _disk_usage=None):
    """Fail the observability step when free disk on `path` is below
    `minimum_bytes`. Returns the free bytes when the check passes.
    Raises RuntimeError otherwise - the deploy aborts BEFORE any image
    is pulled, instead of failing mid-pull with a fuller disk.
    Self-contained (shutil only) so the dry-run harness can exec it
    standalone against a stubbed disk_usage.
    """
    disk_usage = _disk_usage or shutil.disk_usage
    free = disk_usage(path).free
    if free < minimum_bytes:
        raise RuntimeError(
            f'collector deploy refused: {free / 1e9:.2f}GB free on {path}, '
            f'need >= ~{minimum_bytes / 1e9:.1f}GB - prune releases/backups first')
    return free


def prometheus_targets_up(base_url, jobs, timeout=120, interval=5):
    """Poll Prometheus /api/v1/targets until every job in `jobs` has at
    least one target with health 'up'. Returns {job: up_count}.
    Raises RuntimeError if any job is still without an up target after
    `timeout` seconds. Self-contained (urllib/json/time only) so the
    dry-run harness can exec it standalone against a stub server.
    """
    deadline = time.monotonic() + timeout
    last = {job: 0 for job in jobs}
    last_error = None
    while True:
        try:
            with urllib.request.urlopen(base_url + '/api/v1/targets?state=active', timeout=5) as response:
                payload = json.load(response)
            last_error = None
        except Exception as error:
            payload = None
            last_error = error
        if payload is not None and payload.get('status') == 'success':
            counts = {job: 0 for job in jobs}
            for target in payload.get('data', {}).get('activeTargets', []):
                job = target.get('labels', {}).get('job')
                if job in counts and target.get('health') == 'up':
                    counts[job] += 1
            last = counts
            if all(counts.values()):
                return counts
        if time.monotonic() >= deadline:
            detail = f'up targets per job: {last}'
            if last_error is not None:
                detail += f'; last targets-query error: {type(last_error).__name__}: {last_error}'
            raise RuntimeError(f'Prometheus scraping not up for all jobs after {timeout}s ({detail})')
        time.sleep(interval)


def prometheus_rules_loaded(base_url):
    """Return how many alert rules Prometheus has loaded (via
    /api/v1/rules). Raises RuntimeError when zero are loaded: the deploy
    used to POST /-/reload and assume the rules were in, without ever
    checking. Self-contained, see prometheus_targets_up.
    """
    with urllib.request.urlopen(base_url + '/api/v1/rules', timeout=5) as response:
        payload = json.load(response)
    groups = payload.get('data', {}).get('groups', [])
    total = sum(len(group.get('rules', [])) for group in groups)
    if total == 0:
        raise RuntimeError('Prometheus has no alert rules loaded')
    return total


def prometheus_query_first_value(base_url, query):
    """Query Prometheus /api/v1/query and return the first sample's value
    as a float, or None when the query returns no series at all. The
    query is a plain metric name (no PromQL operators), so no URL
    quoting is needed. Self-contained (urllib/json only), see
    prometheus_targets_up — the dry-run harness execs it standalone
    against a stub server.
    """
    with urllib.request.urlopen(base_url + '/api/v1/query?query=' + query, timeout=5) as response:
        payload = json.load(response)
    results = payload.get('data', {}).get('result', [])
    if not results:
        return None
    return float(results[0]['value'][1])


# OBS-01 deploy-fix (Option B, audit F-12): the three loopback-only
# metrics publishes the merged compose config must contain before any
# container moves. The previous deploy failed at the scrape check because
# nothing had verified the merge — the prod.yml ship gap and the ssl.yml
# `!reset` wipe were both invisible until then. These functions are
# self-contained (json only) so the dry-run harness can exec them
# standalone against a stubbed `docker compose config --format json`.
# Only ports are inspected — environment is never read, let alone
# printed (the merged config carries interpolated secrets), and every
# error names services only, never a config value.
METRICS_PORTS_EXPECTED = {
    'backend': '127.0.0.1:9090:9090',
    'edge': '127.0.0.1:2019:2019',
    'frontend': '127.0.0.1:8081:8081',
}
METRICS_PORT_TARGETS = {9090, 2019, 8081}


def _metrics_published_ports(port_entries):
    """Normalise one service's `ports` list from `docker compose config
    --format json` to a set of "host_ip:published:target" strings. Dict
    entries carry host_ip/published/target keys; short strings pass
    through unchanged."""
    normalised = set()
    for entry in port_entries or []:
        if isinstance(entry, dict):
            normalised.add(f"{entry.get('host_ip') or ''}:{entry.get('published')}:{entry.get('target')}")
        else:
            normalised.add(str(entry))
    return normalised


def assert_metrics_ports(config_json, require_edge=True):
    """Fail unless the merged compose config publishes every expected
    metrics port on host loopback only. Raises RuntimeError listing only
    the missing service names, and refuses any metrics target published
    on a non-loopback host IP (including an empty host_ip, which means
    all interfaces). With require_edge=False the edge publish is not
    required (non-SSL deploys have no edge service)."""
    try:
        config = json.loads(config_json)
    except (TypeError, ValueError) as error:
        raise RuntimeError('merged compose config is not valid JSON') from error
    services = config.get('services') or {}
    missing = []
    for service, expected in METRICS_PORTS_EXPECTED.items():
        if service == 'edge' and not require_edge:
            continue
        ports = _metrics_published_ports((services.get(service) or {}).get('ports'))
        if expected not in ports:
            missing.append(service)
            continue
        for entry in ports:
            parts = entry.split(':')
            try:
                target = int(parts[-1])
            except ValueError:
                continue
            if target in METRICS_PORT_TARGETS:
                host_ip = ':'.join(parts[:-2])
                if host_ip != '127.0.0.1':
                    raise RuntimeError(
                        f'metrics port for {service} is published on a non-loopback host')
    if missing:
        raise RuntimeError(
            'merged compose config is missing metrics publishes for: ' + ', '.join(missing))
    return True


# REL-02 (S-5): extend the assert_metrics_ports pattern to the whole
# merged config — restart policies, resource limits, healthchecks and
# log rotation — run before migrations, next to assert_metrics_ports.
# Reads structural keys ONLY; environment values are never read, let
# alone printed (the merged config carries interpolated secrets), and
# every error names services / missing keys only.
REL02_MERGED_EXPECTED = {
    # service: (restart, memory limit, pids limit, healthcheck required?,
    #           cpus limit, memory reservation, stop_grace_period or None)
    # M-1: cpus / reservations / grace are asserted exactly, like
    # memory / pids — a future edit that unpins an image, changes a CPU
    # limit, or drops a grace period must fail this gate, not sail
    # through it. grace=None means the compose files declare no grace
    # period for that service (only backend/frontend/edge declare 30s).
    # NOTE: pids lives inside deploy.resources.limits — a top-level
    # `pids_limit` key alongside ANY deploy.resources block makes
    # `docker compose config` fail ("can't set distinct values", Compose
    # v5.6; caught by the real-merge validation 2026-11-20). Do not
    # re-add a top-level pids_limit.
    'postgres': ('always', '1024M', 256, True, '1.0', '256M', None),
    'redis': ('always', '640M', 256, True, '0.5', '64M', None),
    'backend': ('always', '512M', 512, True, '2.0', '128M', '30s'),
    'frontend': ('always', '128M', 256, True, '0.5', '32M', '30s'),
    'edge': ('unless-stopped', '256M', 256, True, '0.5', '64M', '30s'),
    'prometheus': ('unless-stopped', '256M', 256, True, '0.25', '64M', None),
    'grafana': ('unless-stopped', '256M', 256, True, '0.25', '64M', None),
    'node-exporter': ('unless-stopped', '64M', 256, True, '0.25', '16M', None),
    # nginx-exporter is scratch-based (no shell, no wget): it cannot
    # carry a container healthcheck, so none is required for it — its
    # health is proven by deploy_observability's nginx_up == 1 check.
    'nginx-exporter': ('unless-stopped', '64M', 256, False, '0.25', '16M', None),
}
REL02_APP_PORTS = {5432, 6379, 8080, 8081, 9090, 2019}
REL02_LOG_MAX_SIZE = '10m'
REL02_LOG_MAX_FILE = '3'
# Collector services live in docker-compose.observability.yml, which the
# app deploy never merges — the app-deploy assertion skips them
# (require_collectors=False); deploy_observability() asserts them on
# the observability merge instead.
REL02_COLLECTORS = frozenset({'prometheus', 'grafana', 'node-exporter', 'nginx-exporter'})


def _canonical_memory(value):
    """A memory value as a canonical "NM" string, or ''.

    `docker compose config --format json` emits memory as a BYTES
    string ('536870912'), while the repo files write '512M' —
    normalize both to the '512M' form before comparing.
    """
    if value is None:
        return ''
    text = str(value).strip()
    if text.isdigit():
        n = int(text)
        if n % (1024 * 1024) == 0:
            return f'{n // (1024 * 1024)}M'
        if n % 1024 == 0:
            return f'{n // 1024}K'
        return text
    return text.upper()


def _merged_memory_limit(service):
    """Memory limit as a canonical "NM" string from a merged-config
    service dict, or ''."""
    try:
        value = (service or {}).get('deploy', {}).get('resources', {}).get('limits', {}).get('memory')
    except AttributeError:
        return ''
    return _canonical_memory(value)


def _merged_memory_reservation(service):
    """Memory reservation as a canonical "NM" string from a merged-config
    service dict, or ''."""
    try:
        value = (service or {}).get('deploy', {}).get('resources', {}).get('reservations', {}).get('memory')
    except AttributeError:
        return ''
    return _canonical_memory(value)


def _merged_cpus_limit(service):
    """CPU limit as a canonical string from a merged-config service
    dict, or ''.

    `docker compose config --format json` emits cpus as a JSON number
    (2, 0.5, 0.25), while the repo files write quoted strings ('2.0',
    '0.5') — normalize both through float() so '2' == '2.0' == 2.
    """
    try:
        value = (service or {}).get('deploy', {}).get('resources', {}).get('limits', {}).get('cpus')
    except AttributeError:
        return ''
    if value is None:
        return ''
    try:
        return str(float(str(value).strip()))
    except (TypeError, ValueError):
        return ''


def _image_is_digest_pinned(image):
    """True when an image reference is digest-pinned: `name@sha256:<64
    lowercase hex>`. Plain string ops only (no `re`) so the harness's
    AST-extracted namespace — which provides `json` only — can exec
    this helper unchanged."""
    if not isinstance(image, str) or '@sha256:' not in image:
        return False
    digest = image.rsplit('@sha256:', 1)[1]
    return len(digest) == 64 and all(c in '0123456789abcdef' for c in digest)


def _merged_pids_limit(service):
    """PIDs limit from a merged-config service dict, or None."""
    try:
        return (service or {}).get('deploy', {}).get('resources', {}).get('limits', {}).get('pids')
    except AttributeError:
        return None


def assert_merged_config(config_json, require_edge=True, require_collectors=True):
    """Fail unless the merged compose config carries the REL-02 budgets.

    Checks, per service in REL02_MERGED_EXPECTED (edge skipped when
    require_edge is False, collectors skipped when require_collectors
    is False): restart policy, deploy.resources.limits.memory,
    deploy.resources.limits.pids, deploy.resources.limits.cpus,
    deploy.resources.reservations.memory, a healthcheck (except
    nginx-exporter, see above), and json-file log rotation (max-size
    10m, max-file 3). Also requires every service image to be
    digest-pinned (`name@sha256:<64 hex>`) and, where the compose
    files declare one, stop_grace_period to equal the declared value
    (backend/frontend/edge: 30s). Also fails when any app/metrics port
    (5432/6379/8080/8081/9090/2019) is published on a non-loopback
    host IP — the public edge (80/443) is the only service allowed a
    non-loopback publish. Raises RuntimeError naming only the
    offending services / keys; returns True on success.
    """
    try:
        config = json.loads(config_json)
    except (TypeError, ValueError) as error:
        raise RuntimeError('merged compose config is not valid JSON') from error
    services = config.get('services') or {}
    problems = []
    for service, (restart, memory, pids, need_health, cpus, reservation, grace) in REL02_MERGED_EXPECTED.items():
        if service == 'edge' and not require_edge:
            continue
        if service in REL02_COLLECTORS and not require_collectors:
            continue
        cfg = services.get(service)
        if cfg is None:
            problems.append(f'{service}: missing from merged config')
            continue
        if not _image_is_digest_pinned(cfg.get('image') or ''):
            problems.append(f'{service}: image is not digest-pinned')
        if cfg.get('restart') != restart:
            problems.append(f'{service}: restart is not {restart}')
        if _merged_memory_limit(cfg) != memory:
            problems.append(f'{service}: memory limit is not {memory}')
        if _merged_pids_limit(cfg) != pids:
            problems.append(f'{service}: pids limit is not {pids}')
        if _merged_cpus_limit(cfg) != cpus:
            problems.append(f'{service}: cpus limit is not {cpus}')
        if _merged_memory_reservation(cfg) != reservation:
            problems.append(f'{service}: memory reservation is not {reservation}')
        if grace is not None and str(cfg.get('stop_grace_period') or '') != grace:
            problems.append(f'{service}: stop_grace_period is not {grace}')
        if need_health and not cfg.get('healthcheck'):
            problems.append(f'{service}: healthcheck missing')
        logging = cfg.get('logging') or {}
        options = logging.get('options') or {}
        if logging.get('driver') != 'json-file':
            problems.append(f'{service}: logging driver is not json-file')
        if str(options.get('max-size')) != REL02_LOG_MAX_SIZE:
            problems.append(f'{service}: log max-size is not {REL02_LOG_MAX_SIZE}')
        if str(options.get('max-file')) != REL02_LOG_MAX_FILE:
            problems.append(f'{service}: log max-file is not {REL02_LOG_MAX_FILE}')
        for entry in cfg.get('ports') or []:
            if isinstance(entry, dict):
                target = entry.get('target')
                host_ip = entry.get('host_ip') or ''
            else:
                parts = str(entry).split(':')
                try:
                    target = int(parts[-1])
                except ValueError:
                    continue
                host_ip = ':'.join(parts[:-2]) if len(parts) > 2 else ''
            try:
                target = int(target)
            except (TypeError, ValueError):
                continue
            if target in REL02_APP_PORTS and host_ip != '127.0.0.1':
                problems.append(f'{service}: port {target} published on a non-loopback host')
    if problems:
        raise RuntimeError('merged compose config fails REL-02 checks: ' + '; '.join(problems))
    return True


def compose_observability(*args, capture=False):
    """docker compose with the collector file added (C-02). Only the
    observability step uses this: the app deploy and rollback use
    compose(), which never includes docker-compose.observability.yml,
    so an unset GRAFANA_ADMIN_PASSWORD cannot block them.

    capture mirrors compose() above: deploy_observability() calls this
    with capture=True for the `config --format json` assertion. Until
    the REL-02 deploy fix this parameter was missing, so that call
    raised TypeError — the assert line was added by REL-02 (69c8400)
    but never executed end to end before the b464d03 deploy failed
    earlier at the ssl assert, so the bug hid behind the first one.
    """
    cmd = ['docker', 'compose', '--project-directory', str(root)]
    for file in compose_files + [root / 'docker-compose.observability.yml']:
        cmd += ['-f', str(file)]
    return run(*cmd, *args, capture=capture)


def deploy_observability():
    """OBS-01 (D-01/G-01, C-02/C-04): start the collector stack and
    VERIFY it is actually scraping before the deploy may claim success.

    This step is FATAL to the deploy: on any failure it raises, no
    .deployed-commit is written, and the normal rollback runs. That is
    deliberate (C-04). The old design swallowed every failure into an
    "OBSERVABILITY DEPLOY WARNING" and stamped DEPLOYED anyway, so the
    most likely outcome of a failed collector pull (disk full, C-03) was
    a DEPLOYED stamp with no collector and a fuller disk. A collector
    failure must not silently pass as a shipped OBS-01.

    What is verified, in order: Prometheus /-/healthy, then at least one
    target UP per scrape job (sellersho-backend, caddy, nginx, node) via
    /api/v1/targets, then at least one alert rule loaded via /api/v1/rules,
    then the actual scraped DATA — nginx_up == 1 and a sellersho_ series
    returning data via /api/v1/query (target-up alone is hollow: the last
    deploy's `nginx: 1` hid nginx_up == 0) — then Grafana /api/health
    (database ok). /-/healthy alone proves nothing about scraping — it
    is true with zero targets up.

    No POST /-/reload anymore (C-09): --web.enable-lifecycle was removed
    from the Prometheus command (it also exposed POST /-/quit), and the
    container is force-recreated here, so rules load from disk at startup.

    On success the per-job result is recorded in
    /opt/drup/.observability-status.json, SEPARATE from .deployed-commit:
    .deployed-commit says the app deployed; the status file says the
    collector was verified scraping at deploy time. Grafana datasource
    health is NOT checked here — it needs Grafana admin auth, which this
    script does not carry; the dashboard actually returning data is a
    post-deploy check (F-7), not a deploy-time claim.
    """
    # OBS-01 (C-03 / B-1): refuse BEFORE pulling anything when the
    # disk cannot hold the collector images + deploy overhead.
    require_free_space(root)
    # REL-02 (S-5): assert the collector rows (budgets/healthchecks/
    # rotation) on the merged observability config before recreating
    # anything. The app-deploy assertion above cannot see these rows:
    # docker-compose.observability.yml is not in compose_files. Same
    # function, superset config (app + collectors); require_edge follows
    # the deploy's ssl flag, as in the app-deploy call.
    assert_merged_config(compose_observability('config', '--format', 'json', capture=True), require_edge=ssl)
    compose_observability('up', '-d', '--no-deps', '--force-recreate',
            'prometheus', 'grafana', 'node-exporter', 'nginx-exporter')
    for _ in range(30):
        try:
            with urllib.request.urlopen(OBSERVABILITY_PROMETHEUS_URL + '/-/healthy', timeout=2) as response:
                if response.status == 200:
                    break
        except Exception:
            time.sleep(2)
    else:
        raise RuntimeError('Prometheus did not become healthy')
    targets_up = prometheus_targets_up(OBSERVABILITY_PROMETHEUS_URL, OBSERVABILITY_JOBS)
    rules_loaded = prometheus_rules_loaded(OBSERVABILITY_PROMETHEUS_URL)
    # OBS-01 deploy-fix round 2 (critic S-1): target-up alone is hollow
    # — the last deploy's `nginx: 1` hid nginx_up == 0, because the
    # nginx job is UP whenever the exporter serves its own /metrics,
    # even while it scrapes nothing. Assert the actual DATA before the
    # status file may claim success: nginx_up must be 1, and at least
    # one sellersho_ series must return data. Both raise, failing the
    # deploy and triggering rollback — the same fatal semantics as the
    # target/rule checks above. (A sellersho_ gauge may legitimately
    # read 0 when idle; what matters here is that the series EXISTS —
    # None means the backend target serves no data at all.)
    nginx_up = prometheus_query_first_value(OBSERVABILITY_PROMETHEUS_URL, 'nginx_up')
    if nginx_up != 1:
        raise RuntimeError(
            f'nginx_up is {nginx_up}, expected 1: nginx stub_status data is not reaching Prometheus')
    sellersho_value = prometheus_query_first_value(OBSERVABILITY_PROMETHEUS_URL, 'sellersho_http_active')
    if sellersho_value is None:
        raise RuntimeError('no sellersho_ series returned data: backend metrics are not reaching Prometheus')
    with urllib.request.urlopen(OBSERVABILITY_GRAFANA_URL + '/api/health', timeout=5) as response:
        if json.load(response).get('database') != 'ok':
            raise RuntimeError('Grafana health check failed')
    status = {'ok': True, 'targets_up': targets_up, 'rules_loaded': rules_loaded,
              'nginx_up': nginx_up, 'sellersho_http_active': sellersho_value,
              'checked_at': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())}
    (root / '.observability-status.json').write_text(json.dumps(status) + '\n')
    print('Observability stack up and scraping: Prometheus '
          f'{OBSERVABILITY_PROMETHEUS_URL}, Grafana {OBSERVABILITY_GRAFANA_URL} '
          f'(loopback only, SSH tunnel); targets up per job: {targets_up}; '
          f'alert rules loaded: {rules_loaded}', flush=True)


try:
    # OBS-01 deploy-fix round 2 (critic B-1): ship the metrics-ports
    # file from the release to root BEFORE any `compose config`
    # invocation. compose_files already names root / METRICS_PORTS_FILE
    # (appended under `if ssl:` above), but on the first redeploy
    # carrying this fix the file does not exist on the server yet — the
    # old ship step (after the migrations, below) ran downstream of the
    # failing `compose('config', '--quiet')`, so the deploy could never
    # succeed and would fail identically on every retry. The backup loop
    # above already recorded the file's prior state in `existed` (it is
    # in saved_files), so rollback restores the backed-up copy — or
    # unlinks the file when it did not exist — exactly as for every
    # other shipped file. SSL-only, like the compose_files entry: the
    # file declares edge ports and the edge service exists only with
    # ssl.yml.
    if ssl:
        shutil.copy2(release / METRICS_PORTS_FILE, root / METRICS_PORTS_FILE)
    # SEC-01 (supervisor F-03, gate G-15): fail fast on a reintroduced
    # secret literal BEFORE any migration or compose change. Only the
    # non-Docker checks run here; the 2 Docker-dependent checks are
    # Docker-host-only and are not claimed by this step (see
    # scripts/run_secret_checks_nodocker.py).
    run('python3', str(release / 'scripts/run_secret_checks_nodocker.py'), str(release))
    # REL-02 reconciliation (S-1): ship the base + prod compose files
    # from the release to root BEFORE any `compose config` invocation,
    # so the deploy validates the config it will actually run. Until
    # this step the root files were server-only literals the repo never
    # reached (the audit's "prod.yml ship gap"): repo edits to pins,
    # budgets, healthchecks and logging were fiction. The ship runs
    # AFTER the secret check above (a literal credential in the release
    # files must fail there, before anything is copied to root) and the
    # previous root copies are in the backup via saved_files, so a
    # rollback restores them exactly as for every other shipped file.
    # The late ship loop below (success path) re-copies the same files;
    # that copy is now a no-op safety net, kept so a future reader sees
    # the full ship list in one place.
    for name in ['docker-compose.yml', 'docker-compose.prod.yml']:
        shutil.copy2(release / name, root / name)
    # REL-02 deploy fix (ssl ship-order bug — the failed b464d03
    # deploy): docker-compose.ssl.yml MUST ship here, before ANY
    # `compose config` invocation, exactly like base+prod above. Until
    # this fix it shipped only in the late loop below (after the
    # migrations), so assert_merged_config validated a MIXED config —
    # NEW base/prod + the OLD root ssl file — whose edge service has
    # no digest pin, budgets, healthcheck or logging, and the deploy
    # failed fail-closed at that assert on every SSL deploy. The ship
    # is ssl-gated, mirroring the compose_files entry above: a
    # non-SSL deploy has no edge service and must not gain one.
    # docker-compose.observability.yml ships here too (unconditionally,
    # like the late loop): its own assert runs inside
    # deploy_observability(), after the late loop, so it was not
    # live-broken — but leaving it late would leave the same
    # mixed-config hazard one refactor away from the collectors. Both
    # files are in saved_files above, so rollback restores the
    # previous root copies exactly as for base+prod.
    if ssl:
        shutil.copy2(release / 'docker-compose.ssl.yml', root / 'docker-compose.ssl.yml')
    shutil.copy2(release / 'docker-compose.observability.yml', root / 'docker-compose.observability.yml')
    # SEC-01 (critic F-02 #4): validate the merged compose BEFORE any
    # SQL migration runs, so a bad compose fails fast with no DB
    # change. The runtime file is written first: compose config reads
    # it as the last -f file.
    runtime_file.write_text(json.dumps(runtime))
    compose('config', '--quiet')
    # OBS-01 deploy-fix (F-12): assert the merged config actually
    # contains the three metrics publishes BEFORE any migration or
    # container move. Reads ports only; env is never printed. The
    # metrics file is ssl-only, so a non-SSL deploy skips this.
    if ssl:
        assert_metrics_ports(compose('config', '--format', 'json', capture=True), require_edge=True)
    # REL-02 (S-5): assert budgets/healthchecks/rotation + loopback-only
    # app ports on the same merged config, before any migration or
    # container move. The observability services are not in this merge
    # (compose_files excludes docker-compose.observability.yml), so
    # their rows are asserted inside deploy_observability() on the
    # observability merge instead (same function, superset config).
    # require_edge=ssl is exact: ssl is already OR'd with
    # bool(previous_edge) at the top, so a host running an edge
    # asserts it, and a host without one skips it — and the early ship
    # above guarantees the edge the assert sees is the release's, not
    # the previous root file's. (Non-SSL deploys have no edge service:
    # require_edge=False.)
    assert_merged_config(compose('config', '--format', 'json', capture=True),
                         require_edge=ssl, require_collectors=False)
    # MIG-02 step 3b — pre-deploy applied-set snapshot (version +
    # checksum, exactly what the rollback/forward-repair path needs to
    # know which rows this deploy added). Written before `migrate`
    # runs, into the deploy backup dir.
    applied_before = query_applied()
    backup.mkdir(parents=True, exist_ok=True)
    (backup / 'applied-before.json').write_text(
        json.dumps({'versions': sorted(applied_before),
                    'checksums': applied_before}, indent=2, sort_keys=True) + '\n')
    run('docker', 'exec', 'drup_backend', 'mkdir', '-p', migration_stage + '/migrations')
    run('docker', 'cp', str(release / 'backend/bin/migrate-linux'), 'drup_backend:' + migration_stage + '/migrate')
    prepare_migrator(run, 'drup_backend', migration_stage + '/migrate')
    run('docker', 'cp', str(release / 'backend/migrations') + '/.', 'drup_backend:' + migration_stage + '/migrations')
    # MIG-02 step 5 (D-5) — the staged manifest travels into the
    # container so `migrate` reads its per-migration lock/statement
    # timeout overrides from migrations.timeouts instead of relying on
    # CLI defaults (lock 30s / statement 10min).
    run('docker', 'cp', str(release / 'release.json'), 'drup_backend:' + migration_stage + '/release.json')
    run('docker', 'exec', 'drup_backend', migration_stage + '/migrate',
        '-dir', migration_stage + '/migrations', '-manifest', migration_stage + '/release.json', '-adopt-legacy')
    run('docker', 'exec', 'drup_backend', migration_stage + '/migrate',
        '-dir', migration_stage + '/migrations', '-manifest', migration_stage + '/release.json')
    run('docker', 'exec', 'drup_backend', 'rm', '-rf', migration_stage)
    missing_checksums = run('docker', 'exec', 'drup_postgres', 'psql', '-U', 'drup_admin', '-d', 'drup_db',
                            '-Atc', "SELECT count(*) FROM schema_migrations WHERE checksum IS NULL OR checksum=''",
                            capture=True).strip()
    if missing_checksums != '0':
        raise RuntimeError('Migration checksum adoption incomplete')
    # MIG-02 step 3b — post-run applied-set diff: exactly the rows
    # this deploy added. Recorded in the backup dir and in
    # rollback-point.json; the rollback path reads it, never guesses.
    applied_after = query_applied()
    newly_applied = sorted(set(applied_after) - set(applied_before))
    (backup / 'applied-after.json').write_text(
        json.dumps({'versions': sorted(applied_after),
                    'checksums': applied_after}, indent=2, sort_keys=True) + '\n')
    (backup / 'migrations-applied.json').write_text(
        json.dumps({'applied_by_this_deploy': newly_applied}, indent=2) + '\n')
    rollback_point['migrationsApplied'] = newly_applied
    write_rollback_point(backup, rollback_point)
    print(f"Migrations applied by this deploy: {len(newly_applied)}", flush=True)
    # MIG-02 step 5.3 — post-conditions, only for the migrations this
    # deploy applied. Any failure aborts the deploy (rollback/forward
    # repair below) instead of shipping a half-validated schema.
    checked = run_post_conditions(manifest, newly_applied)
    print(f"Post-conditions passed: {checked} checked for {len(newly_applied)} applied migration(s)", flush=True)
    changed = True
    # OBS-01 (C-02): ship the collector compose file with the release so
    # deploy_observability() can include it. It is also in saved_files
    # above, so rollback restores the previous version. Without this copy
    # the observability step would fail: the file lives only in the repo.
    # NOTE: METRICS_PORTS_FILE is NOT in this loop — critic B-1 moved
    # its ship to the top of this try block, before `compose config`,
    # because the config assertion needs the file to exist on disk.
    # REL-02: the base + prod compose files are likewise shipped early
    # (before `compose config`); this loop re-copies them as a no-op
    # safety net so the full ship list stays visible in one place.
    # REL-02 deploy fix: docker-compose.ssl.yml and
    # docker-compose.observability.yml are likewise shipped early now
    # (before `compose config`) — the ssl file's late ship was the
    # b464d03 failure (the assert validated a mixed config); this loop
    # re-copies them as the same no-op safety net.
    for name in ['Caddyfile', 'docker-compose.ssl.yml', 'docker-compose.observability.yml', 'docker-compose.yml', 'docker-compose.prod.yml']:
        shutil.copy2(release / name, root / name)
    if ssl:
        shutil.copy2(release / 'Caddy.json', root / 'Caddy.json')
    # OBS-01 (D-01/G-01): ship the collector config with the release so
    # the compose bind-mounts (./ops/observability/...) resolve on the
    # server. The previous tree is backed up for rollback below.
    obs_src, obs_dst = release / 'ops/observability', root / 'ops/observability'
    if obs_dst.exists():
        shutil.copytree(obs_dst, backup / 'ops-observability', dirs_exist_ok=True)
    shutil.copytree(obs_src, obs_dst, dirs_exist_ok=True)
    # OBS-01 (D-02/G-06, supervisor F-4) + BAK-01: ship the
    # backup-success publisher and the consolidated backup entry
    # point + its companions with the release. The entry point
    # expects the publisher at
    # /opt/drup/scripts/publish-backup-success.sh; until both files
    # are shipped the D-02 signal can never publish on prod.
    # Previous versions were preserved in the deploy backup dir
    # above via saved_files, and the rollback below restores them.
    (root / 'scripts').mkdir(parents=True, exist_ok=True)
    (root / 'scripts/backup').mkdir(parents=True, exist_ok=True)
    for source, destination in backup_scripts:
        if not source.is_file():
            raise RuntimeError(f'Backup script missing from release: {source}')
        shutil.copy2(source, destination)
        destination.chmod(0o755)
        if hashlib.sha256(source.read_bytes()).hexdigest() != hashlib.sha256(destination.read_bytes()).hexdigest():
            raise RuntimeError(f'Backup script copy mismatch: {destination}')
        if not os.access(destination, os.X_OK):
            raise RuntimeError(f'Backup script not executable: {destination}')
    print('Backup scripts shipped: scripts/publish-backup-success.sh, scripts/backup/backup-entrypoint.sh, scripts/backup/publish-backup-failure.sh, scripts/backup/check-backup-health.sh, scripts/backup/restore-verify.sh, backup-auto.sh, backup-daily.sh', flush=True)
    # Compose was already validated above (before migrations); it is
    # not re-run here.
    # Keep old hashed assets available to clients with already-open pages.
    assets = root / 'dist/assets'
    if assets.exists():
        for item in assets.iterdir():
            target = release / 'dist/assets' / item.name
            if item.is_file() and not target.exists():
                shutil.copy2(item, target)
    os.rename(root / 'dist', backup / 'dist')
    dist_saved = True
    shutil.copytree(release / 'dist', root / 'dist')
    shutil.copy2(release / 'nginx.conf', root / 'nginx.conf')
    new_binary = root / 'backend/bin/server-linux.next'
    shutil.copy2(release / 'backend/bin/server-linux', new_binary)
    new_binary.chmod(0o755)
    os.replace(new_binary, root / 'backend/bin/server-linux')
    # MIG-02 step 3c — the persistent-dir copy is manifest-driven:
    # copy exactly the manifest files (sha256-verified after copy),
    # remove unapplied extras, fail closed on an applied file the
    # manifest does not cover. The copied/removed sets are recorded
    # in rollback-point.json.
    persistent_copied, persistent_removed = copy_manifest_migrations(
        manifest, release, root / 'backend/migrations', applied_after)
    rollback_point['persistentFilesCopied'] = persistent_copied
    rollback_point['persistentFilesRemoved'] = persistent_removed
    write_rollback_point(backup, rollback_point)
    print(f"Persistent migration dir: {len(persistent_copied)} copied, "
          f"{len(persistent_removed)} removed", flush=True)
    if ssl and move_xray:
        xray_path = next((p for p in [Path('/usr/local/etc/xray/config.json'), Path('/etc/xray/config.json')] if p.exists()), None)
        if xray_path is None:
            raise RuntimeError('Xray configuration not found')
        xray_previous = xray_path.read_bytes()
        settings = json.loads(xray_previous)
        moved = [item for item in settings.get('inbounds', []) if item.get('port') == 443]
        if moved:
            with socket.socket() as probe:
                probe.bind(('0.0.0.0', 24443))
            (backup / 'xray-config.json').write_bytes(xray_previous)
            for item in moved:
                item['port'] = 24443
            xray_path.write_text(json.dumps(settings, indent=2))
            run('systemctl', 'restart', 'xray')
            run('systemctl', 'is-active', '--quiet', 'xray')
            for attempt in range(10):
                try:
                    with socket.create_connection(('127.0.0.1', 24443), timeout=2):
                        break
                except OSError:
                    if attempt == 9:
                        raise
                    time.sleep(1)
            if shutil.which('ufw') and 'Status: active' in run('ufw', 'status', capture=True):
                run('ufw', 'allow', '24443/tcp')
            print('Xray moved from 443 to 24443', flush=True)
    compose('up', '-d', '--no-deps', '--force-recreate', 'backend')
    health()
    if ssl:
        compose('stop', 'frontend')
    compose('up', '-d', '--no-deps', '--force-recreate', 'frontend')
    run('docker', 'exec', 'drup_frontend', 'nginx', '-t')
    if ssl:
        deploy_edge()
    run('curl', '--fail', '--silent', '--show-error', '--retry', '8', '--retry-connrefused',
        '--retry-delay', '2', '--output', '/dev/null', 'http://46.249.98.190/')
    health()
    verify_release_version(root / 'dist/contract.json', manifest)
    # MIG-02 step 2 — replaces the old hardcoded 202611090001
    # assertion: every manifest file must be recorded in
    # schema_migrations with a matching checksum.
    check_manifest_recorded(manifest, query_applied(), release / 'backend/migrations')
    # MIG-02 step 4 (D-3) — write-path readiness probe (deploy gate).
    # /api/health above is liveness-only; the probe exercises the
    # write paths a schema change can silently break, inside one
    # always-rolled-back transaction (no real orders are created).
    run_write_probe(release, 'current')
    deploy_observability()
    (root / '.deployed-commit').write_text(commit + '\n')
    # MIG-02 step 2 — terminal record: deployed. The ledger entry is
    # the audit trail the next rollback reads.
    rollback_point['status'] = 'deployed'
    write_rollback_point(backup, rollback_point)
    append_record(root, rollback_point)
    print('DEPLOYED', commit, 'BACKUP', backup, flush=True)
except Exception as deploy_error:
    # MIG-02 step 4 — schema-aware rollback. Decide BEFORE touching
    # anything: the pre-check compares the old binary's migration set
    # against the live schema_migrations + persistent dir and checks
    # the old binary against lastCompatibleRevision. A blind restore
    # is refused when the old binary cannot run this schema.
    print('Deployment failed; deciding rollback strategy (MIG-02 step 4).', flush=True)
    rollback_point['reason'] = f'{type(deploy_error).__name__}: {deploy_error}'
    # Pre-try fallback only: recomputed from the fresh applied set +
    # the pre-deploy snapshot below, once query_applied() succeeds.
    # (If the query itself raises, the pre-check fails closed to
    # `incompatible` and this value is never used.)
    schema_changed = bool(rollback_point.get('migrationsApplied'))
    old_commit = ''
    applied_now = {}
    applied_known = False
    old_release = Path('')
    verdict = None
    try:
        # The old commit is read from the LIVE root, not from the backup:
        # root/.deployed-commit still holds the previous commit until a
        # deploy succeeds and overwrites it, while the backup's copy is
        # encrypted (<relative>.enc) by the entry point (see
        # decrypt_saved_files). Reading the backup's plaintext copy
        # always missed and forced every rollback to Mode B.
        deployed_commit_path = root / '.deployed-commit'
        if deployed_commit_path.is_file():
            old_commit = deployed_commit_path.read_text().strip()
        if old_commit:
            old_release = root / 'releases' / old_commit
        applied_now = query_applied()
        applied_known = True
        # MIG-02 fix (supervisor F-1): `migrationsApplied` is only set
        # after a *successful* migrate run, so a mid-migrate failure
        # left it empty and the schema was misread as unchanged,
        # bypassing the floor check. Compare the fresh applied set
        # against the pre-deploy snapshot instead (falls back to the
        # rollback-point list when the snapshot is unreadable).
        schema_changed = compute_schema_changed(
            backup, applied_now, rollback_point.get('migrationsApplied'))
        old_set = {p.name for p in (old_release / 'backend/migrations').glob('*.up.sql')} if old_release.is_dir() else set()
        aliases = load_aliases(old_release) or load_aliases(release)
        verdict = rollback_precheck(applied_now, old_set, aliases, root / 'backend/migrations',
                                    release, old_release, manifest_checksums,
                                    last_compatible, old_commit, root,
                                    schema_changed=schema_changed)
    except Exception as error:
        # Cannot even assess compatibility (e.g. the DB is unreadable):
        # fail closed to forward repair — never a blind restore.
        verdict = {'verdict': 'incompatible',
                   'reason': f'rollback pre-check could not run ({type(error).__name__}: {error}); refusing a blind restore',
                   'stranded': [], 'old_ge_floor': False}
    print(f"Rollback pre-check: {verdict['verdict']} — {verdict['reason']}", flush=True)
    rollback_point['compatibility'] = verdict
    write_rollback_point(backup, rollback_point)
    try:
        subprocess.run(['docker', 'exec', 'drup_backend', 'rm', '-rf', migration_stage],
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if ssl and changed:
            # A configuration validation error must not prevent rollback.
            for edge in run('docker', 'ps', '--filter', 'label=com.docker.compose.project=drup',
                            '--filter', 'label=com.docker.compose.service=edge', '--format', '{{.ID}}', capture=True).splitlines():
                run('docker', 'stop', edge)
        if verdict['verdict'] in ('incompatible', 'unavailable'):
            # Mode B — forward repair (MIG-02 step 4): do NOT restore
            # the old binary. Keep the new release: ensure it is fully
            # shipped, recreate backend/frontend from it, and verify
            # identity, health and the write-path probe. Accepted data is never
            # erased (no DB restore). If this fails, the inner except
            # below prints ROLLBACK NEEDS ATTENTION.
            print('Mode B forward repair: keeping the new release; the old binary is not restored.', flush=True)
            rollback_point['repair'] = 'mode-b'
            if applied_known:
                mode_b_copied, mode_b_removed = copy_manifest_migrations(
                    manifest, release, root / 'backend/migrations', applied_now)
                rollback_point['persistentFilesCopied'] = mode_b_copied
                rollback_point['persistentFilesRemoved'] = mode_b_removed
            new_binary = root / 'backend/bin/server-linux.next'
            shutil.copy2(release / 'backend/bin/server-linux', new_binary)
            new_binary.chmod(0o755)
            os.replace(new_binary, root / 'backend/bin/server-linux')
            if not dist_saved:
                if (root / 'dist').exists():
                    os.rename(root / 'dist', backup / 'failed-dist')
                shutil.copytree(release / 'dist', root / 'dist')
            shutil.copy2(release / 'nginx.conf', root / 'nginx.conf')
            # REL-02: forward repair re-ships the base + prod compose
            # files too, exactly as the success path does — a repair
            # that leaves stale root compose files behind would run the
            # old pins/budgets/healthchecks under the new release.
            for name in ['Caddyfile', 'docker-compose.ssl.yml', 'docker-compose.observability.yml', 'docker-compose.yml', 'docker-compose.prod.yml']:
                shutil.copy2(release / name, root / name)
            if ssl:
                shutil.copy2(release / 'Caddy.json', root / 'Caddy.json')
            for source, destination in backup_scripts:
                if not source.is_file():
                    raise RuntimeError(f'Backup script missing from release: {source}')
                shutil.copy2(source, destination)
                destination.chmod(0o755)
            compose('up', '-d', '--no-deps', '--force-recreate', 'backend', 'frontend')
            if ssl:
                deploy_edge()
            health()
            verify_release_version(root / 'dist/contract.json', manifest)
            run_write_probe(release, 'current')
            rollback_point['status'] = 'forward-repaired'
            rollback_point['restoredCommit'] = None
            rollback_point['observabilityReverified'] = False
            write_rollback_point(backup, rollback_point)
            append_record(root, rollback_point)
            (root / '.deployed-commit').write_text(commit + '\n')
            print('FORWARD REPAIR SUCCEEDED: the new release is live and healthy '
                  '(health + write-path probe pass); the original failure was: '
                  f'{type(deploy_error).__name__}: {deploy_error}. '
                  'Observability was NOT re-verified by this repair — check it separately. '
                  'This script still exits non-zero so the failure is reviewed.', flush=True)
        else:
            # Compatible restore, with Mode A filing first when the
            # pre-check found applied-but-unfiled migrations: file them
            # sha-verified so runAutoMigrations can validate and the
            # old binary boots (the proven §2.2 boot failure).
            if verdict['verdict'] == 'mode-a':
                copied = file_stranded_migrations(verdict['stranded'], release, old_release,
                                                  root / 'backend/migrations')
                rollback_point['repair'] = 'mode-a'
                rollback_point['filesCopied'] = copied
                print(f"Mode A repair: filed {len(copied)} stranded migration(s) into the persistent dir: {copied}", flush=True)
            else:
                rollback_point['repair'] = 'none'
            # The saved files in the backup are encrypted (.enc) — see
            # decrypt_saved_files. Decrypt them before restoring.
            key_file = os.environ.get('BAK_KEY_FILE') or str(root / '.backup-encryption-key')
            decrypted = decrypt_saved_files(backup, saved_files, existed, key_file)
            for relative in saved_files:
                target = root / relative
                if existed[relative]:
                    restored = target.with_name(target.name + '.rollback')
                    shutil.copy2(decrypted / relative, restored)
                    # Preserve the replaced file's mode: the decrypted
                    # copy is 0600 (openssl output under umask 077) and
                    # the server binary / scripts must stay executable.
                    # Without this the restored binary is not runnable
                    # and the rollback dies at `compose up` (rehearsal
                    # R-1/R-2, Nov 20 2026).
                    if target.exists():
                        os.chmod(restored, target.stat().st_mode & 0o7777)
                    elif relative == 'backend/bin/server-linux' or relative.endswith('.sh'):
                        os.chmod(restored, 0o755)
                    os.replace(restored, target)
                elif target.exists():
                    target.unlink()
            # OBS-01: restore the previous collector config tree, if any.
            obs_backup = backup / 'ops-observability'
            if obs_backup.exists():
                shutil.rmtree(root / 'ops/observability', ignore_errors=True)
                shutil.copytree(obs_backup, root / 'ops/observability')
            if dist_saved:
                if (root / 'dist').exists():
                    os.rename(root / 'dist', backup / 'failed-dist')
                os.rename(backup / 'dist', root / 'dist')
            if xray_previous is not None:
                xray_path.write_bytes(xray_previous)
                run('systemctl', 'restart', 'xray')
            if old_runtime is not None:
                runtime_file.write_bytes(old_runtime)
            else:
                runtime_file.write_text(json.dumps({'services': {'backend': {'environment': {k: v for k, v in old_env.items() if k != 'PATH'}}}}))
            # OBS-01 (C-02): the observability file is deliberately NOT in
            # rollback_files either, so a rollback never interpolates
            # GRAFANA_ADMIN_PASSWORD and cannot fail on an unset password.
            rollback_files = [root / 'docker-compose.yml', root / 'docker-compose.prod.yml', runtime_file]
            if previous_edge and (root / 'docker-compose.ssl.yml').exists():
                rollback_files.insert(2, root / 'docker-compose.ssl.yml')
            # OBS-01 deploy-fix: roll back the metrics-ports file too, in
            # the same position it holds in compose_files (after ssl.yml),
            # so a rollback leaves the stack in its pre-deploy shape. The
            # file holds no secrets, so unlike the observability file it
            # is safe to include here (no GRAFANA_ADMIN_PASSWORD
            # interpolation).
            if previous_edge and (root / METRICS_PORTS_FILE).exists():
                rollback_files.insert(3, root / METRICS_PORTS_FILE)
            cmd = ['docker', 'compose', '--project-directory', str(root)]
            for file in rollback_files:
                cmd += ['-f', str(file)]
            run(*cmd, 'up', '-d', '--no-deps', '--force-recreate', 'backend', 'frontend')
            if previous_edge:
                run(*cmd, 'up', '-d', '--no-deps', '--force-recreate', 'edge')
            health()
            # MIG-02 step 4 (D-3) — the probe runs against the
            # rolled-back binary too: /api/health passing does not
            # prove the write paths work. A probe failure here means
            # the restored state is broken and needs a human.
            try:
                run_write_probe(release, 'current')
            except Exception as probe_error:
                rollback_point['status'] = 'rolled-back-probe-failed'
                rollback_point['restoredCommit'] = old_commit
                write_rollback_point(backup, rollback_point)
                append_record(root, rollback_point)
                print(f'ROLLBACK PROBE FAILED: the restored binary passes /api/health but the write-path probe fails: {probe_error}', flush=True)
                raise RuntimeError(f'rollback probe failed: {probe_error}') from probe_error
            rollback_point['status'] = 'rolled-back'
            rollback_point['restoredCommit'] = old_commit
            write_rollback_point(backup, rollback_point)
            append_record(root, rollback_point)
            print('Previous code restored; additive migration data retained. Database backup was not restored.', flush=True)
    except Exception as error:
        print('ROLLBACK NEEDS ATTENTION:', type(error).__name__, flush=True)
        # The rollback/repair itself failed: leave a ledger entry so
        # the point is visible as failed, not silently absent.
        rollback_point['status'] = 'rollback-failed'
        rollback_point['rollbackError'] = f'{type(error).__name__}: {error}'
        try:
            write_rollback_point(backup, rollback_point)
            append_record(root, rollback_point)
        except Exception:
            pass
    raise
