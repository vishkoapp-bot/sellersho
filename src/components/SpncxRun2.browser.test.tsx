import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { execFileSync } from 'node:child_process';
import { after, before, test } from 'node:test';
import { build } from 'esbuild';
import puppeteer, { type Browser } from 'puppeteer';
import { clickText, fillInput, waitText } from '../../scripts/fixtures/catalog/batch8-browser';

const code = 'D0F91A60-X0001';
const origin = 'http://localhost';
let browser: Browser;
let javascript: string;
let printCSS: string;
const label = { code, barcode_value: code, variant_name: 'آبی', spn: 'PC-1', spnc: 'D0F91A60', title: 'محصول نمونه' };
const row = { ...label, variant_id: 'variant-1', status: 'in_stock', print_count: 0 };
const receipt = { line_id: 'line-1', quantity: 1, unit_codes: [code], mismatched: [] };
const queue = { id: 'qc-1', inboundId: 'inbound-1', receiptLineId: 'line-1', variantId: 'variant-1', productId: 'p1', productTitle: 'محصول نمونه', receivedQuantity: 1, expectedQuantity: 1, passedQuantity: 0, failedQuantity: 0, remainingQuantity: 1 };

before(async () => {
  const bundle = await build({
    write: false, bundle: true, platform: 'browser', format: 'iife', jsx: 'automatic',
    define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env': '{}' },
    plugins: [{ name: 'staff-store-fixture', setup(build) {
      build.onResolve({ filter: /\/StoreContext$/ }, () => ({ path: 'store', namespace: 'staff-fixture' }));
      build.onLoad({ filter: /.*/, namespace: 'staff-fixture' }, () => ({
        contents: 'export const useStore = () => ({ toPersianDigits: String, stores: [] })',
      }));
    } }],
    stdin: { loader: 'tsx', resolveDir: process.cwd(), contents: `
      import { createRoot } from 'react-dom/client';
      import { ToastProvider } from './src/context/ToastContext';
      import { SupplierVariantsModal } from './src/components/supplier/SupplierVariantsModal';
      import { WarehouseReceiveTab, WarehouseCustomerReturnsTab } from './src/components/warehouse/WarehouseNewTabs';
      import { WarehouseInboundTab } from './src/components/warehouse/WarehouseInboundTab';
      import { WarehouseQCTab } from './src/components/warehouse/WarehouseQCTab';
      import { WarehousePackingTab } from './src/components/warehouse/WarehousePackingTab';
      import { WarehousePickingTab } from './src/components/warehouse/WarehousePickingTab';
      const views = {
        supplier: <SupplierVariantsModal productId="p1" productTitle="محصول نمونه" onClose={() => {}} />,
        receive: <WarehouseReceiveTab />,
        asn: <WarehouseInboundTab onGoToQC={() => {}} />,
        asnMixed: <WarehouseInboundTab onGoToQC={() => {}} />,
        qc: <WarehouseQCTab onGoToPutaway={() => {}} />,
        returns: <WarehouseCustomerReturnsTab />,
        packing: <WarehousePackingTab onGoToShipping={() => {}} />,
        picking: <WarehousePickingTab onGoToPacking={() => {}} />,
      };
      window.printCount = 0;
      window.print = () => {
        document.querySelector('output').textContent = [...document.querySelectorAll('.spncx-label code')].map(el => el.textContent).join(',');
        window.printCount = (window.printCount ?? 0) + 1;
      };
      createRoot(document.querySelector('#root')).render(<ToastProvider>{views[location.hash.slice(1)]}</ToastProvider>);
    ` },
  });
  javascript = bundle.outputFiles[0].text;
  const css = await readFile('src/index.css', 'utf8');
  printCSS = css.slice(css.indexOf('@media print'));
  browser = await puppeteer.launch({ executablePath: process.env.CHROME_PATH, pipe: true, args: ['--no-sandbox', '--disable-setuid-sandbox', '--disable-dev-shm-usage'] });
});
after(async () => { await browser?.close(); });

