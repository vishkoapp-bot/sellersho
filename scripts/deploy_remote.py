"""Deploy to the verified bind-mounted stack; preserve secrets and back up data.

Failure rolls back code/config, never the database over newly received orders.
"""
import fcntl
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
pending = [p for p in sorted((release / 'backend/migrations').glob('*.up.sql')) if p.name not in applied]
unknown_history = applied - {p.name for p in (release / 'backend/migrations').glob('*.up.sql')}
if unknown_history:
    raise SystemExit('Production migration history contains unreviewed aliases/unknown entries; isolated adoption rehearsal required')
print('Pending migrations:', [p.name for p in pending], flush=True)
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
runtime = {'services': {'backend': {'environment': runtime_env}}}
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
compose_files.append(runtime_file)


def compose(*args):
    cmd = ['docker', 'compose', '--project-directory', str(root)]
    for file in compose_files:
        cmd += ['-f', str(file)]
    return run(*cmd, *args)


saved_files = ['backend/bin/server-linux', 'nginx.conf', 'docker-compose.ssl.yml', 'Caddyfile', '.deployed-commit']
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


try:
    changed = True
    runtime_file.write_text(json.dumps(runtime))
    for name in ['Caddyfile', 'docker-compose.ssl.yml']:
        shutil.copy2(release / name, root / name)
    compose('config', '--quiet')
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
        compose('up', '-d', '--no-deps', 'edge')
    run('curl', '--fail', '--silent', '--show-error', '--retry', '8', '--retry-connrefused',
        '--retry-delay', '2', '--output', '/dev/null', 'http://46.249.98.190/')
    health()
    recorded = run('docker', 'exec', 'drup_postgres', 'psql', '-U', 'drup_admin', '-d', 'drup_db', '-Atc',
                   "SELECT COUNT(*) FROM schema_migrations WHERE version='202611090001_operations_automation.up.sql'", capture=True).strip()
    if recorded != '1':
        raise RuntimeError('Operations migration was not recorded')
    (root / '.deployed-commit').write_text(commit + '\n')
    print('DEPLOYED', commit, 'BACKUP', backup, flush=True)
except Exception:
    print('Deployment failed; restoring previous code and runtime configuration.', flush=True)
    try:
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
        rollback_files = [root / 'docker-compose.yml', root / 'docker-compose.prod.yml', runtime_file]
        if previous_edge and (root / 'docker-compose.ssl.yml').exists():
            rollback_files.insert(2, root / 'docker-compose.ssl.yml')
        cmd = ['docker', 'compose', '--project-directory', str(root)]
        for file in rollback_files:
            cmd += ['-f', str(file)]
        run(*cmd, 'up', '-d', '--no-deps', '--force-recreate', 'backend', 'frontend')
        if previous_edge:
            run(*cmd, 'up', '-d', '--no-deps', 'edge')
        health()
        print('Previous code restored; additive migration data retained. Database backup was not restored.', flush=True)
    except Exception as error:
        print('ROLLBACK NEEDS ATTENTION:', type(error).__name__, flush=True)
    raise
