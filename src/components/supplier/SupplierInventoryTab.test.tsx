import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { it } from 'node:test';
import assert from 'node:assert/strict';
import {
  SupplierInventoryView,
  LOW_STOCK_THRESHOLD,
  STOCK_STATUS_LABEL,
  stockStatus,
  sellableOf,
  variantSellableRows,
  filterInventoryItems,
  countByStockStatus,
  formatNumber,
  parseDigits,
  variantPrefillStock,
  formatReturnDate,
  saveStockEdit,
  validateReturnRequest,
} from './SupplierInventoryTab';
import type { SupplierInventoryViewProps } from './SupplierInventoryTab';
import type { SupplierProductDto, SupplierInventoryLocationDto, SupplierReturnRequestDto } from '../../services/supplierApi';

// First coverage for the tab. The harness is tsx --test +
// renderToStaticMarkup (no jsdom), and the container's useToast /
// useSupplierResource hooks throw outside providers — so rendering tests
// target the presentational SupplierInventoryView with fixture data, and
// decision/handler tests target the exported pure helpers. What they
// cannot cover — the container wiring effects (variant rows fetching on
// edit-open, the pre-fill effect firing when rows land, reloads after
// save/return) — needs a DOM + act(), which this harness cannot run.

const product = (overrides: Partial<SupplierProductDto> = {}): SupplierProductDto => ({
  id: 'p1',
  title: 'تیشرت زنانه',
  slug: 'women-tee',
  categorySlug: 'women-tee-shirts',
  supplierId: 's1',
  originalPrice: 250000,
  supplierBaseCost: 150000,
  platformMargin: 20,
  isOriginal: true,
  stock: 12,
  imageUrl: 'https://example.com/t.jpg',
  description: '',
  warehouseName: '',
  preparationDays: 1,
  supplierWarranty: '',
  supplyOption: '1day',
  schemaVersion: 1,
  ...overrides,
} as SupplierProductDto);

const location = (overrides: Partial<SupplierInventoryLocationDto> = {}): SupplierInventoryLocationDto => ({
  productId: 'p1',
  productTitle: 'تیشرت زنانه',
  ownerType: 'supplier',
  locationType: 'central',
  locationKey: 'central-1',
  status: 'sellable',
  quantity: 0,
  reconciled: true,
  ...overrides,
} as SupplierInventoryLocationDto);

const healthy = product();
const outOfStock = product({ id: 'p2', title: 'شلوار مردانه', stock: 0, imageUrl: '' });
const low = product({ id: 'p3', title: 'کلاه', stock: 3, imageUrl: '' });
const variable = product({ id: 'p4', title: 'پیراهن', schemaVersion: 2, stock: 0 });

const baseLocations = [
  location({ productId: 'p1', quantity: 12 }),
  location({ productId: 'p1', locationKey: 'central-2', status: 'sales_reserved', quantity: 2 }),
  location({ productId: 'p2', quantity: 0 }),
  location({ productId: 'p3', quantity: 3 }),
  location({ productId: 'p4', variantId: 'v-blue', variantName: 'آبی / بزرگ', variantSpnc: 'SPNC-A', quantity: 4 }),
  location({ productId: 'p4', variantId: 'v-red', variantName: 'قرمز / کوچک', variantSpnc: 'SPNC-B', quantity: 0 }),
  location({ productId: 'p4', variantId: 'v-red', locationKey: 'central-2', status: 'quarantine', quantity: 1 }),
];

const renderView = (props: Partial<SupplierInventoryViewProps> = {}) =>
  renderToStaticMarkup(
    <SupplierInventoryView
      items={[healthy, outOfStock, low, variable]}
      totalCount={4}
      locations={baseLocations}
      returns={[]}
      productsById={{ p1: healthy, p2: outOfStock, p3: low, p4: variable }}
      shareData={null}
      shareLoaded={false}
      loading={false}
      error=""
      retry={() => {}}
      query=""
      onQueryChange={() => {}}
      statusFilter="all"
      onStatusFilterChange={() => {}}
      statusCounts={{ all: 4, in: 1, low: 2, out: 1 }}
      editing={null}
      editVariants={{ rows: [], loading: false, error: '', reload: () => {} }}
      saving={false}
      onEditVariantChange={() => {}}
      onEditStockChange={() => {}}
      onSaveEdit={() => {}}
      onCancelEdit={() => {}}
      onOpenEdit={() => {}}
      onOpenReturn={() => {}}
      returning={null}
      returnVariants={{ rows: [], loading: false, error: '', reload: () => {} }}
      returnQuantity=""
      returnVariantId=""
      returnMethod="supplier_collect"
      returnReason=""
      returnSaving={false}
      onReturnVariantChange={() => {}}
      onReturnQuantityChange={() => {}}
      onReturnMethodChange={() => {}}
      onReturnReasonChange={() => {}}
      onSubmitReturn={() => {}}
      onCancelReturn={() => {}}
      returnableOf={() => 0}
      {...props}
    />,
  );

