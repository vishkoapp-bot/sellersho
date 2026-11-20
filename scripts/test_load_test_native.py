import importlib.util
import json
import pathlib
import subprocess
import sys
import tempfile
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
    def test_compact_evidence_includes_profile_failed_and_double_counts(self):
        # Critic condition 1a: the committed compact must reconstruct
        # the pass/fail list and the provider counts. b907's compact
        # omitted all three (they lived only in the uncommitted
        # manifest / doubles endpoints).
        out = pathlib.Path(tempfile.mkdtemp(prefix='cap-compact-test-'))
        evidence_dir = pathlib.Path(tempfile.mkdtemp(prefix='cap-compact-evidence-'))
        (evidence_dir / 'evidence').mkdir()
        real_loadtests = M.LOADTESTS
        M.LOADTESTS = evidence_dir
        try:
            run = object.__new__(M.NativeRun)
            run.nonce = 'testnonce0123456789abcd'
            run.out = out
            run.manifest = {'profile_failed': ['api-baseline', 'hot-sku'],
                            'sms_double_counts': {'sms_sends': 10, 'sms_rejected': 0},
                            'carrier_double_counts': {'quotes': 5, 'rejected': 0},
                            'baseline_exit': 1}
            target = run.write_compact_evidence()
            self.assertTrue(str(target).startswith(str(evidence_dir)))
            compact = json.loads(target.read_text())
            self.assertEqual(compact['profile_failed'], ['api-baseline', 'hot-sku'])
            self.assertEqual(compact['sms_double_counts'], {'sms_sends': 10, 'sms_rejected': 0})
            self.assertEqual(compact['carrier_double_counts'], {'quotes': 5, 'rejected': 0})
            self.assertIn('profile_failed', compact['manifest_keys'])
        finally:
            M.LOADTESTS = real_loadtests

    @unittest.skipIf(M is None, 'native runner missing')
    def test_verdict_evidence_uses_baseline_profile_exit_not_suite_exit(self):
        # Critic condition 1b: b907's verdict.json said baseline_exit 1
        # (the SUITE exit) while the baseline profile itself exited 0.
        run = object.__new__(M.NativeRun)
        run.manifest = {'baseline_exit': 0}
        evidence = M.verdict_evidence(run, 1)
        self.assertEqual(evidence['baseline_exit'], 0)
        self.assertEqual(evidence['suite_exit'], 1)
        run.manifest = {'baseline_exit': 1}
        self.assertEqual(M.verdict_evidence(run, 1)['baseline_exit'], 1)
        run.manifest = {}
        self.assertIsNone(M.verdict_evidence(run, 1)['baseline_exit'])

    @unittest.skipIf(M is None, 'native runner missing')
    def test_fault_restart_does_not_clobber_hot_sku(self):
        # Behavioral (replaces the old source-grep test): drive the REAL
        # run_fault_restart with stubbed environment collaborators (no
        # DB/backend) and assert the main hot-sku summary, oracle and
        # exit survive the re-run + restart. 2026-11-20 the re-run used
        # the driver's own name ('hot-sku') and overwrote the main
        # run's 5-paid summary with its expected 409.
        out = pathlib.Path(tempfile.mkdtemp(prefix='cap-fault-restart-test-'))
        main_summary = {'scope': 'hot-sku', 'paid': 5, 'requests': 100}
        (out / 'hot-sku-summary.json').write_text(json.dumps(main_summary))
        matched_oracle = {'source': 5, 'sellable': 0, 'captured': 5, 'sku': 0,
                          'paid_orders': 5, 'amount_mismatch': 0}

        class FakeProc:
            def kill(self):
                pass

            def wait(self, timeout=None):
                return 0

        run = object.__new__(M.NativeRun)
        run.out = out
        run.manifest = {'hot-sku_exit': 0,
                        'hot_sku_oracle': dict(matched_oracle),
                        'hot_sku_oracle_gate': 'matched: five conserved units'}
        run.procs = {'backend': FakeProc()}
        calls = []

        def fake_run_driver(name, driver, extra, timeout):
            calls.append(name)
            # Simulate the re-run: it writes ONLY its own summary file
            # and its own exit key.
            (out / (name + '-summary.json')).write_text(
                json.dumps({'scope': name, 'paid': 0}))
            run.manifest[name + '_exit'] = 1
            return 1, {'scope': name}

        def fake_oracle_hot_sku(status, key='hot_sku_oracle', exit_key='hot-sku_exit'):
            run.manifest[key] = dict(run.manifest['hot_sku_oracle'])
            run.manifest[key + '_gate'] = 'matched: five conserved units'

        def fake_start_backend(payment_mode='success'):
            calls.append('start_backend')

        run.run_driver = fake_run_driver
        run.oracle_hot_sku = fake_oracle_hot_sku
        run.start_backend = fake_start_backend
        status = run.run_fault_restart()
        self.assertEqual(status, 0)
        self.assertEqual(json.loads((out / 'hot-sku-summary.json').read_text()), main_summary)
        self.assertEqual(run.manifest['hot-sku_exit'], 0)
        self.assertEqual(run.manifest['hot_sku_oracle'], matched_oracle)
        self.assertIn('hot-sku-restart', calls)
        self.assertNotIn('hot-sku', calls)
        self.assertTrue(run.manifest['fault_restart']['converged'])
        self.assertEqual(run.manifest['fault_restart']['oracle_before_restart'], matched_oracle)
        self.assertEqual(run.manifest['fault_restart']['oracle_after_restart'], matched_oracle)

    @unittest.skipIf(M is None, 'native runner missing')
    def test_free_port_is_loopback_usable(self):
        port = M.free_port()
        self.assertIsInstance(port, int)
        self.assertTrue(1 <= port <= 65535)


if __name__ == '__main__':
    unittest.main(verbosity=2)
