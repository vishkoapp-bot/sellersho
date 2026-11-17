import test from 'node:test';
import assert from 'node:assert/strict';
test('private carrier double rejects absent capability and real-shop payloads',async()=>{
 const {createCarrierDouble}=await import('./carrier-double.mjs');
 assert.throws(()=>createCarrierDouble({}),/marker/);
 const server=createCarrierDouble({marker:'1234567890abcdef12345678',key:'a'.repeat(32),delay:0});
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 try{
  const url='http://127.0.0.1:'+server.address().port;
  assert.equal((await fetch(url+'/api/v4/tipax/public/user/order/check-price/',{method:'POST'})).status,403);
  const headers={Authorization:'Bearer '+'a'.repeat(32),'Content-Type':'application/json'};
  assert.equal((await fetch(url+'/api/v4/tipax/public/user/order/check-price/',{method:'POST',headers,body:JSON.stringify({shop_id:'real-shop'})})).status,400);
  const quote=await fetch(url+'/api/v4/tipax/public/user/order/check-price/',{method:'POST',headers,body:JSON.stringify({shop_id:'cap-shop-1',products:[{count:1,weight_per_count:100,amount_per_count:1232000}],package_weight:900})});
  assert.equal(quote.status,200);assert.equal((await quote.json()).entries.total_send_price,100000);
  const metrics=await (await fetch(url+'/metrics',{headers})).json();assert.equal(metrics.quotes,1);assert.equal(metrics.outbound_requests,0);
 }finally{await new Promise(r=>server.close(r))}
});
