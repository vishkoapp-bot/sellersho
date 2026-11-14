import assert from 'node:assert/strict';
import {existsSync} from 'node:fs';
import test from 'node:test';
const exists=existsSync(new URL('./baseline-policy.mjs',import.meta.url));
test('baseline verdict policy exists',()=>assert.ok(exists,'latency failure policy missing'));
if(exists){const {evaluate}=await import('./baseline-policy.mjs');
 const valid={missed_schedule_slots:0,scheduling_gate:'matched',requests:1000,offered:1000,errors:0,dropped_iterations:0,failed_identity_assertions:0,distinct_principals:1000,p95_ms:300,p99_ms:1000};
 test('late schedule fails even when eventual requests and latency look good',()=>assert.ok(evaluate({...valid,missed_schedule_slots:1,scheduling_gate:'FAILED'},true).includes('arrival_schedule')));
 test('missing schedule evidence fails closed',()=>{const s={...valid};delete s.scheduling_gate;assert.ok(evaluate(s,true).includes('arrival_schedule'))});
 test('login uses declared mutation latency budget',()=>assert.deepEqual(evaluate({...valid,p95_ms:750,p99_ms:2000},true,'mutation'),[]));
 test('latency at targets accepted for baseline checks only',()=>assert.deepEqual(evaluate(valid,true),[]));
 test('observed slow auth baseline fails even with zero HTTP errors',()=>assert.ok(evaluate({...valid,p95_ms:1330,p99_ms:1755},true).includes('read_latency')));
 test('missing identity, requests or latency evidence fails closed',()=>{for(const key of ['requests','distinct_principals','p95_ms']){const copy={...valid};delete copy[key];assert.notEqual(evaluate(copy,true).length,0)}});
 test('dropped arrivals and wrong identity never pass',()=>{for(const key of ['errors','dropped_iterations','failed_identity_assertions'])assert.notEqual(evaluate({...valid,[key]:1},true).length,0)});
}
