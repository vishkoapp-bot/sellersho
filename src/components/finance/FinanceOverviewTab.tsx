import React, { useState, useEffect } from 'react';
import { useStore } from '../../context/StoreContext';
import { FinanceLedgerDetailModal } from './FinanceLedgerDetailModal';
import { Sparkline } from './Sparkline';
import { ApiError } from '../../services/apiClient';
import { financeApi, recentLedgerWindow, LedgerEntryDto, CashPositionDto, ProfitLocksDto, PendingSettlementDto } from '../../services/financeApi';
import { BadgePercent, DollarSign, ArrowDownToLine, ShieldCheck, CheckCircle2, FileText, AlertCircle } from 'lucide-react';

interface Props {
  onNavigateTab: (tab: string) => void;
}

// Live spec (AdminDashboardView-MERGED.js, Nov 2 18:38 build): this tab
// renders ONLY from server data — GET /finance/ledger (paged),
// GET /finance/treasury/cash-position?days= (success-wrapped),
// GET /finance/settlements/pending ({settlements}) and
// GET /finance/treasury/profit-locks (success-wrapped). The previous
// version derived its figures from client orders plus hard-coded
// constants and never called the ledger endpoint, so the ledger rows
// could never appear here.
//
// FIN-008 (D-18): the cash card is a pure component (TreasuryCashCard)
// so its null/error/trend rendering is testable without a browser.
// Unknown cash (safeToSpend/liquidCash null) renders an explicit
// "unknown — needs reconciliation" panel, never 0 and never the
// "no paid orders yet" empty state. Failed loads render error states,
// never silent zero-fill. Trend days with available:false render as a
// gap ("—"), never as 0.

type TreasuryCashError = 'paid-500' | 'principal-409' | 'generic';

export function classifyTreasuryCashError(error: unknown): TreasuryCashError {
  if (error instanceof ApiError) {
    if (error.status === 409) return 'principal-409';
    const { error: message, reconciliationRequired } = error.body ?? {};
    if (error.status === 500 && reconciliationRequired === true &&
        typeof message === 'string' && message.includes("undefined status 'paid'")) {
      return 'paid-500';
    }
  }
  return 'generic';
}

const cashErrorMessages: Record<TreasuryCashError, string> = {
  'principal-409': 'تطبیق مالی لازم است: مبدأ مالی یک یا چند سفارش مشخص نیست. مبلغی نمایش داده نمی‌شود.',
  'paid-500': 'سفارش‌های پرداخت قدیمی با وضعیت نامشخص («paid») نیازمند بازطبقه‌بندی‌اند؛ تا تصمیم مالک، هیچ عددی نمایش داده نمی‌شود.',
  generic: 'خطا در بارگذاری موقعیت نقدینگی.',
};

export interface TreasuryCashCardProps {
  cash: CashPositionDto | null;
  cashLoaded: boolean;
  cashError: TreasuryCashError | null;
  cashDays: number;
  onCashDaysChange: (days: number) => void;
  formatPrice: (n: number) => string;
  toPersianDigits: (v: string | number) => string;
}

/**
 * Pure treasury cash-position card. All money is integer Toman.
 * Read path: GET /finance/treasury/cash-position?days=N -> CashPositionDto.
 */
