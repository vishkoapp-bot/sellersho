// Dependency-free open-loop LOCAL probe. One request per scheduled arrival;
// no redirects, no retries, no body/header/token output.
import https from 'node:https';
import {ArrivalScheduler,arrivalSummary} from './arrival-plan.mjs';
import {evaluate} from './baseline-policy.mjs';
import {resolveTarget,loadSessions,loadCa} from './target-config.mjs';
import fs from 'node:fs';
import {performance} from 'node:perf_hooks';
const capTarget=resolveTarget();
const sessions=fs.existsSync(capTarget.sessionsFile) ? loadSessions(capTarget) : [];
if(sessions.length && (sessions.length!==1000 || new Set(sessions.map(u=>u.id)).size!==1000 || new Set(sessions.map(u=>u.token)).size!==1000)) throw Error('1000 distinct principals and tokens required');
const smoke=process.env.CAPACITY_SMOKE==='1';
if(process.env.CAPACITY_PROFILE==='login')throw Error('customer password login was removed; use login-otp.mjs (actual OTP ramp) instead');
const rate=smoke?1:100, seconds=smoke?1:10, target=smoke?1:1000;
const agent=new https.Agent({keepAlive:true,maxSockets:200,rejectUnauthorized:true,ca:loadCa(capTarget)});
const droppedSlots=[];
const results=[], identities=new Set();let active=0,dropped=0;
const start=performance.now();
const scheduler=new ArrivalScheduler({total:target,rps:rate,start,clock:()=>performance.now()});
await new Promise(resolve=>{
 const timer=setInterval(()=>{
  scheduler.drain(slot=>{const ordinal=slot.ordinal;
   if(slot.missed_schedule||active>=200){dropped++;droppedSlots.push({...slot,reason:slot.missed_schedule?'missed_schedule':'active_limit'});return}active++;
   const user=sessions.length?sessions[ordinal%sessions.length]:null;
   if(user)identities.add(user.id);
   const t=performance.now();
   const req=https.request({hostname:capTarget.ip||capTarget.host,port:capTarget.port,servername:capTarget.host,path:user?'/api/v1/auth/me':'/api/health',method:'GET',agent,
      headers:user?{Authorization:'Bearer '+user.token,Host:capTarget.host}:{},timeout:5000},res=>{
       let body='';res.on('data',b=>{if(body.length<65536)body+=b});res.on('end',()=>{
        let identityOK=true;
        if(user){try{const parsed=JSON.parse(body);identityOK=parsed.userId===user.id || parsed.id===user.id || parsed.user?.id===user.id || parsed.data?.id===user.id}catch{identityOK=false}}
        results.push({ordinal,status:res.statusCode,latency_ms:performance.now()-t,scheduled_ms:slot.scheduled_ms,arrival_lag_ms:t-start-slot.scheduled_ms,identity_ok:identityOK});active--;
       });
   });
   req.on('timeout',()=>req.destroy());req.on('error',()=>{results.push({ordinal,status:0,scheduled_ms:slot.scheduled_ms,arrival_lag_ms:t-start-slot.scheduled_ms,latency_ms:performance.now()-t,identity_ok:false});active--});req.end();
  });
  if(scheduler.processed===target && active===0){clearInterval(timer);resolve()}
 },2);
});
agent.destroy();
const latencies=results.map(r=>r.latency_ms).sort((a,b)=>a-b);
const summary={generator_version:process.version,scope:sessions.length?'1000 independent auth reads only':'health only',certified:false,
 offered:target,requests:results.length,dropped_iterations:dropped,distinct_principals:identities.size,
 errors:results.filter(r=>r.status!==200).length,failed_identity_assertions:results.filter(r=>!r.identity_ok).length,
 elapsed_seconds:(performance.now()-start)/1000,offered_rps:rate,warmup_seconds:0,connection_mode:'keepAlive=true,maxSockets=200; first requests establish TLS connections',p95_ms:latencies[Math.ceil(.95*latencies.length)-1],p99_ms:latencies[Math.ceil(.99*latencies.length)-1],
 counts:{external_api:results.length,domain_authorization:results.length,assets:0,provider:0,probe:sessions.length?0:results.length},
 count_note:'domain authorization count derived from configured forward_auth; independent server counters remain required',requests_raw:results,dropped_slots_raw:droppedSlots};
Object.assign(summary,arrivalSummary(scheduler,results,summary.elapsed_seconds));
summary.failed_gates=evaluate(summary,sessions.length>0,'read');
fs.writeFileSync(process.env.CAPACITY_SUMMARY_FILE||'/tmp/summary.json',JSON.stringify(summary,null,2));
process.exitCode=summary.failed_gates.length?1:0;