type Request = { path: string; method: string; body?: unknown };
class ApiReply {
  constructor(readonly status: number, readonly body: unknown) {}
}
async function open(view: string, reply: (request: Request) => unknown) {
  const context = await browser.createBrowserContext();
  const page = await context.newPage();
  await page.setViewport({ width: 390, height: 800 });
  page.setDefaultTimeout(15000);
  const requests: Request[] = [];
  await page.setRequestInterception(true);
  page.on('request', request => {
    const url = new URL(request.url());
    if (request.isNavigationRequest() && url.origin === origin) {
      void request.respond({ contentType: 'text/html; charset=utf-8', body: '<meta charset="utf-8"><main id="root"></main><output></output>' });
    } else if (url.origin === origin && url.pathname.startsWith('/api/')) {
      const entry = { path: url.pathname + url.search, method: request.method(), body: JSON.parse(request.postData() || 'null') };
      requests.push(entry);
      const response = reply(entry);
      if (response === 'disconnect') { void request.abort(); return; }
      const status = response instanceof ApiReply ? response.status : response === undefined ? 404 : 200;
      const body = response instanceof ApiReply ? response.body : response;
      void request.respond({ status, contentType: 'application/json', body: JSON.stringify(body ?? {}) });
    } else void request.abort();
  });
  await page.goto(origin + '/#' + view);
  await page.addStyleTag({ content: printCSS });
  await page.addScriptTag({ content: javascript });
  return { page, requests, close: () => context.close() };
}

test('packing displays the variant and allocated code, then hides code elements in print media', async () => {
  const { page, requests, close } = await open('packing', request => {
    if (request.path.endsWith('/ready-to-pack')) return { orders: [{ id: 'order-1', orderNumber: 'ORDER-1', items: [{ id: 'item-1', productTitle: 'محصول نمونه', variantName: 'آبی', quantity: 1 }] }] };
    if (request.path.includes('order_item_id=item-1')) return { units: [row], next_cursor: null };
  });
  try {
    await waitText(page, code);
    await waitText(page, 'محصول نمونه — آبی');
    assert.equal(requests.filter(request => request.path.includes('order_item_id=item-1')).length, 1);
    const selector = 'div.spncx-pack-unit-code code';
    assert.equal(await page.$eval(selector, node => node.textContent), code);
    await page.emulateMediaType('print');
    assert.deepEqual(await page.$$eval('.spncx-pack-unit-code', nodes => nodes.map(node => ({
      display: getComputedStyle(node).display, boxes: node.getClientRects().length,
    }))), [{ display: 'none', boxes: 0 }, { display: 'none', boxes: 0 }]);
    const pdf = await page.pdf();
    assert.doesNotMatch(execFileSync('pdftotext', ['-', '-'], { input: pdf, encoding: 'utf8' }), new RegExp(code));
  } finally { await close(); }
});

test('supplier preview marks selected issued codes before handing the response sheet to print', async () => {
  const { page, requests, close } = await open('supplier', request => {
    if (request.path.endsWith('/variants')) return [];
    if (request.path.endsWith('/labels')) return { labels: [{ spnc: label.spnc }], spn: label.spn };
    if (request.path.includes('/labels/units')) return { units: [row], total_for_filter: 1, next_cursor: null };
    if (request.path.endsWith('/labels/print')) return { units: [label] };
    if (request.path.endsWith('/price-increases')) return [];
  });
  try {
    await clickText(page, 'چاپ برچسب');
    await clickText(page, 'انتخاب همه (صفحه)');
    await clickText(page, 'چاپ', 'button[data-unit-print]');
    await page.waitForFunction(code => document.querySelector('output')?.textContent === code, {}, code);
    const post = requests.find(request => request.path.endsWith('/labels/print'));
    assert.deepEqual((post?.body as { unit_codes: string[] }).unit_codes, [code]);
    assert.match((post?.body as { idempotency_key: string }).idempotency_key, /^[a-f0-9-]{36}$/);
  } finally { await close(); }
});

test('shipment receive submits complete scans and exposes the server receipt line for printing', async () => {
  const { page, requests, close } = await open('receive', request => {
    if (request.path.endsWith('/shipments/BOX-1')) return { shipment: { shipment_code: 'BOX-1', items: [{ variant_id: 'variant-1', expected_qty: 1 }] } };
    if (request.path.endsWith('/shipments/BOX-1/receive')) return receipt;
    if (request.path.endsWith('/warehouse/units/print')) return { units: [label] };
  });
  try {
    await fillInput(page, 'input[placeholder="کد محموله روی کارتن (اسکن یا تایپ دستی)"]', 'BOX-1');
    await page.keyboard.press('Enter');
    await fillInput(page, 'input[aria-label="اسکن کد واحد"]', ' d0f91a60-x0001 ');
    await page.keyboard.press('Enter');
    await clickText(page, 'تأیید اسکن و QC این قلم');
    await waitText(page, '1 از 1 واحد اسکن شد');
    await clickText(page, 'تأیید دریافت محموله (1 قلم تأییدشده)');
    await clickText(page, 'چاپ برچسب واحد');
    await page.waitForFunction(code => document.querySelector('output')?.textContent === code, {}, code);
    assert.deepEqual(requests.find(request => request.path.endsWith('/receive'))?.body, { items: [{ variant_id: 'variant-1', received_qty: 1, qc_status: 'pass', unit_codes: [code] }] });
    assert.equal((requests.find(request => request.path.endsWith('/units/print'))?.body as { receipt_line_id: string }).receipt_line_id, 'line-1');
  } finally { await close(); }
});

