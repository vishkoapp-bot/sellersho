import React, { useState, useEffect, useCallback, useRef } from 'react';
import { useStore } from '../../context/StoreContext';
import { storeApi } from '../../services/storeApi';
import { isMockMode } from '../../services/apiClient';
import { ContextSupportButton } from '../support/ContextSupportButton';
import { SupplierSectionHeading } from '../supplier/SupplierSectionHeading';
import { SupplierEmptyState, SupplierResourceState } from '../supplier/SupplierResourceState';
import { RotateCcw, Search, RefreshCw, CheckCircle2, Clock, XCircle, Package } from 'lucide-react';

// W5 (live parity): seller returns tab rebuilt to the live component
// (SellerDashboardView-MERGED.js, returns component ~byte 179800-187000).
// Live record shape is snake_case (return_code, order_number, created_at,
// items[{product_title, variant_name, qty, qc_status}], refunded_amount,
// return_shipping_amount, return_shipping_bearer, wallet_credited_amount,
// profit_locked, profit_reduced_amount). The tab is read-only: live shows
// no approve/reject actions. CamelCase fallbacks are accepted defensively
// without changing backend semantics.

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

// Live QC badge (jn in the live bundle): ok "سالم", defective "معیوب",
// anything else "در انتظار بررسی".
const QcBadge: React.FC<{ status: string | undefined }> = ({ status }) => {
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
  id: raw.id ?? raw.returnId ?? raw.return_id ?? '',
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
  const [loading, setLoading] = useState(() => !isMockMode() && Boolean(currentStore?.slug));
  const [loadError, setLoadError] = useState<string | null>(null);
  const request = useRef(0);

  const reload = useCallback(async () => {
    const generation = ++request.current;
    const slug = currentStore?.slug;
    setLoadError(null);
    if (isMockMode() || !slug) {
      setReturns([]);
      setLoading(false);
      return;
    }
    setLoading(true);
    try {
      const list = await storeApi.getStoreReturns(slug);
      if (generation === request.current) setReturns((Array.isArray(list) ? list : []).map(normalizeReturn));
    } catch (err: any) {
      if (generation === request.current) setLoadError(err?.message || 'دریافت مرجوعی‌های فروشگاه انجام نشد.');
    } finally {
      if (generation === request.current) setLoading(false);
    }
  }, [currentStore?.slug]);

  useEffect(() => {
    void reload();
    return () => { request.current += 1; };
  }, [reload]);

  const search = query.trim();
  const filtered = returns.filter(
    (r) => String(r.returnCode).includes(search) || String(r.orderNumber).includes(search)
  );

  return (
    <div className="space-y-4 pb-6 sm:space-y-5">
      <SupplierSectionHeading title="مرجوعی‌های مشتریان" description="وضعیت مرجوعی‌ها و اثرشان روی سود فروشگاهت را ببین. بررسی کالا و بازپرداخت با پلتفرم است." icon={<RotateCcw />} />

      <div className="flex items-center gap-2 rounded-[20px] border border-[#e6e2f0] bg-white p-3 shadow-sm">
        <div className="relative min-w-0 flex-1">
          <Search aria-hidden="true" className="pointer-events-none absolute inset-s-3 top-3.5 size-4 text-slate-500" strokeWidth={1.5} />
          <input
            type="search"
            aria-label="جستجوی مرجوعی‌ها"
            placeholder="کد مرجوعی یا شماره سفارش"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            className="min-h-11 w-full rounded-xl border border-[#e6e2f0] bg-[#f8f6fc] ps-9 pe-3 text-sm focus:bg-white focus-visible:ring-2 focus-visible:ring-brand"
          />
        </div>
        <button
          type="button"
          onClick={() => void reload()}
          disabled={loading}
          className="grid size-11 shrink-0 place-items-center rounded-xl border border-brand-border text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50"
          aria-label="تازه‌سازی مرجوعی‌ها"
        >
          <RefreshCw aria-hidden="true" className="size-4" strokeWidth={1.5} />
        </button>
      </div>

      <SupplierResourceState loading={loading} error={loadError ?? ''} retry={() => void reload()} />

      {!loading && !loadError && filtered.length === 0 && (
        search ? <SupplierEmptyState icon={<RotateCcw />} title="با این جستجو مرجوعی‌ای پیدا نشد."
          action={<button type="button" onClick={() => setQuery('')} className="min-h-11 rounded-xl bg-brand-soft px-4 text-sm font-bold text-brand-deep focus-visible:ring-2 focus-visible:ring-brand">پاک کردن جستجو</button>} />
          : <SupplierEmptyState icon={<RotateCcw />} title="هنوز مرجوعی‌ای برای فروشگاهت ثبت نشده است." description="وقتی مشتری کالایی را مرجوع کند، وضعیتش اینجا نمایش داده می‌شود." />
      )}

      {!loading && !loadError && filtered.map((r: any) => {
        const status = STATUS_MAP[r.status] || STATUS_MAP.requested;
        const StatusIcon = status.Icon;
        const bearer = bearerLine(r.returnShippingBearer);
        return (
          <article key={r.id || r.returnCode} className="space-y-3 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
            <div className="flex flex-wrap items-start justify-between gap-2 border-b border-[#eeebf4] pb-3">
              <div className="min-w-0">
                <h3 className="text-sm font-extrabold text-[#261b41]">مرجوعی <bdi dir="ltr" className="break-all font-mono">{r.returnCode || String(r.id).slice(0, 8)}</bdi></h3>
                <p className="mt-1 text-xs text-slate-600">
                  سفارش <bdi dir="ltr" className="break-all">{r.orderNumber}</bdi> · <time dateTime={r.createdAt || undefined}>{formatReturnDate(r.createdAt)}</time>
                </p>
              </div>
              <span className={`inline-flex items-center gap-1 rounded-full border px-2 py-1 text-xs font-bold ${status.cls}`}>
                <StatusIcon aria-hidden="true" className="size-3 shrink-0" />
                {status.label}
              </span>
            </div>

            {r.profitLocked ? (
              <p className="rounded-xl border border-amber-200 bg-amber-50 px-3 py-2 text-sm font-bold text-amber-800">سود این سفارش تا پایان مرجوعی قفل است.</p>
            ) : r.profitReducedAmount > 0 ? (
              <p className="rounded-xl border border-rose-200 bg-rose-50 px-3 py-2 text-sm font-bold text-rose-800">سود آزادشده {formatPrice(r.profitReducedAmount)} تومان کمتر شد.</p>
            ) : null}

            <ul className="space-y-1">
              {(r.items || []).map((item: any, idx: number) => (
                <li key={idx} className="flex flex-wrap items-center justify-between gap-2 rounded-xl bg-[#f6f3fb] px-3 py-2 text-sm">
                  <span className="min-w-0 break-words font-bold text-slate-700">
                    {item.productTitle}
                    {item.variantName ? ` — ${item.variantName}` : ''}
                  </span>
                  <span className="flex flex-wrap items-center gap-2">
                    <span className="text-slate-500">{toPersianDigits(item.qty)} عدد</span>
                    <QcBadge status={item.qcStatus} />
                  </span>
                </li>
              ))}
            </ul>

            {r.refundedAmount != null && (
              <details>
                <summary className="min-h-11 cursor-pointer rounded-lg px-2 py-2.5 text-sm font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand">جزئیات بازپرداخت و هزینهٔ مرجوعی</summary>
                <dl className="space-y-1 rounded-xl bg-slate-50 p-3 text-sm text-slate-600">
                  <div className="flex flex-wrap justify-between gap-x-4">
                    <dt>بهای برگشتی</dt>
                    <dd className="font-bold text-slate-800">{formatPrice(r.refundedAmount)} تومان</dd>
                  </div>
                  {r.returnShippingAmount != null && (
                    <div className="flex flex-wrap justify-between gap-x-4">
                      <dt>هزینه مرجوعی</dt>
                      <dd className="font-bold text-slate-800">{formatPrice(r.returnShippingAmount)} تومان</dd>
                      {bearer && <dd className="w-full text-xs leading-6">{bearer}</dd>}
                    </div>
                  )}
                  {r.returnShippingAmount == null && bearer && <div><dt className="sr-only">پرداخت‌کنندهٔ هزینهٔ مرجوعی</dt><dd>{bearer}</dd></div>}
                  {r.walletCreditedAmount != null && (
                    <div className="flex flex-wrap justify-between gap-x-4 border-t border-slate-200 pt-1">
                      <dt>واریز به کیف پول مشتری</dt>
                      <dd className="font-bold text-slate-800">{formatPrice(r.walletCreditedAmount)} تومان</dd>
                    </div>
                  )}
                </dl>
              </details>
            )}

            <ContextSupportButton role="seller" kind="return" referenceId={r.id} storeId={currentStore.id} className="inline-flex min-h-11 items-center gap-1.5 rounded-lg border border-brand-border px-2.5 text-xs font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand" />
          </article>
        );
      })}
    </div>
  );
};
