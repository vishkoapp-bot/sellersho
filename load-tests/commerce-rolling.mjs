// SHORT LOCAL exact-mix probe. Full sustained/soak requires a rolling feeder:
// production payment holds expire after 15 minutes; never extend them to fake validity.
import https from 'node:https';
import {LifecycleQueues} from './lifecycle-queues.mjs';
import {arrivalPlan,ArrivalScheduler,arrivalSummary} from './arrival-plan.mjs';
import fs from 'node:fs';
import crypto from 'node:crypto';
import {performance} from 'node:perf_hooks';
import {resolveTarget,loadSessions,loadCa} from './target-config.mjs';
const capTarget=resolveTarget();
const fixture={users:loadSessions(capTarget),staff:(()=>{try{return JSON.parse(fs.readFileSync(capTarget.sessionsFile)).staff||[]}catch{return []}})()};
const staffVariant=process.env.CAPACITY_STAFF==='1';
if(staffVariant&&fixture.staff?.length!==20)throw Error('20 independent authorized staff sessions required');
const users=staffVariant?[...fixture.staff,...fixture.users.slice(20)]:fixture.users;

if(users.length!==1000||new Set(users.map(u=>u.id)).size!==1000||new Set(users.map(u=>u.token)).size!==1000)throw Error('1000 independent sessions required');
const agent=new https.Agent({keepAlive:true,maxSockets:1000,ca:loadCa(capTarget),rejectUnauthorized:true});
import {CLASSES as classes,WEIGHTS as weights} from './workload.mjs';
const droppedSlots=[];let scheduler;
const requests=[],helpers=[],rows=[],identities=new Set();let active=0,dropped=0,start=0;
function call(row,phase){return new Promise(resolve=>{
 let done=false;const begun=performance.now();const finish=(status,body)=>{if(done)return;done=true;
  const record={ordinal:row.ordinal,principal:row.principal,route_class:row.kind,status,scheduled_ms:row.arrival?.scheduled_ms??null,arrival_lag_ms:row.arrival?begun-start-row.arrival.scheduled_ms:null,started_ms:phase==='measured'?begun-start:null,latency_ms:performance.now()-begun};
  (phase==='measured'?requests:helpers).push(record);resolve({status,body,record});
 };
 const req=https.request({hostname:capTarget.ip||capTarget.host,port:capTarget.port,servername:capTarget.host,path:row.path,method:row.body?'POST':'GET',agent,timeout:5000,
  headers:{'Content-Type':'application/json',Host:capTarget.host,Authorization:'Bearer '+users[row.principal].token,...(row.body?.idempotencyKey?{'Idempotency-Key':row.body.idempotencyKey}:{})}},res=>{
   let text='';res.on('data',b=>{if(text.length<262144)text+=b});res.on('end',()=>{let body;try{body=JSON.parse(text)}catch{}finish(res.statusCode,body)});res.on('error',()=>finish(0));
  });req.on('timeout',()=>req.destroy());req.on('error',()=>finish(0));req.end(row.body?JSON.stringify(row.body):undefined);
})}
async function helper(row,status){const result=await call(row,'prepare');if(result.status!==status)throw Error('fixture preparation '+row.kind+' expected '+status+' got '+result.status);return result.body}
function uuid(text){const h=crypto.createHash('md5').update(text).digest('hex');return h.slice(0,8)+'-'+h.slice(8,12)+'-'+h.slice(12,16)+'-'+h.slice(16,20)+'-'+h.slice(20)}
const duration=Number(process.env.CAPACITY_DURATION_SECONDS||30);
if(![30,120,300,1800,7200].includes(duration))throw Error('duration must match declared local diagnostic,recovery,sustained orsoak profile');
const plan=arrivalPlan(duration,Number(process.env.CAPACITY_API_RPS||100));
const generatorStarted=performance.now();
const queues=new LifecycleQueues(users,Date.now,2000),target=plan.offered;
let completed=0,fullCompleted=0,recoveryCalls=0,wrongAmount=0,quoteSerial=0,readSerial=0,staffSerial=0,fatal=null;
const summary={certified:false,staff_variant:staffVariant,staff_mix_rationale:staffVariant?'Additional variant:15RPS account slots become seller/supplier balances,warehouse overview and admin revenue; every other staff account slot is a PERF-02 report leg (seller report, supplier orders page, reconciliation summary, ledger filtered, statement page);980customers+20staff=1000distinct principals;recovery remains within same budget':null,scope:'LOCAL rolling100API RPS commerce measurements; full financial/refund oracle incomplete',duration_seconds:duration,offered:target,failed_gates:[],requests_raw:requests,helper_requests_raw:helpers,dropped_slots_raw:droppedSlots};
function cart(principal,serial){
 if(staffVariant)principal=20+principal%980;
 const product=1+((serial*7919)%10000),store=1+(product%100),user=users[principal],logicalId='cap-'+crypto.randomUUID();
 const item={productId:'cap-product-'+product,variantId:uuid('cap-sku-'+((product-1)*5+1)),quantity:1};
 const destination={province:'Tehran',city:'Tehran',address:'Synthetic capacity address',postalCode:'1111111111'};
 return {principal,userId:user.id,logicalId,kind:'quote',path:'/api/v1/shipping/quote',body:{items:[item],destination},storeId:'cap-store-'+store,
  orderBody:{storeId:'cap-store-'+store,items:[item],customerInfo:{...destination,fullName:'Synthetic capacity customer',phone:user.mobile},paymentMethod:'cash',idempotencyKey:logicalId}};
}
function transition(stage,row,body){
 if(stage==='quote'){
  if(!body?.quoteId||!Number.isSafeInteger(body.totalShipping)||body.totalShipping<0)throw Error('invalid quote output');
  queues.push('create',{...row,kind:'create',path:'/api/v1/orders',body:{...row.orderBody,shippingQuoteId:body.quoteId},expiresAt:Date.now()+120000});
 }else if(stage==='create'){
  if(!body?.id||!Number.isSafeInteger(body.payableAmount)||body.payableAmount<=0||body.storeId!==row.storeId)throw Error('invalid or cross-store order output');
  const expiresAt=Date.now()+840000;
  queues.push('initiate',{...row,kind:'initiate',path:'/api/v1/payments/shaparak/token',orderId:body.id,orderNumber:body.orderNumber,amount:body.payableAmount,orderExpiresAt:expiresAt,expiresAt,
   body:{orderId:body.id,orderNumber:body.orderNumber,amount:body.payableAmount}});
 }else if(stage==='initiate'){
  if(!body?.token||body.amount!==row.amount){wrongAmount++;throw Error('token/persisted-order amount mismatch')}
  queues.push('callback',{...row,kind:'callback',path:'/api/v1/payments/shaparak/callback',expiresAt:Math.min(row.orderExpiresAt,Date.now()+840000),
   body:{token:body.token,orderNumber:row.orderNumber,amount:row.amount}});
 }else if(stage==='callback'){
  if(body?.status!=='paid'||body.orderNumber!==row.orderNumber||body.success!==true)throw Error('callback did not confirm expected order');completed++;if(!row.bootstrap)fullCompleted++;
 }
}
function pendingFor(stage){return queues.recovery().find(p=>p.stage===stage&&!p.item.recovering&&(p.item.recoveryAttempts||0)<1&&p.item.expiresAt>Date.now())}
async function measured(kind,ordinal,arrival){
 let row,recovery=null;
 if(['create','initiate'].includes(kind)){
  recovery=pendingFor(kind);
  if(recovery){row=recovery.item;row.recovering=true;row.recoveryAttempts=(row.recoveryAttempts||0)+1;recoveryCalls++;}
  else row=queues.take(kind);
 }else if(kind==='callback')row=queues.take(kind);
 else if(kind==='quote'){row=cart(quoteSerial%1000,quoteSerial++);row.forCheckout=quoteSerial%2===0;}
 else{
  const principal=staffVariant?20+readSerial++%980:readSerial++%1000,user=users[principal],product=1+((ordinal*7919)%10000),store=1+(product%100);
  row={principal,userId:user.id,kind,path:kind==='list'?'/api/v1/products?limit=24':kind==='detail'?'/api/v1/products/cap-product-'+product:kind==='store'?'/api/v1/stores/cap-store-'+store:'/api/v1/auth/me'};
  if(kind==='account'){
   if(staffVariant){
    const serial=staffSerial++,principal=serial%20,user=users[principal],period=new Date().toISOString().slice(0,7);
    const paths={seller:'/api/v1/stores/'+user.storeId+'/balance',supplier:'/api/v1/supplier/settlement/balance',warehouse_staff:'/api/v1/warehouse/overview',admin:'/api/v1/finance/reports/revenue?period='+period};
    if(!paths[user.role])throw Error('unsupported declared staff role');
    // PERF-02 Step 12: every other staff account slot is a report leg so
    // the 100-RPS mix exercises the bounded report paths at their agreed
    // share (~7 RPS). Roles gate the legs: seller report needs OwnedStore;
    // reconciliation/ledger/statement need the admin role; supplier orders
    // is the supplier's own bounded list. warehouse_staff keeps overview.
    const reportLegs={seller:['seller-report'],supplier:['supplier-orders'],warehouse_staff:[],admin:['reconciliation','ledger','statement']};
    const reportPaths={'seller-report':'/api/v1/stores/'+user.storeId+'/report','supplier-orders':'/api/v1/supplier/orders?limit=50','reconciliation':'/api/v1/warehouse/inventory/reconciliation-report?view=summary&limit=50','ledger':'/api/v1/finance/ledger?limit=50','statement':'/api/v1/finance/stores/cap-store-1/statement?limit=50'};
    const legs=reportLegs[user.role]||[];
    let reportLeg=null,path=paths[user.role];
    if(legs.length&&serial%2===1){reportLeg=legs[Math.floor(serial/2)%legs.length];path=reportPaths[reportLeg];}
    row={principal,userId:user.id,kind,path,staffRole:user.role,storeId:user.storeId,period,reportLeg};
   }
   recovery=pendingFor('callback');
   if(recovery){recovery.item.recovering=true;recovery.item.recoveryAttempts=(recovery.item.recoveryAttempts||0)+1;row={...recovery.item,kind:'account',path:'/api/v1/orders',body:undefined};recoveryCalls++;}
  }
 }
 if(!row){dropped++;droppedSlots.push({...arrival,reason:'missing_lifecycle_stage'});return}
 row={...row,ordinal,kind,arrival};identities.add(users[row.principal].id);
 const {status,body,record}=await call(row,'measured');record.recovery=!!recovery;record.report_leg=row.reportLeg||null;
 const accepted=kind==='create'?(status===201||(!!recovery&&status===200)):status===200;
 record.assertion_ok=accepted;
 try{
  if(recovery&&kind==='account'){
   const own=Array.isArray(body)&&body.every(o=>o.customerUserId===row.userId),found=own&&body.find(o=>o.id===row.orderId);
   record.assertion_ok=accepted&&!!found&&found.paymentStatus==='paid';
   if(record.assertion_ok){queues.resolve('callback',row.logicalId);completed++;if(!row.bootstrap)fullCompleted++;}
  }else if(accepted){
   if(kind==='account'){
    record.staff_role=row.staffRole||null;
    if(row.reportLeg){
     record.assertion_ok=row.reportLeg==='seller-report'?Number.isSafeInteger(body?.revenue)&&Number.isSafeInteger(body?.orders):row.reportLeg==='supplier-orders'?Array.isArray(body?.orders):row.reportLeg==='reconciliation'?Array.isArray(body?.rows)&&Number.isSafeInteger(body?.total):row.reportLeg==='ledger'?Array.isArray(body?.entries):row.reportLeg==='statement'?Array.isArray(body?.entries)&&Number.isSafeInteger(body?.totalEntries):false;
    }else{
     record.assertion_ok=row.staffRole==='seller'?body?.storeId===row.storeId&&Number.isSafeInteger(body.availableAmount):row.staffRole==='supplier'?Number.isSafeInteger(body?.available)&&Number.isSafeInteger(body?.earned):row.staffRole==='warehouse_staff'?Number.isSafeInteger(body?.inboundPending):row.staffRole==='admin'?body?.period===row.period&&Number.isSafeInteger(body?.totalRevenue):body?.userId===row.userId;
    }
   }
   else if(kind==='quote'){record.assertion_ok=body?.success===true;if(record.assertion_ok&&row.forCheckout)transition(kind,row,body);}
   else if(['create','initiate','callback'].includes(kind)){transition(kind,row,body);if(recovery)queues.resolve(kind,row.logicalId);}
  }else if((status===0||status>=500)&&['create','initiate','callback'].includes(kind)&&!recovery)queues.ambiguous(kind,row);
 }catch(error){record.assertion_ok=false;record.invariant_failure=error.message;fatal=error.message;}
 finally{if(recovery)recovery.item.recovering=false;}
}
try{
 // Only bootstrap performs feeder requests. Steady-state transitions happen on
 // scheduled quote/create/initiate/callback slots; each slot makes one API call.
 for(let i=0;i<120;i++){
  const row={...cart(i%1000,100000+i),bootstrap:true},quote=await helper({...row,kind:'bootstrap_quote'},200);
  const create={...row,kind:'create',path:'/api/v1/orders',body:{...row.orderBody,shippingQuoteId:quote.quoteId},expiresAt:Date.now()+120000};
  if(i<40){queues.push('create',create);continue}
  const order=await helper({...create,kind:'bootstrap_order'},201),expiresAt=Date.now()+840000;
  if(!order?.id||!Number.isSafeInteger(order.payableAmount)||order.payableAmount<=0)throw Error('bootstrap order invalid');
  const initiate={...row,kind:'initiate',path:'/api/v1/payments/shaparak/token',orderId:order.id,orderNumber:order.orderNumber,amount:order.payableAmount,orderExpiresAt:expiresAt,expiresAt,
   body:{orderId:order.id,orderNumber:order.orderNumber,amount:order.payableAmount}};
  if(i<80){queues.push('initiate',initiate);continue}
  const token=await helper({...initiate,kind:'bootstrap_token'},200);
  if(!token?.token||token.amount!==order.payableAmount)throw Error('bootstrap amount mismatch');
  queues.push('callback',{...initiate,kind:'callback',path:'/api/v1/payments/shaparak/callback',body:{token:token.token,orderNumber:order.orderNumber,amount:order.payableAmount}});
 }
 summary.bootstrap_api_calls=helpers.length;start=performance.now();summary.measurement_start_offset_seconds=(start-generatorStarted)/1000;
 scheduler=new ArrivalScheduler({total:target,rps:plan.rps,start,clock:()=>performance.now()});
 await new Promise(resolve=>{const timer=setInterval(()=>{
  scheduler.drain(arrival=>{const ordinal=arrival.ordinal;let slot=ordinal%100,k=0;while(slot>=weights[k])slot-=weights[k++];
   if(arrival.missed_schedule||active>=1000||fatal){dropped++;droppedSlots.push({...arrival,reason:arrival.missed_schedule?'missed_schedule':fatal?'fatal':'active_limit'});return}active++;
   measured(classes[k],ordinal,arrival).catch(error=>{fatal=error.message}).finally(()=>active--);
  });
  if(scheduler.processed===target&&active===0){clearInterval(timer);resolve()}
 },2)});
 summary.requests=requests.length;summary.elapsed_seconds=(performance.now()-start)/1000;
 const completedDuringMeasurement=fullCompleted;
 // One bounded post-run read per owner; separate helper accounting, no mutation
 // retries outside the declared API budget and no invented provider inquiry.
 let postConfirmed=0;const owners=new Map();
 for(const pending of queues.recovery())if(pending.stage==='callback')owners.set(pending.item.principal,pending.item.userId);
 for(const [principal,userId] of owners){
  const result=await call({ordinal:-1,principal,kind:'post_recovery_account',path:'/api/v1/orders'},'recovery');
  if(result.status!==200||!Array.isArray(result.body)||result.body.some(o=>o.customerUserId!==userId))continue;
  for(const pending of queues.recovery().filter(p=>p.stage==='callback'&&p.item.principal===principal)){
   const found=result.body.find(o=>o.id===pending.item.orderId);
   if(found?.paymentStatus==='paid'&&found.payableAmount===pending.item.amount){queues.resolve('callback',pending.item.logicalId);postConfirmed++;}
  }
 }
 summary.post_recovery_api_calls=owners.size;summary.post_recovery_confirmed_payments=postConfirmed;
summary.distinct_principals=identities.size;summary.dropped_iterations=dropped;
 summary.completed_payment_outcomes=completed;summary.completed_checkouts=fullCompleted;summary.actual_checkout_rps=completedDuringMeasurement/summary.elapsed_seconds;summary.recovery_api_calls=recoveryCalls;summary.wrong_amount=wrongAmount;summary.lifecycle=queues.snapshot();summary.route_metrics={};
 for(const kind of classes){const rs=requests.filter(r=>r.route_class===kind),ts=rs.map(r=>r.latency_ms).sort((a,b)=>a-b),read=classes.indexOf(kind)<4;
  const m={requests:rs.length,failed_assertions:rs.filter(r=>!r.assertion_ok).length,p95_ms:ts[Math.ceil(ts.length*.95)-1],p99_ms:ts[Math.ceil(ts.length*.99)-1]};summary.route_metrics[kind]=m;
  if(rs.length!==plan.counts[classes.indexOf(kind)]||m.failed_assertions||!ts.length||m.p95_ms>(read?300:750)||m.p99_ms>(read?1000:2000))summary.failed_gates.push(kind);
 }
 // PERF-02 Step 12: per-report-leg latencies against the read gates
 // (p95 300 ms / p99 1000 ms). Only in the staff variant — other profiles
 // never run report legs, so gating them there would fail spuriously.
 // A leg with zero requests fails: the leg never ran, so the report
 // profile was not exercised.
 summary.report_metrics={};
 if(staffVariant){for(const leg of ['seller-report','supplier-orders','reconciliation','ledger','statement']){const rs=requests.filter(r=>r.report_leg===leg),ts=rs.map(r=>r.latency_ms).sort((a,b)=>a-b);
  const m={requests:rs.length,failed_assertions:rs.filter(r=>!r.assertion_ok).length,p95_ms:ts.length?ts[Math.ceil(ts.length*.95)-1]:null,p99_ms:ts.length?ts[Math.ceil(ts.length*.99)-1]:null};summary.report_metrics[leg]=m;
  if(!rs.length||m.failed_assertions||m.p95_ms>300||m.p99_ms>1000)summary.failed_gates.push('report_'+leg);
 }}
 if(dropped||requests.length!==target||identities.size!==1000)summary.failed_gates.push('arrivals_or_identities');
 if(fatal||wrongAmount||queues.stats.expired||queues.stats.overflow||queues.stats.missing||queues.stats.ambiguous)summary.failed_gates.push('lifecycle_or_ambiguous_response');
 if(fatal)summary.failure=fatal;
 summary.arrival_plan=plan;
 Object.assign(summary,arrivalSummary(scheduler,requests,summary.elapsed_seconds));
 if(summary.scheduling_gate!=='matched')summary.failed_gates.push('arrival_schedule');
 summary.amount_oracle_scope='token/persisted-order consistency; full independent price/ledger/refund oracle remains OPEN';
}catch(error){summary.failed_gates.push('bootstrap');summary.failure=error.message;summary.requests=requests.length;}
finally{agent.destroy();fs.writeFileSync(process.env.CAPACITY_SUMMARY_FILE||'/tmp/summary.json',JSON.stringify(summary,null,2));process.exitCode=summary.failed_gates.length?1:0;}
