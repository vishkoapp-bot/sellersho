import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { it } from 'node:test';
import assert from 'node:assert/strict';
import { ToastProvider } from '../../context/ToastContext';
import { ApiError } from '../../services/apiClient';
import {
  extractSpncxErrorCode, isSpncxUnavailable, postBackfillDryRun, postBackfillRollback, postBackfillRun, postExcessDecision,
  type BackfillPlan, type BackfillProductPlan, type BackfillStatus, type ReconciliationReport,
} from '../../services/spncxAdminApi';
import {
  AdminSpncxTab, buildScopedProductIds, checkPlanTotals, classifyV0Lists, isRunEnabled,
  SpncxCoverageSummary, SpncxError, SpncxExcessDecisionRow, SpncxPlanView, SpncxRollbackPanel,
  SpncxRunDiff, SpncxStatusView,
} from './AdminSpncxTab';
import { SpncxReconciliationPanel } from './HistoricalReconciliationReviewTab';

const alloc = (variant_id: string, amount: number, is_default = false) => ({
  variant_id, spnc: 'SPEC-SPNC', variant_stock: 10, alloc: amount, already_minted: 0, is_default,
});
const product = (product_id: string, patch: Partial<BackfillProductPlan> = {}): BackfillProductPlan => ({
  product_id, title: 'نمونه', P: 10,
  alloc: [alloc('default-zero', 0, true), alloc('named-ten', 10)],
  lots_to_create: [], lots_to_reduce: [], top_up: 0, excess: 0, excess_decision: null,
  variant_stock_corrections: [], new_variants: [], trim_target: null, excluded: false,
  excluded_reason: null, flipped_variants: [],
  order_history: { orders: 0, default_ever_ordered: false, named_ever_ordered: false, named_variant_orders: [] },
  ...patch,
});
const diff = { previous_plan_hash: 'old-hash', changed_products: [
  { product_id: 'diff-product', alloc_changed: true, top_up_changed: false, excess_changed: true },
] };
const plan: BackfillPlan = {
  dry_run: true, plan_hash: 'abcdef0123456789abcdefghijkl', basis: 'products.stock',
  total_units: 4016, product_count: 40,
  group_totals: { doubled_non_default: 2324, equal: 260, other: 407, variant_less: 1025 },
  products: Array.from({ length: 40 }, (_, index) => product('plan-product-' + index)),
  global_assertions: [], demo_test_products: ['demo-veto'], inactive_products: ['inactive-veto'],
  pre_cutover_orders: { count: 40, items: 46, open_reservations: 1 }, diff_vs_previous_dry_run: diff,
};
const totals = { total_units: 4016, in_stock: 4016, removed: 0, distinct_codes: 4016, counters: 98 };
const report: ReconciliationReport = {
  generated_at: '2026-11-24T00:00:00Z',
  checks: [
    ...['V0', 'V1', 'V1b', 'V2', 'V3', 'V6', 'V7', 'V8', 'V9', 'V11', 'V12'].map(id =>
      ({ id, name: id + '-fixture', passed: true, rows: [], row_count: 0 })),
    { id: 'V1c', name: 'unit_status_vs_lot_status', passed: false,
      rows: [{ location_key: 'return:fixture', expected_status: '###NO-MAPPING###' }], row_count: 1 },
    { id: 'V4', name: 'global_totals', passed: false, totals, expected: { ...totals, counters: 97 } },
    { id: 'V5', name: 'backfill_completion', passed: true, summary: { pending: 0, completed: 3, failed: 0, excluded: 1 } },
    { id: 'V6b', name: 'unreconciled_holding_lots', passed: false, rows: [{ excess_decision: null, product_id: 'gate-item' }], row_count: 1 },
    { id: 'V10', name: 'physical_count_sample', passed: false, sampled_products: [], mismatches: [],
      details: 'no physical-count sample recorded in v1' },
  ],
  held_lots: [{ product_id: 'held-id', location_key: 'held-location', quantity: 3, review_by: '2026-12-24', excess_decision: 'write_off_accepted' }],
  coverage_gaps: Array.from({ length: 11 }, (_, index) => ({
    variant_id: index === 0 ? 'least-gap' : 'gap-' + index, requested: 20,
    allocated: 20 - index, shortfall: index, operation: 'fixture-operation',
  })),
  failed_captures: [{ order_id: 'capture-order', failure: 'unit_coverage_gap', mode: 'strict', at: 'capture-time' }],
};

