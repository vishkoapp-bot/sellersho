import { it } from 'node:test';
import assert from 'node:assert/strict';
import { mergeCatalogLists } from './ProductContext';

const product = (id: string) => ({ id, title: id } as any);

it('keeps the live list first in live mode and appends customs only as gap-fill', () => {
  const live = [product('prod-new'), product('prod-old')];
  const customs = [product('stale-mock'), product('prod-old')];
  const merged = mergeCatalogLists(live, customs, false);
  assert.deepEqual(merged.map((p) => p.id), ['prod-new', 'prod-old', 'stale-mock']);
});

it('keeps customs first in mock mode', () => {
  const base = [product('prod-a'), product('prod-b')];
  const customs = [product('custom-1')];
  const merged = mergeCatalogLists(base, customs, true);
  assert.deepEqual(merged.map((p) => p.id), ['custom-1', 'prod-a', 'prod-b']);
});

it('dedupes by id in both modes', () => {
  const base = [product('x')];
  const customs = [product('x')];
  assert.equal(mergeCatalogLists(base, customs, false).length, 1);
  assert.equal(mergeCatalogLists(base, customs, true).length, 1);
});
