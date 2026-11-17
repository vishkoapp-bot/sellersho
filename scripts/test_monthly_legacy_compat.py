#!/usr/bin/env python3
"""Rehearse actual previous monthly writers on the additive current schema.

Only the existing marker-guarded disposable runner can start database/cache
targets. No application environment or production connection is consumed.
"""
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
from pathlib import Path

ROOT=Path(__file__).resolve().parent.parent

def main():
    if len(sys.argv)!=2 or not re.fullmatch(r'[0-9a-f]{40}',sys.argv[1]):
        raise SystemExit('pass the full previous committed source SHA')
    base=sys.argv[1]
    subprocess.run(['git','-C',str(ROOT),'cat-file','-e',base+'^{commit}'],check=True)
    migration='202611150006_monthly_settlement_progress.up.sql'
    with tempfile.TemporaryDirectory(prefix='sellersho-monthly-legacy-') as tmp:
        temp=Path(tmp).resolve();archive=temp/'source.tar';source=temp/'source'
        subprocess.run(['git','-C',str(ROOT),'-c','core.autocrlf=false','archive','--format=tar','-o',str(archive),base],check=True)
        source.mkdir()
        with tarfile.open(archive) as tar:tar.extractall(source,filter='data')
        assert not (source/'docs/fa').exists() and not (source/'backend/internal/networkreward').exists()
        target=source/'backend/migrations'/migration
        assert not target.exists(),'previous code already contains the new migration'
        shutil.copyfile(ROOT/'backend/migrations'/migration,target)
        shutil.copyfile(ROOT/'scripts/fixtures/monthly-legacy/legacy_test.go',source/'backend/cmd/server/monthly_legacy_compat_test.go')
        print('Previous source '+base+'; only migration006 and the compatibility test fixture added',flush=True)
        result=subprocess.run([sys.executable,str(source/'scripts/test-suite-isolated.py'),'--run','^TestFIN002LegacyWritersPreserveIncompleteCycles$','./cmd/server'],cwd=source/'backend')
        if result.returncode:return result.returncode
    print('Previous migration runner/manual/scheduled writers preserve incomplete progress and money; owned temporary source removed',flush=True)
    return 0

if __name__=='__main__':raise SystemExit(main())
