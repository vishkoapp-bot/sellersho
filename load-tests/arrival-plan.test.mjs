import test from 'node:test';import assert from 'node:assert/strict';
test('a stalled generator cannot catch up missed slots with fast responses',async()=>{
 const {ArrivalScheduler,arrivalSummary}=await import('./arrival-plan.mjs');let now=0;const sent=[];
 const scheduler=new ArrivalScheduler({total:100,rps:100,start:0,clock:()=>now});
 now=10;scheduler.drain(slot=>{if(!slot.missed_schedule)sent.push(slot)});
 now=1000;scheduler.drain(slot=>{if(!slot.missed_schedule)sent.push(slot)});
 assert.equal(scheduler.processed,100);assert.equal(scheduler.missed,97);
 assert.equal(sent.length,3);assert.equal(scheduler.passed,false);
 assert.equal(sent[0].scheduled_ms,10);assert.equal(sent[0].arrival_lag_ms,0);
 assert.equal(sent.at(-1).scheduled_ms,1000);
 const rates=arrivalSummary(scheduler,sent.map(slot=>({...slot,status:200})),1);
 assert.equal(rates.planned_api_rps,100);assert.equal(rates.actual_attempted_api_rps,3);
 assert.equal(rates.actual_response_api_rps,3);assert.equal(rates.scheduling_gate,'FAILED');
});
test('clock is sampled at each dispatch and exact tolerance boundary is explicit',async()=>{
 const {ArrivalScheduler}=await import('./arrival-plan.mjs');let now=20;const slots=[];
 const scheduler=new ArrivalScheduler({total:3,rps:100,start:0,clock:()=>now});
 scheduler.drain(slot=>{slots.push(slot);now+=20});
 assert.deepEqual(slots.map(s=>s.missed_schedule),[false,true,true]);
 assert.equal(scheduler.missed,2);assert.equal(scheduler.max_lag_ms,30);
});
test('bounded arrival plan preserves exact mix at target and headroom rates',async()=>{
 const {arrivalPlan}=await import('./arrival-plan.mjs');
 assert.deepEqual(arrivalPlan(30,100).counts,[900,600,450,450,240,120,120,120]);
 assert.equal(arrivalPlan(300,200).offered,60000);
 assert.deepEqual(arrivalPlan(300,200).counts,[18000,12000,9000,9000,4800,2400,2400,2400]);
 assert.throws(()=>arrivalPlan(7201,100));assert.throws(()=>arrivalPlan(30,1000));assert.throws(()=>arrivalPlan(0,100));
});
