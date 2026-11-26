import React, { useState, useEffect, useCallback, useRef } from 'react';
import { useStore } from '../../context/StoreContext';
import { getSellerOrderPage } from '../../services/panelWorkApi';
import { isMockMode } from '../../services/apiClient';
import { OrderDetailModal } from '../common/OrderDetailModal';
import { ContextSupportButton } from '../support/ContextSupportButton';
import { SupplierSectionHeading } from '../supplier/SupplierSectionHeading';
import { SupplierEmptyState, SupplierResourceState } from '../supplier/SupplierResourceState';
import { Package, Truck, CheckCircle2, Clock, Search, Eye, AlertCircle, XCircle, Printer } from 'lucide-react';

const ORDER_FILTERS = [
  { followUp: false, label: 'همه سفارش‌ها' },
  { followUp: true, label: 'نیازمند پیگیری' },
] as const;

const primaryActionClass = 'inline-flex min-h-11 items-center justify-center gap-1.5 rounded-xl bg-brand-deep px-3 py-2 text-sm font-bold text-white hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2';
const secondaryActionClass = 'inline-flex min-h-11 items-center justify-center gap-1.5 rounded-xl border border-brand-border px-3 py-2 text-sm font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand';

// Live shared date helper shape (fa-IR, year + long month + day; '—' when
// missing/invalid), as used for Et(createdAt) in the live row header.
const formatOrderDate = (value: string): string => {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '—';
  return date.toLocaleDateString('fa-IR', { year: 'numeric', month: 'long', day: 'numeric' });
};

// Live order normalizer is a light camelCase pass-through (the live API
// already serves camelCase); accept snake_case defensively so a raw
// backend row still renders, without changing any field semantics.
export const normalizeOrder = (raw: any): any => {
  const ci = raw.customerInfo || {};
  return {
    ...raw,
    orderNumber: String(raw.orderNumber ?? raw.order_number ?? ''),
    trackingCode: String(raw.trackingCode ?? raw.tracking_code ?? ''),
    createdAt: raw.createdAt ?? raw.created_at ?? '',
    payableAmount: raw.payableAmount ?? raw.payable_amount ?? raw.totalAmount ?? raw.total_amount ?? 0,
    dropshipperProfit: raw.dropshipperProfit ?? raw.dropshipper_profit ?? 0,
    paymentStatus: raw.paymentStatus ?? raw.payment_status ?? null,
    disputeReason: raw.disputeReason ?? raw.dispute_reason,
    customerInfo: {
      ...ci,
      province: ci.province ?? raw.customerProvince ?? raw.customer_province ?? '',
      city: ci.city ?? raw.customerCity ?? raw.customer_city ?? '',
    },
    items: (raw.items || []).map((it: any) => {
      const title = it.product?.title || it.productTitle || it.product_title;
      return {
        ...it,
        product: title ? { ...it.product, title } : undefined,
        variantName: it.variant?.name ?? it.variant_name ?? it.variantName ?? '',
      };
    }),
  };
};

// Live payment badge (index-MERGED.js j3/N3, ~byte 683196-683528):
// paid "پرداخت شد", pending "در انتظار پرداخت", failed "پرداخت ناموفق",
// cancelled "پرداخت لغو شد", refund_pending "در انتظار بازپرداخت";
// unknown status renders its raw value; null renders nothing.
const PaymentStatusBadge: React.FC<{ status: string | null | undefined }> = ({ status }) => {
  if (!status) return null;
  const map: Record<string, { label: string; cls: string; Icon: any }> = {
    paid: { label: 'پرداخت شد', cls: 'text-emerald-700 bg-emerald-50 border-emerald-200', Icon: CheckCircle2 },
    pending: { label: 'در انتظار پرداخت', cls: 'text-amber-700 bg-amber-50 border-amber-200', Icon: Clock },
    failed: { label: 'پرداخت ناموفق', cls: 'text-rose-700 bg-rose-50 border-rose-200', Icon: XCircle },
    cancelled: { label: 'پرداخت لغو شد', cls: 'text-slate-500 bg-slate-100 border-slate-200', Icon: XCircle },
    refund_pending: { label: 'در انتظار بازپرداخت', cls: 'text-brand-deep bg-brand-soft border-brand-border', Icon: Clock },
  };
  const entry = map[status];
  if (!entry) {
    return (
      <span className="text-[10px] font-bold px-2 py-0.5 rounded-full flex items-center gap-1 border text-slate-500 bg-slate-100 border-slate-200">
        <Clock className="w-3 h-3" />
        <span>{status}</span>
      </span>
    );
  }
  const Icon = entry.Icon;
  return (
    <span className={`text-[10px] font-bold px-2 py-0.5 rounded-full flex items-center gap-1 border ${entry.cls}`}>
      <Icon className="w-3 h-3" />
      <span>{entry.label}</span>
    </span>
  );
};

