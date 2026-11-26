import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { test } from 'node:test';
import { build } from 'esbuild';
import puppeteer from 'puppeteer';
import type { AdminSupplierRow, SupplierDetail } from './AdminSuppliersTab';

test('admin can find a supplier on a later page and review and reject its request on a phone', { timeout: 90000 }, async () => {
  const bundle = await build({
    write: false, bundle: true, platform: 'browser', format: 'iife', jsx: 'automatic',
    define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env': '{}' },
    plugins: [{ name: 'admin-session', setup(builder) {
      builder.onResolve({ filter: /context\/StoreContext$/ }, () => ({ path: 'store', namespace: 'test' }));
      builder.onLoad({ filter: /.*/, namespace: 'test' }, () => ({
        contents: 'import { toPersianDigits } from "./src/utils/formatters"; export const useStore = () => ({ toPersianDigits });',
        resolveDir: process.cwd(),
      }));
    } }],
    stdin: { loader: 'tsx', resolveDir: process.cwd(), contents: `
      import { createRoot } from 'react-dom/client';
      import { AdminSuppliersTab } from './src/components/admin/AdminSuppliersTab';
      import { tokenStore } from './src/services/apiClient';
      tokenStore.setToken('synthetic');
      createRoot(document.getElementById('root')).render(<AdminSuppliersTab />);
    ` },
  });
  const first: AdminSupplierRow = { id: 'first', name: 'فروشگاه اول', warehouseCity: 'تهران', rating: 4, totalProducts: 1, deliveryScore: 91, approvalStatus: 'pending_approval', supportsDirectShipping: false };
  const second: AdminSupplierRow = { ...first, id: 'second', name: 'کالای ایرانی', warehouseCity: 'کرج' };
  const approved: AdminSupplierRow = { ...first, id: 'approved', name: 'فروشگاه همکار', approvalStatus: 'approved' };
  const queryHistory: URLSearchParams[] = [];
  let rejectPayload: unknown;
  const server = createServer(async (request, response) => {
    const url = new URL(request.url || '/', 'http://localhost');
    response.setHeader('Content-Type', 'application/json');
    if (url.pathname === '/api/v1/admin/suppliers/records') {
      queryHistory.push(url.searchParams);
      const pending = url.searchParams.get('status') === 'pending_approval';
      const laterPage = url.searchParams.get('cursor') === 'later+/page';
      const suppliers = laterPage ? [second] : pending ? [first] : [first, approved];
      const hasMore = pending && !laterPage && second.approvalStatus === 'pending_approval';
      response.end(JSON.stringify({ suppliers, hasMore, nextCursor: hasMore ? 'later+/page' : '' }));
    } else if (url.pathname === '/api/v1/admin/suppliers/second') {
      const detail: SupplierDetail = {
        supplier: { ...second, owner: { userId: 'owner', fullName: 'مالک فروشگاه', email: 'owner@example.test', mobile: '09120000001', isActive: true } },
        counts: { products: 1, activeProducts: 0, variants: 2, proposals: { pending: 0, approved: 0, rejected: 0 }, orders: { paid: 0, directShipmentsByStatus: {} } },
        warehouses: [], members: [], shippingMethods: [], tapin: { shopId: '', connected: false },
        finance: { currencyUnit: 'TOMAN', balance: null, settlements: [] }, history: [], gaps: [],
      };
      response.end(JSON.stringify(detail));
    } else if (url.pathname === '/api/v1/admin/suppliers/second/reject' && request.method === 'POST') {
      let body = '';
      for await (const chunk of request) body += chunk;
      rejectPayload = JSON.parse(body);
      second.approvalStatus = 'rejected';
      second.rejectionReason = 'مدارک ناقص';
      response.end('{}');
    } else if (url.pathname === '/') {
      response.setHeader('Content-Type', 'text/html; charset=utf-8');
      response.end('<!doctype html><html dir="rtl" lang="fa"><meta name="viewport" content="width=device-width, initial-scale=1"><div id="root"></div><script>' + bundle.outputFiles[0].text + '</script></html>');
    } else {
      response.statusCode = 404;
      response.end('{}');
    }
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  assert.ok(address && typeof address === 'object');
  try {
    const browser = await puppeteer.launch({
      headless: true, args: ['--no-sandbox', `--explicitly-allowed-ports=${address.port}`],
      ...(process.platform === 'win32' ? { channel: 'chrome' as const } : {}),
    });
    try {
      const page = await browser.newPage();
      page.setDefaultTimeout(10000);
      await page.setViewport({ width: 360, height: 740, isMobile: true, hasTouch: true });
      await page.goto(`http://127.0.0.1:${address.port}/`);
      await page.waitForSelector('button[aria-label="مشاهده جزئیات فروشگاه همکار"]');
      const click = async (label: string, rootSelector = 'body') => {
        const handle = await page.waitForFunction((name, region) => [...document.querySelectorAll<HTMLButtonElement>(`${region} button`)].find(element => !element.disabled && element.textContent?.trim() === name), {}, label, rootSelector);
        const button = handle.asElement();
        assert.ok(button);
        await (await button.toElement('button')).click();
      };

      await click('در انتظار تأیید');
      await page.waitForSelector('button[aria-label="مشاهده جزئیات فروشگاه اول"]');
      assert.equal(await page.$('button[aria-label="مشاهده جزئیات فروشگاه همکار"]'), null);
      assert.equal(queryHistory[1].get('status'), 'pending_approval');
      await click('نمایش تأمین‌کنندگان بیشتر');
      const supplierButton = 'button[aria-label="مشاهده جزئیات کالای ایرانی"]';
      await page.waitForSelector(supplierButton);
      assert.equal(queryHistory[2].get('status'), 'pending_approval');
      assert.equal(queryHistory[2].get('cursor'), 'later+/page');

      await page.$eval('input[type="search"]', input => {
        const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set;
        setter?.call(input, 'كالاي');
        input.dispatchEvent(new Event('input', { bubbles: true }));
      });
      await page.waitForFunction(() => !document.querySelector('button[aria-label="مشاهده جزئیات فروشگاه اول"]'));
      await page.click(supplierButton);
      await page.waitForFunction(() => document.querySelector('dialog:modal')?.textContent?.includes('مالک فروشگاه'));
      await click('رد درخواست', 'dialog:modal');
      await page.waitForSelector('dialog:modal textarea');
      await page.keyboard.press('Escape');
      await page.waitForFunction(() => document.querySelectorAll('dialog:modal').length === 1);
      assert.equal(await page.evaluate(() => document.activeElement?.textContent?.trim()), 'رد درخواست');

      await click('رد درخواست', 'dialog:modal');
      await page.$eval('dialog:modal textarea', input => {
        const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set;
        setter?.call(input, 'مدارک ناقص');
        input.dispatchEvent(new Event('input', { bubbles: true }));
      });
      await click('تأیید رد', 'dialog:modal');
      await page.waitForFunction(() => document.querySelector('dialog:modal')?.textContent?.includes('ردشده'));
      assert.deepEqual(rejectPayload, { reason: 'مدارک ناقص' });
      await page.waitForSelector('dialog:modal button[aria-label="بستن کالای ایرانی"]:not([disabled])');
      await page.keyboard.press('Escape');
      await page.waitForFunction(() => !document.querySelector('dialog:modal') && document.activeElement?.getAttribute('type') === 'search');
    } finally {
      await browser.close();
    }
  } finally {
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
});
