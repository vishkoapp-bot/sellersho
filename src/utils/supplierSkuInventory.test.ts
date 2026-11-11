import { it } from 'node:test';
import assert from 'node:assert/strict';
import { supplierReturnQuantity, supplierReturnAttemptScope, supplierInventoryLabel } from './supplierSkuInventory';

const locations = [
  { productId: 'p', ownerType: 'supplier', status: 'sellable', quantity: 7 },
  { productId: 'p', ownerType: 'supplier', status: 'sellable', quantity: 2, variantId: 'a' },
  { productId: 'p', ownerType: 'supplier', status: 'sellable', quantity: 3, variantId: 'b' },
  { productId: 'p', ownerType: 'platform', status: 'sellable', quantity: 30, variantId: 'a' },
  { productId: 'p', ownerType: 'supplier', status: 'quarantine', quantity: 40, variantId: 'a' },
];
it('returns only the selected SKU supplier-owned sellable balance, never a sibling or unallocated stock', () => {
  assert.equal(supplierReturnQuantity(locations, 'p', 'a', true), 2);
  assert.equal(supplierReturnQuantity(locations, 'p', 'b', true), 3);
  assert.equal(supplierReturnQuantity(locations, 'p', undefined, true), 0);
  assert.equal(supplierReturnQuantity(locations, 'p', undefined, false), 7);
});
it('keeps retries of one return stable and gives different SKUs or changed requests distinct scopes', () => {
  const a = supplierReturnAttemptScope('p', 'a', 2, 'postal', 'reason');
  assert.equal(supplierReturnAttemptScope('p', 'a', 2, 'postal', ' reason '), a);
  assert.notEqual(supplierReturnAttemptScope('p', 'b', 2, 'postal', 'reason'), a);
  assert.notEqual(supplierReturnAttemptScope('p', 'a', 1, 'postal', 'reason'), a);
});
it('labels unallocated legacy balances explicitly without fabricating a default SKU', () => {
  assert.equal(supplierInventoryLabel({}), 'موجودی قدیمی بدون تخصیص به تنوع');
  assert.equal(supplierInventoryLabel({ variantId: 'a', variantName: 'آبی / بزرگ', variantSpnc: 'SPNC-1' }), 'آبی / بزرگ · SPNC-1');
});
