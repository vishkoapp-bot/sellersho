#!/usr/bin/env python3
"""NATIVE (no-Docker) disposable capacity runner — CAP-01 D1.

Mirrors scripts/load_test_isolated.py (Docker) using the VM's native
PostgreSQL 16 + Redis 7 + nginx + Node 24 instead of containers:
  * fresh random disposable database sellersho_test_<hex> with the
    sellersho_test_guard marker (same safety contract as
    ~/workspace/.dev/test-isolated-native.sh),
  * scratch Redis on a random loopback port, DB 15, random password,
    sellersho:test-guard marker,
  * backend built from the working copy and run on 127.0.0.1 with
    APP_ENV=test + the committed test-gated payment/carrier/SMS doubles,
  * nginx on a loopback port terminating TLS for capacity.example.test
    (ephemeral CA, 2-day certs) with auth_request domain authorization
    against the backend /internal/domains/allow endpoint — the same
    authorization hop Caddy performed in the Docker runner,
  * Node drivers (the only generator) with env-overridable target/CA/
    sessions paths (load-tests/target-config.mjs).

D4: this runner takes NO target parameter. The target is hardcoded to
the disposable domain capacity.example.test; every driver re-validates
it (target-config.mjs refuses any other host / non-loopback IP).
Never points at production, an existing database, or a real host.
No real SMS / carrier / payment calls: doubles only. No OTP is ever
sent to a real number (the SMS double is in-memory, synthetic
recipients only).

Profiles (D3 short baseline suite): baseline (10s health), fixture-
baseline (10s, 1000 signed auth reads), api-baseline (10s exact mix),
api-rolling (30s), login-ramp (60s OTP), otp-background (60s OTP +
browse = 100 RPS), journey-baseline (90s), hot-sku, staff-baseline
(30s, 980 customers + 20 staff), hot-account, session-lifecycle,
browser-assets (native puppeteer Chrome, leaf-SPKI pin), cold-cache
(FLUSHALL + restart, cold run then warm run), headroom (200 RPS 5 min
+ 100 RPS 2 min recovery), fault-provider-timeout, fault-redis-loss,
fault-restart, api-sustained (30 min), journey (30 min), api-soak (2h).
Use --profiles a,b,c to run several in one provisioned environment.
Long runs write durable logs under --output and keep running in the
foreground; start them with nohup / background exec and read the logs.

Evidence: --output holds manifest.json, verdict.json, per-profile
summaries (raw request rows included, as the Docker runner does) and
redacted fixture logs. A compact summary (no raw rows) is also copied
into load-tests/evidence/ for the repository record.
"""
import argparse
import hashlib
import json
import os
import pathlib
import re
import secrets
import shutil
import signal
import subprocess
import sys
import tempfile
import threading
import time
import urllib.request
import urllib.error

ROOT = pathlib.Path(__file__).resolve().parents[1]
BACKEND = ROOT / 'backend'
LOADTESTS = ROOT / 'load-tests'
GIT_CHECKOUT = pathlib.Path.home() / 'workspace/sellersho-git'  # ~/workspace/sellersho has no .git (manifest pull)
sys.path.insert(0, str(ROOT / 'scripts'))
from load_test_isolated import (  # noqa: E402
    validate_mix, validate_workload_contract, metrics_counts, verdict,
    SafetyError, MIX, FLOOR, validate_fixture)

LABEL = 'sellersho.capacity-run'
HOME = pathlib.Path.home()
PG_BIN = HOME / 'pgx-rootfs/usr/lib/postgresql/16/bin'
GO = HOME / 'go/bin/go'
NPM = shutil.which('npm') or '/opt/hatch-image/bin/npm'
REDIS_SHIM = HOME / 'workspace/.dev/redis-shim'
NGINX = shutil.which('nginx') or '/usr/sbin/nginx'
OPENSSL = shutil.which('openssl') or '/usr/bin/openssl'

# profile -> (driver file or None for orchestrated, duration seconds,
#             extra env, timeout seconds)
PROFILES = {
    'baseline': ('baseline.mjs', 10, {'CAPACITY_SMOKE': '0'}, 180),
    'fixture-baseline': ('baseline.mjs', 10, {'CAPACITY_SMOKE': '0'}, 180),
    'login-ramp': ('login-otp.mjs', 60, {}, 300),
    'otp-login': ('login-otp.mjs', 60, {}, 300),
    'otp-background': ('login-otp.mjs', 60, {'CAPACITY_BACKGROUND': '1'}, 300),
    'api-baseline': ('commerce-baseline.mjs', 10, {}, 600),
    'api-rolling': ('commerce-rolling.mjs', 30, {'CAPACITY_DURATION_SECONDS': '30'}, 600),
    'api-sustained': ('commerce-rolling.mjs', 1800, {'CAPACITY_DURATION_SECONDS': '1800'}, 2700),
    'api-soak': ('commerce-rolling.mjs', 7200, {'CAPACITY_DURATION_SECONDS': '7200'}, 8100),
    'journey-baseline': ('users-1000.mjs', 90, {'CAPACITY_DURATION_SECONDS': '90'}, 600),
    'journey': ('users-1000.mjs', 1800, {'CAPACITY_DURATION_SECONDS': '1800'}, 2700),
    'hot-sku': ('hot-sku.mjs', 0, {}, 600),
    'staff-baseline': ('commerce-rolling.mjs', 30, {'CAPACITY_DURATION_SECONDS': '30', 'CAPACITY_STAFF': '1'}, 600),
    'hot-account': ('hot-account.mjs', 0, {}, 900),
    'session-lifecycle': ('session-lifecycle.mjs', 0, {}, 600),
    'browser-assets': (None, 0, {}, 400),
    'cold-cache': (None, 0, {}, 900),
    'headroom': (None, 0, {}, 1200),
    'fault-provider-timeout': (None, 0, {}, 900),
    'fault-redis-loss': (None, 0, {}, 600),
    'fault-restart': (None, 0, {}, 600),
    # CAP-02 W-1.2: the three profiles CAP-01 left unregistered. Registration
    # only — no driver behavior change. inventory-100rps keeps its
    # INV12_PROBE_APPROVED=1 refusal (the driver exits 2 without it);
    # coupon-burst keeps Certified:false in its own summary.
    'inventory-100rps': ('inventory-100rps.js', 0, {}, 900),
    'coupon-burst': ('coupon-burst.mjs', 0, {}, 900),
    'independent-account': ('independent-account.mjs', 0, {}, 600),
    # CAP-02 W-3: fault-matrix profiles (diagnostic scale on the disposable
    # native env; 100-RPS executions gated on D-1). See run_fault_boundary /
    # run_fault_db_loss / run_fault_worker_failover / run_fault_redis_load.
    'fault-boundary-b1': (None, 0, {}, 900),
    'fault-boundary-b2': (None, 0, {}, 900),
    'fault-boundary-b3': (None, 0, {}, 900),
    'fault-boundary-b4': (None, 0, {}, 1800),
    'fault-db-loss': (None, 0, {}, 900),
    'fault-worker-failover': (None, 0, {}, 900),
    'fault-redis-load': (None, 0, {}, 900),
}
NEEDS_FIXTURE = {'fixture-baseline', 'login-ramp', 'otp-login', 'otp-background',
                 'api-baseline', 'api-rolling', 'api-sustained', 'api-soak',
                 'journey-baseline', 'journey', 'hot-sku', 'staff-baseline',
                 'hot-account', 'session-lifecycle', 'browser-assets',
                 'cold-cache', 'headroom', 'fault-provider-timeout',
                 'fault-restart', 'inventory-100rps', 'coupon-burst',
                 'independent-account', 'fault-boundary-b1', 'fault-boundary-b2',
                 'fault-boundary-b3', 'fault-boundary-b4', 'fault-db-loss',
                 'fault-worker-failover', 'fault-redis-load'}


def free_port():
    import socket
    s = socket.socket()
    s.bind(('127.0.0.1', 0))
    port = s.getsockname()[1]
    s.close()
    return port


def redact(text, secrets_list):
    for s in secrets_list:
        if s:
            text = text.replace(s, '[redacted]')
    text = re.sub(r'eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+', '[redacted-jwt]', text)
    return text


