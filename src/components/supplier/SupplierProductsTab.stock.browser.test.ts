import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { build } from 'esbuild';
import puppeteer from 'puppeteer';

test('stock form requires reason and exact SKU, retains a conflict draft, accepts zero and refreshes actual total', { timeout: 90000 }, async () => {
  const bundle = await build({ write: false, bundle: true, platform: 'browser', format: 'iife', jsx: 'automatic', plugins: [{ name: 'supplier-session', setup(build) {
    build.onResolve({ filter: /context\/StoreContext$/ }, args => ['SupplierProductsTab.tsx', 'SupplierAddonsWidget.tsx', 'useSupplierResource.ts'].some(name => args.importer.endsWith(name)) ? { path: 'supplier-session', namespace: 'test' } : undefined);
    build.onLoad({ filter: /.*/, namespace: 'test' }, () => ({ contents: 'export const useStore=()=>({supplierSession:{supplierId:"sup",token:"fixture",permissions:["supplier.manage"]},formatPrice:v=>String(v),toPersianDigits:v=>String(v)});', loader: 'js' }));
  } }], define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env': '{}' }, stdin: { loader: 'tsx', resolveDir: process.cwd(), contents: `import React from 'react';import {createRoot} from 'react-dom/client';import {ToastProvider} from './src/context/ToastContext';import {SupplierProductsTab} from './src/components/supplier/SupplierProductsTab';createRoot(document.getElementById('root')).render(<ToastProvider><SupplierProductsTab/></ToastProvider>);` } });
  const payloads: any[] = []; let stock = 13;
  const server = createServer(async (request, response) => {
    response.setHeader('Content-Type', 'application/json');
    if (request.url === '/api/v1/supplier/products') response.end(JSON.stringify([{ id: 'counted', title: 'Counted product', schemaVersion: 2, supplierBaseCost: 1000, stock, productType: 'variable' }]));
    else if (request.url === '/api/v1/supplier/products/counted/variants') response.end(JSON.stringify([{ id: 'sku-a', variant_name: 'Red', spnc: 'RED', stock: 10 }, { id: 'sku-b', variant_name: 'Blue', spnc: 'BLUE', stock: 3 }]));
    else if (request.url === '/api/v1/supplier/profile') response.end('{}');
    else if (request.method === 'PUT' && request.url === '/api/v1/supplier/products/counted/stock') {
      let raw = ''; for await (const chunk of request) raw += chunk;
      payloads.push(JSON.parse(raw));
      if (payloads.length === 1) { response.statusCode = 409; response.end('{"error":"count changed; correct the draft"}'); }
      else { stock = 3; response.end('{"success":true,"stock":0,"productStock":3}'); }
    } else if (request.url?.startsWith('/api/')) response.end('[]');
    else { response.setHeader('Content-Type', 'text/html; charset=utf-8'); response.end('<html><body><div id="root"></div><script>' + bundle.outputFiles[0].text + '</script></body></html>'); }
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  const address = server.address(); assert.ok(address && typeof address === 'object');
  const browser = await puppeteer.launch({ headless: true, ...(process.platform === 'win32' ? { channel: 'chrome' as const } : {}) });
  try {
    const page = await browser.newPage(); const errors: string[] = []; page.on('pageerror', error => errors.push(error instanceof Error ? error.stack || error.message : String(error))); await page.goto(`http://127.0.0.1:${address.port}/__inv07`);
    const clickText = async (text: string) => { const buttons = await page.$$('button'); for (const button of buttons) if ((await button.evaluate(el => el.textContent))?.includes(text)) { await button.click(); return; } throw new Error('button missing: ' + text); };
    try { await page.waitForFunction(() => document.body.innerText.includes('ویرایش موجودی'), { timeout: 5000 }); } catch { throw new Error(JSON.stringify({ errors, body: await page.$eval('body', element => element.innerText) })); }
    await clickText('ویرایش موجودی');
    await page.waitForFunction(() => document.querySelectorAll('select[aria-label="تنوع برای اصلاح موجودی"] option').length === 3);
    const fill = async (label: string, text: string) => { await page.$eval(`input[aria-label="${label}"]`, (element, value) => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(element, value); element.dispatchEvent(new Event('input', { bubbles: true }));
    }, text); };
    await fill('موجودی کل قابل فروش', '8'); await fill('علت اصلاح موجودی', 'counted SKU');
    await clickText('ثبت موجودی'); assert.equal(payloads.length, 0, 'unselected SKU submitted');
    await page.select('select[aria-label="تنوع برای اصلاح موجودی"]', 'sku-a');
    await fill('علت اصلاح موجودی', '   '); await clickText('ثبت موجودی'); assert.equal(payloads.length, 0, 'blank reason submitted');
    await fill('علت اصلاح موجودی', '  counted SKU  ');
    for (const invalid of ['-1', '1.5', '2147483648']) { await fill('موجودی کل قابل فروش', invalid); await clickText('ثبت موجودی'); assert.equal(payloads.length, 0, 'invalid stock submitted: ' + invalid); }
    await fill('موجودی کل قابل فروش', '8');
    await Promise.all([page.waitForResponse(response => response.url().endsWith('/stock') && response.request().method() === 'PUT'), clickText('ثبت موجودی')]);
    await page.waitForFunction(() => document.body.innerText.includes('count changed; correct the draft'));
    assert.equal(await page.$eval('input[aria-label="موجودی کل قابل فروش"]', el => (el as HTMLInputElement).value), '8');
    assert.equal(await page.$eval('input[aria-label="علت اصلاح موجودی"]', el => (el as HTMLInputElement).value), '  counted SKU  ');
    assert.equal(await page.$eval('select[aria-label="تنوع برای اصلاح موجودی"]', el => (el as HTMLSelectElement).value), 'sku-a');
    await fill('موجودی کل قابل فروش', '۰');
    await Promise.all([page.waitForResponse(response => response.url().endsWith('/stock') && response.request().method() === 'PUT'), clickText('ثبت موجودی')]);
    await page.waitForFunction(() => document.body.innerText.includes('موجودی فعلی: 3 عدد'));
    assert.deepEqual(payloads, [{ new_stock: 8, reason: 'counted SKU', variantId: 'sku-a' }, { new_stock: 0, reason: 'counted SKU', variantId: 'sku-a' }]);
  } finally { await browser.close(); server.closeAllConnections(); await new Promise<void>(resolve => server.close(() => resolve())); }
});
