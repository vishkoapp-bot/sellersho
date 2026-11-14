"""Safety regressions for the disposable Docker lifecycle (no Docker required)."""
import importlib.util
from pathlib import Path
import sys
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
path = Path(__file__).with_name('test-isolated.py')
spec = importlib.util.spec_from_file_location('isolated', path)
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class LifecycleTests(unittest.TestCase):
    def test_schema_only_opt_in_is_not_inherited_from_parent(self):
        db = runner.DisposableDatabase()
        db.dsn = 'disposable-child-only'
        with patch.dict('os.environ', {'SELLERSHO_TEST_SCHEMA_ONLY': '1'}):
            self.assertNotIn('SELLERSHO_TEST_SCHEMA_ONLY', db.environment())
            db.empty_schema = True
            self.assertEqual(db.environment()['SELLERSHO_TEST_SCHEMA_ONLY'], '1')

    def test_missing_container_during_bootstrap_is_explicitly_refused(self):
        db = runner.DisposableDatabase()
        with self.assertRaises(runner.SafetyError):
            db.verify(None)

    def test_all_legacy_targets_are_overridden_with_disposable_identity(self):
        db = runner.DisposableDatabase()
        db.dsn = 'disposable-child-only'
        with patch.dict('os.environ', {'CAPABILITY_AUDIT_DATABASE_URL': 'external', 'SKU_COMMERCE_DATABASE_URL': 'external', 'UNRELATED_DATABASE_URL': 'external'}):
            env = db.environment()
        self.assertEqual(env['CAPABILITY_AUDIT_DATABASE_URL'], 'disposable-child-only')
        self.assertEqual(env['SKU_COMMERCE_DATABASE_URL'], 'disposable-child-only')
        self.assertNotIn('UNRELATED_DATABASE_URL', env)

    def test_provider_credentials_are_not_inherited_by_fixture_commands(self):
        db = runner.DisposableDatabase()
        db.dsn = 'disposable-child-only'
        with patch.dict('os.environ', {'STRIPE_API_KEY': 'external', 'ADMIN_PASSWORD': 'external', 'TRACKING_WEBHOOK_SECRET': 'external'}):
            env = db.environment()
        self.assertNotIn('STRIPE_API_KEY', env)
        self.assertNotIn('ADMIN_PASSWORD', env)
        self.assertNotIn('TRACKING_WEBHOOK_SECRET', env)

    def test_cleanup_refuses_replaced_container_before_remove(self):
        db = runner.DisposableDatabase()
        db.container_id = 'a' * 64
        wrong = {'Id': 'b' * 64, 'Name': '/' + db.name,
                 'Config': {'Labels': {runner.LABEL: db.token}}}
        with patch.object(db, 'inspect', return_value=wrong), patch.object(db, 'docker') as docker:
            with self.assertRaises(runner.SafetyError):
                db.close()
            docker.assert_not_called()

    def test_cleanup_refuses_wrong_owner_before_remove(self):
        db = runner.DisposableDatabase()
        wrong = {'Id': 'a' * 64, 'Name': '/' + db.name,
                 'Config': {'Labels': {runner.LABEL: 'other'}}}
        with patch.object(db, 'inspect', return_value=wrong), patch.object(db, 'docker') as docker:
            with self.assertRaises(runner.SafetyError):
                db.close()
            docker.assert_not_called()

    def test_failed_bootstrap_always_attempts_cleanup(self):
        db = runner.DisposableDatabase()
        with patch.object(db, 'start', side_effect=runner.SafetyError('bootstrap failed')), patch.object(db, 'close') as close:
            with self.assertRaises(runner.SafetyError):
                with db:
                    self.fail('bootstrap must prevent tests')
            close.assert_called_once()

    def test_test_failure_is_preserved_and_cleanup_runs(self):
        db = runner.DisposableDatabase()
        with patch.object(db, 'start'), patch.object(db, 'close') as close:
            with self.assertRaisesRegex(RuntimeError, 'test failed'):
                with db:
                    raise RuntimeError('test failed')
            close.assert_called_once()

    def test_cleanup_failure_cannot_report_success(self):
        db = runner.DisposableDatabase()
        with patch.object(db, 'start'), patch.object(db, 'close', side_effect=runner.SafetyError('cleanup failed')):
            with self.assertRaises(runner.SafetyError):
                with db:
                    pass


if __name__ == '__main__':
    unittest.main()
