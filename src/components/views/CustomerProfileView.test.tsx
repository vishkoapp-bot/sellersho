import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { it } from 'node:test';
import assert from 'node:assert/strict';
import { CustomerReturnsMenuRow } from './CustomerProfileView';
import { orderApi } from '../../services/orderApi';

// F1: the profile menu gains a «مرجوعی‌های من» row with a count badge from
// orderApi.getMyReturns(), navigating to the existing returns section
// inside the Orders view.
//
// Harness limit (same as SupplierInventoryTab.test.tsx): tsx --test +
// renderToStaticMarkup has no DOM/jsdom, so effects never run — the
// container's getMyReturns fetch and the scroll-to-section navigation
// cannot run here. What IS covered: the row itself (title, count badge,
// empty state, loading state, return-window note) as a pure component,
// and the getMyReturns fetch contract (GET /returns, envelope unwrap).
// The live click-through (tap the row → lands on the returns list with
// matching statuses) stays owed to the parent's live-browser check.

const stubFetch = (payload: unknown) => {
  const calls: Array<{ url: string; init: any }> = [];
  const prev = globalThis.fetch;
  (globalThis as any).fetch = async (url: string, init: any) => {
    calls.push({ url, init });
    return {
      ok: true,
      status: 200,
      headers: { get: () => 'application/json' },
      json: async () => payload,
    };
  };
  return { calls, restore: () => { (globalThis as any).fetch = prev; } };
};

const renderRow = (returnsCount: number | null) =>
  renderToStaticMarkup(
    <CustomerReturnsMenuRow returnsCount={returnsCount} onOpen={() => {}} />,
  );

it('returns row shows the count badge from getMyReturns with the return-window note', () => {
  const html = renderRow(2);
  assert.match(html, /مرجوعی‌های من/);
  assert.match(html, /۲ مرجوعی ثبت شده/);
  assert.match(html, /۷ روز مهلت تست و عودت کالا پس از تحویل/);
});

it('returns row shows the honest empty state when there are no returns, and a loading state while fetching', () => {
  assert.match(renderRow(0), /هنوز مرجوعی‌ای ثبت نکرده‌اید/);
  assert.match(renderRow(0), /۷ روز مهلت تست و عودت کالا پس از تحویل/);
  assert.match(renderRow(null), /در حال دریافت…/);
  assert.doesNotMatch(renderRow(null), /مرجوعی ثبت شده/);
});

it('getMyReturns hits GET /returns and unwraps the {returns: [...]} envelope', async () => {
  const { calls, restore } = stubFetch({ returns: [{ id: 'r1' }, { id: 'r2' }] });
  try {
    const rows = await orderApi.getMyReturns();
    assert.equal(calls.length, 1);
    assert.equal(calls[0].url, '/api/v1/returns');
    assert.equal(calls[0].init.method, 'GET');
    assert.equal(rows.length, 2);
    assert.equal(rows[0].id, 'r1');
  } finally {
    restore();
  }
});

it('getMyReturns also accepts a bare array response', async () => {
  const { restore } = stubFetch([{ id: 'r9' }]);
  try {
    const rows = await orderApi.getMyReturns();
    assert.equal(rows.length, 1);
    assert.equal(rows[0].id, 'r9');
  } finally {
    restore();
  }
});
