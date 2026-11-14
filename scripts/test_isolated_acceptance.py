#!/usr/bin/env python3
"""Live TEST-01 sentinel and refusal checks; Docker only, no application database.

Run from repository root: python scripts/test_isolated_acceptance.py
Default-suite pre-existing failures are reported separately from safety assertions.
"""
import importlib.util
from pathlib import Path
import sys
import time

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location('isolated', Path(__file__).with_name('test-isolated.py'))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


def main():
    with runner.DisposableDatabase() as db:
        db.sql('CREATE TABLE sentinel (id integer PRIMARY KEY, value text); '
               "INSERT INTO sentinel VALUES (1, 'must remain unchanged');", db.database)
        snapshot_sql = ("SELECT md5(string_agg(n.nspname || '.' || c.relname || ':' || c.relkind::text, ',' ORDER BY n.nspname, c.relname)) "
                        "FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'; "
                        'SELECT id, value FROM sentinel ORDER BY id;')
        before = db.sql(snapshot_sql, db.database)
        # Let completed sessions flush pg_stat_database. Control queries run in postgres.
        time.sleep(1.1)
        sessions_sql = "SELECT sessions FROM pg_stat_database WHERE datname='" + db.database + "';"
        sessions_before = db.sql(sessions_sql)
        env = db.environment()
        for key in list(env):
            if key in runner.TEST_URL_KEYS or key.endswith('_DSN'):
                del env[key]
        # A reachable application URL must not be consulted without integration opt-in.
        env['DATABASE_URL'] = db.dsn
        status = db.command(['go', 'test', './...', '-count=1'], env=env)
        time.sleep(1.1)
        sessions_after = db.sql(sessions_sql)
        assert sessions_after == sessions_before, 'default suite connected to application sentinel'
        assert db.sql(snapshot_sql, db.database) == before, 'default suite changed sentinel schema/data'
        print('PASS: default suite made zero sentinel connections; schema/data unchanged')
        print('Default suite process exit status: ' + str(status))
        db.sql('DROP TABLE public.sellersho_test_guard;', db.database)
        unmarked = db.sql(snapshot_sql, db.database)
        status = db.command(['go', 'test', './internal/repository',
                             '-run', '^TestConcurrentPaymentCallbacksConsumeInventoryOnce$', '-count=1'])
        assert status != 0, 'unmarked database was not refused'
        assert db.sql(snapshot_sql, db.database) == unmarked, 'unmarked test target was written'
        print('PASS: unmarked database refused before fixture schema/data writes')
    # Real bootstrap failure: successful start then simulated setup failure must remove it.
    db = runner.DisposableDatabase()
    start = db.start
    def fail_setup():
        start()
        raise runner.SafetyError('intentional bootstrap failure')
    db.start = fail_setup
    try:
        with db:
            raise AssertionError('failed setup allowed child command')
    except runner.SafetyError:
        pass
    assert db.inspect() is None, 'setup failure leaked a disposable container'
    print('PASS: bootstrap failure removed verified disposable container')
    db = runner.DisposableDatabase()
    with db:
        assert db.command(['go', 'test', './internal/testdb', '-run', '^TestDisposableIdentity$', '-count=1']) == 0
        # A nonzero child status must remain nonzero while the context cleans up.
        status = db.command(['go', 'tool', 'definitely-not-a-real-go-tool'])
        assert status != 0
    assert db.inspect() is None, 'test failure leaked a disposable container'
    print('PASS: child failure preserved nonzero status and removed container')


if __name__ == '__main__':
    main()
