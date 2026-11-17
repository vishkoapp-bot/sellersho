import { test, mock } from 'node:test';
import assert from 'node:assert/strict';
import { createCartonDraftOperation } from './cartonDraft';
import { supplierApi } from './supplierApi';
import { tokenStore } from './apiClient';

test('an unchanged carton draft retries its real API payload/key after a lost response; edits and session changes rotate it', async () => {
  const calls: any[] = [];
  let lost = true;
  const fetch = mock.method(globalThis, 'fetch', async (_url: any, init: RequestInit) => {
    calls.push(JSON.parse(String(init.body)));
    if (lost) { lost = false; throw new TypeError('response lost'); }
    return new Response(JSON.stringify({ shipment: { id: 'one', shipment_code: 'CARTON12' } }), { status: 200, headers: { 'Content-Type': 'application/json' } });
  });
  try {
    const draft = createCartonDraftOperation();
    const payload = { warehouse_id: 'w1', items: [{ variant_id: 'sku', expected_qty: 1 }] };
    await assert.rejects(draft.submit(payload));
    assert.deepEqual(await draft.submit(payload), { id: 'one', shipment_code: 'CARTON12' });
    assert.ok(calls[0].creation_key);
    assert.equal(calls[0].creation_key, calls[1].creation_key);
    await draft.submit({ ...payload, items: [{ variant_id: 'sku', expected_qty: 2 }] });
    assert.notEqual(calls[2].creation_key, calls[1].creation_key);
    await draft.submit({ ...payload, warehouse_id: 'w2' });
    assert.notEqual(calls[3].creation_key, calls[2].creation_key);
    tokenStore.invalidateRequests();
    await draft.submit({ ...payload, warehouse_id: 'w2' });
    assert.notEqual(calls[4].creation_key, calls[3].creation_key);
    draft.reset();
    await draft.submit({ ...payload, warehouse_id: 'w2' });
    assert.notEqual(calls[5].creation_key, calls[4].creation_key);
  } finally { fetch.mock.restore(); }
});

test('carton API refuses missing keys and every invalid line before fetching', async () => {
  const fetch = mock.method(globalThis, 'fetch', async () => new Response('{}'));
  try {
    await assert.rejects(supplierApi.createShipment({ warehouse_id: 'w', items: [{ variant_id: 'sku', expected_qty: 1 }] } as any));
    const draft = createCartonDraftOperation();
    await assert.rejects(draft.submit({ warehouse_id: 'w', items: [{ variant_id: 'sku', expected_qty: 1 }, { variant_id: 'other', expected_qty: 2147483648 }] }));
    assert.equal(fetch.mock.callCount(), 0);
  } finally { fetch.mock.restore(); }
});

test('an ambiguous HTTP 200 carton response rejects and retains its creation key for retry', async () => {
  const responses = [
    '{}',
    '{"shipment":{"id":"one"',
    'null',
    '{"shipment":null}',
    '{"shipment":{}}',
    '{"shipment":{"id":"one"}}',
    '{"shipment":{"id":"","shipment_code":"CARTON12"}}',
  ];
  for (const response of responses) {
    const calls: any[] = [];
    const shipment = { id: 'one', shipment_code: 'CARTON12' };
    const fetch = mock.method(globalThis, 'fetch', async (_url: any, init: RequestInit) => {
      calls.push(JSON.parse(String(init.body)));
      return new Response(calls.length === 1 ? response : JSON.stringify({ shipment }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    });
    try {
      const draft = createCartonDraftOperation();
      const payload = { warehouse_id: 'w', items: [{ variant_id: 'sku', expected_qty: 1 }] };
      await assert.rejects(draft.submit(payload), (cause: unknown) => cause instanceof Error, `ambiguous response resolved: ${response}`);
      assert.deepEqual(await draft.submit(payload), shipment);
      assert.ok(calls[0].creation_key);
      assert.equal(calls[1].creation_key, calls[0].creation_key);
    } finally { fetch.mock.restore(); }
  }
});

test('carton source evidence is sent exactly and changing its business reference rotates the retry key', async () => {
  const calls: any[] = [];
  const fetch = mock.method(globalThis, 'fetch', async (_url: any, init: RequestInit) => {
    calls.push(JSON.parse(String(init.body)));
    return new Response(JSON.stringify({shipment: {id: 'one', shipment_code: 'CARTON12'}}), {status: 200, headers: {'Content-Type': 'application/json'}});
  });
  try {
    const draft = createCartonDraftOperation();
    const payload = {warehouse_id: 'w', items: [{variant_id: 'sku', expected_qty: 1}], source: {source_kind: 'external_replenishment', external_reference: 'note-1', reason: 'counted purchase'}};
    await draft.submit(payload); await draft.submit(payload);
    assert.deepEqual(calls[0].source, payload.source);
    assert.equal(calls[0].creation_key, calls[1].creation_key);
    await draft.submit({...payload, source: {...payload.source, external_reference: 'note-2'}});
    assert.notEqual(calls[2].creation_key, calls[1].creation_key);
  } finally {fetch.mock.restore();}
});
