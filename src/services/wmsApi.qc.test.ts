import { test, mock } from 'node:test';
import assert from 'node:assert/strict';
import { wmsApi } from './wmsApi';

test('QC requires an explicit acknowledgment and retries the identical receipt-line payload', async () => {
  const payload = { inboundId: 'INB-one', receiptLineId: 'IL-one', passedQuantity: 3, failedQuantity: 2, shortageQuantity: 0, defectReason: 'counted inspection' };
  for (const incomplete of ['{}', 'null', '{"success":true}', '{"success":false,"status":"completed"}']) {
    const calls: unknown[] = [];
    const fetch = mock.method(globalThis, 'fetch', async (_url: unknown, init: RequestInit) => {
      calls.push(JSON.parse(String(init.body)));
      return new Response(calls.length === 1 ? incomplete : '{"success":true,"status":"completed"}', { headers: { 'Content-Type': 'application/json' } });
    });
    try {
      await assert.rejects(wmsApi.completeQC('p', payload));
      assert.deepEqual(await wmsApi.completeQC('p', payload), { success: true, status: 'completed' });
      assert.deepEqual(calls, [payload, payload]);
    } finally { fetch.mock.restore(); }
  }
});
