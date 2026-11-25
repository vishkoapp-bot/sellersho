import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import test from 'node:test';
import tailwindcss from '@tailwindcss/vite';
import { build } from 'esbuild';
import puppeteer from 'puppeteer';
import { build as buildStyles } from 'vite';

test('supplier sections, navigation history and daily workflows work on mobile and desktop', { timeout: 90000 }, async () => {
  const cssBuild = await buildStyles({ configFile: false, plugins: [tailwindcss()], logLevel: 'silent', build: { write: false, rollupOptions: { input: 'src/index.css' } } });
  const assets = (Array.isArray(cssBuild) ? cssBuild : [cssBuild]).flatMap(result => 'output' in result ? result.output : [])
    .filter(asset => asset.type === 'asset');
  const css = assets.find(asset => asset.fileName.endsWith('.css'));
  assert.ok(css);
  const bundle = await build({
    write: false, bundle: true, platform: 'browser', format: 'iife', jsx: 'automatic',
    define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env': '{}' },
    plugins: [{ name: 'supplier-session', setup(build) {
      build.onResolve({ filter: /context\/StoreContext$/ }, () => ({ path: 'store', namespace: 'fixture' }));
      build.onResolve({ filter: /^\.\/SupplierAuthGuard$/ }, () => ({ path: 'guard', namespace: 'fixture' }));
      build.onLoad({ filter: /^store$/, namespace: 'fixture' }, () => ({
        resolveDir: process.cwd(),
        contents: `
          import { useState } from 'react';
          export const useStore = () => {
            const [supplierSession, setSupplierSession] = useState({ supplierId: 'supplier', token: 'fixture', permissions: ['supplier.manage'], signedContractVersion: 1 });
            const [contracts, updateContracts] = useState(() => new URLSearchParams(location.search).has('contract-check')
              ? { supplierContractVersion: 2, supplierContractText: 'متن قرارداد قبلی' } : undefined);
            return { supplierSession, setSupplierSession, platformSettings: { contracts }, updateContracts, formatPrice: String, toPersianDigits: String };
          };
        `,
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
  let failNextProfileRead = false;
  let failNextDemandRead = true;
  let directStatus = 'requested';
  let shipmentStatus = 'draft';
  let deletedPhotos = 0;
  const contractVersions: number[] = [];
  const shipmentUpdates: unknown[] = [];
  const server = createServer(async (request, response) => {
    const { pathname: path, searchParams } = new URL(request.url ?? '/', 'http://fixture');
    response.setHeader('Content-Type', 'application/json');
    const font = path.endsWith('.woff2') ? assets.find(asset => `/${asset.fileName}` === path) : undefined;
    if (font) { response.setHeader('Content-Type', 'font/woff2'); response.end(font.source); }
    else if (path === '/supplier-test.css') { response.setHeader('Content-Type', 'text/css'); response.end(css.source); }
    else if (path === '/api/v1/platform/contracts') response.end(JSON.stringify({ supplierContractVersion: 3, supplierContractText: 'متن قرارداد به‌روزشده' }));
    else if (path === '/api/v1/supplier/contract/accept' && request.method === 'POST') {
      let body = '';
      for await (const chunk of request) body += chunk;
      const { version } = JSON.parse(body);
      contractVersions.push(version);
      if (version !== 3) {
        response.statusCode = 409;
        response.end('{"code":"contract_version_changed"}');
      } else {
        response.end('{"signedContractVersion":3,"platformContractVersion":3,"success":true}');
      }
    }
    else if (path === '/api/v1/supplier/orders') response.end(JSON.stringify({ orders, hasMore: true }));
    else if (path === '/api/v1/supplier/analytics') response.end('{"orderCount":0,"totalBaseAmount":0,"totalQuantity":0,"returns":{"totalReturned":0}}');
    else if (path === '/api/v1/supplier/shipping-methods') response.end(JSON.stringify([{ method: 'direct', status: directStatus }, { method: 'warehouse', status: 'disabled' }]));
    else if (path === '/api/v1/supplier/search-insight' && searchParams.get('days') === '7' && failNextDemandRead) { failNextDemandRead = false; response.statusCode = 503; response.end('{"error":"دریافت داده‌ها انجام نشد"}'); }
    else if (path === '/api/v1/supplier/search-insight') response.end('{"unmetInScope":[{"term":"کیف چرمی","searches":12,"zeroResultSearches":10}]}');
    else if (path === '/api/v1/supplier/profile' && failNextProfileRead) { failNextProfileRead = false; response.statusCode = 503; response.end('{"error":"خطای دریافت پروفایل"}'); }
    else if (path === '/api/v1/supplier/profile') response.end('{"name":"تأمین‌کننده نمونه","warehouse_city":"تهران"}');
    else if (path === '/api/v1/supplier/products') response.end(JSON.stringify([
      { id: 'a', title: 'کیف دستی', brand: 'آبی', stock: 5, supplierBaseCost: 50, reviewsCount: 1, rating: 5 },
      { id: 'b', title: 'کفش سفید', stock: 2, supplierBaseCost: 80 },
    ]));
    else if (path === '/api/v1/products/a/reviews') response.end('{"items":[{"id":"review1","displayName":"خریدار نمونه","rating":5,"body":"نظر نمونه درباره کالا","createdAt":"2026-11-24T00:00:00Z"}],"rating":5,"reviewsCount":1,"total":1}');
    else if (/^\/api\/v1\/supplier\/products\/[ab]\/images$/.test(path)) response.end(JSON.stringify(deletedPhotos ? [] : [{ id: 'image1', imageUrl: '' }]));
    else if (/^\/api\/v1\/supplier\/products\/[ab]\/images\/image1$/.test(path) && request.method === 'DELETE') { deletedPhotos++; response.end('{}'); }
    else if (path === '/api/v1/supplier/shipments' && request.method === 'POST') {
      let body = '';
      for await (const chunk of request) body += chunk;
      shipmentUpdates.push(JSON.parse(body));
      shipmentStatus = 'cancelled';
      response.end('{"success":true}');
    } else if (path === '/api/v1/supplier/shipments') response.end(JSON.stringify([
      { id: 'a', shipment_code: 'DRAFT', status: shipmentStatus },
      { id: 'b', shipment_code: 'RECEIVED', status: 'received' },
    ]));
    else if (path === '/api/v1/supplier/settlement/balance') response.end('{"available":100000}');
    else if (path.startsWith('/api/')) response.end('[]');
    else {
      response.setHeader('Content-Type', 'text/html; charset=utf-8');
      response.end('<!doctype html><html lang="fa" dir="rtl"><meta name="viewport" content="width=device-width, initial-scale=1"><link rel="stylesheet" href="/supplier-test.css"><div id="root"></div><script>' + bundle.outputFiles[0].text + '</script>');
    }
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  assert.ok(address && typeof address === 'object');
  try {
    const browser = await puppeteer.launch({ headless: true, args: ['--no-sandbox', `--explicitly-allowed-ports=${address.port}`], ...(process.platform === 'win32' ? { channel: 'chrome' as const } : {}) });
    try {
      const page = await browser.newPage();
      const pageErrors: string[] = [];
      page.on('pageerror', cause => pageErrors.push(String(cause)));
      await page.setViewport({ width: 360, height: 800, isMobile: true, hasTouch: true });
      const base = `http://127.0.0.1:${address.port}/supplier`;
      await page.goto(`${base}?contract-check=stale`);
      const contractCheckbox = await page.waitForSelector('label input[type="checkbox"]', { visible: true });
      assert.ok(contractCheckbox);
      assert.ok(await page.evaluate(() => [...document.querySelectorAll('button')].some(button => button.textContent?.trim() === 'قبول دارم، ادامه بده' && button.disabled)));
      const submitContract = async () => {
        const button = await page.waitForFunction(() => [...document.querySelectorAll('button')].find(item => item.textContent?.trim() === 'قبول دارم، ادامه بده' && !item.disabled));
        await button.asElement()!.click();
      };
      await contractCheckbox.click();
      await submitContract();
      await page.waitForFunction(() => document.body.innerText.includes('متن قرارداد به‌روزشده') && [...document.querySelectorAll('button')].some(button => button.textContent?.trim() === 'قبول دارم، ادامه بده' && button.disabled));
      assert.deepEqual(contractVersions, [2]);
      assert.equal(await contractCheckbox.evaluate(input => (input as HTMLInputElement).checked), false);
      await contractCheckbox.click();
      await submitContract();
      await page.waitForSelector('label input[type="checkbox"]', { hidden: true });
      assert.deepEqual(contractVersions, [2, 3]);
      await page.goto(base);
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
      await page.waitForFunction(() => document.querySelector('main')?.textContent?.includes('در انتظار تأیید روش ارسال'), { timeout: 3000 });
      assert.ok(!(await page.$eval('main', main => main.textContent))?.includes('همه‌چیز آماده است'));
      await page.tap('header button[aria-label="اعلان‌ها"]');
      await page.waitForSelector('dialog[open] #supplier-notifications-title', { visible: true });
      await page.keyboard.press('Escape');
      await page.waitForSelector('dialog[open]', { hidden: true });
      await click('رسیدگی به سفارش‌ها');
      await page.waitForFunction(() => document.querySelector('main [aria-label="وضعیت سفارش"] button[aria-pressed="true"]')?.textContent?.includes('منتظر ارسال'));
      assert.equal(new URL(page.url()).searchParams.get('section'), 'orders');
      await page.reload();
      await page.waitForFunction(() => document.querySelector('main [aria-label="وضعیت سفارش"] button[aria-pressed="true"]')?.textContent?.includes('منتظر ارسال'));
      await page.goBack();
      await page.waitForFunction(() => (document.querySelector('#supplier-section') as HTMLSelectElement)?.value === 'overview');
      await page.goForward();
      await page.waitForFunction(() => (document.querySelector('#supplier-section') as HTMLSelectElement)?.value === 'orders');
      await click('ثبت اطلاعات ارسال');
      await page.waitForSelector('dialog[open] #supplier-shipping-tracking', { visible: true });
      await page.keyboard.press('Escape');
      await page.waitForSelector('dialog[open]', { hidden: true });
      await page.type('input[aria-label="جستجوی سفارش‌ها"]', 'no matching order');
      await page.waitForFunction(() => document.querySelector('main')?.innerText.includes('بارگذاری سفارش‌های قدیمی‌تر'));
      await click('پاک کردن فیلترها');
      assert.equal(await page.$eval('input[aria-label="جستجوی سفارش‌ها"]', input => (input as HTMLInputElement).value), '');
      directStatus = 'enabled';
      await page.select('#supplier-section', 'overview');
      await page.waitForFunction(() => document.querySelector('main')?.textContent?.includes('همه‌چیز آماده است'));
      assert.ok(!(await page.$eval('main', main => main.textContent))?.includes('اولین بار را به انبار بفرست'));
      await click('پیگیری محموله‌ها');
      await page.waitForFunction(() => document.querySelector('main [aria-current="step"]')?.textContent?.includes('ارسال و برچسب‌ها'));
      await click('آماده‌سازی محموله');
      assert.equal(new URL(page.url()).searchParams.get('view'), 'prepare');
      await page.goBack();
      await page.waitForFunction(() => document.querySelector('main [aria-current="step"]')?.textContent?.includes('ارسال و برچسب‌ها'));
      await click('دیدن کدهای واحد کالا');
      await page.waitForSelector('#supplier-unit-labels', { visible: true, timeout: 3000 });
      await click('بستن برچسب‌های واحد');
      await page.waitForSelector('#supplier-unit-labels', { hidden: true });
      await page.type('input[aria-label="جستجوی کد محموله"]', 'draft');
      await page.waitForFunction(() => [...document.querySelectorAll('main article bdi')].map(element => element.textContent).join(',') === 'DRAFT');
      await click('لغو محموله');
      await page.waitForSelector('dialog[open]', { visible: true });
      await page.keyboard.press('Escape');
      await page.waitForSelector('dialog[open]', { hidden: true });
      assert.deepEqual(shipmentUpdates, []);
      await click('لغو محموله');
      await page.waitForSelector('dialog[open]', { visible: true });
      await click('تأیید لغو محموله');
      await page.waitForFunction(() => document.querySelector('main article')?.textContent?.includes('لغو شده'));
      assert.deepEqual(shipmentUpdates, [{ shipment_id: 'a', action: 'cancel' }]);
      await page.select('#supplier-section', 'products');
      await page.waitForFunction(() => document.querySelector('main')?.innerText.includes('کفش سفید'));
      await page.type('input[placeholder="نام یا برند کالا"]', 'كيف');
      await page.waitForFunction(() => { const content = document.querySelector('main')?.innerText ?? ''; return content.includes('کیف دستی') && !content.includes('کفش سفید'); });
      await click('گالری تصاویر');
      const deletePhoto = await page.waitForSelector('dialog[open] button[aria-label="حذف تصویر 1"]', { visible: true, timeout: 3500 });
      assert.ok(deletePhoto);
      assert.ok(await deletePhoto.evaluate(button => button.getBoundingClientRect().height >= 44 && !button.closest('[class*="opacity-0"]')));
      await deletePhoto.tap();
      await page.waitForFunction(() => [...document.querySelectorAll('dialog h3')].some(heading => heading.textContent === 'تصویر حذف شود؟'));
      await page.keyboard.press('Escape');
      await page.waitForFunction(() => document.querySelectorAll('dialog[open]').length === 1);
      assert.equal(deletedPhotos, 0);
      await deletePhoto.tap();
      await click('تأیید حذف تصویر');
      await page.waitForFunction(() => document.querySelector('dialog[open]')?.textContent?.includes('هنوز تصویری ثبت نشده است.'));
      assert.equal(deletedPhotos, 1);
      await page.keyboard.press('Escape');
      await page.waitForSelector('dialog[open]', { hidden: true });
      await click('مشاهده نظرات');
      await page.waitForSelector('dialog[open] #supplier-reviews-title', { visible: true });
      await page.waitForFunction(() => document.querySelector('dialog[open]')?.textContent?.includes('نظر نمونه درباره کالا'));
      await page.keyboard.press('Escape');
      await page.waitForSelector('dialog[open]', { hidden: true });
      await page.select('#supplier-section', 'demand-insight');
      await click('۷ روز اخیر');
      await page.waitForSelector('main [role="alert"]');
      assert.ok((await page.$eval('main [aria-label="بازهٔ زمانی تقاضا"] button[aria-pressed="true"]', button => button.textContent))?.includes('۷ روز اخیر'));
      await click('تلاش دوباره');
      await click('پیشنهاد کالای جدید');
      assert.equal(new URL(page.url()).searchParams.get('view'), 'propose');
      await page.waitForSelector('section[aria-label="فرم پیشنهاد کالای جدید"]', { visible: true });
      await page.select('#supplier-section', 'settlement');
      await click('ثبت درخواست برداشت');
      assert.equal(await page.$eval('input[name="holder"]', input => input.getAttribute('aria-invalid')), 'true');
      assert.equal(await page.evaluate(() => document.activeElement?.getAttribute('name')), 'holder');
      await page.type('input[name="holder"]', 'Test holder');
      await click('ثبت درخواست برداشت');
      assert.equal(await page.$eval('input[name="shaba"]', input => input.getAttribute('aria-invalid')), 'true');
      failNextProfileRead = true;
      await page.select('#supplier-section', 'profile');
      await page.waitForSelector('main [role="alert"]');
      assert.equal(await page.$('main input'), null);
      await click('تلاش دوباره');
      await page.waitForFunction(() => [...document.querySelectorAll<HTMLInputElement>('main input')].some(input => input.value === 'تهران'));

      await page.select('#supplier-section', 'inventory');
      await click('عوض‌کردن موجودی');
      await page.waitForSelector('dialog[open] form', { visible: true, timeout: 3000 });
      await page.keyboard.press('Escape');
      await page.waitForSelector('dialog[open]', { hidden: true });
      assert.ok(await page.evaluate(() => document.activeElement?.textContent?.includes('عوض‌کردن موجودی')));

      const sections = await page.$$eval('#supplier-section option', options => options.map(option => ({ id: (option as HTMLOptionElement).value, label: option.textContent })));
      assert.equal(sections.length, 16);
      for (const viewport of [{ width: 360, height: 800, isMobile: true, hasTouch: true }, { width: 1440, height: 900 }]) {
        await page.setViewport(viewport);
        for (const { id, label } of sections) {
          if (viewport.isMobile) await page.select('#supplier-section', id);
          else await page.click(`aside a[data-section="${id}"]`);
          await page.waitForFunction(expected => document.querySelector('header h1')?.textContent === expected, { timeout: 4000 }, label)
            .catch(cause => { throw new Error(`Unable to open ${id} at ${viewport.width}px (${pageErrors.join(', ')})`, { cause }); });
          await page.waitForNetworkIdle({ idleTime: 60 });
          assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${id} overflows at ${viewport.width}px`);
        }
      }
      assert.deepEqual(pageErrors, []);
    } finally { await browser.close(); }
  } finally {
    server.closeAllConnections();
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
});
