"""Deploy to the verified bind-mounted stack; preserve secrets and back up data.

Failure rolls back code/config, never the database over newly received orders.
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
import time
import urllib.request

from migration_history import classify_history
from deploy_migration_stage import prepare_migrator

root = Path(sys.argv[1]).resolve()
commit, ssl, move_xray = sys.argv[2], sys.argv[3] == '1', sys.argv[4] == '1'
if root != Path('/opt/drup') or not re.fullmatch(r'[a-f0-9]{40}', commit):
    raise SystemExit('Invalid installation or commit')
os.umask(0o077)
lock = open(root / '.deployment.lock', 'w')
fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
release = root / 'releases' / commit
backup = root / 'backups' / ('deploy-' + time.strftime('%Y%m%dT%H%M%SZ', time.gmtime()) + '-' + commit[:8])
backup.mkdir(parents=True)


def run(*args, capture=False, **kwargs):
    return subprocess.run(args, check=True, text=True, stdout=subprocess.PIPE if capture else None, **kwargs).stdout


def inspect(name):
    return json.loads(run('docker', 'inspect', name, capture=True))[0]


previous_edge = run('docker', 'ps', '--filter', 'label=com.docker.compose.project=drup',
                    '--filter', 'label=com.docker.compose.service=edge', '--format', '{{.ID}}', capture=True).splitlines()
ssl = ssl or bool(previous_edge)
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
runtime = {'services': {'backend': {'environment': runtime_env, 'stop_grace_period': '30s'}}}
if ssl:
    edge_env = {'ACME_EMAIL': '', 'PLATFORM_HTTP_HOST': '46.249.98.190'}
    if previous_edge:
        actual_edge_env = dict(v.split('=', 1) for v in inspect(previous_edge[0])['Config']['Env'])
        edge_env.update({k: actual_edge_env[k] for k in edge_env if k in actual_edge_env})
    runtime['services']['edge'] = {'environment': edge_env}
    run('docker', 'pull', 'caddy:2-alpine')
    run('docker', 'run', '--rm', '-e', 'PLATFORM_HTTP_HOST=46.249.98.190', '-v',
        str(release / 'Caddyfile') + ':/etc/caddy/Caddyfile:ro', 'caddy:2-alpine',
        'caddy', 'validate', '--config', '/etc/caddy/Caddyfile')
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


saved_files = ['backend/bin/server-linux', 'nginx.conf', 'docker-compose.ssl.yml', 'Caddyfile', '.deployed-commit',
               'scripts/publish-backup-success.sh', 'backup-auto.sh', 'docker-compose.observability.yml',
               METRICS_PORTS_FILE]
existed = {}
for relative in saved_files:
    src = root / relative
    existed[relative] = src.exists()
    if src.exists():
        dst = backup / relative
        dst.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, dst)
with open(backup / 'postgres.dump', 'wb') as dump:
    subprocess.run(['docker', 'exec', 'drup_postgres', 'pg_dump', '-U', 'drup_admin', '-d', 'drup_db', '-Fc'], stdout=dump, check=True)
with open(backup / 'postgres.dump', 'rb') as dump:
    subprocess.run(['docker', 'exec', '-i', 'drup_postgres', 'pg_restore', '--list'], stdin=dump, stdout=subprocess.DEVNULL, check=True)
(backup / 'backend-environment.json').write_text(json.dumps(old_env))
print('Backup:', backup, flush=True)
xray_path = xray_previous = None
dist_saved = changed = False
migration_stage = '/tmp/sellersho-migrate-' + commit[:12]


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


def compose_observability(*args):
    """docker compose with the collector file added (C-02). Only the
    observability step uses this: the app deploy and rollback use
    compose(), which never includes docker-compose.observability.yml,
    so an unset GRAFANA_ADMIN_PASSWORD cannot block them.
    """
    cmd = ['docker', 'compose', '--project-directory', str(root)]
    for file in compose_files + [root / 'docker-compose.observability.yml']:
        cmd += ['-f', str(file)]
    return run(*cmd, *args)


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
    run('docker', 'exec', 'drup_backend', 'mkdir', '-p', migration_stage + '/migrations')
    run('docker', 'cp', str(release / 'backend/bin/migrate-linux'), 'drup_backend:' + migration_stage + '/migrate')
    prepare_migrator(run, 'drup_backend', migration_stage + '/migrate')
    run('docker', 'cp', str(release / 'backend/migrations') + '/.', 'drup_backend:' + migration_stage + '/migrations')
    run('docker', 'exec', 'drup_backend', migration_stage + '/migrate',
        '-dir', migration_stage + '/migrations', '-adopt-legacy')
    run('docker', 'exec', 'drup_backend', migration_stage + '/migrate',
        '-dir', migration_stage + '/migrations')
    run('docker', 'exec', 'drup_backend', 'rm', '-rf', migration_stage)
    missing_checksums = run('docker', 'exec', 'drup_postgres', 'psql', '-U', 'drup_admin', '-d', 'drup_db',
                            '-Atc', "SELECT count(*) FROM schema_migrations WHERE checksum IS NULL OR checksum=''",
                            capture=True).strip()
    if missing_checksums != '0':
        raise RuntimeError('Migration checksum adoption incomplete')
    changed = True
    # OBS-01 (C-02): ship the collector compose file with the release so
    # deploy_observability() can include it. It is also in saved_files
    # above, so rollback restores the previous version. Without this copy
    # the observability step would fail: the file lives only in the repo.
    # NOTE: METRICS_PORTS_FILE is NOT in this loop — critic B-1 moved
    # its ship to the top of this try block, before `compose config`,
    # because the config assertion needs the file to exist on disk.
    for name in ['Caddyfile', 'docker-compose.ssl.yml', 'docker-compose.observability.yml']:
        shutil.copy2(release / name, root / name)
    # OBS-01 (D-01/G-01): ship the collector config with the release so
    # the compose bind-mounts (./ops/observability/...) resolve on the
    # server. The previous tree is backed up for rollback below.
    obs_src, obs_dst = release / 'ops/observability', root / 'ops/observability'
    if obs_dst.exists():
        shutil.copytree(obs_dst, backup / 'ops-observability', dirs_exist_ok=True)
    shutil.copytree(obs_src, obs_dst, dirs_exist_ok=True)
    # OBS-01 (D-02/G-06, supervisor F-4): ship the backup-success
    # publisher and the updated backup-auto.sh (step 5 hook) with the
    # release. backup-auto.sh expects the publisher at
    # /opt/drup/scripts/publish-backup-success.sh; until both files are
    # shipped the D-02 signal can never publish on prod. Previous
    # versions were preserved in the deploy backup dir above via
    # saved_files, and the rollback below restores them.
    (root / 'scripts').mkdir(parents=True, exist_ok=True)
    backup_scripts = [
        (release / 'scripts/publish-backup-success.sh', root / 'scripts/publish-backup-success.sh'),
        (release / 'backup-auto.sh', root / 'backup-auto.sh'),
    ]
    for source, destination in backup_scripts:
        if not source.is_file():
            raise RuntimeError(f'Backup script missing from release: {source}')
        shutil.copy2(source, destination)
        destination.chmod(0o755)
        if hashlib.sha256(source.read_bytes()).hexdigest() != hashlib.sha256(destination.read_bytes()).hexdigest():
            raise RuntimeError(f'Backup script copy mismatch: {destination}')
        if not os.access(destination, os.X_OK):
            raise RuntimeError(f'Backup script not executable: {destination}')
    print('Backup scripts shipped: scripts/publish-backup-success.sh, backup-auto.sh', flush=True)
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
    for migration in (release / 'backend/migrations').iterdir():
        target = root / 'backend/migrations' / migration.name
        if migration.is_file() and not target.exists():
            shutil.copy2(migration, target)
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
        compose('up', '-d', '--no-deps', '--force-recreate', 'edge')
    run('curl', '--fail', '--silent', '--show-error', '--retry', '8', '--retry-connrefused',
        '--retry-delay', '2', '--output', '/dev/null', 'http://46.249.98.190/')
    health()
    recorded = run('docker', 'exec', 'drup_postgres', 'psql', '-U', 'drup_admin', '-d', 'drup_db', '-Atc',
                   "SELECT COUNT(*) FROM schema_migrations WHERE version='202611090001_operations_automation.up.sql'", capture=True).strip()
    if recorded != '1':
        raise RuntimeError('Operations migration was not recorded')
    deploy_observability()
    (root / '.deployed-commit').write_text(commit + '\n')
    print('DEPLOYED', commit, 'BACKUP', backup, flush=True)
except Exception:
    print('Deployment failed; restoring previous code and runtime configuration.', flush=True)
    try:
        subprocess.run(['docker', 'exec', 'drup_backend', 'rm', '-rf', migration_stage],
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if ssl and changed:
            # A configuration validation error must not prevent rollback.
            for edge in run('docker', 'ps', '--filter', 'label=com.docker.compose.project=drup',
                            '--filter', 'label=com.docker.compose.service=edge', '--format', '{{.ID}}', capture=True).splitlines():
                run('docker', 'stop', edge)
        for relative in saved_files:
            target = root / relative
            if existed[relative]:
                restored = target.with_name(target.name + '.rollback')
                shutil.copy2(backup / relative, restored)
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
        print('Previous code restored; additive migration data retained. Database backup was not restored.', flush=True)
    except Exception as error:
        print('ROLLBACK NEEDS ATTENTION:', type(error).__name__, flush=True)
    raise
