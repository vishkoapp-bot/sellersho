// Baseline gates only: passing these never certifies the full capacity contract.
export function evaluate(s, requireIdentities, kind='read') {
 const failures=[];
 if(s.missed_schedule_slots!==0||s.scheduling_gate!=='matched')failures.push('arrival_schedule');
 if(!Number.isInteger(s.offered)||s.offered<1||s.requests!==s.offered)failures.push('request_count');
 for(const key of ['errors','dropped_iterations','failed_identity_assertions'])if(s[key]!==0)failures.push(key);
 if(requireIdentities&&s.distinct_principals!==1000)failures.push('identity_participation');
 if(!Number.isFinite(s.p95_ms)||!Number.isFinite(s.p99_ms)||s.p95_ms>(kind==='mutation'?750:300)||s.p99_ms>(kind==='mutation'?2000:1000))failures.push(kind+'_latency');
 return failures;
}