test('QC queue unit-print response paginates 5 and 50 issued labels at 100 by 60 mm', async () => {
  for (const count of [5, 50]) {
    const units = Array.from({ length: count }, (_, index) => {
      const serial = String(index + 10001).slice(1);
      const value = ['D0F91A60', 'X' + serial].join('-');
      return { ...label, code: value, barcode_value: value };
    });
    const { page, close } = await open('qc', request => {
      if (request.path.endsWith('/qc/queue')) return { success: true, queue: [queue] };
      if (request.path.endsWith('/units/print')) return { units };
    });
    try {
      await clickText(page, 'چاپ برچسب واحد');
      await page.waitForFunction(count => document.querySelectorAll('.spncx-label').length === count, {}, count);
      const pdf = await page.pdf({ preferCSSPageSize: true });
      const details = execFileSync('pdfinfo', ['-'], { input: pdf, encoding: 'utf8' });
      assert.match(details, new RegExp(`Pages:\\s+${count}\\b`));
      assert.match(details, /Page size:\s+282\.96 x 169\.92 pts/);
      const printed = execFileSync('pdftotext', ['-layout', '-', '-'], { input: pdf, encoding: 'utf8' }).split('\f').filter(page => page.trim());
      assert.equal(printed.length, count);
      printed.forEach((page, index) => assert.deepEqual(page.match(/D0F91A60-X\d{4}/g), [units[index].code]));
    } finally { await close(); }
  }
});

test('ASN receive omits codes on quantity-only receipts and shows the issued response codes', async () => {
  const { page, requests, close } = await open('asn', request => {
    if (request.path.endsWith('/inbounds')) return [{ id: 'ASN-1', status: 'receiving', items: [{ line_id: 'line-1', quantity: 1 }] }];
    if (request.path.endsWith('/inbound-lines/line-1/receive')) return receipt;
  });
  try {
    await clickText(page, 'ثبت دریافت ردیف');
    await waitText(page, code);
    assert.deepEqual(requests.find(request => request.path.endsWith('/inbound-lines/line-1/receive'))?.body, { received_quantity: 1 });
    await waitText(page, 'چاپ برچسب واحد');
  } finally { await close(); }
});

const lookup = {
  code, status: 'sold', history: [],
  order: { order_id: 'order-1', order_number: 'ORDER-1' },
  variant: { id: 'variant-1', spnc: label.spnc, name: label.variant_name },
  product: { id: 'p1', spn: label.spn, title: label.title },
  printed: { printed_at: null, print_count: 0, last_printed_at: null },
};

test('QC pass submits the scanned codes and renders the accepted units', async () => {
  const { page, requests, close } = await open('qc', request => {
    if (request.path.endsWith('/qc/queue')) return { success: true, queue: [queue] };
    if (request.path.endsWith('/units/' + code)) return { ...lookup, status: 'quarantined' };
    if (request.path.endsWith('/qc/line-1/pass')) return receipt;
  });
  try {
    await fillInput(page, 'input[aria-label="اسکن کد واحد"]', code);
    await page.keyboard.press('Enter');
    await waitText(page, '1 از 1 واحد اسکن شد');
    await page.type('textarea', 'synthetic QC pass');
    await clickText(page, 'ثبت و تأیید نتیجه QC');
    await waitText(page, 'واحدهای سالم ثبت‌شده');
    assert.deepEqual(requests.find(request => request.path.endsWith('/qc/line-1/pass'))?.body, { quantity: 1, unit_codes: [code] });
  } finally { await close(); }
});

test('return receipt rejects another order, then submits the verified sold unit for its order item', async () => {
  let sameOrder = false;
  let received = false;
  const { page, requests, close } = await open('returns', request => {
    if (request.path.endsWith('/warehouses/returns')) return [{ id: 'return-1', orderId: 'order-1', status: 'in_transit', items: [{ orderItemId: 'item-1', quantity: 1 }] }];
    if (request.path.endsWith('/units/' + code)) return { ...lookup, status: received ? 'returned' : 'sold', order: { order_id: sameOrder ? 'order-1' : 'order-2' } };
    if (request.path.endsWith('/returns/return-1/receive')) { received = true; return receipt; }
  });
  try {
    await page.waitForSelector('input[type=number]');
    assert.equal(await page.$eval('input[type=number]', input => (input as HTMLInputElement).readOnly), true);
    await fillInput(page, 'input[aria-label="اسکن کد واحد"]', code);
    await page.keyboard.press('Enter');
    await waitText(page, 'این واحد متعلق به سفارش دیگری است.');
    assert.equal(requests.filter(request => request.method === 'POST').length, 0);
    sameOrder = true;
    await page.keyboard.press('Enter');
    await waitText(page, '1 از 1 واحد اسکن شد');
    await clickText(page, 'ثبت دریافت مرجوعی');
    await waitText(page, 'دریافت مرجوعی ثبت شد.');
    await page.waitForSelector('[data-unit-status]');
    assert.deepEqual(requests.find(request => request.method === 'POST')?.body, { items: [{ order_item_id: 'item-1', qty: 1, unit_codes: [code] }] });
  } finally { await close(); }
});

