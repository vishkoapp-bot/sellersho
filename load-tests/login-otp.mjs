// Actual customer OTP request + verification, through private SMS double.
// Helper reads are counted separately; no redirect, retry, token/body logging.
import https from 'node:https';
import {ArrivalScheduler} from './arrival-plan.mjs';
import http from 'node:http';
import fs from 'node:fs';
import {performance} from 'node:perf_hooks';
import {resolveTarget,loadSessions,loadCa} from './target-config.mjs';
const capTarget=resolveTarget();
const users=loadSessions(capTarget);
if(users.length!==1000||new Set(users.map(u=>u.id)).size!==1000)throw Error('1000 distinct users required');
const background=process.env.CAPACITY_BACKGROUND==='1';
const agent=new https.Agent({keepAlive:true,maxSockets:1000,ca:loadCa(capTarget),rejectUnauthorized:true});
const droppedSlots=[];
const records=[],identities=new Set();let helpers=0,helperErrors=0,active=0,dropped=0,peakActive=0,maximumArrivalLag=0;
function call(user,route,payload,kind,arrival=null){return new Promise(resolve=>{
 const started=performance.now();let done=false;
 const finish=(status,body)=>{if(done)return;done=true;records.push({principal:users.indexOf(user),route_class:kind,status,scheduled_ms:arrival?.scheduled_ms??null,arrival_lag_ms:arrival?started-start-arrival.scheduled_ms:null,started_ms:started-start,latency_ms:performance.now()-started});resolve({status,body})};
 const req=https.request({hostname:capTarget.ip||capTarget.host,port:capTarget.port,servername:capTarget.host,path:route,method:payload?'POST':'GET',agent,timeout:5000,
  headers:payload?{'Content-Type':'application/json',Host:capTarget.host}:{Authorization:'Bearer '+user.token,Host:capTarget.host}},res=>{
   let text='';res.on('data',b=>{if(text.length<65536)text+=b});res.on('end',()=>{let body;try{body=JSON.parse(text)}catch{}finish(res.statusCode,body)});
 });req.on('timeout',()=>req.destroy());req.on('error',()=>finish(0));req.end(payload?JSON.stringify(payload):undefined);
})}
function readOTP(user){return new Promise(resolve=>{
 helpers++;let done=false;const finish=value=>{if(done)return;done=true;if(!value)helperErrors++;resolve(value)};
 const req=http.request({hostname:capTarget.smsHost,port:capTarget.smsPort,path:'/fixture/otp',method:'POST',headers:{Authorization:process.env.CAPACITY_SMS_KEY,'Content-Type':'application/json'},timeout:5000},res=>{
  let text='';res.on('data',b=>{if(text.length<8192)text+=b});res.on('end',()=>{try{finish(res.statusCode===200?JSON.parse(text).code:null)}catch{finish(null)}});
 });req.on('timeout',()=>req.destroy());req.on('error',()=>finish(null));req.end(JSON.stringify({recipient:'98'+user.mobile.slice(1)}));
})}
async function login(user,arrival){
 const sent=await call(user,'/api/v1/auth/otp/send',{mobile:user.mobile},'otp_send',arrival);
 if(sent.status!==200)return;
 const code=await readOTP(user);if(!code)return;
 const verified=await call(user,'/api/v1/auth/otp/verify',{mobile:user.mobile,code},'otp_verify');
 if(verified.status===200&&verified.body?.user?.id===user.id&&verified.body?.token){user.token=verified.body.token;identities.add(user.id);}
}
const start=performance.now();
const loginSchedule=new ArrivalScheduler({total:1000,rps:1000/60,start,clock:()=>performance.now()});
const browseSchedule=background?new ArrivalScheduler({total:4000,rps:4000/60,start,clock:()=>performance.now()}):null;
await new Promise(resolve=>{const timer=setInterval(()=>{
 loginSchedule.drain(arrival=>{
  maximumArrivalLag=Math.max(maximumArrivalLag,arrival.arrival_lag_ms);
  if(arrival.missed_schedule||active>=1000){dropped++;droppedSlots.push({...arrival,flow:'login',reason:arrival.missed_schedule?'missed_schedule':'active_limit'});return}
  active++;peakActive=Math.max(peakActive,active);login(users[arrival.ordinal],arrival).finally(()=>active--);
 });
 browseSchedule?.drain(arrival=>{const i=arrival.ordinal,user=users[i%1000];
  if(arrival.missed_schedule||active>=1000){dropped++;droppedSlots.push({...arrival,flow:'browse',reason:arrival.missed_schedule?'missed_schedule':'active_limit'});return}
  active++;peakActive=Math.max(peakActive,active);call(user,'/api/v1/products?limit=24',null,'browse',arrival).finally(()=>active--);
 });
 if(loginSchedule.processed===1000&&(!browseSchedule||browseSchedule.processed===4000)&&active===0){clearInterval(timer);resolve()}
},2)});
agent.destroy();
const routes={};const failed=[];
for(const kind of ['otp_send','otp_verify',...(background?['browse']:[])]){
 const rs=records.filter(r=>r.route_class===kind),times=rs.map(r=>r.latency_ms).sort((a,b)=>a-b);
 routes[kind]={requests:rs.length,errors:rs.filter(r=>r.status!==200).length,p95_ms:times[Math.ceil(times.length*.95)-1],p99_ms:times[Math.ceil(times.length*.99)-1]};
 const gate=routes[kind];if(!times.length||gate.p95_ms>(kind==='browse'?300:750)||gate.p99_ms>(kind==='browse'?1000:2000))failed.push(kind+'_latency');
}
const expected=background?6000:2000;
if(records.length!==expected)failed.push('request_count');
if(records.some(r=>r.status!==200))failed.push('http_error');
if(identities.size!==1000)failed.push('identity_or_login_assertion');
if(!loginSchedule.passed||(browseSchedule&&!browseSchedule.passed)||records.some(r=>r.arrival_lag_ms>10))failed.push('arrival_schedule');
if(dropped)failed.push('dropped_iterations');if(helperErrors)failed.push('sms_fixture_read');
const summary={certified:false,scope:'LOCAL actual customer OTP login ramp'+(background?' with 100 total API RPS background browsing':''),
 offered:expected,requests:records.length,distinct_principals:identities.size,dropped_iterations:dropped,
 elapsed_seconds:(performance.now()-start)/1000,offered_api_rps:expected/60,helper_requests:helpers,helper_errors:helperErrors,peak_active_flows:peakActive,max_flow_arrival_lag_ms:maximumArrivalLag,
 provider_sms_expected:1000,provider_counts_source:'independent SMS container metrics in manifest',warmup_seconds:0,
 connection_mode:'keepAlive=true,maxSockets=1000; initial connections cold',route_metrics:routes,failed_gates:failed,requests_raw:records,dropped_slots_raw:droppedSlots,
 missed_schedule_slots:loginSchedule.missed+(browseSchedule?.missed||0),missed_schedule_api_calls:loginSchedule.missed*2+(browseSchedule?.missed||0),arrival_tolerance_ms:10,
 planned_api_rps:expected/60,actual_attempted_api_rps:records.length/((performance.now()-start)/1000),actual_response_api_rps:records.filter(r=>r.status>0).length/((performance.now()-start)/1000),
 schedule_scope:'login flow arrivals plus independent browsing;OTP verify is dependent fan-out,not a separately paced API slot;planned API rate is nominal fan-out only'};
fs.writeFileSync('/tmp/summary.json',JSON.stringify(summary,null,2));process.exitCode=failed.length?1:0;
