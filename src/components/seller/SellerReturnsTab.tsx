import React, { useState, useEffect, useCallback } from 'react';
import { useStore } from '../../context/StoreContext';
import { storeApi } from '../../services/storeApi';
import { isMockMode } from '../../services/apiClient';
import { RotateCcw, Search, RefreshCw, AlertCircle, Info, Loader2, CheckCircle2, Clock, XCircle, Package } from 'lucide-react';

// W5 (live parity): seller returns tab rebuilt to the live component
// (SellerDashboardView-MERGED.js, returns component ~byte 179800-187000).
// Live record shape is snake_case (return_code, order_number, created_at,
// items[{product_title, variant_name, qty, qc_status}], refunded_amount,
// return_shipping_amount, return_shipping_bearer, wallet_credited_amount,
// profit_locked, profit_reduced_amount). The tab is read-only: live shows
// no approve/reject actions. CamelCase fallbacks are accepted defensively
// without changing backend semantics.

// Live intro banner (Pe, text-only call).
const ReturnsIntroBanner: React.FC = () => (
  <div className="rounded-2xl border border-brand-border bg-brand-soft/50 px-3.5 py-2.5">
    <div className="flex items-start gap-2">
      <Info className="w-4 h-4 text-brand-deep shrink-0 mt-0.5" strokeWidth={1.5} />
      <p className="flex-1 text-xs leading-6 text-slate-700">
        این‌جا مرجوعی‌های مشتری‌های فروشگاهت را می‌بینی؛ لازم نیست کاری بکنی — بررسی و تسویه با ادمین و انبار است، ولی اثر هر مرجوعی روی سودت همین‌جا نوشته شده.
      </p>
    </div>
  </div>
);

// Live shared date helper shape (fa-IR, year + long month + day; '—' when
// missing/invalid), as used for Et(created_at) in the live record subline.
const formatReturnDate = (value: string): string => {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '—';
  return date.toLocaleDateString('fa-IR', { year: 'numeric', month: 'long', day: 'numeric' });
};

// Live return status map (gs in the live bundle).
const STATUS_MAP: Record<string, { label: string; cls: string; Icon: any }> = {
  requested: { label: 'درخواست‌شده', cls: 'text-amber-700 bg-amber-50 border-amber-200', Icon: Clock },
  approved: { label: 'تأییدشده', cls: 'text-brand-deep bg-brand-soft border-brand-border', Icon: CheckCircle2 },
  rejected: { label: 'ردشده', cls: 'text-rose-700 bg-rose-50 border-rose-200', Icon: XCircle },
  received: { label: 'دریافت‌شده', cls: 'text-sky-700 bg-sky-50 border-sky-200', Icon: Package },
  refunded: { label: 'بازپرداخت‌شده', cls: 'text-emerald-700 bg-emerald-50 border-emerald-200', Icon: CheckCircle2 },
  completed: { label: 'تکمیل‌شده', cls: 'text-emerald-700 bg-emerald-50 border-emerald-200', Icon: CheckCircle2 },
  cancelled: { label: 'لغوشده', cls: 'text-slate-500 bg-slate-100 border-slate-200', Icon: XCircle },
};

// Recorded QC findings stay visible on cancelled returns.
// Only open returns show outstanding checks as "در انتظار بررسی".
const QcBadge: React.FC<{ status: string | undefined; cancelled: boolean }> = ({ status, cancelled }) => {
  if (status === 'ok') {
    return (
      <span className="text-[10px] font-bold text-emerald-700 bg-emerald-50 border border-emerald-200 px-2 py-0.5 rounded-full">سالم</span>
    );
  }
  if (status === 'defective') {
    return (
      <span className="text-[10px] font-bold text-rose-700 bg-rose-50 border border-rose-200 px-2 py-0.5 rounded-full">معیوب</span>
    );
  }
  if (cancelled) return null;
  return (
    <span className="text-[10px] font-bold text-amber-700 bg-amber-50 border border-amber-200 px-2 py-0.5 rounded-full">در انتظار بررسی</span>
  );
};

