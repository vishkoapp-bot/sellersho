import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { build } from 'esbuild';
import puppeteer from 'puppeteer';

test('admin refund form retains operation after lost response and reports a request rather than bank success', { timeout: 90000 }, async () => {
 const bundle=await build({write:false,bundle:true,platform:'browser',format:'iife',jsx:'automatic',define:{'process.env.NODE_ENV':'"production"','import.meta.env':'{}'},plugins:[{name:'form-fixtures',setup(b){
  b.onResolve({filter:/(?:^|\/)StoreContext$/},()=>({path:'store',namespace:'fixture'}));b.onResolve({filter:/(?:^|\/)ToastContext$/},()=>({path:'toast',namespace:'fixture'}));
  b.onLoad({filter:/.*/,namespace:'fixture'},a=>({loader:'js',contents:a.path==='store'?`export const useStore=()=>({formatPrice:n=>String(n)});`:`export const useToast=()=>({success:s=>document.getElementById('notice').textContent='success:'+s,error:s=>document.getElementById('notice').textContent='error:'+s});`}));
 }}],stdin:{loader:'tsx',resolveDir:process.cwd(),contents:`import React from'react';import{createRoot}from'react-dom/client';import{FinancePendingPaymentsTab}from'./src/components/finance/FinancePendingPaymentsTab';import{tokenStore}from'./src/services/apiClient';tokenStore.setToken('synthetic');createRoot(document.getElementById('root')).render(<FinancePendingPaymentsTab/>);`}});
 const requests:{key:string|string[]|undefined;body:any}[]=[];
 const server=createServer(async(req,res)=>{
  if(req.url?.startsWith('/api/v1/admin/payments/')){let raw='';for await(const p of req)raw+=p;requests.push({key:req.headers['idempotency-key'],body:JSON.parse(raw)});res.setHeader('Content-Type','application/json');
   if(requests.length===1)res.end('{"success":');else{res.statusCode=202;res.end('{"success":true,"status":"unknown","requestedAmountToman":2000000}');}
  }else{res.setHeader('Content-Type','text/html');res.end('<div id="notice"></div><div id="root"></div><script>'+bundle.outputFiles[0].text+'</script>');}
 });
 await new Promise<void>(resolve=>server.listen(0,'127.0.0.1',resolve));const address=server.address();assert.ok(address&&typeof address==='object');
 const browser=await puppeteer.launch({headless:true,...(process.platform==='win32'?{channel:'chrome' as const}:{})});
 try{
  const page=await browser.newPage();await page.goto(`http://127.0.0.1:${address.port}/admin-refund-fixture`);
  const fill=async()=>{await page.waitForSelector('form');await page.$$eval('form input',inputs=>{['paid-order','2000000','partial refund'].forEach((value,index)=>{Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value')!.set!.call(inputs[index],value);inputs[index].dispatchEvent(new Event('input',{bubbles:true}));});});};
  const submit=async()=>{await page.click('form button[type="submit"]');await page.evaluate(()=>[...document.querySelectorAll('button')].find(b=>b.textContent?.trim()==='استرداد قطعی'||b.textContent?.trim()==='ثبت درخواست')!.click());};
  await fill();await submit();await page.waitForFunction(()=>document.getElementById('notice')?.textContent?.startsWith('error:'));
  await page.reload();await fill();await submit();await page.waitForFunction(()=>document.getElementById('notice')?.textContent?.startsWith('success:'));
  assert.equal(requests.length,2);assert.equal(typeof requests[0].key,'string');assert.ok(requests[0].key);assert.equal(requests[0].key,requests[1].key);assert.deepEqual(requests[0].body,requests[1].body);
  const notice=await page.$eval('#notice',e=>e.textContent||'');assert.match(notice,/درخواست.*ثبت/s);assert.ok(!notice.includes('در درگاه انجام'));
 }finally{await browser.close();await new Promise<void>(resolve=>server.close(()=>resolve()));}
});