interface SellerOrdersTabProps {
  onNavigateTab?: (tab: string) => void;
  onOpenStorefront?: () => void;
  initialWorkFilter?: 'follow-up';
}

export const SellerOrdersTab: React.FC<SellerOrdersTabProps> = ({ onNavigateTab, onOpenStorefront, initialWorkFilter }) => {
  const { orders, currentStore, formatPrice, toPersianDigits } = useStore();
  const [searchTerm, setSearchTerm] = useState('');
  const [selectedOrder, setSelectedOrder] = useState<any | null>(null);
  const [list, setList] = useState<any[]>([]);
  const [loading, setLoading] = useState(() => !isMockMode() && Boolean(currentStore?.slug));
  const [loadError, setLoadError] = useState<string | null>(null);
  const [nextCursor, setNextCursor] = useState('');
  const [loadingMore, setLoadingMore] = useState(false);
  const [followUpOnly, setFollowUpOnly] = useState(initialWorkFilter === 'follow-up');
  const request = useRef(0);

  const reload = useCallback(async (cursor?: string) => {
    const generation = ++request.current;
    setLoadError(null);
    // Mock mode has no backend: keep the previous context-orders source.
    if (isMockMode()) {
      setList(
        orders
          .filter((o) => o.storeId === currentStore.id || currentStore.ownerType === 'central')
          .map((o) => normalizeOrder(o))
      );
      return;
    }
    const slug = currentStore?.slug;
    if (!slug) {
      setList([]);
      setLoading(false);
      setLoadingMore(false);
      return;
    }
    setLoading(!cursor);
    setLoadingMore(Boolean(cursor));
    try {
      const page = await getSellerOrderPage(slug, followUpOnly ? 'follow-up' : undefined, cursor);
      if (request.current !== generation) return;
      const received = (page.orders ?? []).map(normalizeOrder);
      setList(previous => cursor ? [...previous, ...received] : received);
      setNextCursor(page.hasMore ? page.nextCursor ?? '' : '');
    } catch (err: any) {
      if (request.current === generation) setLoadError(err?.message || 'دریافت سفارش‌های فروشگاه انجام نشد.');
    } finally {
      if (request.current === generation) {
        setLoading(false);
        setLoadingMore(false);
      }
    }
  }, [currentStore?.slug, currentStore?.id, currentStore?.ownerType, orders, followUpOnly]);

  useEffect(() => {
    setNextCursor('');
    void reload();
    return () => { request.current += 1; };
  }, [reload]);

  const search = searchTerm.trim();
  const filteredOrders = list.filter(
    (o) =>
      (!isMockMode() || !followUpOnly || ['delivery_disputed', 'delivered_unconfirmed'].includes(o.status)) && (
      o.orderNumber.includes(search) ||
      o.trackingCode.includes(search) ||
      String(o.customerInfo.city).includes(search))
  );

  // Live order status badges, verbatim (text-[10px], informal copy).
  const getStatusBadge = (status: string, disputeReason?: string) => {
    switch (status) {
      case 'cancelled':
        return (
          <span className="text-[10px] font-bold text-slate-500 bg-slate-100 border border-slate-200 px-2 py-0.5 rounded-full flex items-center gap-1">
            <XCircle className="w-3 h-3" />
            <span>لغو شده</span>
          </span>
        );
      case 'delivered_confirmed':
      case 'delivered':
        return (
          <span className="text-[10px] font-bold text-emerald-700 bg-emerald-50 border border-emerald-200 px-2 py-0.5 rounded-full flex items-center gap-1">
            <CheckCircle2 className="w-3 h-3 text-emerald-600" strokeWidth={1.5} />
            <span>به دست مشتری رسید</span>
          </span>
        );
      case 'delivered_unconfirmed':
        return (
          <span className="text-[10px] font-bold text-brand-deep bg-brand-soft border border-brand-border px-2 py-0.5 rounded-full flex items-center gap-1">
            <Clock className="w-3 h-3 text-brand-deep" />
            <span>رسیده؛ منتظر تأیید مشتری</span>
          </span>
        );
      case 'delivery_disputed':
        return (
          <span className="inline-flex flex-col gap-0.5">
            <span className="text-[10px] font-bold text-rose-700 bg-rose-50 border border-rose-200 px-2 py-0.5 rounded-full flex items-center gap-1">
              <AlertCircle className="w-3 h-3" />
              <span>مشتری گفته به دستش نرسیده</span>
            </span>
            {disputeReason ? (
              <span className="text-[10px] font-normal text-rose-700 leading-relaxed">دلیل ثبت‌شده: {disputeReason}</span>
            ) : null}
          </span>
        );
      case 'shipped':
        return (
          <span className="text-[10px] font-bold text-brand-deep bg-brand-soft border border-brand-border px-2 py-0.5 rounded-full flex items-center gap-1">
            <Truck className="w-3 h-3" />
            <span>فرستاده شده؛ در راه است</span>
          </span>
        );
      default:
        return (
          <span className="text-[10px] font-bold text-amber-700 bg-amber-50 border border-amber-200 px-2 py-0.5 rounded-full flex items-center gap-1">
            <Clock className="w-3 h-3" />
            <span>در حال آماده‌سازی در انبار</span>
          </span>
        );
    }
  };

  return (
    <div className="space-y-4 pb-6 sm:space-y-5">
      <SupplierSectionHeading title="سفارش‌های من" description="وضعیت ارسال، پرداخت و سود سفارش‌های فروشگاهت را پیگیری کن. ارسال کالا با پلتفرم است." icon={<Truck />} />

      <div className="space-y-3 rounded-[20px] border border-[#e6e2f0] bg-white p-3 shadow-sm sm:p-4">
        <div role="group" aria-label="وضعیت سفارش" className="flex flex-wrap gap-1.5">
          {ORDER_FILTERS.map(({ followUp, label }) => (
            <button key={label} type="button" aria-pressed={followUpOnly === followUp} onClick={() => setFollowUpOnly(followUp)}
              className={`min-h-11 rounded-xl px-3 text-sm font-bold transition-colors focus-visible:ring-2 focus-visible:ring-brand ${followUpOnly === followUp ? 'bg-brand-deep text-white' : 'bg-slate-100 text-slate-700 hover:bg-slate-200'}`}>
              {label}
            </button>
          ))}
        </div>
        <div className="relative">
          <Search aria-hidden="true" className="pointer-events-none absolute inset-s-3 top-3.5 size-4 text-slate-500" strokeWidth={1.5} />
          <input
            type="search"
            aria-label="جستجوی سفارش‌ها"
            placeholder="شماره سفارش، شهر یا کد رهگیری"
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            className="min-h-11 w-full rounded-xl border border-[#e6e2f0] bg-[#f8f6fc] ps-9 pe-3 text-sm focus:bg-white focus-visible:ring-2 focus-visible:ring-brand"
          />
        </div>
      </div>

      <SupplierResourceState loading={loading} error={loadError ?? ''} retry={() => void reload(nextCursor || undefined)} />
      {!loading && !loadError && filteredOrders.length === 0 && (
        search ? <SupplierEmptyState icon={<Package />} title="با این جستجو سفارشی پیدا نشد."
          action={<button type="button" onClick={() => setSearchTerm('')} className={secondaryActionClass}>پاک کردن جستجو</button>} />
          : followUpOnly ? <SupplierEmptyState icon={<Package />} title="سفارشی در صف پیگیری نیست."
            action={<button type="button" onClick={() => setFollowUpOnly(false)} className={secondaryActionClass}>دیدن همه سفارش‌ها</button>} />
            : <SupplierEmptyState icon={<Package />} title="هنوز سفارشی نداری" description="برای اولین فروش، لینک فروشگاهت را با مشتری‌ها به اشتراک بگذار."
              action={(onNavigateTab || onOpenStorefront) && (
                <div className="flex flex-wrap items-center justify-center gap-2">
                  {onNavigateTab && <button type="button" onClick={() => onNavigateTab('links')} className={primaryActionClass}>گرفتن لینک فروشگاه</button>}
                  {onOpenStorefront && <button type="button" onClick={onOpenStorefront} className={secondaryActionClass}>مشاهده فروشگاه من</button>}
                </div>
              )} />
      )}
      {!loading && (!loadError || nextCursor) && filteredOrders.length > 0 && (
        <div className="space-y-2.5">
          {filteredOrders.map((order) => (
            <article
              key={order.id}
              className="space-y-3 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5"
            >
              <div className="flex flex-wrap items-start justify-between gap-2 border-b border-[#eeebf4] pb-3">
                <div className="min-w-0">
                  <h3 className="text-sm font-extrabold text-brand-deep">سفارش <bdi dir="ltr" className="break-all font-mono">{order.orderNumber}</bdi></h3>
                  <time dateTime={order.createdAt || undefined} className="mt-1 block text-xs text-slate-600">{formatOrderDate(order.createdAt)}</time>
                </div>
                <div className="flex flex-wrap items-start gap-1.5">
                  {getStatusBadge(order.status, order.disputeReason)}
                  <PaymentStatusBadge status={order.paymentStatus} />
                </div>
              </div>

              <dl className="grid grid-cols-2 gap-3 rounded-xl bg-[#f6f3fb] p-3 text-sm">
                <div className="min-w-0">
                  <dt className="text-xs text-slate-600">مبلغ سفارش</dt>
                  <dd className="break-words font-bold tabular-nums text-slate-800">{formatPrice(order.payableAmount)} تومان</dd>
                </div>
                <div className="min-w-0">
                  <dt className="text-xs text-slate-600">سود تو از این سفارش</dt>
                  <dd className="break-words font-extrabold tabular-nums text-emerald-700">{formatPrice(order.dropshipperProfit)} تومان</dd>
                </div>
              </dl>

              {(order.customerInfo.province || order.customerInfo.city || order.trackingCode) && (
                <dl className="flex flex-wrap gap-x-5 gap-y-2 text-xs text-slate-600">
                  {(order.customerInfo.province || order.customerInfo.city) && <div className="flex flex-wrap gap-1"><dt>مقصد</dt><dd className="font-bold text-slate-800">{[order.customerInfo.province, order.customerInfo.city].filter(Boolean).join('، ')}</dd></div>}
                  {order.trackingCode && <div className="flex min-w-0 flex-wrap gap-1"><dt>کد رهگیری پستی</dt><dd><bdi dir="ltr" className="break-all font-mono font-bold text-brand-deep">{order.trackingCode}</bdi></dd></div>}
                </dl>
              )}

              {order.items.length > 0 && (
                <ul className="divide-y divide-slate-100 text-xs text-slate-600">
                  {order.items.map((item: any, idx: number) => (
                    <li key={idx} className="flex items-start justify-between gap-3 py-1.5">
                      <span className="min-w-0 break-words">
                        {item.product?.title || 'کالای سفارشی'}
                        {item.variantName ? <span> — {item.variantName}</span> : null}
                      </span>
                      <span className="shrink-0 font-bold">{toPersianDigits(item.quantity)} عدد</span>
                    </li>
                  ))}
                </ul>
              )}

              <div className="flex flex-wrap items-start gap-x-3 gap-y-1 border-t border-slate-100 pt-3">
                <button type="button" onClick={() => setSelectedOrder(order)} className={primaryActionClass}>
                  <Eye aria-hidden="true" className="size-4" strokeWidth={1.5} />
                  دیدن جزئیات
                </button>
                <details className="min-w-0">
                  <summary className="min-h-11 cursor-pointer rounded-lg px-2 py-2.5 text-sm font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand">{isMockMode() ? 'چاپ' : 'چاپ و پشتیبانی'}</summary>
                  <div className="mt-2 flex flex-wrap gap-2">
                    <button type="button" onClick={() => window.print()} className={secondaryActionClass}><Printer aria-hidden="true" className="size-4" /> چاپ فاکتور</button>
                    {!isMockMode() && (
                      <>
                        <ContextSupportButton role="seller" kind="order" referenceId={order.id} storeId={currentStore.id} />
                        {order.trackingCode && <ContextSupportButton role="seller" kind="shipment" referenceType="order_fulfillment" referenceId={order.id} storeId={currentStore.id} />}
                      </>
                    )}
                  </div>
                </details>
              </div>
            </article>
          ))}
        </div>
      )}

      {!loading && nextCursor && <button type="button" onClick={() => void reload(nextCursor)} disabled={loadingMore} className={`${secondaryActionClass} disabled:opacity-60`}>{loadingMore ? 'در حال دریافت سفارش‌های قدیمی‌تر…' : 'بارگذاری سفارش‌های قدیمی‌تر'}</button>}

      <OrderDetailModal
        isOpen={!!selectedOrder}
        order={selectedOrder}
        hideSensitiveCustomerInfo={true}
        onClose={() => setSelectedOrder(null)}
      />
    </div>
  );
};
