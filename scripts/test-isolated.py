#!/usr/bin/env python3
"""Disposable integration runner. Requires Python 3, Docker and Go.

    python scripts/test-isolated.py
    python scripts/test-isolated.py -- go test ./internal/repository -count=1
    python scripts/test-isolated.py -- python ../scripts/my-fixture-probe.py
    python scripts/test-isolated.py --schema-only -- go test ./internal/database/seeder -count=1

Commands run from backend/ with SELLERSHO_TEST_DATABASE_URL, TEST_DATABASE_URL,
TEAM04_TEST_DATABASE_URL and DATABASE_URL pointing at a freshly migrated database.
Use a child command for fixture work: the database exists only for its lifetime.
No external DSN, production config, persistent volume or keep mode is accepted.
SIGINT/SIGTERM and setup/test failures all use identity-checked cleanup. SIGKILL or
host failure cannot run cleanup; remove only the printed container name after
checking its sellersho.test-run label, never broad Docker prune commands.
"""
import argparse
import json
import os
from pathlib import Path
import secrets
import signal
import subprocess
import sys
import time

# Windows redirected streams otherwise default to cp1252 and can abort on the
# application's Persian validation messages before Go returns its test status.
for stream in (sys.stdout, sys.stderr):
    if hasattr(stream, 'reconfigure'):
        stream.reconfigure(encoding='utf-8', errors='replace')

LABEL = 'sellersho.test-run'
BACKEND = Path(__file__).resolve().parent.parent / 'backend'
# Legacy opt-in names retained by existing suites. Every one resolves to this run.
TEST_URL_KEYS = (
    'SELLERSHO_TEST_DATABASE_URL', 'TEST_DATABASE_URL', 'TEAM03_DATABASE_URL',
    'TEAM04_TEST_DATABASE_URL', 'TEAM04B_TEST_DATABASE_URL', 'TEAM05_TEST_DATABASE_URL',
    'TEAM09_WORKER_DATABASE_URL', 'CAPABILITY_AUDIT_DATABASE_URL',
    'SKU_COMMERCE_DATABASE_URL', 'GRANULAR_REVIEW_TEST_DATABASE_URL',
    'FINANCE_PAYOUT_TEST_DATABASE_URL', 'SUPPLIER_NOTIFICATIONS_TEST_DATABASE_URL',
    'OPERATIONS_TEST_DATABASE_URL', 'NOTIFY_WRITERS_TEST_DATABASE_URL',
    'GAP13_TEST_DATABASE_URL', 'SUPPLIER_FLOW_TEST_DATABASE_URL',
    'REJECT_NOTIFY_TEST_DATABASE_URL',
)


class SafetyError(RuntimeError):
    pass


