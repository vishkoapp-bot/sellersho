import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { build } from 'esbuild';
import puppeteer from 'puppeteer';

// Exercises the shipped component and real API client in Chromium. The fake
// HTTP peer only supplies counted progress and a response-loss boundary.
test('partial QC refresh preserves cumulative counts and a lost response retries the same line payload', { timeout: 90000 }, async () => {
  const bundle = await build({ write: false, bundle: true, platform: 'browser', format: 'iife', jsx: 'automatic', define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env': '{}' }, stdin: { loader: 'tsx', resolveDir: process.cwd(), contents: `import React from 'react';import {createRoot} from 'react-dom/client';import {ToastProvider} from './src/context/ToastContext';import {WarehouseQCTab} from './src/components/warehouse/WarehouseQCTab';createRoot(document.getElementById('root')).render(<ToastProvider><WarehouseQCTab onGoToPutaway={()=>{}}/></ToastProvider>);` } });
  let passed = 0; let failed = 0; let loseResponse = true;
  const payloads: any[] = [];
  const server = createServer(async (request, response) => {
    response.setHeader('Content-Type', 'application/json');
    if (request.url === '/api/v1/warehouse/qc/queue') {
      response.end(JSON.stringify({ success: true, queue: passed + failed === 5 ? [] : [{ id: 'line-one', receiptLineId: 'IL-one', variantId: 'sku-red', productId: 'p', productTitle: 'Counted receipt', inboundId: 'INB-one', supplierName: 'Supplier', expectedQuantity: 5, receivedQuantity: 5, passedQuantity: passed, failedQuantity: failed, shortageQuantity: 0, remainingQuantity: 5 - passed - failed, createdAt: '2026-11-14T00:00:00Z' }] }));
    } else if (request.url === '/api/v1/warehouse/qc/p/complete') {
      let raw = ''; for await (const chunk of request) raw += chunk;
      const payload = JSON.parse(raw); payloads.push(payload);
      passed = payload.passedQuantity; failed = payload.failedQuantity;
      if (failed === 2 && loseResponse) { loseResponse = false; response.end('{"success":'); }
      else { response.end(JSON.stringify({ success: true, status: 'completed' })); }
    } else {
      response.setHeader('Content-Type', 'text/html');
      response.end('<html><body><div id="root"></div><script>' + bundle.outputFiles[0].text + '</script></body></html>');
    }
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  assert.ok(address && typeof address === 'object');
  const browser = await puppeteer.launch({ headless: true, ...(process.platform === 'win32' ? { channel: 'chrome' as const } : {}) }).catch(async e => { server.closeAllConnections(); await new Promise<void>(resolve => server.close(() => resolve())); throw e; });
  try {
    const page = await browser.newPage();
    await page.goto(`http://127.0.0.1:${address.port}/__inv04_qc`);
    await page.waitForSelector('section input');
    const fill = async (selector: string, value: string) => {
      await page.$eval(selector, (element, text) => {
        const input = element as HTMLInputElement;
        Object.getOwnPropertyDescriptor(input instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype, 'value')!.set!.call(input, text);
        input.dispatchEvent(new Event('input', { bubbles: true }));
      }, value);
    };
    await fill('section input[type=number]:nth-of-type(1)', '3');
    await fill('section textarea', 'first partial inspection');
    await page.click('section button[data-qc-submit]');
    await page.waitForFunction(() => !document.querySelector('section button[data-qc-submit]:disabled'));
    assert.equal(payloads.length, 1);
    await page.reload();
    await page.waitForSelector('section input');
    // A remounted form must show the accepted 3, not overwrite it with 5.
    assert.deepEqual(await page.$$eval('section input[type=number]', inputs => inputs.map(input => (input as HTMLInputElement).value)), ['3', '0', '0']);
    await fill('section label:nth-child(2) input', '2');
    await fill('section textarea', 'remaining two failed');
    await page.click('section button[data-qc-submit]');
    await page.waitForFunction(() => !document.querySelector('section button[data-qc-submit]:disabled'));
    assert.equal(payloads.length, 2);
    await page.click('section button[data-qc-submit]');
    await page.waitForFunction(() => !document.querySelector('section'));
    assert.equal(payloads.length, 3);
    assert.deepEqual(payloads[1], { inboundId: 'INB-one', receiptLineId: 'IL-one', passedQuantity: 3, failedQuantity: 2, shortageQuantity: 0, defectReason: 'remaining two failed' });
    assert.deepEqual(payloads[2], payloads[1]);
  } finally { await browser.close(); server.closeAllConnections(); await new Promise<void>(resolve => server.close(() => resolve())); }
});

test('equal-quantity SKU rows expose exact identities and submit only the visibly selected receipt line', { timeout: 90000 }, async () => {
  const bundle = await build({ write: false, bundle: true, platform: 'browser', format: 'iife', jsx: 'automatic', define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env': '{}' }, stdin: { loader: 'tsx', resolveDir: process.cwd(), contents: `import React from 'react';import {createRoot} from 'react-dom/client';import {ToastProvider} from './src/context/ToastContext';import {WarehouseQCTab} from './src/components/warehouse/WarehouseQCTab';createRoot(document.getElementById('root')).render(<ToastProvider><WarehouseQCTab onGoToPutaway={()=>{}}/></ToastProvider>);` } });
  const queue = [
    { id: 'IL-red', receiptLineId: 'IL-red', variantId: 'sku-red' },
    { id: 'IL-blue', receiptLineId: 'IL-blue', variantId: 'sku-blue' },
    { id: 'IL-legacy', receiptLineId: 'IL-legacy' },
  ].map(item => ({ ...item, productId: 'same-product', productTitle: 'Same product title', inboundId: 'INB-same', supplierName: 'Same supplier', expectedQuantity: 2, receivedQuantity: 2, passedQuantity: 0, failedQuantity: 0, shortageQuantity: 0, remainingQuantity: 2, createdAt: '2026-11-14T00:00:00Z' }));
  const payloads: any[] = [];
  const server = createServer(async (request, response) => {
    response.setHeader('Content-Type', 'application/json');
    if (request.url === '/api/v1/warehouse/qc/queue') {
      response.end(JSON.stringify({ success: true, queue: queue.filter(item => !payloads.some(payload => payload.receiptLineId === item.receiptLineId)) }));
    } else if (request.url === '/api/v1/warehouse/qc/same-product/complete') {
      let raw = ''; for await (const chunk of request) raw += chunk;
      payloads.push(JSON.parse(raw));
      response.end(JSON.stringify({ success: true, status: 'completed' }));
    } else {
      response.setHeader('Content-Type', 'text/html');
      response.end('<html><body><div id="root"></div><script>' + bundle.outputFiles[0].text + '</script></body></html>');
    }
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  assert.ok(address && typeof address === 'object');
  const browser = await puppeteer.launch({ headless: true, ...(process.platform === 'win32' ? { channel: 'chrome' as const } : {}) }).catch(async e => { server.closeAllConnections(); await new Promise<void>(resolve => server.close(() => resolve())); throw e; });
  try {
    const page = await browser.newPage();
    await page.goto(`http://127.0.0.1:${address.port}/__inv04_skus`);
    await page.waitForSelector('section input');
    const visibleRows = await page.$$eval('section', rows => rows.map(row => (row as HTMLElement).innerText));
    assert.ok(visibleRows.some(row => row.includes('sku-red') && row.includes('IL-red')), 'red SKU and exact receipt-line identity must be visible');
    assert.ok(visibleRows.some(row => row.includes('sku-blue') && row.includes('IL-blue')), 'blue SKU and exact receipt-line identity must be visible');
    assert.ok(visibleRows.some(row => row.includes('بدون SKU') && row.includes('IL-legacy')), 'legacy row must explicitly identify the absence of a SKU');
    const visibleRow = async (sku: string) => {
      const rows = await page.$$('section');
      for (const row of rows) if ((await row.evaluate(element => (element as HTMLElement).innerText)).includes(sku)) return row;
      throw new Error(`Visible SKU row not found: ${sku}`);
    };
    const fill = async (row: Awaited<ReturnType<typeof visibleRow>>, selector: string, value: string) => {
      await row.$eval(selector, (element, text) => {
        const input = element as HTMLInputElement;
        Object.getOwnPropertyDescriptor(input instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype, 'value')!.set!.call(input, text);
        input.dispatchEvent(new Event('input', { bubbles: true }));
      }, value);
    };
    const blue = await visibleRow('sku-blue');
    assert.equal(await blue.$('input[type=number]'), null);
    await blue.$eval('button[data-qc-open]', button => (button as HTMLButtonElement).click());
    await fill(blue, 'label:nth-child(1) input', '0');
    await fill(blue, 'label:nth-child(2) input', '2');
    await fill(blue, 'textarea', 'blue failed inspection');
    await blue.$eval('button[data-qc-submit]', button => (button as HTMLButtonElement).click());
    await page.waitForFunction(() => document.querySelectorAll('section').length === 2);
    assert.deepEqual(payloads, [{ inboundId: 'INB-same', receiptLineId: 'IL-blue', passedQuantity: 0, failedQuantity: 2, shortageQuantity: 0, defectReason: 'blue failed inspection' }]);
    const red = await visibleRow('sku-red');
    assert.deepEqual(await red.$$eval('input[type=number]', inputs => inputs.map(input => (input as HTMLInputElement).value)), ['2', '0', '0']);
    assert.equal(await red.$eval('textarea', textarea => (textarea as HTMLTextAreaElement).value), '');
    await fill(red, 'textarea', 'red passed inspection');
    await red.$eval('button[data-qc-submit]', button => (button as HTMLButtonElement).click());
    await page.waitForFunction(() => document.querySelectorAll('section').length === 1);
    assert.deepEqual(payloads[1], { inboundId: 'INB-same', receiptLineId: 'IL-red', passedQuantity: 2, failedQuantity: 0, shortageQuantity: 0, defectReason: 'red passed inspection' });
    assert.ok((await page.$eval('section', row => (row as HTMLElement).innerText)).includes('IL-legacy'));
  } finally { await browser.close(); server.closeAllConnections(); await new Promise<void>(resolve => server.close(() => resolve())); }
});
