import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { build } from 'esbuild';
import puppeteer from 'puppeteer';

test('paid cancellation shows pending and unknown refunds on response and reload without false success', { timeout: 90000 }, async () => {
  const contextModules: Record<string, string> = {
    auth: `export const adminRolesForCapabilities = () => [];
      export const panelsForCapabilities = () => ['customer'];
      export const useAuth = () => ({
        currentUser: { id: 'buyer', storeId: 'central', capabilities: ['customer'] },
        authToken: 'synthetic', isAuthenticated: true, isSessionReady: true,
      });`,
    store: `export const useStore = () => ({
      currentStore: { id: 'central' }, formatPrice: String, toPersianDigits: String,
      setTrackingOrder: () => {}, setIsTrackingModalOpen: () => {},
    });`,
    toast: `export const useToast = () => ({
      success: text => document.getElementById('notice').textContent = 'success:' + text,
      error: text => document.getElementById('notice').textContent = 'error:' + text,
    });`,
  };
  const bundle = await build({ write: false, bundle: true, platform: 'browser', format: 'iife', jsx: 'automatic', define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env': '{}' }, plugins: [{ name: 'session-fixtures', setup(b) {
    b.onResolve({ filter: /(?:^|\/)AuthContext$/ }, () => ({ path: 'auth', namespace: 'fixture' }));
    b.onResolve({ filter: /(?:^|\/)StoreContext$/ }, () => ({ path: 'store', namespace: 'fixture' }));
    b.onResolve({ filter: /(?:^|\/)ToastContext$/ }, () => ({ path: 'toast', namespace: 'fixture' }));
    b.onLoad({ filter: /.*/, namespace: 'fixture' }, args => ({ loader: 'js', contents: contextModules[args.path] }));
  } }], stdin: { loader: 'tsx', resolveDir: process.cwd(), contents: `import React from 'react';import{createRoot}from'react-dom/client';import{OrderProvider}from'./src/context/OrderContext';import{CustomerOrdersView}from'./src/components/views/CustomerOrdersView';import{tokenStore}from'./src/services/apiClient';tokenStore.setToken('synthetic');createRoot(document.getElementById('root')).render(<OrderProvider><CustomerOrdersView onBack={()=>{}}/></OrderProvider>);` } });
  let cancellations = 0; let cancelled = false; let refundStatus = 'pending';
  const refund = () => ({ status: refundStatus, currency: 'TOMAN', amountToman: 10000000, pendingToman: refundStatus === 'pending' ? 10000000 : 0, succeededToman: 0, unknownToman: refundStatus === 'unknown' ? 10000000 : 0, failedToman: 0, destination: 'original_payment', needsReview: refundStatus === 'unknown' });
  const server = createServer(async (req, res) => {
    res.setHeader('Content-Type', 'application/json');
    if (req.url === '/api/v1/orders') res.end(JSON.stringify([{ id: 'paid-order', orderNumber: 'PAID-10M', storeId: 'central', storeName: 'central', totalAmount: 10000000, payableAmount: 10000000, discountAmount: 0, status: cancelled ? 'cancelled' : 'processing', paymentStatus: 'paid', items: [], createdAt: '2026-11-15', refund: cancelled ? refund() : undefined }]));
    else if (req.url === '/api/v1/returns') res.end('[]');
    else if (req.url?.endsWith('/cancel')) { cancellations++; if (cancellations === 1) { res.statusCode = 409; res.end('{"error":"dispatched child rejects cancellation"}'); } else { cancelled = true; res.end(JSON.stringify({ success: true, orderId: 'paid-order', status: 'cancelled', refund: refund() })); } }
    else { res.setHeader('Content-Type', 'text/html'); res.end('<div id="notice"></div><div id="root"></div><script>' + bundle.outputFiles[0].text + '</script>'); }
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve)); const address = server.address(); assert.ok(address && typeof address === 'object');
  const browser = await puppeteer.launch({
    headless: true, executablePath: process.env.CHROME_PATH, pipe: true,
    args: ['--no-sandbox', '--disable-setuid-sandbox', '--disable-dev-shm-usage'],
  }).catch(async e => { await new Promise<void>(resolve => server.close(() => resolve())); throw e; });
  try {
    const page = await browser.newPage(); await page.goto(`http://127.0.0.1:${address.port}/refund-fixture`); await page.waitForFunction(() => document.body.innerText.includes('PAID-10M'));
    const clickAction = async (label: string) => { await page.evaluate(name => [...document.querySelectorAll('button')].find(button => button.textContent?.trim() === name)!.click(), label); };
    await clickAction('لغو سفارش');
    await clickAction('انصراف');
    assert.equal(cancellations, 0);
    await clickAction('لغو سفارش');
    await clickAction('تأیید لغو سفارش');
    await page.waitForFunction(() => document.getElementById('notice')?.textContent?.startsWith('error:'));
    assert.equal(cancelled, false); assert.ok(!await page.$('[data-refund-status]'));
    await clickAction('تأیید لغو سفارش');
    await page.waitForFunction(() => document.querySelector('[data-refund-status]')?.getAttribute('data-refund-status') === 'pending');
    assert.match(await page.$eval('[data-refund-status]', e => e.textContent || ''), /10000000.*روش پرداخت اولیه/s);
    assert.ok(!(await page.$eval('#notice', e => e.textContent || '')).includes('بازپرداخت انجام شد'));
    await page.reload(); await page.waitForSelector('[data-refund-status="pending"]');
    refundStatus = 'unknown'; await page.reload(); await page.waitForSelector('[data-refund-status="unknown"]');
    assert.match(await page.$eval('[data-refund-status]', e => e.textContent || ''), /نیازمند بررسی/);
    assert.equal(cancellations, 2);
  } finally { await browser.close(); await new Promise<void>(resolve => server.close(() => resolve())); }
});
