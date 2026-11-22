import { test } from 'node:test';
import assert from 'node:assert/strict';
import { canDeleteDiscountCode } from './SellerDiscountTab';
import type { DiscountCode } from '../../services/storeApi';

// G8: the «حذف» button shows only on a 0-use row — deleting a used
// code would orphan the orders that redeemed it. The tab itself
// cannot render in this harness (useStore throws without the provider
// tree, and the rows only appear after the load effect, which
// renderToStaticMarkup never runs), so the exported rule the button
// hinges on is pinned directly — the SellerDomainsPanel precedent.
const code = (usedCount: number): DiscountCode => ({
  id: 'c1', storeId: 's1', code: 'TEST10', mode: 'percent', value: 10,
  startsAt: '', endsAt: '', isActive: true, usedCount, createdAt: '',
});

test('a 0-use code can be deleted; a used code cannot', () => {
  assert.equal(canDeleteDiscountCode(code(0)), true);
  assert.equal(canDeleteDiscountCode(code(1)), false);
  assert.equal(canDeleteDiscountCode(code(5)), false);
});
