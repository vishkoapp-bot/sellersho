import assert from 'node:assert/strict';
import test from 'node:test';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import type { UnitLabelRow } from '../../services/unitsApi';
import { loadOrderItemUnits, OrderUnitCodesList } from './SupplierOrderItemIdentity';

const codes = ['Unit-Ab01', 'UNIT-02', 'unit-03', 'UNIT-04'];

test('up to three captured codes render inline and exactly as returned', () => {
  const html = renderToStaticMarkup(<OrderUnitCodesList codes={codes.slice(0, 3)} />);
  assert.match(html, /dir="ltr" class="font-mono[^"]*">Unit-Ab01/);
  assert.match(html, /UNIT-02/);
  assert.match(html, /unit-03/);
  assert.doesNotMatch(html, /نمایش کدها/);
});

test('four codes collapse behind a count toggle and expanded pages offer load more', () => {
  const collapsed = renderToStaticMarkup(<OrderUnitCodesList codes={codes} nextCursor="page-2" />);
  assert.match(collapsed, /۴\+ واحد: نمایش کدها/);
  assert.doesNotMatch(collapsed, /Unit-Ab01/);
  const expanded = renderToStaticMarkup(<OrderUnitCodesList codes={codes} nextCursor="page-2" expanded onLoadMore={() => {}} />);
  assert.match(expanded, /نمایش بیشتر/);
  for (const code of codes) assert.ok(expanded.includes(code));
});

test('an empty captured-code list renders nothing', () => {
  assert.equal(renderToStaticMarkup(<OrderUnitCodesList codes={[]} />), '');
});

test('order pagination uses the order-item ID and returns the endpoint page', async () => {
  const unit: UnitLabelRow = {
    code: 'Unit-Ab01', spnc: 'V1', variant_id: 'v1', variant_name: 'آبی', product_id: 'p1', spn: 'P1',
    title: 'نمونه', status: 'sold', printed_at: null, print_count: 0, last_printed_at: null,
  };
  const page = { units: [unit], next_cursor: null };
  assert.deepEqual(await loadOrderItemUnits(async params => {
    assert.deepEqual(params, { order_item_id: 'line-1', limit: 200, cursor: 'page-2' });
    return page;
  }, 'line-1', 'page-2'), page);
});
