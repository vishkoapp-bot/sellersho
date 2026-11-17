// Prepared protocol workload. The isolated runner intentionally does not enable
// this profile until fixture lifecycle queues + reconciliation are integrated.
import http from 'k6/http';
import {check, fail} from 'k6';
import exec from 'k6/execution';
import {Counter} from 'k6/metrics';
import {SharedArray} from 'k6/data';
const contract=JSON.parse(open('./workload.json'));
const fixtures=new SharedArray('guarded fixture',()=>[JSON.parse(open('/fixtures/workload.json'))])[0];
if(fixtures.guard!=='runner-owned-disposable' || fixtures.users.length!==1000 ||
 new Set(fixtures.users.map(u=>u.id)).size!==1000 || new Set(fixtures.users.map(u=>u.token)).size!==1000) throw Error('unverified independent fixture sessions');
const counts=new Counter('external_api');
const assertions=new Counter('business_assertion_failures');
const participants=new Counter('principal_requests');
const seconds=Number(__ENV.SECONDS||1800);
if(![1800,7200].includes(seconds))throw Error('qualification duration must be sustained or soak');
const scenarios={};
for(let i=0;i<contract.classes.length;i++)scenarios[contract.classes[i]]={executor:'constant-arrival-rate',rate:contract.weights[i],timeUnit:'1s',duration:`${seconds}s`,preAllocatedVUs:Math.max(100,contract.weights[i]*10),maxVUs:Math.max(100,contract.weights[i]*10)};
const thresholds={http_req_failed:['rate<0.001'],checks:['rate==1'],dropped_iterations:['count==0'],business_assertion_failures:['count==0'],external_api:[`count==${100*seconds}`]};
for(const name of contract.classes){thresholds[`http_req_duration{scenario:${name}}`]=['list','detail','store','account'].includes(name)?['p(95)<=300','p(99)<=1000']:['p(95)<=750','p(99)<=2000'];
 if(!Array.isArray(fixtures.queues[name]) || fixtures.queues[name].length<contract.weights[contract.classes.indexOf(name)]*seconds)throw Error('insufficient unique lifecycle queue: '+name);
}
for(let i=0;i<1000;i++)thresholds[`principal_requests{principal:${i}}`]=['count>0'];
export const options={scenarios,thresholds,maxRedirects:0,insecureSkipTLSVerify:true,systemTags:['status','method','name','scenario','expected_response']};
export default function(){
 const row=fixtures.queues[exec.scenario.name][exec.scenario.iterationInTest];
 if(!row || !Number.isInteger(row.principal) || row.principal<0 || row.principal>=1000 || !/^\/api\//.test(row.path) || row.path.includes('://'))fail('invalid fixture request');
 const user=fixtures.users[row.principal];
 const r=http.request(row.method,'https://capacity.example.test'+row.path,row.body?JSON.stringify(row.body):null,
  {redirects:0,headers:{Authorization:`Bearer ${user.token}`,'Content-Type':'application/json'},tags:{name:exec.scenario.name,traffic:'external_api'}});
 counts.add(1);participants.add(1,{principal:String(row.principal)});
 let pass=row.status===r.status;
 try{const body=r.json();for(const [key,value] of Object.entries(row.expected||{}))if(body[key]!==value)pass=false;}catch{pass=false}
 if(!pass)assertions.add(1);
 check(pass,{'expected status, amount and ownership':p=>p});
}