it('derives the status pill from the sellable sum: 0 is out, 1-5 is low, 6+ is in', () => {
  assert.equal(LOW_STOCK_THRESHOLD, 5);
  assert.equal(stockStatus(0), 'out');
  assert.equal(stockStatus(1), 'low');
  assert.equal(stockStatus(5), 'low');
  assert.equal(stockStatus(6), 'in');
  assert.equal(STOCK_STATUS_LABEL.out, 'ناموجود');
  assert.equal(STOCK_STATUS_LABEL.low, 'کم');
  assert.equal(STOCK_STATUS_LABEL.in, 'موجود');
});

it('sums only sellable locations for the hero figure', () => {
  assert.equal(sellableOf(baseLocations, 'p1'), 12);
  assert.equal(sellableOf(baseLocations, 'p2'), 0);
  assert.equal(sellableOf(baseLocations, 'p4'), 4);
});

it('groups per-variant sellable rows from locations without a fetch', () => {
  const rows = variantSellableRows(baseLocations, 'p4');
  assert.equal(rows.length, 2);
  assert.deepEqual(rows.map(row => [row.variantId, row.label, row.sellable]), [
    ['v-blue', 'آبی / بزرگ · SPNC-A', 4],
    ['v-red', 'قرمز / کوچک · SPNC-B', 0],
  ]);
  assert.deepEqual(variantSellableRows(baseLocations, 'p1'), []);
});

it('search matches title, product id, and variant labels from locations', () => {
  const all = [healthy, outOfStock, low, variable];
  assert.deepEqual(filterInventoryItems(all, '', 'all', baseLocations).map(item => item.id), ['p1', 'p2', 'p3', 'p4']);
  assert.deepEqual(filterInventoryItems(all, 'تیشرت', 'all', baseLocations).map(item => item.id), ['p1']);
  assert.deepEqual(filterInventoryItems(all, 'p2', 'all', baseLocations).map(item => item.id), ['p2']);
  assert.deepEqual(filterInventoryItems(all, 'آبی', 'all', baseLocations).map(item => item.id), ['p4']);
  assert.deepEqual(filterInventoryItems(all, 'SPNC-B', 'all', baseLocations).map(item => item.id), ['p4']);
  assert.deepEqual(filterInventoryItems(all, 'چیزی که نیست', 'all', baseLocations), []);
});

it('status filter slices by the sellable-derived status', () => {
  const all = [healthy, outOfStock, low, variable];
  assert.deepEqual(filterInventoryItems(all, '', 'out', baseLocations).map(item => item.id), ['p2']);
  assert.deepEqual(filterInventoryItems(all, '', 'low', baseLocations).map(item => item.id), ['p3', 'p4']);
  assert.deepEqual(filterInventoryItems(all, '', 'in', baseLocations).map(item => item.id), ['p1']);
  assert.deepEqual(countByStockStatus(all, baseLocations), { all: 4, in: 1, low: 2, out: 1 });
});

it('renders a ناموجود pill with a red zero and a موجود pill with the sellable total', () => {
  const html = renderView();
  assert.match(html, /ناموجود/);
  assert.match(html, /bg-rose-50 text-rose-700/);
  assert.match(html, /موجود/);
  assert.match(html, /bg-emerald-50 text-emerald-700/);
  assert.match(html, /کم/);
  // Hero figures in large Persian digits.
  assert.match(html, />۱۲</);
  assert.match(html, />۰</);
  // Card context the old flat card lacked: thumbnail, SKU, price.
  assert.match(html, /src="https:\/\/example\.com\/t\.jpg"/);
  assert.match(html, /کد کالا: p1/);
  assert.match(html, /قیمت: ۲۵۰٬۰۰۰ تومان/);
});

