import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { build } from 'esbuild';
import puppeteer from 'puppeteer';

test('putaway verifies the exact SKU, reserves its shelf and opens that shelf for verification', { timeout: 90000 }, async () => {
  const bundle = await build({ write: false, bundle: true, platform: 'browser', format: 'iife', jsx: 'automatic', plugins: [{ name: 'staff-session', setup(build) { build.onResolve({ filter: /context\/StoreContext$/ }, () => ({ path: 'staff-session', namespace: 'test' })); build.onLoad({ filter: /.*/, namespace: 'test' }, () => ({ contents: 'export const useStore=()=>({warehouseSession:{role:"warehouse_staff"},adminSession:null});', loader: 'js' })); } }], define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env': '{}' }, stdin: { loader: 'tsx', resolveDir: process.cwd(), contents: `import React from 'react';import {createRoot} from 'react-dom/client';import {ToastProvider} from './src/context/ToastContext';import {WarehousePutawayTab} from './src/components/warehouse/WarehousePutawayTab';createRoot(document.getElementById('root')).render(<ToastProvider><WarehousePutawayTab onGoToPicking={()=>{}}/></ToastProvider>);` } });
  const payloads: unknown[] = [];
  const reservations: unknown[] = [];
  let measurementStatus = 'unverified';
  let binLocation = '';
  let completed = false;
  const server = createServer(async (request, response) => {
    response.setHeader('Content-Type', 'application/json');
    if (request.url === '/api/v1/warehouse/putaway/queue') response.end(JSON.stringify({ success: true, queue: completed ? [] : [{ id: 'task-red', productId: 'same-product', title: 'Product', variantId: 'sku-red', variantName: 'Red large', variantSpnc: 'SPNC-RED', quantity: 1, status: binLocation ? 'assigned' : 'pending', binLocation, measurementStatus, measurementSource: 'sku', lengthMm: 100, widthMm: 100, heightMm: 100, weightGrams: 1000 }] }));
    else if (request.url === '/api/v1/warehouse/bins') response.end(JSON.stringify(['Shelf-Mixed-01', 'SHELF-MIXED-01', 'BIN-02'].map(binCode => ({
      binCode, capacityStatus: 'verified', remainingVolumeCm3: binCode === binLocation ? 0 : 3000, warehouseName: 'Central',
      hallName: 'Hall 1', aisle: 'Aisle 4', shelf: 'Level 2', capacityPercent: 50, quantity: completed && binCode === binLocation ? 1 : 0,
      productTitle: completed && binCode === binLocation ? 'Product' : undefined, updatedAt: '2026-01-01T00:00:00Z',
    }))));
    else if (request.method === 'POST' && request.url === '/api/v1/warehouse/products/same-product/measurements') {
      let raw = ''; for await (const chunk of request) raw += chunk;
      payloads.push(JSON.parse(raw)); measurementStatus = 'verified'; response.end('{"success":true,"measurementStatus":"verified"}');
    } else if (request.method === 'POST' && request.url === '/api/v1/warehouse/putaway/tasks/task-red/assign') {
      let raw = ''; for await (const chunk of request) raw += chunk;
      const reservation = JSON.parse(raw);
      reservations.push(reservation); binLocation = reservation.binLocation; response.end('{"success":true}');
    } else if (request.method === 'POST' && request.url === '/api/v1/warehouse/putaway/task-red/confirm') {
      completed = true; response.end('{"success":true}');
    } else if (request.url?.startsWith('/api/')) response.end('{"suggestions":[]}');
    else { response.setHeader('Content-Type', 'text/html'); response.end('<html><body><div id="root"></div><script>' + bundle.outputFiles[0].text + '</script></body></html>'); }
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  const address = server.address(); assert.ok(address && typeof address === 'object');
  const browser = await puppeteer.launch({ headless: true, ...(process.platform === 'win32' ? { channel: 'chrome' as const } : {}) }).catch(async e => { server.closeAllConnections(); await new Promise<void>(resolve => server.close(() => resolve())); throw e; });
  try {
    const page = await browser.newPage(); await page.goto(`http://127.0.0.1:${address.port}/__inv08`);
    await page.waitForSelector('section input');
    assert.match(await page.$eval('section', element => (element as HTMLElement).innerText), /Red large.*SPNC-RED/s);
    assert.equal(await page.$('section select'), null, 'unverified SKU must be measured before choosing a destination');
    await page.$eval('input[aria-label="علت اندازه‌گیری"]', element => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(element, 'measured red SKU');
      element.dispatchEvent(new Event('input', { bubbles: true }));
    });
    await Promise.all([page.waitForResponse(response => response.url().endsWith('/measurements') && response.request().method() === 'POST'), page.click('section button[type="submit"]')]);
    assert.deepEqual(payloads, [{ variantId: 'sku-red', lengthMm: 100, widthMm: 100, heightMm: 100, weightGrams: 1000, reason: 'measured red SKU' }]);
    await page.waitForSelector('section select', { visible: true });
    await page.select('section select', 'Shelf-Mixed-01');
    await Promise.all([page.waitForResponse(response => response.url().endsWith('/assign') && response.request().method() === 'POST'), page.click('section button[type="submit"]')]);
    assert.deepEqual(reservations, [{ binLocation: 'Shelf-Mixed-01' }]);
    await page.waitForFunction(() => Array.from(document.querySelectorAll('section button')).some(button => button.textContent?.includes('تکمیل جانمایی') && !(button as HTMLButtonElement).disabled));
    assert.match(await page.$eval('section', element => (element as HTMLElement).innerText), /Shelf-Mixed-01/);
    assert.equal(await page.$eval('section select', select => select.closest('details')?.open), false, 'reassignment is secondary after a reservation');
    await page.locator('::-p-text(تغییر قفسه مقصد)').click();
    await Promise.all([
      page.waitForResponse(response => response.url().endsWith('/assign') && response.request().method() === 'POST'),
      page.locator('::-p-text(تازه‌سازی رزرو همین قفسه)').click(),
    ]);
    assert.deepEqual(reservations, [{ binLocation: 'Shelf-Mixed-01' }, { binLocation: 'Shelf-Mixed-01' }]);
    await Promise.all([
      page.waitForResponse(response => response.url().endsWith('/task-red/confirm') && response.request().method() === 'POST'),
      page.locator('::-p-text(تکمیل جانمایی)').click(),
    ]);
    await page.waitForFunction(() => Array.from(document.querySelectorAll('[role="status"]')).some(status => status.textContent?.includes('Shelf-Mixed-01')));
    await page.locator('::-p-text(بررسی موجودی قفسه)').click();
    await page.waitForSelector('dialog[open] li');
    assert.deepEqual(await page.$$eval('dialog[open] li h3', headings => headings.map(heading => heading.textContent)), ['Shelf-Mixed-01']);
    assert.match(await page.$eval('dialog[open] li', card => card.textContent || ''), /Product/);
  } finally { await browser.close(); server.closeAllConnections(); await new Promise<void>(resolve => server.close(() => resolve())); }
});
