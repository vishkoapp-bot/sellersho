#!/usr/bin/env python3
"""Run each top-level Go test against its own migrated database clone.

The container, loopback binding, restricted SQL role and database markers use the
same guards as test-isolated.py. Test binaries are compiled once per package.
Only the runner can create/drop databases; tests retain NOCREATEDB privileges.
"""
import argparse
import importlib.util
import os
from pathlib import Path
import re
import secrets
import subprocess
import sys
import tempfile

spec = importlib.util.spec_from_file_location('isolated', Path(__file__).with_name('test-isolated.py'))
isolated = importlib.util.module_from_spec(spec)
spec.loader.exec_module(isolated)


def run_suite(packages, pattern):
    failures = []
    total = 0
    with isolated.DisposableDatabase() as db, isolated.DisposableRedis() as cache, tempfile.TemporaryDirectory(prefix='sellersho-suite-') as build:
        db.redis = cache
        if db.command(['go', 'run', './cmd/migrate', '-dir', 'migrations']):
            return 1
        seeded_name, seeded_dsn = db.database, db.dsn
        db.schema_only()
        empty_name, empty_dsn = db.database, db.dsn
        listed = subprocess.run(['go', 'list', *packages], cwd=isolated.BACKEND,
                                env=db.environment(), text=True, capture_output=True, check=True)
        for index, package in enumerate(listed.stdout.splitlines()):
            directory = subprocess.run(['go', 'list', '-f', '{{.Dir}}', package], cwd=isolated.BACKEND,
                                       env=db.environment(), text=True, capture_output=True, check=True).stdout.strip()
            binary = Path(build) / ('suite-' + str(index) + ('.exe' if os.name == 'nt' else ''))
            if db.command(['go', 'test', '-c', '-o', str(binary), package]):
                failures.append(package + ':compile')
                continue
            if not binary.exists():
                continue
            discovery = subprocess.run([str(binary), '-test.list', '^Test'], cwd=directory,
                                       env=db.environment(), text=True, capture_output=True, check=True)
            for test in discovery.stdout.splitlines():
                if not re.fullmatch(r'Test\w+', test) or not re.search(pattern, test):
                    continue
                total += 1
                schema_only = package.endswith('/internal/database/seeder')
                template, template_dsn = (empty_name, empty_dsn) if schema_only else (seeded_name, seeded_dsn)
                clone = 'sellersho_test_clone_' + secrets.token_hex(12)
                db.verify(db.inspect())
                db.sql('CREATE DATABASE ' + clone + ' WITH TEMPLATE ' + template + ' OWNER test_runner;')
                try:
                    db.sql("UPDATE public.sellersho_test_guard SET database_name='" + clone + "';", clone)
                    db.database = clone
                    db.dsn = template_dsn.replace('/' + template + '?', '/' + clone + '?')
                    db.empty_schema = schema_only
                    cache.reset()
                    print('ISOLATED TEST ' + package + ':' + test, flush=True)
                    if db.command([str(binary), '-test.run', '^' + test + '$', '-test.v', '-test.timeout', '3m'], cwd=directory):
                        failures.append(package + ':' + test)
                finally:
                    db.verify(db.inspect())
                    db.sql('DROP DATABASE ' + clone + ' WITH (FORCE);')
                    db.database, db.dsn = seeded_name, seeded_dsn
                    db.empty_schema = False
        print('ISOLATED RESULT tests=' + str(total) + ' failures=' + str(len(failures)), flush=True)
        for failure in failures:
            print('ISOLATED FAILURE ' + failure, flush=True)
    return int(bool(failures))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--run', default='.')
    parser.add_argument('packages', nargs='*', default=['./...'])
    args = parser.parse_args()
    sys.exit(run_suite(args.packages, args.run))
