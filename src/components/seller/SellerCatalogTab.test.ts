import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  addedProductIds,
  buildCatalogCostPresence,
  catalogRowCostMissing,
  catalogVariantCost,
  removedProductIds,
  resolveScopedCatalogOutcome,
  shouldRefetchCatalogAfterConfirm,
  staleAddedIds,
  unionUndoProductIds,
} from './SellerCatalogTab';
import type { ProductDto } from '../../services/catalogApi';
import { buildProductCandidatesQuery } from '../../services/storeApi';

// C5 (Batch 4b): the tab must never render 0 as a price. The tab itself
// cannot render in this harness (useStore throws without the provider
// tree, and the scoped-fetch effect never runs under
// renderToStaticMarkup), so the exported rules the row render hinges on
// are pinned directly — the SellerDomainsPanel precedent. What they
// cannot cover — the effect wiring (fetch rejects -> error card,
// retry re-runs the scoped fetch) — needs a DOM + act() or the manual
// network-blocking check in the spec.
//
// The missing-only predicate: cost is missing iff for EVERY variant
// v.supplierBaseCost == null AND the product baseCostB == null
// (null/undefined only — NOT !cost, so a genuine 0 is a number).

const dto = (overrides: Partial<ProductDto> = {}): ProductDto => ({
  id: 'p1',
  title: 'کالای تست',
  slug: 'test',
  category: 'cat',
  originalPrice: 100000,
  price: 100000,
  platformFeeF: 0,
  isOriginal: true,
  stock: 1,
  image: '',
  description: '',
  warehouse: '',
  ...overrides,
} as ProductDto);

test('buildCatalogCostPresence: missing baseCostB and variant costs are unknown, a genuine 0 is known', () => {
  const presence = buildCatalogCostPresence([
    dto({
      id: 'p-missing',
      baseCostB: undefined,
      variants: [
        { id: 'v1', name: 'a', variantName: 'a', spnc: 's', attributes: {}, isDefault: true, stock: 1, minCartQty: 1, maxCartQty: null, supplierBaseCost: undefined },
        { id: 'v2', name: 'b', variantName: 'b', spnc: 's', attributes: {}, isDefault: false, stock: 1, minCartQty: 1, maxCartQty: null, supplierBaseCost: null as unknown as number },
      ],
    }),
    dto({ id: 'p-zero', baseCostB: 0 }),
    dto({
      id: 'p-real',
      baseCostB: 100000,
      variants: [
        { id: 'v1', name: 'a', variantName: 'a', spnc: 's', attributes: {}, isDefault: true, stock: 1, minCartQty: 1, maxCartQty: null, supplierBaseCost: 0 },
      ],
    }),
  ]);
  assert.deepEqual(presence['p-missing'], { product: false, variants: { v1: false, v2: false } });
  assert.deepEqual(presence['p-zero'], { product: true, variants: {} });
  assert.deepEqual(presence['p-real'], { product: true, variants: { v1: true } });
});

test('catalogRowCostMissing: rows lacking every cost are missing; a known cost (even 0) is not', () => {
  // (ii) rows lacking baseCostB -> missing, never «خرید: ۰».
  const missing = buildCatalogCostPresence([dto({ id: 'p1', baseCostB: undefined })])['p1'];
  assert.equal(catalogRowCostMissing({ variants: [] }, missing), true);
  // No presence entry at all (central fallback list) -> missing, never 0.
  assert.equal(catalogRowCostMissing({ variants: [] }, undefined), true);
  // (iii) baseCostB present -> not missing.
  const real = buildCatalogCostPresence([dto({ id: 'p1', baseCostB: 100000 })])['p1'];
  assert.equal(catalogRowCostMissing({ variants: [] }, real), false);
  // (iv) genuine cost 0 -> not missing; the row renders computed 0/0/0.
  const zero = buildCatalogCostPresence([dto({ id: 'p1', baseCostB: 0 })])['p1'];
  assert.equal(catalogRowCostMissing({ variants: [] }, zero), false);
  // Variant products: one known variant cost saves the row; none known -> missing.
  const mixed = buildCatalogCostPresence([
    dto({
      id: 'p1',
      baseCostB: undefined,
      variants: [
        { id: 'v1', name: 'a', variantName: 'a', spnc: 's', attributes: {}, isDefault: true, stock: 1, minCartQty: 1, maxCartQty: null, supplierBaseCost: undefined },
        { id: 'v2', name: 'b', variantName: 'b', spnc: 's', attributes: {}, isDefault: false, stock: 1, minCartQty: 1, maxCartQty: null, supplierBaseCost: 50000 },
      ],
    }),
  ])['p1'];
  assert.equal(
    catalogRowCostMissing({ variants: [{ id: 'v1' }, { id: 'v2' }] }, mixed),
    false,
  );
  const allMissing = buildCatalogCostPresence([
    dto({
      id: 'p1',
      baseCostB: undefined,
      variants: [
        { id: 'v1', name: 'a', variantName: 'a', spnc: 's', attributes: {}, isDefault: true, stock: 1, minCartQty: 1, maxCartQty: null, supplierBaseCost: undefined },
      ],
    }),
  ])['p1'];
  assert.equal(catalogRowCostMissing({ variants: [{ id: 'v1' }] }, allMissing), true);
});

