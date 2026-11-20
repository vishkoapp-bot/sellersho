import {WEIGHTS as weights} from './workload.mjs';
export function arrivalPlan(seconds,rps){
 if(!Number.isInteger(seconds)||seconds<1||seconds>7200||![100,200].includes(rps))throw Error('arrival plan outside bounded local contract');
 return {seconds,rps,offered:seconds*rps,counts:weights.map(w=>w*seconds*rps/100)};
}
// Fixed 10ms generator tolerance, independent of application latency gates.
// A slot late by more than this is recorded/dropped, never caught up.
export class ArrivalScheduler {
 constructor({total,rps,start,clock}){
  if(!Number.isInteger(total)||total<1||!Number.isFinite(rps)||rps<=0||rps>200||!Number.isFinite(start)||typeof clock!=='function')throw Error('invalid bounded arrival schedule');
  Object.assign(this,{total,rps,start,clock,processed:0,missed:0,max_lag_ms:0,tolerance_ms:10});
 }
 drain(visit){
  while(this.processed<this.total){
   const elapsed=this.clock()-this.start,scheduled=(this.processed+1)*1000/this.rps;
   if(elapsed<scheduled)return;
   const lag=elapsed-scheduled,missed=lag>this.tolerance_ms;
   const slot={ordinal:this.processed++,scheduled_ms:scheduled,decision_ms:elapsed,arrival_lag_ms:lag,missed_schedule:missed};
   this.max_lag_ms=Math.max(this.max_lag_ms,lag);if(missed)this.missed++;
   visit(slot);
  }
 }
 get passed(){return this.processed===this.total&&this.missed===0}
}
export function arrivalSummary(scheduler,requests,elapsed){
 const dispatchLate=requests.filter(r=>r.arrival_lag_ms>scheduler.tolerance_ms).length;
 return {planned_api_rps:scheduler.rps,actual_attempted_api_rps:requests.length/elapsed,actual_response_api_rps:requests.filter(r=>r.status>0).length/elapsed,
  rate_scope:'measured client attempts/responses over elapsed seconds including graceful completion;server counters independently reconciled',
  missed_schedule_slots:scheduler.missed,late_dispatched_calls:dispatchLate,max_arrival_lag_ms:requests.reduce((max,r)=>Math.max(max,r.arrival_lag_ms||0),scheduler.max_lag_ms),arrival_tolerance_ms:scheduler.tolerance_ms,scheduling_gate:scheduler.passed&&!dispatchLate?'matched':'FAILED'};
}
