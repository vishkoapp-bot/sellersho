import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { test } from 'node:test';
import { build } from 'esbuild';
import puppeteer from 'puppeteer';

test('contextual support previews without writing and keeps the same key when submission is retried', { timeout: 120000 }, async () => {
  const bundle = await build({
    write: false, bundle: true, platform: 'browser', format: 'iife', jsx: 'automatic',
    define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env': '{}' },
    stdin: { loader: 'tsx', resolveDir: process.cwd(), contents: `
      import { createRoot } from 'react-dom/client';
      import { ContextSupportButton } from './src/components/support/ContextSupportButton';
      createRoot(document.getElementById('root')).render(<ContextSupportButton role="supplier" kind="shipment" referenceType="carton" referenceId="shipment-id" />);
    ` },
  });
  const writes: { key: string | string[] | undefined; body: unknown }[] = [];
  const previews: URLSearchParams[] = [];
  const server = createServer(async (request, response) => {
    const url = new URL(request.url ?? '/', 'http://fixture');
    if (url.pathname === '/api/v1/supplier/tickets/context') {
      previews.push(url.searchParams);
      response.setHeader('Content-Type', 'application/json');
      response.end('{"category":"shipment","referenceType":"carton","referenceId":"shipment-id","referenceLabel":"CARTON-4321"}');
    } else if (url.pathname === '/api/v1/supplier/tickets' && request.method === 'POST') {
      let body = '';
      for await (const chunk of request) body += chunk;
      writes.push({ key: request.headers['idempotency-key'], body: JSON.parse(body) });
      response.setHeader('Content-Type', 'application/json');
      if (writes.length === 1) {
        response.writeHead(502).end('{"error":"نتیجه ارسال مشخص نیست؛ دوباره تلاش کنید."}');
        return;
      }
      response.writeHead(201).end('{"id":"ticket-1"}');
    } else {
      response.setHeader('Content-Type', 'text/html; charset=utf-8');
      response.end('<!doctype html><html lang="fa" dir="rtl"><meta name="viewport" content="width=device-width,initial-scale=1"><div id="root"></div><script>' + bundle.outputFiles[0].text + '</script>');
    }
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  try {
    const address = server.address();
    assert.ok(address && typeof address === 'object');
    const browser = await puppeteer.launch({ headless: true, protocolTimeout: 40000, args: ['--no-sandbox', `--explicitly-allowed-ports=${address.port}`], ...(process.platform === 'win32' ? { channel: 'chrome' as const } : {}) });
    try {
      const page = await browser.newPage();
      page.setDefaultTimeout(15000);
      await page.setViewport({ width: 360, height: 800, isMobile: true, hasTouch: true });
      await page.goto(`http://127.0.0.1:${address.port}`);
      await page.waitForSelector('button');
      assert.equal(previews.length, 0);
      await page.click('button');
      await page.waitForSelector('dialog[open] form');
      assert.equal(await page.$eval('dialog', dialog => dialog.dir), 'rtl');
      assert.match(await page.$eval('dialog input', input => input.value), /CARTON-4321/);
      assert.equal(await page.$eval('dialog [type=submit]', button => (button as HTMLButtonElement).disabled), true);
      await page.keyboard.press('Escape');
      await page.waitForSelector('dialog', { hidden: true });
      assert.equal(await page.$eval('body > #root > button', button => button === document.activeElement), true);
      assert.equal(writes.length, 0);
      await page.click('button');
      await page.waitForSelector('dialog textarea');
      await page.type('dialog textarea', 'Please check this delivery');
      await page.click('dialog [type=submit]');
      await page.waitForSelector('dialog [role="alert"]');
      await page.click('dialog [type=submit]');
      await page.waitForFunction(() => document.querySelector('dialog [role="status"]')?.textContent?.includes('ticket-1'));
      assert.equal(previews[0].get('referenceId'), 'shipment-id');
      assert.equal(writes.length, 2);
      assert.match(String(writes[0].key), /^[\da-f]{8}-(?:[\da-f]{4}-){3}[\da-f]{12}$/i);
      assert.deepEqual(writes[0].body, { kind: 'shipment', referenceType: 'carton', referenceId: 'shipment-id', subject: 'پیگیری ارسال و محموله - CARTON-4321', message: 'Please check this delivery' });
      assert.deepEqual(writes[1], writes[0]);
    } finally {
      await browser.close();
    }
  } finally {
    server.closeAllConnections();
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
});
