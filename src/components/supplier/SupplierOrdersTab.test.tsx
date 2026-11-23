import { it } from 'node:test';
import assert from 'node:assert/strict';
import { directOrderChip, isReadyOrder, isTerminalOrderStatus } from './SupplierOrdersTab';

it('shows rose chips for cancelled and failed orders', () => {
  const roseClasses = 'bg-rose-50 text-rose-700 border-rose-200';
  assert.equal(directOrderChip('cancelled').text, 'لغو شده');
  assert.equal(directOrderChip('cancelled').cls, roseClasses);
  assert.equal(directOrderChip('failed').text, 'ناموفق');
  assert.equal(directOrderChip('failed').cls, roseClasses);
});

it('labels pending, processing, shipped, and delivered orders', () => {
  assert.equal(directOrderChip('pending').text, 'منتظر ارسال');
  assert.equal(directOrderChip('pending').cls, 'bg-amber-50 text-amber-700 border-amber-200');
  assert.equal(directOrderChip('processing').text, 'در حال آماده‌سازی');
  assert.equal(directOrderChip('shipped').text, 'ارسال شده');
  assert.equal(directOrderChip('in_transit').text, 'ارسال شده');
  assert.equal(directOrderChip('delivered').text, 'به دست مشتری رسیده');
  assert.equal(directOrderChip('delivered_confirmed').text, 'به دست مشتری رسیده');
});

it('shows a neutral chip for unknown statuses', () => {
  const { text, cls } = directOrderChip('bogus_status');
  assert.equal(text, 'وضعیت نامشخص');
  assert.equal(cls, 'bg-slate-50 text-slate-600 border-slate-200');
});

it('includes only pending and processing orders in the ready queue', () => {
  for (const status of ['pending', 'processing']) {
    assert.equal(isReadyOrder(status), true, status);
  }
  for (const status of ['cancelled', 'failed', 'delivered_confirmed', 'shipped', 'delivered', 'in_transit', 'bogus_status']) {
    assert.equal(isReadyOrder(status), false, status);
  }
});

it('recognizes only cancelled and failed orders as terminal', () => {
  for (const status of ['cancelled', 'failed']) {
    assert.equal(isTerminalOrderStatus(status), true, status);
  }
  for (const status of ['pending', 'processing', 'shipped', 'in_transit', 'delivered', 'delivered_confirmed', 'returned', 'refunded', 'delivery_disputed', 'bogus_status']) {
    assert.equal(isTerminalOrderStatus(status), false, status);
  }
});