// Live return-shipping bearer line (fn in the live bundle).
const bearerLine = (bearer: string | undefined): string | null => {
  switch (bearer) {
    case 'supplier':
      return 'هزینه مرجوعی از حساب تأمین‌کننده پرداخت شد';
    case 'customer':
      return 'هزینه مرجوعی از کیف پول مشتری کسر شد';
    case 'mixed':
      return 'هزینه مرجوعی ترکیبی است (بخشی تأمین‌کننده، بخشی مشتری)';
    default:
      return null;
  }
};

// Accept the live snake_case record first, with camelCase fallbacks.
const normalizeReturn = (raw: any): any => ({
  ...raw,
  id: raw.id ?? raw.return_code ?? '',
  returnCode: raw.return_code ?? raw.returnCode ?? '',
  orderNumber: raw.order_number ?? raw.orderNumber ?? raw.orderId ?? raw.order_id ?? '',
  createdAt: raw.created_at ?? raw.createdAt ?? '',
  status: raw.status ?? '',
  items: (raw.items || []).map((it: any) => ({
    ...it,
    productTitle: it.product_title ?? it.productTitle ?? it.title ?? '',
    variantName: it.variant_name ?? it.variantName ?? '',
    qty: it.qty ?? it.quantity ?? 0,
    qcStatus: it.qc_status ?? it.qcStatus,
  })),
  refundedAmount: raw.refunded_amount ?? raw.refundedAmount ?? null,
  returnShippingAmount: raw.return_shipping_amount ?? raw.returnShippingAmount ?? null,
  returnShippingBearer: raw.return_shipping_bearer ?? raw.returnShippingBearer,
  walletCreditedAmount: raw.wallet_credited_amount ?? raw.walletCreditedAmount ?? null,
  profitLocked: raw.profit_locked ?? raw.profitLocked ?? false,
  profitReducedAmount: raw.profit_reduced_amount ?? raw.profitReducedAmount ?? 0,
});

