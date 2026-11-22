import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { build } from 'esbuild';
import puppeteer from 'puppeteer';

test('unpaid orders paginate with gated admin controls and the refund form retains operation after lost response and reports a request rather than bank success', { timeout: 90000 }, async () => {
 const bundle=await build({write:false,bundle:true,platform:'browser',format:'iife',jsx:'automatic',define:{'process.env.NODE_ENV':'"production"','import.meta.env':'{}'},plugins:[{name:'form-fixtures',setup(b){
  b.onResolve({filter:/(?:^|\/)StoreContext$/},()=>({path:'store',namespace:'fixture'}));b.onResolve({filter:/(?:^|\/)ToastContext$/},()=>({path:'toast',namespace:'fixture'}));
  b.onLoad({filter:/.*/,namespace:'fixture'},a=>({loader:'js',contents:a.path==='store'?`export const useStore=()=>({formatPrice:n=>String(n),adminSession:{role:new URLSearchParams(location.search).get('role')||'admin'}});`:`export const useToast=()=>({success:s=>document.getElementById('notice').textContent='success:'+s,error:s=>document.getElementById('notice').textContent='error:'+s});`}));
 }}],stdin:{loader:'tsx',resolveDir:process.cwd(),contents:`import React from'react';import{createRoot}from'react-dom/client';import{FinancePendingPaymentsTab}from'./src/components/finance/FinancePendingPaymentsTab';import{tokenStore}from'./src/services/apiClient';tokenStore.setToken('synthetic');createRoot(document.getElementById('root')).render(<FinancePendingPaymentsTab/>);`}});
 let listMode: 'orders' | 'empty' | 'absent' = 'orders';
 const listRequests: URLSearchParams[] = [];
 const requests:{key:string|string[]|undefined;body:any}[]=[];
 const server=createServer(async(req,res)=>{
  if (req.url?.startsWith('/api/v1/admin/orders/pending-payment')) {
   const query = new URL(req.url, 'http://localhost').searchParams;
   listRequests.push(query);
   res.setHeader('Content-Type', 'application/json');
   if (listMode === 'absent') { res.statusCode = 404; res.end('{"error":"missing"}'); return; }
   const secondPage = query.get('cursor') === 'next+cursor';
   const order = (n: number) => ({ id: `id-${n}`, orderNumber: `UNPAID-${n}`, customerName: `Customer ${n}`, amountToman: n * 1000, paymentStatus: 'pending', status: 'pending', createdAt: '2026-11-22', ageHours: 12 });
   const hasMore = listMode === 'orders' && !secondPage;
   const orders = listMode === 'empty' ? [] : secondPage ? [order(3)] : [order(1), order(2)];
   res.end(JSON.stringify({ success: true, orders, nextCursor: hasMore ? 'next+cursor' : '', hasMore, limit: 50 }));
  } else if(req.url?.startsWith('/api/v1/admin/payments/')){let raw='';for await(const p of req)raw+=p;requests.push({key:req.headers['idempotency-key'],body:JSON.parse(raw)});res.setHeader('Content-Type','application/json');
   if(requests.length===1)res.end('{"success":');else{res.statusCode=202;res.end('{"success":true,"status":"unknown","requestedAmountToman":2000000}');}
  }else{res.setHeader('Content-Type','text/html');res.end('<div id="notice"></div><div id="root"></div><script>'+bundle.outputFiles[0].text+'</script>');}
 });
 await new Promise<void>(resolve=>server.listen(0,'127.0.0.1',resolve));const address=server.address();assert.ok(address&&typeof address==='object');
 const browser=await puppeteer.launch({headless:true,args:['--no-sandbox','--disable-setuid-sandbox'],...(process.platform==='win32'?{channel:'chrome' as const}:{})}).catch(async e=>{await new Promise<void>(resolve=>server.close(()=>resolve()));throw e;});
 try{
  const page=await browser.newPage();
  const fixture = `http://127.0.0.1:${address.port}/admin-refund-fixture`;
  await page.goto(fixture);
  await page.waitForFunction(() => document.body.textContent?.includes('UNPAID-2'));
  const list = await page.$eval('section', section => section.textContent ?? '');
  assert.match(list, /سفارش‌های پرداخت‌نشده/);
  assert.match(list, /شماره سفارش.*نام مشتری.*مبلغ.*زمان انتظار/s);
  assert.match(list, /UNPAID-1.*Customer 1.*1000.*UNPAID-2.*Customer 2.*2000/s);
  await page.click('tbody tr:first-child summary');
  const admin = await page.$eval('tbody tr:first-child', row => ({
    inputStates: Array.from(row.querySelectorAll('input')).map(input => input.matches(':disabled')),
    confirmDisabled: row.querySelector('button[type="submit"]')?.matches(':disabled'),
    identity: row.querySelector('form')?.dataset.operationId,
    text: row.textContent ?? '',
  }));
  assert.deepEqual(admin.inputStates, [true, true]);
  assert.equal(admin.confirmDisabled, true);
  assert.equal(admin.identity, 'admin-confirm-manual:id-1');
  assert.match(admin.text, /نوع مدرک موردنیاز هنوز از طرف مالک مشخص نشده است/);
  await page.evaluate(() => Array.from(document.querySelectorAll('button')).find(button => button.textContent?.includes('بارگذاری سفارش‌های بیشتر'))!.click());
  await page.waitForFunction(() => document.body.textContent?.includes('UNPAID-3'));
  assert.equal(listRequests[0].get('limit'), '50');
  assert.equal(listRequests[1].get('cursor'), 'next+cursor');
  assert.equal(await page.$$eval('tbody tr', rows => rows.length), 3);

  await page.goto(`${fixture}?role=finance_specialist`);
  await page.waitForFunction(() => document.body.textContent?.includes('UNPAID-2'));
  assert.equal(await page.$('tbody summary'), null);
  assert.equal(await page.$('tbody form'), null);

  listMode = 'empty';
  await page.goto(fixture);
  await page.waitForFunction(() => document.body.textContent?.includes('سفارش پرداخت‌نشده‌ای نیست.'));
  assert.equal(await page.$('tbody tr'), null);

  listMode = 'absent';
  await page.reload();
  await page.waitForFunction(() => document.body.textContent?.includes('فهرست سفارش‌های پرداخت‌نشده هنوز در دسترس نیست.'));
  assert.deepEqual(await page.$$eval('form input', inputs => inputs.map(input => input.placeholder)), [
    'شناسه سفارش (الزامی)', 'مبلغ (تومان) — خالی = کل', 'دلیل استرداد (الزامی)',
  ]);
  const fill=async()=>{await page.waitForSelector('form');await page.$$eval('form input',inputs=>{['paid-order','2000000','partial refund'].forEach((value,index)=>{Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value')!.set!.call(inputs[index],value);inputs[index].dispatchEvent(new Event('input',{bubbles:true}));});});};
  const submit=async()=>{await page.click('form button[type="submit"]');await page.evaluate(()=>[...document.querySelectorAll('button')].find(b=>b.textContent?.trim()==='استرداد قطعی'||b.textContent?.trim()==='ثبت درخواست')!.click());};
  await fill();await submit();await page.waitForFunction(()=>document.getElementById('notice')?.textContent?.startsWith('error:'));
  await page.reload();await fill();await submit();await page.waitForFunction(()=>document.getElementById('notice')?.textContent?.startsWith('success:'));
  assert.equal(requests.length,2);assert.equal(typeof requests[0].key,'string');assert.ok(requests[0].key);assert.equal(requests[0].key,requests[1].key);assert.deepEqual(requests[0].body,requests[1].body);
  const notice=await page.$eval('#notice',e=>e.textContent||'');assert.match(notice,/درخواست.*ثبت/s);assert.ok(!notice.includes('در درگاه انجام'));
 }finally{await browser.close();await new Promise<void>(resolve=>server.close(()=>resolve()));}
});
