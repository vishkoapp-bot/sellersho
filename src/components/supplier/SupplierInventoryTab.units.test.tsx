import assert from 'node:assert/strict';
import test from 'node:test';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { ApiError } from '../../services/apiClient';
import type { SupplierProductDto } from '../../services/supplierApi';
import type { UnitLabelRow } from '../../services/unitsApi';
import { loadLotUnits, LotUnitsDrillDown, RetiredPrintedCodesPanel, retiredPrintedCodesOf, saveStockEdit, stockAdjustmentErrorMessage, SupplierInventoryView } from './SupplierInventoryTab';

const unit: UnitLabelRow = {
  code: 'UNIT-01', spnc: 'V1', variant_id: 'v1', variant_name: 'آبی', product_id: 'p1', spn: 'P1',
  title: 'نمونه', status: 'in_stock', printed_at: null, print_count: 0, last_printed_at: null,
};
const page = { units: [unit, { ...unit, code: 'UNIT-02', status: 'reserved' as const, print_count: 2 }], next_cursor: 'page-2' };
const noop = () => {};

test('unallocated and unidentified lot rows have distinct states', () => {
  assert.match(renderToStaticMarkup(<LotUnitsDrillDown allocation="unallocated" balanceId="b1" />), /بدون واحد سریال‌دار/);
  assert.match(renderToStaticMarkup(<LotUnitsDrillDown allocation="sku" />), />—</);
});

test('inventory location rows expose a drill-down only for an allocated balance ID', () => {
  const product = { id: 'p1', title: 'نمونه', stock: 15, schemaVersion: 2 } as SupplierProductDto;
  const base = {
    productId: 'p1', productTitle: 'نمونه', ownerType: 'supplier', locationType: 'central',
    status: 'sellable', quantity: 5, reconciled: true,
  };
  const variants = { rows: [], loading: false, error: '', reload: noop };
  const html = renderToStaticMarkup(<SupplierInventoryView
    items={[product]} totalCount={1} productsById={{ p1: product }}
    locations={[
      { ...base, locationKey: 'with-balance', allocation: 'sku', balanceId: 'b1' },
      { ...base, locationKey: 'without-balance', allocation: 'sku' },
      { ...base, locationKey: 'unallocated', allocation: 'unallocated', balanceId: 'b2' },
    ]}
    returns={[]} shareData={null} shareLoaded={false} loading={false} error="" retry={noop}
    query="" onQueryChange={noop} statusFilter="all" onStatusFilterChange={noop}
    statusCounts={{ all: 1, in: 1, low: 0, out: 0 }}
    editing={null} editVariants={variants} saving={false}
    onEditVariantChange={noop} onEditStockChange={noop} onEditReasonChange={noop}
    onSaveEdit={noop} onCancelEdit={noop} onOpenEdit={noop} onOpenReturn={noop}
    returning={null} returnVariants={variants} returnQuantity="" returnVariantId=""
    returnMethod="supplier_collect" returnReason="" returnSaving={false}
    onReturnVariantChange={noop} onReturnQuantityChange={noop} onReturnMethodChange={noop}
    onReturnReasonChange={noop} onSubmitReturn={noop} onCancelReturn={noop} returnableOf={() => 0}
  />);
  assert.match(html, /with-balance[^<]*۵ عدد[^<]*<\/p><div[^>]*><button[^>]*>— واحد/);
  assert.match(html, /without-balance[^<]*۵ عدد[^<]*<\/p><span>—<\/span>/);
  assert.match(html, /unallocated[^<]*۵ عدد[^<]*<\/p><span>بدون واحد سریال‌دار<\/span>/);
});

test('an expanded lot shows its unit codes, statuses, print states and pagination', () => {
  const html = renderToStaticMarkup(<LotUnitsDrillDown balanceId="b1" page={page} expanded onLoadMore={noop} />);
  for (const copy of ['۲+ واحد', 'UNIT-01', 'UNIT-02', 'موجود در انبار', 'رزرو شده', 'هنوز چاپ نشده', '۲ بار چاپ شده', 'نمایش بیشتر']) {
    assert.ok(html.includes(copy), copy);
  }
  assert.match(html, /dir="ltr" class="font-mono[^"]*">UNIT-01/);
});

test('lot pagination keeps the balance filter and sends the cursor', async () => {
  const result = await loadLotUnits(async params => {
    assert.deepEqual(params, { balance_id: 'b1', limit: 200, cursor: 'page-2' });
    return page;
  }, 'b1', 'page-2');
  assert.deepEqual(result, page);
});

test('retired printed codes are read only from the flat adjust envelope', () => {
  const response = { retired_printed_codes: ['UNIT-01'] };
  assert.deepEqual(retiredPrintedCodesOf(response), ['UNIT-01']);
  assert.deepEqual(retiredPrintedCodesOf({ data: response }), []);
  assert.deepEqual(retiredPrintedCodesOf({ retired_printed_codes: 'UNIT-01' }), []);
});

test('the warning exposes codes, copy and acknowledgement', () => {
  const html = renderToStaticMarkup(<RetiredPrintedCodesPanel codes={['UNIT-01', 'UNIT-02']} onAcknowledge={noop} />);
  assert.match(html, /دیگر معتبر نیستند/);
  assert.match(html, /dir="ltr" class="font-mono[^"]*">UNIT-01/);
  assert.match(html, /UNIT-02/);
  assert.match(html, /کپی همه کدها/);
  assert.match(html, /متوجه شدم/);
});

test('saveStockEdit returns the flat adjust response', async () => {
  const product = { id: 'p1', schemaVersion: 1 } as SupplierProductDto;
  const response = { product_id: 'p1', variant_id: null, previous_stock: 3, stock: 2, delta: -1, retired_codes: ['UNIT-01'], retired_printed_codes: ['UNIT-01'], coverage_gap: null };
  const api = { updateStock: async (...args: unknown[]) => {
    assert.deepEqual(args, ['p1', 2, 'recount', undefined]);
    return response;
  } };
  assert.deepEqual(await saveStockEdit(api as never, product, '2', '', [], ' recount '), response);
});

test('stock validation rejects invalid totals, SKUs and reasons before the API call', async () => {
  const product = { id: 'p1', schemaVersion: 2 } as SupplierProductDto;
  const api = { updateStock: async () => assert.fail('invalid edit reached the API') };
  await assert.rejects(saveStockEdit(api as never, product, '2147483648', '', [], 'recount'), /موجودی را با عدد وارد کن/);
  await assert.rejects(saveStockEdit(api as never, product, '2', 'missing', [], 'recount'), /تنوع دقیق کالا را انتخاب کنید/);
  const variant = { id: 'v1', variant_name: 'آبی', spnc: 'V1', stock: 3 };
  await assert.rejects(saveStockEdit(api as never, product, '2', 'v1', [variant], ' '), /علت اصلاح موجودی/);
});

test('the stock conflict message includes the verbatim held-stock code and a Persian gloss', () => {
  const message = stockAdjustmentErrorMessage(new ApiError(409, 'خطایی رخ داد؛ دوباره تلاش کنید.', { error: 'stock_below_held' }), 'ذخیره انجام نشد.');
  assert.match(message, /stock_below_held/);
  assert.match(message, /رزرو/);
});
