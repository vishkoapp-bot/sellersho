"""Exercise deploy.ps1's server-build delivery without Git, SSH or a build.

The copied real script runs in an owned directory with fail-closed PowerShell
command shims. Only its emitted remote scripts reach Bash, with -n (parse only).
Run: python scripts/test_deploy_server_build.py -v

Docker prerequisite: the frontend-build tests (exercise_frontend_build_phase —
test_successful_frontend_build_discards_only_generated_dependencies and the
test_failed_or_incomplete_frontend_build_blocks_the_next_phase subtests) run a
REAL `docker run ... node:24-alpine` container. Start Docker Desktop before
running this file, or those tests fail with docker API connection errors
because the daemon is not reachable. The other tests do not need Docker.
"""
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
COMMIT = 'a' * 40
BASH = Path('D:/Git/bin/bash.exe')
# REL-02: build images are digest-pinned in deploy.ps1 to the exact images
# running on prod 2026-11-20. Keep these constants in sync with deploy.ps1.
NODE_IMAGE = 'node:24-alpine@sha256:116b800c3754b440ee0aa3ed2f20f5bf3d99a554bfd9adeea09ea8017f6e85f5'
GO_IMAGE = 'golang:1.23@sha256:60deed95d3888cc5e4d9ff8a10c54e5edc008c6ae3fba6187be6fb592e19e8c0'


