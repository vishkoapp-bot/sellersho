#!/usr/bin/env python3
"""Run the previous release's real workers against additive JOB-01 schema.

Only the marker-guarded disposable suite provisions PostgreSQL and Redis.
No production connection, provider credentials or current worker code is used.
"""
import argparse
import hashlib
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
MIGRATION = '202611150007_background_jobs.up.sql'
TESTS = (
    'TestDeliveryLockReleaserStagedCycle',
    'TestOperationsCycleEmitsCorrelatedSQLAndJob',
    'TestOperationsOrderExpiryAndPaymentRace',
    'TestOperationsQCDeadline',
    'TestOperationsSLAThresholdAndManualOverride',
    'TestOperationsAutomaticSLARevocation',
    'TestOperationsAdDeduplication',
    'TestOperationsAuthorizationAndDomainPermission',
)
STARVATION_TEST = 'TestLegacySchedulerSlowDeliveryDoesNotStarveExpiry'
STARVATION_FIXTURE = 'scripts/fixtures/background-legacy/starvation_test.go'


def overlay_current_allowed_backend(git_root, source):
    # Copy only Go source/module files into the disposable prior archive. Filter
    # protected paths before opening anything; no build sees paused rewards.
    names = subprocess.check_output([
        'git', '-C', git_root, 'ls-files', '-z', '--cached', '--others',
        '--exclude-standard', '--', 'backend',
    ]).decode('utf-8').split('\0')
    for name in names:
        if not name or name.startswith('backend/internal/networkreward/'):
            continue
        if not (name.endswith('.go') or name in ('backend/go.mod', 'backend/go.sum')):
            continue
        original = ROOT / name
        if not original.is_file():
            continue
        target = source / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(original, target)
    for name in ('scripts/test-suite-isolated.py', 'scripts/test-isolated.py'):
        shutil.copyfile(ROOT / name, source / name)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('base', help='full previous committed source SHA')
    modes = parser.add_mutually_exclusive_group()
    modes.add_argument('--starvation-red', action='store_true', help='verify the actual old serial scheduler fails the named runtime assertion')
    modes.add_argument('--starvation-current', action='store_true', help='run the identical barrier fixture against current allowed backend source')
    args = parser.parse_args()
    if not re.fullmatch(r'[0-9a-f]{40}', args.base):
        raise SystemExit('pass the full previous committed source SHA')
    base = args.base
    git_root = subprocess.check_output([
        'git', '-C', str(ROOT), 'rev-parse', '--show-toplevel',
    ], text=True).strip()
    subprocess.run(['git', '-C', git_root, 'cat-file', '-e', base + '^{commit}'], check=True)
    with tempfile.TemporaryDirectory(prefix='sellersho-background-legacy-') as tmp:
        temp = Path(tmp).resolve()
        archive, source = temp / 'source.tar', temp / 'source'
        subprocess.run([
            'git', '-C', git_root, '-c', 'core.autocrlf=false', 'archive',
            '--format=tar', '-o', str(archive), base,
        ], check=True)
        source.mkdir()
        with tarfile.open(archive) as tar:
            members = [entry for entry in tar.getmembers() if not (
                entry.name == 'docs/fa' or entry.name.startswith('docs/fa/') or
                entry.name == 'backend/internal/networkreward' or entry.name.startswith('backend/internal/networkreward/'))]
            tar.extractall(source, members=members, filter='data')
        assert not (source / 'docs/fa').exists()
        assert not (source / 'backend/internal/networkreward').exists()
        target = source / 'backend/migrations' / MIGRATION
        assert not target.exists(), 'previous source already contains migration007'
        shutil.copyfile(ROOT / 'backend/migrations' / MIGRATION, target)
        starvation = args.starvation_red or args.starvation_current
        if args.starvation_current:
            overlay_current_allowed_backend(git_root, source)
            print('Current allowed backend source overlaid on previous archive; protected sources excluded', flush=True)
        else:
            print('Previous source ' + base + '; only additive migration007 added', flush=True)
        if starvation:
            fixture = ROOT / STARVATION_FIXTURE
            shutil.copyfile(fixture, source / 'backend/internal/worker/zz_job01_starvation_control_test.go')
            print('Shared starvation fixture SHA256=' + hashlib.sha256(fixture.read_bytes()).hexdigest(), flush=True)
        selected = (STARVATION_TEST,) if starvation else TESTS
        packages = ('./internal/worker',) if starvation else ('./internal/worker', './internal/repository', './cmd/server')
        result = subprocess.run([
            sys.executable, str(source / 'scripts/test-suite-isolated.py'),
            '--run', '^(' + '|'.join(selected) + ')$', *packages,
        ], cwd=source / 'backend', text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        print(result.stdout, end='', flush=True)
        summary = re.search(r'ISOLATED RESULT tests=(\d+) failures=(\d+)', result.stdout)
        assert result.stdout.count('Verified disposable container cleanup:') == 2, 'owned cleanup not verified'
        if args.starvation_red:
            assert result.returncode != 0, 'previous serial scheduler unexpectedly passed the starvation assertion'
            assert summary and (int(summary[1]), int(summary[2])) == (1, 1), 'expected one actual test failure, not compilation/tool failure'
            assert '--- FAIL: ' + STARVATION_TEST in result.stdout, 'named runtime test did not fail'
            assert 'JOB01_DELIVERY_STAGE_BARRIER_CONFIRMED' in result.stdout, 'actual delivery SQL barrier was never observed'
            assert 'JOB01_EXPIRY_STARVED_BEHIND_DELIVERY:' in result.stdout, 'failure was not the intended expiry starvation assertion'
            print('EXPECTED RED VERIFIED: actual previous serial scheduler starved expiry behind delivery SQL', flush=True)
            return 0
        if result.returncode:
            return result.returncode
        assert summary and (int(summary[1]), int(summary[2])) == (len(selected), 0), 'incomplete compatibility coverage'
        if args.starvation_current:
            assert 'JOB01_DELIVERY_STAGE_BARRIER_CONFIRMED' in result.stdout, 'actual delivery SQL barrier was never observed'
            assert 'JOB01_EXPIRY_ADVANCED_WHILE_DELIVERY_BLOCKED' in result.stdout, 'expiry did not progress with delivery still blocked'
            print('CURRENT GREEN VERIFIED: identical fixture advances expiry while delivery SQL remains blocked', flush=True)
    if args.starvation_current:
        print('Current starvation control passed; temporary allowed source removed', flush=True)
    else:
        print('Previous migration runner/workers/admin preserve business behavior on007; temporary source removed', flush=True)
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
