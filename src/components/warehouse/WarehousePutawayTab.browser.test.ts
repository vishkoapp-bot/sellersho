import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { build } from 'esbuild';
import puppeteer from 'puppeteer';

test('putaway shows the exact SKU and verifies that SKU rather than its parent', { timeout: 90000 }, async () => {
  const bundle = await build({ write: false, bundle: true, platform: 'browser', format: 'iife', jsx: 'automatic', plugins: [{ name: 'staff-session', setup(build) { build.onResolve({ filter: /context\/StoreContext$/ }, () => ({ path: 'staff-session', namespace: 'test' })); build.onLoad({ filter: /.*/, namespace: 'test' }, () => ({ contents: 'export const useStore=()=>({warehouseSession:{role:"warehouse_staff"},adminSession:null});', loader: 'js' })); } }], define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env': '{}' }, stdin: { loader: 'tsx', resolveDir: process.cwd(), contents: `import React from 'react';import {createRoot} from 'react-dom/client';import {ToastProvider} from './src/context/ToastContext';import {WarehousePutawayTab} from './src/components/warehouse/WarehousePutawayTab';createRoot(document.getElementById('root')).render(<ToastProvider><WarehousePutawayTab onGoToPicking={()=>{}}/></ToastProvider>);` } });
  const payloads: unknown[] = [];
  const server = createServer(async (request, response) => {
    response.setHeader('Content-Type', 'application/json');
    if (request.url === '/api/v1/warehouse/putaway/queue') response.end(JSON.stringify({ success: true, queue: [{ id: 'task-red', productId: 'same-product', title: 'Product', variantId: 'sku-red', variantName: 'Red large', variantSpnc: 'SPNC-RED', quantity: 1, status: 'pending', measurementStatus: 'unverified', measurementSource: 'sku', lengthMm: 100, widthMm: 100, heightMm: 100, weightGrams: 1000 }] }));
    else if (request.url === '/api/v1/warehouse/bins') response.end('[]');
    else if (request.method === 'POST' && request.url === '/api/v1/warehouse/products/same-product/measurements') {
      let raw = ''; for await (const chunk of request) raw += chunk;
      payloads.push(JSON.parse(raw)); response.end('{"success":true,"measurementStatus":"verified"}');
    } else if (request.url?.startsWith('/api/')) response.end('{"suggestions":[]}');
    else { response.setHeader('Content-Type', 'text/html'); response.end('<html><body><div id="root"></div><script>' + bundle.outputFiles[0].text + '</script></body></html>'); }
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  const address = server.address(); assert.ok(address && typeof address === 'object');
  const browser = await puppeteer.launch({ headless: true, ...(process.platform === 'win32' ? { channel: 'chrome' as const } : {}) });
  try {
    const page = await browser.newPage(); await page.goto(`http://127.0.0.1:${address.port}/__inv08`);
    await page.waitForSelector('section input');
    assert.match(await page.$eval('section', element => (element as HTMLElement).innerText), /Red large.*SPNC-RED/s);
    await page.$eval('input[aria-label="علت اندازه‌گیری"]', element => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(element, 'measured red SKU');
      element.dispatchEvent(new Event('input', { bubbles: true }));
    });
    await Promise.all([page.waitForResponse(response => response.url().endsWith('/measurements') && response.request().method() === 'POST'), page.click('section button')]);
    assert.deepEqual(payloads, [{ variantId: 'sku-red', lengthMm: 100, widthMm: 100, heightMm: 100, weightGrams: 1000, reason: 'measured red SKU' }]);
  } finally { await browser.close(); server.closeAllConnections(); await new Promise<void>(resolve => server.close(() => resolve())); }
});