it('extracts the wire error code independently of the mapped message', () => {
  assert.equal(extractSpncxErrorCode(new ApiError(423, 'خطا', { error: 'backfill_already_running' })), 'backfill_already_running');
  assert.equal(extractSpncxErrorCode(new ApiError(409, 'خطا')), null);
  assert.equal(extractSpncxErrorCode(new Error('test')), null);
});

it('asserts the full catalogue and skips scoped and excluded plans', () => {
  const wrong = { ...plan, group_totals: { ...plan.group_totals, doubled_non_default: 2323 } };
  assert.equal(checkPlanTotals(plan).passed, true);
  assert.equal(checkPlanTotals(plan).zeroAllocCount, 40);
  assert.equal(checkPlanTotals(wrong).passed, false);
  assert.match(renderToStaticMarkup(<SpncxPlanView plan={wrong} />), /role="alert".*خطای کنترل طرح/);
  assert.equal(checkPlanTotals(wrong, ['one-product']).applicable, false);
  assert.equal(checkPlanTotals({ ...wrong, products: [product('excluded', { excluded: true })] }).applicable, false);
});

it('classifies doubled history, trims and equal-order judgement calls', () => {
  const trim = (id: string, orders: number) => product(id, {
    trim_target: 'trim',
    alloc: [alloc('trim', 0), alloc('kept-low', 1), alloc('kept-high', 9), alloc('unkept', 0)],
    order_history: {
      orders: 20, default_ever_ordered: false, named_ever_ordered: true,
      named_variant_orders: [['trim', orders], ['kept-low', 1], ['kept-high', 5], ['unkept', 20]].map(([id, count]) => ({
        variant_id: String(id), spnc: 'SPNC', orders: Number(count),
      })),
    },
  });
  const lists = classifyV0Lists({ ...plan, products: [
    product('v0a-flag', { order_history: { ...product('').order_history, default_ever_ordered: true, orders: 1 } }),
    product('no-history'), trim('prod-vitamin-serum', 6), trim('prod-brush-set', 5), trim('demo-women-style-19', 4),
  ] });
  assert.deepEqual(lists.v0a.map(row => row.product_id), ['v0a-flag']);
  assert.deepEqual(lists.v0b.map(row => [row.product.product_id, row.flagged]), [
    ['prod-vitamin-serum', true], ['prod-brush-set', false], ['demo-women-style-19', false],
  ]);
  assert.deepEqual(lists.judgementCalls.map(row => row.product_id), ['no-history', 'prod-brush-set']);
});

it('requires a viewed dry run for the exact veto scope', () => {
  assert.deepEqual(buildScopedProductIds([product('b'), product('a')], new Set(['b'])), ['a']);
  assert.deepEqual(buildScopedProductIds([product('b')], new Set(['b'])), []);
  assert.equal(isRunEnabled(null, 'ALL'), false);
  assert.equal(isRunEnabled('ALL', 'a'), false);
  assert.equal(isRunEnabled('a', 'a'), true);
});

it('renders allocations, the hash, the zero-allocation assertion, diff and veto lists', () => {
  const html = renderToStaticMarkup(<SpncxPlanView plan={plan} />);
  for (const value of ['default-zero', 'named-ten', plan.plan_hash, '۴۰ تنوع با تخصیص صفر', 'diff-product', 'demo-veto', 'inactive-veto']) {
    assert.ok(html.includes(value), value);
  }
  assert.match(html, /تغییر تخصیص/);
});

