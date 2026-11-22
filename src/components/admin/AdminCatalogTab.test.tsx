import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { build } from 'esbuild';
import puppeteer, { type Page } from 'puppeteer';

// Batch 4a, D2: AdminCatalogTab refreshProposals dedupe + stale-while-
// revalidate + honest timeout failure. The tab is rendered live in a
// headless browser against a local HTTP server that counts proposals
// requests and controls when they resolve, so the dedupe / in-flight
// guard is exercised through the real apiRequest (15 s timeout included)
// instead of a mocked never-resolving apiRequest (which proves nothing).

const ROW = {
  id: 'prop-1', title: 'کالای تست', supplierId: 'sup-1',
  supplierName: 'تأمین‌کننده تست', supplierBaseCost: 1000,
  status: 'pending', barcode: '123', category: 'test',
};

async function waitFor(fn: () => boolean | Promise<boolean>, timeoutMs = 30000, stepMs = 100) {
  const start = Date.now();
  for (;;) {
    if (await fn()) return;
    if (Date.now() - start > timeoutMs) throw new Error('waitFor timed out');
    await new Promise((r) => setTimeout(r, stepMs));
  }
}

test('D2 dedupe, stale-while-revalidate, focus debounce and timeout error', { timeout: 120000 }, async () => {
  const bundle = await build({
    write: false, bundle: true, platform: 'browser', format: 'iife', jsx: 'automatic',
    define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env': '{}' },
    plugins: [{
      name: 'fixtures', setup(b) {
        b.onResolve({ filter: /(?:^|\/)StoreContext$/ }, () => ({ path: 'store', namespace: 'fixture' }));
        b.onResolve({ filter: /(?:^|\/)ToastContext$/ }, () => ({ path: 'toast', namespace: 'fixture' }));
        b.onResolve({ filter: /(?:^|\/)AuthContext$/ }, () => ({ path: 'auth', namespace: 'fixture' }));
        b.onResolve({ filter: /(?:^|\/)ProductContext$/ }, () => ({ path: 'products', namespace: 'fixture' }));
        b.onLoad({ filter: /.*/, namespace: 'fixture' }, (a) => ({
          loader: 'js',
          contents: a.path === 'store'
            ? `export const useStore=()=>({products:[],proposedProducts:[],approveAndMergeProduct:async()=>{},rejectProposedProduct:()=>{},toggleProductOriginality:()=>{},formatPrice:String,toPersianDigits:String,setActiveView:()=>{}});`
            : a.path === 'toast'
              ? `export const useToast=()=>({success:()=>{},warning:()=>{},error:()=>{}});`
              : a.path === 'auth'
                ? `export const useAuth=()=>({authToken:'test-token'});`
                : `export const useProducts=()=>({fetchProducts:async()=>{}});`,
        }));
      },
    }],
    stdin: {
      loader: 'tsx', resolveDir: process.cwd(),
      contents: `import React from'react';import{createRoot}from'react-dom/client';import{AdminCatalogTab}from'./src/components/admin/AdminCatalogTab';createRoot(document.getElementById('root')).render(<AdminCatalogTab/>);`,
    },
  });

  let mode: 'hold' | 'rows' | 'empty' | 'error500' | 'slow' = 'hold';
  let proposalRequests = 0;
  const held: Array<import('node:http').ServerResponse> = [];
  const respond = (res: import('node:http').ServerResponse, m: typeof mode) => {
    try {
      if (m === 'rows') { res.setHeader('Content-Type', 'application/json'); res.statusCode = 200; res.end(JSON.stringify([ROW])); }
      else if (m === 'empty') { res.setHeader('Content-Type', 'application/json'); res.statusCode = 200; res.end('[]'); }
      else if (m === 'error500') { res.setHeader('Content-Type', 'application/json'); res.statusCode = 500; res.end(JSON.stringify({ error: 'boom' })); }
      else { res.setHeader('Content-Type', 'application/json'); res.statusCode = 200; res.end('[]'); }
    } catch { /* client aborted (timeout path) — nothing to do */ }
  };
  const server = createServer((req, res) => {
    if (req.url === '/api/v1/admin/products/proposals') {
      proposalRequests++;
      if (mode === 'hold') { held.push(res); return; }
      if (mode === 'slow') { setTimeout(() => respond(res, 'empty'), 16000); return; }
      respond(res, mode);
      return;
    }
    res.setHeader('Content-Type', 'text/html');
    res.end('<div id="root"></div><script>' + bundle.outputFiles[0].text + '</script>');
  });
  await new Promise<void>((r) => server.listen(0, '127.0.0.1', r));
  const address = server.address(); assert.ok(address && typeof address === 'object');
  const browser = await puppeteer.launch({ headless: true, args: ['--no-sandbox', '--disable-setuid-sandbox'] });
  const text = async (page: Page) => await page.$eval('#root', (e) => e.textContent || '');
  const clickRefresh = async (page: Page) => {
    await page.evaluate(() => {
      const b = [...document.querySelectorAll('button')].find((x) => x.textContent?.includes('به‌روزرسانی صف بازبینی'))!;
      b.click();
    });
  };
  try {
    const page = await browser.newPage();
    const url = `http://127.0.0.1:${address.port}`;

    // --- dedupe: mount fetch in flight; focus storm + a focus after the
    // 10 s debounce window must not start a second request (the second
    // focus reaches refreshProposals only because the debounce passed, so
    // the in-flight guard is what blocks it).
    await page.goto(url);
    await waitFor(() => proposalRequests >= 1);
    assert.equal(proposalRequests, 1);
    assert.match(await text(page), /در حال دریافت درخواست‌ها…/);
    await page.evaluate(() => { for (let i = 0; i < 3; i++) window.dispatchEvent(new Event('focus')); });
    await new Promise((r) => setTimeout(r, 500));
    assert.equal(proposalRequests, 1, 'focus storm within 10 s must not refetch');
    await new Promise((r) => setTimeout(r, 11000));
    await page.evaluate(() => window.dispatchEvent(new Event('focus')));
    await new Promise((r) => setTimeout(r, 500));
    assert.equal(proposalRequests, 1, 'background refresh must skip while a fetch is in flight');

    // --- release the mount fetch: the row renders, spinner clears.
    mode = 'rows';
    held.splice(0).forEach((res) => respond(res, 'rows'));
    await page.waitForFunction(() => document.getElementById('root')?.textContent?.includes('کالای تست'));
    assert.doesNotMatch(await text(page), /در حال دریافت درخواست‌ها…/);

    // --- stale-while-revalidate: a refresh with rows already loaded keeps
    // the row visible and shows only the small inline indicator.
    mode = 'hold';
    await clickRefresh(page);
    await waitFor(() => proposalRequests >= 2);
    assert.equal(proposalRequests, 2);
    const during = await text(page);
    assert.match(during, /کالای تست/, 'background refresh must keep the last good list visible');
    assert.match(during, /در حال به‌روزرسانی…/);
    assert.doesNotMatch(during, /در حال دریافت درخواست‌ها…/);
    mode = 'rows';
    held.splice(0).forEach((res) => respond(res, 'rows'));
    await page.waitForFunction(() => !(document.getElementById('root')?.textContent?.includes('در حال به‌روزرسانی…')));
    assert.match(await text(page), /کالای تست/);

    // --- timeout: the real 15 s apiRequest timeout must land in the error
    // box with the elapsed time, and the refresh button must be enabled again.
    mode = 'slow';
    await page.reload();
    await waitFor(() => proposalRequests >= 3);
    await page.waitForFunction(() => document.querySelector('[role="alert"]'), { timeout: 30000 });
    const errText = await text(page);
    assert.match(errText, /بارگذاری صف بازبینی ناموفق بود/);
    assert.match(errText, /ثانیه/, 'timeout error must carry the elapsed time');
    const disabled = await page.evaluate(() => {
      const b = [...document.querySelectorAll('button')].find((x) => x.textContent?.includes('به‌روزرسانی صف بازبینی'))!;
      return (b as HTMLButtonElement).disabled;
    });
    assert.equal(disabled, false, 'refresh button must not stay disabled without a visible error');

    // --- empty state, then a 500 lands in the same error box.
    mode = 'empty';
    await page.reload();
    await page.waitForFunction(() => document.getElementById('root')?.textContent?.includes('درخواستی با این فیلترها وجود ندارد.'));
    mode = 'error500';
    await page.reload();
    await page.waitForFunction(() => document.querySelector('[role="alert"]'));
    assert.match(await text(page), /بارگذاری صف بازبینی ناموفق بود/);
  } finally {
    held.splice(0).forEach((res) => { try { res.end('[]'); } catch { /* ignore */ } });
    await browser.close();
    await new Promise<void>((r) => server.close(() => r()));
  }
});