test('supplier retries the same job after a dropped response and reports all-or-nothing print rejection', async () => {
  let attempts = 0;
  const { page, requests, close } = await open('supplier', request => {
    if (request.path.endsWith('/variants') || request.path.endsWith('/price-increases')) return [];
    if (request.path.endsWith('/labels')) return { labels: [{ spnc: label.spnc }], spn: label.spn };
    if (request.path.includes('/labels/units')) return { units: [row], total_for_filter: 1, next_cursor: null };
    if (request.path.endsWith('/labels/print')) {
      return ++attempts === 1 ? 'disconnect' : new ApiReply(422, {
        error: 'unit_not_printable', errors: [{ code, error: 'unit_not_printable', status: 'sold' }],
      });
    }
  });
  try {
    await page.waitForFunction(() => [...document.querySelectorAll('button')].some(button => button.textContent?.trim() === 'چاپ برچسب' && !button.disabled));
    await clickText(page, 'چاپ برچسب');
    await clickText(page, 'انتخاب همه (صفحه)');
    await page.waitForSelector('.no-print .spncx-label', { visible: true });
    assert.equal(await page.$('.spncx-print-sheet'), null);
    await clickText(page, 'چاپ', 'button[data-unit-print]');
    await waitText(page, 'خطای ارتباط با سرور');
    await clickText(page, 'چاپ', 'button[data-unit-print]');
    await waitText(page, `${code}: فروخته‌شده`);
    const bodies = requests.filter(request => request.method === 'POST').map(request => request.body);
    assert.equal(bodies.length, 2);
    assert.deepEqual(bodies[0], bodies[1]);
    assert.equal(await page.$eval('output', node => node.textContent), '');
  } finally { await close(); }
});

test('QC rejects a partial scan and shows the mismatched unit from a 422 response', async () => {
  const { page, requests, close } = await open('qc', request => {
    if (request.path.endsWith('/qc/queue')) return { success: true, queue: [{ ...queue, expectedQuantity: 2, receivedQuantity: 2, remainingQuantity: 2 }] };
    if (request.path.endsWith('/units/' + code)) return lookup;
    if (request.path.endsWith('/qc/line-1/pass')) return new ApiReply(422, {
      error: 'unit_mismatch', line_id: 'line-1', errors: [{ code, error: 'unit_mismatch', expected: 'line-1', actual: 'other-line' }],
    });
  });
  try {
    await fillInput(page, 'input[aria-label="اسکن کد واحد"]', code);
    await page.keyboard.press('Enter');
    await waitText(page, '1 از 2 واحد اسکن شد');
    await page.type('textarea', 'synthetic QC reason');
    await clickText(page, 'ثبت و تأیید نتیجه QC');
    await waitText(page, 'همه واحدهای این نتیجه را اسکن کنید');
    assert.equal(requests.filter(request => request.method === 'POST').length, 0);
    await fillInput(page, 'input[type=number]', '1');
    await clickText(page, 'ثبت و تأیید نتیجه QC');
    await waitText(page, 'unit_mismatch / line-1 / other-line');
    assert.equal(requests.filter(request => request.method === 'POST').length, 1);
    await waitText(page, '1 از 1 واحد اسکن شد');
  } finally { await close(); }
});

test('warehouse reprints the exact issued codes with a fresh key and explicit confirmation', async () => {
  const { page, requests, close } = await open('qc', request => {
    if (request.path.endsWith('/qc/queue')) return { success: true, queue: [queue] };
    if (request.path.endsWith('/units/print')) return { units: [label] };
  });
  try {
    await clickText(page, 'چاپ برچسب واحد');
    await clickText(page, 'چاپ مجدد همین برچسب‌ها');
    await page.waitForSelector('input[type=checkbox]');
    assert.equal(requests.filter(request => request.method === 'POST').length, 1);
    await page.click('input[type=checkbox]');
    await clickText(page, 'چاپ مجدد همان کدها');
    await page.waitForFunction(() => (window as unknown as { printCount: number }).printCount === 2);
    const posted = requests.filter(request => request.method === 'POST').map(request => request.body as Record<string, unknown>);
    assert.deepEqual(posted[0], { receipt_line_id: 'line-1', idempotency_key: posted[0].idempotency_key });
    assert.deepEqual(posted[1], { unit_codes: [code], idempotency_key: posted[1].idempotency_key, reprint: true });
    assert.notEqual(posted[0].idempotency_key, posted[1].idempotency_key);
  } finally { await close(); }
});


