import { before, after, test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer, type Server } from 'node:http';
import { build } from 'esbuild';
import puppeteer, { type Browser, type Page } from 'puppeteer';

let browser: Browser; let server: Server; let url: string;
let tokenRequests = 0; let breakRecovery = false; let loseResponse = false; let holdResponse = false; let releaseResponse: (()=>void)|null = null; let creates: any[] = []; const orders = new Map<string, any>(); const abandoned = new Set<string>();
before(async () => {
  const bundle = await build({ write:false, bundle:true, platform:'browser', format:'iife', jsx:'automatic', define:{'process.env.NODE_ENV':'"production"','import.meta.env':'{}'}, plugins:[{name:'checkout-auth',setup(b){
    b.onResolve({filter:/(?:^|\/)AuthContext$/},()=>({path:'auth',namespace:'fixture'}));
    b.onLoad({filter:/.*/,namespace:'fixture'},()=>({loader:'js',contents:`export const useAuth=()=>window.checkoutAuth;`}));
  }}], stdin:{loader:'tsx',resolveDir:process.cwd(),contents:`
    import React from 'react';import{createRoot}from'react-dom/client';import{OrderProvider,useOrders}from'./src/context/OrderContext';import{tokenStore}from'./src/services/apiClient';
    const root=createRoot(document.getElementById('root'));const h=window.checkoutHarness={};
    function Probe(){h.orders=useOrders();return <div>ready</div>}
    h.login=(id)=>{window.checkoutAuth={currentUser:{id,storeId:'central'},authToken:'synthetic-'+id,isSessionReady:true};tokenStore.setToken('synthetic-'+id);root.render(<OrderProvider><Probe/></OrderProvider>)};
    h.params={customerInfo:{fullName:'fixture',phone:'09120000000',province:'x',city:'x',address:'PRIVATE-ADDRESS',postalCode:'1234512345'},paymentMethod:'cash',cartItems:[{product:{id:'product',price:2000},quantity:1,storeId:'central'}],currentStore:{id:'central',slug:'central'}};
    h.place=()=>h.orders.placeOrder(h.params).then(o=>({id:o.id,items:o.items.map(i=>i.quantity)})).catch(e=>({error:e.message}));h.login('A');
  `}});
  server=createServer(async(req,res)=>{
    if(req.url==='/fixture'){res.setHeader('Content-Type','text/html');res.end('<div id="root"></div><script>'+bundle.outputFiles[0].text+'</script>');return;}
    res.setHeader('Content-Type','application/json'); const owner=req.headers.authorization??'';
    if(req.url==='/api/v1/orders'&&req.method==='POST'){
      let raw='';for await(const c of req)raw+=c;const payload=JSON.parse(raw);creates.push(payload);const key=owner+':'+payload.idempotencyKey;let order=orders.get(key);
      if(abandoned.has(key)){res.statusCode=409;res.end('{"error":"checkout_attempt_abandoned"}');return;}
      const replay=!!order;
      if(!order){order={id:'order-'+orders.size,orderNumber:'N-'+orders.size,storeId:'central',storeName:'central',customerFullName:'fixture',customerPhone:'09120000000',customerAddress:'PRIVATE-ADDRESS',totalAmount:2000,payableAmount:2000,discountAmount:0,shippingCost:0,status:'processing',paymentStatus:'pending',createdAt:'2026-11-15',items:[{id:'line',productId:'product',quantity:payload.items[0].quantity,finalUnitPrice:2000}]};orders.set(key,order);}
      if(loseResponse){loseResponse=false;res.writeHead(200,{'Content-Length':'100000'});res.write('{');setImmediate(()=>res.destroy());return;}const send=()=>res.end(JSON.stringify({...order,checkoutReplayed:replay}));if(holdResponse){holdResponse=false;releaseResponse=send;}else send();return;
    }
    if(req.url?.startsWith('/api/v1/checkout-attempts/')) {
      const id=decodeURIComponent(req.url.split('/').at(-1)!);const key=owner+':'+id;const order=orders.get(key);
      if(req.method==='POST'&&!order)abandoned.add(key);
      if(!order&&!abandoned.has(key)){res.statusCode=404;res.end('{"error":"not found"}');return;}
      res.end(JSON.stringify({attemptId:id,state:order?'completed':'abandoned',order:breakRecovery&&order?{...order,items:null}:order}));return;
    }
    if(req.url==='/api/v1/payments/shaparak/token')tokenRequests++;
    res.statusCode=503;res.end('{"error":"Synthetic unavailable payment provider"}');
  });
  await new Promise<void>(r=>server.listen(0,'127.0.0.1',r));const a=server.address();assert.ok(a&&typeof a==='object');url=`http://127.0.0.1:${a.port}/fixture`;
  browser=await puppeteer.launch({headless:true,...(process.platform==='win32'?{channel:'chrome' as const}:{})});
});
after(async()=>{await browser?.close();await new Promise<void>(r=>server?.close(()=>r()));});
const ready=async(page:Page)=>{await page.goto(url);await page.waitForFunction(()=>!!(window as any).checkoutHarness?.orders);};
const place=(page:Page)=>page.evaluate(()=>(window as any).checkoutHarness.place());
test('committed response loss retries the same logical order instead of charging a second cart',async()=>{
  creates=[];orders.clear();tokenRequests=0;loseResponse=true;const context=await browser.createBrowserContext();
  try{const page=await context.newPage();await ready(page);const first=await place(page);assert.ok(first.error);const second=await place(page);assert.ok(second.id);assert.equal(orders.size,1,'response loss must not create a second order');assert.equal(creates[0].idempotencyKey,creates[1].idempotencyKey);assert.equal(tokenRequests,0,'recovering a lost order response must not start another payment intent');}finally{await context.close();}
});

