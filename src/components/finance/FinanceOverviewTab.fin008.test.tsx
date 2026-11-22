import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { it } from 'node:test';
import assert from 'node:assert/strict';
import { ApiError } from '../../services/apiClient';
import { classifyTreasuryCashError, TreasuryCashCard, ProfitLocksCard, PendingSettlementsCard } from './FinanceOverviewTab';
import type { CashPositionDto, ProfitLocksDto, PendingSettlementDto } from '../../services/financeApi';
import { formatPrice, toPersianDigits } from '../../utils/formatters';

// FIN-008 gate G-18 (audit D-18): FinanceOverviewTab / DTO null, error and
// trend handling. Pure-component rendering tests via renderToStaticMarkup,
// following the repo's existing tsx --test pattern (AdminOperationsTab.test.tsx).
// Run: npx tsx --test src/components/finance/FinanceOverviewTab.fin008.test.tsx
// (or: npm test — scripts/test-frontend.mjs runs every src/**/*.test.ts(x)).
// All money is integer Toman.

const baseCash = (patch: Partial<CashPositionDto> = {}): CashPositionDto => ({
  success: true,
  days: 30,
  allTimeIn: 50000000,
  allTimeOut: 0,
  overObligated: false,
  overObligatedAmount: 0,
  liquidCash: 20000000,
  safeToSpend: 10000000,
  cashStatus: 'verified',
  reconciliationRequired: false,
  unresolvedProviderCash: 0,
  liability: 10000000,
  recoveryReceivableStores: 0,
  recoveryReceivableSuppliers: 0,
  walletLiability: 0,
  refundLiability: 0,
  holds: 0,
  period: {
    in: 1000000,
    out: 0,
    trend: [
      { date: '2026-11-16', in: 1000000, out: 0, available: true },
      { date: '2026-11-17', in: 2000000, out: 500000, available: true },
      { date: '2026-11-18', in: 1500000, out: 250000, available: true },
    ],
  },
  heldOrderCount: 0,
  heldInOrders: 0,
  owedToStores: 10000000,
  owedToSuppliers: 0,
  pendingRefundCount: 0,
  pendingRefunds: 0,
  pendingCount: 0,
  pendingPayouts: 0,
  platformEarned: 0,
  sellerInFlight: 0,
  supplierInFlight: 0,
  ...patch,
});

const cashCard = (cash: CashPositionDto | null, extra: { cashLoaded?: boolean; cashError?: ReturnType<typeof classifyTreasuryCashError> } = {}) => renderToStaticMarkup(
  <TreasuryCashCard
    cash={cash}
    cashLoaded={extra.cashLoaded ?? true}
    cashError={extra.cashError ?? null}
    cashDays={30}
    onCashDaysChange={() => {}}
    formatPrice={formatPrice}
    toPersianDigits={toPersianDigits}
  />,
);

const locksCard = (locks: ProfitLocksDto | null, locksError = false) => renderToStaticMarkup(
  <ProfitLocksCard
    locks={locks}
    locksError={locksError}
    onNavigateTab={() => {}}
    formatPrice={formatPrice}
    toPersianDigits={toPersianDigits}
  />,
);

const pendingCard = (pending: PendingSettlementDto[], pendingError = false, pendingLoaded = true) => renderToStaticMarkup(
  <PendingSettlementsCard
    pending={pending}
    pendingError={pendingError}
    pendingLoaded={pendingLoaded}
    formatPrice={formatPrice}
    toPersianDigits={toPersianDigits}
  />,
);

it('TestFinanceOverviewUnknownCashRendersReconciliation', () => {
  const html = cashCard(baseCash({
    safeToSpend: null,
    liquidCash: null,
    cashStatus: 'unknown',
    reconciliationRequired: true,
    unresolvedProviderCash: 50000000,
    allTimeIn: 50000000,
  }));
  assert.match(html, /نامشخص/);
  assert.match(html, /نیازمند تطبیق/);
  assert.ok(html.includes(formatPrice(50000000)), 'unresolved 50,000,000 Toman must be shown');
  assert.doesNotMatch(html, /هنوز هیچ سفارش پرداخت‌شده‌ای/, 'unknown cash must not render the no-orders empty state');
  assert.doesNotMatch(html, /text-xl font-extrabold/, 'unknown cash must not render safeToSpend as a number');
});

it('TestFinanceOverviewOverObligatedShowsMagnitude', () => {
  const html = cashCard(baseCash({
    safeToSpend: 0,
    liquidCash: 8000000,
    overObligated: true,
    overObligatedAmount: -12000000,
  }));
  assert.match(html, /تعهدات بیش از نقدینگی!/);
  assert.match(html, /کسری نقدینگی/);
  assert.ok(html.includes(formatPrice(12000000)), 'shortfall magnitude 12,000,000 Toman must be shown');
  assert.ok(html.includes(formatPrice(0)), 'safeToSpend 0 must still be shown');
});

it('TestFinanceOverviewLocksFailureShowsError', () => {
  const html = locksCard(null, true);
  assert.match(html, /سود دراپ‌شیپرها/, 'the locks card must not be silently hidden on failure');
  assert.match(html, /خطا در بارگذاری سودهای قفل‌شده/);
  assert.match(html, /role="alert"/);
  assert.doesNotMatch(html, /قفل‌شده \(مهلت مرجوعی\)/);

  const ok = locksCard({ success: true, totals: { locked: 3000000, lockedCount: 2, releasable: 1000000, releasableCount: 1 } }, false);
  assert.ok(ok.includes(formatPrice(3000000)), 'locks render their real amounts when loaded');
});

