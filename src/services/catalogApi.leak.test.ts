import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { build } from 'esbuild';
import puppeteer from 'puppeteer';
import type { Order } from '../types';
import type { ProductDto, ProductVariantDto } from './catalogApi';

const unitPattern = /[0-9A-Z]+-X[0-9]{4}/;
const variantKeys = ['id', 'name', 'variantName', 'spnc', 'attributes', 'isDefault', 'stock', 'minCartQty', 'maxCartQty'] as const;
const existingOptionalKeys = [
  'selection', 'combinationKey', 'inventoryMode', 'isActive', 'supplierBaseCost', 'imageUrl', 'images',
  'weightGrams', 'lengthMm', 'widthMm', 'heightMm', 'shippingClass', 'taxClass', 'salePrice', 'saleFrom', 'saleTo',
] as const;
// The current tree calls the spec's CatalogVariantDto ProductVariantDto and
// already has optional extensions. Freeze those keys as well as the base keys.
type VariantKeyContract = (typeof variantKeys)[number] | (typeof existingOptionalKeys)[number];
const noUnexpectedVariantKeys: Record<Exclude<keyof ProductVariantDto, VariantKeyContract>, never> = {};

const red = {
  id: 'red', name: 'تنوع قرمز', variantName: 'تنوع قرمز', spnc: 'D0F91A60', attributes: {},
  isDefault: false, stock: 1, minCartQty: 1, maxCartQty: null,
} satisfies ProductVariantDto;
const blue = { ...red, id: 'blue', name: 'تنوع آبی', variantName: 'تنوع آبی', spnc: '85D123B1', isDefault: true };
const serverUnits = [{ variant_id: red.id, code: 'D0F91A60-X0001' }];
const publicProduct = {
  id: 'catalog-fixture', spnNumber: 3724980, slug: 'serialized-fixture', productType: 'variable',
  title: 'کالای نمونه سریال‌دار', category: 'digital', warehouse: 'fixture', platformFeeF: 0,
  description: 'کالای آزمایشی', price: 100, originalPrice: 100, stock: 2,
  image: '', isOriginal: true, isActive: true, variants: [red, blue],
  variantPrices: {
    red: { price: 150, originalPrice: 150, discountPercent: 0 },
    blue: { price: 100, originalPrice: 100, discountPercent: 0 },
  },
} satisfies ProductDto;
const fixture = { pdp: publicProduct, catalog: { items: [publicProduct] }, units: serverUnits };

test('public variant fixture has the exact base variant keys', () => {
  assert.deepEqual(Object.keys(red).sort(), [...variantKeys].sort());
  assert.deepEqual(noUnexpectedVariantKeys, {});
});

test('serialized public catalog and PDP fixtures exclude populated unit rows', () => {
  // The verification harness runs the full §7.2 live-DB gate against a real
  // server and scratch DB. This assertion covers only the local fixture JSON.
  assert.equal(fixture.units[0].variant_id, fixture.pdp.variants[0].id);
  assert.match(fixture.units[0].code, unitPattern);
  for (const publicPayload of [fixture.pdp, fixture.catalog]) {
    const serialized = JSON.stringify(publicPayload);
    assert.doesNotMatch(serialized, unitPattern);
    assert.doesNotMatch(serialized, /"(?:code|units|unit_codes|unitCodes|spncx)"\s*:/);
  }
});

