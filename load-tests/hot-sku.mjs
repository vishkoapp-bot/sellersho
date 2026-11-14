// Focused100-customer contention profile, not the1000-user qualification.
import https from 'node:https';
import fs from 'node:fs';
import crypto from 'node:crypto';
import {performance} from 'node:perf_hooks';
import {evaluateHotSKU} from './hot-sku-policy.mjs';
const users=JSON.parse(fs.readFileSync('/sessions/users.json')).users;
if(users.length!==1000||new Set(users.map(u=>u.id)).size!==1000)throw Error('independent full session fixture required');
const agent=new https.Agent({keepAlive:true,maxSockets:100,ca:fs.readFileSync('/trust/root.crt'),rejectUnauthorized:true});
const requests=[],helpers=[],summary={certified:false,scope:'LOCAL focused100-customer hot SKU; five units',contenders:100,paid:0,conflicts:0,wrongAmount:0,failures:0,duplicateCallbacks:0,failed_gates:[],requests_raw:requests,helper_requests_raw:helpers};
function call(principal,kind,path,body,helper=false){return new Promise(resolve=>{
 let done=false;const start=performance.now();
 const finish=(status,data)=>{if(done)return;done=true;(helper?helpers:requests).push({principal,route_class:kind,status,latency_ms:performance.now()-start});resolve({status,body:data})};
 const req=https.request({hostname:'capacity.example.test',path,method:body?'POST':'GET',agent,timeout:10000,headers:{Authorization:'Bearer '+users[principal].token,'Content-Type':'application/json',...(body?.idempotencyKey?{'Idempotency-Key':body.idempotencyKey}:{})}},res=>{
  let raw='';res.on('data',b=>{if(raw.length<262144)raw+=b});res.on('end',()=>{let data;try{data=JSON.parse(raw)}catch{}finish(res.statusCode,data)});res.on('error',()=>finish(0));
 });req.on('timeout',()=>req.destroy());req.on('error',()=>finish(0));req.end(body?JSON.stringify(body):undefined);
})}
const orders=[];const h=crypto.createHash('md5').update('cap-hot-sku').digest('hex'),variant=h.slice(0,8)+'-'+h.slice(8,12)+'-'+h.slice(12,16)+'-'+h.slice(16,20)+'-'+h.slice(20);
try{
 for(let principal=0;principal<100;principal++){
  const items=[{productId:'cap-hot-product',variantId:variant,quantity:1}],destination={province:'Tehran',city:'Tehran',address:'Synthetic hot SKU address',postalCode:'1111111111'};
  const quote=await call(principal,'prepare_quote','/api/v1/shipping/quote',{items,destination},true);
  if(quote.status!==200||!quote.body?.quoteId)throw Error('valid hot SKU quote preparation failed');
  const order=await call(principal,'prepare_order','/api/v1/orders',{storeId:'cap-store-100',items,shippingQuoteId:quote.body.quoteId,customerInfo:{...destination,fullName:'Synthetic capacity customer',phone:users[principal].mobile},paymentMethod:'cash',idempotencyKey:'cap-hot-'+crypto.randomUUID()},true);
  if(order.status!==201||!order.body?.id||order.body.storeId!=='cap-store-100'||!Number.isSafeInteger(order.body.payableAmount))throw Error('valid owned hot SKU order preparation failed');
  orders.push({principal,...order.body});
 }
 const started=performance.now();
 const winners=[];
 // All100 orders exist before simultaneous reservation attempts; insufficient
 // stock is accepted only with the application's explicit409 error code.
 await Promise.all(orders.map(async order=>{
  const token=await call(order.principal,'initiate','/api/v1/payments/shaparak/token',{orderId:order.id,orderNumber:order.orderNumber,amount:order.payableAmount});
  if(token.status===409&&token.body?.code==='insufficient_stock'){summary.conflicts++;return}
  if(token.status!==200||!token.body?.token){summary.failures++;return}
  if(token.body.amount!==order.payableAmount){summary.wrongAmount++;return}
  winners.push({...order,token:token.body.token});
 }));
 await Promise.all(winners.map(async order=>{
  const body={token:order.token,orderNumber:order.orderNumber,amount:order.payableAmount};
  const paid=await call(order.principal,'callback','/api/v1/payments/shaparak/callback',body);
  if(paid.status!==200||paid.body?.status!=='paid'||paid.body.orderNumber!==order.orderNumber){summary.failures++;return}
  summary.paid++;
  const duplicates=await Promise.all([call(order.principal,'duplicate_callback','/api/v1/payments/shaparak/callback',body),call(order.principal,'duplicate_callback','/api/v1/payments/shaparak/callback',body)]);
  for(const replay of duplicates){if(replay.status===200&&replay.body?.status==='paid'&&replay.body.orderNumber===order.orderNumber)summary.duplicateCallbacks++;else summary.failures++}
  const own=await call(order.principal,'post_owner_read','/api/v1/orders',undefined,true);
  if(own.status!==200||!Array.isArray(own.body)||own.body.some(o=>o.customerUserId!==users[order.principal].id)||!own.body.some(o=>o.id===order.id&&o.paymentStatus==='paid'&&o.payableAmount===order.payableAmount))summary.failures++;
 }));
 summary.elapsed_seconds=(performance.now()-started)/1000;summary.failed_gates=evaluateHotSKU(summary);
 summary.route_metrics={};
 for(const kind of ['initiate','callback','duplicate_callback']){
  const times=requests.filter(r=>r.route_class===kind).map(r=>r.latency_ms).sort((a,b)=>a-b),p95=times[Math.ceil(times.length*.95)-1],p99=times[Math.ceil(times.length*.99)-1];
  summary.route_metrics[kind]={requests:times.length,p95_ms:p95,p99_ms:p99};
  if(!times.length||p95>750||p99>2000)summary.failed_gates.push(kind+'_latency');
 }
}catch(error){summary.failed_gates.push('preparation_or_execution');summary.failure=error.message}
finally{summary.requests=requests.length;summary.helper_api_calls=helpers.length;summary.distinct_principals=new Set(requests.map(r=>r.principal)).size;agent.destroy();fs.writeFileSync('/tmp/summary.json',JSON.stringify(summary,null,2));process.exitCode=summary.failed_gates.length?1:0}
