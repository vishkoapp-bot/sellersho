import { test, mock } from 'node:test';
import assert from 'node:assert/strict';
import { supplierApi } from './supplierApi';

test('all image-upload requests carry the finite long-upload browser budget', async () => {
  const budgets = new Map<AbortSignal, number>(); const requested: (AbortSignal | undefined | null)[] = [];
  const timeout = mock.method(AbortSignal, 'timeout', (ms: number) => { const signal = new AbortController().signal; budgets.set(signal, ms); return signal; });
  const fetch = mock.method(globalThis, 'fetch', async (_url: RequestInfo | URL, init?: RequestInit) => { requested.push(init?.signal); return new Response('{"success":true,"imageUrl":"/synthetic.png","images":[]}', { status: 200, headers: { 'Content-Type': 'application/json' } }); });
  try {
    const file = new File(['synthetic'], 'image.png', { type: 'image/png' });
    await supplierApi.uploadVariantImage(file); await supplierApi.uploadVariantImages('p', 'v', [file]);
    await supplierApi.uploadProductImages('p', [file]); await supplierApi.uploadLogoImage(file);
    assert.equal(requested.length, 4);
    assert.deepEqual(requested.map(s => s ? budgets.get(s) : undefined), [75000, 75000, 75000, 75000]);
  } finally { fetch.mock.restore(); timeout.mock.restore(); }
});