test('unavailable supplier serial endpoint hides unit print actions', async () => {
  const { page, close } = await open('supplier', request => {
    if (request.path.endsWith('/variants') || request.path.endsWith('/price-increases')) return [];
    if (request.path.endsWith('/labels')) return { labels: [{ spnc: label.spnc }], spn: label.spn };
    if (request.path.includes('/labels/units')) return new ApiReply(501, { error: 'not_implemented' });
  });
  try {
    await clickText(page, 'چاپ برچسب');
    await page.waitForFunction(() => document.querySelector('button[aria-pressed="true"]')?.textContent === 'برچسب تنوع');
    await clickText(page, 'برچسب واحد');
    await waitText(page, 'قابلیت سریال واحد هنوز فعال نشده است.');
    assert.equal(await page.$('button[data-unit-print]'), null);
  } finally { await close(); }
});

test('unavailable warehouse print endpoint hides its action buttons', async () => {
  const { page, close } = await open('qc', request => {
    if (request.path.endsWith('/qc/queue')) return { success: true, queue: [queue] };
    if (request.path.endsWith('/units/print')) return new ApiReply(404, { error: 'not_found' });
  });
  try {
    await clickText(page, 'چاپ برچسب واحد');
    await waitText(page, 'قابلیت سریال واحد هنوز فعال نشده است.');
    assert.equal(await page.$$eval('button', nodes => nodes.some(node => node.textContent === 'چاپ برچسب واحد')), false);
    assert.equal(await page.$eval('output', node => node.textContent), '');
  } finally { await close(); }
});

test('supplier duplicate conflict preserves the machine code and requires deliberate reprint', async () => {
  let attempts = 0;
  const { page, requests, close } = await open('supplier', request => {
    if (request.path.endsWith('/variants') || request.path.endsWith('/price-increases')) return [];
    if (request.path.endsWith('/labels')) return { labels: [{ spnc: label.spnc }], spn: label.spn };
    if (request.path.includes('/labels/units')) return { units: [row], total_for_filter: 1, next_cursor: null };
    if (request.path.endsWith('/labels/print')) return ++attempts === 1
      ? new ApiReply(409, { error: 'already_printed_in_job' }) : { units: [label] };
  });
  try {
    await clickText(page, 'چاپ برچسب');
    await clickText(page, 'انتخاب همه (صفحه)');
    await clickText(page, 'چاپ', 'button[data-unit-print]');
    await page.waitForSelector('[role=alert]');
    assert.match(await page.$eval('[role=alert]', node => node.textContent || ''), /already_printed_in_job/);
    assert.equal(requests.filter(request => request.method === 'POST').length, 1);
    assert.equal(await page.$eval('button[data-unit-print]', node => (node as HTMLButtonElement).disabled), true);
    await page.$$eval('label', nodes => (nodes.find(node => node.textContent?.includes('می‌دانم این‌ها'))?.querySelector('input') as HTMLInputElement).click());
    await clickText(page, 'چاپ مجدد همان کدها', 'button[data-unit-print]');
    await page.waitForFunction(() => (window as unknown as { printCount: number }).printCount === 1);
    const bodies = requests.filter(request => request.method === 'POST').map(request => request.body as Record<string, unknown>);
    assert.equal(bodies[0].reprint, undefined);
    assert.equal(bodies[1].reprint, true);
    assert.notEqual(bodies[0].idempotency_key, bodies[1].idempotency_key);
  } finally { await close(); }
});

test('supplier count shortcut requires a variant and prints the server-resolved next set', async () => {
  const next = { ...label, code: 'D0F91A60-X0002', barcode_value: 'D0F91A60-X0002' };
  const { page, requests, close } = await open('supplier', request => {
    if (request.path.endsWith('/variants')) return [{ id: 'variant-1', variant_name: 'آبی', spnc: label.spnc, stock: 1 }];
    if (request.path.endsWith('/price-increases')) return [];
    if (request.path.endsWith('/labels')) return { labels: [{ spnc: label.spnc }], spn: label.spn };
    if (request.path.includes('/labels/units')) return { units: [row], total_for_filter: 1, next_cursor: null };
    if (request.path.endsWith('/labels/print')) return { units: [next] };
  });
  try {
    await clickText(page, 'چاپ برچسب');
    await clickText(page, 'انتخاب همه (صفحه)');
    assert.equal(await page.$eval('input[type=number]', node => (node as HTMLInputElement).disabled), true);
    await page.select('select', 'variant-1');
    await page.waitForFunction(() => !(document.querySelector('fieldset') as HTMLFieldSetElement).disabled);
    await clickText(page, 'چاپ 1 برچسب بعدی');
    await page.waitForFunction(code => document.querySelector('output')?.textContent === code, {}, next.code);
    const body = requests.find(request => request.method === 'POST')?.body as Record<string, unknown>;
    assert.deepEqual({ ...body, idempotency_key: undefined }, { variant_id: 'variant-1', count: 1, unprinted_only: true, idempotency_key: undefined });
  } finally { await close(); }
});