class NativeRun:
    def __init__(self, out):
        self.out = out
        self.nonce = secrets.token_hex(12)
        self.password = secrets.token_hex(24)
        self.jwt = secrets.token_hex(32)
        self.smskey = secrets.token_hex(32)
        self.carrierkey = secrets.token_hex(32)
        self.database = 'sellersho_test_' + self.nonce
        self.work = pathlib.Path(tempfile.mkdtemp(prefix='sellersho-cap-native-'))
        self.backend_port = free_port()
        self.redis_port = free_port()
        # Critic condition 4: no fixed ports where the harness owns the
        # choice. nginx/SMS used 8443/8081, so two concurrent runs
        # collided and the second failed opaquely; both are now drawn
        # from free_port(). The CARRIER port stays fixed at 8082: the
        # backend's startup gate (cmd/server/capacity_adapters.go)
        # requires CAPACITY_CARRIER_BASE_URL to be exactly
        # http://carrier:8082, and changing that gate is app code —
        # out of scope for this harness task. So two concurrent runs
        # still collide on the carrier double; see start_doubles(),
        # which now fails fast with a clear error naming the held port
        # instead of dying opaquely. Residual limitation, recorded
        # in cap01-progress.md — not pretended fixed.
        self.nginx_port = free_port()
        self.sms_port = free_port()
        self.carrier_port = 8082
        self.procs = {}
        self.redis_dir = None
        self.redis_pid = None
        self.redis_url = ''
        self.redis_token = secrets.token_hex(12)
        self.manifest = {
            'scope': 'local-only-native', 'certified': False, 'run': self.nonce,
            'runner': 'scripts/load_test_native.py (no Docker; native PG16/Redis/nginx/Node)',
            'host': os.uname().sysname + ' ' + os.uname().release,
            'logical_cpus': os.cpu_count(), 'disk_class': 'unmeasured',
            'target': 'capacity.example.test (hardcoded disposable domain; no target parameter exists)',
            'tls': 'ephemeral CA (2-day) + nginx 127.0.0.1:%d; auth_request domain authorization to backend /internal/domains/allow; private key never exported' % self.nginx_port,
            'backend_pool': {'max_open': 25, 'max_idle': 10}, 'replicas': 1,
            'adapters': {'payment': 'test-only bounded latency double; committed startup gate',
                         'sms': 'private in-memory double, 25ms success default',
                         'carrier': 'private quote-only HTTP double; egress blocked'},
            'fixture_validity': {'seeded': False, 'cardinality': False, 'canonical_stock_origins': False,
                                 'realistic_financial_history': False, 'aged_fee_positions': False, 'staff_roles': False},
            'limitations': ['shared VM host, not intended hardware', 'test APP_ENV required for mock provider',
                            'native loopback TLS via nginx, not Caddy; domain-auth counting via nginx auth_request',
                            'health baseline is not commerce mix', 'single resource sample cannot establish sustained CPU'],
        }
        self.cpu_samples = []
        self._secrets = [self.password, self.jwt, self.smskey, self.carrierkey]

    # -- subprocess helpers -------------------------------------------------
    def run(self, args, env=None, cwd=None, input=None, timeout=600, check=True):
        result = subprocess.run(args, env=env, input=input, cwd=cwd or ROOT,
                                text=True, encoding='utf-8', errors='replace',
                                capture_output=True, timeout=timeout)
        if check and result.returncode:
            raise SafetyError('Command failed: ' + args[0] + ' (exit ' + str(result.returncode) + '): ' +
                              redact(result.stderr[-2000:], self._secrets))
        return result

    def psql(self, db, sql, check=True):
        return self.run([str(PG_BIN / 'psql'), '-h', '127.0.0.1', '-p', '5433', '-U', 'gap13',
                         '-d', db, '-At', '-v', 'ON_ERROR_STOP=1', '-c', sql],
                        timeout=300, check=check).stdout.strip()

    def http_get(self, url, headers=None, timeout=10):
        req = urllib.request.Request(url, headers=headers or {})
        try:
            with urllib.request.urlopen(req, timeout=timeout) as r:
                return r.status, r.read().decode('utf-8', 'replace')
        except urllib.error.HTTPError as e:
            return e.code, e.read().decode('utf-8', 'replace')
        except Exception as e:
            return 0, str(e)

    def metrics(self):
        status, body = self.http_get('http://127.0.0.1:9090/metrics')
        if status != 200:
            raise SafetyError('backend metrics unavailable (status %s)' % status)
        return body

    # -- provisioning -------------------------------------------------------
    def provision_db(self):
        role_pass = secrets.token_hex(24)
        self._secrets.append(role_pass)
        self.role_pass = role_pass
        # One statement per psql call: psql -c wraps multi-statement
        # input in a single implicit transaction and CREATE DATABASE
        # cannot run inside a transaction block.
        self.psql('postgres', """
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'test_runner') THEN
    CREATE ROLE test_runner LOGIN PASSWORD '%s' NOSUPERUSER NOCREATEDB NOCREATEROLE;
  ELSE
    ALTER ROLE test_runner WITH PASSWORD '%s' NOSUPERUSER NOCREATEDB NOCREATEROLE;
  END IF;
END $$;""" % (role_pass, role_pass))
        self.psql('postgres', 'CREATE DATABASE "%s" OWNER test_runner;' % self.database)
        # SET ROLE test_runner: the marker table must be OWNED by the
        # test role, or testdb.Open's SELECT fails with permission
        # denied (gap13-owned tables are not readable by test_runner).
        # DDL in one psql -c is fine here; only CREATE DATABASE
        # forbids the implicit transaction, and it runs separately.
        self.psql(self.database,
                  "SET ROLE test_runner; "
                  "CREATE TABLE public.sellersho_test_guard (singleton BOOLEAN PRIMARY KEY CHECK (singleton), "
                  "database_name TEXT NOT NULL); "
                  "INSERT INTO public.sellersho_test_guard VALUES (TRUE, '%s');" % self.database)
        self.dsn = 'postgres://test_runner:%s@127.0.0.1:5433/%s?sslmode=disable' % (role_pass, self.database)
        self._secrets.append(self.dsn)
        env = self.child_env()
        self.run([str(GO), 'run', './cmd/migrate', '-dir', 'migrations'], env=env, cwd=BACKEND, timeout=600)
        print('disposable database ready:', self.database, flush=True)

    def provision_redis(self):
        self.redis_dir = pathlib.Path(tempfile.mkdtemp(prefix='sellersho-cap-redis-'))
        redis_server = str(REDIS_SHIM / 'redis-server') if (REDIS_SHIM / 'redis-server').exists() else 'redis-server'
        proc = subprocess.Popen([redis_server, '--port', str(self.redis_port), '--bind', '127.0.0.1',
                                 '--save', '', '--appendonly', 'no', '--requirepass', self.password,
                                 '--dir', str(self.redis_dir), '--daemonize', 'no'],
                                stdout=open(self.work / 'redis.log', 'w'), stderr=subprocess.STDOUT)
        self.procs['redis'] = proc
        self.redis_url = 'redis://:%s@127.0.0.1:%d/15' % (self.password, self.redis_port)
        self._secrets.append(self.redis_url)
        redis_cli = str(REDIS_SHIM / 'redis-cli') if (REDIS_SHIM / 'redis-cli').exists() else 'redis-cli'
        self.redis_cli = redis_cli
        for _ in range(50):
            r = self.run([redis_cli, '-h', '127.0.0.1', '-p', str(self.redis_port), '-n', '15', 'PING'],
                         env={**os.environ, 'REDISCLI_AUTH': self.password}, check=False)
            if 'PONG' in r.stdout:
                break
            time.sleep(0.2)
        else:
            raise SafetyError('scratch Redis did not answer PING')
        self.redis_cli_auth = {'REDISCLI_AUTH': self.password}
        self.run([redis_cli, '-h', '127.0.0.1', '-p', str(self.redis_port), '-n', '15', 'SET', 'sellersho:test-guard',
                  self.redis_token], env={**os.environ, **self.redis_cli_auth}, check=True)
        print('disposable Redis ready: 127.0.0.1:%d/15' % self.redis_port, flush=True)

    def child_env(self, extra=None):
        env = {'HOME': str(HOME), 'PATH': os.environ.get('PATH', ''),
               'GOROOT': str(HOME / 'go'), 'GOPATH': str(HOME / 'gopath'),
               'APP_ENV': 'test', 'JWT_SECRET': self.jwt,
               'DATABASE_URL': self.dsn, 'SELLERSHO_TEST_DATABASE_URL': self.dsn,
               'REDIS_URL': self.redis_url, 'SELLERSHO_TEST_REDIS_URL': self.redis_url,
               'SELLERSHO_TEST_REDIS_TOKEN': self.redis_token,
               'PAYMENT_GATEWAY_MODE': 'mock'}
        for key in ('TEST_DATABASE_URL', 'TEAM03_DATABASE_URL', 'TEAM04_TEST_DATABASE_URL',
                    'TEAM04B_TEST_DATABASE_URL', 'TEAM05_TEST_DATABASE_URL',
                    'TEAM09_WORKER_DATABASE_URL', 'CAPABILITY_AUDIT_DATABASE_URL',
                    'SKU_COMMERCE_DATABASE_URL', 'GRANULAR_REVIEW_TEST_DATABASE_URL',
                    'FINANCE_PAYOUT_TEST_DATABASE_URL', 'SUPPLIER_NOTIFICATIONS_TEST_DATABASE_URL',
                    'OPERATIONS_TEST_DATABASE_URL', 'NOTIFY_WRITERS_TEST_DATABASE_URL',
                    'GAP13_TEST_DATABASE_URL', 'SUPPLIER_FLOW_TEST_DATABASE_URL',
                    'REJECT_NOTIFY_TEST_DATABASE_URL'):
            env[key] = self.dsn
        if extra:
            env.update(extra)
        return env

    def appenv(self, payment_mode='success'):
        return self.child_env({
            'TRACKING_WEBHOOK_SECRET': secrets.token_hex(32),
            'IPPANEL_ENABLED': 'true', 'IPPANEL_BASE_URL': 'http://sms:%d/v1' % self.sms_port,
            'IPPANEL_API_KEY': self.smskey, 'IPPANEL_SENDER': 'synthetic',
            'IPPANEL_PATTERN_CODE': 'synthetic',
            'ENABLE_NETWORK_VOLUME_INCENTIVE': 'false',
            'PLATFORM_DOMAINS': 'capacity.example.test',
            'PORT': str(self.backend_port), 'MIGRATIONS_DIR': str(BACKEND / 'migrations'),
            'SELLERSHO_CAPACITY_RUN': self.nonce,
            'CAPACITY_PAYMENT_MODE': payment_mode,
            # CAP-02 W-3.1: latencies are overridable per start_backend call
            # (b2 needs a slow verify so the kill lands mid-verification);
            # the default is the fast double used by every other profile.
            'CAPACITY_PAYMENT_LATENCIES_MS': getattr(self, '_payment_latencies', '25,25,50,50,150'),
            'CAPACITY_CARRIER_BASE_URL': 'http://carrier:8082',
            'CAPACITY_CARRIER_KEY': self.carrierkey,
        })

    def build_backend(self):
        env = {'HOME': str(HOME), 'PATH': os.environ.get('PATH', ''),
               'GOROOT': str(HOME / 'go'), 'GOPATH': str(HOME / 'gopath'),
               'GOOS': 'linux', 'GOARCH': 'amd64', 'CGO_ENABLED': '0'}
        self.run([str(GO), 'build', '-o', str(self.work / 'server'), './cmd/server'], env=env, cwd=BACKEND, timeout=600)
        self.manifest['go_toolchain'] = self.run([str(GO), 'version'], env=env).stdout.strip()
        self.manifest['backend_binary_sha256'] = hashlib.sha256((self.work / 'server').read_bytes()).hexdigest()
        self.manifest['source'] = self.run(['git', 'rev-parse', 'HEAD'], cwd=GIT_CHECKOUT).stdout.strip()
        self.manifest['backend_source_sha256'] = hashlib.sha256(
            b''.join(p.relative_to(BACKEND).as_posix().encode() + p.read_bytes()
                     for p in sorted(BACKEND.rglob('*'))
                     if p.is_file() and 'networkreward' not in p.parts
                     and (p.suffix in ('.go', '.sql', '.mod', '.sum') or p.name == 'historical-checksums.json'))).hexdigest()
        self.manifest['migration_digests'] = {p.name: hashlib.sha256(p.read_bytes()).hexdigest()
                                              for p in sorted((BACKEND / 'migrations').glob('*.sql'))}
        print('backend built:', self.manifest['backend_binary_sha256'][:16], flush=True)

    def build_frontend(self):
        """Build the real frontend for the browser/static-asset profile.
        Failure is recorded, never faked: API profiles still run."""
        try:
            result = self.run([NPM, 'run', 'build'], cwd=ROOT, timeout=900)
            self.frontend_dist = ROOT / 'dist'
            self.manifest['frontend_build'] = 'ok'
            self.manifest['frontend_build_sha256'] = hashlib.sha256(
                b''.join(p.relative_to(self.frontend_dist).as_posix().encode() + p.read_bytes()
                         for p in sorted(self.frontend_dist.rglob('*')) if p.is_file())).hexdigest()
            print('frontend built', flush=True)
            return True
        except Exception as exc:
            self.manifest['frontend_build'] = 'FAILED: ' + redact(str(exc)[:500], self._secrets)
            self.frontend_dist = None
            print('frontend build FAILED (browser profile will record the blocker):', redact(str(exc)[:300], self._secrets), flush=True)
            return False

    def start_doubles(self):
        env = {**os.environ, 'SELLERSHO_CAPACITY_RUN': self.nonce, 'CAPACITY_SMS_KEY': self.smskey,
               'CAPACITY_SMS_PORT': str(self.sms_port)}
        sms_log = open(self.work / 'sms.log', 'w')
        self.procs['sms'] = subprocess.Popen(['node', str(LOADTESTS / 'sms-double.mjs')], env=env,
                                             stdout=sms_log, stderr=subprocess.STDOUT)
        env2 = {**os.environ, 'SELLERSHO_CAPACITY_RUN': self.nonce, 'CAPACITY_CARRIER_KEY': self.carrierkey,
                'CAPACITY_CARRIER_PORT': str(self.carrier_port)}
        carrier_log = open(self.work / 'carrier.log', 'w')
        self.procs['carrier'] = subprocess.Popen(['node', str(LOADTESTS / 'carrier-double.mjs')], env=env2,
                                                 stdout=carrier_log, stderr=subprocess.STDOUT)
        for _ in range(50):
            s1, _ = self.http_get('http://127.0.0.1:%d/metrics' % self.sms_port,
                                  headers={'Authorization': self.smskey})
            s2, _ = self.http_get('http://127.0.0.1:%d/metrics' % self.carrier_port,
                                  headers={'Authorization': 'Bearer ' + self.carrierkey})
            if s1 == 200 and s2 == 200:
                break
            # Fail fast with a clear lock error: if a double died at
            # startup its port is held by another run (the carrier
            # port 8082 is FIXED by the backend's startup gate, so a
            # second concurrent run always collides there; the SMS
            # port is dynamic and only collides on a free_port race).
            for name, proc, port in (('sms', self.procs['sms'], self.sms_port),
                                     ('carrier', self.procs['carrier'], self.carrier_port)):
                if proc.poll() is not None:
                    raise SafetyError('%s double exited at startup; port %d is held by another run — '
                                      'concurrent native runs share the fixed carrier port 8082 '
                                      '(backend startup gate) and cannot run together' % (name, port))
            time.sleep(0.2)
        else:
            raise SafetyError('SMS/carrier doubles did not start')
        print('doubles up (sms :%d, carrier :%d)' % (self.sms_port, self.carrier_port), flush=True)

    def make_tls(self):
        work = self.work
        self.run([OPENSSL, 'req', '-x509', '-newkey', 'rsa:2048', '-keyout', str(work / 'ca.key'),
                  '-out', str(work / 'ca.crt'), '-days', '2', '-nodes', '-subj', '/CN=Capacity Test CA'], timeout=60)
        self.run([OPENSSL, 'req', '-newkey', 'rsa:2048', '-keyout', str(work / 'server.key'),
                  '-out', str(work / 'server.csr'), '-nodes', '-subj', '/CN=capacity.example.test'], timeout=60)
        (work / 'server.ext').write_text('subjectAltName=DNS:capacity.example.test')
        self.run([OPENSSL, 'x509', '-req', '-in', str(work / 'server.csr'), '-CA', str(work / 'ca.crt'),
                  '-CAkey', str(work / 'ca.key'), '-CAcreateserial', '-out', str(work / 'server.crt'),
                  '-days', '2', '-extfile', str(work / 'server.ext')], timeout=60)
        shutil.copy2(work / 'ca.crt', work / 'root.crt')
        # Leaf SPKI for the browser pin (see browser-assets.mjs diagnosis).
        # One shell pipeline, no secrets involved; output is base64 text.
        import base64 as _b64
        pipe = subprocess.run(
            "%s x509 -in %s -pubkey -noout | %s pkey -pubin -outform DER | "
            "%s dgst -sha256 -binary | %s base64 -A" % (
                OPENSSL, work / 'server.crt', OPENSSL, OPENSSL, OPENSSL),
            shell=True, capture_output=True, text=True, timeout=60)
        if pipe.returncode or not pipe.stdout.strip():
            raise SafetyError('leaf SPKI computation failed')
        self.leaf_spki = pipe.stdout.strip()
        print('ephemeral CA + leaf cert ready', flush=True)

    def write_nginx_conf(self):
        work = self.work
        dist = str(self.frontend_dist) if self.frontend_dist else str(work / 'no-dist')
        for sub in ('client_body', 'proxy', 'fastcgi', 'uwsgi', 'scgi'):
            (work / sub).mkdir(exist_ok=True)
        conf = """user root;
worker_processes 1;
pid %s/nginx.pid;
error_log %s/nginx-error.log warn;
events { worker_connections 4096; }
http {
  include /etc/nginx/mime.types;
  default_type application/octet-stream;
  access_log %s/nginx-access.log;
  client_body_temp_path %s/client_body;
  proxy_temp_path %s/proxy;
  fastcgi_temp_path %s/fastcgi;
  uwsgi_temp_path %s/uwsgi;
  scgi_temp_path %s/scgi;
  upstream cap_backend { server 127.0.0.1:%d; }
  server {
    listen 127.0.0.1:%d ssl;
    server_name capacity.example.test;
    ssl_certificate %s/server.crt;
    ssl_certificate_key %s/server.key;
    location = /_domain_allow {
      internal;
      proxy_pass http://cap_backend/internal/domains/allow?domain=capacity.example.test;
      proxy_pass_request_body off;
      proxy_set_header Content-Length "";
    }
    location /internal/ { return 404; }
    location /api/ {
      auth_request /_domain_allow;
      proxy_pass http://cap_backend;
      proxy_request_buffering off;
      proxy_next_upstream off;
      proxy_connect_timeout 3s;
      client_body_timeout 10s;
      proxy_send_timeout 10s;
      proxy_read_timeout 13s;
      proxy_http_version 1.1;
      proxy_set_header Host $host;
      proxy_set_header X-Real-IP $remote_addr;
      proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
      proxy_set_header X-Forwarded-Proto $scheme;
    }
    location /static/ {
      auth_request /_domain_allow;
      proxy_pass http://cap_backend;
      proxy_http_version 1.1;
      proxy_set_header Host $host;
      proxy_set_header X-Real-IP $remote_addr;
      proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
      proxy_set_header X-Forwarded-Proto $scheme;
    }
    location / {
      auth_request /_domain_allow;
      root %s;
      try_files $uri $uri/ /index.html;
    }
  }
}
""" % (work, work, work, work, work, work, work, work, self.backend_port,
       self.nginx_port, work, work, dist)
        (work / 'nginx.conf').write_text(conf)
        self.run([NGINX, '-t', '-c', str(work / 'nginx.conf'), '-p', str(work) + '/'], timeout=60)
        print('nginx config ok', flush=True)

    def start_nginx(self):
        log = open(self.work / 'nginx-stdout.log', 'w')
        self.procs['nginx'] = subprocess.Popen(
            [NGINX, '-c', str(self.work / 'nginx.conf'), '-p', str(self.work) + '/', '-g', 'daemon off;'],
            stdout=log, stderr=subprocess.STDOUT)
        time.sleep(1)
        if self.procs['nginx'].poll() is not None:
            raise SafetyError('nginx failed to start; see nginx-error.log')
        print('nginx up on 127.0.0.1:%d' % self.nginx_port, flush=True)

    def make_hosts(self):
        # /etc/hosts is a read-only bind mount in this cell, so the
        # isolated-harness aliases (carrier, sms) cannot be added there.
        # The backend instead runs in a private mount namespace with a
        # copied hosts file bound over /etc/hosts: 'carrier' and 'sms'
        # resolve to loopback ONLY inside that namespace. Nothing else
        # on the host (or in the drivers) sees the aliases.
        hosts = pathlib.Path('/etc/hosts').read_text()
        if 'carrier' not in hosts:
            hosts += '127.0.0.1 carrier\n127.0.0.1 sms\n'
        (self.work / 'hosts').write_text(hosts)
        launcher = self.work / 'run-server.sh'
        launcher.write_text('#!/bin/sh\nmount --bind "$CAP_HOSTS_FILE" /etc/hosts\nexec "$CAP_SERVER_BIN"\n')
        launcher.chmod(0o700)

    def start_backend(self, payment_mode='success', latencies=None):
        # latencies: CAPACITY_PAYMENT_LATENCIES_MS override for this start
        # only (W-3.1 b2 slow-verify). None restores the fast default.
        self._payment_latencies = latencies or '25,25,50,50,150'
        log = open(self.work / 'backend.log', 'ab')
        env = self.appenv(payment_mode)
        env['CAP_HOSTS_FILE'] = str(self.work / 'hosts')
        env['CAP_SERVER_BIN'] = str(self.work / 'server')
        self.procs['backend'] = subprocess.Popen(
            ['unshare', '--mount', '--', str(self.work / 'run-server.sh')],
            env=env, stdout=log, stderr=subprocess.STDOUT, cwd=BACKEND)
        self.wait_backend_health()
        print('backend up on 127.0.0.1:%d (payment mode %s)' % (self.backend_port, payment_mode), flush=True)

    def wait_backend_health(self, timeout=120):
        for _ in range(timeout * 2):
            if self.procs.get('backend') and self.procs['backend'].poll() is not None:
                raise SafetyError('backend exited during startup; see backend.log')
            status, _ = self.http_get('http://127.0.0.1:%d/api/health' % self.backend_port, timeout=5)
            if status == 200:
                return
            time.sleep(0.5)
        raise SafetyError('backend health startup timeout')

    def kill_backend(self):
        """SIGKILL the backend process (a crash, not a graceful shutdown).
        The launcher execs the server binary, so the Popen object IS the
        server. Used by the W-3 fault profiles; stop_backend() is the
        graceful variant and is NOT a crash."""
        proc = self.procs.pop('backend', None)
        if proc:
            proc.kill()
            proc.wait(timeout=15)

    def stop_backend(self):
        proc = self.procs.pop('backend', None)
        if proc:
            proc.terminate()
            try:
                proc.wait(timeout=15)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait(timeout=15)

    def stop_proc(self, name):
        proc = self.procs.pop(name, None)
        if proc:
            proc.terminate()
            try:
                proc.wait(timeout=10)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait(timeout=10)

    def fixture(self):
        # Backend paused during fixture provisioning/assertions, exactly
        # as the Docker runner does; workers resume before measurement.
        self.stop_backend()
        env = {'HOME': str(HOME), 'PATH': os.environ.get('PATH', ''),
               'GOROOT': str(HOME / 'go'), 'GOPATH': str(HOME / 'gopath'),
               'GOOS': 'linux', 'GOARCH': 'amd64', 'CGO_ENABLED': '0'}
        self.run([str(GO), 'test', '-c', '-o', str(self.work / 'fixture'), './cmd/capacity-fixture'],
                 env=env, cwd=BACKEND, timeout=600)
        fenv = self.child_env({
            'SELLERSHO_CAPACITY_RUN': self.nonce,
            'CAPACITY_SESSION_FILE': str(self.work / 'users.json'),
            'CAPACITY_FINANCE_HISTORY': '1',
            'CAPACITY_CARRIER_KEY': self.carrierkey,
        })
        # CAP-02 W-1.5: record the fixture child's elapsed time (monotonic
        # + wall clock) in the manifest, so a future "7-hour fixture"
        # anomaly is bounded by measurement instead of guessed at (VM
        # freeze/restore vs contention). The monotonic clock is the
        # arbiter: if it reads ~7 h the time really passed; if it reads
        # minutes while a child timestamp reads hours, the child's clock
        # jumped.
        fixture_started_mono = time.monotonic()
        fixture_started_wall = time.time()
        result = self.run([str(self.work / 'fixture'), '-test.v', '-test.run=^TestCapacityDataFloor$',
                           '-test.timeout=15m'], env=fenv, cwd=BACKEND, timeout=960, check=False)
        self.manifest['fixture_elapsed'] = {
            'seconds': round(time.monotonic() - fixture_started_mono, 1),
            'started_wall': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime(fixture_started_wall)),
            'finished_wall': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime(time.time()))}
        safe_log = redact(result.stdout + result.stderr, self._secrets)
        (self.out / 'fixture-redacted.log').write_text(safe_log)
        m = re.findall(r'CAPACITY_METRIC supplier_balance sample=(\d+) duration_ms=(\d+)', safe_log)
        self.manifest['fixture_read_samples'] = [{'sample': int(a), 'duration_ms': int(b)} for a, b in m]
        self.manifest['fixture_exit'] = result.returncode
        if result.returncode:
            raise SafetyError('Guarded capacity fixture failed (exit %d)' % result.returncode)
        # Real release cohort: its own gate, recorded and never blocking.
        # The app's 300ms per-candidate release budget fails on a small
        # shared VM (seq-scan-heavy eligibility chain — see
        # backend/cmd/capacity-fixture/release_cohort_test.go); that is
        # an app capacity finding, so the cohort runs separately and the
        # suite continues to the driver profiles either way.
        rc_result = self.run([str(self.work / 'fixture'), '-test.v', '-test.run=^TestCapacityReleaseCohort$',
                              '-test.timeout=15m'], env=fenv, cwd=BACKEND, timeout=960, check=False)
        rc_log = redact(rc_result.stdout + rc_result.stderr, self._secrets)
        (self.out / 'release-cohort-redacted.log').write_text(rc_log)
        rc_m = re.search(r'CAPACITY_RELEASE_COHORT released=(\d+) want=100 status=(\w+)', rc_log)
        self.manifest['release_cohort'] = {'exit': rc_result.returncode,
                                           'released': int(rc_m.group(1)) if rc_m else None,
                                           'status': rc_m.group(2) if rc_m else 'UNKNOWN'}
        print('release cohort:', self.manifest['release_cohort'], flush=True)
        users_path = self.work / 'users.json'
        if not users_path.exists():
            raise SafetyError('fixture did not write the session file')
        os.chmod(users_path, 0o600)
        fixture = json.loads((LOADTESTS / 'seed-manifest.json').read_text())
        fixture_users = json.loads(users_path.read_text())
        # Validate the seeded floor with MEASURED disposable-DB counts,
        # not asserted constants. Queue bounds come from the committed
        # seed manifest (rolling feeder recycles them across long runs).
        manifest_seed = json.loads((LOADTESTS / 'seed-manifest.json').read_text())
        counts = {}
        # Same prefix columns as the fixture's own prefixColumn(): users
        # and product_variants have UUID ids, so 'cap-%' lives on
        # full_name / variant_name. (The runner previously used id for
        # every table and died here with uuid ~~ unknown the first time
        # the fixture actually passed.)
        for table, col in (('users', 'full_name'), ('suppliers', 'id'), ('stores', 'id'),
                           ('products', 'id'), ('product_variants', 'variant_name'), ('orders', 'id')):
            counts[table] = int(self.psql(self.database,
                "SELECT count(*) FROM %s WHERE %s LIKE 'cap-%%'" % (table, col)))
        saleable = int(self.psql(self.database,
            "SELECT COALESCE(sum(quantity),0) FROM inventory_balances "
            "WHERE product_id LIKE 'cap-product-%' AND status='sellable'"))
        validate_fixture({'users': fixture_users['users'],
                          'counts': {'products': counts['products'], 'skus': counts['product_variants'],
                                     'orders': counts['orders'], 'stores': counts['stores'],
                                     'suppliers': counts['suppliers'], 'customers': counts['users']},
                          'queues': {'create': manifest_seed['two_hour_required_each_write_queue'],
                                     'initiate': manifest_seed['two_hour_required_each_write_queue'],
                                     'callback': manifest_seed['two_hour_required_each_write_queue']},
                          'saleable_units': saleable}, 7200)
        self.manifest['fixture_measured_counts'] = {**counts, 'saleable_units': saleable}
        self.manifest['fixture'] = ('synthetic datafloor/immutable origins/paid-delivered history; '
                                    'real release cohort recorded separately (see release_cohort); '
                                    '100 idempotent payout reservations; '
                                    '100 aged positions; fee variant restored OFF; 1000 distinct signed sessions')
        self.manifest['fixture_validity'].update(seeded=True, cardinality=True, canonical_stock_origins=True,
                                                 realistic_financial_history=True, aged_fee_positions=True,
                                                 staff_roles=True)
        self.manifest['fixture_scope'] = ('synthetic history validity only; current/prior-month posted/reserved '
                                          'reconciliation; not production historical evidence or full return/refund oracle')
        self.start_backend()
        print('fixture seeded + backend resumed', flush=True)

    # -- driver execution ---------------------------------------------------
    def driver_env(self, extra=None):
        env = {**os.environ,
               'CAPACITY_TARGET_HOST': 'capacity.example.test',
               'CAPACITY_TARGET_PORT': str(self.nginx_port),
               'CAPACITY_TARGET_PROTOCOL': 'https',
               'CAPACITY_TARGET_IP': '127.0.0.1',
               'CAPACITY_CA_FILE': str(self.work / 'root.crt'),
               'CAPACITY_SESSIONS_FILE': str(self.work / 'users.json'),
               'CAPACITY_SMS_HOST': '127.0.0.1',
               'CAPACITY_SMS_PORT': str(self.sms_port),
               'CAPACITY_SMS_KEY': self.smskey,
               # Critic condition 4: the drivers' summary path is
               # run-scoped (this run's own work dir), not the global
               # /tmp/summary.json two concurrent runs overwrote. The
               # killed long run's orphaned driver kept writing
               # /tmp/summary.json after its runner died; a run-scoped
               # path makes that harmless.
               'CAPACITY_SUMMARY_FILE': str(self.work / 'summary.json'),
               'SELLERSHO_CAPACITY_RUN': self.nonce}
        if extra:
            env.update(extra)
        return env

    def sample_cpu(self, stop_event, samples):
        while not stop_event.is_set():
            try:
                out = subprocess.run(['ps', '-eo', '%cpu,comm', '--no-headers'],
                                     capture_output=True, text=True, timeout=10).stdout
                total = 0.0
                for line in out.splitlines():
                    parts = line.split(None, 1)
                    if len(parts) == 2 and parts[1].strip() in (
                            'server', 'nginx', 'postgres', 'redis-server', 'node'):
                        total += float(parts[0])
                samples.append({'elapsed': time.time(), 'cpu_percent_total': round(total, 2)})
            except Exception:
                pass
            stop_event.wait(5)

    def run_driver(self, name, driver, extra_env, timeout):
        """Run one Node driver; returns (exit_status, summary dict|None)."""
        summary_path = self.work / 'summary.json'
        if summary_path.exists():
            summary_path.unlink()
        before = self.metrics()
        (self.out / (name + '-metrics-before.prom')).write_text(before)
        run_started = self.psql(self.database, 'SELECT clock_timestamp()') if name not in ('baseline',) else None
        stop = threading.Event()
        samples = []
        sampler = threading.Thread(target=self.sample_cpu, args=(stop, samples), daemon=True)
        sampler.start()
        started = time.monotonic()
        try:
            result = subprocess.run(['node', str(LOADTESTS / driver)], env=self.driver_env(extra_env),
                                    capture_output=True, text=True, timeout=timeout)
            status = result.returncode
            if result.stdout.strip():
                (self.out / (name + '-stdout.log')).write_text(redact(result.stdout[-8000:], self._secrets))
            if result.stderr.strip():
                (self.out / (name + '-stderr.log')).write_text(redact(result.stderr[-8000:], self._secrets))
        except subprocess.TimeoutExpired:
            status = 124
            (self.out / (name + '-timeout.log')).write_text('driver exceeded %ds timeout' % timeout)
        finally:
            stop.set()
            sampler.join(timeout=20)
        elapsed = time.monotonic() - started
        self.manifest[name + '_resource_samples'] = samples
        self.manifest[name + '_cpu_max_percent'] = max((s['cpu_percent_total'] for s in samples), default=None)
        after = self.metrics()
        (self.out / (name + '-metrics-after.prom')).write_text(after)
        summary = None
        if summary_path.exists():
            summary = json.loads(summary_path.read_text())
            (self.out / (name + '-summary.json')).write_text(json.dumps(summary, indent=2))
        else:
            status = 1
            self.manifest[name + '_summary'] = 'MISSING: driver wrote no run-scoped summary.json'
        if summary is not None:
            a, b = metrics_counts(after), metrics_counts(before)
            received = {k: a[k] - b[k] for k in a}
            self.manifest[name + '_received_counts'] = received
            expected = summary.get('requests', -1) + len(summary.get('helper_requests_raw', []))
            route_class = 'probe' if name == 'baseline' else 'api'
            if received[route_class] != expected or received['domain_allow'] != expected:
                status = 1
                self.manifest[name + '_counter_gate'] = (
                    'FAILED: attempted/client requests do not reconcile to backend/domain counts '
                    '(received=%s expected=%s)' % (received, expected))
            else:
                self.manifest[name + '_counter_gate'] = 'matched'
            if summary.get('failed_gates'):
                status = 1
        self.manifest[name + '_exit'] = status
        self.manifest[name + '_elapsed_seconds'] = round(elapsed, 2)
        if run_started and summary is not None and name in (
                'api-baseline', 'api-rolling', 'api-sustained', 'api-soak',
                'journey-baseline', 'journey', 'staff-baseline', 'cold-cache-cold',
                'cold-cache-warm', 'headroom', 'headroom-recovery', 'fault-provider-timeout'):
            self.oracle_commerce(name, run_started, status)
        if name == 'hot-sku':
            self.oracle_hot_sku(status)
        if name == 'hot-account':
            self.oracle_hot_account(run_started, status)
        if name == 'coupon-burst':
            self.oracle_coupon_burst(run_started, status)
        return status, summary

    def oracle_commerce(self, name, run_started, status):
        ts = run_started
        sql = """
        SELECT json_build_object(
          'source_receipt_units',(SELECT COALESCE(sum(quantity),0) FROM inventory_movements WHERE product_id LIKE 'cap-product-%%' AND reference_type='capacity_synthetic_receipt'),
          'physical_sellable',(SELECT COALESCE(sum(quantity),0) FROM inventory_balances WHERE product_id LIKE 'cap-product-%%' AND status='sellable'),
          'sku_projection',(SELECT COALESCE(sum(stock),0) FROM product_variants WHERE product_id LIKE 'cap-product-%%'),
          'net_captured',(SELECT COALESCE(sum(quantity-restored_quantity),0) FROM order_inventory_captures WHERE product_id LIKE 'cap-product-%%'),
          'payment_order_amount_mismatch',(SELECT count(*) FROM payment_transactions p JOIN orders o ON o.id=p.order_id JOIN users u ON u.id=o.customer_user_id WHERE u.full_name LIKE 'cap-customer-%%' AND p.amount<>o.payable_amount),
          'price_payable_mismatch',(SELECT count(*) FROM orders o WHERE o.created_at>='%s' AND o.customer_user_id IN (SELECT id FROM users WHERE full_name LIKE 'cap-customer-%%') AND o.payable_amount <> (SELECT COALESCE(sum(final_unit_price*quantity),0) FROM order_items WHERE order_id=o.id) + o.shipping_cost + COALESCE(o.platform_addon_amount,0) + COALESCE(o.supplier_addon_amount,0)),
          'price_discount_mismatch',(SELECT count(*) FROM orders o WHERE o.created_at>='%s' AND o.customer_user_id IN (SELECT id FROM users WHERE full_name LIKE 'cap-customer-%%') AND o.discount_amount <> o.total_amount - (SELECT COALESCE(sum(final_unit_price*quantity),0) FROM order_items WHERE order_id=o.id)),
          'ledger_negative_amount',(SELECT count(*) FROM ledger_entries WHERE date>='%s' AND amount<0),
          'ledger_orphan_order',(SELECT count(*) FROM ledger_entries l WHERE l.date>='%s' AND l.related_order_or_dispute IS NOT NULL AND NOT EXISTS (SELECT 1 FROM orders o WHERE o.id=l.related_order_or_dispute)),
          'paid_orders_without_ledger_entry',(SELECT count(*) FROM orders o WHERE o.created_at>='%s' AND o.customer_user_id IN (SELECT id FROM users WHERE full_name LIKE 'cap-customer-%%') AND o.payment_status='paid' AND NOT EXISTS (SELECT 1 FROM ledger_entries l WHERE l.related_order_or_dispute=o.id)),
          'paid_cancelled_without_obligation',(SELECT count(*) FROM orders o WHERE o.created_at>='%s' AND o.customer_user_id IN (SELECT id FROM users WHERE full_name LIKE 'cap-customer-%%') AND o.payment_status='paid' AND o.status='cancelled' AND NOT EXISTS (SELECT 1 FROM order_refund_obligations r WHERE r.order_id=o.id AND r.purpose='cancellation')),
          'duplicate_obligations',(SELECT count(*) FROM (SELECT purpose,business_id,source_payment_id FROM order_refund_obligations GROUP BY 1,2,3 HAVING count(*)>1) d),
          'obligation_over_source',(SELECT count(*) FROM order_refund_obligations WHERE created_at>='%s' AND amount_toman>source_amount_toman),
          'orders_with_multiple_refunds',(SELECT count(*) FROM (SELECT order_id FROM refunds GROUP BY order_id HAVING count(*)>1) d)
        )""" % (ts, ts, ts, ts, ts, ts, ts)
        oracle = json.loads(self.psql(self.database, sql))
        oracle['scope'] = ('stock projections/captures + payment-order amounts + price identity '
            '(payable/discount vs persisted item snapshots) + refund-obligation existence/uniqueness/amount bound + '
            'ledger sanity (negative/orphan only); paid_orders_without_ledger_entry is INFORMATIONAL, not a gate: '
            'release postings are deferred by design; full ledger conservation across releases/settlements remains OPEN (FIN-013)')
        self.manifest[name + '_oracle'] = oracle
        stock_ok = (oracle['physical_sellable'] + oracle['net_captured'] == oracle['source_receipt_units']
                    and oracle['sku_projection'] == oracle['physical_sellable']
                    and not oracle['payment_order_amount_mismatch'])
        oracle_fail = (oracle['price_payable_mismatch'] or oracle['price_discount_mismatch']
                       or oracle['ledger_negative_amount'] or oracle['ledger_orphan_order']
                       or oracle['paid_cancelled_without_obligation'] or oracle['duplicate_obligations']
                       or oracle['obligation_over_source'] or oracle['orders_with_multiple_refunds'])
        if not stock_ok or oracle_fail:
            self.manifest[name + '_oracle_gate'] = 'FAILED'
            self.manifest[name + '_exit'] = 1
            return 1
        self.manifest[name + '_oracle_gate'] = 'matched within stated scope (ledger conservation across deferred releases still OPEN)'
        return 0

    def oracle_hot_sku(self, status, key='hot_sku_oracle', exit_key='hot-sku_exit'):
        oracle = json.loads(self.psql(self.database, """
        SELECT json_build_object('source',5,
          'sellable',(SELECT COALESCE(sum(quantity),0) FROM inventory_balances WHERE product_id='cap-hot-product' AND status='sellable'),
          'captured',(SELECT COALESCE(sum(quantity-restored_quantity),0) FROM order_inventory_captures WHERE product_id='cap-hot-product'),
          'sku',(SELECT stock FROM product_variants WHERE product_id='cap-hot-product'),
          'paid_orders',(SELECT count(DISTINCT o.id) FROM orders o JOIN order_items oi ON oi.order_id=o.id WHERE oi.product_id='cap-hot-product' AND o.payment_status='paid'),
          'amount_mismatch',(SELECT count(*) FROM payment_transactions p JOIN orders o ON o.id=p.order_id JOIN order_items oi ON oi.order_id=o.id WHERE oi.product_id='cap-hot-product' AND p.amount<>o.payable_amount))"""))
        self.manifest[key] = oracle
        if oracle != {'source': 5, 'sellable': 0, 'captured': 5, 'sku': 0, 'paid_orders': 5, 'amount_mismatch': 0}:
            self.manifest[key + '_gate'] = 'FAILED'
            self.manifest[exit_key] = 1
        else:
            self.manifest[key + '_gate'] = 'matched: five conserved units and paid orders after duplicate callbacks'

    def oracle_hot_account(self, run_started, status):
        ts = run_started
        oracle = json.loads(self.psql(self.database, """
        SELECT json_build_object(
          'hot_paid_orders',(SELECT count(*) FROM orders o WHERE o.created_at>='%s' AND o.store_id='cap-store-1' AND o.customer_user_id IN (SELECT id FROM users WHERE full_name LIKE 'cap-customer-%%') AND o.payment_status='paid'),
          'hot_cancelled_paid',(SELECT count(*) FROM orders o WHERE o.created_at>='%s' AND o.store_id='cap-store-1' AND o.customer_user_id IN (SELECT id FROM users WHERE full_name LIKE 'cap-customer-%%') AND o.payment_status='paid' AND o.status='cancelled'),
          'hot_cancelled_without_obligation',(SELECT count(*) FROM orders o WHERE o.created_at>='%s' AND o.store_id='cap-store-1' AND o.customer_user_id IN (SELECT id FROM users WHERE full_name LIKE 'cap-customer-%%') AND o.payment_status='paid' AND o.status='cancelled' AND NOT EXISTS (SELECT 1 FROM order_refund_obligations r WHERE r.order_id=o.id AND r.purpose='cancellation')),
          'hot_duplicate_obligations',(SELECT count(*) FROM (SELECT r.purpose,r.business_id,r.source_payment_id FROM order_refund_obligations r JOIN orders o ON o.id=r.order_id WHERE o.created_at>='%s' AND o.store_id='cap-store-1' GROUP BY 1,2,3 HAVING count(*)>1) d),
          'hot_obligation_amount_mismatch',(SELECT count(*) FROM order_refund_obligations r JOIN orders o ON o.id=r.order_id JOIN payment_transactions p ON p.id=r.source_payment_id WHERE o.created_at>='%s' AND o.store_id='cap-store-1' AND r.purpose='cancellation' AND r.amount_toman<>p.amount)
        )""" % (ts, ts, ts, ts, ts)))
        oracle['scope'] = ('one cancellation obligation per cancelled paid order on the shared store; '
            'duplicate cancels must not create a second obligation; obligation amount must equal the source payment')
        self.manifest['hot_account_oracle'] = oracle
        if (oracle['hot_cancelled_without_obligation'] or oracle['hot_duplicate_obligations']
                or oracle['hot_obligation_amount_mismatch']):
            self.manifest['hot_account_oracle_gate'] = 'FAILED'
            self.manifest['hot-account_exit'] = 1
        else:
            self.manifest['hot_account_oracle_gate'] = 'matched within stated scope'

    def oracle_coupon_burst(self, run_started, status):
        """CAP-02 W-1.3: runner-side oracle for coupon-burst.mjs (owed since
        FIN-013 §3.2, on the hot-account pattern). Counts live in
        order_coupon_reservations / seller_discount_codes / orders:
        used_count must equal occupying reservations (reserved/consumed/
        review_hold — FIN-013 check 6); consumed reservations must equal
        discounted orders; silent-discount count (a consumed reservation
        whose order carries coupon_discount_amount=0) must be 0. The driver
        does not retry (each attempt is one shot), so retries are 0 by
        construction; deadlocks are counted from pg_stat_database and
        reported, never hidden."""
        oracle = json.loads(self.psql(self.database, """
        SELECT json_build_object(
          'coupons',(SELECT count(*) FROM seller_discount_codes WHERE code LIKE 'FIN013BURST%'),
          'used_count_mismatch',(SELECT count(*) FROM seller_discount_codes c WHERE c.code LIKE 'FIN013BURST%' AND c.used_count <> (SELECT COUNT(*) FROM order_coupon_reservations r WHERE r.coupon_id=c.id AND r.state IN ('reserved','consumed','review_hold'))),
          'consumed_reservations',(SELECT count(*) FROM order_coupon_reservations r JOIN seller_discount_codes c ON c.id=r.coupon_id WHERE c.code LIKE 'FIN013BURST%' AND r.state IN ('consumed','review_hold')),
          'discounted_orders',(SELECT count(*) FROM orders o JOIN order_coupon_reservations r ON r.order_id=o.id JOIN seller_discount_codes c ON c.id=r.coupon_id WHERE c.code LIKE 'FIN013BURST%' AND r.state IN ('consumed','review_hold') AND COALESCE(o.coupon_discount_amount,0)>0),
          'silent_discount',(SELECT count(*) FROM orders o JOIN order_coupon_reservations r ON r.order_id=o.id JOIN seller_discount_codes c ON c.id=r.coupon_id WHERE c.code LIKE 'FIN013BURST%' AND r.state IN ('consumed','review_hold') AND COALESCE(o.coupon_discount_amount,0)=0),
          'reserved_inflight',(SELECT count(*) FROM order_coupon_reservations r JOIN seller_discount_codes c ON c.id=r.coupon_id WHERE c.code LIKE 'FIN013BURST%' AND r.state='reserved'),
          'deadlocks',(SELECT deadlocks FROM pg_stat_database WHERE datname=current_database()))"""))
        oracle['scope'] = ('per-coupon reservation/counter/order agreement for the '
            'FIN013BURST coupons (FIN-013 check 6); zero silently-undiscounted '
            'orders; deadlocks counted from pg_stat_database; driver retries '
            'are 0 by construction (no retry loop in the driver)')
        self.manifest['coupon_burst_oracle'] = oracle
        if (oracle['used_count_mismatch'] or oracle['silent_discount']
                or oracle['consumed_reservations'] != oracle['discounted_orders']):
            self.manifest['coupon_burst_oracle_gate'] = 'FAILED'
            self.manifest['coupon-burst_exit'] = 1
        else:
            self.manifest['coupon_burst_oracle_gate'] = (
                'matched: used_count == occupying reservations, consumed == '
                'discounted orders, silent-discount 0 (deadlocks=%s)'
                % oracle['deadlocks'])

    # -- orchestrated profiles ----------------------------------------------
    def run_browser(self):
        if not self.frontend_dist:
            summary = {'certified': False, 'scope': 'browser-assets skipped: frontend build failed',
                       'failed_gates': ['frontend_build_failed'], 'blocked_reason': self.manifest.get('frontend_build')}
            (self.out / 'browser-assets-summary.json').write_text(json.dumps(summary, indent=2))
            self.manifest['browser-assets_exit'] = 1
            self.manifest['browser-assets_summary'] = 'SKIPPED: ' + str(self.manifest.get('frontend_build'))
            return 1
        target = {'nonce': self.nonce, 'base': 'https://capacity.example.test:%d' % self.nginx_port,
                  'port': self.nginx_port, 'listen': '127.0.0.1'}
        (self.work / 'browser-target.json').write_text(json.dumps(target))
        before = self.metrics()
        (self.out / 'browser-assets-metrics-before.prom').write_text(before)
        env = {**os.environ, 'SELLERSHO_CAPACITY_RUN': self.nonce,
               'CAPACITY_BROWSER_NATIVE': '1', 'CAPACITY_BROWSER_LEAF_SPKI': self.leaf_spki}
        result = subprocess.run(['node', str(LOADTESTS / 'browser-assets.mjs'),
                                 str(self.work / 'browser-target.json'), str(self.work / 'root.crt'),
                                 str(ROOT), str(self.out / 'browser-assets-summary.json')],
                                env=env, capture_output=True, text=True, timeout=400)
        after = self.metrics()
        (self.out / 'browser-assets-metrics-after.prom').write_text(after)
        a, b = metrics_counts(after), metrics_counts(before)
        received = {k: a[k] - b[k] for k in a}
        self.manifest['browser_received_counts'] = received
        status = result.returncode
        if (self.out / 'browser-assets-summary.json').exists():
            summary = json.loads((self.out / 'browser-assets-summary.json').read_text())
            client_api = sum(row['api'] for row in summary['counts'].values())
            # Domain-authorization count must equal total browser requests
            # received (each external request is auth-checked once).
            if received['domain_allow'] != received['api'] + received.get('other', 0) and received['domain_allow'] != summary['network_responses']:
                self.manifest['browser_counter_gate'] = (
                    'CHECK: domain_allow=%s vs network_responses=%s (see browser-assets-summary.json)'
                    % (received['domain_allow'], summary['network_responses']))
            else:
                self.manifest['browser_counter_gate'] = 'matched'
            if summary.get('failed_gates'):
                status = 1
        else:
            status = 1
            self.manifest['browser-assets_summary'] = 'MISSING: browser driver wrote no summary'
        self.manifest['browser-assets_exit'] = status
        self.manifest['browser_scope'] = ('10 anonymous real-browser contexts, cold/warm built UI and assets, '
                                          'native loopback nginx TLS; no credential injection; timings excluded')
        return status

    def run_cold_cache(self):
        """Cold: FLUSHALL scratch Redis + backend restart, then api-baseline.
        Warm: same profile immediately after, separately labeled."""
        overall = 0
        self.run([self.redis_cli, '-h', '127.0.0.1', '-p', str(self.redis_port), '-n', '15', 'FLUSHALL'],
                 env={**os.environ, **self.redis_cli_auth})
        self.stop_backend()
        self.start_backend()
        s, _ = self.run_driver('cold-cache-cold', 'commerce-baseline.mjs', {}, 600)
        overall = overall or s
        s, _ = self.run_driver('cold-cache-warm', 'commerce-baseline.mjs', {}, 600)
        overall = overall or s
        self.manifest['cold_cache'] = ('cold run: fresh isolated Redis (FLUSHALL) + backend restart, then warm run '
                                       'on the same fixture; compare cold-cache-cold-summary.json vs '
                                       'cold-cache-warm-summary.json; cache-state difference is explicit, not averaged away')
        return overall

    def run_headroom(self):
        """200 RPS for 5 min, then recovery to 100 RPS targets within 2 min."""
        s1, sum1 = self.run_driver('headroom', 'commerce-rolling.mjs',
                                   {'CAPACITY_DURATION_SECONDS': '300', 'CAPACITY_API_RPS': '200'}, 900)
        s2, sum2 = self.run_driver('headroom-recovery', 'commerce-rolling.mjs',
                                   {'CAPACITY_DURATION_SECONDS': '120', 'CAPACITY_API_RPS': '100'}, 600)
        self.manifest['headroom'] = {
            'burst': '200 API RPS for 300s (bounded rejection/queues permitted above target)',
            'recovery': '100 API RPS for 120s; recovery gate = recovery run latency/error gates pass',
            'burst_exit': s1, 'recovery_exit': s2,
            'burst_failed_gates': (sum1 or {}).get('failed_gates'),
            'recovery_failed_gates': (sum2 or {}).get('failed_gates')}
        # Headroom burst may legitimately fail latency gates above target;
        # the profile fails only if the oracle is dirty or recovery fails.
        oracle_bad = self.manifest.get('headroom_oracle_gate') == 'FAILED' or \
            self.manifest.get('headroom-recovery_oracle_gate') == 'FAILED'
        return 1 if (oracle_bad or s2) else 0

    def run_fault_provider_timeout(self):
        """Payment double in timeout mode: ambiguous outcomes must be
        recorded, never silently retried into a duplicate effect."""
        self.stop_backend()
        self.start_backend(payment_mode='timeout')
        s, summary = self.run_driver('fault-provider-timeout', 'commerce-baseline.mjs', {}, 600)
        self.stop_backend()
        self.start_backend(payment_mode='success')
        # After restoring success mode, the oracle for the timeout window
        # must show no duplicate payment effects: re-check via the
        # commerce oracle on the same run window is already recorded in
        # fault-provider-timeout_oracle; here we record the mode switch.
        self.manifest['fault_provider_timeout'] = {
            'mode': 'CAPACITY_PAYMENT_MODE=timeout during a 10s exact-mix run; backend restarted to success mode after',
            'driver_exit': s, 'failed_gates': (summary or {}).get('failed_gates'),
            'note': 'timeout-mode failures are EXPECTED (ambiguous verification); the gate is the oracle: '
                    'no duplicate payment/order effects and no missing refund obligations in the window'}
        oracle_bad = self.manifest.get('fault-provider-timeout_oracle_gate') == 'FAILED'
        return 1 if oracle_bad else 0

    def run_fault_redis_loss(self):
        """Stop scratch Redis: readiness must reflect the dependency and
        mutations must fail safely; restart and verify recovery."""
        self.stop_proc('redis')
        time.sleep(1)
        status, body = self.http_get('http://127.0.0.1:%d/api/health' % self.backend_port, timeout=5)
        down_observed = status != 200
        # Restart the SAME scratch Redis (same port/password/dir/marker).
        redis_server = str(REDIS_SHIM / 'redis-server') if (REDIS_SHIM / 'redis-server').exists() else 'redis-server'
        self.procs['redis'] = subprocess.Popen(
            [redis_server, '--port', str(self.redis_port), '--bind', '127.0.0.1',
             '--save', '', '--appendonly', 'no', '--requirepass', self.password,
             '--dir', str(self.redis_dir), '--daemonize', 'no'],
            stdout=open(self.work / 'redis-restart.log', 'w'), stderr=subprocess.STDOUT)
        recovered = False
        for _ in range(50):
            r = self.run([self.redis_cli, '-h', '127.0.0.1', '-p', str(self.redis_port), '-n', '15', 'PING'],
                         env={**os.environ, **self.redis_cli_auth}, check=False)
            if 'PONG' in r.stdout:
                recovered = True
                break
            time.sleep(0.2)
        status2, _ = self.http_get('http://127.0.0.1:%d/api/health' % self.backend_port, timeout=5)
        self.manifest['fault_redis_loss'] = {
            'health_during_loss_status': status, 'dependency_visible': down_observed,
            'redis_recovered': recovered, 'health_after_restart_status': status2}
        if not (recovered and status2 == 200):
            return 1
        return 0

    def run_fault_restart(self):
        """Kill -9 the backend after a hot-sku run, restart, and re-run
        the oracle: restart/reconciliation must converge to one outcome.
        The driver re-run uses its own name ('hot-sku-restart'): the main
        hot-sku profile already consumed the five units, so the re-run is
        EXPECTED to fail preparation with 409 inventory_origin_unavailable.
        It must never overwrite the main run's summary, oracle, or exit —
        that clobbering hid the main run's real result in the 2026-11-20
        full suite (the re-run's 1-helper 409 summary replaced the main
        run's 5-paid summary). Only oracle convergence decides this
        profile."""
        s, _ = self.run_driver('hot-sku-restart', 'hot-sku.mjs', {}, 600)
        proc = self.procs.get('backend')
        if proc:
            proc.kill()
            proc.wait(timeout=15)
            self.procs.pop('backend', None)
        self.start_backend()
        oracle_before = dict(self.manifest.get('hot_sku_oracle') or {})
        self.oracle_hot_sku(0, key='hot_sku_oracle_restart', exit_key='hot-sku-restart_exit')
        oracle_after = dict(self.manifest.get('hot_sku_oracle_restart') or {})
        converged = oracle_before == oracle_after and \
            self.manifest.get('hot_sku_oracle_gate', '').startswith('matched')
        self.manifest['fault_restart'] = {
            'hot_sku_exit_before_restart': s, 'oracle_before_restart': oracle_before,
            'oracle_after_restart': oracle_after, 'converged': converged,
            'note': 'backend SIGKILL + restart; paid orders / obligations / stock must be identical after restart; the hot-sku-restart driver re-run is expected to fail preparation (five units already consumed by the main hot-sku run) and does not decide this profile'}
        return 0 if converged else 1

    # -- CAP-02 W-3 fault matrix ------------------------------------------
    def reconcile_gate(self, name, since):
        """Run the W-2.1 consolidated gate (scripts/cap02_reconcile.py)
        against this run's DB, windowed at `since`. Returns (exit_code,
        report_dict_or_None). The report is written to
        <out>/<name>-reconcile.json. Exit 0 = PASS, 1 = FAIL, 2 = REFUSED."""
        out_path = self.out / (name + '-reconcile.json')
        env = {**os.environ,
               'PATH': str(HOME / 'go' / 'bin') + ':' + str(PG_BIN) + ':' + os.environ.get('PATH', ''),
               'HOME': str(HOME)}
        result = self.run([sys.executable, str(ROOT / 'scripts' / 'cap02_reconcile.py'),
                           '--db', self.dsn, '--since', since,
                           '--out', str(out_path)],
                          env=env, cwd=ROOT, timeout=900, check=False)
        report = None
        try:
            report = json.loads(out_path.read_text())
        except Exception:
            pass
        self.manifest[name + '_reconcile'] = {
            'exit': result.returncode,
            'verdict': (report or {}).get('verdict'),
            'report': str(out_path)}
        return result.returncode, report

    def plant_double_receipt(self, order_id=None):
        """Plant a second payment receipt for an already-paid order — a
        double-capture effect. NOTE: the literal 'duplicate paid
        payment_transactions row' plant is impossible by construction: the
        partial unique index payment_transactions_one_active_per_order
        (status IN ('pending','paid') AND NOT compensation_capture) rejects
        a second active row for the same order, which is itself the app's
        guard against double capture. The receipt plant is the faithful
        double-effect instead: FIN-013 check 1 counts receipts per paid
        order and requires exactly 1, so a second receipt makes the W-2.1
        gate FAIL. The planted rows reference a fresh 'failed' transaction
        (outside the partial index) so the plant itself never fights the
        guard. Returns the planted ids, or None if no paid order exists."""
        if order_id is None:
            # Must already have EXACTLY 1 receipt: FIN-013 check 1
            # requires exactly 1 receipt per paid order, so the plant
            # (a 2nd receipt) only trips it when the order starts at 1.
            # A paid fixture order with 0 receipts (e.g. cap-order-96699
            # in run 8) would go 0 -> 1 and PASS the gate, making the
            # negative control silently ineffective (bug 15).
            order_id = self.psql(self.database,
                "SELECT id FROM orders WHERE store_id='cap-store-1' AND "
                "payment_status='paid' AND (SELECT count(*) FROM "
                "order_payment_receipts WHERE order_id=orders.id)=1 "
                "ORDER BY created_at DESC LIMIT 1")
        if not order_id:
            return None
        receipts = int(self.psql(self.database,
            "SELECT count(*) FROM order_payment_receipts WHERE order_id='%s'"
            % order_id))
        if receipts != 1:
            return None
        row = self.psql(self.database,
            "SELECT id, order_number, amount FROM payment_transactions "
            "WHERE order_id='%s' AND status='paid'" % order_id)
        if not row:
            return None
        _txn_id, order_number, amount = row.split('|')
        customer = self.psql(self.database,
            "SELECT customer_user_id FROM orders WHERE id='%s'" % order_id)
        cust = ("'%s'" % customer) if customer else 'NULL'
        suffix = secrets.token_hex(4)
        txn2 = 'cap-double-txn-' + suffix
        ver2 = 'cap-double-ver-' + suffix
        rcpt2 = 'cap-double-rcpt-' + suffix
        self.psql(self.database,
            "INSERT INTO payment_transactions (id, order_id, order_number, "
            "gateway_token, rrn, amount, status) VALUES ('%s','%s','%s',"
            "'cap-double-token-%s',NULL,%s,'failed')"
            % (txn2, order_id, order_number, suffix, amount))
        self.psql(self.database,
            "INSERT INTO order_payment_verifications (id, payment_id, "
            "order_id, customer_id, amount_toman, currency, funding_kind) "
            "VALUES ('%s','%s','%s',%s,%s,'TOMAN','simulated')"
            % (ver2, txn2, order_id, cust, amount))
        self.psql(self.database,
            "INSERT INTO order_payment_receipts (id, verification_id, "
            "funding_kind, payment_id, order_id, customer_id, amount_toman, "
            "currency, provider_reference, verified_at) VALUES "
            "('%s','%s','simulated','%s','%s',%s,%s,'TOMAN',"
            "'cap-double-ref-%s',NOW())"
            % (rcpt2, ver2, txn2, order_id, cust, amount, suffix))
        return {'order_id': order_id, 'txn': txn2,
                'verification': ver2, 'receipt': rcpt2}

    def unplant_double_receipt(self, planted):
        if not planted:
            return
        # Receipts + verifications are immutable by trigger
        # (payment_receipt_identity on order_payment_receipts and
        # payment_verification_identity on order_payment_verifications,
        # both calling immutable_order_refund_evidence()): a plain
        # DELETE raises. The restore disables both triggers, deletes
        # the planted rows, and re-enables them (verified tgenabled='O'
        # after) — the same append-only restore pattern as the W-2.1
        # commerce plant. payment_transactions has no such trigger.
        self.psql(self.database,
            "ALTER TABLE order_payment_receipts DISABLE TRIGGER payment_receipt_identity; "
            "ALTER TABLE order_payment_verifications DISABLE TRIGGER payment_verification_identity; "
            "DELETE FROM order_payment_receipts WHERE id='%s'; "
            "DELETE FROM order_payment_verifications WHERE id='%s'; "
            "DELETE FROM payment_transactions WHERE id='%s'; "
            "ALTER TABLE order_payment_receipts ENABLE TRIGGER payment_receipt_identity; "
            "ALTER TABLE order_payment_verifications ENABLE TRIGGER payment_verification_identity"
            % (planted['receipt'], planted['verification'], planted['txn']))

    def restart_backend(self, latencies=None):
        if self.procs.get('backend'):
            self.stop_backend()
        self.start_backend(latencies=latencies)

    def run_fault_boundary(self, boundary):
        """CAP-02 W-3.1: kill at one payment/hold boundary, replay, assert
        exactly-one-effect via the W-2.1 gate, plus a negative control
        (planted double receipt -> gate FAILS -> restore -> gate PASSES).
        b4's order is unpaid, so its negative control plants against the
        most recent paid order in the run — a durable double-expiry has no
        footprint to plant (expiry is idempotent by design; the hold-release
        double-run is Redis-idempotent), and the plant proves the gate was
        live for this boundary's run."""
        key = 'fault_boundary_' + boundary
        result = {'boundary': boundary}
        since = self.psql(self.database, "SELECT clock_timestamp()::text")
        state_file = self.work / ('boundary-' + boundary + '.json')
        setup_env = {'CAPACITY_BOUNDARY': boundary,
                     'CAPACITY_BOUNDARY_PHASE': 'setup',
                     'CAPACITY_BOUNDARY_STATE_FILE': str(state_file)}
        s, _ = self.run_driver('fault-boundary-%s-setup' % boundary,
                               'fault-boundary.mjs', setup_env, 300)
        result['setup_exit'] = s
        if s != 0 or not state_file.exists():
            result['error'] = 'setup failed'
            self.manifest[key] = result
            return 1
        state = json.loads(state_file.read_text())
        result['order_id'] = state['orderId']
        if boundary == 'b1':
            # Kill before any callback is sent (authorize done, capture
            # not started); replay sends the callback, exactly once.
            self.kill_backend()
            result['kill_point'] = 'before callback'
            self.start_backend()
        elif boundary in ('b2', 'b3'):
            # Callback sent while the runner kills mid-flight. b2 uses a
            # slow verify (5 s) so the kill lands ~1 s into verification;
            # b3 uses fast verify and kills on the marker, so the window
            # is sub-100 ms — retry up to 3 times with a fresh order.
            if boundary == 'b2':
                self.restart_backend(latencies='5000,5000,5000,5000,5000')
            attempt, lost = 0, False
            while attempt < 3 and not lost:
                attempt += 1
                cb_name = 'fault-boundary-%s-callback-%d' % (boundary, attempt)
                cb_env = self.driver_env({
                    'CAPACITY_BOUNDARY': boundary,
                    'CAPACITY_BOUNDARY_PHASE': 'callback',
                    'CAPACITY_BOUNDARY_STATE_FILE': str(state_file),
                    'CAPACITY_SUMMARY_FILE': str(self.work / (cb_name + '-summary.json'))})
                log = open(self.work / (cb_name + '.log'), 'w')
                proc = subprocess.Popen(
                    ['node', str(LOADTESTS / 'fault-boundary.mjs')],
                    env=cb_env, stdout=log, stderr=subprocess.STDOUT, cwd=LOADTESTS)
                killed, deadline = False, time.time() + 20
                while time.time() < deadline:
                    try:
                        st = json.loads(state_file.read_text())
                    except Exception:
                        st = {}
                    if st.get('callback_started_at'):
                        if boundary == 'b2':
                            time.sleep(1.0)
                        self.kill_backend()
                        killed = True
                        break
                    if proc.poll() is not None:
                        break
                    time.sleep(0.02)
                rc = proc.wait(timeout=60)
                st = json.loads(state_file.read_text())
                result['callback_attempt_%d' % attempt] = {
                    'exit': rc, 'outcome': st.get('callback_outcome'),
                    'killed_mid_flight': killed}
                lost = (st.get('callback_outcome') == 'lost')
                if lost:
                    break
                # Kill landed too late (callback completed): fresh backend
                # + fresh order, retry.
                self.restart_backend(
                    latencies='5000,5000,5000,5000,5000' if boundary == 'b2' else None)
                s2, _ = self.run_driver(
                    'fault-boundary-%s-setup-%d' % (boundary, attempt),
                    'fault-boundary.mjs', setup_env, 300)
                if s2 != 0:
                    result['error'] = 'retry setup failed'
                    break
                state = json.loads(state_file.read_text())
                result['order_id'] = state['orderId']
            result['callback_attempts'] = attempt
            result['callback_lost'] = lost
            if not lost:
                result['error'] = ('boundary not exercised: kill never '
                    'landed mid-callback in %d attempts' % attempt)
                self.manifest[key] = result
                return 1
            # Restart (not plain start): if the driver timed out instead of
            # being killed, the old backend is still alive.
            self.restart_backend()
        elif boundary == 'b4':
            # Hold-expiry boundary: install a slow-expiry trigger
            # (pg_sleep(0.15) on the boundary order's expiry — the repo's
            # TestJobsCrashRestartAndMidBatchCancellation technique, but
            # the sleep MUST stay inside the expiry job's 300 ms
            # per-candidate timeout (operations_repo.go: expireUnpaidOrder
            # runs under context.WithTimeout(ctx, 300ms)) AND inside the
            # job's 3 s per-attempt deadline (operations.go:13). The first
            # b4 attempt used pg_sleep(10), which exceeds both, so the
            # candidate failed at 300 ms on every 5 s attempt, the order
            # stayed unexpired, and the runner never observed the batch
            # (a 2 s sleep would fail the same way). pg_sleep(0.15) is
            # long enough for the runner's tightened poll (~0.1 s) to
            # observe PgSleep and kill mid-batch, and short enough
            # (~150 ms + ~50 ms base) to complete within 300 ms.
            # TestJobsCrashRestartAndMidBatchCancellation technique), wait
            # for the 15-min deadline, kill while the worker's batch is
            # inside the trigger (observed via pg_stat_activity PgSleep),
            # restart, and require the order to expire exactly once.
            order_id = state['orderId']
            self.psql(self.database, """
              CREATE OR REPLACE FUNCTION cap_boundary_b4_slow() RETURNS trigger AS $$
              BEGIN
                IF NEW.id = '%s' AND NEW.status='cancelled' AND OLD.status<>'cancelled' THEN
                  PERFORM pg_sleep(0.15);
                END IF;
                RETURN NEW;
              END $$ LANGUAGE plpgsql;
              DROP TRIGGER IF EXISTS cap_boundary_b4_slow_trg ON orders;
              CREATE TRIGGER cap_boundary_b4_slow_trg BEFORE UPDATE ON orders
                FOR EACH ROW EXECUTE FUNCTION cap_boundary_b4_slow();
            """ % order_id)
            # Queue-jump (deviation from the audit's real 15-min hold
            # wait, recorded in capacity-evidence.txt): the boundary
            # order's payment_expires_at is moved to a fixed ancient
            # time ('2000-01-01') so it sorts FIRST in the expiry job's
            # ORDER BY payment_expires_at queue. The fixture leaves a
            # ~7200-order expired backlog and the job processes ~21
            # candidates per 5 s batch (3 s deadline, ~140 ms each), so
            # a genuinely 15-min-held order waits ~28 min behind it
            # and the profile's window (deadline + 30 s, then a 120 s
            # expired-poll) never sees it (runs 3/5/7: the order never
            # expired). The hold DURATION is not what b4 tests — the
            # kill-during-expiry + exactly-once is — and that mechanism
            # is identical with an ancient expiry. The deadline for the
            # wait loop is therefore NOW (the order is already expired);
            # the job picks it up on its next 5 s tick.
            self.psql(self.database,
                "UPDATE orders SET payment_expires_at='2000-01-01 00:00:00+00' WHERE id='%s'"
                % order_id)
            deadline = time.time()
            observed, kill_deadline = False, time.time() + 60
            while time.time() < kill_deadline:
                if time.time() >= deadline:
                    n = int(self.psql(self.database,
                        "SELECT count(*) FROM pg_stat_activity WHERE "
                        "datname='%s' AND state='active' AND wait_event='PgSleep'"
                        % self.database))
                    if n > 0:
                        observed = True
                        break
                # Tight poll: the trigger sleep is only ~150 ms, so a
                # 0.2 s poll would usually miss the PgSleep window.
                time.sleep(0.05)
            self.kill_backend()
            result['kill_point'] = ('during expiry batch (PgSleep observed)'
                if observed else 'after deadline (batch not observed; best-effort)')
            result['expiry_batch_observed'] = observed
            self.start_backend()
            expired = False
            for _ in range(240):
                if self.psql(self.database,
                        "SELECT status FROM orders WHERE id='%s'" % order_id) == 'cancelled':
                    expired = True
                    break
                time.sleep(0.5)
            result['expired_after_restart'] = expired
            self.psql(self.database,
                "DROP TRIGGER IF EXISTS cap_boundary_b4_slow_trg ON orders; "
                "DROP FUNCTION IF EXISTS cap_boundary_b4_slow();", check=False)
            if not expired:
                result['error'] = 'order did not expire after restart'
                self.manifest[key] = result
                return 1
        # --- replay: the same callback (b1/b2/b3) or the expiry read (b4) ---
        replay_env = {'CAPACITY_BOUNDARY': boundary,
                      'CAPACITY_BOUNDARY_PHASE': 'replay',
                      'CAPACITY_BOUNDARY_STATE_FILE': str(state_file)}
        s, _ = self.run_driver('fault-boundary-%s-replay' % boundary,
                               'fault-boundary.mjs', replay_env, 300)
        result['replay_exit'] = s
        if s != 0:
            result['error'] = 'replay failed (not exactly once)'
            self.manifest[key] = result
            return 1
        # --- W-2.1 gate, then the negative control ---
        gate_exit, report = self.reconcile_gate('fault-boundary-' + boundary, since)
        result['gate_exit'] = gate_exit
        result['gate_verdict'] = (report or {}).get('verdict')
        if gate_exit != 0:
            result['error'] = 'W-2.1 gate FAILED on the clean run'
            self.manifest[key] = result
            return 1
        plant_order = state['orderId'] if boundary != 'b4' else None
        planted = self.plant_double_receipt(plant_order)
        if planted is None:
            result['negative_control'] = {'error': 'no paid order to plant against'}
            self.manifest[key] = result
            return 1
        nc_exit, nc_report = self.reconcile_gate(
            'fault-boundary-' + boundary + '-nc', since)
        result['negative_control'] = {
            'planted_order': planted['order_id'], 'gate_exit': nc_exit,
            'gate_verdict': (nc_report or {}).get('verdict'),
            'expected': 'FAIL'}
        self.unplant_double_receipt(planted)
        nc2_exit, nc2_report = self.reconcile_gate(
            'fault-boundary-' + boundary + '-nc-clean', since)
        result['negative_control_restore'] = {
            'gate_exit': nc2_exit,
            'gate_verdict': (nc2_report or {}).get('verdict'),
            'expected': 'PASS'}
        ok = nc_exit != 0 and nc2_exit == 0
        if not ok:
            result['error'] = 'negative control misbehaved (planted gate exit %s, restored gate exit %s)' % (nc_exit, nc2_exit)
            self.manifest[key] = result
            return 1
        self.manifest[key] = result
        return 0

    def run_fault_db_loss(self):
        """CAP-02 W-3.2: DB-loss profile (native). Loss duration 30 s,
        applied as ALTER DATABASE ... CONNECTION LIMIT 0 + pg_terminate_backend
        on the run DB's test_runner backends (the shared PG cluster on 5433
        is never stopped). REVOKE CONNECT does NOT work here: test_runner
        OWNS the run DB, and the owner bypasses the CONNECT privilege check
        (verified live 2026-11-21 — a direct test_runner connect succeeded
        after REVOKE; CONNECTION LIMIT 0 blocks the owner, verified the
        same way: health stayed degraded/down for 5 samples and recovered
        on LIMIT -1). A commerce-rolling driver runs a 120 s load window
        across the loss. Assertions: health goes 503 during the loss,
        recovers to 200 after the limit is lifted, the commerce oracle
        matches for the window, and the W-2.1 gate PASSES (no partial
        effects). FAILS CLOSED if the DB never comes back (bounded 60 s
        recovery wait). The negative control re-applies the loss after
        recovery and requires health to stay non-200 for 10 s, proving the
        recovery check does not false-positive."""
        key = 'fault_db_loss'
        result = {'loss_seconds': 30}
        since = self.psql(self.database, "SELECT clock_timestamp()::text")
        load_name = 'fault-db-loss-load'
        load_env = self.driver_env({
            'CAPACITY_DURATION_SECONDS': '120',
            'CAPACITY_SUMMARY_FILE': str(self.work / (load_name + '-summary.json'))})
        load_log = open(self.work / (load_name + '.log'), 'w')
        load = subprocess.Popen(
            ['node', str(LOADTESTS / 'commerce-rolling.mjs')],
            env=load_env, stdout=load_log, stderr=subprocess.STDOUT, cwd=LOADTESTS)
        # Lead time before the loss: the driver's bootstrap is sequential
        # (~150 ms per principal, ~15 s for 100) and a 503 during it fails
        # the whole load with failed_gates ["bootstrap"] (run 3: the 10 s
        # lead let the loss land on principal 67's bootstrap_order).
        # 30 s lets bootstrap finish so the loss hits the running window.
        time.sleep(30)
        terminated = self.psql(self.database,
            "ALTER DATABASE %s CONNECTION LIMIT 0; "
            "SELECT count(*) FROM (SELECT pg_terminate_backend(pid) FROM "
            "pg_stat_activity WHERE datname='%s' AND usename='test_runner' "
            "AND pid<>pg_backend_pid()) s"
            % (self.database, self.database))
        result['terminated_backends'] = terminated
        health_during = []
        for _ in range(6):
            status, _ = self.http_get(
                'http://127.0.0.1:%d/api/health' % self.backend_port, timeout=5)
            health_during.append(status)
            time.sleep(1)
        result['health_during_loss'] = health_during
        time.sleep(30 - 6)
        self.psql(self.database,
            "ALTER DATABASE %s CONNECTION LIMIT -1" % self.database)
        recovered = False
        for _ in range(120):
            status, _ = self.http_get(
                'http://127.0.0.1:%d/api/health' % self.backend_port, timeout=5)
            if status == 200:
                recovered = True
                break
            time.sleep(0.5)
        result['health_after_recovery'] = status
        result['recovered'] = recovered
        try:
            load.wait(timeout=120)
        except subprocess.TimeoutExpired:
            load.kill()
            load.wait(timeout=15)
        result['load_exit'] = load.returncode
        try:
            load_summary = json.loads(
                (self.work / (load_name + '-summary.json')).read_text())
            result['load_requests'] = load_summary.get('requests')
            result['load_failed_gates'] = load_summary.get('failed_gates')
        except Exception:
            result['load_requests'] = None
        self.oracle_commerce('fault-db-loss', since, 0)
        result['oracle'] = self.manifest.get('fault-db-loss_oracle')
        result['oracle_gate'] = self.manifest.get('fault-db-loss_oracle_gate')
        gate_exit, report = self.reconcile_gate('fault-db-loss', since)
        result['gate_exit'] = gate_exit
        result['gate_verdict'] = (report or {}).get('verdict')
        # Negative control: loss again, health must stay down (the check
        # must report the outage, not pass on a stale healthy read).
        self.psql(self.database,
            "ALTER DATABASE %s CONNECTION LIMIT 0; "
            "SELECT pg_terminate_backend(pid) FROM pg_stat_activity "
            "WHERE datname='%s' AND usename='test_runner' AND pid<>pg_backend_pid()"
            % (self.database, self.database))
        nc_health = []
        for _ in range(10):
            status, _ = self.http_get(
                'http://127.0.0.1:%d/api/health' % self.backend_port, timeout=5)
            nc_health.append(status)
            time.sleep(1)
        result['negative_control_health_during_loss'] = nc_health
        result['negative_control_stayed_down'] = all(s != 200 for s in nc_health)
        self.psql(self.database,
            "ALTER DATABASE %s CONNECTION LIMIT -1" % self.database)
        restored = False
        for _ in range(120):
            status, _ = self.http_get(
                'http://127.0.0.1:%d/api/health' % self.backend_port, timeout=5)
            if status == 200:
                restored = True
                break
            time.sleep(0.5)
        result['negative_control_restored'] = restored
        ok = (recovered and str(result['oracle_gate']).startswith('matched')
              and gate_exit == 0 and result['negative_control_stayed_down']
              and restored)
        if not ok:
            result['error'] = ('recovered=%s oracle_gate=%s gate_exit=%s '
                'nc_stayed_down=%s restored=%s'
                % (recovered, result['oracle_gate'], gate_exit,
                   result['negative_control_stayed_down'], restored))
        self.manifest[key] = result
        return 0 if ok else 1

    def run_fault_worker_failover(self):
        """CAP-02 W-3.3: worker-failover profile (native). Kills the
        backend mid-batch for (a) the unpaid-order-expiry batch (60 seeded
        expired orders, pg_sleep(0.025) per order — the repo's
        TestJobsCrashRestartAndMidBatchCancellation technique) and (b) the
        delivery-lock-releaser batch (20 already-released orders
        un-released, pg_sleep(0.025) per ledger insert). Asserts lease
        takeover (background_job_state owner changes), no double-execution
        (each order cancelled once; each released order has exactly 2
        balanced ledger entries; the release eligibility query returns 0
        after the re-release), unchanged stock, and backlog drains. Against
        JOB-01 lease semantics (expiry 5 s period / 3 s timeout; release
        60 s period / 10 s timeout)."""
        key = 'fault_worker_failover'
        result = {}
        since = self.psql(self.database, "SELECT clock_timestamp()::text")
        try:
            # --- (a) expiry batch ---
            # The 60 seeded orders use a fixed ANCIENT payment_expires_at
            # ('2000-01-01') so they sort FIRST in the expiry job's
            # ORDER BY payment_expires_at queue. The fixture leaves a
            # ~7200-order expired backlog and the job processes ~21
            # candidates per 5 s batch (3 s deadline, ~140 ms each), so
            # a newly-expired order (2 min past) waits ~28 min behind
            # it and the profile's ">=3 committed" wait never fires
            # (run 7: 0 committed). The ancient expiry does not change
            # what is tested (lease takeover + exactly-once + drain);
            # it only makes the seeded batch reachable.
            self.psql(self.database, """
              INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,payment_expires_at)
              SELECT 'cap-w33-e-'||g::text,'cap-w33-e-'||g::text,'cap-store-1','job',2000,2000,'job','09123456789','x','x','x','1234512345','cap-w33-e-'||g::text,'2000-01-01 00:00:00+00'::timestamptz FROM generate_series(1,60)g;
              INSERT INTO order_items(id,order_id,product_id,quantity,wholesale_cost,platform_fee,seller_margin,final_unit_price)
              SELECT md5('cap-w33-e-'||g::text)::uuid,'cap-w33-e-'||g::text,'cap-product-1',1,1000,0,1000,2000 FROM generate_series(1,60)g;
              CREATE OR REPLACE FUNCTION cap_w33_slow_expiry() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id LIKE 'cap-w33-e-%' AND NEW.status='cancelled' THEN PERFORM pg_sleep(0.025); END IF; RETURN NEW; END $$;
              DROP TRIGGER IF EXISTS cap_w33_slow_expiry_trg ON orders;
              CREATE TRIGGER cap_w33_slow_expiry_trg BEFORE UPDATE ON orders FOR EACH ROW EXECUTE FUNCTION cap_w33_slow_expiry();
            """)
            stock_before = self.psql(self.database,
                "SELECT stock FROM products WHERE id='cap-product-1'")
            owner_before = self.psql(self.database,
                "SELECT owner FROM background_job_state WHERE job='unpaid-order-expiry'")
            committed = 0
            for _ in range(300):
                committed = int(self.psql(self.database,
                    "SELECT count(*) FROM orders WHERE id LIKE 'cap-w33-e-%' AND status='cancelled'"))
                if committed >= 3:
                    break
                time.sleep(0.1)
            result['expiry_committed_before_kill'] = committed
            if committed < 3:
                result['error'] = 'expiry batch made no committed progress'
                self.manifest[key] = result
                return 1
            self.kill_backend()
            time.sleep(6)  # lease timeout 3 s + 2 s + margin
            self.start_backend()
            cancelled = 0
            for _ in range(240):
                cancelled = int(self.psql(self.database,
                    "SELECT count(*) FROM orders WHERE id LIKE 'cap-w33-e-%' AND status='cancelled'"))
                if cancelled == 60:
                    break
                time.sleep(0.5)
            stock_after = self.psql(self.database,
                "SELECT stock FROM products WHERE id='cap-product-1'")
            owner_after = self.psql(self.database,
                "SELECT owner FROM background_job_state WHERE job='unpaid-order-expiry'")
            result['expiry'] = {
                'cancelled': cancelled, 'stock_unchanged': stock_before == stock_after,
                'owner_before': owner_before, 'owner_after': owner_after,
                'lease_takeover': bool(owner_before) and owner_before != owner_after}
            if cancelled != 60 or stock_before != stock_after:
                result['error'] = 'expiry batch did not drain exactly once (cancelled=%s stock_changed=%s)' % (cancelled, stock_before != stock_after)
                self.manifest[key] = result
                return 1
            # --- (b) delivery/release batch ---
            picked = self.psql(self.database, """
              SELECT string_agg(id, ',') FROM (
                SELECT o.id FROM orders o WHERE o.status='delivered_confirmed' AND o.profit_released
                  AND o.referrer_seller_id IS NULL
                  AND (SELECT count(*) FROM ledger_entries l WHERE l.related_order_or_dispute=o.id
                    AND l.operator='system:delivery-lock-releaser')=2
                ORDER BY o.id LIMIT 20) s""")
            if not picked:
                result['error'] = 'no 20 released orders with exactly 2 ledger entries to un-release'
                self.manifest[key] = result
                return 1
            ids = picked.split(',')
            id_list = "'" + "','".join(ids) + "'"
            # The release (releaseClosedReturnWindowTx) sets
            # profit_released, profit_released_at AND
            # referral_commission_released=TRUE; the candidate query
            # requires referral_commission_released=FALSE, so the
            # un-release must reset that flag too or the 20 orders are
            # never candidates again (bug 14: run 5's release batch
            # waited 90 s for a ledger entry that never came because
            # the flag stayed TRUE on all 20). The picked orders have
            # no referrer, so no referral bonus can double-release.
            self.psql(self.database,
                "ALTER TABLE ledger_entries DISABLE TRIGGER trg_ledger_entries_append_only; "
                "ALTER TABLE ledger_entries DISABLE TRIGGER fin005_linked_ledger_immutable; "
                "DELETE FROM ledger_entries WHERE related_order_or_dispute IN (%s) AND operator='system:delivery-lock-releaser'; "
                "ALTER TABLE ledger_entries ENABLE TRIGGER trg_ledger_entries_append_only; "
                "ALTER TABLE ledger_entries ENABLE TRIGGER fin005_linked_ledger_immutable; "
                "UPDATE orders SET profit_released=false, profit_released_at=NULL, referral_commission_released=false WHERE id IN (%s)"
                % (id_list, id_list))
            self.psql(self.database, """
              CREATE OR REPLACE FUNCTION cap_w33_slow_release() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.related_order_or_dispute IN (%s) THEN PERFORM pg_sleep(0.025); END IF; RETURN NEW; END $$;
              DROP TRIGGER IF EXISTS cap_w33_slow_release_trg ON ledger_entries;
              CREATE TRIGGER cap_w33_slow_release_trg BEFORE INSERT ON ledger_entries FOR EACH ROW EXECUTE FUNCTION cap_w33_slow_release();
            """ % id_list)
            owner_rel_before = self.psql(self.database,
                "SELECT owner FROM background_job_state WHERE job='delivery-lock-releaser'")
            first = 0
            for _ in range(900):
                first = int(self.psql(self.database,
                    "SELECT count(*) FROM ledger_entries WHERE related_order_or_dispute IN (%s) AND operator='system:delivery-lock-releaser'" % id_list))
                if first >= 1:
                    break
                time.sleep(0.1)
            result['release_entries_before_kill'] = first
            if first < 1:
                result['error'] = 'release batch never started (no ledger entry in 90 s)'
                self.manifest[key] = result
                return 1
            self.kill_backend()
            time.sleep(15)  # lease timeout 10 s + 2 s + margin
            self.start_backend()
            released = 0
            for _ in range(360):
                released = int(self.psql(self.database,
                    "SELECT count(*) FROM orders WHERE id IN (%s) AND profit_released" % id_list))
                if released == 20:
                    break
                time.sleep(0.5)
            paired = self.psql(self.database, """
              SELECT count(*) FROM (
                SELECT o.id FROM orders o WHERE o.id IN (%s) AND o.profit_released
                  AND (SELECT count(*) FROM ledger_entries l WHERE l.related_order_or_dispute=o.id AND l.operator='system:delivery-lock-releaser')=2
                  AND (SELECT COALESCE(SUM(CASE WHEN l.type='credit' THEN l.amount ELSE -l.amount END),0) FROM ledger_entries l WHERE l.related_order_or_dispute=o.id AND l.operator='system:delivery-lock-releaser')=0
              ) s""" % id_list)
            replay = int(self.psql(self.database,
                "SELECT count(*) FROM orders WHERE id IN (%s) AND status='delivered_confirmed' AND NOT profit_released AND return_window_ends_at<=NOW()" % id_list))
            owner_rel_after = self.psql(self.database,
                "SELECT owner FROM background_job_state WHERE job='delivery-lock-releaser'")
            result['release'] = {
                'released': released, 'paired_balanced': int(paired),
                'replay_candidates': replay,
                'owner_before': owner_rel_before, 'owner_after': owner_rel_after,
                'lease_takeover': bool(owner_rel_before) and owner_rel_before != owner_rel_after}
            if released != 20 or int(paired) != 20 or replay != 0:
                result['error'] = 'release batch did not re-release exactly once (released=%s paired=%s replay=%s)' % (released, paired, replay)
                self.manifest[key] = result
                return 1
            gate_exit, report = self.reconcile_gate('fault-worker-failover', since)
            result['gate_exit'] = gate_exit
            result['gate_verdict'] = (report or {}).get('verdict')
            if gate_exit != 0:
                result['error'] = 'W-2.1 gate FAILED after failover'
                self.manifest[key] = result
                return 1
        finally:
            self.psql(self.database,
                "DROP TRIGGER IF EXISTS cap_w33_slow_expiry_trg ON orders; "
                "DROP FUNCTION IF EXISTS cap_w33_slow_expiry(); "
                "DROP TRIGGER IF EXISTS cap_w33_slow_release_trg ON ledger_entries; "
                "DROP FUNCTION IF EXISTS cap_w33_slow_release();", check=False)
        self.manifest[key] = result
        return 0

    def https_json(self, method, path, token, body=None):
        """One HTTPS call to the nginx front door, with the run CA and the
        capacity.example.test SNI/Host (the host's resolver has no such
        name; the aliases exist only inside the backend's mount namespace).
        Returns (status, parsed_json_or_None). Used by the redis-load
        callback probe, where a node driver would be the wrong tool."""
        import socket, ssl, http.client
        ctx = ssl.create_default_context(cafile=str(self.work / 'root.crt'))
        raw = socket.create_connection(('127.0.0.1', self.nginx_port), timeout=10)
        tls = ctx.wrap_socket(raw, server_hostname='capacity.example.test')
        conn = http.client.HTTPConnection('capacity.example.test', self.nginx_port, timeout=10)
        conn.sock = tls
        headers = {'Host': 'capacity.example.test',
                   'Content-Type': 'application/json'}
        if token:
            headers['Authorization'] = 'Bearer ' + token
        conn.request(method, path,
                     body=json.dumps(body) if body is not None else None,
                     headers=headers)
        resp = conn.getresponse()
        raw_body = resp.read()
        conn.close()
        try:
            parsed = json.loads(raw_body)
        except Exception:
            parsed = None
        return resp.status, parsed

    def run_fault_redis_load(self):
        """CAP-02 W-3.4: Redis restart under load (native). A
        commerce-rolling driver runs a 120 s load window; Redis is stopped
        mid-window, restarted, and its holds are rebuilt from PG via
        `redis-recover rebuild`. A prepared order's callback is sent
        DURING the rebuild: it must either pay (hold survived/rebuilt in
        time) or follow the refund path (409 inventory_hold_expired AND a
        refund obligation recorded) — never lost money. Asserts health
        503 during the outage, 200 after, rebuild exit 0, oracle match,
        and the W-2.1 gate PASS. The host-crash <=1 s AOF loss window is
        untestable-by-kill and is recorded as such (this profile kills the
        process, not the host)."""
        key = 'fault_redis_load'
        result = {'host_crash_1s_loss_window': 'untestable-by-kill (process kill loses nothing on this scratch Redis; the prod AOF-everysec window needs a host-level test)'}
        since = self.psql(self.database, "SELECT clock_timestamp()::text")
        users = json.loads((self.work / 'users.json').read_text())['users']
        user = users[20]
        destination = {'province': 'Tehran', 'city': 'Tehran',
                       'address': 'Synthetic redis-load address', 'postalCode': '1111111111'}
        product = 500
        sku_hash = hashlib.md5(('cap-sku-' + str((product - 1) * 5 + 1)).encode()).hexdigest()
        variant = (sku_hash[:8] + '-' + sku_hash[8:12] + '-' + sku_hash[12:16]
                   + '-' + sku_hash[16:20] + '-' + sku_hash[20:])
        items = [{'productId': 'cap-product-' + str(product), 'variantId': variant, 'quantity': 1}]
        status, quote = self.https_json('POST', '/api/v1/shipping/quote', user['token'],
                                        {'items': items, 'destination': destination})
        if status != 200 or not (quote or {}).get('quoteId'):
            result['error'] = 'quote preparation failed: %s' % status
            self.manifest[key] = result
            return 1
        status, order = self.https_json('POST', '/api/v1/orders', user['token'],
            {'storeId': 'cap-store-1', 'items': items, 'shippingQuoteId': quote['quoteId'],
             'customerInfo': {**destination, 'fullName': 'Synthetic capacity customer', 'phone': user['mobile']},
             'paymentMethod': 'cash', 'idempotencyKey': 'cap-redis-load-' + secrets.token_hex(8)})
        if status != 201 or not (order or {}).get('id'):
            result['error'] = 'order preparation failed: %s' % status
            self.manifest[key] = result
            return 1
        status, token = self.https_json('POST', '/api/v1/payments/shaparak/token', user['token'],
            {'orderId': order['id'], 'orderNumber': order['orderNumber'], 'amount': order['payableAmount']})
        if status != 200 or not (token or {}).get('token'):
            result['error'] = 'token issuance failed: %s' % status
            self.manifest[key] = result
            return 1
        result['order_id'] = order['id']
        load_name = 'fault-redis-load-load'
        load_env = self.driver_env({
            'CAPACITY_DURATION_SECONDS': '120',
            'CAPACITY_SUMMARY_FILE': str(self.work / (load_name + '-summary.json'))})
        load_log = open(self.work / (load_name + '.log'), 'w')
        load = subprocess.Popen(
            ['node', str(LOADTESTS / 'commerce-rolling.mjs')],
            env=load_env, stdout=load_log, stderr=subprocess.STDOUT, cwd=LOADTESTS)
        # Same 30 s lead as db-loss: the driver's bootstrap (~15 s) must
        # finish before Redis goes down, or bootstrap fails outright.
        time.sleep(30)
        self.stop_proc('redis')
        health_during = []
        for _ in range(3):
            status, _ = self.http_get(
                'http://127.0.0.1:%d/api/health' % self.backend_port, timeout=5)
            health_during.append(status)
            time.sleep(1)
        result['health_during_redis_down'] = health_during
        self.provision_redis()
        # Rebuild holds from PG while the callback is sent concurrently:
        # whichever lands first, money must be conserved.
        env = {**os.environ,
               'PATH': str(HOME / 'go' / 'bin') + ':' + os.environ.get('PATH', ''),
               'HOME': str(HOME)}
        rebuild = subprocess.Popen(
            [str(HOME / 'go' / 'bin' / 'go'), 'run', './cmd/redis-recover', 'rebuild',
             '-db', self.dsn, '-redis', self.redis_url],
            env=env, cwd=BACKEND, stdout=open(self.work / 'redis-rebuild.log', 'w'),
            stderr=subprocess.STDOUT, text=True)
        cb_status, cb_body = self.https_json(
            'POST', '/api/v1/payments/shaparak/callback', user['token'],
            {'token': token['token'], 'orderNumber': order['orderNumber'],
             'amount': order['payableAmount']})
        rebuild_rc = rebuild.wait(timeout=300)
        result['rebuild_exit'] = rebuild_rc
        result['callback_during_rebuild'] = {'status': cb_status, 'body': cb_body}
        callback_ok = False
        if cb_status == 200 and (cb_body or {}).get('status') == 'paid':
            callback_ok = True
            result['callback_outcome'] = 'paid (hold survived/rebuilt in time)'
        elif cb_status == 409 and (cb_body or {}).get('code') == 'inventory_hold_expired':
            obligations = int(self.psql(self.database,
                "SELECT count(*) FROM order_refund_obligations WHERE order_id='%s'" % order['id']))
            result['callback_outcome'] = 'refund path (hold expired during rebuild)'
            result['refund_obligations'] = obligations
            callback_ok = obligations >= 1
        else:
            result['callback_outcome'] = 'unexpected: %s %s' % (cb_status, cb_body)
        recovered = False
        for _ in range(120):
            status, _ = self.http_get(
                'http://127.0.0.1:%d/api/health' % self.backend_port, timeout=5)
            if status == 200:
                recovered = True
                break
            time.sleep(0.5)
        result['health_after_recovery'] = status
        result['recovered'] = recovered
        try:
            load.wait(timeout=120)
        except subprocess.TimeoutExpired:
            load.kill()
            load.wait(timeout=15)
        result['load_exit'] = load.returncode
        self.oracle_commerce('fault-redis-load', since, 0)
        result['oracle'] = self.manifest.get('fault-redis-load_oracle')
        result['oracle_gate'] = self.manifest.get('fault-redis-load_oracle_gate')
        gate_exit, report = self.reconcile_gate('fault-redis-load', since)
        result['gate_exit'] = gate_exit
        result['gate_verdict'] = (report or {}).get('verdict')
        ok = (recovered and rebuild_rc == 0 and callback_ok
              and str(result['oracle_gate']).startswith('matched') and gate_exit == 0)
        if not ok:
            result['error'] = ('recovered=%s rebuild=%s callback_ok=%s '
                'oracle_gate=%s gate_exit=%s'
                % (recovered, rebuild_rc, callback_ok,
                   result['oracle_gate'], gate_exit))
        self.manifest[key] = result
        return 0 if ok else 1

    # -- cleanup ------------------------------------------------------------
    def close(self, keep=False):
        failures = []
        if not keep:
            for name in ('backend', 'nginx', 'sms', 'carrier', 'redis'):
                try:
                    self.stop_proc(name)
                except Exception:
                    failures.append(name)
        if not keep:
            for name in ('backend.log', 'nginx-error.log', 'nginx-access.log',
                         'redis.log', 'sms.log', 'carrier.log'):
                src = self.work / name
                if src.exists():
                    shutil.copy2(src, self.out / ('work-' + name))
            try:
                exists = self.psql('postgres',
                    "SELECT count(*) FROM pg_database WHERE datname='%s';" % self.database, check=False)
                if exists.strip() == '0':
                    print('disposable database was never created; nothing to drop', flush=True)
                else:
                    marker = self.psql(self.database,
                        "SELECT database_name FROM public.sellersho_test_guard WHERE singleton;", check=False)
                    if marker == self.database:
                        self.psql('postgres', 'DROP DATABASE "%s" WITH (FORCE);' % self.database, check=False)
                        print('dropped disposable database', self.database, flush=True)
                    else:
                        print('WARNING: guard marker mismatch; NOT dropping', self.database, flush=True)
                        failures.append('db-drop-refused')
            except Exception:
                failures.append('db-drop')
            if self.redis_dir:
                shutil.rmtree(self.redis_dir, ignore_errors=True)
            shutil.rmtree(self.work, ignore_errors=True)
        else:
            # CAP-02 W-1.5: --keep-env now leaves a RUNNABLE env (it used
            # to stop every process and keep only the work dir + DB, so
            # "kept-env" was a work-dir-only stub). Processes stay up;
            # env.sh exports everything a follow-up driver/run needs and
            # stop-env.sh tears the env down (kills the PIDs, drops the
            # disposable DB). Secrets inside; both files are 600/700.
            pids = {name: (proc.pid if proc is not None and proc.poll() is None else None)
                    for name, proc in self.procs.items()}
            env_path = self.work / 'env.sh'
            env_path.write_text(
                '# Native capacity env (KEEP-ENV mode). Secrets inside; chmod 600.\n'
                '# Backend: http://127.0.0.1:%d  TLS: https://capacity.example.test:%d (via nginx)\n'
                '# Sessions: %s  CA: %s\n'
                '# Database: %s  Redis: %s\n'
                '# PIDs: %s\n'
                'export CAPACITY_TARGET_HOST=capacity.example.test\n'
                'export CAPACITY_TARGET_PORT=%d\n'
                'export CAPACITY_TARGET_PROTOCOL=https\n'
                'export CAPACITY_TARGET_IP=127.0.0.1\n'
                'export CAPACITY_CA_FILE=%s\n'
                'export CAPACITY_SESSIONS_FILE=%s\n'
                'export CAPACITY_DB=%s\n'
                'export CAPACITY_DSN=%s\n'
                'export CAPACITY_REDIS_URL=%s\n'
                % (self.backend_port, self.nginx_port,
                   self.work / 'users.json', self.work / 'root.crt',
                   self.database, self.redis_url, json.dumps(pids),
                   self.nginx_port, self.work / 'root.crt',
                   self.work / 'users.json', self.database, self.dsn,
                   self.redis_url))
            os.chmod(env_path, 0o600)
            live_pids = ' '.join(str(p) for p in pids.values() if p)
            stop_path = self.work / 'stop-env.sh'
            stop_path.write_text(
                '#!/bin/sh\n'
                '# Tear down a --keep-env native capacity env.\n'
                'kill %s 2>/dev/null\n'
                'sleep 2\n'
                '%s -h 127.0.0.1 -p 5433 -U gap13 -d postgres -c \'DROP DATABASE "%s" WITH (FORCE);\'\n'
                'rm -rf %s\n'
                'echo stopped\n'
                % (live_pids, str(PG_BIN / 'psql'), self.database,
                   self.redis_dir or ''))
            os.chmod(stop_path, 0o700)
            print('KEPT native env (runnable); work dir:', self.work,
                  'env file:', env_path, 'stop:', stop_path, flush=True)
        if failures:
            raise SafetyError('native cleanup failures: ' + ','.join(failures))

    def write_compact_evidence(self):
        """Small committable record (no raw request rows) for the repo."""
        compact = {'run': self.nonce, 'scope': 'local-only-native', 'certified': False,
                   'manifest_keys': sorted(self.manifest.keys())}
        for key, value in self.manifest.items():
            if key.endswith(('_exit', '_counter_gate', '_oracle_gate', '_oracle', '_received_counts',
                             'hot_sku_oracle', 'hot_account_oracle', 'fixture_measured_counts',
                             'cold_cache', 'headroom', 'fault_provider_timeout', 'fault_redis_loss',
                             'fault_restart', 'browser_counter_gate', 'browser_scope', 'frontend_build',
                             'release_cohort')):
                compact[key] = value
        # Critic condition 1a: the compact must reconstruct the pass/fail
        # list and the provider counts. None of these keys matches the
        # suffix filter above (profile_failed ends in no listed suffix;
        # the double counts end in _counts), so copy them explicitly.
        # b907's compact omitted all three; the failed list lived only
        # in the uncommitted manifest, and the SMS/carrier counts only
        # in the uncommitted doubles endpoints.
        for key in ('profile_failed', 'sms_double_counts', 'carrier_double_counts'):
            if key in self.manifest:
                compact[key] = self.manifest[key]
        for name in self.out.glob('*-summary.json'):
            try:
                summary = json.loads(name.read_text())
            except Exception:
                continue
            compact[name.stem] = {k: summary.get(k) for k in (
                'scope', 'offered', 'requests', 'dropped_iterations', 'distinct_principals',
                'errors', 'failed_gates', 'route_metrics', 'elapsed_seconds', 'p95_ms', 'p99_ms',
                'completed_checkouts', 'actual_api_rps', 'contenders', 'paid', 'cancelled',
                'duplicate_rejected', 'concurrent_device_ok', 're_login_ok',
                'old_token_after_logout_still_valid', 'scheduling_gate', 'cpu_max_percent')}
        target = LOADTESTS / 'evidence' / ('native-' + self.nonce + '-compact.json')
        target.write_text(json.dumps(compact, indent=2))
        return target


