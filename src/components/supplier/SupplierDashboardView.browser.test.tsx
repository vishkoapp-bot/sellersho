import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import test from 'node:test';
import { build } from 'esbuild';
import puppeteer from 'puppeteer';

test('mobile supplier navigation exposes each section and overview shortcuts and withdrawal guidance work', { timeout: 60000 }, async () => {
  const bundle = await build({
    write: false, bundle: true, platform: 'browser', format: 'iife', jsx: 'automatic',
    define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env': '{}' },
    plugins: [{ name: 'supplier-session', setup(build) {
      build.onResolve({ filter: /context\/StoreContext$/ }, () => ({ path: 'store', namespace: 'fixture' }));
      build.onResolve({ filter: /^\.\/SupplierAuthGuard$/ }, () => ({ path: 'guard', namespace: 'fixture' }));
      build.onLoad({ filter: /^store$/, namespace: 'fixture' }, () => ({
        contents: 'export const useStore=()=>({supplierSession:{supplierId:"supplier",token:"fixture",permissions:["supplier.manage"]},platformSettings:{},formatPrice:String,toPersianDigits:String});',
      }));
      build.onLoad({ filter: /^guard$/, namespace: 'fixture' }, () => ({
        contents: 'export const SupplierAuthGuard=({children})=>children;',
      }));
    } }],
    stdin: { loader: 'tsx', resolveDir: process.cwd(), contents: `
      import { createRoot } from 'react-dom/client';
      import { ToastProvider } from './src/context/ToastContext';
      import { SupplierDashboardView } from './src/components/supplier/SupplierDashboardView';
      createRoot(document.getElementById('root')).render(<ToastProvider><SupplierDashboardView onBackToCustomerSite={()=>{}} /></ToastProvider>);
    ` },
  });
  const statuses = ['pending', 'processing', 'cancelled', 'delivered_confirmed'];
  const orders = statuses.map((status, index) => ({ id: String(index), orderNumber: String(index), status, items: [] }));
  const server = createServer((request, response) => {
    const path = new URL(request.url ?? '/', 'http://fixture').pathname;
    response.setHeader('Content-Type', 'application/json');
    if (path === '/api/v1/supplier/orders') response.end(JSON.stringify({ orders, hasMore: true }));
    else if (path === '/api/v1/supplier/shipments') response.end(JSON.stringify([
      { id: 'a', shipment_code: 'DRAFT', status: 'draft' },
      { id: 'b', shipment_code: 'RECEIVED', status: 'received' },
    ]));
    else if (path === '/api/v1/supplier/settlement/balance') response.end('{"available":100000}');
    else if (path.startsWith('/api/')) response.end('[]');
    else {
      response.setHeader('Content-Type', 'text/html; charset=utf-8');
      response.end('<div id="root"></div><script>' + bundle.outputFiles[0].text + '</script>');
    }
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  assert.ok(address && typeof address === 'object');
  try {
    const browser = await puppeteer.launch({ headless: true, args: ['--no-sandbox'], ...(process.platform === 'win32' ? { channel: 'chrome' as const } : {}) });
    try {
      const page = await browser.newPage();
      await page.setViewport({ width: 390, height: 844 });
      await page.goto(`http://127.0.0.1:${address.port}/`);
      await page.waitForFunction(() => {
        const counts = [...document.querySelectorAll('main li')].map(row => row.textContent ?? '');
        return counts.some(row => row.includes('منتظر ارسال در سفارش‌های اخیر') && row.includes('2'))
          && counts.some(row => row.includes('محموله‌های باز') && row.includes('1'));
      });
      const labels = await page.$$eval('#supplier-section optgroup', groups => groups.map(group => (group as HTMLOptGroupElement).label));
      assert.ok(labels.includes('کالا و انبار') && labels.includes('مالی'));
      const click = async (label: string) => {
        const button = await page.waitForFunction(text => [...document.querySelectorAll<HTMLButtonElement>('main button')].find(item => item.offsetWidth && item.innerText.includes(text)), {}, label);
        const element = button.asElement();
        assert.ok(element);
        await (await element.toElement('button')).click();
      };
      await click('رسیدگی به سفارش‌ها');
      await page.waitForFunction(() => document.querySelector('main [aria-label="وضعیت سفارش"] button[aria-pressed="true"]')?.textContent?.includes('منتظر ارسال'));
      await page.type('input[aria-label="جستجوی سفارش‌ها"]', 'no matching order');
      await page.waitForFunction(() => document.querySelector('main')?.innerText.includes('بارگذاری سفارش‌های قدیمی‌تر'));
      await click('پاک کردن فیلترها');
      assert.equal(await page.$eval('input[aria-label="جستجوی سفارش‌ها"]', input => (input as HTMLInputElement).value), '');
      await page.select('#supplier-section', 'overview');
      await click('پیگیری محموله‌ها');
      await page.waitForFunction(() => document.querySelector('main [aria-current="step"]')?.textContent?.includes('ارسال و برچسب‌ها'));
      await page.select('#supplier-section', 'settlement');
      await click('ثبت درخواست برداشت');
      assert.equal(await page.$eval('input[name="holder"]', input => input.getAttribute('aria-invalid')), 'true');
      assert.equal(await page.evaluate(() => document.activeElement?.getAttribute('name')), 'holder');
      await page.type('input[name="holder"]', 'Test holder');
      await click('ثبت درخواست برداشت');
      assert.equal(await page.$eval('input[name="shaba"]', input => input.getAttribute('aria-invalid')), 'true');
    } finally { await browser.close(); }
  } finally {
    server.closeAllConnections();
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
});
