import assert from 'node:assert/strict';
import { test } from 'node:test';
import { parsePathname, pathForView } from './routing';

test('panel routes resolve to their guarded views and preserve seller tab deep links', () => {
  const overview = { view: 'seller_dashboard', sellerTab: 'overview' };
  assert.deepEqual(parsePathname('/seller'), overview);
  assert.deepEqual(parsePathname('/seller/'), overview);
  assert.deepEqual(parsePathname('/supplier'), { view: 'supplier_dashboard' });
  assert.equal(pathForView('seller_dashboard'), '/seller');
  assert.equal(pathForView('seller_dashboard', { sellerTab: 'overview' }), '/seller');
  assert.equal(pathForView('supplier_dashboard'), '/supplier');
  const orders = pathForView('seller_dashboard', { sellerTab: 'orders' });
  assert.equal(orders, '/?panel=seller&tab=orders');
  assert.deepEqual(parsePathname('/', orders.slice(1)), { view: 'seller_dashboard', sellerTab: 'orders' });
  assert.deepEqual(parsePathname('/admin'), { view: 'admin_panel' });
  assert.deepEqual(parsePathname('/unknown'), { view: 'not_found' });
  assert.deepEqual(parsePathname('/dashboard'), { view: 'not_found' });
});
