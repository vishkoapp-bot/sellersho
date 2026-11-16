#!/usr/bin/env python3
"""Real Linux migrator permission regression; no database/network/credentials.

Run with --migrator pointing to an explicitly verified Linux binary. The
--baseline-red mode reproduces the previous copy-then-exec path without repair
and must fail with the named permission regression after verifying exit126.
"""
import argparse
import io
import json
from pathlib import Path
import secrets
import subprocess
import tarfile

LABEL = 'sellersho.deploy-permissions-test'


def command(*args, check=True):
    result = subprocess.run(args, text=True, encoding='utf-8', errors='replace',
                            capture_output=True, timeout=45)
    if check and result.returncode:
        raise RuntimeError('Permission-fixture command failed: ' + result.stderr.strip())
    return result


class OwnedLinuxContainer:
    def __init__(self):
        self.token = secrets.token_hex(12)
        self.name = 'sellersho-deploy-permissions-' + self.token
        self.container_id = None

    def inspect(self):
        result = command('docker', 'container', 'inspect', self.container_id or self.name, check=False)
        if result.returncode:
            if 'No such' in result.stderr:
                return None
            raise RuntimeError('Cannot verify owned permission-test container')
        return json.loads(result.stdout)[0]

    def verify(self, info):
        if not isinstance(info, dict):
            raise RuntimeError('Owned permission-test container missing')
        cfg = info['HostConfig']
        mounts = info.get('Mounts', [])
        if (info['Name'] != '/' + self.name or
                info['Config'].get('Labels', {}).get(LABEL) != self.token or
                info['Id'] != self.container_id or cfg['NetworkMode'] != 'none' or
                not cfg['ReadonlyRootfs'] or cfg.get('Binds') or cfg.get('PortBindings') or
                set(cfg.get('Tmpfs', {})) != {'/tmp'} or
                'exec' not in cfg['Tmpfs']['/tmp'].split(',') or
                any(m['Type'] != 'tmpfs' or m['Destination'] != '/tmp' for m in mounts)):
            raise RuntimeError('Permission-test identity/isolation mismatch; operation refused')

    def start(self):
        # Use the locally available minimal Linux image; no provider/network work.
        command('docker', 'image', 'inspect', 'alpine:latest')
        self.container_id = command('docker', 'create', '--name', self.name,
                                    '--label', LABEL + '=' + self.token,
                                    '--network', 'none', '--read-only', '--tmpfs',
                                    '/tmp:rw,exec,nosuid,nodev,size=32m,mode=1777',
                                    'alpine:latest', 'sleep', '300').stdout.strip()
        self.verify(self.inspect())
        command('docker', 'start', self.container_id)

    def close(self):
        info = self.inspect()
        if info is not None:
            self.verify(info)
            command('docker', 'rm', '--force', self.container_id)
        if self.inspect() is not None:
            raise RuntimeError('Owned permission-test cleanup failed')
        print('Verified disposable container cleanup: ' + self.name, flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--migrator', type=Path, required=True)
    parser.add_argument('--baseline-red', action='store_true')
    args = parser.parse_args()
    binary = args.migrator.resolve(strict=True)
    payload = binary.read_bytes()
    if payload[:4] != b'\x7fELF':
        raise SystemExit('Explicit Linux ELF migrator required')
    prepare = None
    if not args.baseline_red:
        from deploy_migration_stage import prepare_migrator
        prepare = prepare_migrator
    owned = OwnedLinuxContainer()
    try:
        owned.start()
        command('docker', 'exec', owned.container_id, 'mkdir', '-p', '/tmp/release-stage')
        target = '/tmp/release-stage/migrate'
        for mode in ('0755-control', '0644', '0666', '0755'):
            owned.verify(owned.inspect())
            # Docker Desktop's cp/archive API cannot address exec-created tmpfs
            # directories. Extract a one-file tar inside the owned mount instead;
            # this also preserves the exact incoming Unix mode on Windows.
            data = io.BytesIO()
            with tarfile.open(fileobj=data, mode='w') as archive:
                entry = tarfile.TarInfo('migrate')
                entry.size, entry.mode = len(payload), int(mode.split('-')[0], 8)
                archive.addfile(entry, io.BytesIO(payload))
            copied = subprocess.run(['docker', 'exec', '-i', owned.container_id,
                                     'tar', '-xpf', '-', '-C', '/tmp/release-stage'],
                                    input=data.getvalue(), capture_output=True, timeout=45)
            if copied.returncode:
                raise RuntimeError('Owned tmpfs fixture extraction failed: ' + copied.stderr.decode('utf-8', errors='replace'))
            actual = command('docker', 'exec', owned.container_id, 'stat', '-c', '%a', target).stdout.strip()
            assert actual == mode.split('-')[0].lstrip('0'), 'Linux input mode precondition failed'
            if mode == '0755-control':
                control = command('docker', 'exec', owned.container_id, target, '-h', check=False)
                assert control.returncode == 0, 'Linux executable/mount positive control failed before regression'
                print('Verified0755 Linux ELF positive control; tmpfs permits execution', flush=True)
                continue
            if prepare is not None:
                prepare(command, owned.container_id, target)
            result = command('docker', 'exec', owned.container_id, target, '-h', check=False)
            output = result.stdout + result.stderr
            if args.baseline_red:
                assert result.returncode == 126 and 'permission denied' in output.lower(), 'not the intended Linux permission regression'
                print('Observed actual Linux mode0644 migrator -h exit126: permission denied', flush=True)
                raise AssertionError('REGRESSION_RED: copied migrator is not executable before database work')
            assert result.returncode == 0, 'Prepared Linux migrator -h failed with exit' + str(result.returncode)
            assert '-adopt-legacy' in output and '-migration-timeout' in output, 'actual migrator flags not observed'
            actual = command('docker', 'exec', owned.container_id, 'stat', '-c', '%a', target).stdout.strip()
            assert actual == '755', 'Migrator mode must be0755, including no world-write'
            print('PASS input' + mode + ' ->0755 actual Linux migrator -h exit0', flush=True)
    finally:
        owned.close()
    print('PERMISSIONS RESULT cases=3 failures=0; no database or credentials supplied', flush=True)
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
