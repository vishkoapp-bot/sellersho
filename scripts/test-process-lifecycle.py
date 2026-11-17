#!/usr/bin/env python3
"""Actual Linux SIGTERM/checkout replay inside an identity-checked disposable PG.

No host volumes, production configuration, provider calls, external DSNs or
paused source. The baseline mode reproduces the original server.Serve path with
no signal handler and is expected to fail the admitted-request assertion.
"""
import argparse
import importlib.util
import io
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
from urllib.parse import urlsplit, urlunsplit

spec = importlib.util.spec_from_file_location('isolated', Path(__file__).with_name('test-isolated.py'))
isolated = importlib.util.module_from_spec(spec)
spec.loader.exec_module(isolated)


def run(baseline):
    with isolated.DisposableDatabase() as db, tempfile.TemporaryDirectory(prefix='sellersho-process-') as build:
        if db.command(['go', 'run', './cmd/migrate', '-dir', 'migrations']):
            return 1
        binary = Path(build) / 'server.test'
        env = dict(db.environment(), GOOS='linux', GOARCH='amd64', CGO_ENABLED='0')
        if db.command(['go', 'test', '-c', '-o', str(binary), './cmd/server'], env=env):
            return 1
        data = io.BytesIO()
        with tarfile.open(fileobj=data, mode='w') as archive:
            archive.add(binary, arcname='ops01/server.test')
            for migration in (isolated.BACKEND / 'migrations').iterdir():
                if migration.is_file():
                    archive.add(migration, arcname='ops01/migrations/' + migration.name)
        db.verify(db.inspect())
        copied = subprocess.run(['docker', 'cp', '-', db.container_id + ':/tmp'],
                                input=data.getvalue(), capture_output=True, timeout=30)
        if copied.returncode:
            raise isolated.SafetyError('Cannot copy owned Linux process fixture')
        db.docker('exec', db.container_id, 'chmod', '0755', '/tmp/ops01/server.test')
        db.docker('exec', db.container_id, 'mkdir', '-p', '/tmp/ops01/cmd/server')
        parts = urlsplit(db.dsn)
        internal = urlunsplit((parts.scheme, parts.netloc.rsplit(':', 1)[0] + ':5432', parts.path, parts.query, ''))
        child_env = db.environment()
        for key in ('DATABASE_URL', *isolated.TEST_URL_KEYS):
            child_env[key] = internal
        child_env['SELLERSHO_LINUX_PROCESS_FIXTURE'] = '1'
        child_env['SELLERSHO_LIFECYCLE_BASELINE'] = '1' if baseline else '0'
        keys = ['SELLERSHO_TEST_DATABASE_URL', 'GAP13_TEST_DATABASE_URL',
                'SELLERSHO_LINUX_PROCESS_FIXTURE', 'SELLERSHO_LIFECYCLE_BASELINE']
        command = ['docker', 'exec', '--workdir', '/tmp/ops01/cmd/server']
        for key in keys:
            command += ['--env', key]
        command += [db.container_id, '/tmp/ops01/server.test', '-test.v',
                    '-test.run=^TestProcessSIGTERMCheckoutRestartConservation$', '-test.timeout=30s']
        # command() streams only redacted output and inherits only owned fixture URLs.
        return db.command(command, env=child_env)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--baseline-red', action='store_true')
    args = parser.parse_args()
    try:
        sys.exit(run(args.baseline_red))
    except (isolated.SafetyError, OSError, subprocess.SubprocessError):
        print('Owned Linux process fixture setup failed; details withheld', file=sys.stderr)
        sys.exit(2)