it('posts exact excess, reapproved run, and confirmed rollback payloads', async t => {
  const requests: unknown[] = [];
  t.mock.method(globalThis, 'fetch', async (url: string, options: RequestInit) => {
    requests.push([url, options.method, JSON.parse(String(options.body))]);
    return Response.json({});
  });
  await postBackfillDryRun();
  await postBackfillDryRun(['fixture'], 10);
  await postExcessDecision('fixture', 'physical_count_ordered');
  await postBackfillRun(['fixture'], 10, true, { fixture: 'reviewed' });
  await postBackfillRollback('fixture', undefined, true);
  await postBackfillRollback(undefined, true, true);
  assert.deepEqual(requests, [
    ['/api/v1/admin/spncx/backfill', 'POST', { dry_run: true }],
    ['/api/v1/admin/spncx/backfill', 'POST', { dry_run: true, product_ids: ['fixture'], batch_size: 10 }],
    ['/api/v1/admin/spncx/backfill/excess-decision', 'POST', { product_id: 'fixture', excess_decision: 'physical_count_ordered' }],
    ['/api/v1/admin/spncx/backfill', 'POST', { dry_run: false, product_ids: ['fixture'], batch_size: 10, confirm_plan_diff: true, reinclude: { fixture: 'reviewed' } }],
    ['/api/v1/admin/spncx/backfill/rollback', 'POST', { product_id: 'fixture', confirm_printed_destruction: true }],
    ['/api/v1/admin/spncx/backfill/rollback', 'POST', { all: true, confirm_printed_destruction: true }],
  ]);
});

it('renders excess decision evidence and the dry-run-first backend conflict', () => {
  const row = product('excess-product', { excess: 31 });
  const html = renderToStaticMarkup(<SpncxExcessDecisionRow product={row} decision={{
    product_id: row.product_id, excess_amount: 31, excess_decision: 'physical_count_ordered',
    excess_decided_by: 'fixture-actor', excess_decided_at: 'fixture-date', review_by: 'fixture-deadline',
  }} />);
  assert.match(html, /31/);
  assert.match(html, /fixture-actor/);
  assert.match(html, /fixture-deadline/);
  const conflict = renderToStaticMarkup(<SpncxExcessDecisionRow product={row} errorCode="no_excess_to_decide" />);
  assert.match(conflict, /اول یک dry-run اجرا کنید\./);
  assert.match(conflict, /<code dir="ltr"[^>]*>no_excess_to_decide<\/code>/);
});

it('renders durable exclusion, zero-allocation flips, excess and server-side target', () => {
  const status: BackfillStatus = {
    running: true, summary: { pending: 5, completed: 9, failed: 0, excluded: 1, total_units_minted: 31 },
    products: [{ product_id: 'status-fixture', status: 'excluded', units_minted: 3, units_target: 17,
      started_at: 'started-fixture', completed_at: null, error: 'status_error_code', excluded_reason: 'manual-veto',
      excluded_by: 'owner-id', excluded_at: 'exclusion-date', flipped_variants: ['default-zero'], excess_held: 7,
      excess_review_by: 'excess-deadline', excess_amount: 9, excess_decision: 'physical_count_ordered',
      excess_decided_by: 'decision-owner', excess_decided_at: 'decision-date' }],
  };
  const html = renderToStaticMarkup(<SpncxStatusView status={status} />);
  for (const value of ['excluded', 'manual-veto', 'owner-id', 'exclusion-date', 'default-zero', '3 / 17',
    'excess-deadline', 'decision-owner', 'decision-date', 'physical_count_ordered', 'status_error_code', '31']) {
    assert.ok(html.includes(value), value);
  }
});

it('pauses for explicit diff approval', () => {
  const html = renderToStaticMarkup(<SpncxRunDiff diff={diff} onApprove={() => {}} />);
  assert.match(html, /diff-product/);
  assert.match(html, /<button[^>]*>تأیید تفاوت و اجرای مجدد/);
});

it('shows printed labels for destruction and preserves rollback refusal codes', () => {
  const html = renderToStaticMarkup(<SpncxRollbackPanel printedCodes={['PRINTED-UNIT']} errorCode="rollback_refused"
    onCopy={() => {}} onConfirmPrinted={() => {}} result={{ product_id: 'restored', printed_codes_to_destroy: ['DESTROYED-UNIT'], rolled_back: true, restored_snapshot: true }} />);
  assert.match(html, /<code dir="ltr"[^>]*>PRINTED-UNIT<\/code>/);
  assert.match(html, /DESTROYED-UNIT/);
  assert.match(html, /کپی همه/);
  assert.match(html, /rollback_refused/);
  assert.match(html, /بازیابی شد/);
});

