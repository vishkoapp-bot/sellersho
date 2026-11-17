"""Verify the real release runtime assignment merges a 30s backend stop grace."""
import ast
import json
from pathlib import Path
import subprocess
import tempfile
import unittest


class DeploymentStopGrace(unittest.TestCase):
    def test_runtime_override_preserves_environment_and_sets_30_seconds(self):
        source = Path(__file__).with_name('deploy_remote.py').read_text(encoding='utf-8')
        tree = ast.parse(source)
        assignments = [node for node in tree.body if isinstance(node, ast.Assign)
                       and any(isinstance(target, ast.Name) and target.id == 'runtime'
                               for target in node.targets)]
        self.assertEqual(len(assignments), 1)
        environment = {'FIXTURE_RUNTIME_VALUE': 'preserved'}
        namespace = {'runtime_env': environment}
        exec(compile(ast.Module(body=assignments, type_ignores=[]), '<release-runtime>', 'exec'), namespace)
        runtime = namespace['runtime']
        self.assertIs(runtime['services']['backend']['environment'], environment)
        self.assertEqual(runtime['services']['backend']['stop_grace_period'], '30s')
        with tempfile.TemporaryDirectory(prefix='sellersho-stop-grace-') as folder:
            base, override = Path(folder) / 'base.json', Path(folder) / 'runtime.json'
            base.write_text(json.dumps({'services': {'backend': {'image': 'alpine:3.19',
                             'stop_grace_period': '10s', 'environment': {'BASE_VALUE': 'retained'}}}}))
            override.write_text(json.dumps(runtime))
            result = subprocess.run(['docker', 'compose', '-p', 'sellersho-grace-fixture',
                                     '-f', str(base), '-f', str(override), 'config', '--format', 'json'],
                                    text=True, capture_output=True, timeout=20)
            self.assertEqual(result.returncode, 0, 'fixture Compose merge failed')
            backend = json.loads(result.stdout)['services']['backend']
            self.assertEqual(backend['stop_grace_period'], '30s')
            self.assertEqual(backend['environment'], dict(environment, BASE_VALUE='retained'))


if __name__ == '__main__':
    unittest.main()
