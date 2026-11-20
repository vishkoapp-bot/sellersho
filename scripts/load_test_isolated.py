#!/usr/bin/env python3
"""LOCAL ONLY disposable capacity preparation. Never accepts an existing target."""
import argparse
import json
import hashlib
import os
from pathlib import Path
import platform
import re
import secrets
import shutil
import subprocess
import tempfile
import threading
import time
from capacity_frontend import snapshot_frontend, build_frontend

ROOT = Path(__file__).resolve().parents[1]
LABEL = 'sellersho.capacity-run'
MIX = [30,20,15,15,8,4,4,4]
FLOOR = dict(products=10000, skus=50000, orders=100000, stores=100, suppliers=100, customers=1000)
class SafetyError(RuntimeError): pass

def verify_container_identity(info, cid, name, nonce, network):
    if (info.get('Id') != cid or info.get('Name') != '/' + name or
        info.get('Config', {}).get('Labels', {}).get(LABEL) != nonce or
        set(info.get('NetworkSettings', {}).get('Networks', {})) != {network}):
        raise SafetyError('Disposable identity/network mismatch; operation refused')

def verify_container(info, cid, name, nonce, network, network_id):
    verify_container_identity(info,cid,name,nonce,network)
    if not network_id or info['NetworkSettings']['Networks'][network].get('NetworkID') != network_id:
        raise SafetyError('Disposable attachment network ID mismatch; operation refused')

def verify_created_container(info, cid, name, nonce, network, network_id):
    # Docker fills attachment NetworkID only on start. Immutable NetworkMode pins
    # the fresh created container to the verified ID, never a replaceable name.
    verify_container_identity(info,cid,name,nonce,network)
    if (not network_id or info.get('State',{}).get('Status')!='created' or
        info.get('HostConfig',{}).get('NetworkMode')!=network_id or
        info['NetworkSettings']['Networks'][network].get('NetworkID') not in ('',network_id)):
        raise SafetyError('Fresh container network configuration mismatch; start refused')


def validate_mix(weights):
    if weights != MIX: raise SafetyError('Workload must preserve the recorded 100 API RPS contract')
    if sum(weights) != 100: raise SafetyError('Workload weights must sum to exactly 100')

def validate_workload_contract(workload):
    # workload.json fields the runner previously read but never gated on.
    # Any drift here is a contract change, not a quiet redefinition.
    expected = {'total_api_rps': 100, 'cpu_max_percent': 70,
                'unexpected_error_rate_lt': 0.001, 'headroom_rps': 200,
                'recovery_seconds': 120, 'sustained_seconds': 1800,
                'soak_seconds': 7200, 'dropped_iterations': 0}
    for key, want in expected.items():
        if workload.get(key) != want:
            raise SafetyError(f'Workload contract drift: {key}={workload.get(key)!r} want {want!r}')
    if workload.get('classes') != ['list', 'detail', 'store', 'account', 'quote', 'create', 'initiate', 'callback']:
        raise SafetyError('Workload contract drift: scenario classes changed')

def validate_fixture(f, seconds):
    users = f.get('users', [])
    if len(users) != 1000 or any(len({u.get(key) for u in users}) != 1000 or any(not u.get(key) for u in users) for key in ('id','token')):
        raise SafetyError('Exactly 1000 distinct principals AND sessions required')
    if any(f.get('counts',{}).get(k,0) < v for k,v in FLOOR.items()): raise SafetyError('Dataset below contract floor')
    if any(f.get('queues',{}).get(k,0) < 4*seconds for k in ('create','initiate','callback')): raise SafetyError('Lifecycle queue exhaustion predicted')
    if f.get('saleable_units',0) < 12*seconds: raise SafetyError('Insufficient soak stock')

def metrics_counts(raw):
    counts=dict(api=0,domain_allow=0,probe=0,asset=0,other=0)
    for line in raw.splitlines():
        if not line.startswith('sellersho_http_duration_seconds_count{'): continue
        labels=dict(re.findall(r'(\w+)="([^"\n]*)"',line))
        kind='probe' if labels.get('route')=='/api/health' else labels.get('class','other')
        counts[kind if kind in counts else 'other']+=int(line.rsplit(' ',1)[1])
    return counts

def verdict(evidence):
    return dict(certified=False, scope='local-preparation-only', evidence=evidence,
                open_gates=['CAP-02 intended-hardware certification', 'complete lifecycle and conservation evidence',
                            'real-browser/expiry/failure profiles', 'OBS-01 full acceptance'])

