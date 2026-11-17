import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { build } from 'esbuild';
import puppeteer from 'puppeteer';

test('return bin form keeps exact item and command on conflict and lost response', { timeout: 90000 }, async () => {
 const bundle = await build({write:false,bundle:true,platform:'browser',format:'iife',jsx:'automatic',define:{'process.env.NODE_ENV':'"production"','import.meta.env':'{}'},stdin:{loader:'tsx',resolveDir:process.cwd(),contents:`import React from 'react';import{createRoot}from'react-dom/client';import{CustomerReturnBinEntry}from'./src/components/warehouse/CustomerReturnBinEntry';createRoot(document.getElementById('root')).render(<CustomerReturnBinEntry returnID="return-one"/>);`}});
 const commands: {body: any; key: string | string[] | undefined; url: string | undefined}[]=[];
 let available=2;
 const source=(id:string)=>({returnItemId:id,productId:'p',variantId:id==='item-red'?'sku-red':'sku-blue',productTitle:'Product',variantName:id==='item-red'?'Red large':'Blue small',variantSpnc:id==='item-red'?'RED':'BLUE',warehouseId:'wh',qcStatus:'ok',quantity:2,availableQuantity:available,sourceProven:true,physical:{lengthMm:100,widthMm:100,heightMm:100,weightGrams:100,verified:true,revision:'measurement'},bins:[{binCode:'BIN-CENTRAL',remainingVolumeCm3:10000,remainingWeightGrams:1000}]});
 const server=createServer(async(req,res)=>{
  res.setHeader('Content-Type','application/json');
  if(req.url?.endsWith('/putaway-sources'))res.end(JSON.stringify([source('item-red'),source('item-blue')]));
  else if(req.method==='POST'){
   let raw='';for await(const part of req)raw+=part;commands.push({body:JSON.parse(raw),key:req.headers['idempotency-key'],url:req.url});
   if(commands.length<=2){res.statusCode=409;res.end('{"error":"capacity changed"}');}
   else if(commands.length===3){available=0;res.end('{"id":');}
   else res.end('{"id":"entry-one","quantity":2}');
  }else{res.setHeader('Content-Type','text/html');res.end('<html><body><div id="root"></div><script>'+bundle.outputFiles[0].text+'</script></body></html>');}
 });
 await new Promise<void>(resolve=>server.listen(0,'127.0.0.1',resolve));const addr=server.address();assert.ok(addr&&typeof addr==='object');
 const browser=await puppeteer.launch({headless:true,...(process.platform==='win32'?{channel:'chrome' as const}:{})});
 try{
  const page=await browser.newPage();await page.goto(`http://127.0.0.1:${addr.port}/__return-bin`);await page.click('button');await page.waitForSelector('form[data-item="item-red"]');
  const form='form[data-item="item-red"]';
  assert.match(await page.$eval(form,e=>(e as HTMLElement).innerText),/Red large.*RED/s);
  await page.select(`${form} select`,'BIN-CENTRAL');
  const fill=async(selector:string,value:string)=>page.$eval(selector,(element,text)=>{Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value')!.set!.call(element,text);element.dispatchEvent(new Event('input',{bubbles:true}));},value);
  await fill(`${form} input[aria-label="تعداد جانمایی"]`,'1.5');
  assert.equal(await page.$eval(`${form} button[type="submit"]`,e=>(e as HTMLButtonElement).disabled),true);
  await fill(`${form} input[aria-label="تعداد جانمایی"]`,'2');
  assert.equal(await page.$eval(`${form} button[type="submit"]`,e=>(e as HTMLButtonElement).disabled),true);
  await fill(`${form} input[aria-label="دلیل جانمایی"]`,'QC passed red');
  for(let i=0;i<4;i++){
   if(i===1)await fill(`${form} input[aria-label="دلیل جانمایی"]`,'QC passed red revised');
   if(i===3){
    await Promise.all([page.waitForResponse(r=>r.url().endsWith('/putaway-sources')),page.click('#root > div > button')]);
    await page.waitForFunction(selector=>document.querySelector(selector)?.textContent?.includes('باقی‌مانده: ۰'),{},form);
    assert.equal(await page.$eval(`${form} button[type="submit"]`,e=>(e as HTMLButtonElement).disabled),true);
   }
   await Promise.all([page.waitForResponse(r=>r.url().endsWith('/putaway')&&r.request().method()==='POST'),page.click(`${form} button${i===3?'[data-retry]':'[type="submit"]'}`)]);
   if(i<3){await page.waitForFunction(selector=>!(document.querySelector(selector) as HTMLButtonElement)?.disabled,{},`${form} button[type="submit"]`);assert.equal(await page.$eval(`${form} input[aria-label="دلیل جانمایی"]`,e=>(e as HTMLInputElement).value),i===0?'QC passed red':'QC passed red revised');}
  }
  assert.equal(commands.length,4);assert.ok(commands[0].key);assert.notEqual(commands[0].key,commands[1].key);assert.equal(commands[1].key,commands[2].key);assert.equal(commands[2].key,commands[3].key);
  commands.forEach((c,i)=>{assert.equal(c.url,'/api/v1/warehouse/customer-returns/return-one/items/item-red/putaway');assert.deepEqual(c.body,{binCode:'BIN-CENTRAL',quantity:2,reason:i===0?'QC passed red':'QC passed red revised'});});
  await page.waitForFunction(selector=>document.querySelector(selector)?.textContent?.includes('باقی‌مانده: ۰'),{},form);
 }finally{await browser.close();server.closeAllConnections();await new Promise<void>(resolve=>server.close(()=>resolve()));}
});
