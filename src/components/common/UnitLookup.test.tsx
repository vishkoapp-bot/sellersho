import assert from 'node:assert/strict';
import { after, before, test } from 'node:test';
import { build } from 'esbuild';
import puppeteer, { type Browser } from 'puppeteer';
import { clickText, fillInput, waitText } from '../../../scripts/fixtures/catalog/batch8-browser';
import type { UnitLookupResponse } from '../../services/unitsApi';
import type { UnitStatus } from '../../utils/unitStatus';

const code = 'D0F91A60-X0001';
const supplierReturnKey = 'supplier-return:sample-lot';
const fixture = {
  code,
  status: 'sold',
  variant: { id: 'variant-1', spnc: 'D0F91A60', name: 'تنوع نمونه' },
  product: { id: 'product-1', spn: 'PC-3724980', title: 'محصول نمونه' },
  location: { balance_id: 'lot', location_key: supplierReturnKey, warehouse_name: 'انبار نمونه', bin_code: 'BIN-7', lot_status: 'quarantine' },
  printed: { printed_at: '2026-11-15', print_count: 2, last_printed_at: '2026-11-16' },
  order: { order_id: 'order-1', order_number: 'ORD-1', captured_at: '2026-11-17', restored_at: '2026-11-18' },
  history: [
    { event_type: 'minted', created_at: '2026-11-14', from_status: null, to_status: 'defined', reference_type: 'lot', reference_id: 'lot', actor_user_id: 'actor' },
    { event_type: 'new_event', created_at: '2026-11-17', from_status: 'in_stock', to_status: 'sold', reference_type: 'order', reference_id: 'order-1', actor_user_id: null },
  ],
} satisfies UnitLookupResponse;
const privateOrder = { ...fixture.order, customerName: 'PRIVATE-NAME', phone: 'PRIVATE-PHONE', address: 'PRIVATE-ADDRESS' };
const origin = 'http://unit-lookup.test';
let browser: Browser;
let javascript: string;

before(async () => {
  const bundle = await build({
    write: false, bundle: true, platform: 'browser', format: 'iife', jsx: 'automatic',
    define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env': '{}' },
    stdin: { loader: 'tsx', resolveDir: process.cwd(), contents: `
      import { createRoot } from 'react-dom/client';
      import { UnitLookup } from './src/components/common/UnitLookup';
      import { UnitLabelSheet } from './src/components/common/UnitLabelSheet';
      import { ToastProvider } from './src/context/ToastContext';
      import { tokenStore } from './src/services/apiClient';
      tokenStore.setToken('fixture-token');
      createRoot(document.querySelector('#root')).render(<ToastProvider>
        <UnitLookup onSelect={unit => document.querySelector('output').textContent = unit.code}
          getOrderHref={order => '/staff-fixture/' + order.order_id} />
        <UnitLabelSheet units={[
          { code: 'INVALID', barcode_value: 'INVALID', title: 'Invalid label', variant_name: '', spn: 'SPN', spnc: 'SPNC' },
          { code: '${code}', barcode_value: '${code}', title: 'Valid label', variant_name: 'پیش‌فرض', spn: 'SPN', spnc: 'SPNC' },
        ]} />
      </ToastProvider>);
    ` },
  });
  javascript = bundle.outputFiles[0].text;
  browser = await puppeteer.launch({ executablePath: process.env.CHROME_PATH, pipe: true, args: ['--no-sandbox', '--disable-setuid-sandbox', '--disable-dev-shm-usage'] });
});

after(async () => { await browser?.close(); });

async function openLookup(reply: (path: string) => { status?: number; body: unknown }) {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage();
    page.setViewport({ width: 390, height: 760 });
    page.setDefaultTimeout(5000);
    const requests: { path: string; method: string; auth?: string }[] = [];
    await page.setRequestInterception(true);
    page.on('request', request => {
      const url = new URL(request.url());
      const path = url.pathname + url.search;
      if (request.isNavigationRequest() && request.url() === origin + '/') {
        void request.respond({ contentType: 'text/html; charset=utf-8', body: '<main id="root"></main><output></output>' });
      } else if (url.origin === origin && path.startsWith('/api/')) {
        requests.push({ path, method: request.method(), auth: request.headers().authorization });
        const { status = 200, body } = reply(path);
        void request.respond({ status, contentType: 'application/json', body: JSON.stringify(body) });
      } else {
        void request.abort();
      }
    });
    await page.goto(origin);
    await page.addScriptTag({ content: javascript });
    const search = async (input: string) => {
      await fillInput(page, 'input', input);
      await page.keyboard.press('Enter');
    };
    return { context, page, requests, search };
  } catch (error) {
    await context.close();
    throw error;
  }
}