test('on-screen packing codes have a print CSS exclusion', () => {
  // The DOM check belongs with the actual on-screen codes added to the packing tab.
  const css = readFileSync(new URL('../index.css', import.meta.url), 'utf8');
  const print = css.match(/@media\s+print\s*\{((?:[^{}]|\{(?:[^{}]|\{[^{}]*\})*\})*)\}/)?.[1];
  assert.match(print ?? '', /\.spncx-pack-unit-code\s*\{\s*display:\s*none\s*!important\s*;/);
});

const orderLine = {
  product: { id: publicProduct.id, title: publicProduct.title },
  quantity: 1, code: serverUnits[0].code, unit_codes: [serverUnits[0].code], unitCodes: [serverUnits[0].code],
  units: serverUnits,
};
const order = {
  id: 'fixture-order', orderNumber: 'ORDER-FIXTURE', items: [orderLine],
  totalAmount: 100, discountAmount: 0, payableAmount: 100, paymentMethod: 'cash',
  customerInfo: { fullName: 'نمونه', phone: '', province: 'تهران', city: 'تهران', address: '', postalCode: '' },
  status: 'shipped', trackingCode: 'TRACKING-FIXTURE', createdAt: '2026-11-15',
  storeId: 'fixture-central', storeName: 'فروشگاه نمونه', dropshipperProfit: 0, platformProfit: 0,
} satisfies Omit<Order, 'items'> & { items: (typeof orderLine)[] };

test('real customer components and PDP variant selection keep unit data out of the DOM', async t => {
  const contextModules: Record<string, string> = {
    StoreContext: `const noop = () => {};
      export const useStoreManager = noop;
      export const useActiveStoreCategorySlugs = noop;
      export const expandCategorySlugsWithAncestors = noop;
      export const activeStoreSlugOf = noop;
      export const useStore = () => ({
        currentUser: { role: 'customer' }, customerUser: { isLoggedIn: true },
        currentStore: { id: 'fixture-central', slug: 'central', ownerType: 'central' },
        formatPrice: String, toPersianDigits: String, products: [], categories: [], stores: [], cart: [],
        cartItemsCount: 0, isWishlisted: () => false, setTrackingOrder: noop, setIsTrackingModalOpen: noop,
      });`,
    OrderContext: `export const useOrders = () => ({ orders: [${JSON.stringify(order)}], fetchMyOrders: async () => {} });`,
    AuthContext: `export const adminRolesForCapabilities = () => [];
      export const panelsForCapabilities = () => ['customer'];
      export const useAuth = () => ({ currentUser: { role: 'customer' }, isSessionReady: true });`,
  };
  const bundle = await build({
    write: false, bundle: true, platform: 'browser', format: 'iife', jsx: 'automatic',
    loader: { '.css': 'empty' },
    define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env': '{}' },
    plugins: [{ name: 'customer-session-fixture', setup(bundler) {
      bundler.onResolve({ filter: /(?:^|\/)(StoreContext|OrderContext|AuthContext)$/ }, args => ({
        path: args.path.split('/').at(-1)!, namespace: 'customer-session-fixture',
      }));
      bundler.onLoad({ filter: /.*/, namespace: 'customer-session-fixture' }, args => ({
        contents: contextModules[args.path], loader: 'js',
      }));
    } }],
    stdin: { loader: 'tsx', resolveDir: process.cwd(), contents: `
      import { createRoot } from 'react-dom/client';
      import { OrderDetailModal } from './src/components/common/OrderDetailModal';
      import { CustomerOrdersView } from './src/components/views/CustomerOrdersView';
      import { ProductDetailView } from './src/components/views/ProductDetailView';
      import { ToastProvider } from './src/context/ToastContext';
      import { mapBackendProduct } from './src/context/ProductContext';
      const order = ${JSON.stringify(order)};
      const product = mapBackendProduct(${JSON.stringify(fixture.pdp)});
      createRoot(document.querySelector('#root')).render(<ToastProvider>
        {location.pathname.startsWith('/product/')
          ? <ProductDetailView product={product} onBack={() => {}} />
          : <><section id="detail"><OrderDetailModal order={order} isOpen onClose={() => {}} /></section>
              <section id="tracking"><CustomerOrdersView onBack={() => {}} /></section></>}
      </ToastProvider>);
    ` },
  });
  const browser = await puppeteer.launch({ executablePath: process.env.CHROME_PATH, pipe: true, args: ['--no-sandbox', '--disable-setuid-sandbox', '--disable-dev-shm-usage'] });
  try {
    const origin = 'http://unit-leak.test';
    const page = await browser.newPage();
    const unitRequests: string[] = [];
    page.setDefaultTimeout(10000);
    page.on('pageerror', error => t.diagnostic(String(error)));
    await page.setRequestInterception(true);
    page.on('request', request => {
      const url = new URL(request.url());
      if (url.origin !== origin || request.method() !== 'GET') {
        void request.abort();
      } else if (request.isNavigationRequest()) {
        void request.respond({ contentType: 'text/html; charset=utf-8', body: '<main id="root"></main>' });
      } else if (url.pathname.startsWith('/api/v1/products/')) {
        void request.respond({ contentType: 'application/json', body: JSON.stringify(fixture.pdp) });
      } else if (url.pathname === '/api/v1/returns') {
        void request.respond({ contentType: 'application/json', body: '[]' });
      } else {
        if (/\/units(?:\/|$)/.test(url.pathname)) unitRequests.push(url.pathname);
        void request.abort();
      }
    });

    async function visit(path: string) {
      await page.goto(origin + path);
      await page.addScriptTag({ content: bundle.outputFiles[0].text });
    }

    await visit('/customer-orders');
    await page.waitForFunction(() => document.querySelector('#tracking')?.textContent?.includes('TRACKING-FIXTURE'));
    await t.test('shared customer order modal excludes raw order line codes', async () => {
      const detail = await page.$eval('#detail', node => node.innerHTML);
      assert.match(detail, /اقلام سفارش/);
      assert.match(detail, /کالای نمونه سریال‌دار/);
      assert.doesNotMatch(detail, unitPattern);
    });
    await t.test('customer tracking displays shipment tracking without unit codes', async () => {
      const tracking = await page.$eval('#tracking', node => node.innerHTML);
      assert.match(tracking, /TRACKING-FIXTURE/);
      assert.match(tracking, /کالای نمونه سریال‌دار/);
      assert.doesNotMatch(tracking, unitPattern);
    });
    await t.test('PDP treats a unit code like an unknown variant parameter', async () => {
      const selectedVariant = async (spnc: string, expected: string) => {
        await visit('/product/spn-3724980?spnc=' + encodeURIComponent(spnc));
        await page.waitForFunction(text =>
          !!document.querySelector('[data-testid="variant-selector"]')
            && (document.querySelector('[data-testid="variant-selector"] button[aria-pressed="true"]')?.textContent?.trim() ?? null) === text,
        {}, expected);
        const html = await page.$eval('#root', node => node.innerHTML);
        assert.match(html, /کالای نمونه سریال‌دار/);
        assert.doesNotMatch(html, unitPattern);
      };
      await selectedVariant(red.spnc, red.name);
      await selectedVariant('unknown-variant', blue.name);
      await selectedVariant(serverUnits[0].code, blue.name);
      assert.deepEqual(unitRequests, []);
    });
  } finally {
    await browser.close();
  }
});