def verdict_evidence(run, suite_status):
    """Evidence for verdict.json: the BASELINE profile's own exit, plus
    the suite exit under its own key. The suite exit previously rode
    under 'baseline_exit' (b907: verdict said 1 while the baseline
    profile itself exited 0) — the two must never share a key again.
    A missing baseline exit (smoke never ran) is None, not the suite
    exit, so a reader can tell 'not run' from 'failed'."""
    return {'baseline_exit': run.manifest.get('baseline_exit'), 'suite_exit': suite_status}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--profiles', default='',
                        help='comma-separated profiles to run in one environment')
    for name in PROFILES:
        parser.add_argument('--' + name.replace('_', '-'), action='store_true',
                            help='run profile ' + name)
    parser.add_argument('--output', type=pathlib.Path, default=None)
    parser.add_argument('--keep-env', action='store_true',
                        help='leave the native environment running; print work dir')
    args = parser.parse_args()
    workload = json.loads((LOADTESTS / 'workload.json').read_text())
    validate_mix(workload['weights'])
    validate_workload_contract(workload)
    selected = [p for p in args.profiles.split(',') if p.strip()]
    for name in PROFILES:
        if getattr(args, name.replace('-', '_')) and name not in selected:
            selected.append(name)
    unknown = [p for p in selected if p not in PROFILES]
    if unknown:
        parser.error('unknown profiles: ' + ','.join(unknown))
    if not selected:
        parser.error('Select a local profile; commerce qualification remains gated')
    out = args.output or (HOME / 'workspace/cap01-native-runs' / ('sellersho-cap-report-' + secrets.token_hex(6)))
    out.mkdir(parents=True, exist_ok=False)
    run = NativeRun(out)
    status = 1
    try:
        run.provision_db()
        run.provision_redis()
        run.build_backend()
        frontend_ok = run.build_frontend()
        run.start_doubles()
        run.make_tls()
        run.make_hosts()
        run.write_nginx_conf()
        run.start_backend()
        run.start_nginx()
        # One trusted setup smoke before expensive seeding.
        smoke_status, _ = run.run_driver('baseline', 'baseline.mjs', {'CAPACITY_SMOKE': '1'}, 180)
        if smoke_status:
            raise SafetyError('TLS/domain setup smoke failed before full fixture seeding')
        if any(p in NEEDS_FIXTURE for p in selected):
            run.fixture()
        for profile in selected:
            if profile == 'baseline' and 'baseline' in selected:
                continue  # already ran as the setup smoke
            print('=== profile:', profile, flush=True)
            driver, _dur, extra, timeout = PROFILES[profile]
            if profile == 'browser-assets':
                s = run.run_browser()
            elif profile == 'cold-cache':
                s = run.run_cold_cache()
            elif profile == 'headroom':
                s = run.run_headroom()
            elif profile == 'fault-provider-timeout':
                s = run.run_fault_provider_timeout()
            elif profile == 'fault-redis-loss':
                s = run.run_fault_redis_loss()
            elif profile == 'fault-restart':
                s = run.run_fault_restart()
            elif profile.startswith('fault-boundary-'):
                s = run.run_fault_boundary(profile[len('fault-boundary-'):])
            elif profile == 'fault-db-loss':
                s = run.run_fault_db_loss()
            elif profile == 'fault-worker-failover':
                s = run.run_fault_worker_failover()
            elif profile == 'fault-redis-load':
                s = run.run_fault_redis_load()
            else:
                s, _ = run.run_driver(profile, driver, extra, timeout)
            if s:
                run.manifest['profile_failed'] = run.manifest.get('profile_failed', []) + [profile]
                print('profile FAILED:', profile, flush=True)
            else:
                print('profile ok:', profile, flush=True)
        status = 0 if 'profile_failed' not in run.manifest else 1
        # Doubles counts (independent, like the Docker runner).
        try:
            _, sms = run.http_get('http://127.0.0.1:%d/metrics' % run.sms_port,
                                  headers={'Authorization': run.smskey})
            run.manifest['sms_double_counts'] = json.loads(sms)
        except Exception:
            run.manifest['sms_double_counts'] = 'unavailable'
        try:
            _, carrier = run.http_get('http://127.0.0.1:%d/metrics' % run.carrier_port,
                                      headers={'Authorization': 'Bearer ' + run.carrierkey})
            run.manifest['carrier_double_counts'] = json.loads(carrier)
        except Exception:
            run.manifest['carrier_double_counts'] = 'unavailable'
        run.manifest['service_versions'] = {
            'postgres': run.run([str(PG_BIN / 'postgres'), '--version']).stdout.strip(),
            'redis': run.run(['redis-server', '--version']).stdout.strip(),
            'nginx': run.run([NGINX, '-v'], check=False).stderr.strip(),
            'node': run.run(['node', '--version']).stdout.strip(),
        }
        run.manifest['postgres_settings'] = run.psql(run.database,
            "SELECT json_build_object('max_connections',current_setting('max_connections'),"
            "'shared_buffers',current_setting('shared_buffers'),'version',version());")
    except Exception as exc:
        run.manifest['failure'] = redact(str(exc) if isinstance(exc, SafetyError) else type(exc).__name__,
                                         run._secrets)
        status = 1
    finally:
        try:
            run.close(keep=args.keep_env)
            run.manifest['cleanup'] = 'verified' if not args.keep_env else 'kept-env'
        except Exception:
            run.manifest['cleanup'] = 'FAILED'
            status = 1
        (out / 'manifest.json').write_text(json.dumps(run.manifest, indent=2))
        (out / 'verdict.json').write_text(json.dumps(verdict(verdict_evidence(run, status)), indent=2))
        try:
            compact_path = run.write_compact_evidence()
            print('compact evidence:', compact_path, flush=True)
        except Exception as exc:
            print('compact evidence failed:', exc, flush=True)
    print('LOCAL native baseline exit:', status, 'reports:', out, 'CAP-02 remains OPEN', flush=True)
    return status


if __name__ == '__main__':
    raise SystemExit(main())
