import re
import os
import secrets
import subprocess
import tempfile
import ast
import time
import uuid
import unittest
from pathlib import Path
from credential_inventory import scan
class ProductionSecrets(unittest.TestCase):
 def test_inventory_never_contains_source_values(self):
  material=secrets.token_urlsafe(36)
  rows=scan('services:\n  backend:\n    environment:\n      JWT_SECRET: "'+material+'"\n','fixture.yml','fixture')
  self.assertEqual(len(rows),1)
  self.assertNotIn(material,str(rows))
  self.assertEqual(rows[0]['classification'],'source_literal_usage_unverified')
  self.assertEqual(len(rows[0]['sha256']),64)
 def test_prod_uses_required_runtime_injection(self):
  text=Path('docker-compose.prod.yml').read_text()
  for key in ['POSTGRES_PASSWORD','REDIS_PASSWORD','DATABASE_URL','REDIS_URL','JWT_SECRET','TRACKING_WEBHOOK_SECRET','TAPIN_TOKEN_KEY']:
   value=next((line.split(':',1)[1].strip().strip('"') for line in text.splitlines() if line.strip().startswith(key+':')),None)
   self.assertTrue(value and re.fullmatch(r'\$\{'+key+r':\?[^}]+\}',value),key+' must require runtime injection')
 def test_current_production_inventory_has_no_literal_credentials(self):
  rows=scan(Path('docker-compose.prod.yml').read_text(),'docker-compose.prod.yml','working-tree')
  self.assertTrue(all(r['classification']!='source_literal_usage_unverified' for r in rows),'production credential expressions must use runtime injection')
 def test_redis_healthcheck_does_not_contain_password_argument(self):
  text=Path('docker-compose.prod.yml').read_text();self.assertFalse(bool(re.search(r'redis-cli[^\n]+["\s]-a["\s]',text)),'redis healthcheck must use auth environment')
 def test_redis_runtime_keeps_unprivileged_entrypoint(self):
  text=Path('docker-compose.prod.yml').read_text()
  block=text.split('  redis:',1)[1].split('  backend:',1)[0]
  command=ast.literal_eval(next(line.split(':',1)[1].strip() for line in block.splitlines() if line.strip().startswith('command:')))
  command=[part.replace('$$','$') for part in command]
  name='sellersho-sec-test-'+uuid.uuid4().hex
  result=subprocess.run(['docker','run','--pull','never','--detach','--rm','--name',name,'--label','sellersho.test-run=true','--network','none','-e','REDIS_PASSWORD='+secrets.token_urlsafe(36),'redis:7-alpine',*command],capture_output=True,text=True)
  self.assertEqual(result.returncode,0,'disposable Redis startup failed; output withheld')
  try:
   deadline=time.monotonic()+10
   while True:
    comm=subprocess.run(['docker','exec',name,'cat','/proc/1/comm'],capture_output=True,text=True)
    if comm.stdout.strip()=='redis-server':break
    self.assertLess(time.monotonic(),deadline,'Redis did not reach exec state')
    time.sleep(.05)
   uid=subprocess.run(['docker','exec',name,'sh','-c',"awk '/^Uid:/ {print $2}' /proc/1/status"],capture_output=True,text=True)
   self.assertEqual(uid.returncode,0,'Redis UID inspection failed')
   self.assertNotEqual(uid.stdout.strip(),'0','Redis server lost official entrypoint privilege drop')
  finally:
   label=subprocess.run(['docker','inspect','--format','{{ index .Config.Labels "sellersho.test-run" }}',name],capture_output=True,text=True)
   if label.returncode==0 and label.stdout.strip()=='true':subprocess.run(['docker','rm','--force',name],capture_output=True)
 def test_merged_compose_requires_and_uses_injection(self):
  keys=['POSTGRES_PASSWORD','REDIS_PASSWORD','DATABASE_URL','REDIS_URL','JWT_SECRET','TRACKING_WEBHOOK_SECRET','TAPIN_TOKEN_KEY']
  env=os.environ.copy()
  for key in keys:env.pop(key,None)
  with tempfile.NamedTemporaryFile(mode='w',suffix='.env',delete=False) as f:path=f.name
  try:
   command=['docker','compose','--env-file',path,'-f','docker-compose.yml','-f','docker-compose.prod.yml','config','--quiet']
   absent=subprocess.run(command,env=env,capture_output=True,text=True)
   self.assertNotEqual(absent.returncode,0,'missing production credentials accepted')
   for key in keys:env[key]=secrets.token_urlsafe(36)
   env['DATABASE_URL']='postgres://synthetic:'+env['POSTGRES_PASSWORD']+'@postgres:5432/fixture'
   env['REDIS_URL']='redis://:'+env['REDIS_PASSWORD']+'@redis:6379/0'
   result=subprocess.run(command,env=env,capture_output=True,text=True)
   self.assertEqual(result.returncode,0,'synthetic runtime injection failed (output intentionally withheld)')
  finally:os.unlink(path)
if __name__=='__main__':unittest.main()
