import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import test from 'node:test';
import { build } from 'esbuild';
import puppeteer from 'puppeteer';

test('mobile supplier recovers an interrupted batch, printed labels and the original retry key after reload', { timeout: 120000 }, async t => {
  let step = 'bundle the supplier workspace';
  let completed = false;
  t.after(() => { if (!completed) t.diagnostic('Last browser step: ' + step); });
  const claims = Buffer.from(JSON.stringify({ userId: 'supplier-member' })).toString('base64url');
  const session = { supplierId: 'supplier-1', token: 'fixture.' + claims + '.fixture' };
  const bundle = await build({
    bundle: true, platform: 'browser', format: 'iife', jsx: 'automatic', write: false,
    define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env': '{}' },
    stdin: {
      loader: 'tsx', resolveDir: process.cwd(),
      contents: [
        "import { createRoot } from 'react-dom/client';",
        "import { SupplierVariantWorkspace } from './src/components/supplier/SupplierVariantWorkspace';",
        'createRoot(document.getElementById("root")).render(<SupplierVariantWorkspace session={' +
          JSON.stringify(session) + '} onChanged={() => {}} />);',
      ].join('\n'),
    },
  });

  let stock = 5;
  let cost: number | null = null;
  const stockKeys: Array<string | string[] | undefined> = [];
  const costRequests: { key: string | string[] | undefined; payload: unknown }[] = [];
  let costInFlight: () => void;
  const initialCostRequest = new Promise<void>(resolve => { costInFlight = resolve; });

  const server = createServer(async (request, response) => {
    response.setHeader('Content-Type', 'application/json');
    if (request.url === '/api/v1/supplier/products/variant-workspace') {
      response.end(JSON.stringify({ items: [{
        id: 'sku-a', productId: 'product-a', title: 'کالای نمونه', name: 'آبی', spnc: 'SKU-A', active: true,
        sellable: stock, reserved: 2, cost, productCost: 1000, canEditStock: true, canEditCost: true,
      }] }));
    } else if (request.method === 'PUT' && request.url === '/api/v1/supplier/products/product-a/stock') {
      stockKeys.push(request.headers['idempotency-key']);
      stock = 4;
      response.end(JSON.stringify({ product_id: 'product-a', variant_id: 'sku-a', stock, retired_printed_codes: ['RETIRED-PRINTED-CODE'] }));
    } else if (request.method === 'PUT' && request.url === '/api/v1/supplier/products/product-a/variants/sku-a/pricing') {
      let raw = '';
      for await (const chunk of request) raw += chunk;
      costRequests.push({ key: request.headers['idempotency-key'], payload: JSON.parse(raw) });
      if (costRequests.length === 1) {
        costInFlight();
        return;
      }
      cost = 900;
      response.end(JSON.stringify({ id: 'sku-a', supplier_base_cost: cost, applied: true }));
    } else {
      response.setHeader('Content-Type', 'text/html; charset=utf-8');
      response.end('<html><body><div id="root"></div><script>' + bundle.outputFiles[0].text + '</script></body></html>');
    }
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  assert.ok(address && typeof address === 'object');
  step = 'open the browser';
  const browser = await puppeteer.launch({
    headless: true, pipe: true,
    args: ['--no-sandbox', '--disable-setuid-sandbox'],
    ...(process.platform === 'win32' ? { channel: 'chrome' as const } : {}),
  }).catch(async error => {
    server.closeAllConnections();
    await new Promise<void>(resolve => server.close(() => resolve()));
    throw error;
  });
  try {
    const page = await browser.newPage();
    const pageErrors: string[] = [];
    page.on('pageerror', error => pageErrors.push(String(error)));
    t.after(() => { if (pageErrors.length) t.diagnostic(pageErrors.join('\n')); });
    await page.setViewport({ width: 390, height: 760 });
    page.setDefaultTimeout(10000);
    const waitText = (text: string) => page.waitForFunction(value => document.body.innerText.includes(value), {}, text);
    const click = async (text: string) => {
      await page.waitForFunction(value => [...document.querySelectorAll('button')].some(button => button.innerText.trim() === value && !button.disabled), {}, text);
      await page.evaluate(value => [...document.querySelectorAll('button')].find(button => button.innerText.trim() === value)!.click(), text);
    };
    step = 'upload and preview the stock and price edits';
    await page.goto('http://127.0.0.1:' + address.port);
    await click('موجودی و قیمت عمده تنوع‌ها');
    await waitText('کالای نمونه');
    await click('ورود از Excel یا CSV');
    await page.$eval('input[type="file"]', input => {
      const files = new DataTransfer();
      files.items.add(new File(['variant_id;stock;base_cost;reason\r\nsku-a;4;900;counted'], 'count.csv'));
      (input as HTMLInputElement).files = files.files;
      input.dispatchEvent(new Event('change', { bubbles: true }));
    });
    step = 'observe the in-flight price operation after receipt of stock';
    await click('ثبت ۱ سطر');
    const costStarted = await Promise.race([
      initialCostRequest.then(() => true),
      new Promise<boolean>(resolve => AbortSignal.timeout(10000).addEventListener('abort', () => resolve(false))),
    ]);
    assert.equal(costStarted, true, await page.$eval('body', element => element.innerText));
    assert.equal(stockKeys.length, 1);
    assert.ok(stockKeys[0]);
    step = 'restore the stock receipt and unfinished edit after reload';
    await page.reload();
    await waitText('ویرایش ناتمام بازیابی شد.');
    await waitText('RETIRED-PRINTED-CODE');
    await click('متوجه شدم');
    step = 'retry only the incomplete price operation';
    await click('تلاش دوباره برای ۱ سطر');
    await waitText('قیمت عمده ثبت شد.');
    assert.equal(stockKeys.length, 1, 'an acknowledged stock adjustment must not be sent again');
    assert.equal(costRequests.length, 2);
    assert.deepEqual(costRequests[0], costRequests[1]);
    assert.ok(costRequests[0].key);
    step = 'verify that acknowledged receipts remain closed';
    await page.reload();
    assert.equal(await page.$('dialog[open]'), null, 'acknowledged labels should not reopen');
    completed = true;
  } finally {
    await browser.close();
    server.closeAllConnections();
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
});
