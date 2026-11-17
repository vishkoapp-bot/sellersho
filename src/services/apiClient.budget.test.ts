import { test, mock } from 'node:test';
import assert from 'node:assert/strict';
import { apiRequest, ApiError } from './apiClient';

test('a caller signal does not disable the finite browser request timeout', async () => {
  let aborted = false;
  const fake = mock.method(globalThis, 'fetch', async (_input: RequestInfo | URL, init?: RequestInit) => new Promise<Response>((_resolve, reject) => {
    const signal = init!.signal!;
    signal.addEventListener('abort', () => { aborted = true; reject(signal.reason); }, { once: true });
    setTimeout(() => reject(new Error('synthetic safety cutoff')), 250);
  }));
  try {
    const caller = new AbortController(); const started = Date.now();
    await assert.rejects(apiRequest('/budget-read', { signal: caller.signal, timeout: 25 }), (e: unknown) => e instanceof ApiError && e.status === 0);
    assert.equal(aborted, true, 'caller signal must be combined with request timeout');
    assert.ok(Date.now() - started < 200, 'request waited for the safety cutoff');
  } finally { fake.mock.restore(); }
});