export const TreasuryCashCard: React.FC<TreasuryCashCardProps> = ({ cash, cashLoaded, cashError, cashDays, onCashDaysChange, formatPrice, toPersianDigits }) => {
  // Trend: only days with a real computed value reach the sparkline.
  // A day with available:false (or a null in/out) is a gap, never 0.
  const trend = cash?.period.trend ?? [];
  const trendAvailable = !!cash && cash.period.trendAvailable !== false;
  const trendIn = trend.filter((t) => t.available !== false && typeof t.in === 'number').map((t) => t.in as number);
  const trendOut = trend.filter((t) => t.available !== false && typeof t.out === 'number').map((t) => t.out as number);
  const trendGapCount = trend.filter((t) => t.available === false || t.in == null || t.out == null).length;
  const moneyOrDash = (v: number | null | undefined) => (typeof v === 'number' ? `${formatPrice(v)} تومان` : '—');

  const renderDetails = (c: CashPositionDto) => (
    <>
      <div className="grid grid-cols-2 gap-2 px-4">
        <div className="bg-emerald-50/60 border border-emerald-100 rounded-xl p-2.5">
          <div className="flex items-center gap-1 text-emerald-700">
            <span className="text-[10px] font-bold">ورودی {toPersianDigits(cashDays)} روز</span>
          </div>
          <div className="text-sm font-extrabold text-slate-900 mt-0.5">{formatPrice(c.period.in)} <span className="text-[9px] font-normal text-slate-500">تومان</span></div>
          {trendAvailable && <Sparkline data={trendIn} color="#059669" height={38} ariaLabel="روند ورودی نقدینگی" />}
        </div>
        <div className="bg-rose-50/60 border border-rose-100 rounded-xl p-2.5">
          <div className="flex items-center gap-1 text-rose-700">
            <span className="text-[10px] font-bold">خروجی {toPersianDigits(cashDays)} روز</span>
          </div>
          <div className="text-sm font-extrabold text-slate-900 mt-0.5">{formatPrice(c.period.out)} <span className="text-[9px] font-normal text-slate-500">تومان</span></div>
          {trendAvailable && <Sparkline data={trendOut} color="#e11d48" height={38} ariaLabel="روند خروجی نقدینگی" />}
        </div>
      </div>
      {!trendAvailable ? (
        <p className="px-4 pt-1.5 text-[10px] text-slate-500 leading-relaxed">روند روزانه در دسترس نیست؛ صفر فرض نشده است.</p>
      ) : trendGapCount > 0 ? (
        <p className="px-4 pt-1.5 text-[10px] text-amber-600 leading-relaxed">روند {toPersianDigits(trendGapCount)} روز در دسترس نیست و با «—» نمایش داده می‌شود؛ صفر فرض نشده است.</p>
      ) : null}
      <div className="p-4 pt-2.5 space-y-1.5 text-[11px]">
        <div className="flex justify-between"><span className="text-slate-500">نقدینگی تأییدشده (بانک/درگاه)</span><strong className="text-slate-700">{c.liquidCash == null ? 'نامشخص — نیازمند تطبیق' : `${formatPrice(c.liquidCash)} تومان`}</strong></div>
        <div className="flex justify-between"><span className="text-slate-500">بدهی کل (سود آزادشده + تعهدات در صف)</span><strong className="text-slate-700">{moneyOrDash(c.liability)}</strong></div>
        <div className="flex justify-between"><span className="text-slate-500">بلوکه در سفارش‌ها — فقط سود آزادنشده فروشنده ({toPersianDigits(c.heldOrderCount)} سفارش پرداخت‌شده آزادنشده)</span><strong className="text-slate-700">{formatPrice(c.heldInOrders)} تومان</strong></div>
        <div className="flex justify-between"><span className="text-slate-500">بدهی به دراپ‌شیپرها (سود آزادشده)</span><strong className="text-slate-700">{formatPrice(c.owedToStores)} تومان</strong></div>
        <div className="flex justify-between"><span className="text-slate-500">بدهی به تأمین‌کنندگان (سهم عمده‌فروشی آزادشده)</span><strong className="text-slate-700">{formatPrice(c.owedToSuppliers)} تومان</strong></div>
        <div className="flex justify-between"><span className="text-slate-500">موجودی کیف پول مشتریان</span><strong className="text-slate-700">{moneyOrDash(c.walletLiability)}</strong></div>
        <div className="flex justify-between"><span className="text-slate-500">بازپرداخت‌های در انتظار (درگاه و کیف پول)</span><strong className="text-amber-700">{moneyOrDash(c.refundLiability)}</strong></div>
        <div className="flex justify-between"><span className="text-slate-500">بازپرداخت‌های در انتظار مشتری ({toPersianDigits(typeof c.pendingRefundCount === 'number' ? c.pendingRefundCount : '—')} مورد — زیرمجموعه موجودی کیف پول، جمع نمی‌شود)</span><strong className="text-amber-700">{moneyOrDash(c.pendingRefunds)}</strong></div>
        <div className="flex justify-between"><span className="text-slate-500">در صف تسویه شبا ({toPersianDigits(c.pendingCount)} درخواست)</span><strong className="text-amber-700">{formatPrice(c.pendingPayouts)} تومان</strong></div>
        <div className="flex justify-between"><span className="text-slate-500">تعهدات در صف تسویه (در انتظار/تأییدشده/در حال پردازش)</span><strong className="text-amber-700">{moneyOrDash(c.holds)}</strong></div>
        {(c.recoveryReceivableStores || 0) > 0 && (
          <div className="flex justify-between"><span className="text-slate-500">بازیافتی از فروشگاه‌ها (مانده منفی؛ پول نقد نیست)</span><strong className="text-slate-700">{formatPrice(c.recoveryReceivableStores)} تومان</strong></div>
        )}
        {(c.recoveryReceivableSuppliers || 0) > 0 && (
          <div className="flex justify-between"><span className="text-slate-500">بازیافتی از تأمین‌کنندگان (مانده منفی؛ پول نقد نیست)</span><strong className="text-slate-700">{formatPrice(c.recoveryReceivableSuppliers)} تومان</strong></div>
        )}
        {(c.unresolvedProviderCash || 0) > 0 && (
          <div className="flex justify-between"><span className="text-slate-500">مبلغ تطبیق‌نشده درگاه/بانک (پول نقد نیست)</span><strong className="text-amber-700">{formatPrice(c.unresolvedProviderCash)} تومان</strong></div>
        )}
        <div className="flex justify-between"><span className="text-slate-500">سهم ثبت‌شده پلتفرم روی سفارش‌های پرداخت‌شده (پیش از تطبیق)</span><strong className="text-slate-700">{formatPrice(c.platformEarned)} تومان</strong></div>
        {c.note && (
          <p className="text-[9px] text-slate-400 leading-relaxed pt-1">{c.note}</p>
        )}
        {(c.sellerInFlight || 0) + (c.supplierInFlight || 0) > 0 && (
          <p className="text-[9px] text-slate-400 leading-relaxed">از بدهی‌های بالا، {formatPrice((c.sellerInFlight || 0) + (c.supplierInFlight || 0))} تومان تأیید شده اما هنوز منتقل نشده است (در مسیر پایا) — فقط «تکمیل‌شده» یعنی پول خارج‌شده.</p>
        )}
      </div>
    </>
  );

  let body: React.ReactNode;
  if (cashError) {
    body = (
      <div role="alert" className="p-3.5 text-[11px] text-rose-600">
        {cashError === 'principal-409' && <AlertCircle aria-hidden="true" className="inline w-3.5 h-3.5 ml-1" />}
        {cashErrorMessages[cashError]}
      </div>
    );
  } else if (!cashLoaded) {
    body = <div className="p-3.5 text-[11px] text-slate-400">در حال محاسبه از داده‌های واقعی…</div>;
  } else if (!cash) {
    body = <div className="p-3.5 text-[11px] text-rose-600">خطا در بارگذاری موقعیت نقدینگی؛ هیچ عددی حدس زده نمی‌شود.</div>;
  } else if (cash.safeToSpend == null) {
    // Unknown cash: explicit reconciliation panel — never 0, never a
    // guessed number, and never the "no paid orders yet" empty state.
    body = (
      <>
        <div className="mx-4 mb-2.5 mt-2.5 rounded-xl p-3 bg-amber-50 border border-amber-200">
          <div className="text-[11px] font-bold text-amber-800">مبلغ قابل خرج‌کرد امن: نامشخص — نیازمند تطبیق</div>
          <p className="text-[10px] text-amber-700 mt-1 leading-relaxed">نقدینگی تأییدشده بانک/درگاه هنوز تطبیق نشده است؛ هیچ عددی حدس زده نمی‌شود و مبلغ قابل خرج‌کرد امن صفر فرض نشده است.</p>
          <div className="mt-2 space-y-1 text-[11px]">
            <div className="flex justify-between"><span className="text-amber-700">نقدینگی تأییدشده</span><strong className="text-amber-800">نامشخص</strong></div>
            <div className="flex justify-between"><span className="text-amber-700">مبلغ تطبیق‌نشده (ورودی پرداخت‌شده بدون سند تسویه)</span><strong className="text-amber-800">{moneyOrDash(cash.unresolvedProviderCash)}</strong></div>
            <div className="flex justify-between"><span className="text-amber-700">ورودی کل ثبت‌شده (فقط برای اطلاع؛ پول نقد نیست)</span><strong className="text-amber-800">{formatPrice(cash.allTimeIn)} تومان</strong></div>
          </div>
        </div>
        {renderDetails(cash)}
      </>
    );
  } else if (cash.allTimeIn === 0 && cash.allTimeOut === 0 && (cash.liquidCash ?? 0) === 0 && cash.liability === 0 && !cash.overObligated) {
    // F-06: the "no paid orders yet" empty state fires ONLY for a
    // genuinely empty position — cash known (safeToSpend numeric,
    // guaranteed by the branch order above), no orders, no verified
    // opening cash (liquidCash 0 with no orders means no opening
    // cash), and no liabilities at all. Verified opening cash alone
    // or adjustment-created liabilities must render their numbers,
    // never this text.
    body = (
      <div className="p-3.5 text-[11px] text-slate-500 leading-relaxed">
        <span className="font-bold text-slate-600 block mb-1">—</span>
        هنوز هیچ سفارش پرداخت‌شده‌ای در سیستم ثبت نشده است؛ پس از اولین پرداخت قطعی، ورودی/خروجی و مبلغ قابل خرج‌کرد امن اینجا محاسبه می‌شود.
      </div>
    );
  } else {
    body = (
      <>
        <div className={`mx-4 mb-2.5 mt-2.5 rounded-xl p-3 text-white ${cash.overObligated ? 'bg-gradient-to-l from-rose-700 via-rose-800 to-rose-900' : 'bg-gradient-to-l from-slate-900 via-teal-950 to-slate-900'}`}>
          <div className="flex items-center justify-between">
            <span className="text-[11px] font-bold opacity-90 flex items-center">مبلغ قابل خرج‌کرد امن</span>
            {cash.overObligated && (
              <span className="text-[9px] bg-white/20 border border-white/30 px-1.5 py-0.5 rounded-full font-bold">تعهدات بیش از نقدینگی!</span>
            )}
          </div>
          <div className="text-xl font-extrabold mt-1" dir="rtl">{formatPrice(cash.safeToSpend)} <span className="text-[10px] font-normal opacity-80">تومان</span></div>
          {cash.overObligated && typeof cash.overObligatedAmount === 'number' && (
            <div className="text-[11px] font-bold mt-1">کسری نقدینگی: {formatPrice(Math.abs(cash.overObligatedAmount))} تومان</div>
          )}
          <p className="text-[9px] opacity-70 mt-1 leading-relaxed">مبلغ قابل خرج‌کرد امن = نقدینگی تأییدشده منهای بدهی‌ها (سود آزادشده فروشگاه‌ها و تأمین‌کننده‌ها، موجودی کیف پول مشتریان، بازپرداخت‌های در انتظار و تسویه‌های در صف). سود آزادنشده فقط سود فروشنده است، نه کل مبلغ سفارش؛ مبلغ تطبیق‌نشده پول نقد نیست.</p>
        </div>
        {renderDetails(cash)}
      </>
    );
  }

  return (
    <div className="bg-white rounded-2xl border border-slate-200/90 shadow-2xs overflow-hidden">
      <div className="p-4 pb-2 flex items-center justify-between">
        <div className="flex items-center gap-1.5 text-teal-700">
          <DollarSign className="w-4 h-4" strokeWidth={1.5} />
          <h3 className="text-xs font-bold text-slate-800">موقعیت نقدینگی خزانه</h3>
        </div>
        <div className="flex bg-slate-100 p-0.5 rounded-lg text-[10px] font-bold gap-0.5">
          {[7, 30].map((d) => (
            <button
              key={d}
              type="button"
              onClick={() => onCashDaysChange(d)}
              className={`px-2 py-0.5 rounded-md transition-all ${cashDays === d ? 'bg-white text-teal-700 shadow-2xs' : 'text-slate-500'}`}
            >
              {toPersianDigits(d)} روزه
            </button>
          ))}
        </div>
      </div>
      <p className="px-4 text-[10px] text-slate-400 leading-relaxed">یعنی همین حالا چقدر پول واقعاً دست پلتفرم است و چقدر از آن را بدهکار دیگرانیم. عدد مهم این کارت، «مبلغ قابل خرج‌کرد امن» است، نه ورودی کل.</p>
      {body}
    </div>
  );
};