it('TestFinanceOverviewPendingFailureShowsError', () => {
  const html = pendingCard([], true);
  assert.match(html, /خطا در بارگذاری/);
  assert.match(html, /صفر فرض نشده است/);
  assert.doesNotMatch(html, /۰ درخواست شبا/, 'a failed pending load must not render as 0 requests');

  const ok = pendingCard([{ amount: 2000000 }, { amount: 1000000 }], false, true);
  assert.ok(ok.includes(formatPrice(3000000)), 'pending total 3,000,000 Toman must be shown when loaded');
  assert.match(ok, /۲ درخواست شبا/);
});

it('TestFinanceOverviewTrendGapIsNotZero', () => {
  const html = cashCard(baseCash({
    period: {
      in: 3000000,
      out: 3000000,
      trend: [
        { date: '2026-11-16', in: null, out: 5, available: false },
        { date: '2026-11-17', in: 1000000, out: 2000000, available: true },
        { date: '2026-11-18', in: 2000000, out: 1000000, available: true },
      ],
    },
  }));
  assert.match(html, /—/, 'a missing trend day renders as a gap');
  assert.match(html, /صفر فرض نشده است/, 'a missing trend day is never zero-filled');
  assert.match(html, /۱ روز در دسترس نیست/);
  // The unavailable day's `out: 5` must not leak into the out sparkline as a
  // lone point: with the gap excluded, both sparklines still draw from the
  // two available days only (no zero point is ever synthesised).
  assert.doesNotMatch(html, /روند روزانه در دسترس نیست/);
});

it('TestFinanceOverviewEmptyStateOnlyWhenCashKnownAndNoOrders', () => {
  // F-06: the empty state is ONLY for a genuinely empty position —
  // the old test pinned it with liability 10,000,000 still on the
  // books, which hid real liabilities behind "no orders yet".
  const html = cashCard(baseCash({
    allTimeIn: 0,
    allTimeOut: 0,
    liquidCash: 0,
    safeToSpend: 0,
    cashStatus: 'verified',
    liability: 0,
    owedToStores: 0,
    period: { in: 0, out: 0, trend: [] },
  }));
  assert.match(html, /هنوز هیچ سفارش پرداخت‌شده‌ای/);

  const withOrders = cashCard(baseCash({ allTimeIn: 1000000, allTimeOut: 0 }));
  assert.doesNotMatch(withOrders, /هنوز هیچ سفارش پرداخت‌شده‌ای/);
});

it('TestFinanceOverviewOpeningCashOnlyRendersNumbersNotEmptyState', () => {
  // F-06 (G-15's own fixture): verified opening cash 7,000,000, no
  // orders at all — the tab must show the 7,000,000, not "no paid
  // orders yet".
  const html = cashCard(baseCash({
    allTimeIn: 0,
    allTimeOut: 0,
    liquidCash: 7000000,
    safeToSpend: 7000000,
    cashStatus: 'verified',
    liability: 0,
    owedToStores: 0,
    period: { in: 0, out: 0, trend: [] },
  }));
  assert.doesNotMatch(html, /هنوز هیچ سفارش پرداخت‌شده‌ای/);
  assert.ok(html.includes(formatPrice(7000000)), 'opening cash 7,000,000 Toman must be shown');
});

it('TestFinanceOverviewAdjustmentLiabilitiesRenderNumbersNotEmptyState', () => {
  // F-06 (G-17's own fixture shape): adjustment-created liabilities
  // 40,000,000, no orders, zero verified cash — the over-obligation
  // must be shown, never hidden behind the empty state.
  const html = cashCard(baseCash({
    allTimeIn: 0,
    allTimeOut: 0,
    liquidCash: 0,
    safeToSpend: 0,
    cashStatus: 'verified',
    liability: 40000000,
    owedToStores: 40000000,
    overObligated: true,
    overObligatedAmount: -40000000,
    period: { in: 0, out: 0, trend: [] },
  }));
  assert.doesNotMatch(html, /هنوز هیچ سفارش پرداخت‌شده‌ای/);
  assert.ok(html.includes(formatPrice(40000000)), 'liability 40,000,000 Toman must be shown');
});

it('TestFinanceOverviewRefundLiabilityLabelCoversGatewayAndWallet', () => {
  // F-07: the row renders refundLiability (gateway + wallet), so the
  // label must say so — never gateway-only.
  const html = cashCard(baseCash({ refundLiability: 3000000 }));
  assert.match(html, /بازپرداخت‌های در انتظار \(درگاه و کیف پول\)/);
  assert.ok(html.includes(formatPrice(3000000)), 'refund liability 3,000,000 Toman must be shown');
});

it('distinguishes a principal conflict, legacy paid reconciliation and generic failures without figures', () => {
  const cases = [
    [new ApiError(409, 'unresolved', { reconciliationRequired: true }), 'مبدأ مالی یک یا چند سفارش مشخص نیست'],
    [new ApiError(500, 'failed', { error: "treasury: 2 payout_requests row(s) with undefined status 'paid'; reconciliation required", reconciliationRequired: true }), 'نیازمند بازطبقه‌بندی‌اند'],
    [new ApiError(500, 'failed', { error: 'خطای داخلی سرور' }), 'خطا در بارگذاری موقعیت نقدینگی.'],
    [new Error('network'), 'خطا در بارگذاری موقعیت نقدینگی.'],
  ] as const;
  const messages = new Set(cases.map(([, message]) => message));
  for (const [error, expected] of cases) {
    const markup = cashCard(baseCash(), { cashError: classifyTreasuryCashError(error) });
    assert.ok(markup.includes(expected));
    for (const message of messages) {
      if (message !== expected) assert.ok(!markup.includes(message));
    }
    assert.ok(!markup.includes('نقدینگی تأییدشده (بانک/درگاه)'));
  }
});
