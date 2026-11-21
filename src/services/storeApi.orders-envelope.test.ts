import { test, mock } from 'node:test';
import assert from 'node:assert/strict';
import { storeApi } from './storeApi';

// Batch 1 (C1/C4): GET /stores/{slug}/orders returns the PERF-02
// envelope {orders, nextCursor, hasMore, limit}, not a bare array.
// getStoreOrders must always resolve to the bare array — the envelope
// object has no .filter, which crashed the wallet tab (C1) and emptied
// the orders tab (C4).

const order = { id: 'o-1', orderNumber: '29', status: 'delivered', dropshipperProfit: 52800, profitReleased: true };

function fakeFetch(payload: unknown, seen: string[]) {
  return mock.method(globalThis, 'fetch', async (input: RequestInfo | URL) => {
    seen.push(String(input));
    return {
      ok: true,
      status: 200,
      headers: { get: () => 'application/json' },
      json: async () => payload,
    } as unknown as Response;
  });
}

test('getStoreOrders unwraps the PERF-02 envelope into a bare array', async () => {
  const seen: string[] = [];
  const fake = fakeFetch({ orders: [order], nextCursor: null, hasMore: false, limit: 20 }, seen);
  try {
    const result = await storeApi.getStoreOrders('mone');
    assert.deepEqual(result, [order]);
    assert.equal(seen[0], '/api/v1/stores/mone/orders');
  } finally { fake.mock.restore(); }
});

test('getStoreOrders still passes a bare array through unchanged', async () => {
  const seen: string[] = [];
  const fake = fakeFetch([order], seen);
  try {
    assert.deepEqual(await storeApi.getStoreOrders('mone'), [order]);
  } finally { fake.mock.restore(); }
});

test('getStoreOrders returns [] for a non-array, non-envelope payload instead of handing callers an object', async () => {
  const seen: string[] = [];
  for (const payload of [null, {}, { orders: null }, 'error-page']) {
    const fake = fakeFetch(payload, seen);
    try {
      assert.deepEqual(await storeApi.getStoreOrders('mone'), [], `payload ${JSON.stringify(payload)}`);
    } finally { fake.mock.restore(); }
  }
});
