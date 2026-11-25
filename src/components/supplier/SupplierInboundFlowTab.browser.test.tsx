import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import test from 'node:test';
import { build } from 'esbuild';
import puppeteer from 'puppeteer';

test('supplier can combine different products, resume the draft and reach dispatch after submitting', { timeout: 60000 }, async () => {
  const bundle = await build({
    write: false, bundle: true, platform: 'browser', format: 'iife', jsx: 'automatic',
    define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env': '{}' },
    plugins: [{ name: 'supplier-session', setup(build) {
      build.onResolve({ filter: /context\/StoreContext$/ }, () => ({ path: 'store', namespace: 'test' }));
      build.onLoad({ filter: /.*/, namespace: 'test' }, () => ({
        contents: 'export const useStore=()=>({supplierSession:{supplierId:"supplier",token:"fixture"},formatPrice:String,toPersianDigits:String});',
      }));
    } }],
    stdin: { loader: 'tsx', resolveDir: process.cwd(), contents: `
      import { createRoot } from 'react-dom/client';
      import { ToastProvider } from './src/context/ToastContext';
      import { SupplierInboundFlowTab } from './src/components/supplier/SupplierInboundFlowTab';
      createRoot(document.getElementById('root')).render(
        <ToastProvider><SupplierInboundFlowTab initialProductId="product" onProductConsumed={()=>{}} canManageProducts={false} canManageShipments={true} /></ToastProvider>
      );
    ` },
  });
  const shipment = { id: 'shipment', shipment_code: 'CARTON01', item_count: 2, status: 'draft' };
  let submitted: unknown;
  const server = createServer(async (request, response) => {
    const path = request.url?.replace('/api/v1/supplier', '');
    response.setHeader('Content-Type', 'application/json');
    if (path === '/products') response.end(JSON.stringify([{ id: 'product', title: 'Test product' }, { id: 'other', title: 'Other product' }]));
    else if (path === '/products/product/variants') response.end(JSON.stringify([{ id: 'sku', variant_name: 'Test model', spnc: 'SKU001' }]));
    else if (path === '/products/other/variants') response.end(JSON.stringify([{ id: 'other-sku', variant_name: 'Other model', spnc: 'SKU002' }]));
    else if (path === '/warehouses/central') response.end(JSON.stringify([{ id: 'central', name: 'Central' }]));
    else if (path === '/warehouses') response.end(JSON.stringify([{ id: 'active', name: 'Active source', isActive: true }, { id: 'inactive', name: 'Inactive source', isActive: false }]));
    else if (path === '/inventory') response.end('{"inventory":[]}');
    else if (path === '/shipments' && request.method === 'POST') {
      let body = '';
      for await (const chunk of request) body += chunk;
      submitted = JSON.parse(body);
      response.end(JSON.stringify({ shipment }));
    } else if (path === '/shipments') response.end(JSON.stringify(submitted ? [shipment] : []));
    else if (request.url?.startsWith('/api/')) response.end('[]');
    else {
      response.setHeader('Content-Type', 'text/html; charset=utf-8');
      response.end('<div id="root"></div><script>' + bundle.outputFiles[0].text + '</script>');
    }
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  assert.ok(address && typeof address === 'object');
  try {
    const browser = await puppeteer.launch({ headless: true, args: ['--no-sandbox', `--explicitly-allowed-ports=${address.port}`], ...(process.platform === 'win32' ? { channel: 'chrome' as const } : {}) });
    try {
      const page = await browser.newPage();
      await page.goto(`http://127.0.0.1:${address.port}/`);
      const click = async (label: string) => {
        const button = await page.waitForFunction(text => [...document.querySelectorAll('button')].find(item => item.offsetWidth && item.innerText.includes(text)), {}, label);
        const element = button.asElement();
        assert.ok(element);
        await (await element.toElement('button')).click();
      };
      await click('Test model');
      await page.waitForSelector('option[value="active"]');
      assert.equal(await page.$('option[value="inactive"]'), null);
      await page.type('input[aria-label="تعداد Test model"]', '5');
      await page.select('#carton-product', 'other');
      await click('Other model');
      await page.type('input[aria-label="تعداد Other model"]', '3');
      await click('ارسال و برچسب‌ها');
      await click('آماده‌سازی محموله');
      assert.equal(await page.$eval('input[aria-label="تعداد Test model"]', input => (input as HTMLInputElement).value), '5');
      await page.select('label select', 'external_replenishment');
      await page.type('input[aria-describedby="carton-document-help"]', 'INV-1');
      await page.type('input[placeholder="مثلاً خرید از تولیدکننده برای شارژ موجودی"]', 'Purchase');
      await page.select('select[aria-label="انبار مرکزی مقصد"]', 'central');
      await click('ثبت محموله و گرفتن کد کارتن');
      await page.waitForFunction(() => document.body.innerText.includes('CARTON01') && document.body.innerText.includes('ثبت ارسال'));
      assert.deepEqual((submitted as { items: unknown }).items, [{ variant_id: 'other-sku', expected_qty: 3 }, { variant_id: 'sku', expected_qty: 5 }]);
    } finally { await browser.close(); }
  } finally {
    server.closeAllConnections();
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
});
