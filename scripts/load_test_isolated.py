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
    def create(self,alias,image,args=(),env=None,mounts=(),extra=(),writable=()):
        self.verify_network(); name=self.network+'-'+alias
        cmd=['create','--name',name,'--network',self.network_id,'--network-alias',alias,'--label',LABEL+'='+self.nonce,'--log-driver','local','--log-opt','max-size=10m','--log-opt','max-file=2']
        for key in (env or {}): cmd+=['--env',key]
        for source,target in mounts: cmd+=['--mount',f'type=bind,source={source},destination={target},readonly']
        for source,target in writable: cmd+=['--mount',f'type=bind,source={source},destination={target}']
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
    def baseline(self, smoke=False, login=False, otp=False, background=False, commerce=False, rolling=0, journey=0, hot=False, staff=False, inventory=False, coupon=False, independent=False):
        self.verify()
        run_started=self.sql("SELECT clock_timestamp()") if (commerce or rolling or journey or hot) else None
        before=self.docker('exec',self.backend,'wget','-q','-O','-','http://127.0.0.1:9090/metrics')
        prefix='setup' if smoke else 'baseline'
        (self.out/(prefix+'-metrics-before.prom')).write_text(before)
        cid=self.create('smoke' if smoke else 'generator','node:24-bookworm-slim',['node','/tests/inventory-100rps.js' if inventory and not smoke else '/tests/coupon-burst.mjs' if coupon and not smoke else '/tests/independent-account.mjs' if independent and not smoke else '/tests/hot-sku.mjs' if hot and not smoke else '/tests/users-1000.mjs' if journey and not smoke else '/tests/commerce-rolling.mjs' if rolling and not smoke else '/tests/commerce-baseline.mjs' if commerce and not smoke else '/tests/login-otp.mjs' if otp and not smoke else '/tests/baseline.mjs'],env={'CAPACITY_SMOKE':'1' if smoke else '0','CAPACITY_PROFILE':'login' if login else 'auth','CAPACITY_SMS_KEY':self.smskey,'CAPACITY_BACKGROUND':'1' if background else '0','CAPACITY_DURATION_SECONDS':str(journey or rolling or 30),'CAPACITY_STAFF':'1' if staff else '0','INV12_PROBE_APPROVED':os.environ.get('INV12_PROBE_APPROVED','0')},mounts=[(self.tests,'/tests'),(self.work/'root.crt','/trust/root.crt')]+([(self.work/'users.json','/sessions/users.json')] if (self.work/'users.json').exists() else []))
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
        # CAP-02 W-1.2: inventory-100rps keeps its INV12_PROBE_APPROVED=1
        # refusal — the driver exits 2 before any request (no summary is
        # written, so there is nothing to reconcile). The refusal is the
        # expected outcome, recorded as a gate, never a pass.
        if inventory and status==2:
            self.manifest['inventory_100rps_gate']='refused: INV12_PROBE_APPROVED=1 not set (INV-12 probe approval required; driver exited 2 before any request)'
            self.manifest['baseline_exit']=2
            return 2
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
              'paid_cancelled_without_obligation',(SELECT count(*) FROM orders o WHERE o.created_at>='{ts}' AND o.customer_user_id IN (SELECT id FROM users WHERE full_name LIKE 'cap-customer-%') AND o.payment_status='paid' AND o.status='cancelled' AND NOT EXISTS (SELECT 1 FROM order_refund_obligations r WHERE r.order_id=o.id AND r.purpose='cancellation')),
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
        if coupon and not smoke:
            # CAP-02 W-1.3 (Docker side): same oracle as the native
            # oracle_coupon_burst — used_count == occupying reservations,
            # consumed == discounted orders, silent-discount 0; deadlocks
            # counted from pg_stat_database; driver retries are 0 by
            # construction (no retry loop in the driver).
            coupon_oracle=json.loads(self.sql("SELECT json_build_object("
              "'coupons',(SELECT count(*) FROM seller_discount_codes WHERE code LIKE 'FIN013BURST%'),"
              "'used_count_mismatch',(SELECT count(*) FROM seller_discount_codes c WHERE c.code LIKE 'FIN013BURST%' AND c.used_count <> (SELECT COUNT(*) FROM order_coupon_reservations r WHERE r.coupon_id=c.id AND r.state IN ('reserved','consumed','review_hold'))),"
              "'consumed_reservations',(SELECT count(*) FROM order_coupon_reservations r JOIN seller_discount_codes c ON c.id=r.coupon_id WHERE c.code LIKE 'FIN013BURST%' AND r.state IN ('consumed','review_hold')),"
              "'discounted_orders',(SELECT count(*) FROM orders o JOIN order_coupon_reservations r ON r.order_id=o.id JOIN seller_discount_codes c ON c.id=r.coupon_id WHERE c.code LIKE 'FIN013BURST%' AND r.state IN ('consumed','review_hold') AND COALESCE(o.coupon_discount_amount,0)>0),"
              "'silent_discount',(SELECT count(*) FROM orders o JOIN order_coupon_reservations r ON r.order_id=o.id JOIN seller_discount_codes c ON c.id=r.coupon_id WHERE c.code LIKE 'FIN013BURST%' AND r.state IN ('consumed','review_hold') AND COALESCE(o.coupon_discount_amount,0)=0),"
              "'reserved_inflight',(SELECT count(*) FROM order_coupon_reservations r JOIN seller_discount_codes c ON c.id=r.coupon_id WHERE c.code LIKE 'FIN013BURST%' AND r.state='reserved'),"
              "'deadlocks',(SELECT deadlocks FROM pg_stat_database WHERE datname=current_database()))"))
            coupon_oracle['scope']='per-coupon reservation/counter/order agreement for the FIN013BURST coupons (FIN-013 check 6); zero silently-undiscounted orders; deadlocks counted from pg_stat_database; driver retries are 0 by construction'
            self.manifest['coupon_burst_oracle']=coupon_oracle
            if coupon_oracle['used_count_mismatch'] or coupon_oracle['silent_discount'] or coupon_oracle['consumed_reservations']!=coupon_oracle['discounted_orders']:
                status=1;self.manifest['coupon_burst_oracle_gate']='FAILED'
            else:self.manifest['coupon_burst_oracle_gate']='matched: used_count == occupying reservations, consumed == discounted orders, silent-discount 0 (deadlocks=%s)'%coupon_oracle['deadlocks']
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

    # -- CAP-02 W-3 fault matrix (Docker) --------------------------------
    # Faithful port of the native runner's W-3 profiles, using Docker
    # primitives (docker kill/start/stop, self.sql, generator containers).
    # UNPROVEN on this VM: Docker cannot run here, so no Docker W-3
    # profile has ever executed; the native profiles are the proven ones.
    # Every Docker W-3 result records docker_host_required=True and must
    # be re-run on a Docker host before it is trusted. The FIN-014 leg of
    # the W-2.1 gate is deferred on Docker (fin014-report needs a DSN to
    # the container DB); the exact host command is recorded in each gate
    # report.
    def docker_health(self):
        return subprocess.run(['docker','exec',self.backend,'wget','-q','-O','-','http://127.0.0.1:8080/api/health'],capture_output=True).returncode==0
    def docker_wait_health(self, timeout=120):
        for _ in range(timeout*2):
            if self.docker_health(): return True
            time.sleep(0.5)
        return False
    def run_container_driver(self, name, driver, extra_env, timeout=600, wait=True):
        env={'CAPACITY_SMOKE':'0','CAPACITY_PROFILE':'auth','CAPACITY_SMS_KEY':self.smskey,'CAPACITY_BACKGROUND':'0','CAPACITY_DURATION_SECONDS':'30','CAPACITY_STAFF':'0','INV12_PROBE_APPROVED':os.environ.get('INV12_PROBE_APPROVED','0')}
        env.update(extra_env or {})
        state_dir=self.work/'fault-state'; state_dir.mkdir(exist_ok=True)
        mounts=[(self.tests,'/tests'),(self.work/'root.crt','/trust/root.crt')]+([(self.work/'users.json','/sessions/users.json')] if (self.work/'users.json').exists() else [])
        cid=self.create(name,'node:24-bookworm-slim',['node','/tests/'+driver],env=env,mounts=mounts,writable=[(state_dir,'/state')])
        if not wait: return cid, None
        status=int(self.docker('wait',cid,timeout=timeout).strip())
        summary=None
        try:
            self.docker('cp',cid+':/tmp/summary.json',self.out/(name+'-summary.json'))
            summary=json.loads((self.out/(name+'-summary.json')).read_text())
        except Exception: pass
        return status, summary
    def docker_gate(self, name, since):
        import importlib.util
        spec=importlib.util.spec_from_file_location('cap02_reconcile',ROOT/'scripts'/'cap02_reconcile.py')
        mod=importlib.util.module_from_spec(spec); spec.loader.exec_module(mod)
        report={'name':name,'verdict':'PASS','legs':[],'docker_host_required':True,
                'fin014_note':'FIN-014 leg DEFERRED on Docker: run scripts/cap02_reconcile.py leg 3 on the Docker host with a DSN to the container DB (publish the PG port, or run inside the network) before trusting this verdict'}
        oracle=json.loads(self.sql(mod.COMMERCE_SQL.replace('{TS}',since.replace("'","''"))))
        stock_ok=(oracle['physical_sellable']+oracle['net_captured']==oracle['source_receipt_units'] and oracle['sku_projection']==oracle['physical_sellable'] and not oracle['payment_order_amount_mismatch'])
        oracle_fail=(oracle['price_payable_mismatch'] or oracle['price_discount_mismatch'] or oracle['ledger_negative_amount'] or oracle['ledger_orphan_order'] or oracle['paid_cancelled_without_obligation'] or oracle['duplicate_obligations'] or oracle['obligation_over_source'] or oracle['orders_with_multiple_refunds'])
        report['legs'].append({'name':'commerce (runner oracle_commerce)','status':'PASS' if (stock_ok and not oracle_fail) else 'FAIL','oracle':oracle,'paid_orders_without_ledger_entry':'INFO (not a gate)'})
        counts={}
        for line in self.sql((ROOT/'scripts'/'fin013-oracle.sql').read_text()).splitlines():
            if '|' in line:
                k,v=line.split('|',1); counts[k]=int(v)
        rows=[]; fin013_ok=True
        for k,v in counts.items():
            if k=='orders_payments_no_payment_record': ok=(v==1); note='informational, expected exactly 1 (legacy seed ord-ready-pick-001)'
            else: ok=(v==0); note=''
            rows.append({'check':k,'value':v,'status':'PASS' if ok else 'FAIL','note':note})
            if not ok: fin013_ok=False
        report['legs'].append({'name':'FIN-013 10-point oracle','status':'PASS' if fin013_ok else 'FAIL','rows':rows})
        inv_rows=[]; inv_ok=True
        has_fn=int(self.sql("SELECT count(*) FROM pg_proc WHERE proname='expected_storage_departure_sources'"))
        for cname,csql in mod.INV12_CHECKS:
            if cname=='inv_over_departures' and not has_fn:
                inv_rows.append({'check':cname,'value':None,'status':'N/A','note':'expected_storage_departure_sources() absent on this DB'}); continue
            value=int(self.sql(csql))
            inv_rows.append({'check':cname,'value':value,'status':'PASS' if value==0 else 'FAIL'})
            if value!=0: inv_ok=False
        inv_rows.append({'check':'per_lot_movement_reconstruction','value':None,'status':'N/A','note':'NOT-APPLICABLE on capacity-run DBs (see cap02_reconcile.py)'})
        report['legs'].append({'name':'INV-12 section-5 inventory checks','status':'PASS' if inv_ok else 'FAIL','rows':inv_rows})
        if any(leg['status']!='PASS' for leg in report['legs']): report['verdict']='FAIL'
        (self.out/(name+'-reconcile.json')).write_text(json.dumps(report,indent=2))
        self.manifest[name+'_reconcile']={'verdict':report['verdict'],'report':str(self.out/(name+'-reconcile.json')),'docker_host_required':True}
        return (0 if report['verdict']=='PASS' else 1), report
    def docker_plant_double_receipt(self, order_id=None):
        if order_id is None:
            order_id=self.sql("SELECT id FROM orders WHERE store_id='cap-store-1' AND payment_status='paid' AND (SELECT count(*) FROM order_payment_receipts WHERE order_id=orders.id)=1 ORDER BY created_at DESC LIMIT 1")
        if not order_id: return None
        receipts=int(self.sql("SELECT count(*) FROM order_payment_receipts WHERE order_id='%s'"%order_id))
        if receipts!=1: return None
        row=self.sql("SELECT id, order_number, amount FROM payment_transactions WHERE order_id='%s' AND status='paid'"%order_id)
        if not row: return None
        _txn_id,order_number,amount=row.split('|')
        customer=self.sql("SELECT customer_user_id FROM orders WHERE id='%s'"%order_id)
        cust=("'%s'"%customer) if customer else 'NULL'
        suffix=secrets.token_hex(4)
        txn2='cap-double-txn-'+suffix; ver2='cap-double-ver-'+suffix; rcpt2='cap-double-rcpt-'+suffix
        self.sql("INSERT INTO payment_transactions (id, order_id, order_number, gateway_token, rrn, amount, status) VALUES ('%s','%s','%s','cap-double-token-%s',NULL,%s,'failed')"%(txn2,order_id,order_number,suffix,amount))
        self.sql("INSERT INTO order_payment_verifications (id, payment_id, order_id, customer_id, amount_toman, currency, funding_kind) VALUES ('%s','%s','%s',%s,%s,'TOMAN','simulated')"%(ver2,txn2,order_id,cust,amount))
        self.sql("INSERT INTO order_payment_receipts (id, verification_id, funding_kind, payment_id, order_id, customer_id, amount_toman, currency, provider_reference, verified_at) VALUES ('%s','%s','simulated','%s','%s',%s,%s,'TOMAN','cap-double-ref-%s',NOW())"%(rcpt2,ver2,txn2,order_id,cust,amount,suffix))
        return {'order_id':order_id,'txn':txn2,'verification':ver2,'receipt':rcpt2}
    def docker_unplant_double_receipt(self, planted):
        if not planted: return
        # Receipts + verifications are immutable by trigger (same as the
        # native unplant_double_receipt): disable, delete, re-enable.
        self.sql("ALTER TABLE order_payment_receipts DISABLE TRIGGER payment_receipt_identity; ALTER TABLE order_payment_verifications DISABLE TRIGGER payment_verification_identity; DELETE FROM order_payment_receipts WHERE id='%s'; DELETE FROM order_payment_verifications WHERE id='%s'; DELETE FROM payment_transactions WHERE id='%s'; ALTER TABLE order_payment_receipts ENABLE TRIGGER payment_receipt_identity; ALTER TABLE order_payment_verifications ENABLE TRIGGER payment_verification_identity"%(planted['receipt'],planted['verification'],planted['txn']))
    def restart_backend_docker(self, latencies=None):
        env=dict(self.appenv); env['CAPACITY_PAYMENT_LATENCIES_MS']=latencies or '25,25,50,50,150'
        old=self.backend
        try: self.docker('stop',old)
        except Exception: pass
        try: self.docker('rm',old)
        except Exception: pass
        self.containers=[(c,n) for c,n in self.containers if c!=old]
        self.backend=self.create('backend','alpine:latest',['/server'],env=env,mounts=[(self.work/'server','/server'),(self.source/'migrations','/migrations')])
        if not self.docker_wait_health(): raise SafetyError('backend health failed after restart')
    def run_fault_boundary_docker(self, boundary):
        key='fault_boundary_'+boundary+'_docker'; result={'boundary':boundary,'docker_host_required':True}
        since=self.sql('SELECT clock_timestamp()')
        state_path=self.work/'fault-state'/('boundary-'+boundary+'.json')
        setup_env={'CAPACITY_BOUNDARY':boundary,'CAPACITY_BOUNDARY_PHASE':'setup','CAPACITY_BOUNDARY_STATE_FILE':'/state/boundary-'+boundary+'.json'}
        s,_=self.run_container_driver('fault-boundary-%s-setup'%boundary,'fault-boundary.mjs',setup_env,300)
        result['setup_exit']=s
        if s!=0 or not state_path.exists():
            result['error']='setup failed'; self.manifest[key]=result; return 1
        state=json.loads(state_path.read_text()); result['order_id']=state['orderId']
        if boundary=='b1':
            self.docker('kill',self.backend); self.docker('start',self.backend)
            if not self.docker_wait_health(): result['error']='backend did not recover'; self.manifest[key]=result; return 1
            result['kill_point']='before callback'
        elif boundary in ('b2','b3'):
            if boundary=='b2': self.restart_backend_docker(latencies='5000,5000,5000,5000,5000')
            attempt,lost=0,False
            while attempt<3 and not lost:
                attempt+=1
                cb_name='fault-boundary-%s-callback-%d'%(boundary,attempt)
                cb_env={'CAPACITY_BOUNDARY':boundary,'CAPACITY_BOUNDARY_PHASE':'callback','CAPACITY_BOUNDARY_STATE_FILE':'/state/boundary-'+boundary+'.json'}
                cid,_=self.run_container_driver(cb_name,'fault-boundary.mjs',cb_env,300,wait=False)
                killed=False; deadline=time.monotonic()+20
                while time.monotonic()<deadline:
                    try: logs=self.docker('logs',cid)
                    except Exception: logs=''
                    if 'CAPACITY_CALLBACK_STARTED' in logs:
                        if boundary=='b2': time.sleep(1.0)
                        self.docker('kill',self.backend); killed=True; break
                    try:
                        if int(self.docker('wait',cid,timeout=1).strip()) is not None: break
                    except Exception: pass
                    time.sleep(0.1)
                try: rc=int(self.docker('wait',cid,timeout=60).strip())
                except Exception: rc=None
                st=json.loads(state_path.read_text())
                result['callback_attempt_%d'%attempt]={'exit':rc,'outcome':st.get('callback_outcome'),'killed_mid_flight':killed}
                lost=(st.get('callback_outcome')=='lost')
                if lost: break
                try: self.docker('start',self.backend)
                except Exception: self.restart_backend_docker(latencies='5000,5000,5000,5000,5000' if boundary=='b2' else None)
                if not self.docker_wait_health(): result['error']='backend did not recover after retry'; self.manifest[key]=result; return 1
                s2,_=self.run_container_driver('fault-boundary-%s-setup-%d'%(boundary,attempt),'fault-boundary.mjs',setup_env,300)
                if s2!=0: result['error']='retry setup failed'; break
                state=json.loads(state_path.read_text()); result['order_id']=state['orderId']
            result['callback_attempts']=attempt; result['callback_lost']=lost
            if not lost:
                result['error']='boundary not exercised: kill never landed mid-callback in %d attempts (Docker log polling is ~100ms; b3 is best-effort here)'%attempt
                self.manifest[key]=result; return 1
            try: self.docker('start',self.backend)
            except Exception: self.restart_backend_docker()
            if not self.docker_wait_health(): result['error']='backend did not recover'; self.manifest[key]=result; return 1
        elif boundary=='b4':
            order_id=state['orderId']
            self.sql("CREATE OR REPLACE FUNCTION cap_boundary_b4_slow() RETURNS trigger AS $$ BEGIN IF NEW.id = '%s' AND NEW.status='cancelled' AND OLD.status<>'cancelled' THEN PERFORM pg_sleep(0.15); END IF; RETURN NEW; END $$ LANGUAGE plpgsql; DROP TRIGGER IF EXISTS cap_boundary_b4_slow_trg ON orders; CREATE TRIGGER cap_boundary_b4_slow_trg BEFORE UPDATE ON orders FOR EACH ROW EXECUTE FUNCTION cap_boundary_b4_slow();"%order_id)
            # Queue-jump: the boundary order's payment_expires_at is moved
            # to a fixed ancient time so it sorts FIRST in the expiry job's
            # ORDER BY payment_expires_at queue (the fixture leaves a ~7200
            # expired backlog the job cannot drain within the profile's
            # window; the hold DURATION is not what b4 tests). Deadline=NOW.
            self.sql("UPDATE orders SET payment_expires_at='2000-01-01 00:00:00+00' WHERE id='%s'"%order_id)
            deadline=time.time()
            observed=False; kill_deadline=time.time()+60
            while time.time()<kill_deadline:
                if time.time()>=deadline:
                    n=int(self.sql("SELECT count(*) FROM pg_stat_activity WHERE datname='%s' AND state='active' AND wait_event='PgSleep'"%self.database))
                    if n>0: observed=True; break
                time.sleep(0.05)
            self.docker('kill',self.backend); self.docker('start',self.backend)
            result['kill_point']='during expiry batch (PgSleep observed)' if observed else 'after deadline (batch not observed; best-effort)'
            result['expiry_batch_observed']=observed
            if not self.docker_wait_health(): result['error']='backend did not recover'; self.manifest[key]=result; return 1
            expired=False
            for _ in range(240):
                if self.sql("SELECT status FROM orders WHERE id='%s'"%order_id)=='cancelled': expired=True; break
                time.sleep(0.5)
            result['expired_after_restart']=expired
            self.sql('DROP TRIGGER IF EXISTS cap_boundary_b4_slow_trg ON orders; DROP FUNCTION IF EXISTS cap_boundary_b4_slow();')
            if not expired: result['error']='order did not expire after restart'; self.manifest[key]=result; return 1
        replay_env={'CAPACITY_BOUNDARY':boundary,'CAPACITY_BOUNDARY_PHASE':'replay','CAPACITY_BOUNDARY_STATE_FILE':'/state/boundary-'+boundary+'.json'}
        s,_=self.run_container_driver('fault-boundary-%s-replay'%boundary,'fault-boundary.mjs',replay_env,300)
        result['replay_exit']=s
        if s!=0: result['error']='replay failed (not exactly once)'; self.manifest[key]=result; return 1
        gate_exit,report=self.docker_gate('fault-boundary-'+boundary,since)
        result['gate_exit']=gate_exit; result['gate_verdict']=report['verdict']
        if gate_exit!=0: result['error']='W-2.1 gate FAILED on the clean run'; self.manifest[key]=result; return 1
        plant_order=state['orderId'] if boundary!='b4' else None
        planted=self.docker_plant_double_receipt(plant_order)
        if planted is None: result['negative_control']={'error':'no paid order to plant against'}; self.manifest[key]=result; return 1
        nc_exit,nc_report=self.docker_gate('fault-boundary-'+boundary+'-nc',since)
        result['negative_control']={'planted_order':planted['order_id'],'gate_exit':nc_exit,'gate_verdict':nc_report['verdict'],'expected':'FAIL'}
        self.docker_unplant_double_receipt(planted)
        nc2_exit,nc2_report=self.docker_gate('fault-boundary-'+boundary+'-nc-clean',since)
        result['negative_control_restore']={'gate_exit':nc2_exit,'gate_verdict':nc2_report['verdict'],'expected':'PASS'}
        ok=nc_exit!=0 and nc2_exit==0
        if not ok: result['error']='negative control misbehaved (planted gate exit %s, restored gate exit %s)'%(nc_exit,nc2_exit)
        self.manifest[key]=result; return 0 if ok else 1
    def run_fault_db_loss_docker(self):
        key='fault_db_loss_docker'; result={'loss_seconds':30,'docker_host_required':True}
        since=self.sql('SELECT clock_timestamp()')
        load_cid,_=self.run_container_driver('fault-db-loss-load','commerce-rolling.mjs',{'CAPACITY_DURATION_SECONDS':'120'},900,wait=False)
        time.sleep(30)
        self.docker('stop',self.pg)
        health_during=[]
        for _ in range(6): health_during.append(self.docker_health()); time.sleep(1)
        result['health_during_loss']=health_during
        time.sleep(30-6)
        self.docker('start',self.pg)
        recovered=self.docker_wait_health(60)
        result['recovered']=recovered
        try: load_exit=int(self.docker('wait',load_cid,timeout=120).strip())
        except Exception: load_exit=None
        result['load_exit']=load_exit
        gate_exit,report=self.docker_gate('fault-db-loss',since)
        result['gate_exit']=gate_exit; result['gate_verdict']=report['verdict']
        self.docker('stop',self.pg)
        nc_health=[]
        for _ in range(10): nc_health.append(self.docker_health()); time.sleep(1)
        result['negative_control_health_during_loss']=nc_health
        result['negative_control_stayed_down']=all(not h for h in nc_health)
        self.docker('start',self.pg)
        restored=self.docker_wait_health(60)
        result['negative_control_restored']=restored
        ok=(recovered and gate_exit==0 and result['negative_control_stayed_down'] and restored)
        if not ok: result['error']='recovered=%s gate_exit=%s nc_stayed_down=%s restored=%s'%(recovered,gate_exit,result['negative_control_stayed_down'],restored)
        self.manifest[key]=result; return 0 if ok else 1
    def run_fault_worker_failover_docker(self):
        key='fault_worker_failover_docker'; result={'docker_host_required':True}
        since=self.sql('SELECT clock_timestamp()')
        try:
            self.sql("INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,payment_expires_at) SELECT 'cap-w33-e-'||g::text,'cap-w33-e-'||g::text,'cap-store-1','job',2000,2000,'job','09123456789','x','x','x','1234512345','cap-w33-e-'||g::text,'2000-01-01 00:00:00+00'::timestamptz FROM generate_series(1,60)g; INSERT INTO order_items(id,order_id,product_id,quantity,wholesale_cost,platform_fee,seller_margin,final_unit_price) SELECT md5('cap-w33-e-'||g::text)::uuid,'cap-w33-e-'||g::text,'cap-product-1',1,1000,0,1000,2000 FROM generate_series(1,60)g; CREATE OR REPLACE FUNCTION cap_w33_slow_expiry() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id LIKE 'cap-w33-e-%' AND NEW.status='cancelled' THEN PERFORM pg_sleep(0.025); END IF; RETURN NEW; END $$; DROP TRIGGER IF EXISTS cap_w33_slow_expiry_trg ON orders; CREATE TRIGGER cap_w33_slow_expiry_trg BEFORE UPDATE ON orders FOR EACH ROW EXECUTE FUNCTION cap_w33_slow_expiry();")
            stock_before=self.sql("SELECT stock FROM products WHERE id='cap-product-1'")
            owner_before=self.sql("SELECT owner FROM background_job_state WHERE job='unpaid-order-expiry'")
            committed=0
            for _ in range(300):
                committed=int(self.sql("SELECT count(*) FROM orders WHERE id LIKE 'cap-w33-e-%' AND status='cancelled'"))
                if committed>=3: break
                time.sleep(0.1)
            result['expiry_committed_before_kill']=committed
            if committed<3: result['error']='expiry batch made no committed progress'; self.manifest[key]=result; return 1
            self.docker('kill',self.backend); time.sleep(6); self.docker('start',self.backend)
            if not self.docker_wait_health(): result['error']='backend did not recover'; self.manifest[key]=result; return 1
            cancelled=0
            for _ in range(240):
                cancelled=int(self.sql("SELECT count(*) FROM orders WHERE id LIKE 'cap-w33-e-%' AND status='cancelled'"))
                if cancelled==60: break
                time.sleep(0.5)
            stock_after=self.sql("SELECT stock FROM products WHERE id='cap-product-1'")
            owner_after=self.sql("SELECT owner FROM background_job_state WHERE job='unpaid-order-expiry'")
            result['expiry']={'cancelled':cancelled,'stock_unchanged':stock_before==stock_after,'owner_before':owner_before,'owner_after':owner_after,'lease_takeover':bool(owner_before) and owner_before!=owner_after}
            if cancelled!=60 or stock_before!=stock_after: result['error']='expiry batch did not drain exactly once'; self.manifest[key]=result; return 1
            picked=self.sql("SELECT string_agg(id, ',') FROM (SELECT o.id FROM orders o WHERE o.status='delivered_confirmed' AND o.profit_released AND o.referrer_seller_id IS NULL AND (SELECT count(*) FROM ledger_entries l WHERE l.related_order_or_dispute=o.id AND l.operator='system:delivery-lock-releaser')=2 ORDER BY o.id LIMIT 20) s")
            if not picked: result['error']='no 20 released orders with exactly 2 ledger entries to un-release'; self.manifest[key]=result; return 1
            ids=picked.split(','); id_list="'"+"','".join(ids)+"'"
            self.sql("ALTER TABLE ledger_entries DISABLE TRIGGER trg_ledger_entries_append_only; ALTER TABLE ledger_entries DISABLE TRIGGER fin005_linked_ledger_immutable; DELETE FROM ledger_entries WHERE related_order_or_dispute IN (%s) AND operator='system:delivery-lock-releaser'; ALTER TABLE ledger_entries ENABLE TRIGGER trg_ledger_entries_append_only; ALTER TABLE ledger_entries ENABLE TRIGGER fin005_linked_ledger_immutable; UPDATE orders SET profit_released=false, profit_released_at=NULL, referral_commission_released=false WHERE id IN (%s)"%(id_list,id_list))
            self.sql("CREATE OR REPLACE FUNCTION cap_w33_slow_release() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.related_order_or_dispute IN (%s) THEN PERFORM pg_sleep(0.025); END IF; RETURN NEW; END $$; DROP TRIGGER IF EXISTS cap_w33_slow_release_trg ON ledger_entries; CREATE TRIGGER cap_w33_slow_release_trg BEFORE INSERT ON ledger_entries FOR EACH ROW EXECUTE FUNCTION cap_w33_slow_release();"%id_list)
            owner_rel_before=self.sql("SELECT owner FROM background_job_state WHERE job='delivery-lock-releaser'")
            first=0
            for _ in range(900):
                first=int(self.sql("SELECT count(*) FROM ledger_entries WHERE related_order_or_dispute IN (%s) AND operator='system:delivery-lock-releaser'"%id_list))
                if first>=1: break
                time.sleep(0.1)
            result['release_entries_before_kill']=first
            if first<1: result['error']='release batch never started (no ledger entry in 90 s)'; self.manifest[key]=result; return 1
            self.docker('kill',self.backend); time.sleep(15); self.docker('start',self.backend)
            if not self.docker_wait_health(): result['error']='backend did not recover'; self.manifest[key]=result; return 1
            released=0
            for _ in range(360):
                released=int(self.sql("SELECT count(*) FROM orders WHERE id IN (%s) AND profit_released"%id_list))
                if released==20: break
                time.sleep(0.5)
            paired=self.sql("SELECT count(*) FROM (SELECT o.id FROM orders o WHERE o.id IN (%s) AND o.profit_released AND (SELECT count(*) FROM ledger_entries l WHERE l.related_order_or_dispute=o.id AND l.operator='system:delivery-lock-releaser')=2 AND (SELECT COALESCE(SUM(CASE WHEN l.type='credit' THEN l.amount ELSE -l.amount END),0) FROM ledger_entries l WHERE l.related_order_or_dispute=o.id AND l.operator='system:delivery-lock-releaser')=0) s"%id_list)
            replay=int(self.sql("SELECT count(*) FROM orders WHERE id IN (%s) AND status='delivered_confirmed' AND NOT profit_released AND return_window_ends_at<=NOW()"%id_list))
            owner_rel_after=self.sql("SELECT owner FROM background_job_state WHERE job='delivery-lock-releaser'")
            result['release']={'released':released,'paired_balanced':int(paired),'replay_candidates':replay,'owner_before':owner_rel_before,'owner_after':owner_rel_after,'lease_takeover':bool(owner_rel_before) and owner_rel_before!=owner_rel_after}
            if released!=20 or int(paired)!=20 or replay!=0: result['error']='release batch did not re-release exactly once'; self.manifest[key]=result; return 1
            gate_exit,report=self.docker_gate('fault-worker-failover',since)
            result['gate_exit']=gate_exit; result['gate_verdict']=report['verdict']
            if gate_exit!=0: result['error']='W-2.1 gate FAILED after failover'; self.manifest[key]=result; return 1
        finally:
            self.sql('DROP TRIGGER IF EXISTS cap_w33_slow_expiry_trg ON orders; DROP FUNCTION IF EXISTS cap_w33_slow_expiry(); DROP TRIGGER IF EXISTS cap_w33_slow_release_trg ON ledger_entries; DROP FUNCTION IF EXISTS cap_w33_slow_release();')
        self.manifest[key]=result; return 0
    def run_fault_redis_load_docker(self):
        key='fault_redis_load_docker'; result={'docker_host_required':True,'host_crash_1s_loss_window':'untestable-by-kill (process kill loses nothing on this scratch Redis; the prod AOF-everysec window needs a host-level test)'}
        since=self.sql('SELECT clock_timestamp()')
        state_path=self.work/'fault-state'/'boundary-b1.json'
        s,_=self.run_container_driver('fault-redis-load-setup','fault-boundary.mjs',{'CAPACITY_BOUNDARY':'b1','CAPACITY_BOUNDARY_PHASE':'setup','CAPACITY_BOUNDARY_STATE_FILE':'/state/boundary-b1.json'},300)
        if s!=0 or not state_path.exists(): result['error']='order preparation failed'; self.manifest[key]=result; return 1
        state=json.loads(state_path.read_text()); result['order_id']=state['orderId']
        load_cid,_=self.run_container_driver('fault-redis-load-load','commerce-rolling.mjs',{'CAPACITY_DURATION_SECONDS':'120'},900,wait=False)
        time.sleep(30)
        self.docker('stop',self.network+'-cache')
        health_during=[]
        for _ in range(3): health_during.append(self.docker_health()); time.sleep(1)
        result['health_during_redis_down']=health_during
        self.docker('start',self.network+'-cache')
        env={k:v for k,v in os.environ.items() if k in ('PATH','SystemRoot','SYSTEMROOT','TEMP','TMP','USERPROFILE','LOCALAPPDATA','APPDATA','HOME','GOPATH','GOCACHE')}
        env.update(GOOS='linux',GOARCH='amd64',CGO_ENABLED='0')
        self.command(['go','build','-o',str(self.work/'redis-recover'),'./cmd/redis-recover'],env=env,cwd=self.source)
        rebuild_cid=self.create('recover','alpine:latest',['/redis-recover','rebuild','-db',self.appenv['DATABASE_URL'],'-redis',self.appenv['REDIS_URL']],mounts=[(self.work/'redis-recover','/redis-recover')])
        cb_cid,_=self.run_container_driver('fault-redis-load-callback','fault-boundary.mjs',{'CAPACITY_BOUNDARY':'b1','CAPACITY_BOUNDARY_PHASE':'callback','CAPACITY_BOUNDARY_STATE_FILE':'/state/boundary-b1.json'},300,wait=False)
        try: rebuild_rc=int(self.docker('wait',rebuild_cid,timeout=300).strip())
        except Exception: rebuild_rc=None
        try: cb_rc=int(self.docker('wait',cb_cid,timeout=60).strip())
        except Exception: cb_rc=None
        result['rebuild_exit']=rebuild_rc; result['callback_exit']=cb_rc
        st=json.loads(state_path.read_text()); result['callback_during_rebuild']={'outcome':st.get('callback_outcome'),'body':st.get('callback_body')}
        callback_ok=False
        if st.get('callback_outcome')=='paid': callback_ok=True; result['callback_outcome']='paid (hold survived/rebuilt in time)'
        elif st.get('callback_outcome','').startswith('other:409'):
            obligations=int(self.sql("SELECT count(*) FROM order_refund_obligations WHERE order_id='%s'"%state['orderId']))
            result['callback_outcome']='refund path (hold expired during rebuild)'; result['refund_obligations']=obligations; callback_ok=obligations>=1
        else: result['callback_outcome']='unexpected: %s'%st.get('callback_outcome')
        recovered=self.docker_wait_health(60); result['recovered']=recovered
        try: load_exit=int(self.docker('wait',load_cid,timeout=120).strip())
        except Exception: load_exit=None
        result['load_exit']=load_exit
        gate_exit,report=self.docker_gate('fault-redis-load',since)
        result['gate_exit']=gate_exit; result['gate_verdict']=report['verdict']
        ok=(recovered and rebuild_rc==0 and callback_ok and gate_exit==0)
        if not ok: result['error']='recovered=%s rebuild=%s callback_ok=%s gate_exit=%s'%(recovered,rebuild_rc,callback_ok,gate_exit)
        self.manifest[key]=result; return 0 if ok else 1
    def run_fault_docker(self, profile):
        if profile.startswith('fault-boundary-'): return self.run_fault_boundary_docker(profile[len('fault-boundary-'):])
        if profile=='fault-db-loss': return self.run_fault_db_loss_docker()
        if profile=='fault-worker-failover': return self.run_fault_worker_failover_docker()
        if profile=='fault-redis-load': return self.run_fault_redis_load_docker()
        raise SafetyError('unknown Docker fault profile: '+profile)

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
    parser.add_argument('--inventory-100rps',action='store_true',help='CAP-02 W-1.2: 100-RPS inventory probe; KEEPS its INV12_PROBE_APPROVED=1 refusal (exits 2 without approval); registration only')
    parser.add_argument('--coupon-burst',action='store_true',help='CAP-02 W-1.2: coupon-burst contention profile with the runner-side oracle (W-1.3); Certified:false')
    parser.add_argument('--independent-account',action='store_true',help='CAP-02 W-1.2: 100 independent-account profile; registration only')
    parser.add_argument('--fault-boundary-b1',action='store_true',help='CAP-02 W-3.1 (Docker): kill before callback, replay exactly once; diagnostic scale; UNPROVEN on this VM')
    parser.add_argument('--fault-boundary-b2',action='store_true',help='CAP-02 W-3.1 (Docker): kill ~1s into slow payment verification; diagnostic scale; UNPROVEN on this VM')
    parser.add_argument('--fault-boundary-b3',action='store_true',help='CAP-02 W-3.1 (Docker): kill after callback flush, before response read; diagnostic scale; UNPROVEN on this VM')
    parser.add_argument('--fault-boundary-b4',action='store_true',help='CAP-02 W-3.1 (Docker): kill during expiry batch after the 15-min hold deadline; diagnostic scale; UNPROVEN on this VM')
    parser.add_argument('--fault-db-loss',action='store_true',help='CAP-02 W-3.2 (Docker): 30s DB loss via docker stop/start; fails closed; diagnostic scale; UNPROVEN on this VM')
    parser.add_argument('--fault-worker-failover',action='store_true',help='CAP-02 W-3.3 (Docker): kill mid-batch for expiry and delivery/release; diagnostic scale; UNPROVEN on this VM')
    parser.add_argument('--fault-redis-load',action='store_true',help='CAP-02 W-3.4 (Docker): Redis restart under load + rebuild + callback-during-rebuild; diagnostic scale; UNPROVEN on this VM')
    parser.add_argument('--baseline',action='store_true',help='10 seconds, health only; never certification')
    parser.add_argument('--output',type=Path,default=Path(tempfile.gettempdir())/('sellersho-cap-report-'+secrets.token_hex(6)))
    args=parser.parse_args()
    _workload=json.loads((ROOT/'load-tests/workload.json').read_text())
    validate_mix(_workload['weights'])
    validate_workload_contract(_workload)
    if not (args.staff_baseline or args.browser_assets or args.browser_relay or args.hot_sku or args.baseline or args.fixture_baseline or args.login_ramp or args.otp_login or args.otp_background or args.api_baseline or args.api_rolling or args.api_sustained or args.api_soak or args.journey_baseline or args.journey or args.inventory_100rps or args.coupon_burst or args.independent_account or args.fault_boundary_b1 or args.fault_boundary_b2 or args.fault_boundary_b3 or args.fault_boundary_b4 or args.fault_db_loss or args.fault_worker_failover or args.fault_redis_load): parser.error('Select a local profile; commerce qualification remains gated')
    args.output.mkdir(parents=True,exist_ok=False)
    run=Run(args.output,browser=args.browser_assets); status=1
    try:
        run.start()
        if run.baseline(smoke=True): raise SafetyError('TLS/domain setup smoke failed before full fixture seeding')
        if args.staff_baseline or args.hot_sku or args.fixture_baseline or args.login_ramp or args.otp_login or args.otp_background or args.api_baseline or args.api_rolling or args.api_sustained or args.api_soak or args.journey_baseline or args.journey or args.inventory_100rps or args.coupon_burst or args.independent_account or args.fault_boundary_b1 or args.fault_boundary_b2 or args.fault_boundary_b3 or args.fault_boundary_b4 or args.fault_db_loss or args.fault_worker_failover or args.fault_redis_load: run.fixture()
        fault_profile=('fault-boundary-b1' if args.fault_boundary_b1 else 'fault-boundary-b2' if args.fault_boundary_b2 else 'fault-boundary-b3' if args.fault_boundary_b3 else 'fault-boundary-b4' if args.fault_boundary_b4 else 'fault-db-loss' if args.fault_db_loss else 'fault-worker-failover' if args.fault_worker_failover else 'fault-redis-load' if args.fault_redis_load else None)
        status=run.run_fault_docker(fault_profile) if fault_profile else run.browser_assets(relay=args.browser_relay) if args.browser_assets or args.browser_relay else run.baseline(staff=args.staff_baseline,hot=args.hot_sku,otp=args.login_ramp or args.otp_login or args.otp_background,background=args.otp_background,commerce=args.api_baseline,rolling=7200 if args.api_soak else 1800 if args.api_sustained else 30 if args.api_rolling or args.staff_baseline else 0,journey=1800 if args.journey else 90 if args.journey_baseline else 0,inventory=args.inventory_100rps,coupon=args.coupon_burst,independent=args.independent_account)
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

