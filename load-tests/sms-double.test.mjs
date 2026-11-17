import test from 'node:test';
import assert from 'node:assert/strict';
import {existsSync} from 'node:fs';
const exists=existsSync(new URL('./sms-double.mjs',import.meta.url));
test('private SMS double exists',()=>assert.ok(exists));
if(exists){const {createSMSDouble}=await import('./sms-double.mjs');
 test('rejects missing private marker',()=>assert.throws(()=>createSMSDouble({marker:'',key:'x'})));
 test('captures synthetic OTP only behind private key and counts sends',async()=>{
  const server=createSMSDouble({marker:'0123456789abcdef01234567',key:'x'.repeat(32),delayMs:1});
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const base='http://127.0.0.1:'+server.address().port;
  try{
   assert.equal((await fetch(base+'/v1/api/send',{method:'POST',body:'{}'})).status,401);
   const r=await fetch(base+'/v1/api/send',{method:'POST',headers:{Authorization:'x'.repeat(32)},body:JSON.stringify({recipients:['989900000001'],params:{code:'1234'}})});
   assert.equal(r.status,200);assert.equal((await r.json()).meta.status,true);
   const otp=await fetch(base+'/fixture/otp',{method:'POST',headers:{Authorization:'x'.repeat(32)},body:JSON.stringify({recipient:'989900000001'})});assert.equal((await otp.json()).code,'1234');
   const metrics=await (await fetch(base+'/metrics',{headers:{Authorization:'x'.repeat(32)}})).json();assert.equal(metrics.sms_sends,1);assert.ok(!JSON.stringify(metrics).includes('1234'));
  }finally{await new Promise(resolve=>server.close(resolve))}
 });
}
