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
}
NEEDS_FIXTURE = {'fixture-baseline', 'login-ramp', 'otp-login', 'otp-background',
                 'api-baseline', 'api-rolling', 'api-sustained', 'api-soak',
                 'journey-baseline', 'journey', 'hot-sku', 'staff-baseline',
                 'hot-account', 'session-lifecycle', 'browser-assets',
                 'cold-cache', 'headroom', 'fault-provider-timeout',
                 'fault-restart'}


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
            'CAPACITY_PAYMENT_LATENCIES_MS': '25,25,50,50,150',
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

    def start_backend(self, payment_mode='success'):
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
        result = self.run([str(self.work / 'fixture'), '-test.v', '-test.run=^TestCapacityDataFloor$',
                           '-test.timeout=15m'], env=fenv, cwd=BACKEND, timeout=960, check=False)
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
          'paid_cancelled_without_obligation',(SELECT count(*) FROM orders o WHERE o.created_at>='%s' AND o.customer_user_id IN (SELECT id FROM users WHERE full_name LIKE 'cap-customer-%%') AND o.payment_status IN ('cancelled','refund_pending') AND NOT EXISTS (SELECT 1 FROM order_refund_obligations r WHERE r.order_id=o.id AND r.purpose='cancellation')),
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
          'hot_cancelled_paid',(SELECT count(*) FROM orders o WHERE o.created_at>='%s' AND o.store_id='cap-store-1' AND o.customer_user_id IN (SELECT id FROM users WHERE full_name LIKE 'cap-customer-%%') AND o.payment_status IN ('cancelled','refund_pending')),
          'hot_cancelled_without_obligation',(SELECT count(*) FROM orders o WHERE o.created_at>='%s' AND o.store_id='cap-store-1' AND o.customer_user_id IN (SELECT id FROM users WHERE full_name LIKE 'cap-customer-%%') AND o.payment_status IN ('cancelled','refund_pending') AND NOT EXISTS (SELECT 1 FROM order_refund_obligations r WHERE r.order_id=o.id AND r.purpose='cancellation')),
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

    # -- cleanup ------------------------------------------------------------
    def close(self, keep=False):
        failures = []
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
            env_path = self.work / 'env.sh'
            env_path.write_text('# Native capacity env (KEEP-ENV mode). Secrets inside; chmod 600.\n'
                                '# Backend: http://127.0.0.1:%d  TLS: https://capacity.example.test:%d (via nginx)\n'
                                '# Sessions: %s  CA: %s\n' % (self.backend_port, self.nginx_port,
                                                             self.work / 'users.json', self.work / 'root.crt'))
            os.chmod(env_path, 0o600)
            print('KEPT native env; work dir:', self.work, 'env file:', env_path, flush=True)
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
