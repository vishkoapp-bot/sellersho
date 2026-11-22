import { test } from 'node:test';
import assert from 'node:assert/strict';
import { storefrontProducts } from './templates/templateTypes';
import type { Product, Store } from '../../types';

// B2: the footer «محصولات» modal must list the store-scoped list the
// grid already has — never the central paged catalogue. The modal
// itself cannot be rendered in this harness (it only renders after a
// click, and there is no DOM/interaction here — same honest limit as
// SellerDomainsPanel.test.tsx), so the test pins the selection rule
// the modal depends on: the store-scoped list of vishkostor's 7 ids,
// none of which are on central page 1, must yield 7 items, while the
// central page-1 list yields 0 — the exact pre-fix bug.
const sevenIds = ['vish-1', 'vish-2', 'vish-3', 'vish-4', 'vish-5', 'vish-6', 'vish-7'];
const store = {
  id: 'store-vishkostor',
  slug: 'vishkostor',
  name: 'vishkostor',
  ownerType: 'dropshipper',
  activeProductIds: sevenIds,
} as unknown as Store;

const product = (id: string): Product =>
  ({ id, title: id, price: 1000, originalPrice: 1000, discountPercent: 0, stock: 1 } as unknown as Product);

const scopedSeven = sevenIds.map(product);
const centralPage1 = Array.from({ length: 24 }, (_, i) => product(`central-${i + 1}`));

test('B2: the modal source (store-scoped list) lists all 7 assortment products', () => {
  assert.equal(storefrontProducts(store, scopedSeven).length, 7);
  assert.deepEqual(
    storefrontProducts(store, scopedSeven).map((p) => p.id),
    sevenIds,
  );
});

test('B2: the old modal source (central page 1) lists 0 of the 7 — the pre-fix bug', () => {
  assert.equal(storefrontProducts(store, centralPage1).length, 0);
});

test('B2: a genuinely empty store still lists 0 (empty state, not a regression)', () => {
  assert.equal(storefrontProducts(store, []).length, 0);
});
