import importlib.util
import pathlib
import unittest
import subprocess
import sys
from unittest import mock
from types import SimpleNamespace

ROOT = pathlib.Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('capacity', ROOT / 'scripts/load_test_isolated.py')
M = importlib.util.module_from_spec(SPEC) if SPEC and SPEC.loader and pathlib.Path(SPEC.origin).exists() else None
if M: SPEC.loader.exec_module(M)

class GuardTests(unittest.TestCase):
    def test_external_target_flag_refused_before_docker(self):
        if M is None: self.skipTest('guard not implemented')
        result=subprocess.run([sys.executable,str(ROOT/'scripts/load_test_isolated.py'),'--baseline','--url','https://example.com'],capture_output=True,text=True)
        self.assertEqual(result.returncode,2)
        self.assertIn('unrecognized arguments',result.stderr)

    def test_private_metrics_counts_separate_probes_domain_and_api(self):
        self.assertTrue(M and hasattr(M,'metrics_counts'), 'independent private-counter parser missing')
        sample='sellersho_http_duration_seconds_count{class="api",route="/api/v1/auth/me"} 1000\nsellersho_http_duration_seconds_count{class="domain_allow",route="/internal/domains/allow"} 1001\nsellersho_http_duration_seconds_count{class="api",route="/api/health"} 1\n'
        self.assertEqual(M.metrics_counts(sample), {'api':1000,'domain_allow':1001,'probe':1,'asset':0,'other':0})

    def test_guard_exists(self):
        self.assertIsNotNone(M, 'isolated capacity guard is missing')

    @unittest.skipIf(M is None, 'guard not implemented')
    def test_reject_external_or_replaced_identity(self):
        valid = {'Id': 'abc', 'Name': '/cap-test', 'Config': {'Labels': {M.LABEL: 'nonce'}},
                 'NetworkSettings': {'Networks': {'cap-net': {'NetworkID':'net-id'}}}}
        M.verify_container(valid, 'abc', 'cap-test', 'nonce', 'cap-net', 'net-id')
        for key, value in [('Id', 'replaced'), ('Name', '/production'), ('Config', {}), ('NetworkSettings', {'Networks': {'bridge': {}}})]:
            with self.subTest(key=key), self.assertRaises(M.SafetyError):
                M.verify_container(dict(valid, **{key: value}), 'abc', 'cap-test', 'nonce', 'cap-net', 'net-id')

    def test_attachment_network_id_must_match(self):
        base={'Id':'abc','Name':'/cap-test','Config':{'Labels':{M.LABEL:'nonce'}},'NetworkSettings':{'Networks':{'cap-net':{'NetworkID':'net-id'}}}}
        for attachment in ({},{'NetworkID':'other'}):
            bad=dict(base,NetworkSettings={'Networks':{'cap-net':attachment}})
            with self.subTest(attachment=attachment),self.assertRaises(M.SafetyError):M.verify_container(bad,'abc','cap-test','nonce','cap-net','net-id')

    def test_prestart_requires_created_state_and_immutable_network_configuration(self):
        info={'Id':'abc','Name':'/cap-test','Config':{'Labels':{M.LABEL:'nonce'}},'HostConfig':{'NetworkMode':'net-id'},'State':{'Status':'created'},'NetworkSettings':{'Networks':{'cap-net':{'NetworkID':''}}}}
        M.verify_created_container(info,'abc','cap-test','nonce','cap-net','net-id')
        with self.assertRaises(M.SafetyError):M.verify_container(info,'abc','cap-test','nonce','cap-net','net-id')
        for changed in ({'HostConfig':{'NetworkMode':'cap-net'}},{'State':{'Status':'running'}},{'NetworkSettings':{'Networks':{'cap-net':{'NetworkID':'other'}}}}):
            with self.subTest(changed=changed),self.assertRaises(M.SafetyError):M.verify_created_container(dict(info,**changed),'abc','cap-test','nonce','cap-net','net-id')

    def test_cleanup_refuses_container_deletion_when_network_guard_fails(self):
        run=M.Run.__new__(M.Run)
        run.containers=[('abc','cap-test')];run.network_id='net-id';run.network='cap-net';run.nonce='nonce';run.browser_image_id=None;run.work=ROOT/'never-delete'
        run.verify_network=mock.Mock(side_effect=M.SafetyError('network identity changed'))
        run.docker=mock.Mock(return_value='[{"Id":"abc","Name":"/cap-test","Config":{"Labels":{"sellersho.capacity-run":"nonce"}},"NetworkSettings":{"Networks":{"cap-net":{"NetworkID":"net-id"}}}}]')
        with mock.patch.object(M.shutil,'rmtree'),mock.patch.object(M.Run,'delete_owned_work',create=True):
            with self.assertRaises(M.SafetyError):run.close()
        self.assertFalse(any(c.args and c.args[0]=='rm' for c in run.docker.call_args_list))

    def test_owned_temp_cleanup_refuses_outside_replaced_or_junction_targets(self):
        owned=ROOT/'mock-temp'/'sellersho-cap-owned'
        for case in ('outside','replaced','junction','resolved_outside','wrong_parent'):
            run=M.Run.__new__(M.Run);run.owned_work_path=owned;run.temp_parent=owned.parent;run.owned_work_identity=(1,2)
            work=mock.MagicMock();run.work=work
            work.absolute.return_value=ROOT/'outside' if case=='outside' else owned
            work.resolve.return_value=ROOT/'outside' if case=='resolved_outside' else owned
            work.is_symlink.return_value=False;work.is_junction.return_value=case=='junction'
            work.lstat.return_value=SimpleNamespace(st_dev=1,st_ino=3 if case=='replaced' else 2)
            if case=='wrong_parent':run.temp_parent=ROOT/'other-temp'
            with self.subTest(case=case),mock.patch.object(pathlib.Path,'resolve',return_value=run.temp_parent),mock.patch.object(M.shutil,'rmtree') as delete:
                with self.assertRaises(M.SafetyError):run.delete_owned_work()
                delete.assert_not_called()

    def test_owned_temp_cleanup_accepts_only_captured_identity(self):
        run=M.Run.__new__(M.Run);owned=ROOT/'mock-temp'/'sellersho-cap-owned';run.owned_work_path=owned;run.temp_parent=owned.parent;run.owned_work_identity=(1,2)
        work=mock.MagicMock();run.work=work;work.absolute.return_value=owned;work.resolve.return_value=owned
        work.lstat.return_value=SimpleNamespace(st_dev=1,st_ino=2);work.is_symlink.return_value=False;work.is_junction.return_value=False
        with mock.patch.object(pathlib.Path,'resolve',return_value=run.temp_parent),mock.patch.object(M.shutil,'rmtree') as delete:
            run.delete_owned_work();delete.assert_called_once_with(work)

    @unittest.skipIf(M is None, 'guard not implemented')
    def test_mix_is_exact_contract(self):
        M.validate_mix([30,20,15,15,8,4,4,4])
        for mix in ([100], [30,20,15,15,8,4,4,3], [20,30,15,15,8,4,4,4]):
            with self.assertRaises(M.SafetyError): M.validate_mix(mix)

    @unittest.skipIf(M is None, 'guard not implemented')
    def test_mix_weights_must_sum_to_100(self):
        # Same total via a different split must still be refused: the
        # contract is the exact mix AND the 100 total, not either alone.
        with self.assertRaises(M.SafetyError): M.validate_mix([25,25,25,25])
        with self.assertRaises(M.SafetyError): M.validate_mix([30,20,15,15,8,4,4,5])
        M.validate_mix([30,20,15,15,8,4,4,4])

    @unittest.skipIf(M is None, 'guard not implemented')
    def test_workload_contract_gates_unread_fields(self):
        import json
        workload = json.loads((ROOT/'load-tests/workload.json').read_text())
        M.validate_workload_contract(workload)
        for key, bad in [('cpu_max_percent', 80), ('unexpected_error_rate_lt', 0.01),
                         ('headroom_rps', 150), ('recovery_seconds', 300),
                         ('total_api_rps', 50), ('dropped_iterations', 1)]:
            with self.subTest(key=key), self.assertRaises(M.SafetyError):
                M.validate_workload_contract(dict(workload, **{key: bad}))

    @unittest.skipIf(M is None, 'guard not implemented')
    def test_failed_or_incomplete_runs_never_qualify(self):
        for evidence in ({}, {'errors': 0, 'identities': 1000}, {'dropped_iterations': 1}, {'wrong_amount': 1}):
            self.assertFalse(M.verdict(evidence)['certified'])

    @unittest.skipIf(M is None, 'guard not implemented')
    def test_fixture_floor_and_unique_sessions(self):
        fixture = {'users': [{'id': str(i), 'token': str(i)} for i in range(1000)],
                   'counts': {'products':10000,'skus':50000,'orders':100000,'stores':100,'suppliers':100,'customers':1000},
                   'queues': {'create':28800,'initiate':28800,'callback':28800}, 'saleable_units':86400}
        M.validate_fixture(fixture,7200)
        fixture['users'][999]['token']='0'
        with self.assertRaises(M.SafetyError): M.validate_fixture(fixture,7200)
        fixture['users'][999]['token']='999'
        fixture['queues']['callback']=28799
        with self.assertRaises(M.SafetyError): M.validate_fixture(fixture,7200)

if __name__ == '__main__': unittest.main(verbosity=2)