class DisposableDatabase:
    def __init__(self):
        self.token = secrets.token_hex(12)
        self.name = 'sellersho-test-' + self.token
        self.database = 'sellersho_test_' + self.token
        self.password = secrets.token_hex(24)
        self.container_id = None
        self.dsn = None
        self.empty_schema = False

    def redact(self, value):
        cache = getattr(self, 'redis', None)
        for secret in (self.dsn, self.password, getattr(cache, 'dsn', None), getattr(cache, 'password', None)):
            if secret:
                value = value.replace(secret, '[redacted]')
        return value

    def docker(self, *args, input=None, check=True, env=None):
        result = subprocess.run(['docker', *args], input=input, text=True,
                                encoding='utf-8', capture_output=True, env=env, timeout=90)
        if check and result.returncode:
            raise SafetyError('Docker operation failed: ' + self.redact(result.stderr.strip()))
        return result

    def inspect(self):
        # Query only our unique name or resolved ID; never list/modify other targets.
        target = self.container_id or self.name
        result = self.docker('container', 'inspect', target, check=False)
        if result.returncode:
            if 'No such' in result.stderr:
                return None
            raise SafetyError('Cannot verify disposable container identity; cleanup refused')
        return json.loads(result.stdout)[0]

    def verify(self, info):
        if (not isinstance(info, dict) or info.get('Name') != '/' + self.name or
                info.get('Config', {}).get('Labels', {}).get(LABEL) != self.token or
                (self.container_id and info.get('Id') != self.container_id)):
            raise SafetyError('Disposable container identity mismatch; operation refused')

    def sql(self, sql, database='postgres'):
        return self.docker('exec', '-i', self.container_id, 'psql', '-X',
                           '-v', 'ON_ERROR_STOP=1', '-U', 'postgres', '-d', database,
                           '-At', input=sql).stdout.strip()

    def start(self):
        env = dict(os.environ, POSTGRES_PASSWORD=self.password)
        created = self.docker('create', '--name', self.name, '--label', LABEL + '=' + self.token,
                              '--publish', '127.0.0.1::5432', '--mount',
                              'type=tmpfs,destination=/var/lib/postgresql/data',
                              '--env', 'POSTGRES_PASSWORD', 'postgres:16-alpine', env=env)
        self.container_id = created.stdout.strip()
        info = self.inspect()
        self.verify(info)
        self.docker('start', self.container_id)
        deadline = time.monotonic() + 60
        # Initdb's temporary server has a Unix socket but never listens on TCP.
        while self.docker('exec', self.container_id, 'pg_isready', '-h', '127.0.0.1', '-U', 'postgres', check=False).returncode:
            if time.monotonic() > deadline:
                raise SafetyError('Disposable PostgreSQL startup timed out')
            time.sleep(0.3)
        self.sql("CREATE ROLE test_runner LOGIN PASSWORD '" + self.password + "' NOSUPERUSER NOCREATEDB NOCREATEROLE;\n"
                 'CREATE DATABASE ' + self.database + ' OWNER test_runner;')
        actual = self.sql('SELECT current_database();', self.database)
        if actual != self.database:
            raise SafetyError('Database identity mismatch before marker creation')
        self.sql('SET ROLE test_runner; CREATE TABLE public.sellersho_test_guard ('
                 'singleton boolean PRIMARY KEY CHECK (singleton), database_name text NOT NULL); '
                 "INSERT INTO public.sellersho_test_guard VALUES (TRUE, '" + self.database + "');", self.database)
        info = self.inspect()
        self.verify(info)
        ports = info['NetworkSettings']['Ports']['5432/tcp']
        if len(ports) != 1 or ports[0]['HostIp'] != '127.0.0.1':
            raise SafetyError('Disposable PostgreSQL must bind only IPv4 loopback')
        port = int(ports[0]['HostPort'])
        self.dsn = f'postgres://test_runner:{self.password}@127.0.0.1:{port}/{self.database}?sslmode=disable'
        print('Disposable database ready in ' + self.name, flush=True)

    def environment(self):
        # Discard inherited DB/provider credentials; do not consume repository .env.
        env = {k: v for k, v in os.environ.items() if not (
            k.endswith(('_DATABASE_URL', '_DSN', '_REDIS_URL', '_REDIS_TOKEN')) or
            any(part in k for part in ('PASSWORD', 'SECRET', 'API_KEY', 'PRIVATE_KEY', 'ACCESS_TOKEN')) or
            k.startswith(('PG', 'POSTGRES_', 'IPPANEL_', 'PAYMENT_', 'TEAM04_', 'TEST_DATABASE', 'SELLERSHO_TEST_')) or
            k in ('DATABASE_URL', 'REDIS_URL', 'JWT_SECRET', 'TRACKING_WEBHOOK_SECRET'))}
        for key in ('DATABASE_URL', *TEST_URL_KEYS):
            env[key] = self.dsn
        env.update(APP_ENV='test', JWT_SECRET=secrets.token_hex(32), PAYMENT_GATEWAY_MODE='mock')
        cache = getattr(self, 'redis', None)
        if cache:
            env.update(SELLERSHO_TEST_REDIS_URL=cache.dsn, SELLERSHO_TEST_REDIS_TOKEN=cache.token)
        if self.empty_schema:
            env['SELLERSHO_TEST_SCHEMA_ONLY'] = '1'
        return env

    def schema_only(self):
        """Copy the canonical migrated schema, including immutable-ledger triggers,
        to another unique empty fixture DB. No migration demo rows are copied.
        The dump stays in memory; it never includes data or runtime credentials.
        """
        info = self.inspect()
        self.verify(info)
        dump = self.docker('exec', self.container_id, 'pg_dump', '--schema-only',
                           '--no-owner', '--no-privileges', '-U', 'postgres', '-d', self.database).stdout
        original = self.database
        empty = original + '_empty'
        self.sql('CREATE DATABASE ' + empty + ' OWNER test_runner;')
        if self.sql('SELECT current_database();', empty) != empty:
            raise SafetyError('Empty fixture database identity mismatch')
        self.sql('SET ROLE test_runner;\n' + dump, empty)
        self.sql("SET ROLE test_runner; INSERT INTO public.sellersho_test_guard VALUES (TRUE, '" + empty + "');", empty)
        verified = self.sql('SELECT current_database() = database_name FROM public.sellersho_test_guard WHERE singleton;', empty)
        role_safe = self.sql("SELECT NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole FROM pg_roles WHERE rolname='test_runner';")
        if verified != 't' or role_safe != 't':
            raise SafetyError('Empty fixture marker/role verification failed')
        self.database = empty
        self.dsn = self.dsn.replace('/' + original + '?', '/' + empty + '?')
        self.empty_schema = True
        print('Verified empty current-schema fixture with non-superuser role and intact triggers', flush=True)

    def command(self, args, env=None, cwd=None):
        # Stream redacted output, including failures, without echoing command/env secrets.
        with subprocess.Popen(args, cwd=cwd if cwd is not None else BACKEND, env=env if env is not None else self.environment(), text=True,
                              stdout=subprocess.PIPE, stderr=subprocess.STDOUT, encoding='utf-8', errors='replace') as child:
            try:
                for line in child.stdout:
                    print(self.redact(line), end='', flush=True)
                return child.wait()
            except BaseException:
                child.terminate()
                try:
                    child.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    child.kill()
                    child.wait()
                raise

    def close(self):
        info = self.inspect()
        if info is None:
            return
        self.verify(info)
        resolved = info['Id']
        self.docker('rm', '--force', '--volumes', resolved)
        if self.inspect() is not None:
            raise SafetyError('Disposable cleanup did not remove the verified container')
        print('Verified disposable container cleanup: ' + self.name, flush=True)

    def __enter__(self):
        try:
            self.start()
            return self
        except BaseException:
            self.close()
            raise

    def __exit__(self, *_):
        self.close()


