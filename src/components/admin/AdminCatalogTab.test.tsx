import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import tailwindcss from '@tailwindcss/vite';
import { build } from 'esbuild';
import puppeteer, { type Page } from 'puppeteer';
import { build as buildStyles } from 'vite';

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

function buildCatalog() {
  return build({
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
}

test('D2 dedupe, stale-while-revalidate, focus debounce and timeout error', { timeout: 120000 }, async () => {
  const bundle = await buildCatalog();
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

test('mobile catalog review keeps field decisions available before saving and gates publication', { timeout: 60000 }, async () => {
  const [bundle, styles] = await Promise.all([
    buildCatalog(),
    buildStyles({ configFile: false, plugins: [tailwindcss()], logLevel: 'silent', build: { write: false, rollupOptions: { input: 'src/index.css' } } }),
  ]);
  const outputs = (Array.isArray(styles) ? styles : [styles]).flatMap(result => 'output' in result ? result.output : []);
  const css = outputs.find(output => output.type === 'asset' && output.fileName.endsWith('.css'));
  assert.ok(css && css.type === 'asset');
  const secondRow = { ...ROW, id: 'prop-2', title: 'کالای دوم', supplierId: 'sup-2', supplierName: 'تأمین‌کننده دوم' };
  let rows = [ROW, secondRow];
  let fields = [{ field_key: '/title', decision: 'pending', reason: '', value_hash: 'title-hash', value: ROW.title }];
  const decisions: Array<{ id: string; status: string; rejectionReason?: string }> = [];
  const fieldSaves: unknown[] = [];
  let rejectAttempts = 0;
  let rejectResponse: import('node:http').ServerResponse | undefined;
  const server = createServer(async (req, res) => {
    const path = req.url ?? '';
    res.setHeader('Content-Type', 'application/json');
    if (path === '/api/v1/admin/products/proposals') return res.end(JSON.stringify(rows));
    if (path.startsWith('/api/v1/admin/product-field-reviews/')) {
      const proposal = rows.find(row => path.endsWith(`/${row.id}`));
      if (req.method === 'PUT') {
        let body = '';
        for await (const chunk of req) body += chunk;
        const parsed = JSON.parse(body);
        fieldSaves.push(parsed);
        fields = parsed.fields;
      }
      return res.end(JSON.stringify({ proposal, fields: proposal?.id === ROW.id ? fields : [], history: [] }));
    }
    const decision = path.match(/^\/api\/v1\/admin\/approvals\/([^/]+)\/review$/);
    if (decision && req.method === 'POST') {
      let body = '';
      for await (const chunk of req) body += chunk;
      const parsed = JSON.parse(body);
      if (parsed.status === 'rejected' && ++rejectAttempts === 1) {
        rejectResponse = res;
        return;
      }
      decisions.push({ id: decision[1], ...parsed });
      rows = rows.map(row => row.id === decision[1] ? { ...row, status: parsed.status } : row);
      return res.end(JSON.stringify({ success: true, id: decision[1], status: parsed.status }));
    }
    if (path.startsWith('/api/')) return res.end('[]');
    if (path === '/catalog.css') {
      res.setHeader('Content-Type', 'text/css');
      return res.end(css.source);
    }
    res.setHeader('Content-Type', 'text/html; charset=utf-8');
    return res.end(`<meta name="viewport" content="width=device-width, initial-scale=1"><link rel="stylesheet" href="/catalog.css"><div id="root"></div><script>${bundle.outputFiles[0].text}</script>`);
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  assert.ok(address && typeof address === 'object');
  const browser = await puppeteer.launch({ headless: true, args: ['--no-sandbox', '--disable-setuid-sandbox'] });
  try {
    const page = await browser.newPage();
    await page.setViewport({ width: 320, height: 568, isMobile: true, hasTouch: true });
    await page.goto(`http://127.0.0.1:${address.port}`);
    await page.waitForFunction(() => document.getElementById('root')?.textContent?.includes('کالای تست'));
    assert.ok(await page.$('button[aria-label="بازبینی کالای تست"]'), 'a proposal should have one explicit review button');
    assert.equal(await page.$$eval('article button', buttons => buttons.filter(button => button.closest('article')?.textContent?.includes('کالای تست')).length), 1);
    await page.type('input[aria-label="جستجوی صف بازبینی"]', '123');
    await page.select('select[aria-label="فیلتر تأمین‌کننده"]', ROW.supplierId);
    await page.waitForFunction(() => !document.querySelector('button[aria-label="بازبینی کالای دوم"]'));
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth), 320);
    await page.tap('button[aria-label="بازبینی کالای تست"]');
    await page.waitForSelector('dialog[open] [data-field-key="/title"]');
    const publish = 'dialog button::-p-text(تأیید نهایی و انتشار)';
    assert.equal(await page.$eval(publish, button => (button as HTMLButtonElement).disabled), true);
    await page.tap('dialog [aria-label="رد و درخواست اصلاح نام کالا"]');
    await page.waitForSelector('textarea[aria-label="دلیل رد نام کالا"]');
    assert.equal(await page.$eval('dialog button::-p-text(ارسال برای اصلاح)', button => (button as HTMLButtonElement).disabled), true);
    await page.tap('dialog [aria-label="تأیید نام کالا"]');
    assert.equal(await page.$eval('dialog [aria-label="تأیید نام کالا"]', button => button.getAttribute('aria-pressed')), 'true');
    assert.equal(await page.$eval(publish, button => (button as HTMLButtonElement).disabled), false);
    const bounds = await page.$eval(publish, button => button.getBoundingClientRect().toJSON());
    assert.ok(bounds.height >= 44 && bounds.top >= 0 && bounds.bottom <= 568 && bounds.left >= 0 && bounds.right <= 320);
    await page.tap(publish);
    await page.waitForFunction(() => !document.querySelector('dialog[open]'));
    assert.deepEqual(fieldSaves, [{ fields: [{ field_key: '/title', decision: 'approved', reason: '', value_hash: 'title-hash', value: ROW.title }] }]);
    assert.deepEqual(decisions, [{ id: ROW.id, status: 'approved' }]);
    await page.waitForFunction(() => !document.querySelector('button[aria-label="بازبینی کالای تست"]'));
    await page.select('select[aria-label="فیلتر تأمین‌کننده"]', 'all');
    await page.tap('button[aria-label="بازبینی کالای دوم"]');
    await page.tap('dialog button::-p-text(رد کل کالا)');
    const reject = 'dialog button::-p-text(تأیید رد کالا)';
    assert.equal(await page.$eval(reject, button => (button as HTMLButtonElement).disabled), true);
    await page.type('dialog textarea[aria-label="دلیل رد کالا"]', 'تصویر اصلی باید اصلاح شود');
    await page.tap(reject);
    await waitFor(() => !!rejectResponse);
    await page.keyboard.press('Escape');
    assert.ok(await page.$('dialog[open]'), 'Escape must keep the decision open while the request is pending');
    rejectResponse?.writeHead(500).end('{"error":"ثبت تصمیم انجام نشد"}');
    await page.waitForFunction(() => Array.from(document.querySelectorAll('dialog [role="alert"]')).some(alert => alert.textContent?.includes('ثبت تصمیم انجام نشد')));
    assert.equal(await page.$eval('dialog textarea[aria-label="دلیل رد کالا"]', el => (el as HTMLTextAreaElement).value), 'تصویر اصلی باید اصلاح شود');
    await page.tap(reject);
    await page.waitForFunction(() => !document.querySelector('dialog[open]'));
    assert.deepEqual(decisions[1], { id: secondRow.id, status: 'rejected', rejectionReason: 'تصویر اصلی باید اصلاح شود' });
    await page.select('select[aria-label="فیلتر وضعیت"]', 'rejected');
    await page.waitForSelector('button[aria-label="بازبینی کالای دوم"]');
  } finally {
    rejectResponse?.end();
    await browser.close();
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
});
