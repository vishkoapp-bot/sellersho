import { test } from 'node:test';
import assert from 'node:assert/strict';
import { CATEGORY_TO_OFFICIAL_NICHES_MAP, getProductNicheIds } from './niches';

// G10: the gold bracelet (prod-1793551155965, «دستبند طلا ۱۸ عیار»,
// category 'gold-silver', nicheIds []) derived the shared fallback
// niche-smartwatch because 'gold-silver' was missing from the map —
// so it appeared in BOTH catalog-tab sections. It must derive
// niche-gold and land in §2.3 only.
test('gold-silver category derives niche-gold', () => {
  assert.deepEqual(CATEGORY_TO_OFFICIAL_NICHES_MAP['gold-silver'], ['niche-gold']);
  assert.deepEqual(getProductNicheIds({ category: 'gold-silver', nicheIds: [] }), ['niche-gold']);
  assert.deepEqual(getProductNicheIds({ category: 'gold-silver' }), ['niche-gold']);
});

// The derivation §1 and §2.3 of SellerCatalogTab share: explicit
// nicheIds win, empty nicheIds fall through to the category map.
test('explicit nicheIds win over the category map; empty nicheIds fall through', () => {
  assert.deepEqual(getProductNicheIds({ category: 'gold-silver', nicheIds: ['niche-leather'] }), ['niche-leather']);
  assert.deepEqual(getProductNicheIds({ category: 'digital', nicheIds: [] }), ['niche-smartwatch', 'niche-powerbank', 'niche-audio']);
});