test('scanner Enter normalizes an issued code and renders only unit and order fields', async () => {
  const { context, page, requests, search } = await openLookup(path => ({
    body: path.includes('variant_id=') ? { units: [], next_cursor: null } : { ...fixture, order: privateOrder },
  }));
  try {
    await search('  d0f91a60-x0001\n');
    await waitText(page, 'new_event');
    assert.deepEqual(requests, [{ path: '/api/v1/units/D0F91A60-X0001', method: 'GET', auth: 'Bearer fixture-token' }]);
    const result = await page.$eval('article', node => node.textContent!);
    for (const value of [code, 'فروخته‌شده', 'محصول نمونه', 'PC-3724980', 'تنوع نمونه', 'D0F91A60', 'انبار نمونه', 'BIN-7',
      'quarantine', supplierReturnKey, 'ORD-1', '2026-11-15', '2026-11-16', '2026-11-17', '2026-11-18']) {
      assert.ok(result.includes(value), value);
    }
    assert.doesNotMatch(result, /PRIVATE-(?:NAME|PHONE|ADDRESS)/);
    assert.deepEqual(await page.$$eval('article ol li', events => events.map(event => event.querySelector('strong')?.textContent)), ['صدور', 'new_event']);
    assert.equal(await page.$eval('article a', link => link.getAttribute('href')), '/staff-fixture/order-1');
    await clickText(page, 'چاپ برچسب این واحد');
    assert.equal(await page.$eval('output', output => output.textContent), code);
    await clickText(page, 'مشاهده واحدهای همین تنوع');
    await waitText(page, 'واحدی برای این تنوع یافت نشد.');
    assert.match(requests[1].path, /^\/api\/v1\/units\?.*variant_id=variant-1/);
  } finally {
    await context.close();
  }
});

test('prefix search, malformed input, and bare SPNC use distinct outcomes', async () => {
  const { context, page, requests, search } = await openLookup(() => ({ body: { units: [], next_cursor: null } }));
  try {
    await search('  d0f91a60-x00 ');
    await waitText(page, 'واحدی با این پیشوند یافت نشد.');
    const url = new URL(requests[0].path, origin);
    assert.equal(url.pathname, '/api/v1/units');
    assert.equal(url.searchParams.get('q'), 'D0F91A60-X00');
    await search('D0F91A60-X۱۲');
    await waitText(page, 'قالب کد واحد درست نیست');
    await search('D0F91A60');
    await waitText(page, 'این کد تنوع است، نه کد واحد — کد واحد به شکل SPNC-XNNNN است.');
    assert.equal(requests.length, 1);
  } finally {
    await context.close();
  }
});

test('all eight statuses render with the defined labels', async () => {
  const labels: Record<UnitStatus, string> = {
    defined: 'تعریف‌شده (در انتظار جانمایی)',
    in_stock: 'موجود در انبار',
    reserved: 'رزرو شده',
    sold: 'فروخته‌شده',
    returned: 'برگشتی (در انتظار QC)',
    quarantined: 'قرنطینه',
    damaged: 'آسیب‌دیده',
    removed: 'خارج‌شده از انبار',
  };
  let status: UnitStatus = 'defined';
  const { context, page, search } = await openLookup(() => ({
    body: { ...fixture, status, location: status === 'removed' ? null : fixture.location,
      printed: { printed_at: null, last_printed_at: null, print_count: 0 } },
  }));
  try {
    for (const [value, label] of Object.entries(labels)) {
      status = value as UnitStatus;
      await search(code);
      await page.waitForFunction(text => document.querySelector('article [data-unit-status]')?.textContent === text, {}, label);
    }
    const removed = await page.$eval('article dl', node => node.textContent!);
    assert.match(removed, /خارج‌شده از انبار/);
    assert.match(removed, /هنوز چاپ نشده/);
    assert.doesNotMatch(removed, /BIN-7/);
  } finally {
    await context.close();
  }
});

test('404 scopes have identical copy and server error codes reach the toast', async () => {
  let response = { status: 404, body: { error: 'not_yours' } };
  const { context, page, requests, search } = await openLookup(() => response);
  try {
    const messages: string[] = [];
    for (const error of ['not_yours', 'does_not_exist']) {
      response = { status: 404, body: { error } };
      const pending = page.waitForResponse(res => res.status() === 404);
      await search(code);
      await pending;
      await waitText(page, 'واحد یافت نشد.');
      messages.push(await page.$eval('[role="status"]', node => node.textContent!));
    }
    assert.deepEqual(messages, ['واحد یافت نشد.', 'واحد یافت نشد.']);
    response = { status: 503, body: { error: 'unit_service_unavailable' } };
    await search(code);
    await waitText(page, 'unit_service_unavailable');
    assert.equal(requests.length, 3);
  } finally {
    await context.close();
  }
});

test('label card handles invalid payloads and hides the synthetic variant', async () => {
  const { context, page } = await openLookup(() => ({ body: {} }));
  try {
    await page.waitForSelector('.spncx-label', { visible: false });
    const cards = await page.$$eval('.spncx-label', nodes => nodes.map(node => ({
      copy: node.textContent, barcode: node.querySelector('.spncx-barcode svg') !== null,
      code: node.querySelector('code')?.textContent, dir: node.querySelector('code')?.dir,
    })));
    assert.equal(cards.length, 2);
    assert.match(cards[0].copy!, /قالب کد واحد درست نیست/);
    assert.equal(cards[0].barcode, false);
    assert.equal(cards[1].barcode, true);
    assert.equal(cards[1].code, code);
    assert.equal(cards[1].dir, 'ltr');
    assert.doesNotMatch(cards[1].copy!, /پیش‌فرض|موجودی ثبت‌شده|QR/);
  } finally {
    await context.close();
  }
});