test('supplier key mismatch prints nothing and preserves the selection', async () => {
  const { page, requests, close } = await open('supplier', request => {
    if (request.path.endsWith('/variants') || request.path.endsWith('/price-increases')) return [];
    if (request.path.endsWith('/labels')) return { labels: [{ spnc: label.spnc }], spn: label.spn };
    if (request.path.includes('/labels/units')) return { units: [row], total_for_filter: 1, next_cursor: null };
    if (request.path.endsWith('/labels/print')) return new ApiReply(409, { error: 'idempotency_key_mismatch' });
  });
  try {
    await clickText(page, 'چاپ برچسب');
    await clickText(page, 'انتخاب همه (صفحه)');
    await page.$eval('button[data-unit-print]', node => { (node as HTMLButtonElement).click(); (node as HTMLButtonElement).click(); });
    await waitText(page, 'idempotency_key_mismatch');
    assert.equal(requests.filter(request => request.method === 'POST').length, 1);
    assert.equal(await page.$eval('li input', node => (node as HTMLInputElement).checked), true);
    assert.equal(await page.$eval('output', node => node.textContent), '');
  } finally { await close(); }
});

test('supplier paginated selection preserves server order and legacy variant printing remains available', async () => {
  const second = { ...row, code: 'D0F91A60-X0002' };
  const { page, requests, close } = await open('supplier', request => {
    if (request.path.endsWith('/variants')) return [{ id: 'variant-1', variant_name: 'آبی', spnc: label.spnc, stock: 2 }];
    if (request.path.endsWith('/price-increases')) return [];
    if (request.path.endsWith('/labels')) return { labels: [{ spnc: label.spnc, qr_payload: 'legacy-payload' }], spn: label.spn };
    if (request.path.includes('/labels/units')) return { units: request.path.includes('cursor=page2') ? [second] : [row], total_for_filter: 2, next_cursor: request.path.includes('cursor=page2') ? null : 'page2' };
    if (request.path.endsWith('/labels/print')) return { units: [{ ...label, code: second.code, barcode_value: second.code }, label] };
  });
  try {
    await clickText(page, 'چاپ برچسب');
    await clickText(page, 'انتخاب همه (2)');
    await page.waitForFunction(() => document.querySelectorAll('li input:checked').length === 2);
    await clickText(page, 'چاپ', 'button[data-unit-print]');
    await page.waitForFunction(expected => document.querySelector('output')?.textContent === expected, {}, second.code + ',' + code);
    assert.deepEqual((requests.find(request => request.method === 'POST')?.body as { unit_codes: string[] }).unit_codes, [code, second.code]);
    await clickText(page, 'برچسب تنوع');
    await waitText(page, 'legacy-payload');
    await waitText(page, 'موجودی ثبت‌شده: 2 عدد');
    assert.equal(await page.$('.spncx-print-sheet'), null);
  } finally { await close(); }
});

test('supplier print response paginates 5 and 50 issued labels without blank pages', async () => {
  for (const count of [5, 50]) {
    const units = Array.from({ length: count }, (_, index) => {
      const value = ['D0F91A60', 'X' + String(index + 10001).slice(1)].join('-');
      return { ...label, code: value, barcode_value: value };
    });
    const { page, close } = await open('supplier', request => {
      if (request.path.endsWith('/variants') || request.path.endsWith('/price-increases')) return [];
      if (request.path.endsWith('/labels')) return { labels: [], spn: label.spn };
      if (request.path.includes('/labels/units')) return { units: units.map(unit => ({ ...row, ...unit })), total_for_filter: count, next_cursor: null };
      if (request.path.endsWith('/labels/print')) return { units };
    });
    try {
      await clickText(page, 'چاپ برچسب');
      await clickText(page, 'انتخاب همه (صفحه)');
      await clickText(page, 'چاپ', 'button[data-unit-print]');
      await page.waitForSelector('.spncx-print-sheet');
      const pdf = await page.pdf({ preferCSSPageSize: true });
      const details = execFileSync('pdfinfo', ['-'], { input: pdf, encoding: 'utf8' });
      assert.match(details, new RegExp('Pages:\\s+' + count + '\\b'));
      assert.match(details, /Page size:\s+282\.96 x 169\.92 pts/);
      const pages = execFileSync('pdftotext', ['-layout', '-', '-'], { input: pdf, encoding: 'utf8' }).split('\f').filter(page => page.trim());
      assert.equal(pages.length, count);
      pages.forEach((page, index) => assert.deepEqual(page.match(/D0F91A60-X\d{4}/g), [units[index].code]));
    } finally { await close(); }
  }
});

