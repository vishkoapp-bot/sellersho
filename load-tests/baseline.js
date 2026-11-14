import http from 'k6/http';
import { check } from 'k6';
export const options = {insecureSkipTLSVerify:true, maxRedirects:0,
 scenarios:{health:{executor:'constant-arrival-rate',rate:100,timeUnit:'1s',duration:'10s',preAllocatedVUs:100,maxVUs:100}},
 thresholds:{http_req_failed:['rate<0.001'],checks:['rate==1'],dropped_iterations:['count==0']},
 systemTags:['status','method','name','scenario','expected_response']};
export default function() {
 const r=http.get('https://capacity.example.test/api/health',{redirects:0,tags:{name:'health',traffic:'probe'}});
 check(r,{'health status 200':r=>r.status===200});
}