it('uses the server checklist and counters, ranks ten gaps, and has no strict or corrective action', () => {
  const html = renderToStaticMarkup(<SpncxCoverageSummary report={report} onRefresh={() => {}} />);
  for (const check of report.checks) assert.ok(html.includes(check.name), check.name);
  assert.match(html, /V6b[\s\S]*رد شد/);
  assert.match(html, /no physical-count sample recorded in v1/);
  assert.match(html, />97</);
  assert.match(html, /حالت سخت‌گیرانه: خاموش/);
  assert.ok(html.indexOf('gap-10') < html.indexOf('gap-1<'));
  assert.doesNotMatch(html, /least-gap|repai\x72|\u0631فع مغایرت|role="switch"|type="checkbox"/i);
  assert.equal((html.match(/<button/g) ?? []).length, 1);
});

it('replaces the vacuous V4 0/0 pass badge with a warning', () => {
  const zero = { total_units: 0, in_stock: 0, removed: 0, distinct_codes: 0, counters: 0 };
  const html = renderToStaticMarkup(<SpncxCoverageSummary report={{ ...report, checks: [
    { id: 'V4', name: 'global_totals', passed: true, totals: zero, expected: zero },
  ] }} />);
  assert.match(html, /هنوز هیچ محصولی تکمیل نشده است/);
  assert.match(html, /—/);
  assert.doesNotMatch(html, /قبول شد|NaN/);
});

it('renders historical check evidence, lot deadlines and per-order retry errors', () => {
  const html = renderToStaticMarkup(<SpncxReconciliationPanel report={report} onRetryCapture={() => {}} retryState={{
    'capture-order': { busy: false, done: false, errorCode: 'payment_not_retryable' },
  }} />);
  for (const value of ['واحدهای سریال‌دار', 'return:fixture', '###NO-MAPPING###', 'V12', 'V6b', 'gate-item',
    '2026-12-24', 'write_off_accepted', 'least-gap', 'capture-order', 'unit_coverage_gap',
    'capture-time', 'payment_not_retryable', 'تلاش مجدد ثبت سفارش']) assert.ok(html.includes(value), value);
});

it('mounts the unit lookup and exposes no corrective or strict-mode controls in the console', () => {
  const html = renderToStaticMarkup(<ToastProvider><AdminSpncxTab /></ToastProvider>);
  assert.match(html, /جست‌وجوی واحد/);
  assert.match(html, /حالت سخت‌گیرانه: خاموش/);
  assert.doesNotMatch(html, /repai\x72|\u0631فع مغایرت|role="switch"|type="checkbox"|فعال‌سازی حالت سخت‌گیرانه<\/button>/i);
  assert.match(html, /<button[^>]*disabled[^>]*>اجرای سریال‌گذاری/);
});

it('distinguishes an undeployed route from an unknown product and shows the locked-run code', () => {
  const notDeployed = new ApiError(404, 'خطا', '404 page not found');
  const unknownProduct = new ApiError(404, 'خطا', { error: 'product_not_found' });
  assert.equal(isSpncxUnavailable(notDeployed, extractSpncxErrorCode(notDeployed)), true);
  assert.equal(isSpncxUnavailable(unknownProduct, extractSpncxErrorCode(unknownProduct)), false);
  assert.equal(isSpncxUnavailable(new ApiError(501, 'خطا'), null), true);
  const html = renderToStaticMarkup(<SpncxError code="backfill_already_running" />);
  assert.match(html, /یک اجرای دیگر در حال انجام است\./);
  assert.match(html, /<code dir="ltr"[^>]*>backfill_already_running<\/code>/);
});

it('shows mixed rollback-all results including the per-product error from a 200', () => {
  const html = renderToStaticMarkup(<SpncxRollbackPanel result={{ all: true, results: [
    { product_id: 'rolled-back', rolled_back: true, printed_codes_to_destroy: ['PRINTED-FIXTURE'], restored_snapshot: true, error: null },
    { product_id: 'refused', rolled_back: false, printed_codes_to_destroy: [], restored_snapshot: false, error: 'rollback_refused' },
  ] }} />);
  assert.match(html, /PRINTED-FIXTURE/);
  assert.match(html, /rollback_refused/);
});
