import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { it } from 'node:test';
import assert from 'node:assert/strict';
import { JobHealthPanel, RequestLookupPanel, RequestLookupResult, type JobHealth, type RequestLookup } from './AdminOperationsTab';

const job = (patch: Partial<JobHealth> = {}): JobHealth => ({ job: 'unpaid-order-expiry', running: false, missing: false, stale: false, disabled: false, error: '', lastSuccess: '2026-11-15T20:00:00Z', started: null, finished: null, cutover: null, runs: 2, affected: 7, ready: 0, review: 0, capped: false, oldestReadySeconds: 0, ...patch });
it('shows missing successes and review blockers separately from ready work', () => {
  const html = renderToStaticMarkup(<JobHealthPanel jobs={[job({ missing: true, stale: true, ready: 5, review: 2, oldestReadySeconds: 310 })]} />);
  assert.match(html, /role="alert"/);
  assert.match(html, /هنوز اجرای موفقی ثبت نشده/);
  assert.match(html, /نیازمند بررسی/);
  assert.match(html, /آمادهٔ پردازش/);
  assert.match(html, /لغو سفارش‌های پرداخت‌نشده/);
});
it('explains disabled monthly work without an alarm', () => {
  const html = renderToStaticMarkup(<JobHealthPanel jobs={[job({ job: 'monthly-settlement', missing: true, stale: false, disabled: true })]} />);
  assert.match(html, /تنظیم نشده/);
  assert.doesNotMatch(html, /role="alert"/);
});
it('alerts on financial review blockers even when monthly scheduling is disabled', () => {
  const html = renderToStaticMarkup(<JobHealthPanel jobs={[job({ job: 'monthly-settlement', missing: true, stale: false, disabled: true, review: 2 })]} />);
  assert.match(html, /role="alert"/);
  assert.match(html, /bg-amber-50/);
  assert.match(html, /تنظیم نشده/);
  assert.match(html, /نیازمند بررسی/);
  assert.match(html, /بررسی مالی لازم است/);
  assert.doesNotMatch(html, /هنوز اجرای موفقی ثبت نشده/);
});
it('distinguishes automatic expiry retries from financial review', () => {
  const automatic = { ...job({ ready: 2, review: 0 }), automaticRetry: 2 };
  const html = renderToStaticMarkup(<JobHealthPanel jobs={[automatic]} />);
  assert.match(html, /تلاش مجدد خودکار/);
  assert.doesNotMatch(html, /بررسی مالی لازم است/);
  assert.doesNotMatch(html, /role="alert"/);
});
it('keeps missing telemetry visible instead of showing an empty healthy panel', () => {
  assert.match(renderToStaticMarkup(<JobHealthPanel />), /وضعیت اجراها دریافت نشده/);
});

// OBS-01 G-03: admin-panel request lookup view.
const lookup = (patch: Partial<RequestLookup> = {}): RequestLookup => ({
  request_id: 'req-abc123', method: 'GET', route: '/api/v1/products/{id}', class: 'api',
  opclass: 'read', status: 200, outcome: 'success', duration_ms: 412,
  started_at: '2026-11-18T14:00:00Z',
  spans: [{ component: 'sql', operation: 'query', outcome: 'success', duration_ms: 380 }],
  dropped_spans: 0, ...patch,
});
it('renders the lookup box in the operations panel', () => {
  const html = renderToStaticMarkup(<RequestLookupPanel />);
  assert.match(html, /جست‌وجوی درخواست با شناسه/);
  assert.match(html, /X-Request-ID/);
  assert.match(html, /۱۰۰۰ درخواستِ اخیر/);
});
it('renders a full breakdown: route, status, duration and spans table', () => {
  const html = renderToStaticMarkup(<RequestLookupResult result={lookup()} notFound={false} />);
  assert.match(html, /req-abc123/);
  assert.match(html, /\/api\/v1\/products\/\{id\}/);
  assert.match(html, /412/);
  assert.match(html, /sql/);
  assert.match(html, /query/);
  assert.match(html, /380/);
});
it('shows the dropped-spans count', () => {
  const html = renderToStaticMarkup(<RequestLookupResult result={lookup({ dropped_spans: 7 })} notFound={false} />);
  assert.match(html, /عملیات‌های حذف‌شده/);
  assert.match(html, /۷/);
});
it('shows an honest empty state on 404 and never a fabricated breakdown', () => {
  const html = renderToStaticMarkup(<RequestLookupResult result={null} notFound={true} />);
  assert.match(html, /در حلقه نیست/);
  assert.match(html, /۱۰۰۰ درخواستِ اخیر/);
  assert.doesNotMatch(html, /req-abc123/);
  assert.doesNotMatch(html, /<table/);
});
it('renders nothing when there is no result and no 404', () => {
  assert.equal(renderToStaticMarkup(<RequestLookupResult result={null} notFound={false} />), '');
});