export interface ProfitLocksCardProps {
  locks: ProfitLocksDto | null;
  locksError: boolean;
  onNavigateTab: (tab: string) => void;
  formatPrice: (n: number) => string;
  toPersianDigits: (v: string | number) => string;
}

/** Pure profit-locks card. A failed load renders an error state — the card is never silently hidden (FIN-008 D-18). */
export const ProfitLocksCard: React.FC<ProfitLocksCardProps> = ({ locks, locksError, onNavigateTab, formatPrice, toPersianDigits }) => {
  if (locksError || (locks && locks.available === false)) {
    return (
      <div className="bg-white rounded-2xl border border-rose-200/90 shadow-2xs p-4 space-y-2">
        <div className="flex items-center gap-1.5 text-rose-700">
          <DollarSign className="w-4 h-4" strokeWidth={1.5} />
          <h3 className="text-xs font-bold text-slate-800">سود دراپ‌شیپرها: قفل‌شده در برابر قابل آزادسازی</h3>
        </div>
        <p className="text-[11px] text-rose-600 leading-relaxed" role="alert">خطا در بارگذاری سودهای قفل‌شده؛ کارت پنهان نشده و هیچ عددی حدس زده نمی‌شود.</p>
      </div>
    );
  }
  if (!locks || (locks.totals.locked <= 0 && locks.totals.releasable <= 0)) return null;
  return (
    <div className="bg-white rounded-2xl border border-slate-200/90 shadow-2xs p-4 space-y-2">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1.5 text-teal-700">
          <DollarSign className="w-4 h-4" strokeWidth={1.5} />
          <h3 className="text-xs font-bold text-slate-800">سود دراپ‌شیپرها: قفل‌شده در برابر قابل آزادسازی</h3>
        </div>
        <button type="button" onClick={() => onNavigateTab('payouts')} className="text-[10px] font-bold text-teal-700 hover:underline">جزئیات در صف تسویه</button>
      </div>
      <div className="grid grid-cols-2 gap-2">
        <div className="bg-slate-50 border border-slate-200 rounded-xl p-2.5">
          <span className="text-[10px] font-bold text-slate-500 block">قفل‌شده (مهلت مرجوعی)</span>
          <div className="text-sm font-extrabold text-slate-800 mt-0.5">{formatPrice(locks.totals.locked)} <span className="text-[9px] font-normal text-slate-500">تومان</span></div>
          <span className="text-[9px] text-slate-400">{toPersianDigits(locks.totals.lockedCount)} سفارش</span>
        </div>
        <div className="bg-emerald-50/70 border border-emerald-200 rounded-xl p-2.5">
          <span className="text-[10px] font-bold text-emerald-700 block">آماده آزادسازی</span>
          <div className="text-sm font-extrabold text-emerald-700 mt-0.5">{formatPrice(locks.totals.releasable)} <span className="text-[9px] font-normal text-slate-500">تومان</span></div>
          <span className="text-[9px] text-emerald-600">{toPersianDigits(locks.totals.releasableCount)} سفارش</span>
        </div>
      </div>
      {locks.rule && (
        <p className="text-[9px] text-slate-400 leading-relaxed">{locks.rule}</p>
      )}
    </div>
  );
};