test('catalogVariantCost: variant cost wins, else the product cost, else null — never 0', () => {
  assert.equal(catalogVariantCost({ supplierBaseCost: 5000 }, 7000, true), 5000);
  // Genuine variant 0 stays 0 (a number, not missing).
  assert.equal(catalogVariantCost({ supplierBaseCost: 0 }, 7000, true), 0);
  // Unknown variant falls back to the product cost...
  assert.equal(catalogVariantCost({ supplierBaseCost: undefined }, 7000, false), 7000);
  // ...and when the product cost is unknown too, the result is null, not 0.
  assert.equal(catalogVariantCost({ supplierBaseCost: undefined }, null, false), null);
  assert.equal(catalogVariantCost({ supplierBaseCost: undefined }, null, true), null);
});

test('resolveScopedCatalogOutcome: rejection, incomplete walk and empty resolve are the same failure state', () => {
  // (i) scoped fetch rejects / no result -> 'walk' (error card + retry).
  assert.equal(resolveScopedCatalogOutcome(null), 'walk');
  assert.equal(resolveScopedCatalogOutcome(undefined), 'walk');
  // Empty / non-array resolve -> the same error state, never a fallback.
  assert.equal(resolveScopedCatalogOutcome({ items: [] }), 'walk');
  assert.equal(resolveScopedCatalogOutcome({ items: 'not-an-array' }), 'walk');
  assert.equal(resolveScopedCatalogOutcome({}), 'walk');
  // Incomplete walk -> 'walk'.
  assert.equal(resolveScopedCatalogOutcome({ items: [{ id: 'p1' }], complete: false, includeInactiveApplied: true }), 'walk');
  // Carve-out not applied -> 'inactive' (re-login state).
  assert.equal(resolveScopedCatalogOutcome({ items: [{ id: 'p1' }], complete: true, includeInactiveApplied: false }), 'inactive');
  assert.equal(resolveScopedCatalogOutcome({ items: [{ id: 'p1' }], complete: true }), 'inactive');
  // Complete walk with rows -> 'ok'.
  assert.equal(resolveScopedCatalogOutcome({ items: [{ id: 'p1' }], complete: true, includeInactiveApplied: true }), 'ok');
});

test('resolveScopedCatalogOutcome: an empty + complete walk is ok (genuinely empty assortment), not the error card', () => {
  // F1 (critic): a store with a genuinely empty assortment got a
  // permanent retry error card because an empty walk read as failed.
  assert.equal(resolveScopedCatalogOutcome({ items: [], complete: true, includeInactiveApplied: true }), 'ok');
  // The other empty cases keep their old failure states: an incomplete
  // empty walk is still 'walk', and an empty walk without the inactive
  // carve-out is still 'inactive'.
  assert.equal(resolveScopedCatalogOutcome({ items: [], complete: false, includeInactiveApplied: true }), 'walk');
  assert.equal(resolveScopedCatalogOutcome({ items: [], complete: true, includeInactiveApplied: false }), 'inactive');
  assert.equal(resolveScopedCatalogOutcome({ items: [], complete: true }), 'inactive');
});

test('shouldRefetchCatalogAfterConfirm: refetch only after a resolved product PUT, never for niche-only or failed confirms', () => {
  assert.equal(shouldRefetchCatalogAfterConfirm(1, { ok: true }), true);
  assert.equal(shouldRefetchCatalogAfterConfirm(3, { ok: true }), true);
  assert.equal(shouldRefetchCatalogAfterConfirm(1, { ok: false }), false);
  assert.equal(shouldRefetchCatalogAfterConfirm(1, null), false);
  assert.equal(shouldRefetchCatalogAfterConfirm(0, { ok: true }), false);
  assert.equal(shouldRefetchCatalogAfterConfirm(0, { ok: false }), false);
});

test('addedProductIds returns only draft additions in draft order', () => {
  assert.deepEqual(addedProductIds(['new-b', 'keep', 'new-a'], ['keep', 'removed']), ['new-b', 'new-a']);
  assert.deepEqual(addedProductIds(['keep'], ['keep']), []);
});

test('removedProductIds returns only saved ids missing from the draft in saved order', () => {
  assert.deepEqual(removedProductIds(['old-b', 'keep', 'old-a'], ['keep', 'new']), ['old-b', 'old-a']);
  assert.deepEqual(removedProductIds([], ['new']), []);
});

test('unionUndoProductIds dedupes both lists and keeps fresh ids first', () => {
  assert.deepEqual(unionUndoProductIds(['fresh', 'shared', 'fresh'], ['old-b', 'shared', 'old-a', 'old-b']),
    ['fresh', 'shared', 'old-b', 'old-a']);
});

test('staleAddedIds returns the additions missing from the eligible response', () => {
  assert.deepEqual(staleAddedIds(['stale-b', 'eligible', 'stale-a'], ['eligible', 'other']), ['stale-b', 'stale-a']);
  assert.deepEqual(staleAddedIds([], ['eligible']), []);
});

test('buildProductCandidatesQuery includes only supplied options and joins ids in one value', () => {
  assert.equal(buildProductCandidatesQuery({ storeSlug: 'mone' }), 'storeSlug=mone');
  assert.equal(buildProductCandidatesQuery({ storeSlug: 'mone', ids: [], q: '' }), 'storeSlug=mone&q=');
  const query = buildProductCandidatesQuery({
    storeSlug: 'mone', q: 'هدفون & اسپیکر', cursor: 'a+b=', limit: 2, ids: ['one', 'two'],
  });
  assert.deepEqual([...new URLSearchParams(query)], [
    ['storeSlug', 'mone'], ['q', 'هدفون & اسپیکر'], ['cursor', 'a+b='], ['limit', '2'], ['ids', 'one,two'],
  ]);
});
