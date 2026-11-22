import { test } from 'node:test';
import assert from 'node:assert/strict';
import { deriveSubscribersPage } from './SellerSubscribersTab';

// C8 (Batch 4b): the header totals must be derived from the items
// actually received when the server omits total/counts. The tab itself
// cannot render in this harness (useStore throws without the provider
// tree, and loadPage's effect never runs under renderToStaticMarkup),
// so the exported derivation the header hinges on is pinned directly
// — the SellerDomainsPanel precedent.

const row = (mobile: string, status: string) => ({ id: `s-${mobile}`, mobile, status });

test('{subscribers: [2 rows]} with no total/counts derives total 2 and counts from the items', () => {
  const out = deriveSubscribersPage({ subscribers: [row('09120000001', 'active'), row('09120000002', 'unsubscribed')] });
  assert.equal(out.total, 2);
  assert.deepEqual(out.counts, { active: 1, unsubscribed: 1 });
  assert.equal(out.items.length, 2);
  assert.equal(out.totalIsMinimum, false);
});

test('{items, total, counts} from the server wins over the derivation', () => {
  const out = deriveSubscribersPage({
    items: [row('09120000001', 'active'), row('09120000002', 'active')],
    total: 2,
    counts: { active: 2, unsubscribed: 0 },
  });
  assert.equal(out.total, 2);
  assert.deepEqual(out.counts, { active: 2, unsubscribed: 0 });
});

test('a bare array derives the same totals (regression: the branch that already worked)', () => {
  const out = deriveSubscribersPage([row('09120000001', 'active'), row('09120000002', 'unsubscribed')]);
  assert.equal(out.total, 2);
  assert.deepEqual(out.counts, { active: 1, unsubscribed: 1 });
  assert.equal(out.totalIsMinimum, false);
});

test('?status=active page keeps store-wide total/counts while listing only the active rows', () => {
  const out = deriveSubscribersPage({
    subscribers: [row('09120000001', 'active'), row('09120000002', 'active')],
    total: 3,
    counts: { active: 2, unsubscribed: 1 },
  });
  assert.equal(out.total, 3);
  assert.deepEqual(out.counts, { active: 2, unsubscribed: 1 });
  assert.equal(out.items.length, 2);
});

test('a full 20-row page with no server total is a lower bound, not the total', () => {
  const page = Array.from({ length: 20 }, (_, i) => row(`091200000${String(i).padStart(2, '0')}`, 'active'));
  const out = deriveSubscribersPage({ subscribers: page });
  assert.equal(out.total, 20);
  assert.equal(out.totalIsMinimum, true);
  // ...while a server total on a full page is exact.
  const exact = deriveSubscribersPage({ subscribers: page, total: 25, counts: { active: 25, unsubscribed: 0 } });
  assert.equal(exact.total, 25);
  assert.equal(exact.totalIsMinimum, false);
});

test('an empty page derives zero totals without a throw', () => {
  const out = deriveSubscribersPage({ subscribers: [] });
  assert.equal(out.total, 0);
  assert.deepEqual(out.counts, { active: 0, unsubscribed: 0 });
  assert.equal(out.totalIsMinimum, false);
});
