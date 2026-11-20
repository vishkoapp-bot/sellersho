// SHORT LOCAL exact-mix probe. Full sustained/soak requires a rolling feeder:
// production payment holds expire after 15 minutes; never extend them to fake validity.
import https from 'node:https';
import fs from 'node:fs';
import crypto from 'node:crypto';
import {performance} from 'node:perf_hooks';
import {resolveTarget,loadSessions,loadCa} from './target-config.mjs';
const capTarget=resolveTarget();
const users=loadSessions(capTarget);
if(users.length!==1000||new Set(users.map(u=>u.id)).size!==1000||new Set(users.map(u=>u.token)).size!==1000)throw Error('1000 independent sessions required');
const agent=new https.Agent({keepAlive:true,maxSockets:1000,ca:loadCa(capTarget),rejectUnauthorized:true});
import {CLASSES as classes,WEIGHTS as weights} from './workload.mjs';
const requests=[],helpers=[],rows=[],identities=new Set();let active=0,dropped=0,start=0;
function call(row,phase){return new Promise(resolve=>{
 let done=false;const begun=performance.now();const finish=(status,body)=>{if(done)return;done=true;
  const record={ordinal:row.ordinal,principal:row.principal,route_class:row.kind,status,started_ms:phase==='measured'?begun-start:null,latency_ms:performance.now()-begun};
  (phase==='measured'?requests:helpers).push(record);resolve({status,body,record});
 };
 const req=https.request({hostname:capTarget.ip||capTarget.host,port:capTarget.port,servername:capTarget.host,path:row.path,method:row.body?'POST':'GET',agent,timeout:5000,
  headers:{'Content-Type':'application/json',Host:capTarget.host,Authorization:'Bearer '+users[row.principal].token,...(row.body?.idempotencyKey?{'Idempotency-Key':row.body.idempotencyKey}:{})}},res=>{
   let text='';res.on('data',b=>{if(text.length<262144)text+=b});res.on('end',()=>{let body;try{body=JSON.parse(text)}catch{}finish(res.statusCode,body)});res.on('error',()=>finish(0));
  });req.on('timeout',()=>req.destroy());req.on('error',()=>finish(0));req.end(row.body?JSON.stringify(row.body):undefined);
})}
async function helper(row,status){const result=await call(row,'prepare');if(result.status!==status)throw Error('fixture preparation '+row.kind+' expected '+status+' got '+result.status);return result.body}
function uuid(text){const h=crypto.createHash('md5').update(text).digest('hex');return h.slice(0,8)+'-'+h.slice(8,12)+'-'+h.slice(12,16)+'-'+h.slice(16,20)+'-'+h.slice(20)}
const duration=Number(process.env.CAPACITY_DURATION_SECONDS||90);
if(![90,1800,7200].includes(duration))throw Error('unsupported paced-journey duration');
const until=performance.now()+duration*1000,ambiguous=new Map();let sequence=0,completed=0,failed=0,wrongAmount=0,peak=0;
const delay=ms=>new Promise(resolve=>setTimeout(resolve,ms));
async function journey(principal){
 const user=users[principal];let cycle=0,readOnly=false;
 await delay((principal*7919)%10000);
 while(performance.now()<until){
  const product=1+((principal*37+cycle*7919)%10000),store=1+(product%100),logical='cap-'+crypto.randomUUID();
  const item={productId:'cap-product-'+product,variantId:uuid('cap-sku-'+((product-1)*5+1)),quantity:1};
  const destination={province:'Tehran',city:'Tehran',address:'Synthetic capacity address',postalCode:'1111111111'};
  let quote,order,token;
  for(const kind of (readOnly?['list','detail','store','account']:classes)){
   if(performance.now()>=until)break;
   const row={ordinal:sequence++,principal,kind};
   if(kind==='list')row.path='/api/v1/products?limit=24';
   if(kind==='detail')row.path='/api/v1/products/cap-product-'+product;
   if(kind==='store')row.path='/api/v1/stores/cap-store-'+store;
   if(kind==='account')row.path='/api/v1/auth/me';
   if(kind==='quote'){row.path='/api/v1/shipping/quote';row.body={items:[item],destination};}
   if(kind==='create'){row.path='/api/v1/orders';row.body={storeId:'cap-store-'+store,items:[item],customerInfo:{...destination,fullName:'Synthetic capacity customer',phone:user.mobile},paymentMethod:'cash',shippingQuoteId:quote.quoteId,idempotencyKey:logical};}
   if(kind==='initiate'){row.path='/api/v1/payments/shaparak/token';row.body={orderId:order.id,orderNumber:order.orderNumber,amount:order.payableAmount};}
   if(kind==='callback'){row.path='/api/v1/payments/shaparak/callback';row.body={token:token.token,orderNumber:order.orderNumber,amount:order.payableAmount};}
   identities.add(user.id);active++;peak=Math.max(peak,active);
   const result=await call(row,'measured');active--;
   let ok=result.status===(kind==='create'?201:200);
   if(ok&&kind==='account')ok=result.body?.userId===user.id;
   if(ok&&kind==='quote'){quote=result.body;ok=!!quote?.quoteId&&Number.isSafeInteger(quote.totalShipping);}
   if(ok&&kind==='create'){order=result.body;ok=!!order?.id&&order.storeId==='cap-store-'+store&&Number.isSafeInteger(order.payableAmount)&&order.payableAmount>0;}
   if(ok&&kind==='initiate'){token=result.body;ok=!!token?.token&&token.amount===order.payableAmount;if(!ok)wrongAmount++;}
   if(ok&&kind==='callback'){ok=result.body?.status==='paid'&&result.body?.orderNumber===order.orderNumber;if(ok)completed++;}
   result.record.assertion_ok=ok;
   await delay(5000+((principal*43+sequence*97)%10000));
   if(!ok){failed++;if(['create','initiate','callback'].includes(kind)&&(result.status===0||result.status>=500)){ambiguous.set(user.id,{logical,kind});readOnly=true;}break}
   // Think time also applies to failures; sockets can be shared, state cannot.
  }
  cycle++;await delay(1000);
 }
}
start=performance.now();
await Promise.all(users.map((_,i)=>journey(i)));
agent.destroy();
const elapsed=(performance.now()-start)/1000,routeMetrics={},gates=[];
for(const kind of classes){const rs=requests.filter(r=>r.route_class===kind),ts=rs.map(r=>r.latency_ms).sort((a,b)=>a-b),read=classes.indexOf(kind)<4;
 routeMetrics[kind]={requests:rs.length,failed_assertions:rs.filter(r=>!r.assertion_ok).length,p95_ms:ts[Math.ceil(ts.length*.95)-1],p99_ms:ts[Math.ceil(ts.length*.99)-1]};
 const m=routeMetrics[kind];if(!ts.length||m.failed_assertions||m.p95_ms>(read?300:750)||m.p99_ms>(read?1000:2000))gates.push(kind);
}
if(identities.size!==1000||ambiguous.size||wrongAmount||failed)gates.push('identities_or_business_assertions');
const summary={certified:false,scope:'LOCAL closed-loop1000independent complete checkout journeys; actual throughput only',duration_seconds:duration,
 requests:requests.length,distinct_principals:identities.size,elapsed_seconds:elapsed,actual_api_rps:requests.length/elapsed,
 completed_checkouts:completed,actual_checkout_rps:completed/elapsed,peak_inflight:peak,retained_sessions:users.length,retained_ambiguous:ambiguous.size,
 degraded_readonly_sessions:ambiguous.size,wrong_amount:wrongAmount,failed_gates:gates,route_metrics:routeMetrics,requests_raw:requests,
 note:'Errors remain failed; ambiguous mutations retain bounded per-user state and stop further writes for that user, never hidden retry. Full pricing/ledger/refund/device/browser gates remain open.'};
fs.writeFileSync(process.env.CAPACITY_SUMMARY_FILE||'/tmp/summary.json',JSON.stringify(summary,null,2));process.exitCode=gates.length?1:0;
