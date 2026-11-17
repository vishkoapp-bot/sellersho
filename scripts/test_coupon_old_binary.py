#!/usr/bin/env python3
"""FIN007 actual mixed-binary rehearsal. Owned disposable Docker targets only.
No existing database, provider URL, password or server target is accepted.
"""
import argparse,base64,hashlib,hmac,json,os,re,queue,secrets,subprocess,sys,threading,time,urllib.error,urllib.request,uuid
from pathlib import Path
LABEL='sellersho.fin007-rehearsal'
class Failure(RuntimeError):pass

def frozen(value):
 p=Path(value)
 if not p.is_absolute():raise Failure('Source must be an absolute frozen archive')
 p=p.resolve(strict=True)
 if p.parent.name!='releases' or not p.name.startswith(('fin007-source-','fin007-old-source-')):raise Failure('Source must be a FIN007 excluded release archive')
 if (p/'.git').exists() or (p/'docs/fa').exists() or (p/'backend/internal/networkreward').exists():raise Failure('Source contains protected paths or mutable checkout metadata')
 if not (p/'backend/go.mod').is_file():raise Failure('Frozen Go source missing')
 return p

def digest(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def tree_digest(p):
 h=hashlib.sha256()
 for file in sorted((p/'backend').rglob('*')):
  if file.is_file() and file.suffix in ('.go','.sql','.json','.mod','.sum'):
   h.update(file.relative_to(p).as_posix().encode());h.update(file.read_bytes())
 return h.hexdigest()

class Run:
 def __init__(self,args):
  self.old=frozen(args.old_source);self.new=frozen(args.new_source)
  if not self.old.name.startswith('fin007-old-source-08c6d5b'):raise Failure('Actual old source must be the sealed 08c6d5b archive')
  self.out=Path(args.output).resolve();self.out.mkdir(parents=True,exist_ok=False)
  self.nonce=secrets.token_hex(12);self.name='sellersho-fin007-'+self.nonce;self.network=None;self.containers=[]
  self.password=secrets.token_hex(24);self.jwt=secrets.token_hex(32);self.carrier=secrets.token_hex(32);self.database='sellersho_test_'+self.nonce
  self.manifest={'scope':'disposable actual old/new server binaries','harness_sha256':digest(Path(__file__)),'old_commit':'08c6d5b5451c3580407e3defc6a3320b11f92710','old_source':str(self.old),'new_source':str(self.new),'old_source_digest':tree_digest(self.old),'new_source_digest':tree_digest(self.new),'results':{},'listener':'Exact old main binds wildcard inside owned internal network; host exposure is127.0.0.1 only','providers':'test-only in-process payment; internal quote-only carrier double; no external egress','fixture_deadlines':'Accepted fixture orders explicitly extended +1h during rehearsal; only expiry targets set due; production timeout/budgets unchanged'}
 def safe(self,text):
  for value in (self.password,self.jwt,self.carrier,getattr(self,'token',''),getattr(self,'supplier_token','')):
   if value:text=text.replace(value,'[redacted]')
  return text
 def cmd(self,args,**kw):
  r=subprocess.run(args,text=True,encoding='utf-8',errors='replace',capture_output=True,timeout=kw.pop('timeout',300),**kw)
  if r.returncode:raise Failure(self.safe('Command failed: '+r.stderr[-3000:]))
  return (r.stdout+r.stderr if len(args)>1 and args[1]=='logs' else r.stdout).strip()
 def docker(self,*args,**kw):return self.cmd(['docker',*args],**kw)
 def netcheck(self):
  i=json.loads(self.docker('network','inspect',self.network))[0]
  if i['Id']!=self.network or not i['Internal'] or i.get('Labels',{}).get(LABEL)!=self.nonce:raise Failure('Owned internal network identity mismatch')
 def inspect(self,cid):
  self.netcheck();i=json.loads(self.docker('inspect',cid))[0]
  if i['Id']!=cid or i['Config'].get('Labels',{}).get(LABEL)!=self.nonce or i['HostConfig']['NetworkMode']!=self.network:raise Failure('Owned container identity mismatch')
  return i
 def create(self,alias,image,command=(),env=None,mounts=(),extra=(),start=True):
  self.netcheck();argv=['create','--name',self.name+'-'+alias,'--network',self.network,'--network-alias',alias,'--label',LABEL+'='+self.nonce,'--log-driver','local','--log-opt','max-size=5m','--log-opt','max-file=2']
  for k,v in (env or {}).items():argv+=['-e',k+'='+v]
  for source,target in mounts:argv+=['--mount','type=bind,source='+str(source)+',target='+target+',readonly']
  argv+=list(extra)+[image]+list(command);cid=self.docker(*argv);self.containers.append((alias,cid));self.inspect(cid)
  if start:self.docker('start',cid)
  return cid
 def sql(self,q):
  self.inspect(self.pg)
  return self.docker('exec','-i',self.pg,'psql','-X','-v','ON_ERROR_STOP=1','-U','postgres','-d',self.database,'-At',input=q)
 def barrier(self,process):
  ready=queue.Queue()
  def read():
   for line in process.stdout:
    if line.strip()=='barrier':ready.put(True);return
   ready.put(False)
  threading.Thread(target=read,daemon=True).start()
  try:ok=ready.get(timeout=10)
  except queue.Empty:raise Failure('Owned SQL lock barrier deadline exceeded')
  if not ok:raise Failure('Owned SQL lock barrier failed before readiness')
 def retain(self,fn,results):
  try:results.append(fn())
  except Exception as e:results.append(Failure(self.safe(str(e))))
 def record(self,name,value):self.manifest['results'][name]=value;print(name+': '+str(value),flush=True)
 def healthy(self,cid):
  r=subprocess.run(['docker','exec',cid,'wget','-q','-O','-','http://127.0.0.1:8080/api/health'],capture_output=True,timeout=5)
  return r.returncode==0
 def server(self,alias,binary,source):
  cid=self.create(alias,'alpine:latest',['/server'],env=self.env,mounts=[(binary,'/server'),(source/'backend/migrations','/migrations')],extra=['--publish','127.0.0.1::8080'])
  i=self.inspect(cid)
  declared=i['HostConfig'].get('PortBindings',{}).get('8080/tcp',[])
  if not declared or any(p['HostIp']!='127.0.0.1' for p in declared):raise Failure('Non-loopback configured host listener')
  # Docker internal networks may deliberately leave host port allocations empty.
  # Exercise the exact server's loopback HTTP listener via its owned container;
  # never add an external network to make a test transport work.
  self.manifest.setdefault('http_transport',{})[alias]={'configured_host_ip':'127.0.0.1','effective_host_bindings':i['NetworkSettings'].get('Ports',{}).get('8080/tcp',[]),'requests':'owned docker exec to127.0.0.1:8080'}
  for _ in range(120):
   if not self.inspect(cid)['State']['Running']:return cid,cid,False
   if self.healthy(cid):return cid,cid,True
   time.sleep(.5)
  raise Failure('Server startup deadline exceeded')
 def request(self,base,path,body=None,token=None):
  self.inspect(base)
  args=['docker','exec',base,'wget','-S','-O','-','-T','15','--header','Content-Type: application/json','--header','Authorization: Bearer '+(token or self.token)]
  if body is not None:args+=['--post-data',json.dumps(body)]
  r=subprocess.run(args+['http://127.0.0.1:8080'+path],text=True,encoding='utf-8',errors='replace',capture_output=True,timeout=20)
  statuses=re.findall(r'HTTP/\d(?:\.\d)? (\d{3})',r.stderr)
  if not statuses:raise Failure(self.safe('Owned loopback HTTP transport failed '+r.stderr[-500:]))
  status=int(statuses[-1]);text=r.stdout
  try:body=json.loads(text)
  except ValueError:body={'error':text[:300]}
  return status,body
 def expect(self,base,path,body,status):
  actual,result=self.request(base,path,body)
  if actual!=status:raise Failure(self.safe('Route '+path+' expected '+str(status)+' got '+str(actual)+' '+str(result)[:500]))
  return result
 def checkout(self,base,discount=True,key=None):
  item={'productId':'cap-product-1','quantity':1,'variantId':str(uuid.UUID(hashlib.md5(b'cap-sku-1').hexdigest()))}
  dest={'province':'Tehran','city':'Tehran','address':'Synthetic FIN007 address','postalCode':'1111111111'}
  quote=getattr(self,'fixture_quote',None)
  if quote is None:
   quote=self.expect(base,'/api/v1/shipping/quote',{'items':[item],'destination':dest},200);self.fixture_quote=quote
  body={'storeId':'cap-store-2','items':[item],'customerInfo':dict(dest,fullName='Synthetic FIN007 customer',phone='09900000001'),'paymentMethod':'cash','shippingQuoteId':quote['quoteId']}
  if discount:body['discountCode']='SAVE'
  if key:body['idempotencyKey']=key
  order=self.expect(base,'/api/v1/orders',body,201)
  self.sql("UPDATE orders SET payment_expires_at=NOW()+INTERVAL '1 hour' WHERE id='"+order['id']+"'")
  return body,order
 def token_for(self,base,o):return self.expect(base,'/api/v1/payments/shaparak/token',{'orderId':o['id'],'orderNumber':o['orderNumber'],'amount':o['payableAmount']},200)
 def callback(self,base,o,token,status=200):return self.expect(base,'/api/v1/payments/shaparak/callback',{'token':token['token'],'orderNumber':o['orderNumber'],'amount':o['payableAmount']},status)
 def wait_sql(self,q,expected,seconds=12):
  deadline=time.monotonic()+seconds
  while time.monotonic()<deadline:
   if self.sql(q)==expected:return
   time.sleep(.2)
  raise Failure('Bounded SQL outcome absent')
 def protected_snapshot(self):
  return self.sql("""SELECT md5(jsonb_build_object(
   'orders',(SELECT jsonb_agg(jsonb_build_array(id,status,payment_status,store_id,discount_code,total_amount,discount_amount,seller_discount_amount,coupon_discount_amount,payable_amount,inventory_captured) ORDER BY id) FROM orders WHERE btrim(discount_code)<>''),
   'payments',(SELECT jsonb_agg(to_jsonb(p)-ARRAY['created_at','updated_at'] ORDER BY p.id) FROM payment_transactions p JOIN orders o ON o.id=p.order_id WHERE btrim(o.discount_code)<>''),
   'inquiries',(SELECT jsonb_agg(to_jsonb(v)-ARRAY['created_at','updated_at'] ORDER BY v.id) FROM order_payment_verifications v JOIN orders o ON o.id=v.order_id WHERE btrim(o.discount_code)<>''),
   'receipts',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM order_payment_receipts r JOIN orders o ON o.id=r.order_id WHERE btrim(o.discount_code)<>''),
   'shipments',(SELECT jsonb_agg(to_jsonb(s)-ARRAY['updated_at'] ORDER BY s.id) FROM supplier_shipments s JOIN orders o ON o.id=s.order_id WHERE btrim(o.discount_code)<>''),
   'reservations',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.order_id) FROM order_coupon_reservations r),
   'count',(SELECT used_count FROM seller_discount_codes WHERE id='fin007-coupon'))::text)""")
 def execute(self):
  env={k:v for k,v in os.environ.items() if k in ('PATH','SystemRoot','SYSTEMROOT','TEMP','TMP','USERPROFILE','LOCALAPPDATA','APPDATA','HOME','GOPATH','GOCACHE')};env.update(GOOS='linux',GOARCH='amd64',CGO_ENABLED='0')
  for label,source in [('old',self.old),('new',self.new)]:
   binary=self.out/(label+'-server');self.cmd(['go','build','-o',str(binary),'./cmd/server'],cwd=source/'backend',env=env,timeout=600)
   self.manifest[label+'_binary_sha256']=digest(binary)
  self.network=self.docker('network','create','--internal','--label',LABEL+'='+self.nonce,self.name)
  self.pg=self.create('db','postgres:16-alpine',env={'POSTGRES_PASSWORD':self.password,'POSTGRES_DB':self.database},extra=['--tmpfs','/var/lib/postgresql/data'])
  for _ in range(60):
   # The entrypoint's temporary init server listens on Unix sockets only and
   # shuts down before the real server starts. Wait for the final TCP listener.
   if subprocess.run(['docker','exec',self.pg,'pg_isready','-h','127.0.0.1','-U','postgres'],capture_output=True).returncode==0:break
   time.sleep(.5)
  else:raise Failure('Disposable PostgreSQL unavailable')
  self.sql("CREATE ROLE fin007_runner LOGIN PASSWORD '"+self.password+"' NOSUPERUSER NOCREATEDB NOCREATEROLE; ALTER DATABASE "+self.database+" OWNER TO fin007_runner; GRANT ALL ON SCHEMA public TO fin007_runner; SET ROLE fin007_runner;CREATE TABLE sellersho_test_guard(singleton BOOLEAN PRIMARY KEY CHECK(singleton),database_name TEXT NOT NULL);INSERT INTO sellersho_test_guard VALUES(TRUE,'"+self.database+"');")
  self.create('cache','redis:7-alpine',['redis-server','--save','','--appendonly','no','--requirepass',self.password],extra=['--tmpfs','/data'])
  self.create('carrier','node:24-bookworm-slim',['node','/carrier.mjs'],env={'SELLERSHO_CAPACITY_RUN':self.nonce,'CAPACITY_CARRIER_KEY':self.carrier},mounts=[(self.new/'load-tests/carrier-double.mjs','/carrier.mjs')])
  self.env={'APP_ENV':'test','DATABASE_URL':f'postgres://fin007_runner:{self.password}@db:5432/{self.database}?sslmode=disable','REDIS_URL':f'redis://:{self.password}@cache:6379/0','JWT_SECRET':self.jwt,'TRACKING_WEBHOOK_SECRET':secrets.token_hex(32),'PAYMENT_GATEWAY_MODE':'mock','IPPANEL_ENABLED':'false','ENABLE_NETWORK_VOLUME_INCENTIVE':'false','PORT':'8080','MIGRATIONS_DIR':'/migrations','SELLERSHO_CAPACITY_RUN':self.nonce,'CAPACITY_PAYMENT_MODE':'success','CAPACITY_PAYMENT_LATENCIES_MS':'1','CAPACITY_CARRIER_BASE_URL':'http://carrier:8082','CAPACITY_CARRIER_KEY':self.carrier}
  old,oldbase,ready=self.server('old',self.out/'old-server',self.old)
  if not ready:raise Failure('Actual old binary failed before migration')
  self.record('actual_old_pre_migration_startup',True)
  sql=(self.new/'backend/cmd/capacity-fixture/seed.sql').read_text(encoding='utf-8').replace('generate_series(1,10000)','generate_series(1,10)').replace('generate_series(1,50000)','generate_series(1,50)').replace('generate_series(1,100000)','generate_series(1,0)')
  self.sql(sql)
  self.sql("UPDATE stores SET profit_margin_percent=20,is_margin_locked_by_admin=FALSE WHERE id='cap-store-2'")
  self.sql("UPDATE suppliers SET tapin_token='"+self.carrier+"',tapin_shop_id=replace(id,'cap-supplier-','cap-shop-');UPDATE supplier_warehouses SET tapin_province_id=1,tapin_city_id=1;INSERT INTO tapin_geo_snapshots(kind,payload) VALUES('tree','[{\"code\":1,\"title\":\"Tehran\",\"cities\":[{\"code\":1,\"title\":\"Tehran\"}]}]') ON CONFLICT(kind) DO UPDATE SET payload=excluded.payload;INSERT INTO seller_discount_codes(id,store_id,code,mode,value,starts_at,ends_at)VALUES('fin007-coupon','cap-store-2','SAVE','fixed',1000,NOW()-INTERVAL '1 day',NOW()+INTERVAL '1 day');")
  payload={'userId':str(uuid.UUID(hashlib.md5(b'cap-customer-1').hexdigest())),'mobile':'09900000001','fullName':'Synthetic FIN007 customer','role':'customer','capabilities':['customer'],'iss':'drup-platform','iat':int(time.time()),'exp':int(time.time())+3600}
  enc=lambda b:base64.urlsafe_b64encode(b).rstrip(b'=');raw=enc(b'{"alg":"HS256","typ":"JWT"}')+b'.'+enc(json.dumps(payload).encode());self.token=(raw+b'.'+enc(hmac.new(self.jwt.encode(),raw,hashlib.sha256).digest())).decode()
  payload.update(userId=str(uuid.UUID(hashlib.md5(b'cap-supplier-user-2').hexdigest())),mobile='09700000002',fullName='Synthetic FIN007 supplier',role='supplier',capabilities=['supplier'])
  raw=enc(b'{"alg":"HS256","typ":"JWT"}')+b'.'+enc(json.dumps(payload).encode());self.supplier_token=(raw+b'.'+enc(hmac.new(self.jwt.encode(),raw,hashlib.sha256).digest())).decode()
  orders={}
  for name in ('paid','pending','cancel','expiry','postcancel','posttoken','postreceipt','postexpiry','predispatch','postdispatch'):
   _,orders[name]=self.checkout(oldbase)
  self.callback(oldbase,orders['paid'],self.token_for(oldbase,orders['paid']))
  for name in ('predispatch','postdispatch'):self.callback(oldbase,orders[name],self.token_for(oldbase,orders[name]))
  preleg=self.sql("SELECT id FROM supplier_shipments WHERE order_id='"+orders['predispatch']['id']+"' AND supplier_id='cap-supplier-2'")
  postleg=self.sql("SELECT id FROM supplier_shipments WHERE order_id='"+orders['postdispatch']['id']+"' AND supplier_id='cap-supplier-2'")
  if not preleg or not postleg:raise Failure('Actual direct-dispatch fixture has no supplier leg')
  status,_=self.request(oldbase,'/api/v1/supplier/shipments/'+preleg+'/ship',{'carrier':'post','trackingNumber':'fin007-pre-direct'},token=self.supplier_token)
  if status!=200:raise Failure('Actual old pre-migration supplier dispatch failed '+str(status))
  # A retained mixed/warehouse aggregate with a direct supplier leg exercises
  # the old route's child-only UPDATE: its aggregate supplier_direct UPDATE
  # matches no row, so an order-table fence cannot supply this protection.
  self.sql("UPDATE orders SET discount_code='MISSING-DISPATCH',fulfillment_method='warehouse' WHERE id='"+orders['postdispatch']['id']+"'")
  self.record('actual_old_pre_migration_direct_dispatch',True)
  self.token_for(oldbase,orders['pending']);postreceipt=self.token_for(oldbase,orders['postreceipt'])
  original_mode=self.env['CAPACITY_PAYMENT_MODE'];self.env['CAPACITY_PAYMENT_MODE']='timeout'
  old_unknown,unknownbase,ready=self.server('old-inquiry',self.out/'old-server',self.old)
  self.env['CAPACITY_PAYMENT_MODE']=original_mode
  if not ready:raise Failure('Actual old test-only unknown adapter failed startup')
  status,_=self.request(unknownbase,'/api/v1/payments/shaparak/callback',{'token':postreceipt['token'],'orderNumber':orders['postreceipt']['orderNumber'],'amount':orders['postreceipt']['payableAmount']})
  if status<400 or self.sql("SELECT count(*) FROM order_payment_verifications WHERE order_id='"+orders['postreceipt']['id']+"'")!='1':raise Failure('Actual old unknown inquiry was not retained')
  self.inspect(old_unknown);self.docker('stop','--time','5',old_unknown)
  self.record('actual_old_pre_migration_unknown_inquiry',True)
  self.expect(oldbase,'/api/v1/orders/'+orders['cancel']['id']+'/cancel',{},200)
  self.sql("UPDATE orders SET payment_expires_at=NOW()-INTERVAL '1 second' WHERE id='"+orders['expiry']['id']+"'")
  self.wait_sql("SELECT status FROM orders WHERE id='"+orders['expiry']['id']+"'",'cancelled')
  positive=self.sql("SELECT count(*) FROM orders WHERE store_id='cap-store-2' AND btrim(discount_code)<>'' AND seller_discount_amount>0")
  if int(positive)<len(orders):raise Failure('Old fixture did not exercise positive discounted coupon orders')
  self.record('actual_old_positive_coupon_orders',int(positive))
  self.record('actual_old_pre_migration_checkout_payment_cancel_worker',True)
  # A real old checkout commits its order then blocks at its independent count
  # UPDATE. Hold that row, observe the actual old SQL wait, and attempt the full
  # canonical new migration while that old writer is retained.
  prepared=self.create('blocked-migration','alpine:latest',['/server'],env=self.env,mounts=[(self.out/'new-server','/server'),(self.new/'backend/migrations','/migrations')],extra=['--publish','127.0.0.1::8080'],start=False)
  blocker=subprocess.Popen(['docker','exec','-i',self.pg,'psql','-X','-v','ON_ERROR_STOP=1','-U','postgres','-d',self.database,'-At'],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
  blocker.stdin.write("BEGIN;SELECT id FROM seller_discount_codes WHERE id='fin007-coupon' FOR UPDATE;SELECT 'barrier';\n");blocker.stdin.flush()
  self.barrier(blocker)
  self.record('old_count_lock_barrier_ready',True)
  result=[];thread=threading.Thread(target=lambda:self.retain(lambda:self.checkout(oldbase),result));thread.start()
  self.wait_sql("SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%UPDATE seller_discount_codes%used_count%')::text",'true')
  self.record('actual_old_count_update_wait_observed',True)
  self.inspect(prepared);self.docker('start',prepared)
  for _ in range(30):
   if not self.inspect(prepared)['State']['Running']:break
   time.sleep(.1)
  else:raise Failure('Contended canonical migrator did not refuse within its bounded startup')
  if self.sql("SELECT (to_regclass('order_coupon_reservations') IS NULL)::text")!='true':raise Failure('Contended whole migration left partial schema')
  self.record('actual_old_inflight_writer_whole_migration_refused',True)
  blocker.stdin.write('COMMIT;\n');blocker.stdin.flush();blocker.communicate(timeout=10);thread.join(timeout=25)
  if thread.is_alive() or not result:raise Failure('Old in-flight checkout did not drain')
  if isinstance(result[0],Exception):raise result[0]
  new,newbase,ready=self.server('new',self.out/'new-server',self.new)
  if not ready:raise Failure('New binary failed after writer drain')
  self.record('new_binary_migration_after_drain',True)
  protected_before=self.protected_snapshot()
  status,_=self.request(oldbase,'/api/v1/supplier/shipments/'+postleg+'/ship',{'carrier':'post','trackingNumber':'fin007-post-direct'},token=self.supplier_token)
  if status!=500 or self.sql("SELECT status FROM supplier_shipments WHERE id='"+postleg+"'")!='pending':raise Failure('Actual old unresolved coupon direct dispatch escaped child fence')
  self.record('actual_old_new_direct_dispatch_fenced',status)
  self.record('loopback_internal_network_proof',{'internal':True,'configured_host_http_binding':'127.0.0.1','request_transport':'owned container loopback; no added external network','external_provider_egress':'unreachable from internal network','containers_label':LABEL})
  # The exact old process remains running across the migration. Startup failure
  # cannot stand in for these observed route and worker protections.
  body,_=self.checkout(newbase,False)
  body['discountCode']='SAVE';status,_=self.request(oldbase,'/api/v1/orders',body)
  if status<400:raise Failure('Old discounted checkout bypassed fence')
  self.record('actual_old_discounted_checkout_fenced',status)
  queued_blocker=subprocess.Popen(['docker','exec','-i',self.pg,'psql','-X','-v','ON_ERROR_STOP=1','-U','postgres','-d',self.database,'-At'],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
  queued_blocker.stdin.write("BEGIN;SELECT id FROM orders WHERE id='"+orders['postcancel']['id']+"' FOR UPDATE;SELECT 'barrier';\n");queued_blocker.stdin.flush()
  self.barrier(queued_blocker)
  queued=[];queued_thread=threading.Thread(target=lambda:self.retain(lambda:self.request(oldbase,'/api/v1/orders/'+orders['postcancel']['id']+'/cancel',{}),queued));queued_thread.start()
  self.wait_sql("SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM orders%FOR UPDATE%')::text",'true')
  queued_blocker.stdin.write('COMMIT;\n');queued_blocker.stdin.flush();queued_blocker.communicate(timeout=10);queued_thread.join(timeout=25)
  if queued_thread.is_alive() or not queued:raise Failure('Queued actual old cancellation did not finish')
  if isinstance(queued[0],Exception):raise queued[0]
  status,_=queued[0]
  self.record('actual_old_queued_after_fence_cancel_fenced',status)
  if status<400:raise Failure('Old cancellation bypassed fence')
  self.record('actual_old_cancellation_fenced',status)
  status,_=self.request(oldbase,'/api/v1/payments/shaparak/token',{'orderId':orders['posttoken']['id'],'orderNumber':orders['posttoken']['orderNumber'],'amount':orders['posttoken']['payableAmount']})
  if status<400:raise Failure('Old token insertion bypassed fence')
  self.record('actual_old_token_fenced',status)
  status,_=self.request(oldbase,'/api/v1/payments/shaparak/callback',{'token':postreceipt['token'],'orderNumber':orders['postreceipt']['orderNumber'],'amount':orders['postreceipt']['payableAmount']})
  if status<400:raise Failure('Old inquiry/receipt bypassed fence')
  self.record('actual_old_inquiry_callback_fenced',status)
  try:
   self.sql("INSERT INTO order_payment_receipts(id,verification_id,funding_kind,payment_id,order_id,customer_id,amount_toman,currency,provider_reference,verified_at) SELECT 'fin007-old-receipt-probe',id,funding_kind,payment_id,order_id,customer_id,amount_toman,currency,'fin007-old-probe',NOW() FROM order_payment_verifications WHERE order_id='"+orders['postreceipt']['id']+"'")
  except Failure as e:
   if 'FIN007-writer-required' not in str(e):raise
  else:raise Failure('Unmarked old receipt SQL bypassed fence')
  self.record('supplemental_unmarked_receipt_statement_fenced',True)

  self.inspect(new);self.docker('stop','--time','5',new)
  self.sql("UPDATE orders SET payment_expires_at=NOW()-INTERVAL '1 second' WHERE id='"+orders['postexpiry']['id']+"'")
  self.wait_sql("SELECT EXISTS(SELECT 1 FROM order_expiry_review_state WHERE order_id='"+orders['postexpiry']['id']+"')::text",'true')
  if self.sql("SELECT status FROM orders WHERE id='"+orders['postexpiry']['id']+"'")=='cancelled':raise Failure('Old expiry bypassed fence')
  self.record('actual_old_expiry_worker_fenced',True)
  self.checkout(oldbase,False);self.record('actual_old_safe_nondiscounted_checkout',True)
  self.inspect(old);self.docker('stop','--time','5',old)
  rollback,rollbackbase,ready=self.server('rollback-old',self.out/'old-server',self.new)
  if not ready:raise Failure('Actual rollback old binary could not start with retained additive migration directory')
  rollbackbody,_=self.checkout(rollbackbase,False)
  rollbackbody['discountCode']='SAVE';status,_=self.request(rollbackbase,'/api/v1/orders',rollbackbody)
  if status<400:raise Failure('Restarted old code rollback bypassed coupon fence')
  self.record('actual_restarted_old_rollback_coupon_fenced',status)
  self.record('actual_restarted_old_rollback_safe_nondiscounted',True)
  # Restricted code rollback retains the additive fence and money snapshots.
  if self.sql("SELECT count(*) FROM seller_discount_codes c WHERE used_count<>(SELECT count(*) FROM order_coupon_reservations r WHERE r.coupon_id=c.id AND state<>'released')")!='0':raise Failure('Counter mismatch after old routes/rollback')
  self.record('rollback_retained_fence_restricted_coupon_availability',True)
  protected_after=self.protected_snapshot()
  if protected_before!=protected_after:raise Failure('Old routes/worker/rollback changed protected order, money, reservation or capacity facts')
  self.record('old_routes_worker_rollback_protected_facts_unchanged',{'before_md5':protected_before,'after_md5':protected_after})
  self.inspect(rollback);self.docker('stop','--time','5',rollback)
  self.inspect(new);self.docker('start',new)
  for _ in range(60):
   if self.healthy(newbase):break
   time.sleep(.5)
  else:raise Failure('New binary restart readiness deadline exceeded')
  key='fin007-'+uuid.uuid4().hex;body,o=self.checkout(newbase,True,key)
  self.expect(newbase,'/api/v1/orders',body,200)
  self.callback(newbase,o,self.token_for(newbase,o))
  if self.sql("SELECT state FROM order_coupon_reservations WHERE order_id='"+o['id']+"'")!='consumed':raise Failure('New binary lifecycle did not consume')
  self.record('actual_new_checkout_replay_receipt_lifecycle',True)
  self.manifest['complete']=True
 def close(self):
  failures=[]
  for alias,cid in reversed(self.containers):
   try:
    self.inspect(cid)
    try:
     log=self.docker('logs','--tail','150',cid)
     (self.out/(alias+'-sanitized.log')).write_text(self.safe(log[-30000:]),encoding='utf-8')
    finally:self.docker('rm','-f','-v',cid)
   except Exception as e:failures.append(alias+': '+self.safe(str(e)))
  if self.network:
   try:self.netcheck();self.docker('network','rm',self.network)
   except Exception as e:failures.append('network: '+self.safe(str(e)))
  self.manifest['cleanup_verified']=not failures;self.manifest['cleanup_failures']=failures
  (self.out/'manifest.json').write_text(json.dumps(self.manifest,indent=2),encoding='utf-8')
  if failures:raise Failure('Owned disposable cleanup failed; see manifest')
def main():
 p=argparse.ArgumentParser();p.add_argument('--old-source',required=True);p.add_argument('--new-source',required=True);p.add_argument('--output',required=True);args=p.parse_args();run=Run(args)
 try:run.execute()
 except Exception as e:run.manifest['failure']=run.safe(str(e));print(run.manifest['failure'],file=sys.stderr);return 1
 finally:run.close()
 return 0
if __name__=='__main__':sys.exit(main())
