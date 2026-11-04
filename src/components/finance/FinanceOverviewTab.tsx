import React, { useState, useEffect } from 'react';
import { useStore } from '../../context/StoreContext';
import { FinanceLedgerDetailModal } from './FinanceLedgerDetailModal';
import { Sparkline } from './Sparkline';
import { financeApi, LedgerEntryDto, CashPositionDto, ProfitLocksDto, PendingSettlementDto } from '../../services/financeApi';
import { BadgePercent, DollarSign, ArrowDownToLine, ShieldCheck, CheckCircle2, FileText, AlertCircle } from 'lucide-react';

interface Props {
  onNavigateTab: (tab: string) => void;
}

// Live spec (AdminDashboardView-MERGED.js, Nov 2 18:38 build): this tab
// renders ONLY from server data — GET /finance/ledger (bare array),
// GET /finance/treasury/cash-position?days= (success-wrapped),
// GET /finance/settlements/pending ({settlements}) and
// GET /finance/treasury/profit-locks (success-wrapped). The previous
// version derived its figures from client orders plus hard-coded
// constants and never called the ledger endpoint, so the ledger rows
// could never appear here.
export const FinanceOverviewTab: React.FC<Props> = ({ onNavigateTab }) => {
  const { formatPrice, toPersianDigits } = useStore();
  const [showLedgerModal, setShowLedgerModal] = useState(false);

  const [ledger, setLedger] = useState<LedgerEntryDto[]>([]);
  const [ledgerLoaded, setLedgerLoaded] = useState(false);
  const [ledgerError, setLedgerError] = useState(false);

  const [cashDays, setCashDays] = useState(30);
  const [cash, setCash] = useState<CashPositionDto | null>(null);
  const [cashLoaded, setCashLoaded] = useState(false);
  const [cashError, setCashError] = useState(false);

  const [pending, setPending] = useState<PendingSettlementDto[]>([]);
  const [locks, setLocks] = useState<ProfitLocksDto | null>(null);

  useEffect(() => {
    setCashLoaded(false);
    setCashError(false);
    financeApi.getCashPosition(cashDays)
      .then((res) => { setCash(res && res.success ? res : null); setCashLoaded(true); })
      .catch(() => { setCashError(true); setCashLoaded(true); });
  }, [cashDays]);

  useEffect(() => {
    financeApi.getLedger()
      .then((data) => { setLedger(Array.isArray(data) ? data : []); setLedgerLoaded(true); })
      .catch(() => { setLedgerError(true); setLedgerLoaded(true); });
    financeApi.getPendingSettlements()
      .then((data) => setPending(data))
      .catch(() => setPending([]));
    financeApi.getProfitLocks()
      .then((res) => setLocks(res && res.success ? res : null))
      .catch(() => setLocks(null));
  }, []);

  const sumWhere = (pred: (e: LedgerEntryDto) => boolean) =>
    ledger.filter(pred).reduce((sum, e) => sum + e.amount, 0);
  const shaparakIn = sumWhere((e) => e.type === 'debit' && e.targetAccount === 'shaparak_clearing');
  const shaparakOut = sumWhere((e) => e.type === 'credit' && e.targetAccount === 'shaparak_clearing');
  const totalDebit = sumWhere((e) => e.type === 'debit');
  const totalCredit = sumWhere((e) => e.type === 'credit');
  const imbalance = totalDebit - totalCredit;
  const pendingSum = pending.reduce((sum, p) => sum + (Number(p.amount) || 0), 0);
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
        <p className="text-[11px] text-slate-600 leading-relaxed">اینجا تصویر کلی پول پلتفرم را می‌بینی: چقدر پول وارد شده، چقدر به فروشنده‌ها و تأمین‌کننده‌ها بدهکاریم و چقدر از پول را می‌توانیم با خیال راحت خرج کنیم. حواست بیشتر به «مبلغ قابل خرج‌کرد امن» باشد و اگر تراز دفتر کل صفر نبود، اول همان مغایرت را بررسی کن.</p>
        <p className="text-[10px] text-slate-500 leading-relaxed">در حسابداری دوبل، جمع بدهکارها و بستانکارها باید دقیقاً برابر باشد. اگر عدد «مغایرت باز» صفر نباشد یعنی یک سند ناقص یا اشتباه ثبت شده و تا اصلاحش نباید به عددهای این صفحه اعتماد کامل کرد.</p>
        <p className="text-[10px] text-slate-400 leading-relaxed">مثال: اگر ورودی شاپرک ۱۰ میلیون است ولی فقط ۹ میلیون سند خروجی داری، ۱ میلیون مغایرت داری و باید سندی که جا افتاده را پیدا کنی.</p>
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
                    تراز متوازن
                  </span>
                )}
              </h2>
              <span className="text-[10px] text-slate-400 block mt-0.5">
                حسابداری دوبل، تطبیق درگاه شاپرک و تسویه‌های شبا
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
              <span>وضعیت تطبیق بانکی: <strong className="text-emerald-400">{toPersianDigits(ledger.length)} سند</strong></span>
              <span>مغایرت باز: <strong className="text-white">{formatPrice(imbalance)} تومان</strong></span>
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
                onClick={() => setCashDays(d)}
                className={`px-2 py-0.5 rounded-md transition-all ${cashDays === d ? 'bg-white text-teal-700 shadow-2xs' : 'text-slate-500'}`}
              >
                {toPersianDigits(d)} روزه
              </button>
            ))}
          </div>
        </div>
        <p className="px-4 text-[10px] text-slate-400 leading-relaxed">یعنی همین حالا چقدر پول واقعاً دست پلتفرم است و چقدر از آن را بدهکار دیگرانیم. عدد مهم این کارت، «مبلغ قابل خرج‌کرد امن» است، نه ورودی کل.</p>
        {cashError ? (
          <div className="p-3.5 text-[11px] text-rose-600">خطا در بارگذاری موقعیت نقدینگی.</div>
        ) : !cashLoaded ? (
          <div className="p-3.5 text-[11px] text-slate-400">در حال محاسبه از داده‌های واقعی…</div>
        ) : !cash || (cash.allTimeIn === 0 && cash.allTimeOut === 0) ? (
          <div className="p-3.5 text-[11px] text-slate-500 leading-relaxed">
            <span className="font-bold text-slate-600 block mb-1">—</span>
            هنوز هیچ سفارش پرداخت‌شده‌ای در سیستم ثبت نشده است؛ پس از اولین پرداخت قطعی، ورودی/خروجی و مبلغ قابل خرج‌کرد امن اینجا محاسبه می‌شود.
          </div>
        ) : (
          <>
            <div className={`mx-4 mb-2.5 mt-2.5 rounded-xl p-3 text-white ${cash.overObligated ? 'bg-gradient-to-l from-rose-700 via-rose-800 to-rose-900' : 'bg-gradient-to-l from-slate-900 via-teal-950 to-slate-900'}`}>
              <div className="flex items-center justify-between">
                <span className="text-[11px] font-bold opacity-90 flex items-center">مبلغ قابل خرج‌کرد امن</span>
                {cash.overObligated && (
                  <span className="text-[9px] bg-white/20 border border-white/30 px-1.5 py-0.5 rounded-full font-bold">تعهدات بیش از نقدینگی!</span>
                )}
              </div>
              <div className="text-xl font-extrabold mt-1" dir="rtl">{formatPrice(cash.safeToSpend)} <span className="text-[10px] font-normal opacity-80">تومان</span></div>
              <p className="text-[9px] opacity-70 mt-1 leading-relaxed">ورودی کل منهای خروجی کل، تسویه‌های در صف، مبالغ بلوکه در سفارش‌ها و بدهی به فروشگاه‌ها و تأمین‌کنندگان</p>
            </div>
            <div className="grid grid-cols-2 gap-2 px-4">
              <div className="bg-emerald-50/60 border border-emerald-100 rounded-xl p-2.5">
                <div className="flex items-center gap-1 text-emerald-700">
                  <span className="text-[10px] font-bold">ورودی {toPersianDigits(cashDays)} روز</span>
                </div>
                <div className="text-sm font-extrabold text-slate-900 mt-0.5">{formatPrice(cash.period.in)} <span className="text-[9px] font-normal text-slate-500">تومان</span></div>
                <Sparkline data={(cash.period.trend || []).map((t) => t.in ?? 0)} color="#059669" height={38} ariaLabel="روند ورودی نقدینگی" />
              </div>
              <div className="bg-rose-50/60 border border-rose-100 rounded-xl p-2.5">
                <div className="flex items-center gap-1 text-rose-700">
                  <span className="text-[10px] font-bold">خروجی {toPersianDigits(cashDays)} روز</span>
                </div>
                <div className="text-sm font-extrabold text-slate-900 mt-0.5">{formatPrice(cash.period.out)} <span className="text-[9px] font-normal text-slate-500">تومان</span></div>
                <Sparkline data={(cash.period.trend || []).map((t) => t.out ?? 0)} color="#e11d48" height={38} ariaLabel="روند خروجی نقدینگی" />
              </div>
            </div>
            <div className="p-4 pt-2.5 space-y-1.5 text-[11px]">
              <div className="flex justify-between"><span className="text-slate-500">بلوکه در سفارش‌ها ({toPersianDigits(cash.heldOrderCount)} سفارش پرداخت‌شده آزادنشده)</span><strong className="text-slate-700">{formatPrice(cash.heldInOrders)} تومان</strong></div>
              <div className="flex justify-between"><span className="text-slate-500">بدهی به دراپ‌شیپرها (سود آزادشده)</span><strong className="text-slate-700">{formatPrice(cash.owedToStores)} تومان</strong></div>
              <div className="flex justify-between"><span className="text-slate-500">بدهی به تأمین‌کنندگان (سهم عمده‌فروشی آزادشده)</span><strong className="text-slate-700">{formatPrice(cash.owedToSuppliers)} تومان</strong></div>
              <div className="flex justify-between"><span className="text-slate-500">بازپرداخت‌های در انتظار مشتری ({toPersianDigits(cash.pendingRefundCount || 0)} مورد)</span><strong className="text-amber-700">{formatPrice(cash.pendingRefunds || 0)} تومان</strong></div>
              <div className="flex justify-between"><span className="text-slate-500">در صف تسویه شبا ({toPersianDigits(cash.pendingCount)} درخواست)</span><strong className="text-amber-700">{formatPrice(cash.pendingPayouts)} تومان</strong></div>
              <div className="flex justify-between"><span className="text-slate-500">سهم محقق‌شده پلتفرم</span><strong className="text-slate-700">{formatPrice(cash.platformEarned)} تومان</strong></div>
              {cash.note && (
                <p className="text-[9px] text-slate-400 leading-relaxed pt-1">{cash.note}</p>
              )}
              {(cash.sellerInFlight || 0) + (cash.supplierInFlight || 0) > 0 && (
                <p className="text-[9px] text-slate-400 leading-relaxed">از بدهی‌های بالا، {formatPrice((cash.sellerInFlight || 0) + (cash.supplierInFlight || 0))} تومان تأیید شده اما هنوز منتقل نشده است (در مسیر پایا) — فقط «تکمیل‌شده» یعنی پول خارج‌شده.</p>
              )}
            </div>
          </>
        )}
      </div>

      {/* 3. Profit locks */}
      {locks && (locks.totals.locked > 0 || locks.totals.releasable > 0) && (
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
      )}

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
          <span className="text-[9px] text-blue-600 font-bold block mt-1">تراکنش‌های تأییدشده در دفتر کل</span>
        </div>

        <div className="bg-white rounded-2xl p-4 border border-slate-200/90 shadow-2xs">
          <div className="flex items-center justify-between mb-1">
            <span className="text-[11px] font-bold text-slate-700">در صف تسویه</span>
            <ArrowDownToLine className="w-3.5 h-3.5 text-amber-600" strokeWidth={1.5} />
          </div>
          <div className="text-sm font-extrabold text-slate-900">
            {ledgerLoaded ? formatPrice(pendingSum) : '—'} <span className="text-[9px] font-normal text-slate-500">تومان</span>
          </div>
          <span className="text-[9px] text-amber-600 font-medium block mt-1">
            {toPersianDigits(pending.length)} درخواست شبا
          </span>
        </div>

        <div className="bg-white rounded-2xl p-4 border border-slate-200/90 shadow-2xs">
          <div className="flex items-center justify-between mb-1">
            <span className="text-[11px] font-bold text-slate-700">اسناد دفتر کل</span>
            <FileText className="w-3.5 h-3.5 text-purple-600" strokeWidth={1.5} />
          </div>
          <div className="text-sm font-extrabold text-slate-900">
            {ledgerLoaded && !ledgerError ? <>{toPersianDigits(ledger.length)} <span className="text-[9px] font-normal text-slate-500">سند</span></> : <span className="text-[11px] font-medium text-slate-400">—</span>}
          </div>
          <span className="text-[9px] text-purple-600 font-medium block mt-1">ثبت‌های دوبل حسابداری</span>
        </div>

        <div className="bg-white rounded-2xl p-4 border border-slate-200/90 shadow-2xs">
          <div className="flex items-center justify-between mb-1">
            <span className="text-[11px] font-bold text-slate-700">تسویه‌های واریزی</span>
            <CheckCircle2 className="w-3.5 h-3.5 text-emerald-600" strokeWidth={1.5} />
          </div>
          <div className="text-sm font-extrabold text-emerald-600">
            {ledgerValue(shaparakOut)} <span className="text-[9px] font-normal text-slate-500">تومان</span>
          </div>
          <span className="text-[9px] text-emerald-600 font-bold block mt-1">حواله‌های قطعی پایا</span>
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
        <span>مشاهده تراز کامل دفتر کل و اسناد روزنامه</span>
      </button>

      <FinanceLedgerDetailModal
        isOpen={showLedgerModal}
        onClose={() => setShowLedgerModal(false)}
      />
    </div>
  );
};