test('mixed QC sends only the failed delta scan to complete QC', async () => {
  const { page, requests, close } = await open('qc', request => {
    if (request.path === '/api/v1/warehouse/qc/queue') {
      return { success: true, queue: [{ ...queue, expectedQuantity: 2, receivedQuantity: 2, remainingQuantity: 2 }] };
    }
    if (request.path === '/api/v1/units/' + code) return { ...lookup, status: 'quarantined' };
    if (request.path === '/api/v1/warehouse/qc/p1/complete') {
      return { success: true, status: 'completed', unit_codes: [code] };
    }
  });
  try {
    await fillInput(page, 'input[type=number]', '1');
    await fillInput(page, 'label:nth-child(2) input[type=number]', '1');
    await page.type('textarea', 'synthetic mixed QC reason');
    await fillInput(page, 'input[aria-label="اسکن کد واحد"]', code);
    await page.keyboard.press('Enter');
    await waitText(page, '1 از 1 واحد اسکن شد');
    await clickText(page, 'ثبت و تأیید نتیجه QC');
    await waitText(page, 'واحدهای آسیب‌دیده ثبت‌شده');
    assert.deepEqual(await page.$$eval('[role=status] code', nodes => nodes.map(node => node.textContent)), [code]);
    assert.deepEqual(requests.filter(request => request.method === 'POST'), [{
      method: 'POST', path: '/api/v1/warehouse/qc/p1/complete',
      body: {
        inboundId: 'inbound-1', receiptLineId: 'line-1', passedQuantity: 1, failedQuantity: 1,
        shortageQuantity: 0, defectReason: 'synthetic mixed QC reason', unit_codes: [code],
      },
    }]);
  } finally { await close(); }
});

test('warehouse deliberate reprint of a sold unit surfaces the 422 and prints nothing', async () => {
  const { page, requests, close } = await open('qc', request => {
    if (request.path === '/api/v1/warehouse/qc/queue') return { success: true, queue: [queue] };
    if (request.path === '/api/v1/units/' + code) {
      return { ...lookup, printed: { printed_at: '2026-11-23T00:00:00Z', print_count: 1, last_printed_at: '2026-11-23T00:00:00Z' } };
    }
    if (request.path === '/api/v1/warehouse/units/print') {
      return new ApiReply(422, { error: 'unit_not_printable', errors: [{ code, error: 'unit_not_printable', status: 'sold' }] });
    }
  });
  try {
    await clickText(page, 'چاپ مجدد برچسب واحد');
    await fillInput(page, 'input[type=text]', code);
    await page.keyboard.press('Enter');
    await clickText(page, 'چاپ برچسب این واحد');
    await page.click('input[type=checkbox]');
    await clickText(page, 'چاپ مجدد همان کدها');
    await page.waitForSelector('[role=alert]');
    const failure = await page.$eval('[role=alert]', node => node.textContent ?? '');
    assert.match(failure, /unit_not_printable/);
    assert.ok(failure.includes(code));
    const posts = requests.filter(request => request.method === 'POST');
    assert.equal(posts.length, 1);
    assert.equal(posts[0].path, '/api/v1/warehouse/units/print');
    const body = posts[0].body as { idempotency_key: string; reprint: boolean; unit_codes: string[] };
    assert.match(body.idempotency_key, /^[a-f0-9-]{36}$/);
    assert.deepEqual(body, { unit_codes: [code], reprint: true, idempotency_key: body.idempotency_key });
    assert.equal(await page.$eval('output', node => node.textContent), '');
    assert.equal(await page.evaluate(() => (window as unknown as { printCount: number }).printCount), 0);
  } finally { await close(); }
});

