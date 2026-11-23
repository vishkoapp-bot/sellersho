import assert from 'node:assert/strict';
import { it } from 'node:test';
import { adminApi } from './adminApi';
import { tokenStore } from './apiClient';

it('gets shipping settings with auth and preserves a null charge', async (t) => {
  const settings = { supplierReturnPostalCharge: null, defaultItemWeightGrams: 300 };
  t.mock.method(tokenStore, 'getToken', () => 'test-admin-token');
  const fetch = t.mock.method(globalThis, 'fetch', async (_url: string | URL | Request, _init?: RequestInit) => Response.json(settings));

  assert.deepEqual(await adminApi.getShippingSettings(), settings);
  assert.equal(fetch.mock.callCount(), 1);
  const [url, init] = fetch.mock.calls[0].arguments;
  assert.equal(url, '/api/v1/admin/settings/shipping');
  assert.equal(init?.method, 'GET');
  assert.equal(new Headers(init?.headers).get('Authorization'), 'Bearer test-admin-token');
});

it('saves the configured charge and preserves an explicit null in the PUT body', async (t) => {
  const calls: { url: string; init?: RequestInit }[] = [];
  t.mock.method(tokenStore, 'getToken', () => 'test-admin-token');
  t.mock.method(globalThis, 'fetch', async (url: string | URL | Request, init?: RequestInit) => {
    calls.push({ url: String(url), init });
    return Response.json(JSON.parse(String(init?.body)));
  });

  for (const payload of [
    { supplierReturnPostalCharge: 200000, defaultItemWeightGrams: 300 },
    { supplierReturnPostalCharge: null, defaultItemWeightGrams: 300 },
    { supplierReturnPostalCharge: null },
  ]) {
    assert.deepEqual(await adminApi.saveShippingSettings(payload), payload);
    const { url, init } = calls.at(-1)!;
    assert.equal(url, '/api/v1/admin/settings/shipping');
    assert.equal(init?.method, 'PUT');
    assert.equal(new Headers(init?.headers).get('Authorization'), 'Bearer test-admin-token');
    assert.deepEqual(JSON.parse(String(init?.body)), payload);
  }
  assert.equal(calls.length, 3);
});
