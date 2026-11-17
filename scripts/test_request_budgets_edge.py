#!/usr/bin/env python3
"""Exercise the actual edge configs on an owned, Internet-isolated Docker network."""
import json,os,re,secrets,subprocess,tempfile,time
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
TOKEN=secrets.token_hex(8);BASE='sellersho-ops02-'+TOKEN;LABEL='io.sellersho.ops02-test';NETWORK=BASE+'-net'
containers=[];network_created=False
NGINX=next((image for image in ['nginx:alpine','drup-frontend:latest'] if subprocess.run(['docker','image','inspect','--format','{{.Id}}',image],capture_output=True).returncode==0),'nginx:alpine')

def run(*args,check=True,env=None):
 result=subprocess.run(list(args),capture_output=True,text=True,encoding="utf-8",errors="replace",env=env)
 if check and result.returncode:raise RuntimeError('command failed: '+args[0]+' '+result.stderr[-2500:])
 return result

def docker(*args,**kw):return run('docker',*args,**kw)
def labelled(name,network=False):
 result=docker('network' if network else 'container','inspect','--format','{{index '+('.Labels' if network else '.Config.Labels')+' "'+LABEL+'"}}',name,check=False)
 return None if result.returncode else result.stdout.strip()==TOKEN

def request(edge,path,method='GET',delay=0):
 assert labelled(edge),'only owned fixture containers may execute probes'
 args=['exec',edge,'wget','-S','-T','21','-O','-','--header','Host: localhost','--header','X-Synthetic-Delay-Ms: '+str(delay)]
 if method=='POST':args+=['--post-data','synthetic-upload-body']
 started=time.monotonic();result=docker(*args,'http://127.0.0.1'+path,check=False)
 statuses=re.findall(r'HTTP/[0-9.]+ ([0-9]{3})',result.stderr)
 return int(statuses[-1]) if statuses else 0,result.stdout.encode(),time.monotonic()-started

def main():
 global network_created
 for image in [NGINX,'caddy:2-alpine','alpine:latest']:docker('image','inspect',image,'--format','{{.Id}}')
 # Parse/provision with no network at all before running the isolated chain.
 docker('run','--rm','--pull=never','--network','none','--add-host','backend:127.0.0.1','--mount',f'type=bind,src={ROOT / "nginx.conf"},dst=/etc/nginx/conf.d/default.conf,readonly',NGINX,'nginx','-t')
 adapted=docker('run','--rm','--pull=never','--network','none','--add-host','backend:127.0.0.1','--add-host','frontend:127.0.0.1','-e','PLATFORM_HTTP_HOST=localhost','-e','ACME_EMAIL=ops02@example.invalid','--mount',f'type=bind,src={ROOT / "Caddyfile"},dst=/etc/caddy/Caddyfile,readonly','caddy:2-alpine','caddy','adapt','--config','/etc/caddy/Caddyfile','--adapter','caddyfile','--validate')
 json.loads(adapted.stdout);print('nginx and Caddy validated without network',flush=True)
 with tempfile.TemporaryDirectory(prefix='sellersho-ops02-edge-') as temp:
  directory=Path(temp).resolve();assert directory.is_relative_to(Path(tempfile.gettempdir()).resolve())
  binary=directory/'upstream';env=dict(os.environ,GOOS='linux',GOARCH='amd64',CGO_ENABLED='0')
  run('go','build','-trimpath','-o',str(binary),str(ROOT/'scripts/fixtures/requestbudget-edge/backend.go'),env=env)
  docker('network','create','--internal','--label',LABEL+'='+TOKEN,NETWORK);network_created=True
  try:
   def start(role,image,*args):
    name=BASE+'-'+role
    docker('run','-d','--rm','--pull=never','--name',name,'--label',LABEL+'='+TOKEN,'--network',NETWORK,'--network-alias',role,*args,image)
    containers.append(name);return name
   start('backend','alpine:latest','--entrypoint','/test/upstream','--mount',f'type=bind,src={binary},dst=/test/upstream,readonly')
   start('frontend',NGINX,'--mount',f'type=bind,src={ROOT / "nginx.conf"},dst=/etc/nginx/conf.d/default.conf,readonly')
   edge=start('edge','caddy:2-alpine','-e','PLATFORM_HTTP_HOST=localhost','-e','ACME_EMAIL=ops02@example.invalid','--mount',f'type=bind,src={ROOT / "Caddyfile"},dst=/etc/caddy/Caddyfile,readonly')
   for attempt in range(30):
    try:
     if request(edge,'/api/v1/budget-read')[0]==200:break
    except OSError:pass
    time.sleep(.25)
   else:raise RuntimeError('isolated edge did not become ready')
   internal_status=request(edge,'/internal/domains/allow')[0]
   print(json.dumps({'internal_public_status':internal_status}),flush=True)
   assert internal_status==404,'internal routes must stay private before proxying'
   for method,path,want in [('GET','/api/v1/budget-read',504),('POST','/api/v1/upload/product-image',200),('GET','/api/v1/upload/product-image',504)]:
    status,body,elapsed=request(edge,path,method,14500)
    print(json.dumps({'method':method,'path':path,'status':status,'seconds':round(elapsed,2)}),flush=True)
    assert status==want,(method,path,status,want)
    assert elapsed<19,'proxy escaped its enclosing budget'
    if want==200:
     payload=json.loads(body);assert payload['method']=='POST' and payload['receivedBody']=='synthetic-upload-body','named upload route changed method/body'
  finally:cleanup()
 print('owned isolated edge acceptance passed; Internet never enabled',flush=True)

def cleanup():
 for name in reversed(containers):
  own=labelled(name)
  if own is False:raise RuntimeError('refusing to remove unowned test container')
  if own:docker('rm','-f',name)
 containers.clear()
 if network_created:
  own=labelled(NETWORK,True)
  if own is False:raise RuntimeError('refusing to remove unowned test network')
  if own:docker('network','rm',NETWORK)

if __name__=='__main__':
 try:main()
 finally:cleanup()
