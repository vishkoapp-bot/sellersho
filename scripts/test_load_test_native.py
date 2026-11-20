import importlib.util
import pathlib
import subprocess
import sys
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('native', ROOT / 'scripts/load_test_native.py')
M = importlib.util.module_from_spec(SPEC) if SPEC and SPEC.loader and pathlib.Path(SPEC.origin).exists() else None
if M:
    SPEC.loader.exec_module(M)


class NativeRunnerTests(unittest.TestCase):
    def test_module_loads(self):
        self.assertIsNotNone(M, 'native runner module missing')

    @unittest.skipIf(M is None, 'native runner missing')
    def test_no_target_parameter(self):
        # D4: the runner must not accept any target/URL/host argument at
        # all — the disposable domain is hardcoded, never supplied.
        result = subprocess.run([sys.executable, str(ROOT / 'scripts/load_test_native.py'),
                                 '--baseline', '--url', 'https://example.com'],
                                capture_output=True, text=True)
        self.assertEqual(result.returncode, 2)
        self.assertIn('unrecognized arguments', result.stderr)

    @unittest.skipIf(M is None, 'native runner missing')
    def test_short_baseline_suite_profiles_exist(self):
        # D3: pinned short baseline suite durations.
        for name in ('baseline', 'fixture-baseline', 'api-baseline', 'api-rolling',
                     'login-ramp', 'otp-background', 'journey-baseline', 'hot-sku'):
            self.assertIn(name, M.PROFILES)
        self.assertEqual(M.PROFILES['api-baseline'][2], {})
        self.assertEqual(M.PROFILES['api-rolling'][2]['CAPACITY_DURATION_SECONDS'], '30')
        self.assertEqual(M.PROFILES['journey-baseline'][2]['CAPACITY_DURATION_SECONDS'], '90')
        self.assertEqual(M.PROFILES['staff-baseline'][2]['CAPACITY_DURATION_SECONDS'], '30')
        self.assertEqual(M.PROFILES['api-sustained'][2]['CAPACITY_DURATION_SECONDS'], '1800')
        self.assertEqual(M.PROFILES['api-soak'][2]['CAPACITY_DURATION_SECONDS'], '7200')

    @unittest.skipIf(M is None, 'native runner missing')
    def test_redact_removes_secrets_and_jwts(self):
        secret = 's3cr3t-value'
        text = M.redact('password=' + secret + ' token=eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.signature', [secret])
        self.assertNotIn(secret, text)
        self.assertNotIn('eyJhbGciOiJIUzI1NiJ9', text)
        self.assertIn('[redacted]', text)

    @unittest.skipIf(M is None, 'native runner missing')
    def test_oracle_sql_percent_formats_evaluate(self):
        # 2026-11-20: oracle_commerce crashed the suite with ValueError
        # (unsupported format character) the first time api-baseline
        # completed — LIKE 'cap-product-%' / 'cap-customer-%' inside a
        # %-formatted SQL string must be escaped as %%. Evaluate every
        # ts-formatted oracle SQL block in the runner source.
        import re
        src = (ROOT / 'scripts/load_test_native.py').read_text()
        sites = list(re.finditer(r'""" % \(ts(, ts)*\)', src))
        self.assertGreaterEqual(len(sites), 2, 'expected oracle %-format sites')
        for site in sites:
            n = site.group(0).count('ts')
            q2 = site.start()
            q1 = src.rfind('"""', 0, q2 - 1)
            block = src[q1 + 3:q2]
            out = block % (('2026-11-20 00:00:00+00',) * n)
            self.assertNotIn('%%', out)
            self.assertIn("LIKE 'cap-", out)

    @unittest.skipIf(M is None, 'native runner missing')
    def test_fault_restart_does_not_clobber_hot_sku(self):
        # 2026-11-20: run_fault_restart re-ran the driver as 'hot-sku',
        # overwriting the main profile's summary/oracle/exit with the
        # re-run's expected 409 (stock already consumed). The re-run
        # must use its own name and oracle key.
        src = (ROOT / 'scripts/load_test_native.py').read_text()
        body = src[src.index('def run_fault_restart'):src.index('def close(')]
        self.assertIn("run_driver('hot-sku-restart'", body)
        self.assertNotIn("run_driver('hot-sku',", body)
        self.assertIn("key='hot_sku_oracle_restart'", body)

    @unittest.skipIf(M is None, 'native runner missing')
    def test_free_port_is_loopback_usable(self):
        port = M.free_port()
        self.assertIsInstance(port, int)
        self.assertTrue(1 <= port <= 65535)


if __name__ == '__main__':
    unittest.main(verbosity=2)
