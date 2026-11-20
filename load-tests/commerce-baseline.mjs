// SHORT LOCAL exact-mix probe. Full sustained/soak requires a rolling feeder:
// production payment holds expire after 15 minutes; never extend them to fake validity.
import https from 'node:https';
import {ArrivalScheduler,arrivalSummary} from './arrival-plan.mjs';
import fs from 'node:fs';
import crypto from 'node:crypto';
import {performance} from 'node:perf_hooks';
import {resolveTarget,loadSessions,loadCa} from './target-config.mjs';
const capTarget=resolveTarget();
const users=loadSessions(capTarget);
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
const summary={certified:false,scope:'SHORT LOCAL exact100API RPS commerce mix,10seconds',offered:1000,failed_gates:[],requests_raw:requests,helper_requests_raw:helpers,dropped_slots_raw:droppedSlots};
try{
 for(let i=0;i<1000;i++){
  let slot=i%100,k=0;while(slot>=weights[k])slot-=weights[k++];const kind=classes[k],principal=i;
  const product=1+((i*7919)%10000),store=1+(product%100),user=users[principal];
  const item={productId:'cap-product-'+product,variantId:uuid('cap-sku-'+((product-1)*5+1)),quantity:1};
  const destination={province:'Tehran',city:'Tehran',address:'Synthetic capacity address',postalCode:'1111111111'};
  const row={ordinal:i,principal,kind,status:200};
  if(kind==='list')row.path='/api/v1/products?limit=24'; // PERF-01 cursor paging: bare page=2..20 is a 400; first page via limit=24 (deep cursor-walking is a future extension)
  if(kind==='detail')row.path='/api/v1/products/cap-product-'+product;
  if(kind==='store')row.path='/api/v1/stores/cap-store-'+store;
  if(kind==='account'){row.path='/api/v1/auth/me';row.expected={userId:user.id};}
  if(kind==='quote'){row.path='/api/v1/shipping/quote';row.body={items:[item],destination};row.expected={success:true};}
  if(['create','initiate','callback'].includes(kind)){
   const quote=await helper({...row,kind:'feeder_quote',path:'/api/v1/shipping/quote',body:{items:[item],destination}},200);
   if(!quote?.quoteId||!Number.isSafeInteger(quote.totalShipping)||quote.totalShipping<0)throw Error('invalid fixture quote');
   const orderBody={storeId:'cap-store-'+store,items:[item],customerInfo:{...destination,fullName:'Synthetic capacity customer',phone:user.mobile},paymentMethod:'cash',shippingQuoteId:quote.quoteId,idempotencyKey:'cap-'+crypto.randomUUID()};
   if(kind==='create'){row.path='/api/v1/orders';row.body=orderBody;row.status=201;row.expected={storeId:'cap-store-'+store,shippingQuoteId:quote.quoteId};}
   else{
    const order=await helper({...row,kind:'feeder_order',path:'/api/v1/orders',body:orderBody},201);
    if(!order?.id||!Number.isSafeInteger(order.payableAmount)||order.payableAmount<=0)throw Error('invalid fixture order amount');
    const tokenBody={orderId:order.id,orderNumber:order.orderNumber,amount:order.payableAmount};
    if(kind==='initiate'){row.path='/api/v1/payments/shaparak/token';row.body=tokenBody;row.expected={amount:order.payableAmount};}
    else{
     const token=await helper({...row,kind:'feeder_token',path:'/api/v1/payments/shaparak/token',body:tokenBody},200);
     if(!token?.token||token.amount!==order.payableAmount)throw Error('fixture token amount mismatch');
     row.path='/api/v1/payments/shaparak/callback';row.body={token:token.token,orderNumber:order.orderNumber,amount:order.payableAmount};row.expected={success:true,status:'paid',orderNumber:order.orderNumber};
    }
   }
  }
  rows.push(row);
 }
 summary.prepared_rows=rows.length;summary.preparation_helper_calls=helpers.length;
 // Separate reports expose feeder calls; they are never hidden inside measured iterations.
 start=performance.now();scheduler=new ArrivalScheduler({total:1000,rps:100,start,clock:()=>performance.now()});
 await new Promise(resolve=>{const timer=setInterval(()=>{
  scheduler.drain(arrival=>{const row={...rows[arrival.ordinal],arrival};
   if(arrival.missed_schedule||active>=1000){dropped++;droppedSlots.push({...arrival,reason:arrival.missed_schedule?'missed_schedule':'active_limit'});return}
   active++;identities.add(users[row.principal].id);
   call(row,'measured').then(({status,body,record})=>{
    record.assertion_ok=status===row.status&&Object.entries(row.expected||{}).every(([key,value])=>body?.[key]===value);
   }).finally(()=>active--);
  });
  if(scheduler.processed===1000&&active===0){clearInterval(timer);resolve()}
 },2)});
 summary.elapsed_seconds=(performance.now()-start)/1000;summary.requests=requests.length;summary.distinct_principals=identities.size;summary.dropped_iterations=dropped;
 summary.route_metrics={};
 for(const kind of classes){const rs=requests.filter(r=>r.route_class===kind),ts=rs.map(r=>r.latency_ms).sort((a,b)=>a-b),read=classes.indexOf(kind)<4;
  const metrics={requests:rs.length,failed_assertions:rs.filter(r=>!r.assertion_ok).length,p95_ms:ts[Math.ceil(ts.length*.95)-1],p99_ms:ts[Math.ceil(ts.length*.99)-1]};summary.route_metrics[kind]=metrics;
  if(rs.length!==weights[classes.indexOf(kind)]*10||metrics.failed_assertions||metrics.p95_ms>(read?300:750)||metrics.p99_ms>(read?1000:2000))summary.failed_gates.push(kind);
 }
 if(dropped||requests.length!==1000||identities.size!==1000)summary.failed_gates.push('arrivals_or_identities');
 Object.assign(summary,arrivalSummary(scheduler,requests,summary.elapsed_seconds));
 if(summary.scheduling_gate!=='matched')summary.failed_gates.push('arrival_schedule');
 summary.amount_oracle_scope='Token amount must equal persisted order payable; independent pricing-policy oracle remains required';
}catch(error){summary.failed_gates.push('fixture_preparation');summary.failure=error.message;summary.requests=requests.length;}
finally{agent.destroy();fs.writeFileSync(process.env.CAPACITY_SUMMARY_FILE||'/tmp/summary.json',JSON.stringify(summary,null,2));process.exitCode=summary.failed_gates.length?1:0;}