class Run:
    def __init__(self, out, browser=False):
        self.browser_enabled=browser;self.browser_image=None;self.browser_image_id=None
        self.nonce=secrets.token_hex(12); self.network='sellersho-cap-'+self.nonce
        self.network_id=None; self.containers=[]; self.password=secrets.token_hex(24)
        self.jwt=secrets.token_hex(32); self.smskey=secrets.token_hex(32); self.carrierkey=secrets.token_hex(32); self.database='sellersho_test_'+self.nonce
        self.out=out; self.temp_parent=Path(tempfile.gettempdir()).resolve(strict=True)
        self.work=Path(tempfile.mkdtemp(prefix='sellersho-cap-',dir=self.temp_parent))
        self.owned_work_path=self.work.resolve(strict=True)
        owned_stat=self.work.lstat();self.owned_work_identity=(owned_stat.st_dev,owned_stat.st_ino)
        self.source=self.work/'source'
        self.tests=self.work/'tests';shutil.copytree(ROOT/'load-tests',self.tests)
        self.frontend_source=self.work/'frontend'
        frontend_digest=snapshot_frontend(ROOT,self.frontend_source)
        for path in sorted((ROOT/'backend').rglob('*')):
            if path.is_file() and (path.suffix in ('.go','.sql','.mod','.sum') or path.name=='historical-checksums.json') and 'networkreward' not in path.parts:
                target=self.source/path.relative_to(ROOT/'backend');target.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(path,target)
        self.manifest={'scope':'local-only','certified':False,'run':self.nonce,'host':platform.platform(),
                       'frontend_source_sha256':frontend_digest,
                       'load_scripts_sha256':hashlib.sha256(b''.join(p.relative_to(self.tests).as_posix().encode()+p.read_bytes() for p in sorted(self.tests.rglob('*')) if p.is_file())).hexdigest(),
                       'logical_cpus':os.cpu_count(),'cpu_description':platform.processor(),'disk_class':'unmeasured','shared_host_services':'not enumerated; developer host shared','source':self.command(['git','rev-parse','HEAD']).strip(),
                       'backend_source_sha256': hashlib.sha256(b''.join(p.relative_to(self.source).as_posix().encode()+p.read_bytes() for p in sorted(self.source.rglob('*')) if p.is_file())).hexdigest(),
                       'migration_digests':{p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted((self.source/'migrations').glob('*.sql'))},
                       'source_note':'Private copied source snapshot including uncommitted input; HEAD is reference only',
                       'backend_pool':{'max_open':25,'max_idle':10},'replicas':1,'images':{},'adapters':{'payment':'test-only bounded latency double;committed startup gate','sms':'private in-memory double,25ms success default','carrier':'private quote-only HTTP double;egress blocked'},
                       'fixture_validity':{'seeded':False,'cardinality':False,'canonical_stock_origins':False,'realistic_financial_history':False,'aged_fee_positions':False,'staff_roles':False},
                       'limitations':['shared developer host, not intended hardware','test APP_ENV required for mock provider',
                                       'health baseline is not commerce mix','browser diagnostic is not1000-user or checkout qualification']}
    def command(self,args,env=None,input=None,cwd=None,timeout=600):
        result=subprocess.run(args,env=env,input=input,cwd=cwd or ROOT,text=True,encoding='utf-8',errors='replace',capture_output=True,timeout=timeout)
        if result.returncode:
            # Never echo command, container logs or environments. Compiler diagnostics
            # contain source locations, not runtime config; redact ephemeral secrets.
            if args[0]=='go':
                detail=result.stderr.replace(self.password,'[redacted]').replace(self.jwt,'[redacted]')
                raise SafetyError('Go build/test failed: '+detail[:2000])
            raise SafetyError('Command failed: '+args[0]+' '+(args[1] if len(args)>1 else '')+' (exit '+str(result.returncode)+')')
        return result.stdout
    def docker(self,*args,**kw): return self.command(['docker',*map(str,args)],**kw)
    def verify_network(self):
        i=json.loads(self.docker('network','inspect',self.network_id))[0]
        if i['Id']!=self.network_id or i['Name']!=self.network or not i['Internal'] or i.get('Labels',{}).get(LABEL)!=self.nonce:
            raise SafetyError('Internal disposable network identity mismatch')
    def verify(self):
        self.verify_network()
        for cid,name in self.containers:
            verify_container(json.loads(self.docker('inspect',cid))[0],cid,name,self.nonce,self.network,self.network_id)
    def create(self,alias,image,args=(),env=None,mounts=(),extra=()):
        self.verify_network(); name=self.network+'-'+alias
        cmd=['create','--name',name,'--network',self.network_id,'--network-alias',alias,'--label',LABEL+'='+self.nonce,'--log-driver','local','--log-opt','max-size=10m','--log-opt','max-file=2']
        for key in (env or {}): cmd+=['--env',key]
        for source,target in mounts: cmd+=['--mount',f'type=bind,source={source},destination={target},readonly']
        cmd+=list(extra)+[image]+list(args)
        cid=self.docker(*cmd,env=dict(os.environ,**(env or {}))).strip()
        self.containers.append((cid,name));self.verify_network()
        verify_created_container(json.loads(self.docker('inspect',cid))[0],cid,name,self.nonce,self.network,self.network_id)
        self.docker('start',cid);self.verify()
        self.manifest['images'][alias]=json.loads(self.docker('inspect',cid))[0]['Image']
        return cid
    def sql(self,sql):
        self.verify()
        return self.docker('exec','-i',self.pg,'psql','-X','-v','ON_ERROR_STOP=1','-U','postgres','-d',self.database,'-At',input=sql).strip()
    def start(self):
        # No inherited app env is passed to containers. Images are provisioned before isolation.
        for image in ('postgres:16-alpine','redis:7-alpine','alpine:latest','drup-frontend:latest','caddy:2-alpine','node:24-bookworm-slim'):
            self.docker('image','inspect',image) if subprocess.run(['docker','image','inspect',image],capture_output=True).returncode==0 else self.docker('pull',image)
        if self.browser_enabled:
            self.browser_image='sellersho-cap-browser:'+self.nonce
            # Public package/build diagnostics only; no runtime credentials enter
            # this build. Drain continuously to a file so setup is diagnosable.
            with (self.out/'browser-build.log').open('w') as build_log:
                build=subprocess.run(['docker','build','--progress=plain','--pull=false','--label',LABEL+'='+self.nonce,'--tag',self.browser_image,'--file',str(self.tests/'Dockerfile.browser'),str(self.tests)],stdout=build_log,stderr=subprocess.STDOUT,text=True,timeout=300)
            if build.returncode: raise SafetyError('Private Chromium image preparation failed;see public package build log')
            built=json.loads(self.docker('image','inspect',self.browser_image))[0]
            if built.get('Config',{}).get('Labels',{}).get(LABEL)!=self.nonce: raise SafetyError('Browser image marker mismatch')
            self.browser_image_id=built['Id']
            self.manifest['browser_image_preparation']='Chromium/dependencies installed before isolated load;unique labeled image;package downloads are not provider traffic'
        self.network_id=self.docker('network','create','--internal','--label',LABEL+'='+self.nonce,self.network).strip()
        self.pg=self.create('db','postgres:16-alpine',env={'POSTGRES_PASSWORD':self.password,'POSTGRES_DB':self.database},extra=['--tmpfs','/var/lib/postgresql/data'])
        for _ in range(120):
            if subprocess.run(['docker','exec',self.pg,'pg_isready','-h','127.0.0.1','-U','postgres'],capture_output=True).returncode==0: break
            time.sleep(.5)
        else: raise SafetyError('PostgreSQL startup timeout')
        self.sql("CREATE ROLE cap_runner LOGIN PASSWORD '"+self.password+"' NOSUPERUSER NOCREATEDB NOCREATEROLE; ALTER DATABASE "+self.database+" OWNER TO cap_runner; GRANT ALL ON SCHEMA public TO cap_runner; SET ROLE cap_runner; CREATE TABLE sellersho_test_guard(singleton boolean PRIMARY KEY CHECK(singleton),database_name text NOT NULL); INSERT INTO sellersho_test_guard VALUES(TRUE,'"+self.database+"');")
        self.create('cache','redis:7-alpine',['redis-server','--save','','--appendonly','no','--requirepass',self.password],extra=['--tmpfs','/data'])
        env={k:v for k,v in os.environ.items() if k in ('PATH','SystemRoot','SYSTEMROOT','TEMP','TMP','USERPROFILE','LOCALAPPDATA','APPDATA','HOME','GOPATH','GOCACHE')}
        env.update(GOOS='linux',GOARCH='amd64',CGO_ENABLED='0')
        self.command(['go','build','-o',str(self.work/'server'),'./cmd/server'],env=env,cwd=self.source)
        self.manifest['go_toolchain']=self.command(['go','version']).strip()
        self.manifest['backend_binary_sha256']=hashlib.sha256((self.work/'server').read_bytes()).hexdigest()
        self.appenv={'APP_ENV':'test','DATABASE_URL':f'postgres://cap_runner:{self.password}@db:5432/{self.database}?sslmode=disable',
                     'SELLERSHO_TEST_DATABASE_URL':f'postgres://cap_runner:{self.password}@db:5432/{self.database}?sslmode=disable',
                     'REDIS_URL':f'redis://:{self.password}@cache:6379/0','JWT_SECRET':self.jwt,
                     'TRACKING_WEBHOOK_SECRET':secrets.token_hex(32),'PAYMENT_GATEWAY_MODE':'mock','IPPANEL_ENABLED':'false',
                     'ENABLE_NETWORK_VOLUME_INCENTIVE':'false','PLATFORM_DOMAINS':'capacity.example.test','PORT':'8080','MIGRATIONS_DIR':'/migrations'}
        self.sms=self.create('sms','node:24-bookworm-slim',['node','/tests/sms-double.mjs'],env={'SELLERSHO_CAPACITY_RUN':self.nonce,'CAPACITY_SMS_KEY':self.smskey},mounts=[(self.tests,'/tests')])
        self.carrier=self.create('carrier','node:24-bookworm-slim',['node','/tests/carrier-double.mjs'],env={'SELLERSHO_CAPACITY_RUN':self.nonce,'CAPACITY_CARRIER_KEY':self.carrierkey},mounts=[(self.tests,'/tests')])
        self.appenv.update(SELLERSHO_CAPACITY_RUN=self.nonce,CAPACITY_PAYMENT_MODE='success',CAPACITY_PAYMENT_LATENCIES_MS='25,25,50,50,150',CAPACITY_CARRIER_BASE_URL='http://carrier:8082',CAPACITY_CARRIER_KEY=self.carrierkey)
        self.manifest['adapters']={'payment':'private test-only in-process verification;deterministic samples25,25,50,50,150ms','sms':'private synthetic OTP double25ms','carrier':'private Tapin quote-only double50ms,100000Rial freight;registration/dispatch remains OPEN'}
        self.appenv.update(IPPANEL_ENABLED='true',IPPANEL_BASE_URL='http://sms:8081/v1',IPPANEL_API_KEY=self.smskey,IPPANEL_SENDER='synthetic',IPPANEL_PATTERN_CODE='synthetic')
        self.backend=self.create('backend','alpine:latest',['/server'],env=self.appenv,mounts=[(self.work/'server','/server'),(self.source/'migrations','/migrations')])
        self.manifest['frontend_build_sha256']=build_frontend(ROOT,self.frontend_source,self.command)
        self.manifest['nginx_config_sha256']=hashlib.sha256((self.frontend_source/'nginx.conf').read_bytes()).hexdigest()
        self.frontend=self.create('frontend','drup-frontend:latest',mounts=[(self.frontend_source/'nginx.conf','/etc/nginx/conf.d/default.conf'),(self.frontend_source/'dist','/usr/share/nginx/html')])
        (self.work/'Caddyfile').write_text('''{\n admin off\n auto_https disable_redirects\n}\nhttps://capacity.example.test {\n tls internal\n @internal path /internal/*\n respond @internal 404\n forward_auth backend:8080 {\n  uri /internal/domains/allow?domain={host}\n }\n reverse_proxy frontend:80\n}\n''')
        self.tls=self.create('tls','caddy:2-alpine',mounts=[(self.work/'Caddyfile','/etc/caddy/Caddyfile')],extra=['--network-alias','capacity.example.test','--tmpfs','/data','--tmpfs','/config'])
        # No public ports: generator resides on the same isolated network, separate container.
        for _ in range(180):
            result=subprocess.run(['docker','exec',self.backend,'wget','-q','-O','-','http://127.0.0.1:8080/api/health'],capture_output=True)
            if result.returncode==0: break
            time.sleep(.5)
        else: raise SafetyError('Backend health startup failed; application remains failed')
        self.verify()
        for _ in range(60):
            probe=subprocess.run(['docker','exec',self.tls,'test','-f','/data/caddy/pki/authorities/local/root.crt'],capture_output=True)
            if probe.returncode==0: break
            time.sleep(.5)
        else: raise SafetyError('Ephemeral Caddy CA startup failed')
        (self.work/'root.crt').write_text(self.docker('exec',self.tls,'cat','/data/caddy/pki/authorities/local/root.crt'))
        self.manifest['tls']='ephemeral CA trusted explicitly; hostname verification enabled; private key never exported'
        self.manifest['postgres_settings']=self.sql("SELECT json_build_object('max_connections',current_setting('max_connections'),'shared_buffers',current_setting('shared_buffers'),'version',version());")
        self.manifest['docker_resources']=json.loads(self.docker('info','--format','{{json .}}'))
        # Whitelist safe resource fields; never persist full Docker info/config/env.
        self.manifest['docker_resources']={k:self.manifest['docker_resources'].get(k) for k in ('NCPU','MemTotal','KernelVersion','OperatingSystem','ServerVersion','Driver')}
        self.manifest['service_versions']={}
        for alias,argv in [('db',['postgres','--version']),('cache',['redis-server','--version']),('frontend',['nginx','-v']),('tls',['caddy','version'])]:
            result=subprocess.run(['docker','exec',self.network+'-'+alias,*argv],text=True,capture_output=True)
            if result.returncode: raise SafetyError('Service version collection failed')
            self.manifest['service_versions'][alias]=(result.stdout+result.stderr).strip()
    def fixture(self):
        self.verify()
        # Migration/setup smoke completed already. Pause only this marked backend
        # while fixture transactions and real release assertions run, so worker
        # races cannot consume the cohort before its assertions. Resume production
        # worker scheduling before any measured load.
        self.docker('stop',self.backend)
        env={k:v for k,v in os.environ.items() if k in ('PATH','SystemRoot','SYSTEMROOT','TEMP','TMP','USERPROFILE','LOCALAPPDATA','APPDATA','HOME','GOPATH','GOCACHE')}
        env.update(GOOS='linux',GOARCH='amd64',CGO_ENABLED='0')
        self.command(['go','test','-c','-o',str(self.work/'fixture'),'./cmd/capacity-fixture'],env=env,cwd=self.source)
        cid=self.create('fixture','alpine:latest',['/fixture','-test.v','-test.run=^TestCapacityDataFloor$','-test.timeout=15m'],env=dict(self.appenv,CAPACITY_SESSION_FILE='/tmp/users.json',CAPACITY_FINANCE_HISTORY='1'),mounts=[(self.work/'fixture','/fixture')])
        status=int(self.docker('wait',cid,timeout=960).strip())
        # Whitelist metric records, never persist arbitrary container logs/env.
        fixture_log=self.docker('logs',cid)
        self.manifest['fixture_read_samples']=[{'sample':int(a),'duration_ms':int(b)} for a,b in re.findall(r'CAPACITY_METRIC supplier_balance sample=(\d+) duration_ms=(\d+)',fixture_log)]
        self.manifest['fixture_exit']=status
        safe_log=fixture_log
        for private in (self.password,self.jwt,self.smskey,self.carrierkey):safe_log=safe_log.replace(private,'[redacted]')
        safe_log=re.sub(r'eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+','[redacted-jwt]',safe_log)
        (self.out/'fixture-redacted.log').write_text(safe_log)
        if status: raise SafetyError('Guarded capacity fixture failed')
        # Real release cohort: its own gate, recorded and never blocking.
        # The app's 300ms per-candidate release budget fails on a small
        # shared VM (seq-scan-heavy eligibility chain — see
        # backend/cmd/capacity-fixture/release_cohort_test.go); that is
        # an app capacity finding, so the cohort runs separately and the
        # suite continues to the driver profiles either way.
        rc_cid=self.create('fixture-release','alpine:latest',['/fixture','-test.v','-test.run=^TestCapacityReleaseCohort$','-test.timeout=15m'],env=dict(self.appenv,CAPACITY_SESSION_FILE='/tmp/users.json',CAPACITY_FINANCE_HISTORY='1'),mounts=[(self.work/'fixture','/fixture')])
        rc_status=int(self.docker('wait',rc_cid,timeout=960).strip())
        rc_log=self.docker('logs',rc_cid)
        rc_m=re.search(r'CAPACITY_RELEASE_COHORT released=(\d+) want=100 status=(\w+)',rc_log)
        self.manifest['release_cohort']={'exit':rc_status,'released':int(rc_m.group(1)) if rc_m else None,'status':rc_m.group(2) if rc_m else 'UNKNOWN'}
        rc_safe=rc_log
        for private in (self.password,self.jwt,self.smskey,self.carrierkey):rc_safe=rc_safe.replace(private,'[redacted]')
        rc_safe=re.sub(r'eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+','[redacted-jwt]',rc_safe)
        (self.out/'release-cohort-redacted.log').write_text(rc_safe)
        print('release cohort:',self.manifest['release_cohort'],flush=True)
        self.verify();self.docker('cp',cid+':/tmp/users.json',self.work/'users.json')
        os.chmod(self.work/'users.json',0o600)
        self.manifest['fixture']='synthetic datafloor/immutable origins/paid-delivered history;real release cohort recorded separately (see release_cohort);100idempotent payout reservations;100aged positions;fee variant restoredOFF;1000distinct signed sessions'
        self.manifest['fixture_validity'].update(seeded=True,cardinality=True,canonical_stock_origins=True,realistic_financial_history=True,aged_fee_positions=True,staff_roles=True)
        self.manifest['fixture_scope']='synthetic history validity only;current/prior-month posted/reserved reconciliation;not production historical evidence or full return/refund oracle'
        self.verify();self.docker('start',self.backend)
        for _ in range(120):
            if subprocess.run(['docker','exec',self.backend,'wget','-q','-O','-','http://127.0.0.1:8080/api/health'],capture_output=True).returncode==0: break
            time.sleep(.5)
        else: raise SafetyError('Backend failed to resume after guarded fixture provisioning')
        self.manifest['worker_schedule']='normal backend workers resumed before measurement;paused only during fixture provisioning/assertions'
    def browser_assets(self, relay=False):
        self.verify()
        if not relay and (not self.browser_enabled or not self.browser_image_id): raise SafetyError('Private browser image not prepared')
        target={'nonce':self.nonce,'base':'https://capacity.example.test'}
        (self.work/'browser-target.json').write_text(json.dumps(target))
        before=self.docker('exec',self.backend,'wget','-q','-O','-','http://127.0.0.1:9090/metrics')
        def nginx_counts():
            counts={}
            for line in self.docker('logs',self.frontend).splitlines():
                if not line.startswith('{"class":'): continue
                row=json.loads(line);kind=row['class'];counts[kind]=counts.get(kind,0)+1
            return counts
        nginx_before=nginx_counts()
        if relay:
            from capacity_browser_relay import BrowserRelay
            relay_cid=self.create('browser-relay','node:24-bookworm-slim',['sleep','300'],extra=['--cap-drop=ALL','--security-opt=no-new-privileges'])
            tls_cid=next(cid for cid,name in self.containers if name==self.network+'-tls')
            capability=dict(nonce=self.nonce,network=self.network,network_id=self.network_id,tls_cid=tls_cid,relay_cid=relay_cid)
            bridge=BrowserRelay(capability,self.verify)
            target.update(port=bridge.port,listen='127.0.0.1',base='https://capacity.example.test:'+str(bridge.port))
            (self.work/'browser-target.json').write_text(json.dumps(target))
            env={k:v for k,v in os.environ.items() if k in ('PATH','SystemRoot','SYSTEMROOT','TEMP','TMP','USERPROFILE','LOCALAPPDATA','APPDATA','HOME')}
            env.update(SELLERSHO_CAPACITY_RUN=self.nonce,CAPACITY_BROWSER_RELAY='1')
            try:
                result=subprocess.run(['node',str(self.tests/'browser-assets.mjs'),str(self.work/'browser-target.json'),str(self.work/'root.crt'),str(ROOT),str(self.out/'browser-summary.json')],env=env,capture_output=True,timeout=210)
                status=result.returncode
                if status and not (self.out/'browser-summary.json').exists():raise SafetyError('Host browser setup failed before report')
            finally:
                bridge.close()
                self.manifest['browser_relay_counts']=bridge.counts
                self.manifest['browser_relay_scope']='hard-bound rawTLS Docker exec to this marked run tls:443;127.0.0.1 ephemeral;32connections/120s/64MiB each;240s overall;UI/count only;timing excluded'
        else:
            browser=self.create('browser',self.browser_image,['node','/tests/browser-assets.mjs','/target.json','/trust/root.crt','/deps','/tmp/browser-summary.json'],env={'SELLERSHO_CAPACITY_RUN':self.nonce,'XDG_CONFIG_HOME':'/tmp/config','XDG_CACHE_HOME':'/tmp/cache'},mounts=[(self.tests,'/tests'),(self.work/'browser-target.json','/target.json'),(self.work/'root.crt','/trust/root.crt'),(ROOT/'node_modules','/deps/node_modules')],extra=['--cap-drop=ALL','--security-opt=no-new-privileges','--shm-size=256m'])
            samples=[];stop=threading.Event()
            def sample_browser():
                while not stop.is_set():
                    try:samples.append([json.loads(line) for line in self.docker('stats','--no-stream','--format','{{json .}}',browser,self.backend,self.pg,timeout=15).splitlines()])
                    except Exception:self.manifest['browser_sampling_error']=True;return
                    stop.wait(5)
            sampler=threading.Thread(target=sample_browser,daemon=True);sampler.start()
            try:status=int(self.docker('wait',browser,timeout=240).strip())
            finally:stop.set();sampler.join(timeout=20)
            if sampler.is_alive() or self.manifest.get('browser_sampling_error'):status=1
            self.manifest['browser_resource_samples']=samples
            self.docker('cp',browser+':/tmp/browser-summary.json',self.out/'browser-summary.json')
        self.verify();after=self.docker('exec',self.backend,'wget','-q','-O','-','http://127.0.0.1:9090/metrics');nginx_after=nginx_counts()
        (self.out/'browser-metrics-before.prom').write_text(before);(self.out/'browser-metrics-after.prom').write_text(after)
        a,b=metrics_counts(after),metrics_counts(before)
        self.manifest['browser_received_counts']={k:a[k]-b[k] for k in a}
        self.manifest['browser_nginx_counts']={k:nginx_after.get(k,0)-nginx_before.get(k,0) for k in set(nginx_before)|set(nginx_after)}
        summary=json.loads((self.out/'browser-summary.json').read_text())
        received=self.manifest['browser_received_counts'];nginx_total=sum(self.manifest['browser_nginx_counts'].values())
        client_api=sum(row['api'] for row in summary['counts'].values())
        if received['api']!=client_api or received['domain_allow']!=nginx_total or nginx_total!=summary['network_responses']:
            status=1;self.manifest['browser_counter_gate']='FAILED: browser/network/nginx/domain/API counts do not reconcile'
        else:self.manifest['browser_counter_gate']='matched'
        self.manifest['browser_scope']='10anonymous real-browser contexts,cold/warm built UI and assets;no credential injection;not1000-user or fullcheckout acceptance'
        self.manifest['browser_exit']=status
        return status
    def baseline(self, smoke=False, login=False, otp=False, background=False, commerce=False, rolling=0, journey=0, hot=False, staff=False):
        self.verify()
        run_started=self.sql("SELECT clock_timestamp()") if (commerce or rolling or journey or hot) else None
        before=self.docker('exec',self.backend,'wget','-q','-O','-','http://127.0.0.1:9090/metrics')
        prefix='setup' if smoke else 'baseline'
        (self.out/(prefix+'-metrics-before.prom')).write_text(before)
        cid=self.create('smoke' if smoke else 'generator','node:24-bookworm-slim',['node','/tests/hot-sku.mjs' if hot and not smoke else '/tests/users-1000.mjs' if journey and not smoke else '/tests/commerce-rolling.mjs' if rolling and not smoke else '/tests/commerce-baseline.mjs' if commerce and not smoke else '/tests/login-otp.mjs' if otp and not smoke else '/tests/baseline.mjs'],env={'CAPACITY_SMOKE':'1' if smoke else '0','CAPACITY_PROFILE':'login' if login else 'auth','CAPACITY_SMS_KEY':self.smskey,'CAPACITY_BACKGROUND':'1' if background else '0','CAPACITY_DURATION_SECONDS':str(journey or rolling or 30),'CAPACITY_STAFF':'1' if staff else '0'},mounts=[(self.tests,'/tests'),(self.work/'root.crt','/trust/root.crt')]+([(self.work/'users.json','/sessions/users.json')] if (self.work/'users.json').exists() else []))
        samples=[]; stop=threading.Event(); started=time.monotonic()
        def sample():
            while not stop.is_set():
                try:
                    stats=[json.loads(line) for line in self.docker('stats','--no-stream','--format','{{json .}}',cid,self.backend,self.pg,timeout=15).splitlines()]
                    metrics=self.docker('exec',self.backend,'wget','-q','-O','-','http://127.0.0.1:9090/metrics',timeout=10)
                    samples.append({'elapsed_seconds':time.monotonic()-started,'resources':stats})
                    (self.out/(prefix+'-metrics-sample-'+str(len(samples))+'.prom')).write_text(metrics)
                except Exception:
                    self.manifest['sampling_error']='resource/private-metrics sampling failed';return
                stop.wait(5)
        sampler=threading.Thread(target=sample,daemon=True);sampler.start()
        try: status=int(self.docker('wait',cid,timeout=max(600,rolling+1800,journey+1800)).strip())
        finally: stop.set();sampler.join(timeout=40)
        if sampler.is_alive() or self.manifest.get('sampling_error'): status=1
        self.manifest[prefix+'_resource_samples']=samples
        self.manifest['sampling_scope']='approximately every5seconds during short local run; not sustained/soak certification'
        self.manifest['log_plumbing']='detached Docker backend; local logging driver bounded10MiB*2; no native backend PIPE; finite CLI/build pipes drained by subprocess.run'

        self.verify()
        self.docker('cp',cid+':/tmp/summary.json',self.out/('setup-smoke.json' if smoke else 'baseline-summary.json'))
        after=self.docker('exec',self.backend,'wget','-q','-O','-','http://127.0.0.1:9090/metrics')
        (self.out/(prefix+'-metrics-after.prom')).write_text(after)
        a,b=metrics_counts(after),metrics_counts(before)
        self.manifest[prefix+'_received_counts']={key:a[key]-b[key] for key in a}
        self.manifest['count_scope']='Independent backend/private metrics deltas; nginx-only assets remain separate profile'
        summary=json.loads((self.out/('setup-smoke.json' if smoke else 'baseline-summary.json')).read_text())
        received=self.manifest[prefix+'_received_counts']
        expected=summary.get('requests',-1)+len(summary.get('helper_requests_raw',[]))
        route_class='probe' if smoke or summary.get('scope')=='health only' else 'api'
        if received[route_class]!=expected or received['domain_allow']!=expected:
            status=1;self.manifest[prefix+'_counter_gate']='FAILED: attempted/client requests do not reconcile to backend/domain counts'
        else:self.manifest[prefix+'_counter_gate']='matched'
        if hot and not smoke:
            oracle=json.loads(self.sql("SELECT json_build_object('source',5,'sellable',(SELECT COALESCE(sum(quantity),0) FROM inventory_balances WHERE product_id='cap-hot-product' AND status='sellable'),'captured',(SELECT COALESCE(sum(quantity-restored_quantity),0) FROM order_inventory_captures WHERE product_id='cap-hot-product'),'sku',(SELECT stock FROM product_variants WHERE product_id='cap-hot-product'),'paid_orders',(SELECT count(DISTINCT o.id) FROM orders o JOIN order_items oi ON oi.order_id=o.id WHERE oi.product_id='cap-hot-product' AND o.payment_status='paid'),'amount_mismatch',(SELECT count(*) FROM payment_transactions p JOIN orders o ON o.id=p.order_id JOIN order_items oi ON oi.order_id=o.id WHERE oi.product_id='cap-hot-product' AND p.amount<>o.payable_amount))"))
            self.manifest['hot_sku_oracle']=oracle
            if oracle!={'source':5,'sellable':0,'captured':5,'sku':0,'paid_orders':5,'amount_mismatch':0}: status=1;self.manifest['hot_sku_oracle_gate']='FAILED'
            else:self.manifest['hot_sku_oracle_gate']='matched:five conserved units and paid orders after duplicate callbacks'
        if (commerce or rolling or journey) and not smoke:
            physical=int(self.sql("SELECT COALESCE(sum(quantity),0) FROM inventory_balances WHERE product_id LIKE 'cap-product-%' AND status='sellable'"))
            projected=int(self.sql("SELECT COALESCE(sum(stock),0) FROM product_variants WHERE product_id LIKE 'cap-product-%'"))
            captured=int(self.sql("SELECT COALESCE(sum(quantity-restored_quantity),0) FROM order_inventory_captures WHERE product_id LIKE 'cap-product-%'"))
            mismatched=int(self.sql("SELECT count(*) FROM payment_transactions p JOIN orders o ON o.id=p.order_id JOIN users u ON u.id=o.customer_user_id WHERE u.full_name LIKE 'cap-customer-%' AND p.amount<>o.payable_amount"))
            receipts=int(self.sql("SELECT COALESCE(sum(quantity),0) FROM inventory_movements WHERE product_id LIKE 'cap-product-%' AND reference_type='capacity_synthetic_receipt'"))
            self.manifest['commerce_stock_amount_assertions']={'source_receipt_units':receipts,'physical_sellable':physical,'sku_projection':projected,'net_captured':captured,'payment_order_amount_mismatch':mismatched,'scope':'stock projections/captures and payment-order amounts only; ledger/refund oracle remains OPEN'}
            if physical+captured!=receipts or projected!=physical or mismatched: status=1;self.manifest['commerce_invariant_gate']='FAILED'
            else:self.manifest['commerce_invariant_gate']='matched within stated scope'
            # Extended oracle (CAP-01): price policy, ledger sanity and
            # refund obligations, scoped to orders created in THIS run
            # (created_at >= run start, cap-customer owners only).
            # Settled identities (order_handlers.go): payable =
            # SUM(order_items.final_unit_price*quantity) + shipping +
            # addons; discount = total - SUM(final*qty). FIN-003: every
            # paid-then-cancelled order has exactly one
            # order_refund_obligations row (purpose=cancellation).
            # Ledger: only checkable invariants are gated (no negative
            # amounts, no entries referencing unknown orders); full
            # conservation across deferred releases/settlements stays
            # OPEN (FIN-013) because release postings are deferred by
            # design and are not due inside the run window.
            oracle_sql = """
            SELECT json_build_object(
              'price_payable_mismatch',(SELECT count(*) FROM orders o WHERE o.created_at>='{ts}' AND o.customer_user_id IN (SELECT id FROM users WHERE full_name LIKE 'cap-customer-%') AND o.payable_amount <> (SELECT COALESCE(sum(final_unit_price*quantity),0) FROM order_items WHERE order_id=o.id) + o.shipping_cost + COALESCE(o.platform_addon_amount,0) + COALESCE(o.supplier_addon_amount,0)),
              'price_discount_mismatch',(SELECT count(*) FROM orders o WHERE o.created_at>='{ts}' AND o.customer_user_id IN (SELECT id FROM users WHERE full_name LIKE 'cap-customer-%') AND o.discount_amount <> o.total_amount - (SELECT COALESCE(sum(final_unit_price*quantity),0) FROM order_items WHERE order_id=o.id)),
              'ledger_negative_amount',(SELECT count(*) FROM ledger_entries WHERE date>='{ts}' AND amount<0),
              'ledger_orphan_order',(SELECT count(*) FROM ledger_entries l WHERE l.date>='{ts}' AND l.related_order_or_dispute IS NOT NULL AND NOT EXISTS (SELECT 1 FROM orders o WHERE o.id=l.related_order_or_dispute)),
              'paid_orders_without_ledger_entry',(SELECT count(*) FROM orders o WHERE o.created_at>='{ts}' AND o.customer_user_id IN (SELECT id FROM users WHERE full_name LIKE 'cap-customer-%') AND o.payment_status='paid' AND NOT EXISTS (SELECT 1 FROM ledger_entries l WHERE l.related_order_or_dispute=o.id)),
              'paid_cancelled_without_obligation',(SELECT count(*) FROM orders o WHERE o.created_at>='{ts}' AND o.customer_user_id IN (SELECT id FROM users WHERE full_name LIKE 'cap-customer-%') AND o.payment_status IN ('cancelled','refund_pending') AND NOT EXISTS (SELECT 1 FROM order_refund_obligations r WHERE r.order_id=o.id AND r.purpose='cancellation')),
              'duplicate_obligations',(SELECT count(*) FROM (SELECT purpose,business_id,source_payment_id FROM order_refund_obligations GROUP BY 1,2,3 HAVING count(*)>1) d),
              'obligation_over_source',(SELECT count(*) FROM order_refund_obligations WHERE created_at>='{ts}' AND amount_toman>source_amount_toman),
              'orders_with_multiple_refunds',(SELECT count(*) FROM (SELECT order_id FROM refunds GROUP BY order_id HAVING count(*)>1) d)
            )""".format(ts=run_started)
            oracle = json.loads(self.sql(oracle_sql))
            oracle['scope'] = ('price identity (payable/discount vs persisted item snapshots), refund-obligation existence/uniqueness/amount bound, '
              'ledger sanity (negative/orphan only); paid_orders_without_ledger_entry is INFORMATIONAL, not a gate: release postings are deferred by design; '
              'full ledger conservation across releases/settlements remains OPEN (FIN-013)')
            self.manifest['commerce_price_ledger_refund_oracle'] = oracle
            oracle_fail = (oracle['price_payable_mismatch'] or oracle['price_discount_mismatch'] or oracle['ledger_negative_amount']
              or oracle['ledger_orphan_order'] or oracle['paid_cancelled_without_obligation'] or oracle['duplicate_obligations']
              or oracle['obligation_over_source'] or oracle['orders_with_multiple_refunds'])
            if oracle_fail:
                status = 1
                self.manifest['oracle_gate'] = 'FAILED: price/ledger/refund oracle mismatch (see commerce_price_ledger_refund_oracle)'
            else:
                self.manifest['oracle_gate'] = 'matched within stated scope (ledger conservation across deferred releases still OPEN)'
        self.manifest['setup_smoke_exit' if smoke else 'baseline_exit']=status
        self.manifest['sms_double_counts']=json.loads(self.docker('exec',self.sms,'node','-e',"fetch('http://127.0.0.1:8081/metrics',{headers:{Authorization:process.env.CAPACITY_SMS_KEY}}).then(r=>r.text()).then(console.log)"))
        self.manifest['carrier_double_counts']=json.loads(self.docker('exec',self.carrier,'node','-e',"fetch('http://127.0.0.1:8082/metrics',{headers:{Authorization:'Bearer '+process.env.CAPACITY_CARRIER_KEY}}).then(r=>r.text()).then(console.log)"))
        return status
    def delete_owned_work(self):
        # Validate the destructive boundary, not merely the mkdtemp origin.
        try:
            resolved=self.work.resolve(strict=True);identity=self.work.lstat()
            if (self.work.absolute()!=self.owned_work_path or resolved!=self.owned_work_path or
                resolved.parent!=self.temp_parent or self.temp_parent.resolve(strict=True)!=self.temp_parent or
                self.work.is_symlink() or self.work.is_junction() or
                (identity.st_dev,identity.st_ino)!=self.owned_work_identity):
                raise SafetyError('Owned temporary directory identity/containment changed; deletion refused')
        except (OSError,AttributeError) as exc:
            raise SafetyError('Owned temporary directory cannot be verified; deletion refused') from exc
        shutil.rmtree(self.work)
    def close(self):
        failures=[]
        for cid,name in reversed(self.containers):
            try:
                self.verify_network()
                verify_container(json.loads(self.docker('inspect',cid))[0],cid,name,self.nonce,self.network,self.network_id)
                self.docker('rm','-f','-v',cid)
            except Exception: failures.append(name)
        if self.network_id:
            try: self.verify_network(); self.docker('network','rm',self.network_id)
            except Exception: failures.append(self.network)
        if self.browser_image_id:
            try:
                built=json.loads(self.docker('image','inspect',self.browser_image_id))[0]
                if built['Id']!=self.browser_image_id or built.get('Config',{}).get('Labels',{}).get(LABEL)!=self.nonce or self.browser_image not in built.get('RepoTags',[]): raise SafetyError('Browser image cleanup identity mismatch')
                self.docker('image','rm',self.browser_image)
            except Exception:failures.append('private-browser-image')
        self.delete_owned_work()
        if failures: raise SafetyError('Identity-checked cleanup failed for: '+','.join(failures))

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--browser-relay',action='store_true',help='Bounded host browser functional UI/count profile through guarded loopback rawTLS relay;timings excluded')
    parser.add_argument('--browser-assets',action='store_true',help='Focused10-context actual built UI cold/warm burst in guarded internal Chromium container')
    parser.add_argument('--staff-baseline',action='store_true',help='Additional30second100RPS commerce/staff variant:980customers+20staff')
    parser.add_argument('--hot-sku',action='store_true',help='Focused100-customer/five-unit isolated contention and duplicate callback profile')
    parser.add_argument('--journey-baseline',action='store_true',help='90-second LOCAL paced1000-user complete checkout journey diagnostic')
    parser.add_argument('--journey',action='store_true',help='LOCAL30-minute paced1000-user journeys; reports actual throughput')
    parser.add_argument('--api-rolling',action='store_true',help='30-second LOCAL rolling lifecycle diagnostic at100API RPS')
    parser.add_argument('--api-sustained',action='store_true',help='LOCAL30-minute100API RPS rolling run; never CAP02certification')
    parser.add_argument('--api-soak',action='store_true',help='LOCAL2-hour100API RPS rolling run; never CAP02certification')
    parser.add_argument('--api-baseline',action='store_true',help='Short exact100API RPS commerce mix with measured preparation helpers')
    parser.add_argument('--otp-login',action='store_true',help='Actual1000customer OTP request+verify/60s via isolated SMS double')
    parser.add_argument('--otp-background',action='store_true',help='OTP login with browsing:6000 API calls/60s,100 total API RPS')
    parser.add_argument('--login-ramp',action='store_true',help='Actual customer OTP request+verify for1000users/60s; local only')
    parser.add_argument('--fixture-baseline',action='store_true',help='Seed full cardinality and probe 1000 independent signed sessions; still not commerce qualification')
    parser.add_argument('--baseline',action='store_true',help='10 seconds, health only; never certification')
    parser.add_argument('--output',type=Path,default=Path(tempfile.gettempdir())/('sellersho-cap-report-'+secrets.token_hex(6)))
    args=parser.parse_args()
    _workload=json.loads((ROOT/'load-tests/workload.json').read_text())
    validate_mix(_workload['weights'])
    validate_workload_contract(_workload)
    if not (args.staff_baseline or args.browser_assets or args.browser_relay or args.hot_sku or args.baseline or args.fixture_baseline or args.login_ramp or args.otp_login or args.otp_background or args.api_baseline or args.api_rolling or args.api_sustained or args.api_soak or args.journey_baseline or args.journey): parser.error('Select a local profile; commerce qualification remains gated')
    args.output.mkdir(parents=True,exist_ok=False)
    run=Run(args.output,browser=args.browser_assets); status=1
    try:
        run.start()
        if run.baseline(smoke=True): raise SafetyError('TLS/domain setup smoke failed before full fixture seeding')
        if args.staff_baseline or args.hot_sku or args.fixture_baseline or args.login_ramp or args.otp_login or args.otp_background or args.api_baseline or args.api_rolling or args.api_sustained or args.api_soak or args.journey_baseline or args.journey: run.fixture()
        status=run.browser_assets(relay=args.browser_relay) if args.browser_assets or args.browser_relay else run.baseline(staff=args.staff_baseline,hot=args.hot_sku,otp=args.login_ramp or args.otp_login or args.otp_background,background=args.otp_background,commerce=args.api_baseline,rolling=7200 if args.api_soak else 1800 if args.api_sustained else 30 if args.api_rolling or args.staff_baseline else 0,journey=1800 if args.journey else 90 if args.journey_baseline else 0)
    except Exception as exc:
        run.manifest['failure']=str(exc) if isinstance(exc,SafetyError) else type(exc).__name__
    finally:
        try: run.close(); run.manifest['cleanup']='verified'
        except Exception: run.manifest['cleanup']='FAILED'; status=1
        (args.output/'manifest.json').write_text(json.dumps(run.manifest,indent=2))
        (args.output/'verdict.json').write_text(json.dumps(verdict({'baseline_exit':status}),indent=2))
    print('LOCAL baseline exit:',status,'reports:',args.output,'CAP-02 remains OPEN')
    return status
if __name__=='__main__': raise SystemExit(main())