test('picking ticks allocated codes locally and falls back to quantity for a line with no units', async () => {
  const secondCode = 'D0F91A60-X0002';
  const thirdCode = 'D0F91A60-X0003';
  const fallback = 'بدون کد واحد (سفارش پیش از سریال‌دار شدن)';
  const { page, requests, close } = await open('picking', request => {
    if (request.path === '/api/v1/warehouse/orders/ready-to-pick') {
      return { orders: [{ id: 'order-1', orderNumber: 'ORDER-1', status: 'ready_to_pick', items: [
        { orderItemId: 'item-1', productTitle: 'قلم الف', quantity: 3 },
        { orderItemId: 'item-2', productTitle: 'قلم ب', quantity: 1 },
      ] }] };
    }
    // The checklist pages at the server max (200/page): item-1 arrives in
    // two pages, so all three allocated codes must render, not just the
    // first page.
    if (request.path === '/api/v1/units?order_item_id=item-1&limit=200') {
      return { units: [row, { ...row, code: secondCode }], next_cursor: 'p2' };
    }
    if (request.path === '/api/v1/units?order_item_id=item-1&cursor=p2&limit=200') {
      return { units: [{ ...row, code: thirdCode }], next_cursor: null };
    }
    if (request.path === '/api/v1/units?order_item_id=item-2&limit=200') return { units: [], next_cursor: null };
  });
  try {
    await waitText(page, '0 از 3 تیک خورد');
    await waitText(page, fallback);
    const checklist = await page.$$eval('label:has(input[type=checkbox])', nodes => nodes.map(node => ({
      code: node.querySelector('code')?.textContent,
      direction: node.querySelector('code')?.dir,
      disabled: node.querySelector('input')?.disabled,
    })));
    assert.deepEqual(checklist, [code, secondCode, thirdCode].map(value => ({ code: value, direction: 'ltr', disabled: false })));
    assert.equal(await page.$$eval('p', (nodes, text) => nodes.filter(node => node.textContent === text).length, fallback), 1);
    await page.click('label:has(input[type=checkbox]) input');
    await waitText(page, '1 از 3 تیک خورد');
    assert.equal(await page.$$eval('input[type=checkbox]:checked', nodes => nodes.length), 1);
    assert.deepEqual(requests.filter(request => request.path.startsWith('/api/v1/units')).map(request => [request.method, request.path]).sort(), [
      ['GET', '/api/v1/units?order_item_id=item-1&cursor=p2&limit=200'],
      ['GET', '/api/v1/units?order_item_id=item-1&limit=200'],
      ['GET', '/api/v1/units?order_item_id=item-2&limit=200'],
    ]);
    assert.equal(requests.filter(request => request.method === 'POST').length, 0);
  } finally { await close(); }
});

test('mixed ASN line IDs expose one receive control and block manifest receive', async () => {
  const { page, requests, close } = await open('asnMixed', request => {
    if (request.path === '/api/v1/warehouse/inbounds') return [{
      id: 'ASN-MIXED', status: 'receiving', items: [{ line_id: 'line-1', quantity: 1 }, { quantity: 1 }],
    }];
  });
  try {
    await waitText(page, 'ثبت دریافت ردیف');
    const controls = await page.$$eval('button', nodes => nodes
      .filter(node => node.textContent === 'ثبت دریافت ردیف').map(node => node.parentElement?.querySelector('bdi')?.textContent));
    assert.deepEqual(controls, ['line-1']);
    await fillInput(page, 'input[placeholder="کد محموله را اسکن یا وارد کنید"]', 'ASN-MIXED');
    await page.keyboard.press('Enter');
    await waitText(page, 'برای این محموله، دریافت را در هر ردیف ورودی ثبت کنید.');
    assert.equal(requests.filter(request => request.method === 'POST').length, 0);
  } finally { await close(); }
});

test('warehouse receive dashboard resolves a unit in its lookup section', async () => {
  const warehouseName = 'انبار آزمایشی';
  const { page, requests, close } = await open('receive', request => {
    if (request.path === '/api/v1/units/' + code) return {
      ...lookup, status: 'in_stock', order: null,
      location: { balance_id: 'balance-1', warehouse_name: warehouseName, bin_code: 'BIN-1', lot_status: 'available', location_key: 'bin:BIN-1' },
      history: [{ event_type: 'minted', from_status: null, to_status: 'in_stock', reference_type: 'inbound', reference_id: 'line-1', actor_user_id: null, created_at: '2026-11-23T00:00:00Z' }],
    };
  });
  try {
    await waitText(page, 'جست‌وجوی واحد');
    await fillInput(page, 'section[aria-labelledby="warehouse-unit-lookup-heading"] input', code.toLowerCase());
    await page.keyboard.press('Enter');
    await waitText(page, warehouseName);
    const result = await page.$eval('section[aria-labelledby="warehouse-unit-lookup-heading"]', node => node.textContent ?? '');
    for (const text of [code, warehouseName, 'BIN-1', 'صدور']) assert.ok(result.includes(text));
    assert.deepEqual(requests, [{ method: 'GET', path: '/api/v1/units/' + code, body: null }]);
  } finally { await close(); }
});
