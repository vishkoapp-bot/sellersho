// Actual built UI,10 isolated browser contexts, cold then warm. No credentials.
import fs from 'node:fs';
import path from 'node:path';
import {createRequire} from 'node:module';
import {pathToFileURL} from 'node:url';
import {X509Certificate,createHash} from 'node:crypto';
import {performance} from 'node:perf_hooks';
import {validateInternalBrowserTarget,validateRelayBrowserTarget,validateNativeBrowserTarget,isNetworkResponse} from './browser-target.mjs';
const [targetFile,rootCertificate,modulesRoot,output]=process.argv.slice(2);
const target=JSON.parse(fs.readFileSync(targetFile));
// The runner checks container/network IDs and labels before creation and load.
// No Docker socket is exposed here; the private marker is mounted by that runner.
const relay=process.env.CAPACITY_BROWSER_RELAY==='1';
const native=process.env.CAPACITY_BROWSER_NATIVE==='1';
(relay?validateRelayBrowserTarget:native?validateNativeBrowserTarget:validateInternalBrowserTarget)(target,process.env.SELLERSHO_CAPACITY_RUN);
const require=createRequire(path.join(modulesRoot,'package.json'));
const puppeteer=(await import(pathToFileURL(require.resolve('puppeteer')).href)).default;
const certificate=new X509Certificate(fs.readFileSync(rootCertificate));
// Relay-failure diagnosis (CAP-01 2026-11-20): the relay run pinned the CA cert SPKI, but Chrome's
// --ignore-certificate-errors-spki-list matches the LEAF cert SPKI. The relay exchanged TLS handshake
// bytes then aborted before any HTTP request - exactly a pin mismatch. Native runs pass the leaf SPKI
// (CAPACITY_BROWSER_LEAF_SPKI); if native pages render with it, the hypothesis is confirmed.
const spki=process.env.CAPACITY_BROWSER_LEAF_SPKI||createHash('sha256').update(certificate.publicKey.export({type:'spki',format:'der'})).digest('base64');
const executable=relay?['C:/Program Files/Google/Chrome/Application/chrome.exe','C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'].find(p=>fs.existsSync(p)):native?undefined:'/usr/bin/chromium';
const rows=[],pages=[],summary={certified:false,scope:'LOCAL anonymous real-browser UI/assets burst10contexts;not1000-user or checkout qualification',requests_raw:rows,pages,failed_gates:[],blocked_external:0,page_errors:0,tls:'exception pinned to this ephemeral CA public-key hash only;no global certificate bypass',tls_pin:process.env.CAPACITY_BROWSER_LEAF_SPKI?'leaf-spki':'ca-spki (legacy)',generator_cpu_scope:'isolated Chromium container;not sustained capacity evidence'};
let browser;
try{
 if(relay){summary.scope+=';loopback raw TLS relay functional evidence only';summary.timing_qualification='EXCLUDED: Docker exec relay and shared host browser timings';summary.generator_cpu_scope='host Chrome CPU unmeasured;not capacity evidence'}
 if(native){summary.scope+=';native loopback nginx TLS endpoint, local puppeteer Chrome';summary.generator_cpu_scope='local puppeteer Chrome on shared VM;not sustained capacity evidence'}
 browser=await puppeteer.launch({headless:true,executablePath:executable,args:[...((relay||native)?['--host-resolver-rules=MAP capacity.example.test 127.0.0.1']:[]),...(native?['--no-sandbox']:relay?[]:['--no-sandbox']),'--no-proxy-server','--disable-background-networking','--disable-component-update','--ignore-certificate-errors-spki-list='+spki]});
 summary.browser_version=await browser.version();
 await Promise.all(Array.from({length:10},async(_,principal)=>{
  const context=await browser.createBrowserContext();const page=await context.newPage();let phase='cold';const begun=new Map();
  try{
   await page.setRequestInterception(true);
   page.on('request',req=>{
    const url=new URL(req.url());
    if(['data:','blob:'].includes(url.protocol)){req.continue();return}
    if(url.origin!==target.base){summary.blocked_external++;req.abort();return}
    begun.set(req,performance.now());req.continue();
   });
   page.on('response',res=>{
    const req=res.request(),url=new URL(res.url());if(url.origin!==target.base)return;
    rows.push({principal,phase,path:url.pathname,kind:url.pathname.startsWith('/api/')?'api':url.pathname.startsWith('/assets/')?'asset':req.isNavigationRequest()?'document':'other',status:res.status(),from_cache:res.fromCache(),from_service_worker:res.fromServiceWorker(),redirect_depth:req.redirectChain().length,latency_to_headers_ms:performance.now()-(begun.get(req)||performance.now())});
   });
   page.on('requestfailed',req=>{const url=new URL(req.url());if(url.origin===target.base)rows.push({principal,phase,path:url.pathname,kind:url.pathname.startsWith('/api/')?'api':url.pathname.startsWith('/assets/')?'asset':'other',status:0,from_cache:false,failure_code:(req.failure()?.errorText||'').match(/^net::[A-Z_]+$/)?.[0]||'unclassified'})});
   page.on('pageerror',()=>summary.page_errors++);
   for(phase of ['cold','warm']){
    const started=performance.now();
    const response=phase==='cold'?await page.goto(target.base,{waitUntil:'networkidle2',timeout:45000}):await page.reload({waitUntil:'networkidle2',timeout:45000});
    const ui=await page.evaluate(()=>({root_children:document.querySelector('#root')?.childElementCount||0,text_length:document.querySelector('#root')?.textContent?.trim().length||0,images:[...document.images].length,broken_images:[...document.images].filter(i=>i.complete&&i.naturalWidth===0).length,navigation:performance.getEntriesByType('navigation').map(n=>({dom_content_loaded_ms:n.domContentLoadedEventEnd,load_ms:n.loadEventEnd,tls_ms:n.secureConnectionStart>0?n.connectEnd-n.secureConnectionStart:null,transfer_bytes:n.transferSize}))}));
    pages.push({principal,phase,status:response?.status(),elapsed_ms:performance.now()-started,...ui});
    if(response?.status()!==200||!ui.root_children||ui.text_length<10||ui.broken_images)summary.failed_gates.push('render_'+principal+'_'+phase);
   }
  }catch{summary.failed_gates.push('navigation_'+principal)}finally{await context.close()}
 }));
 if(rows.some(r=>r.status===0||r.status>=400)||summary.page_errors||summary.blocked_external)summary.failed_gates.push('network_or_ui_errors');
 if(pages.length!==20)summary.failed_gates.push('incomplete_cold_warm_cohort');
}catch{summary.failed_gates.push('browser_setup')}
finally{
 await browser?.close();summary.requests=rows.length;summary.network_responses=rows.filter(isNetworkResponse).length;
 summary.counts={};for(const phase of ['cold','warm']){summary.counts[phase]={};for(const kind of ['api','asset','document','other'])summary.counts[phase][kind]=rows.filter(r=>r.phase===phase&&r.kind===kind&&isNetworkResponse(r)).length;}
 fs.writeFileSync(output,JSON.stringify(summary,null,2));process.exitCode=summary.failed_gates.length?1:0;
}
