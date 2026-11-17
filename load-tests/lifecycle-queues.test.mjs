import test from 'node:test';
import assert from 'node:assert/strict';
import {existsSync} from 'node:fs';
const exists=existsSync(new URL('./lifecycle-queues.mjs',import.meta.url));
test('rolling lifecycle implementation exists',()=>assert.ok(exists));
if(exists){const {LifecycleQueues}=await import('./lifecycle-queues.mjs');
 const users=[{id:'a'},{id:'b'}], item={logicalId:'attempt1',principal:0,userId:'a',expiresAt:1500};
 test('preserves owner and rejects cross-user queue reuse',()=>{const q=new LifecycleQueues(users,()=>1000,2);q.push('create',item);assert.equal(q.take('create').userId,'a');assert.throws(()=>q.push('create',{...item,logicalId:'bad',userId:'b'}));});
 test('duplicate stages, backlog overflow and missing work remain failed',()=>{const q=new LifecycleQueues(users,()=>1000,1);q.push('callback',item);assert.throws(()=>q.push('callback',item));assert.throws(()=>q.push('callback',{...item,logicalId:'two'}));q.take('callback');assert.equal(q.take('callback'),null);assert.equal(q.stats.missing,1)});
 test('expired entries are never used or refreshed',()=>{let now=1000;const q=new LifecycleQueues(users,()=>now,2);q.push('initiate',item);now=1500;assert.equal(q.take('initiate'),null);assert.equal(q.stats.expired,1)});
 test('ambiguous mutation quarantines once and recovery is explicit',()=>{const q=new LifecycleQueues(users,()=>1000,2);q.push('create',item);q.take('create');q.ambiguous('create',item);assert.equal(q.stats.ambiguous,1);assert.equal(q.recovery()[0].item.logicalId,'attempt1');q.resolve('create','attempt1');assert.equal(q.recovery().length,0);assert.equal(q.stats.recovered,1)});
 test('hold deadlines cannot exceed production15minute budget',()=>{const q=new LifecycleQueues(users,()=>1000,2);assert.throws(()=>q.push('callback',{...item,expiresAt:902000}))});
}
