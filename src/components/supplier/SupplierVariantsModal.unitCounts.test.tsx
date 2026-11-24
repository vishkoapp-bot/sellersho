import assert from 'node:assert/strict';
import test from 'node:test';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { ApiError } from '../../services/apiClient';
import { fetchVariantUnitCounts, VariantUnitCountLine } from './SupplierVariantsModal';

test('variant counts use filtered label totals and preserve raw codes for failed variants', async () => {
  let calls = 0;
  const result = await fetchVariantUnitCounts(async (productId, params) => {
    assert.equal(productId, 'p1');
    assert.deepEqual(params, { variant_id: params.variant_id, status: 'in_stock', limit: 1 });
    calls += 1;
    await Promise.resolve();
    assert.equal(calls, 2);
    assert.ok(['blue', 'red'].includes(params.variant_id));
    if (params.variant_id === 'red') throw new ApiError(409, 'خطایی رخ داد؛ دوباره تلاش کنید.', { error: 'variant_conflict' });
    return { total_for_filter: 17 };
  }, 'p1', [{ id: 'blue' }, { id: 'red' }]);
  assert.deepEqual(result, { counts: { blue: 17 }, errors: { red: 'variant_conflict' } });
});

test('variant stock and an equal unit count appear on one line', () => {
  const html = renderToStaticMarkup(<VariantUnitCountLine stock={4} unitCount={4} />);
  assert.match(html, /موجودی: ۴ عدد • ۴ واحد سریال‌دار/);
  assert.doesNotMatch(html, /amber/);
});

test('variant discrepancies are amber and failures show a dash and the backend code', () => {
  const mismatch = renderToStaticMarkup(<VariantUnitCountLine stock={4} unitCount={3} />);
  assert.match(mismatch, /amber/);
  assert.match(mismatch, /مغایرت: ۳ واحد در برابر ۴ عدد/);
  const failed = renderToStaticMarkup(<VariantUnitCountLine stock={4} errorCode="variant_conflict" />);
  assert.match(failed, /موجودی: ۴ عدد • — واحد سریال‌دار/);
  assert.match(failed, /variant_conflict/);
});