it('renders the status filter chips with counts and the showing X of Y line', () => {
  const html = renderView();
  assert.match(html, /همه \(۴\)/);
  assert.match(html, /موجود \(۱\)/);
  assert.match(html, /کم \(۲\)/);
  assert.match(html, /ناموجود \(۱\)/);
  assert.match(html, /نمایش ۴ از ۴ کالا/);
  assert.match(html, /aria-pressed="true"/);
});

it('renders per-variant stock rows on the card without opening the edit form', () => {
  const html = renderView();
  assert.match(html, /آبی \/ بزرگ · SPNC-A/);
  assert.match(html, /قرمز \/ کوچک · SPNC-B/);
  assert.match(html, /آماده فروش: <b[^>]*>۴<\/b>/);
  assert.match(html, /aria-label="تغییر موجودی آبی \/ بزرگ · SPNC-A"/);
  assert.doesNotMatch(html, /ذخیره موجودی/);
  assert.doesNotMatch(html, /موجودی جدید/);
});

it('renders the six-bucket breakdown collapsed, with sellable emphasised', () => {
  const html = renderView();
  assert.match(html, /جزئیات وضعیت انبار/);
  assert.match(html, /کنار گذاشته برای سفارش/);
  assert.match(html, /در حال بررسی کیفیت/);
  assert.match(html, /هنوز شمرده نشده/);
  assert.match(html, /text-emerald-700">آماده فروش/);
});

it('edit form opens pre-filled with the selected variant stock', () => {
  assert.equal(variantPrefillStock([
    { id: 'v-blue', variant_name: 'آبی / بزرگ', stock: 7, spnc: 'SPNC-A' },
    { id: 'v-red', variant_name: 'قرمز / کوچک', stock: 0, spnc: 'SPNC-B' },
  ], 'v-blue'), '7');
  assert.equal(variantPrefillStock([{ id: 'v-red', variant_name: 'قرمز / کوچک', stock: 0, spnc: 'SPNC-B' }], 'v-blue'), '');
  const html = renderView({
    editing: { product: variable, variantId: 'v-blue', stock: '7' },
    editVariants: {
      rows: [{ id: 'v-blue', variant_name: 'آبی / بزرگ', stock: 7, spnc: 'SPNC-A' }],
      loading: false, error: '', reload: () => {},
    },
  });
  assert.match(html, /تغییر موجودی: پیراهن/);
  assert.match(html, /value="۷"/);
  assert.match(html, /ذخیره موجودی/);
});

it('keeps Persian-digit formatting for stock inputs', () => {
  assert.equal(formatNumber(''), '');
  assert.equal(formatNumber('100000'), '۱۰۰٬۰۰۰');
  assert.equal(formatNumber('7'), '۷');
  assert.equal(parseDigits('۱۰۰٬۰۰۰'), '100000');
  assert.equal(parseDigits('12a3'), '123');
  assert.equal(parseDigits(''), '');
});

it('save calls updateStock with the same payload shape as before', async () => {
  const calls: Array<[string, number, unknown, unknown]> = [];
  const api = { updateStock: async (id: string, value: number, reason: unknown, variantId: unknown) => { calls.push([id, value, reason, variantId]); } };
  await saveStockEdit(api as never, variable, '7', 'v-blue', [{ id: 'v-blue', variant_name: 'آبی', stock: 4, spnc: 'SPNC-A' }]);
  assert.deepEqual(calls, [['p4', 7, undefined, 'v-blue']]);
  await saveStockEdit(api as never, healthy, '3', '', []);
  assert.deepEqual(calls[1], ['p1', 3, undefined, undefined]);
});

it('save still enforces the variant-required validation and non-negative integers', async () => {
  const api = { updateStock: async () => {} };
  await assert.rejects(saveStockEdit(api as never, variable, '7', '', []), /تنوع دقیق کالا را انتخاب کنید\./);
  await assert.rejects(saveStockEdit(api as never, variable, '7', 'v-nope', [{ id: 'v-blue', variant_name: 'آبی', stock: 4, spnc: 'SPNC-A' }]), /تنوع دقیق کالا را انتخاب کنید\./);
  await assert.rejects(saveStockEdit(api as never, healthy, '', '', []), /موجودی را با عدد وارد کن/);
  await assert.rejects(saveStockEdit(api as never, healthy, '-1', '', []), /موجودی را با عدد وارد کن/);
  await assert.rejects(saveStockEdit(api as never, healthy, '1.5', '', []), /موجودی را با عدد وارد کن/);
});

it('return validation keeps the variant, max-quantity and reason guards', () => {
  const valid = { requiresSKU: true, variantId: 'v-blue', variantIds: ['v-blue'], quantity: 2, max: 4, reason: 'خراب است' };
  assert.equal(validateReturnRequest(valid), null);
  assert.match(validateReturnRequest({ ...valid, variantId: '' }) || '', /تنوع دقیق کالای برگشتی را انتخاب کنید\./);
  assert.match(validateReturnRequest({ ...valid, quantity: 5 }) || '', /تعداد را درست وارد کن/);
  assert.match(validateReturnRequest({ ...valid, quantity: 0 }) || '', /تعداد را درست وارد کن/);
  assert.match(validateReturnRequest({ ...valid, reason: '  ' }) || '', /تعداد را درست وارد کن/);
  assert.equal(validateReturnRequest({ requiresSKU: false, variantId: '', variantIds: [], quantity: 1, max: 12, reason: 'اضافی است' }), null);
});

it('returns section renders above the list with product titles, dates and card links', () => {
  const returns: SupplierReturnRequestDto[] = [{
    id: 'r1',
    productId: 'p1',
    productTitle: 'تیشرت زنانه',
    warehouseName: 'مرکزی',
    requestedQuantity: 2,
    eligibleQuantity: 2,
    collectionMethod: 'supplier_collect',
    postalCharge: 0,
    status: 'requested',
    reason: 'خراب است',
    createdAt: '2026-11-13T10:00:00Z',
  } as SupplierReturnRequestDto];
  const html = renderView({ returns });
  assert.match(html, /برگرداندن‌هایی که خواسته‌ای/);
  assert.match(html, /تیشرت زنانه/);
  assert.match(html, /۲ عدد · ثبت شده/);
  assert.match(html, /href="#inventory-p1"/);
  assert.match(html, /مشاهده کالا/);
  // The summary sits above the first product card.
  assert.ok(html.indexOf('برگرداندن‌هایی که خواسته‌ای') < html.indexOf('id="inventory-p1"'));
  // Date renders in Persian digits when ICU is available, ISO otherwise.
  assert.match(formatReturnDate('2026-11-13T10:00:00Z'), /۱۴۰۵|2026/);
  assert.equal(formatReturnDate(''), '');
  assert.equal(formatReturnDate('not-a-date'), '');
});

it('return modal lists per-variant returnable stock before a variant is chosen', () => {
  const html = renderView({
    returning: variable,
    returnVariants: {
      rows: [{ id: 'v-blue', variant_name: 'آبی / بزرگ', stock: 4, spnc: 'SPNC-A' }],
      loading: false, error: '', reload: () => {},
    },
    returnQuantity: '1',
    returnVariantId: '',
    returnReason: '',
    returnableOf: (productId, variantId) => (variantId === 'v-blue' ? 4 : 0),
  });
  assert.match(html, /برگرداندن کالا: پیراهن/);
  assert.match(html, /آبی \/ بزرگ · SPNC-A/);
  assert.match(html, /آماده برگشت: <b[^>]*>۴<\/b>/);
  assert.match(html, /آماده برگشت: ۰ عدد/);
});

it('renders the stock-share box only when loaded, with the fast-shipping accent', () => {
  const html = renderView({ shareLoaded: true, shareData: { centralStockShare: 0.4, centralUnits: 4, supplierUnits: 6 } });
  assert.match(html, /سهم انبار مرکزی/);
  assert.match(html, /ارسال سریع/);
  assert.match(html, /۴۰٪/);
  assert.doesNotMatch(renderView({ shareLoaded: false, shareData: { centralStockShare: 0.4, centralUnits: 4, supplierUnits: 6 } }), /سهم انبار مرکزی/);
});

it('empty states distinguish search misses from a status filter with no matches', () => {
  assert.match(renderView({ items: [], totalCount: 0, query: 'چیزی که نیست', statusCounts: { all: 0, in: 0, low: 0, out: 0 } }), /کالایی با این اسم پیدا نشد/);
  assert.match(renderView({ items: [], totalCount: 4, statusFilter: 'out', statusCounts: { all: 4, in: 1, low: 2, out: 1 } }), /کالایی با این وضعیت نیست/);
  assert.match(renderView({ items: [], totalCount: 0, statusCounts: { all: 0, in: 0, low: 0, out: 0 } }), /هنوز کالای تأییدشده‌ای نداری/);
});
