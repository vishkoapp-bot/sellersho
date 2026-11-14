"""Private source snapshot/build for the isolated capacity stack; no env files."""
import hashlib
import os
from pathlib import Path
import shutil
import subprocess

def snapshot_frontend(source, target):
    source=Path(source);target=Path(target);target.mkdir(parents=True,exist_ok=False)
    for name in ('index.html','package.json','package-lock.json','tsconfig.json','vite.config.ts','nginx.conf'):
        shutil.copy2(source/name,target/name)
    for name in ('src','public'):
        shutil.copytree(source/name,target/name,ignore=shutil.ignore_patterns('.env','.env.*','node_modules','.git'))
    return hashlib.sha256(b''.join(p.relative_to(target).as_posix().encode()+p.read_bytes() for p in sorted(target.rglob('*')) if p.is_file())).hexdigest()

def build_frontend(source, snapshot, command):
    """Use installed dependencies without modifying shared source or build output."""
    modules=(Path(source)/'node_modules').resolve();link=Path(snapshot)/'node_modules'
    if not modules.is_dir() or link.exists():raise RuntimeError('private frontend dependency link precondition failed')
    if os.name=='nt':
        result=subprocess.run(['cmd','/c','mklink','/J',str(link),str(modules)],capture_output=True,text=True)
        if result.returncode:raise RuntimeError('private frontend dependency junction failed')
    else:link.symlink_to(modules,target_is_directory=True)
    try:
        if link.resolve()!=modules:raise RuntimeError('private dependency target mismatch')
        command(['node',str(modules/'typescript/bin/tsc'),'--noEmit'],cwd=snapshot)
        command(['node',str(modules/'vite/bin/vite.js'),'build'],cwd=snapshot)
    finally:
        # Remove only this exact link, never traverse/delete shared node_modules.
        if link.resolve()!=modules:raise RuntimeError('dependency cleanup target mismatch')
        if os.name=='nt':os.rmdir(link)
        else:link.unlink()
    output=Path(snapshot)/'dist'
    if not (output/'index.html').is_file():raise RuntimeError('frontend build output missing')
    return hashlib.sha256(b''.join(p.relative_to(output).as_posix().encode()+p.read_bytes() for p in sorted(output.rglob('*')) if p.is_file())).hexdigest()