class ServerBuildDelivery(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.lock = json.loads((ROOT / 'package-lock.json').read_text())
        with tempfile.TemporaryDirectory(prefix='sellersho-server-build-') as folder:
            owned = Path(folder)
            (owned / 'deploy.ps1').write_bytes((ROOT / 'deploy.ps1').read_bytes())
            # SEC-01 gate files: deploy.ps1 runs scripts/run_secret_checks_nodocker.py
            # against this owned dir, and that gate imports
            # scripts/test_production_secrets (which imports
            # scripts/credential_inventory) and reads docker-compose.prod.yml.
            # Copy the real files so the gate runs against the real production
            # compose text instead of failing on a missing module/file.
            (owned / 'scripts').mkdir()
            for name in ('run_secret_checks_nodocker.py', 'test_production_secrets.py',
                         'credential_inventory.py'):
                (owned / 'scripts' / name).write_bytes(
                    (ROOT / 'scripts' / name).read_bytes())
            (owned / 'docker-compose.prod.yml').write_bytes(
                (ROOT / 'docker-compose.prod.yml').read_bytes())
            # No real command can fall through to PATH except the test's own
            # Python: deploy.ps1's SEC-01 gate resolves python via Get-Command,
            # which needs PATH to contain the Python directory. Every other
            # tool stays shimmed — PowerShell functions take precedence over
            # PATH lookup, so git/ssh/scp/npm/go/tar/docker still cannot fall
            # through. Backslashes are literal in a single-quoted PowerShell
            # string; a single quote in the path is doubled.
            python_dir = str(Path(sys.executable).resolve().parent)
            wrapper = r'''
$ErrorActionPreference = 'Stop'
$env:PATH = '__PYTHON_DIR__'
$global:remoteBodies = [System.Collections.Generic.List[string]]::new()
$global:gitCalls = [System.Collections.Generic.List[string]]::new()
function global:git {
    $op = ($args | Select-Object -Skip 4) -join ' '
    $global:gitCalls.Add($op)
    $global:LASTEXITCODE = 0
    switch -Exact ($op) {
        'rev-parse HEAD' { 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' }
        'branch --show-current' { 'codex/server-build-fixture' }
        'status --porcelain --untracked-files=no' { }
        'rev-parse refs/remotes/origin/codex/server-build-fixture' { 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' }
        default { throw "Forbidden Git operation: $op" }
    }
}
function global:ssh {
    begin { $lines = [System.Collections.Generic.List[string]]::new() }
    process { $lines.Add([string]$_) }
    end {
        if (($args -join ' ') -ne '-o BatchMode=yes -o ConnectTimeout=20 fixture.invalid bash -se') {
            throw 'Unexpected SSH destination/arguments'
        }
        $global:remoteBodies.Add(($lines -join "`n"))
        $global:LASTEXITCODE = 0
    }
}
function global:scp { throw 'Uploads forbidden in server-build fixture' }
function global:npm { throw 'Local npm forbidden' }
function global:go { throw 'Local Go forbidden' }
function global:tar { throw 'Local archive forbidden' }
function global:docker { throw 'Local Docker forbidden' }
& (Join-Path $PSScriptRoot 'deploy.ps1') -BuildOnServer -SshHost fixture.invalid
@{ bodies = @($global:remoteBodies); git = @($global:gitCalls) } |
    ConvertTo-Json -Depth 4 | Set-Content -Encoding UTF8 (Join-Path $PSScriptRoot 'captured.json')
'''.replace('__PYTHON_DIR__', python_dir.replace("'", "''"))
            runner = owned / 'capture.ps1'
            runner.write_text(wrapper, encoding='utf-8')
            shell = shutil.which('powershell') or shutil.which('pwsh')
            if not shell or not BASH.is_file():
                raise RuntimeError('PowerShell and D:/Git/bin/bash.exe required')
            result = subprocess.run([shell, '-NoProfile', '-NonInteractive',
                                     '-ExecutionPolicy', 'Bypass', '-File', str(runner)],
                                    capture_output=True, text=True, timeout=30)
            if result.returncode:
                raise AssertionError(result.stdout + result.stderr)
            cls.capture = json.loads((owned / 'captured.json').read_text(encoding='utf-8-sig'))
        cls.preflight, cls.release = cls.capture['bodies']
        cls.commands = [shlex.split(line) for line in cls.release.splitlines()
                        if line.startswith('docker run ')]
        cls.node, cls.go = cls.commands

    def test_actual_remote_scripts_parse_and_deliver_guarded_release(self):
        for body in self.capture['bodies']:
            parsed = subprocess.run([str(BASH), '-n'], input=body, text=True,
                                    capture_output=True, timeout=10)
            self.assertEqual(parsed.returncode, 0, parsed.stderr)
        self.assertEqual(len(self.capture['git']), 4)
        self.assertIn('information_schema.tables', self.preflight)
        self.assertIn("exit 1", self.preflight)
        self.assertIn('test ! -e "$release/docs/fa"', self.release)
        self.assertIn('test ! -e "$release/backend/internal/networkreward"', self.release)
        self.assertIn(f"test \"$(git -C '/opt/drup' rev-parse FETCH_HEAD)\" = '{COMMIT}'", self.release)
        self.assertIn(f"git -C '/opt/drup' archive '{COMMIT}' | tar -xf - -C \"$release\"", self.release)
        # The existing runner owns backup, migration, runtime credentials and health.
        # Its exact release path/arguments must still be delivered after both builds.
        delivery = f'python3 "$release/scripts/deploy_remote.py" \'/opt/drup\' \'{COMMIT}\' \'0\' \'0\''
        self.assertIn(delivery, self.release)
        self.assertGreater(self.release.index(delivery), self.release.index('golang:'))
        # REL-02 R-1: the literal 'release.json' below is the fail-fast guard
        # deploy.ps1 emits after make_manifest.py (test -f "$release/release.json").
        # History: the assertion was true when the remote script hand-wrote the
        # manifest inline (c270fcc); MIG-02 (92fe711) replaced that with the
        # make_manifest.py invocation and dropped the reference, so the filename
        # lived only inside make_manifest.py and the assertion could never pass.
        # The guard must sit after the make_manifest.py invocation and before
        # deploy_remote.py consumes the manifest.
        self.assertIn(f'--commit "{COMMIT}" --branch "codex/server-build-fixture"', self.release)
        self.assertIn('release.json', self.release)
        self.assertGreater(self.release.index('test -f "$release/release.json"'),
                           self.release.index('python3 "$release/scripts/release/make_manifest.py"'))
        self.assertLess(self.release.index('test -f "$release/release.json"'),
                        self.release.index('python3 "$release/scripts/deploy_remote.py"'))

    def test_node_image_supports_actual_locked_puppeteer_engine(self):
        engine = self.lock['packages']['node_modules/puppeteer']['engines']['node']
        floor = re.fullmatch(r'>=(\d+)\.(\d+)\.(\d+)', engine)
        self.assertIsNotNone(floor, 'Review changed locked Node engine contract')
        image = next(arg for arg in self.node if arg.startswith('node:'))
        self.assertEqual(image, NODE_IMAGE, 'Build Node image must stay digest-pinned (REL-02)')
        major = int(re.fullmatch(r'node:(\d+)-alpine(?:@sha256:[0-9a-f]{64})?', image).group(1))
        self.assertGreaterEqual((major, 0, 0), tuple(map(int, floor.groups())),
                                f'{image} cannot satisfy locked Puppeteer engine {engine}')
        self.assertEqual(major, 24, 'Use the locally validated Node major')

    def test_browser_skip_is_container_only_and_other_install_scripts_enabled(self):
        self.assertIn('PUPPETEER_SKIP_DOWNLOAD=true', self.node)
        self.assertEqual(self.node[self.node.index('PUPPETEER_SKIP_DOWNLOAD=true') - 1], '-e')
        self.assertNotIn('PUPPETEER_SKIP_DOWNLOAD=true', self.go)
        self.assertTrue(self.node[-1].startswith('npm ci --no-audit --no-fund && npm run build'))
        self.assertNotIn('--ignore-scripts', self.node[-1])

    def test_build_storage_preserves_download_caches_but_discards_go_compiler_cache(self):
        for command in self.commands:
            self.assertIn('--rm', command)
        self.assertIn('sellersho-npm-build-cache:/root/.npm', self.node)
        self.assertIn('$release:/app', self.node)
        self.assertIn('sellersho-go-mod-cache:/go/pkg/mod', self.go)
        self.assertIn('$release/backend:/app', self.go)
        mounts = [self.go[i + 1] for i, arg in enumerate(self.go) if arg == '-v']
        self.assertEqual(set(mounts), {'$release/backend:/app', 'sellersho-go-mod-cache:/go/pkg/mod'})
        self.assertIn(GO_IMAGE, self.go, 'Build Go image must stay digest-pinned (REL-02)')
        for value in ('CGO_ENABLED=0', 'GOMAXPROCS=1'):
            self.assertIn(value, self.go)
            self.assertEqual(self.go[self.go.index(value) - 1], '-e')
        self.assertIn('-o bin/server-linux ./cmd/server', self.go[-1])
        self.assertIn('-o bin/migrate-linux ./cmd/migrate', self.go[-1])

    def test_actual_puppeteer_loader_skips_download_with_emitted_flag(self):
        self.assertIn('PUPPETEER_SKIP_DOWNLOAD=true', self.node)
        with tempfile.TemporaryDirectory(prefix='sellersho-puppeteer-skip-') as folder:
            owned = Path(folder)
            cache = owned / 'cache'
            env = {k: v for k, v in os.environ.items()
                   if not k.upper().startswith(('PUPPETEER_', 'NPM_CONFIG_', 'NODE_'))}
            env['PUPPETEER_SKIP_DOWNLOAD'] = 'true'
            env['PUPPETEER_CACHE_DIR'] = str(cache)
            # Import installed upstream code, not a reimplementation. Reject any
            # attempted network connection even if the skip contract regresses.
            probe = '''
import net from 'node:net';
import {syncBuiltinESMExports} from 'node:module';
net.Socket.prototype.connect = () => { throw Error('Network forbidden in skip probe'); };
syncBuiltinESMExports();
const {getConfiguration} = await import(CONFIG);
const {downloadBrowsers} = await import(INSTALL);
const configuration = await getConfiguration();
if (configuration.skipDownload !== true) throw Error('skipDownload was not true');
await downloadBrowsers();
console.log(JSON.stringify({skipDownload: configuration.skipDownload}));
'''.replace('CONFIG', json.dumps((ROOT / 'node_modules/puppeteer/lib/puppeteer/getConfiguration.js').as_uri())).replace('INSTALL', json.dumps((ROOT / 'node_modules/puppeteer/lib/puppeteer/node/install.js').as_uri()))
            result = subprocess.run([shutil.which('node'), '--input-type=module', '-e', probe],
                                    cwd=owned, env=env, capture_output=True, text=True, timeout=20)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn('"skipDownload":true', result.stdout)
            self.assertEqual(list(owned.rglob('*')), [], 'Skip must not download/create cache files')
            print('Puppeteer installed loader/downloadBrowsers: skipDownload=true; files=0; network forbidden')

    def exercise_frontend_build_phase(self, failure=''):
        # Execute the exact emitted build shell in an owned Linux container.
        # npm is a fixture: package installation and Vite are covered separately.
        # The filesystem effects and shell failure propagation are real.
        with tempfile.TemporaryDirectory(prefix='sellersho-build-phase-') as folder:
            owned = Path(folder).resolve()
            (owned / 'tools').mkdir()
            (owned / 'package.json').write_text('{"fixture":true}')
            stub = '''#!/bin/sh
set -eu
if [ "$1" = ci ]; then
  mkdir -p /app/node_modules
  echo generated > /app/node_modules/build-dependency
  [ "$FAIL_PHASE" != install ] || exit 21
else
  [ "$FAIL_PHASE" != build ] || exit 22
  mkdir -p /app/dist
  [ "$FAIL_PHASE" = missing-artifact ] || echo built > /app/dist/index.html
fi
'''
            (owned / 'tools/npm').write_text(stub, encoding='utf-8', newline='\n')
            # cp makes the executable independent of Windows bind-mount modes.
            prepare = 'cp /app/tools/npm /usr/local/bin/npm && chmod 755 /usr/local/bin/npm && '
            command = ['docker', 'run', '--rm', '--network', 'none',
                       '--mount', 'type=bind,source=' + str(owned) + ',target=/app',
                       '--workdir', '/app', '--env', 'FAIL_PHASE=' + failure,
                       'node:24-alpine', 'sh', '-c', prepare + self.node[-1]]
            result = subprocess.run(command, capture_output=True, text=True, timeout=30)
            self.assertEqual((owned / 'package.json').read_text(), '{"fixture":true}')
            self.assertTrue((owned / 'tools/npm').is_file(), 'Source must be preserved')
            if not failure:
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual((owned / 'dist/index.html').read_text().strip(), 'built')
                self.assertFalse((owned / 'node_modules').exists(),
                                 'Generated npm dependencies must not overlap the Go build peak')
            else:
                self.assertNotEqual(result.returncode, 0, 'Failed/missing build must block release')
                self.assertTrue((owned / 'node_modules/build-dependency').is_file(),
                                'Failed build retains its generated diagnostics')

    def test_successful_frontend_build_discards_only_generated_dependencies(self):
        self.exercise_frontend_build_phase()

    def test_failed_or_incomplete_frontend_build_blocks_the_next_phase(self):
        for failure in ['install', 'build', 'missing-artifact']:
            with self.subTest(failure=failure):
                self.exercise_frontend_build_phase(failure)


if __name__ == '__main__':
    unittest.main()