export interface PendingSettlementsCardProps {
  pending: PendingSettlementDto[];
  pendingError: boolean;
  pendingLoaded: boolean;
  formatPrice: (n: number) => string;
  toPersianDigits: (v: string | number) => string;
}

/** Pure pending-settlements summary card. A failed load renders an error state — never "0 requests" (FIN-008 D-18). */
export const PendingSettlementsCard: React.FC<PendingSettlementsCardProps> = ({ pending, pendingError, pendingLoaded, formatPrice, toPersianDigits }) => {
  const pendingSum = pending.reduce((sum, p) => sum + (Number(p.amount) || 0), 0);
  return (
    <div className="bg-white rounded-2xl p-4 border border-slate-200/90 shadow-2xs">
      <div className="flex items-center justify-between mb-1">
        <span className="text-[11px] font-bold text-slate-700">در صف تسویه</span>
        <ArrowDownToLine className="w-3.5 h-3.5 text-amber-600" strokeWidth={1.5} />
      </div>
      {pendingError ? (
        <>
          <div className="text-sm font-extrabold text-rose-600">خطا در بارگذاری</div>
          <span className="text-[9px] text-rose-600 font-medium block mt-1">تسویه‌های در صف بارگذاری نشد؛ صفر فرض نشده است.</span>
        </>
      ) : !pendingLoaded ? (
        <>
          <div className="text-sm font-extrabold text-slate-900">—</div>
          <span className="text-[9px] text-slate-400 font-medium block mt-1">در حال بارگذاری…</span>
        </>
      ) : (
        <>
          <div className="text-sm font-extrabold text-slate-900">
            {formatPrice(pendingSum)} <span className="text-[9px] font-normal text-slate-500">تومان</span>
          </div>
          <span className="text-[9px] text-amber-600 font-medium block mt-1">
            {toPersianDigits(pending.length)} درخواست شبا
          </span>
        </>
      )}
    </div>
  );
};