export const SellerReturnsTab: React.FC = () => {
  const { currentStore, formatPrice, toPersianDigits } = useStore();
  const [query, setQuery] = useState('');
  const [returns, setReturns] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);

  const reload = useCallback(async () => {
    const slug = currentStore?.slug;
    if (isMockMode() || !slug) {
      setReturns([]);
      return;
    }
    setLoading(true);
    setLoadError(null);
    try {
      const list = await storeApi.getStoreReturns(slug);
      setReturns((Array.isArray(list) ? list : []).map((r: any) => normalizeReturn(r)));
    } catch (err: any) {
      setLoadError(err?.message || 'دریافت مرجوعی‌های فروشگاه انجام نشد.');
    } finally {
      setLoading(false);
    }
  }, [currentStore?.slug]);

  useEffect(() => {
    reload();
  }, [reload]);

  const filtered = returns.filter(
    (r) => (r.returnCode || '').includes(query) || (r.orderNumber || '').includes(query)
  );

  return (
    <div className="space-y-3 pb-6">
      <ReturnsIntroBanner />

      {/* Search + refresh (live: search filters return_code / order_number;
          the refresh icon re-fetches the list). */}
      <div className="relative flex items-center bg-white border border-slate-200 rounded-xl px-2.5 py-2 shadow-2xs">
        <Search className="w-3.5 h-3.5 text-slate-400 shrink-0 ml-1.5" strokeWidth={1.5} />
        <input
          type="text"
          placeholder="جستجو: کد مرجوعی یا شماره سفارش…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="flex-1 bg-transparent outline-none text-xs placeholder:text-slate-400"
        />
        <button
          type="button"
          onClick={() => void reload()}
          className="shrink-0 p-1.5 rounded-lg text-slate-500 hover:bg-slate-100"
          aria-label="تازه‌سازی"
        >
          <RefreshCw className="w-3.5 h-3.5" />
        </button>
      </div>

      {loading && (
        <div className="flex items-center justify-center gap-2 py-10 text-slate-500 text-xs">
          <Loader2 className="w-4 h-4 animate-spin" />
          <span>در حال دریافت مرجوعی‌ها…</span>
        </div>
      )}

      {!loading && loadError && (
        <div className="rounded-xl border border-rose-200 bg-rose-50 px-3.5 py-3 text-xs font-bold text-rose-700 flex items-center gap-2">
          <AlertCircle className="w-4 h-4 shrink-0" />
          <span>{loadError}</span>
        </div>
      )}

      {!loading && !loadError && filtered.length === 0 && (
        <div className="rounded-2xl border border-dashed border-slate-200 bg-white px-4 py-10 text-center">
          <RotateCcw className="w-6 h-6 text-slate-300 mx-auto mb-2" strokeWidth={1.5} />
          <p className="text-xs font-bold text-slate-700">هنوز مرجوعی‌ای برای فروشگاه شما ثبت نشده است</p>
          <p className="text-[11px] text-slate-400 mt-1">وقتی مشتری‌ای کالایی را مرجوع کند، همین‌جا با وضعیت و اثرش روی سودت می‌بینی.</p>
        </div>
      )}

      {!loading && !loadError && filtered.map((r: any) => {
        const status = STATUS_MAP[r.status] || STATUS_MAP.requested;
        const StatusIcon = status.Icon;
        const bearer = bearerLine(r.returnShippingBearer);
        return (
          <article key={r.id || r.returnCode} className="bg-white border border-slate-200 rounded-2xl p-3.5 space-y-2.5 shadow-2xs">
            <div className="flex items-start justify-between gap-2">
              <div className="min-w-0">
                <div className="flex items-center gap-2 flex-wrap">
                  <span className="text-xs font-bold text-slate-800" dir="ltr">
                    {r.returnCode || String(r.id).slice(0, 8)}
                  </span>
                  <span className={`text-[10px] font-bold border px-2 py-0.5 rounded-full flex items-center gap-1 ${status.cls}`}>
                    <StatusIcon className="w-3 h-3" />
                    <span>{status.label}</span>
                  </span>
                </div>
                <p className="text-[11px] text-slate-500 mt-1">
                  سفارش {r.orderNumber} · {formatReturnDate(r.createdAt)}
                </p>
              </div>
            </div>

            <div className="space-y-1">
              {(r.items || []).map((item: any, idx: number) => (
                <div key={idx} className="flex items-center justify-between gap-2 text-[11px] bg-slate-50 border border-slate-100 rounded-lg px-2.5 py-1.5">
                  <span className="text-slate-700 font-bold truncate">
                    {item.productTitle}
                    {item.variantName ? ` — ${item.variantName}` : ''}
                  </span>
                  <span className="flex items-center gap-2 shrink-0">
                    <span className="text-slate-500">{toPersianDigits(item.qty)} عدد</span>
                    <QcBadge status={item.qcStatus} cancelled={r.status === 'cancelled'} />
                  </span>
                </div>
              ))}
            </div>

            {r.refundedAmount != null && (
              <div className="rounded-xl bg-slate-50 border border-slate-100 px-3 py-2 space-y-1 text-[11px]">
                <div className="flex justify-between">
                  <span className="text-slate-500">بهای برگشتی</span>
                  <span className="font-bold text-slate-800">{formatPrice(r.refundedAmount)} تومان</span>
                </div>
                {r.returnShippingAmount != null && (
                  <div className="flex justify-between">
                    <span className="text-slate-500">هزینه مرجوعی</span>
                    <span className="font-bold text-slate-800">{formatPrice(r.returnShippingAmount)} تومان</span>
                  </div>
                )}
                {bearer && <p className="text-slate-500">{bearer}</p>}
                {r.walletCreditedAmount != null && (
                  <div className="flex justify-between border-t border-slate-200 pt-1">
                    <span className="text-slate-500">واریز به کیف پول مشتری</span>
                    <span className="font-bold text-slate-800">{formatPrice(r.walletCreditedAmount)} تومان</span>
                  </div>
                )}
              </div>
            )}

            {r.profitLocked ? (
              <p className="rounded-xl bg-amber-50 border border-amber-200 px-3 py-2 text-[11px] font-bold text-amber-700">
                سود این سفارش تا پایان مرجوعی قفل است.
              </p>
            ) : r.profitReducedAmount > 0 ? (
              <p className="rounded-xl bg-rose-50 border border-rose-200 px-3 py-2 text-[11px] font-bold text-rose-700">
                سود آزادشده {formatPrice(r.profitReducedAmount)} تومان کمتر شد.
              </p>
            ) : null}
          </article>
        );
      })}
    </div>
  );
};
