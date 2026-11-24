import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { writeFile } from 'node:fs/promises';
import { test } from 'node:test';
import { build } from 'esbuild';
import puppeteer from 'puppeteer';

test('staff discover and reprint issued codes, retry one job, and open only approved barcode pages', async () => {
  const code1 = 'A0C00001-X0001';
  const code2 = 'A0C00001-X0002';
  const body = { productId: 'p1', variantId: 'sku1' };
  const requestPaths: string[] = [];
  const printRequests: Array<{ key: string; bearer: string; body: Record<string, unknown> }> = [];
  let sendWrongCode = false;
  const bundle = await build({
    write: false, bundle: true, platform: 'browser', format: 'iife', jsx: 'automatic',
    define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env': '{}' },
    stdin: { loader: 'tsx', resolveDir: process.cwd(), contents: `
      import React from 'react';
      import {createRoot} from 'react-dom/client';
      import {SPNCXLabelPanel} from './src/components/common/SPNCXLabelPanel';
      localStorage.setItem('seler_sho_jwt_token','staff-fixture');
      createRoot(document.getElementById('root')).render(<SPNCXLabelPanel scope="warehouse"/>);
    ` },
  });
  const server = createServer(async (req, res) => {
    const url = req.url ?? '/';
    if (url === '/bundle.js') {
      res.setHeader('Content-Type', 'text/javascript');
      res.end(bundle.outputFiles![0].text);
      return;
    }
    if (!url.startsWith('/api/')) {
      res.setHeader('Content-Type', 'text/html');
      res.end('<!doctype html><html lang="fa" dir="rtl"><meta charset="utf-8"><div id="root"></div><script src="/bundle.js"></script>');
      return;
    }
    const chunks: Buffer[] = [];
    for await (const chunk of req) chunks.push(chunk);
    const payload = JSON.parse(Buffer.concat(chunks).toString());
    requestPaths.push(url);
    res.setHeader('Content-Type', 'application/json');
    if (url.endsWith('/unprinted')) {
      assert.deepEqual(payload, { spnc: 'A0C00001', limit: 2 });
      assert.equal(req.headers.authorization, 'Bearer staff-fixture');
      res.end(JSON.stringify({ ...body, codes: [code1, code2], hasMore: false }));
      return;
    }
    if (url.endsWith('/lookup')) {
      res.end(JSON.stringify({ ...body, code: payload.code, status: 'in_stock', printedAt: '2026-11-24T00:00:00Z', printCount: 1 }));
      return;
    }
    printRequests.push({ key: String(req.headers['idempotency-key']), bearer: String(req.headers.authorization), body: payload });
    if (printRequests.length === 1) {
      res.statusCode = 503;
      res.end(JSON.stringify({ error: 'spncx_unavailable' }));
      return;
    }
    res.end(JSON.stringify({
      ...payload, id: `label-job-${printRequests.length}`, barcodeFormat: 'CODE128',
      codes: sendWrongCode ? ['FOREIGN-X0010'] : payload.codes, createdAt: '2026-11-24T00:00:00Z',
    }));
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  const browser = await puppeteer.launch({
    headless: true, args: ['--no-sandbox', '--disable-setuid-sandbox'],
    ...(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : {}),
  });
  try {
    const page = await browser.newPage();
    await page.evaluateOnNewDocument(() => {
      // Staff networks can serve the app on a plain HTTP origin.
      Object.defineProperty(crypto, 'randomUUID', { configurable: true, value: undefined });
      const open = window.open.bind(window);
      window.open = (...args) => {
        const popup = open(...args);
        if (popup) popup.print = () => { popup.document.documentElement.dataset.printRequested = 'yes'; };
        return popup;
      };
    });
    await page.goto(`http://127.0.0.1:${(server.address() as {port:number}).port}`);
    await page.type('#spncx-sku-code', 'A0C00001');
    await page.$eval('#spncx-label-count', input => {
      (input as HTMLInputElement).select();
    });
    await page.type('#spncx-label-count', '2');
    await page.click('[data-action=discover-labels]');
    await page.waitForSelector('[data-action=prepare-labels]:not([disabled])');
    await page.click('[data-action=prepare-labels]');
    await page.waitForFunction(() => document.body.textContent?.includes('نتیجه ثبت مشخص نیست'));
    assert.equal(await page.$('[data-action=open-label-print]'), null);
    await page.click('[data-action=prepare-labels]');
    await page.waitForSelector('[data-action=open-label-print]');
    assert.equal(printRequests[0].key, printRequests[1].key);
    assert.ok(printRequests[0].key && printRequests[0].key !== 'undefined');
    assert.deepEqual(printRequests[0].body, { ...body, codes: [code1, code2], reprint: false });
    assert.equal(printRequests[0].bearer, 'Bearer staff-fixture');
    const oldPages = await browser.pages();
    await page.click('[data-action=open-label-print]');
    const target = await browser.waitForTarget(t => t.type() === 'page' && !oldPages.some(old => old.target() === t));
    const print = await target.page();
    assert.ok(print);
    await print.waitForSelector('[data-print-requested=yes]');
    assert.deepEqual(await print.$$eval('.spncx-sheet code', nodes => nodes.map(n => n.textContent)), [code1, code2]);
    assert.equal(await print.$$eval('.spncx-sheet svg path', elements => elements.length), 2);
    const pdf = await print.pdf({ preferCSSPageSize: true, printBackground: true });
    assert.equal(Buffer.from(pdf).toString('latin1').match(/\/Type \/Page\b/g)?.length, 2);
    if (process.env.SPNCX_PRINT_TEST_PDF_PATH) await writeFile(process.env.SPNCX_PRINT_TEST_PDF_PATH, pdf);
    await print.close();
    await page.click('[data-action=start-labels]');
    await page.click('[data-action=known-labels]');
    await page.type('#spncx-known-codes', code1);
    await page.click('[data-action=verify-labels]');
    await page.waitForSelector('[data-action=prepare-labels]:not([disabled])');
    sendWrongCode = true;
    await page.click('[data-action=prepare-labels]');
    await page.waitForFunction(() => document.body.textContent?.includes('نتیجه ثبت مشخص نیست'));
    assert.equal(await page.$('[data-action=open-label-print]'), null);
    sendWrongCode = false;
    await page.click('[data-action=prepare-labels]');
    await page.waitForSelector('[data-action=open-label-print]');
    assert.deepEqual(printRequests.at(-1)?.body, { ...body, codes: [code1], reprint: true });
    assert.equal(printRequests.at(-2)?.key, printRequests.at(-1)?.key);
    assert.notEqual(printRequests[1].key, printRequests.at(-1)?.key);
    assert.ok(requestPaths.every(path => path.startsWith('/api/v1/warehouse/inventory/spncx/')));
    await page.evaluate(() => window.dispatchEvent(new Event('drup-session-expired')));
    await page.waitForSelector('[data-action=open-label-print]', { hidden: true });
  } finally {
    await browser.close();
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
});