export const FinanceOverviewTab: React.FC<Props> = ({ onNavigateTab }) => {
  const { formatPrice, toPersianDigits } = useStore();
  const [showLedgerModal, setShowLedgerModal] = useState(false);

  const [ledger, setLedger] = useState<LedgerEntryDto[]>([]);
  const [ledgerLoaded, setLedgerLoaded] = useState(false);
  const [ledgerError, setLedgerError] = useState(false);

  const [cashDays, setCashDays] = useState(30);
  const [cashResponse, setCashResponse] = useState<{
    days: number;
    cash: CashPositionDto | null;
    error: TreasuryCashError | null;
  } | null>(null);
  const currentCash = cashResponse?.days === cashDays ? cashResponse : null;

  const [pending, setPending] = useState<PendingSettlementDto[]>([]);
  const [pendingLoaded, setPendingLoaded] = useState(false);
  const [pendingError, setPendingError] = useState(false);
  const [locks, setLocks] = useState<ProfitLocksDto | null>(null);
  const [locksError, setLocksError] = useState(false);

  useEffect(() => {
    let cancelled = false;
    financeApi.getCashPosition(cashDays)
      .then((res) => {
        if (cancelled) return;
        setCashResponse(res?.success === true
          ? { days: cashDays, cash: res, error: null }
          : { days: cashDays, cash: null, error: 'generic' });
      })
      .catch((error: unknown) => {
        if (!cancelled) setCashResponse({ days: cashDays, cash: null, error: classifyTreasuryCashError(error) });
      });
    return () => { cancelled = true; };
  }, [cashDays]);

  useEffect(() => {
    financeApi.getLedger(recentLedgerWindow())
      .then(({ entries }) => { setLedger(entries); setLedgerLoaded(true); })
      .catch(() => { setLedgerError(true); setLedgerLoaded(true); });
    financeApi.getPendingSettlements()
      .then((data) => { setPending(data); setPendingLoaded(true); })
      .catch(() => { setPendingError(true); setPendingLoaded(true); });
    financeApi.getProfitLocks()
      .then((res) => {
        if (res && res.success) { setLocks(res); }
        else { setLocksError(true); }
      })
      .catch(() => setLocksError(true));
  }, []);

  const sumWhere = (pred: (e: LedgerEntryDto) => boolean) =>
    ledger.filter(pred).reduce((sum, e) => sum + e.amount, 0);
  const shaparakIn = sumWhere((e) => e.type === 'debit' && e.targetAccount === 'shaparak_clearing');
  const shaparakOut = sumWhere((e) => e.type === 'credit' && e.targetAccount === 'shaparak_clearing');
  const totalDebit = sumWhere((e) => e.type === 'debit');
  const totalCredit = sumWhere((e) => e.type === 'credit');
  const imbalance = totalDebit - totalCredit;
  const balanced = ledgerLoaded && !ledgerError && ledger.length > 0 && imbalance === 0;
  const ledgerValue = (v: number) => {
    if (!ledgerLoaded) return '—';
    if (ledgerError) return 'خطا در بارگذاری';
    if (ledger.length === 0) return 'داده‌ای ثبت نشده';
    return formatPrice(v);
  };

  return (
    <div className="space-y-3 pb-6">
      {/* Live help explainer */}
      <div className="bg-white rounded-2xl border border-slate-200/90 shadow-2xs p-3.5 space-y-1">
        <h3 className="text-xs font-bold text-slate-800">تراز دفتر کل یعنی چه؟</h3>
        <p className="text-[11px] text-slate-600 leading-relaxed">اینجا تصویر کلی پول پلتفرم را می‌بینی: چقدر پول وارد شده، چقدر به فروشنده‌ها و تأمین‌کننده‌ها بدهکاریم و چقدر از پول را می‌توانیم با خیال راحت خرج کنیم. حواست بیشتر به «مبلغ قابل خرج‌کرد امن» باشد.</p>
        <p className="text-[10px] text-slate-500 leading-relaxed">در حسابداری دوبل، جمع بدهکارها و بستانکارها باید دقیقاً برابر باشد. اختلاف نمایش‌داده‌شده فقط بر اساس اسناد همین صفحه محاسبه می‌شود.</p>
      </div>

      {/* 1. Header Card */}
      <div className="bg-gradient-to-l from-slate-900 via-teal-950 to-slate-900 rounded-2xl p-3.5 text-white shadow-xs">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <div className="w-9 h-9 rounded-xl bg-teal-500/20 border border-teal-400/30 flex items-center justify-center text-teal-400">
              <BadgePercent className="w-5 h-5" strokeWidth={1.5} />
            </div>
            <div>
              <h2 className="text-xs font-bold flex items-center gap-1.5">
                <span>دفتر مالی کل و تطبیق اسناد</span>
                {balanced && (
                  <span className="text-[9px] bg-emerald-500/20 text-emerald-300 border border-emerald-500/30 px-1.5 py-0.2 rounded-full font-bold">
                    تراز این صفحه متوازن است
                  </span>
                )}
              </h2>
              <span className="text-[10px] text-slate-400 block mt-0.5">
                بر اساس حداکثر ۱۰۰ سند از ۳۱ روز اخیر
              </span>
            </div>
          </div>
        </div>

        <div className="mt-2.5 pt-2 border-t border-white/10 flex items-center justify-between text-[10px] text-slate-300">
          {ledgerError ? (
            <span className="text-rose-300">خطا در بارگذاری دفتر کل</span>
          ) : !ledgerLoaded ? (
            <span>در حال بارگذاری دفتر کل…</span>
          ) : ledger.length === 0 ? (
            <span>داده‌ای ثبت نشده</span>
          ) : (
            <>
              <span>اسناد این صفحه: <strong className="text-emerald-400">{toPersianDigits(ledger.length)} سند</strong></span>
              <span>اختلاف بدهکار و بستانکار این صفحه: <strong className="text-white">{formatPrice(imbalance)} تومان</strong></span>
            </>
          )}
        </div>
      </div>

      {ledgerError && (
        <div className="bg-rose-50 border border-rose-200 rounded-xl p-3 flex items-center gap-2 text-rose-700 text-[11px]">
          <AlertCircle className="w-4 h-4 shrink-0" strokeWidth={1.5} />
          <span>دفتر کل از سرور بارگذاری نشد. اتصال را بررسی کنید؛ هیچ عددی حدس زده نمی‌شود.</span>
        </div>
      )}

      {/* 2. Treasury cash position */}
      <TreasuryCashCard
        cash={currentCash?.cash ?? null}
        cashLoaded={currentCash !== null}
        cashError={currentCash?.error ?? null}
        cashDays={cashDays}
        onCashDaysChange={(days) => {
          if (days === cashDays) return;
          setCashResponse(null);
          setCashDays(days);
        }}
        formatPrice={formatPrice}
        toPersianDigits={toPersianDigits}
      />

      {/* 3. Profit locks */}
      <ProfitLocksCard
        locks={locks}
        locksError={locksError}
        onNavigateTab={onNavigateTab}
        formatPrice={formatPrice}
        toPersianDigits={toPersianDigits}
      />

      {/* 4. Key Balances */}
      <div className="grid grid-cols-2 gap-2">
        <div className="bg-white rounded-2xl p-4 border border-slate-200/90 shadow-2xs">
          <div className="flex items-center justify-between mb-1">
            <span className="text-[11px] font-bold text-slate-700">ورودی شاپرک</span>
            <DollarSign className="w-3.5 h-3.5 text-blue-600" strokeWidth={1.5} />
          </div>
          <div className="text-sm font-extrabold text-slate-900">
            {ledgerValue(shaparakIn)} <span className="text-[9px] font-normal text-slate-500">تومان</span>
          </div>
          <span className="text-[9px] text-blue-600 font-bold block mt-1">بر اساس حداکثر ۱۰۰ سند از ۳۱ روز اخیر</span>
        </div>

        <PendingSettlementsCard
          pending={pending}
          pendingError={pendingError}
          pendingLoaded={pendingLoaded}
          formatPrice={formatPrice}
          toPersianDigits={toPersianDigits}
        />

        <div className="bg-white rounded-2xl p-4 border border-slate-200/90 shadow-2xs">
          <div className="flex items-center justify-between mb-1">
            <span className="text-[11px] font-bold text-slate-700">اسناد دفتر کل</span>
            <FileText className="w-3.5 h-3.5 text-purple-600" strokeWidth={1.5} />
          </div>
          <div className="text-sm font-extrabold text-slate-900">
            {ledgerLoaded && !ledgerError ? <>{toPersianDigits(ledger.length)} <span className="text-[9px] font-normal text-slate-500">سند</span></> : <span className="text-[11px] font-medium text-slate-400">—</span>}
          </div>
          <span className="text-[9px] text-purple-600 font-medium block mt-1">حداکثر ۱۰۰ سند از ۳۱ روز اخیر</span>
        </div>

        <div className="bg-white rounded-2xl p-4 border border-slate-200/90 shadow-2xs">
          <div className="flex items-center justify-between mb-1">
            <span className="text-[11px] font-bold text-slate-700">تسویه‌های واریزی</span>
            <CheckCircle2 className="w-3.5 h-3.5 text-emerald-600" strokeWidth={1.5} />
          </div>
          <div className="text-sm font-extrabold text-emerald-600">
            {ledgerValue(shaparakOut)} <span className="text-[9px] font-normal text-slate-500">تومان</span>
          </div>
          <span className="text-[9px] text-emerald-600 font-bold block mt-1">بر اساس حداکثر ۱۰۰ سند از ۳۱ روز اخیر</span>
        </div>
      </div>

      {/* 5. Action Shortcuts */}
      <div className="grid grid-cols-2 gap-2">
        <button
          onClick={() => onNavigateTab('payouts')}
          className="p-2.5 bg-white border border-slate-200/90 rounded-xl text-right flex items-center gap-2 hover:bg-slate-50 shadow-2xs"
        >
          <ArrowDownToLine className="w-4 h-4 text-emerald-600 shrink-0" strokeWidth={1.5} />
          <div>
            <span className="text-[11px] font-bold text-slate-800 block">درخواست‌های تسویه</span>
            <span className="text-[9px] text-slate-400 block">تأیید حواله شبا</span>
          </div>
        </button>

        <button
          onClick={() => onNavigateTab('disputes')}
          className="p-2.5 bg-white border border-slate-200/90 rounded-xl text-right flex items-center gap-2 hover:bg-slate-50 shadow-2xs"
        >
          <ShieldCheck className="w-4 h-4 text-teal-600 shrink-0" strokeWidth={1.5} />
          <div>
            <span className="text-[11px] font-bold text-slate-800 block">پرونده‌های مرجوعی</span>
            <span className="text-[9px] text-slate-400 block">تعدیل و بازپرداخت</span>
          </div>
        </button>
      </div>

      {/* Ledger Report Action */}
      <button
        onClick={() => setShowLedgerModal(true)}
        className="w-full py-2.5 bg-slate-900 hover:bg-slate-800 text-white font-bold text-xs rounded-xl shadow-xs transition-colors flex items-center justify-center gap-1.5"
      >
        <FileText className="w-4 h-4 text-teal-400" strokeWidth={1.5} />
        <span>مشاهده اسناد روزنامه و دفتر کل</span>
      </button>

      <FinanceLedgerDetailModal
        isOpen={showLedgerModal}
        onClose={() => setShowLedgerModal(false)}
      />
    </div>
  );
};
