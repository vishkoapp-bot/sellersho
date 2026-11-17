"""Inventory source exposure without printing credential or command values.
Only file/line/key/commit/classification/SHA256 fingerprint leave memory.
Runtime environments and private credential files are never read.
"""
import argparse
import hashlib
import json
import re
import subprocess
from pathlib import Path
KEYS={'POSTGRES_PASSWORD','DATABASE_URL','REDIS_URL','JWT_SECRET','TRACKING_WEBHOOK_SECRET','TAPIN_TOKEN_KEY','REDIS_PASSWORD','IPPANEL_API_KEY'}
def scan(text, file, revision):
 result=[];service=''
 for number,line in enumerate(text.splitlines(),1):
  service_match=re.match(r'^  ([a-z][a-z0-9_-]*):\s*$',line)
  if service_match:service=service_match[1]
  match=re.match(r'^\s*([A-Z_]+):\s*(.*)$',line)
  key,value=(match.group(1),match.group(2).strip().strip(chr(34)+chr(39))) if match else ('','')
  if key not in KEYS:
   if service=='redis' and re.match(r'^\s*(command|test):',line):key='redis.'+line.strip().split(':',1)[0];value=line.split(':',1)[1].strip()
   else:continue
  classification='required_runtime_injection' if '${' in value and ':?' in value else 'runtime_reference' if '$$' in value else 'source_literal_usage_unverified'
  result.append({'file':file,'line':number,'key':key,'revision':revision,'classification':classification,'sha256':hashlib.sha256(value.encode()).hexdigest()})
 return result
def main():
 parser=argparse.ArgumentParser();parser.add_argument('--output',required=True);args=parser.parse_args();file='docker-compose.prod.yml';rows=scan(Path(file).read_text(),file,'working-tree')
 revisions=subprocess.check_output(['git','rev-list','--all','--',file],text=True).splitlines()
 for revision in revisions:
  p=subprocess.run(['git','show',revision+':'+file],capture_output=True,text=True)
  if p.returncode==0:rows+=scan(p.stdout,file,revision)
 Path(args.output).write_text(json.dumps({'scope':'tracked production Compose current and reachable Git history; no runtime inspection','active_usage':'unknown; requires restricted operator fingerprint comparison','records':rows},indent=2)+'\n')
 print('Credential inventory written; '+str(len(rows))+' metadata-only records; no values emitted.')
if __name__=='__main__':main()