class DisposableRedis(DisposableDatabase):
    """Separate loopback-only, authenticated tmpfs cache; never host Redis."""
    def __init__(self):
        super().__init__()
        self.name = 'sellersho-test-redis-' + self.token

    def cli(self, *args, check=True):
        env = dict(os.environ, REDISCLI_AUTH=self.password)
        return self.docker('exec', '--env', 'REDISCLI_AUTH', self.container_id,
                           'redis-cli', '-n', '15', '--raw', *args, check=check, env=env)

    def start(self):
        created = self.docker('create', '--name', self.name, '--label', LABEL + '=' + self.token,
                              '--publish', '127.0.0.1::6379', '--mount', 'type=tmpfs,destination=/data',
                              'redis:7-alpine', 'redis-server', '--save', '', '--appendonly', 'no',
                              '--requirepass', self.password)
        self.container_id = created.stdout.strip()
        self.verify(self.inspect())
        self.docker('start', self.container_id)
        deadline = time.monotonic() + 30
        while self.cli('PING', check=False).stdout.strip() != 'PONG':
            if time.monotonic() > deadline:
                raise SafetyError('Disposable Redis startup timed out')
            time.sleep(0.2)
        info = self.inspect()
        self.verify(info)
        ports = info['NetworkSettings']['Ports']['6379/tcp']
        if len(ports) != 1 or ports[0]['HostIp'] != '127.0.0.1':
            raise SafetyError('Disposable Redis must bind only IPv4 loopback')
        self.dsn = f'redis://:{self.password}@127.0.0.1:{int(ports[0]["HostPort"])}/15'
        if self.cli('SET', 'sellersho:test-guard', self.token).stdout.strip() != 'OK':
            raise SafetyError('Cannot provision disposable Redis marker')
        print('Disposable Redis ready in ' + self.name, flush=True)

    def reset(self):
        # Only the runner's verified container and DB15 may be cleared between
        # tests. Check the marker before clearing; tests cannot provision it.
        self.verify(self.inspect())
        if self.cli('GET', 'sellersho:test-guard').stdout.strip() != self.token:
            raise SafetyError('Redis disposable marker mismatch; reset refused')
        if self.cli('FLUSHDB').stdout.strip() != 'OK':
            raise SafetyError('Cannot reset disposable Redis fixture')
        if self.cli('SET', 'sellersho:test-guard', self.token).stdout.strip() != 'OK':
            raise SafetyError('Cannot restore disposable Redis marker')


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--schema-only', action='store_true', help='Use empty migrated schema for seeder lifecycle tests, preserving all triggers')
    parser.add_argument('command', nargs=argparse.REMAINDER, help='command run inside the protected fixture lifetime')
    args = parser.parse_args()
    command = args.command
    if command[:1] == ['--']:
        command = command[1:]
    command = command or ['go', 'test', './...', '-count=1']
    def interrupt(_signum, _frame):
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, interrupt)
    try:
        with DisposableDatabase() as db, DisposableRedis() as cache:
            db.redis = cache
            status = db.command(['go', 'run', './cmd/migrate', '-dir', 'migrations'])
            if status:
                return status
            if args.schema_only:
                db.schema_only()
            return db.command(command)
    except KeyboardInterrupt:
        print('Interrupted; disposable cleanup attempted', file=sys.stderr)
        return 130
    except (SafetyError, OSError, subprocess.SubprocessError) as exc:
        # Exceptions never contain raw child command arguments or environment values.
        print('Isolated test runner failed: ' + str(exc), file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