test('reload after committed response loss keeps the original checkout ID',async()=>{
 creates=[];orders.clear();tokenRequests=0;const context=await browser.createBrowserContext();try{const page=await context.newPage();await ready(page);loseResponse=true;assert.ok((await place(page)).error);await page.reload();await page.waitForFunction(()=>!!(window as any).checkoutHarness?.orders);assert.ok((await place(page)).id);assert.equal(orders.size,1);assert.equal(creates[0].idempotencyKey,creates[1].idempotencyKey);}finally{await context.close();}
});
for(const tabs of [false,true])test(tabs?'simultaneous tabs select the same durable checkout identity':'double submit returns one logical order',async()=>{
 creates=[];orders.clear();tokenRequests=0;const context=await browser.createBrowserContext();try{const a=await context.newPage();await ready(a);const b=tabs?await context.newPage():a;if(tabs)await ready(b);const [one,two]=await Promise.all([place(a),place(b)]);assert.equal(one.id,two.id);assert.equal(orders.size,1);assert.equal(creates[0].idempotencyKey,creates[1].idempotencyKey);}finally{await context.close();}
});
test('changed payload is blocked until explicit resolution, and recovery uses original lines',async()=>{
 creates=[];orders.clear();tokenRequests=0;const context=await browser.createBrowserContext();try{const page=await context.newPage();await ready(page);loseResponse=true;await place(page);await page.evaluate(()=>(window as any).checkoutHarness.params.cartItems[0].quantity=2);assert.ok((await place(page)).error);assert.equal(creates.length,1);const recovered=await page.evaluate(async()=>{const o=await(window as any).checkoutHarness.orders.recoverCheckout();return{id:o.id,quantity:o.items[0].quantity};});assert.equal(recovered.quantity,1);assert.equal(orders.size,1);await page.evaluate(()=>(window as any).checkoutHarness.orders.startNewCheckout());const next=await place(page);assert.notEqual(next.id,recovered.id);assert.equal(orders.size,2);assert.notEqual(creates[0].idempotencyKey,creates[1].idempotencyKey);}finally{await context.close();}
});
test('two deliberate identical purchases get different identities',async()=>{
 creates=[];orders.clear();tokenRequests=0;const context=await browser.createBrowserContext();try{const page=await context.newPage();await ready(page);const first=await place(page);await page.evaluate(()=>(window as any).checkoutHarness.orders.startNewCheckout());const second=await place(page);assert.notEqual(first.id,second.id);assert.equal(orders.size,2);assert.notEqual(creates[0].idempotencyKey,creates[1].idempotencyKey);}finally{await context.close();}
});
test('logout and a new identity discard stale order completion without copying private data',async()=>{
 creates=[];orders.clear();tokenRequests=0;const context=await browser.createBrowserContext();try{const page=await context.newPage();await ready(page);holdResponse=true;await page.evaluate(()=>{const h=(window as any).checkoutHarness;h.late=h.place();});await page.waitForFunction(()=>!!(window as any).checkoutHarness.orders.checkoutAttempt,{polling:50,timeout:5000});assert.equal(creates.length,1);await page.evaluate(()=>(window as any).checkoutHarness.login('B'));await page.waitForFunction(()=>(window as any).checkoutAuth.currentUser.id==='B'&&(window as any).checkoutHarness.orders.currentOrder===null);releaseResponse!();releaseResponse=null;const late=await page.evaluate(()=>(window as any).checkoutHarness.late);assert.match(late.error,/Session changed/);const second=await place(page);assert.ok(second.id);assert.equal(orders.size,2);assert.notEqual(creates[0].idempotencyKey,creates[1].idempotencyKey);assert.equal(await page.evaluate(()=>(window as any).checkoutHarness.orders.currentOrder.id),second.id);
 const records=await page.evaluate(()=>new Promise<any[]>((resolve,reject)=>{const r=indexedDB.open('sellersho.checkout.v1',1);r.onerror=()=>reject(r.error);r.onsuccess=()=>{const db=r.result;const q=db.transaction('attempts').objectStore('attempts').getAll();q.onsuccess=()=>{resolve(q.result);db.close();};};}));assert.ok(!JSON.stringify(records).includes('PRIVATE-ADDRESS'));assert.ok(!JSON.stringify(records).includes('synthetic-'));
 }finally{await context.close();}
});
test('unavailable durable browser storage fails closed before sending an order',async()=>{
 creates=[];orders.clear();tokenRequests=0;const context=await browser.createBrowserContext();try{const page=await context.newPage();await ready(page);await page.evaluate(()=>Object.defineProperty(window,'indexedDB',{get(){throw new Error('synthetic storage disabled')}}));assert.ok((await place(page)).error);assert.equal(creates.length,0);assert.equal(orders.size,0);}finally{await context.close();}
});
test('late old-tab completion cannot overwrite a deliberately new active attempt',async()=>{
 creates=[];orders.clear();tokenRequests=0;const context=await browser.createBrowserContext();try{const a=await context.newPage();const b=await context.newPage();await ready(a);await ready(b);holdResponse=true;await a.evaluate(()=>{const h=(window as any).checkoutHarness;h.late=h.place();});await a.waitForFunction(()=>!!(window as any).checkoutHarness.orders.checkoutAttempt,{polling:50,timeout:5000});await place(b);await b.evaluate(()=>(window as any).checkoutHarness.orders.startNewCheckout());const fresh=await place(b);releaseResponse!();releaseResponse=null;await a.evaluate(()=>(window as any).checkoutHarness.late);const replay=await place(a);assert.equal(replay.id,fresh.id);assert.equal(orders.size,2);}finally{await context.close();}
});

test('incomplete recovery response remains retryable without acknowledging missing order lines',async()=>{
 creates=[];orders.clear();tokenRequests=0;const context=await browser.createBrowserContext();try{const page=await context.newPage();await ready(page);loseResponse=true;await place(page);breakRecovery=true;const result=await page.evaluate(()=>(window as any).checkoutHarness.orders.recoverCheckout().then(()=>({success:true})).catch((e:Error)=>({error:e.message})));assert.ok(result.error,'missing lines must not become acknowledged order state');assert.equal(await page.evaluate(()=>(window as any).checkoutHarness.orders.currentOrder),null);breakRecovery=false;const recovered=await page.evaluate(()=>(window as any).checkoutHarness.orders.recoverCheckout());assert.equal(recovered.items.length,1);assert.equal(orders.size,1);}finally{breakRecovery=false;await context.close();}
});
