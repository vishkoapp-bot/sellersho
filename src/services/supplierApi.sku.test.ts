import { it, mock } from 'node:test';
import assert from 'node:assert/strict';
import { supplierApi } from './supplierApi';

it('serializes exact SKU identity for stock updates and returns while preserving legacy optional requests', async () => {
  const calls: { url: string; init: RequestInit }[] = [];
  const mocked = mock.method(globalThis, 'fetch', async (url: string | URL | Request, init: RequestInit) => {
    calls.push({ url: String(url), init });
    return new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } });
  });
  try {
    await supplierApi.updateStock('p', 4, 'counted', 'sku-a');
    assert.deepEqual(JSON.parse(String(calls[0].init.body)), { new_stock: 4, reason: 'counted', variantId: 'sku-a' });
    await supplierApi.requestReturn('p', 2, 'postal', 'damaged', 'return-a', 'sku-a');
    assert.equal(JSON.parse(String(calls[1].init.body)).variantId, 'sku-a');
    assert.equal((calls[1].init.headers as Record<string, string>)['Idempotency-Key'], 'return-a');
    await supplierApi.updateStock('old', 3, 'legacy recount');
    assert.deepEqual(JSON.parse(String(calls[2].init.body)), { new_stock: 3, reason: 'legacy recount' });
  } finally { mocked.mock.restore(); }
});
