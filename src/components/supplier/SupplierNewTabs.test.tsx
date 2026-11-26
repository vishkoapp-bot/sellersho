import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { it } from 'node:test';
import assert from 'node:assert/strict';
import { ToastProvider } from '../../context/ToastContext';
import { supplierApi } from '../../services/supplierApi';
import { createSupplierReturnBackSubmitter, SupplierTapinCard } from './SupplierNewTabs';

// E6: the Tapin card must NOT auto-test on mount, and the manual test must
// be bounded and cancellable (AbortController + 15 s client timeout).
//
// Harness limit (same as SupplierMyWarehousesTab.test.tsx): tsx --test +
// renderToStaticMarkup has no DOM/jsdom, so effects never run — the card
// cannot load its settings here, no click can run the test, and the
// auto-test removal cannot be observed as a click-through. What IS covered
// here: the testTapinConnection fetch contract (URL/method/body/signal),
// the cancel path (an aborted signal rejects with the Persian message),
// and the initial render shape (loading state, no test button before the
// connection is known, no test request fired by rendering). The live
// click-through (open tab → zero POST /supplier/tapin/test → click →
// «در حال تست اتصال…» with spinner → result line) stays owed to the
// parent's live-browser check.

const stubFetch = (payload: unknown, opts: { throwOnAbort?: boolean } = {}) => {
  const calls: Array<{ url: string; init: any }> = [];
  const prev = globalThis.fetch;
  (globalThis as any).fetch = async (url: string, init: any) => {
    calls.push({ url, init });
    if (opts.throwOnAbort && init?.signal?.aborted) {
      const err: any = new Error('The operation was aborted.');
      err.name = 'AbortError';
      throw err;
    }
    return {
      ok: true,
      status: 200,
      headers: { get: () => 'application/json' },
      json: async () => payload,
    };
  };
  return { calls, restore: () => { (globalThis as any).fetch = prev; } };
};

it('replays an uncertain return-back request and rearms its key after success', async () => {
  const keys: string[] = [];
  const originalRequest = supplierApi.requestCustomerReturnBack;
  supplierApi.requestCustomerReturnBack = async (returnItemId, quantity, reason, key) => {
    keys.push(key);
    if (keys.length === 1) throw new Error('response lost');
    if (keys.length === 2 || keys.length === 5) return {};
    return { id: 'persisted-request', returnItemId, quantity, reason };
  };
  try {
    const submit = createSupplierReturnBackSubmitter();
    await assert.rejects(submit('item-1', 1, 'send back'), /response lost/);
    await assert.rejects(submit('item-1', 1, 'send back'), /پاسخ ثبت درخواست عودت کامل نبود/);
    await submit('item-1', 1, 'send back');
    assert.deepEqual(keys.slice(0, 3), [keys[0], keys[0], keys[0]]);

    await submit('item-1', 1, 'send back');
    assert.notEqual(keys[3], keys[0]);
    await assert.rejects(submit('item-1', 1, 'send back'), /پاسخ ثبت درخواست عودت کامل نبود/);
    await submit('item-1', 1, 'different reason');
    assert.notEqual(keys[5], keys[4]);
  } finally {
    supplierApi.requestCustomerReturnBack = originalRequest;
  }
});

it('testTapinConnection posts an empty body to /supplier/tapin/test with a bounded signal', async () => {
  const { calls, restore } = stubFetch({ ok: true, serviceActive: true, message: 'سرویس فعال است.' });
  try {
    const res = await supplierApi.testTapinConnection();
    assert.equal(calls.length, 1);
    assert.equal(calls[0].url, '/api/v1/supplier/tapin/test');
    assert.equal(calls[0].init.method, 'POST');
    assert.equal(calls[0].init.body, '{}');
    // apiRequest always attaches a signal (its own 15 s AbortSignal.timeout
    // bound, combined with any caller signal) — the call can never hang
    // indefinitely client-side.
    assert.ok(calls[0].init.signal instanceof AbortSignal);
    assert.equal(res.ok, true);
    assert.equal(res.serviceActive, true);
  } finally {
    restore();
  }
});

it('testTapinConnection accepts a caller signal: an aborted signal rejects with the Persian no-response message', async () => {
  const { calls, restore } = stubFetch({ ok: true }, { throwOnAbort: true });
  try {
    const controller = new AbortController();
    controller.abort();
    await assert.rejects(
      supplierApi.testTapinConnection(controller.signal),
      /پاسخی از سرور دریافت نشد؛ لطفاً دوباره تلاش کنید\./,
    );
    assert.equal(calls.length, 1);
    // apiRequest combines the caller signal with its own 15 s timeout via
    // AbortSignal.any — the fetch receives a combined signal, already
    // aborted because the caller controller was aborted.
    assert.ok(calls[0].init.signal instanceof AbortSignal);
    assert.equal(calls[0].init.signal.aborted, true);
  } finally {
    restore();
  }
});

it('tapin card renders a loading state with no test button and fires no test request before the connection is known', () => {
  const { calls, restore } = stubFetch({ tapinConnected: true, tapinShopId: 'shop-1' });
  try {
    const html = renderToStaticMarkup(
      <ToastProvider>
        <SupplierTapinCard />
      </ToastProvider>,
    );
    assert.match(html, /اتصال تاپین/);
    assert.match(html, /<p role="status"[^>]*>در حال بارگذاری…<\/p>/);
    // The test button renders only once the saved connection is known
    // (connected === true after the settings load) — never on first paint,
    // and never in a disabled «در حال تست…» state.
    assert.doesNotMatch(html, /تست اتصال/);
    assert.doesNotMatch(html, /در حال تست/);
    assert.equal(calls.filter((c) => c.url === '/api/v1/supplier/tapin/test').length, 0);
  } finally {
    restore();
  }
});
