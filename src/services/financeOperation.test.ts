import { test, mock } from 'node:test';
import assert from 'node:assert/strict';
import { storeApi } from './storeApi';
import { tokenStore } from './apiClient';
import { financeApi } from './financeApi';
import { supplierApi } from './supplierApi';

test('real seller finance calls retain operation keys after lost responses and rearm after acknowledged success', async () => {
  for (const [submit,result] of [
    [() => storeApi.purchaseService('finance-retry', 'standard', 'store-one'),{id:'store-one',activePlanId:'standard',amount:890000,durationDays:90,planExpiresAt:'2027-02-01T00:00:00Z'}],
    [() => storeApi.requestPayout('finance-retry', 2000000, 'IR-destination', 'store-one'),{id:'payout-one',storeId:'store-one',amount:2000000,status:'pending'}],
    [() => storeApi.requestEarlyWithdrawal('finance-retry', 2000000, 'IR-destination', 'store-one'),{id:'early-one',storeId:'store-one',grossAmount:2000000,netAmount:1800000,feeAmount:200000,status:'pending'}],
    [() => financeApi.createAdjustment({ targetKind: 'seller', targetId: 'store-one', direction: 'credit', amount: 3000000, reason: 'test' }), { success: true, docId: 'adjust-one' }],
    [() => supplierApi.requestSettlement(2000000, 'IR-destination', '', 'Supplier holder'), { success: true, id: 'supplier-payout', payout_request_id: 'supplier-payout' }],
  ] as const) {
    const keys: string[] = [];
    let lose = true;
    const fetch = mock.method(globalThis, 'fetch', async (_url: unknown, init: RequestInit) => {
      keys.push((init.headers as Record<string, string>)['Idempotency-Key']);
      if (lose) { lose = false; throw new TypeError('response lost after commit'); }
      return new Response(JSON.stringify(result), { status: 200, headers: { 'Content-Type': 'application/json' } });
    });
    try {
      await assert.rejects(submit());
      await submit();
      assert.ok(keys[0], 'actual finance API request needs a durable operation identity');
      assert.equal(keys[0], keys[1]);
      await submit();
      assert.notEqual(keys[1], keys[2], 'acknowledged completion starts a new operation');
    } finally { fetch.mock.restore(); }
  }
});

test('finance retries preserve keys on uncertain HTTP errors; changed payload and session rotate them', async () => {
  const keys: string[] = [];
  const fetch = mock.method(globalThis, 'fetch', async (_url: unknown, init: RequestInit) => {
    keys.push((init.headers as Record<string, string>)['Idempotency-Key']);
    return new Response('{"error":"outcome needs recovery"}', { status: 409, headers: { 'Content-Type': 'application/json' } });
  });
  try {
    await assert.rejects(storeApi.requestPayout('finance-edit', 2000000, 'IR-one', 'edit-store'));
    await assert.rejects(storeApi.requestPayout('finance-edit', 2000000, 'IR-one', 'edit-store'));
    assert.ok(keys[0]); assert.equal(keys[0], keys[1]);
    await assert.rejects(storeApi.requestPayout('finance-edit', 3000000, 'IR-one', 'edit-store'));
    assert.notEqual(keys[1], keys[2]);
    tokenStore.invalidateRequests();
    await assert.rejects(storeApi.requestPayout('finance-edit', 3000000, 'IR-one', 'edit-store'));
    assert.notEqual(keys[2], keys[3]);
  } finally { fetch.mock.restore(); }
});

test('an unresolved operation survives token refresh and store slug changes for the same immutable account', async () => {
  let token = `header.${btoa(JSON.stringify({ userId: 'immutable-user', nonce: 1 }))}.signature`;
  const getToken = mock.method(tokenStore, 'getToken', () => token);
  const keys: string[] = [];
  const fetch = mock.method(globalThis, 'fetch', async (_url: unknown, init: RequestInit) => {
    keys.push((init.headers as Record<string, string>)['Idempotency-Key']);
    throw new TypeError('response lost');
  });
  try {
    await assert.rejects(storeApi.purchaseService('old-slug', 'standard', 'immutable-store'));
    token = `header.${btoa(JSON.stringify({ userId: 'immutable-user', nonce: 2 }))}.signature`;
    tokenStore.invalidateRequests();
    await assert.rejects(storeApi.purchaseService('new-slug', 'standard', 'immutable-store'));
    assert.ok(keys[0]); assert.equal(keys[0], keys[1]);
    token = `header.${btoa(JSON.stringify({ userId: 'different-user' }))}.signature`;
    await assert.rejects(storeApi.purchaseService('new-slug', 'standard', 'immutable-store'));
    assert.notEqual(keys[1], keys[2]);
  } finally { fetch.mock.restore(); getToken.mock.restore(); }
});

test('malformed or missing committed result identities preserve the real seller request key', async () => {
  for (const malformed of ['{"id":', '{}', '{"id":"shape-store","activePlanId":"standard"}', '{"id":"different-store","activePlanId":"standard"}']) {
    const keys: string[] = []; let attempts = 0;
    const fetch = mock.method(globalThis, 'fetch', async (_url: unknown, init: RequestInit) => {
      keys.push((init.headers as Record<string, string>)['Idempotency-Key']);
      return new Response(attempts++ === 0 ? malformed : '{"id":"shape-store","activePlanId":"standard","amount":890000,"durationDays":90,"planExpiresAt":"2027-02-01T00:00:00Z"}', { status: 200, headers: { 'Content-Type': 'application/json' } });
    });
    try {
      await assert.rejects(storeApi.purchaseService('shape-slug', 'standard', 'shape-store'));
      await storeApi.purchaseService('shape-slug', 'standard', 'shape-store');
      assert.equal(keys[0], keys[1]);
    } finally { fetch.mock.restore(); }
  }
});
