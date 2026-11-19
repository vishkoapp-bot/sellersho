import {test} from 'node:test';
import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {build} from 'esbuild';
import puppeteer from 'puppeteer';
import {mkdir,readFile,readdir} from 'node:fs/promises';

test('FIN005 live empty/error states, authoritative outcomes, double-click and lost-response reload',{timeout:90000},async()=>{
 const bundle=await build({write:false,bundle:true,platform:'browser',format:'iife',jsx:'automatic',define:{'process.env.NODE_ENV':'"production"','import.meta.env':'{}'},plugins:[{name:'fixtures',setup(b){
 b.onResolve({filter:/(?:^|\/)StoreContext$/},()=>({path:'store',namespace:'fixture'}));b.onResolve({filter:/(?:^|\/)ToastContext$/},()=>({path:'toast',namespace:'fixture'}));
 b.onLoad({filter:/.*/,namespace:'fixture'},a=>({loader:'js',contents:a.path==='store'?`export const useStore=()=>({disputes:[{id:'MOCK-CLAIM',status:'pending',type:'delivery_undelivered'}],adjustmentRecords:[{id:'MOCK-ADJUSTMENT'}],formatPrice:String,toPersianDigits:String,resolveDisputeAndAdjust:()=>{throw Error('local mutation in live')}});`:`export const useToast=()=>({success:s=>document.getElementById('notice').textContent='success:'+s,warning:()=>{},error:s=>document.getElementById('notice').textContent='error:'+s});`}));
 }}],stdin:{loader:'tsx',resolveDir:process.cwd(),contents:`import React from'react';import{createRoot}from'react-dom/client';import{SupportDisputesTab}from'./src/components/finance/SupportDisputesTab';createRoot(document.getElementById('root')).render(<SupportDisputesTab/>);`}});
 let css=''; try {const name=(await readdir('dist/assets')).find(n=>n.startsWith('index-')&&n.endsWith('.css')); if(name)css=await readFile('dist/assets/'+name,'utf8');} catch {}
 let fail=false;let list:any[]=[];let posts=0;let unblock:()=>void=()=>{};
 const base={id:'live-dispute',orderNumber:'ORDER',type:'delivery_undelivered',customerName:'مشتری',productTitle:'کالا',claimAmount:2000000,reason:'درخواست بررسی',storeId:'store',status:'resolved',resolutionAction:'release_to_seller'};
 const outcome=(state:string,status='none')=>({disputeId:base.id,orderId:'order',action:status==='none'?'release_to_seller':'refund_carrier',responsibleParty:'seller',claimAmount:2000000,decisionTime:'2026-11-16T00:00:00Z',replay:false,orderStatus:'delivered_confirmed',sellerReleaseState:state,holdReasons:[],refundScope:'order',refund:{status,amountToman:status==='none'?0:2000000,pendingToman:status==='pending'?2000000:0,succeededToman:status==='succeeded'?2000000:0,failedToman:0,unknownToman:0,needsReview:status==='unknown'}});
 const server=createServer(async(req,res)=>{
 if(req.url==='/api/v1/disputes'){res.setHeader('Content-Type','application/json');res.statusCode=fail?500:200;res.end(fail?'{}':JSON.stringify(list));}
 else if(req.url==='/api/v1/disputes/live-dispute/resolve'){posts++;await new Promise<void>(r=>unblock=r);list=[{...base,outcome:outcome('held','pending')}];res.setHeader('Content-Type','application/json');res.end('{"disputeId":');}
 else if(req.url?.startsWith('/assets/')){try{res.end(await readFile('dist'+req.url));}catch{res.statusCode=404;res.end();}}
 else{res.setHeader('Content-Type','text/html');res.end('<style>'+css+'</style><div id="notice"></div><div id="root"></div><script>'+bundle.outputFiles[0].text+'</script>');}});
 await new Promise<void>(r=>server.listen(0,'127.0.0.1',r));const address=server.address();assert.ok(address&&typeof address==='object');
 const browser=await puppeteer.launch({headless:true,...(process.platform==='win32'?{channel:'chrome' as const}:{})}).catch(async e=>{await new Promise<void>(r=>server.close(()=>r()));throw e;});
 try{const page=await browser.newPage();await page.goto(`http://127.0.0.1:${address.port}`);await page.waitForNetworkIdle();let text=await page.$eval('#root',e=>e.textContent||'');assert.ok(!text.includes('MOCK-CLAIM'));assert.ok(!text.includes('MOCK-ADJUSTMENT'));assert.match(text,/0 پرونده/);
 fail=true;await page.reload();await page.waitForNetworkIdle();text=await page.$eval('#root',e=>e.textContent||'');assert.match(text,/تلاش مجدد/);assert.ok(!text.includes('MOCK-CLAIM'));
 fail=false;
 const checks:[string,string,RegExp][]=[['held','none',/نگه داشته/],['awaiting_delivery','none',/در انتظار تحویل/],['awaiting_window','none',/پایان مهلت/],['released','none',/آزادسازی حسابداری/],['held','pending',/در انتظار اجرا/],['held','succeeded',/استرداد ثبت شده/],['held','failed',/ناموفق/],['needs_review','unknown',/نامشخص/]];
 for(const [state,status,pattern]of checks){list=[{...base,outcome:outcome(state,status)}];await page.reload();await page.waitForNetworkIdle();text=await page.$eval('#root',e=>e.textContent||'');assert.match(text,pattern);assert.ok(!text.includes('«»'));assert.ok(!text.includes('کیف پول سلر مصون'));assert.ok(!text.includes('آزادسازی فوری'));assert.ok(!text.includes('مسترد گردید'));}
 list=[{...base}];await page.reload();await page.waitForNetworkIdle();assert.match(await page.$eval('#root',e=>e.textContent||''),/پرونده تاریخی/);
 list=[{...base,status:'pending'}];await page.reload();await page.waitForNetworkIdle();await page.evaluate(()=>{const button=[...document.querySelectorAll('button')].find(b=>b.textContent?.includes('تأیید مفقودی'))!;button.click();button.click();});await page.waitForFunction(()=>[...document.querySelectorAll('button')].some(b=>b.disabled));assert.equal(posts,1);unblock();await page.waitForFunction(()=>document.getElementById('notice')?.textContent?.startsWith('error:'));await page.waitForNetworkIdle();assert.match(await page.$eval('#root',e=>e.textContent||''),/در انتظار اجرا/);
 await page.reload();await page.waitForNetworkIdle();assert.match(await page.$eval('#root',e=>e.textContent||''),/در انتظار اجرا/);assert.equal(posts,1);
 await mkdir('.superpowers/sdd/2026-11-16-dispute-eligibility/screens',{recursive:true});
 for(const width of [390,1440]){await page.setViewport({width,height:900});await page.screenshot({path:'.superpowers/sdd/2026-11-16-dispute-eligibility/screens/disputes-'+width+'.png',fullPage:true});}
 }finally{unblock();await browser.close();await new Promise<void>(r=>server.close(()=>r()));}
});
